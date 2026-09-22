// The demo session: the in-memory model of eight registered parties, their
// notes, the directional channels between them, and every payment published so
// far. One session lives for the lifetime of the process; restarting the
// launcher against a fresh Hardhat node is the reset.
//
// Everything here is backed by real contract calls. The session caches secrets
// (spend keys, view keys, channel shared secrets) because in a real deployment
// each party's wallet would hold its own — the "Act as" selector is a local
// identity switch across those wallets, not an authentication boundary.
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dvpcore "github.com/raylsnetwork/enygma_dvp/src/core"
	tags "github.com/raylsnetwork/enygma_retail_payments/private_tags/src"
	rpcore "github.com/raylsnetwork/enygma_retail_payments/src/core"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ── Demo parameters ───────────────────────────────────────────────────────────

const (
	seedAmount     = int64(100) // starting private balance for each controllable party
	defaultPayment = int64(30)  // pre-filled amount in the payment form
	demoTokenID    = int64(0)
)

// partyBlueprint describes one row of the registry before any chain state exists.
// hardhatAccount 2 is deliberately absent: that key belongs to the relayer.
type partyBlueprint struct {
	Name         string
	Account      int
	Controllable bool
}

var partyBlueprints = []partyBlueprint{
	{Name: "Alice", Account: 0, Controllable: true},
	{Name: "Bob", Account: 1, Controllable: true},
	{Name: "Charlie", Account: 3, Controllable: true},
	{Name: "Diana", Account: 4},
	{Name: "Evan", Account: 5},
	{Name: "Fatima", Account: 6},
	{Name: "Grace", Account: 7},
	{Name: "Henry", Account: 8},
}

// ── State model ───────────────────────────────────────────────────────────────

type partyState struct {
	Index        int // registry index — assigned by registration order
	Name         string
	Account      int
	Controllable bool

	Address common.Address
	auth    *bind.TransactOpts

	spend *rpcore.SpendKeyPair
	view  *rpcore.ViewKeyPair

	Registered     bool
	RegTxHash      string
	RegBlock       uint64
	RegGas         uint64
	OnChainPkSpend *big.Int
	OnChainPkView  []byte
}

// note is one unspent output the demo knows about.
//
// Discovered separates "this note exists on-chain" from "its owner can spend
// it". A payment's destination note starts undiscovered: the recipient only
// learns its salt after scanning the tag registry, which is exactly the step the
// demo is there to show.
type note struct {
	ID         string
	Owner      int
	Amount     *big.Int
	TokenID    *big.Int
	Salt       *big.Int
	Commitment *big.Int
	Origin     string // seed | change | received
	Spent      bool
	Discovered bool
	Block      uint64
}

// channelPreview is a fully-computed channel setup that has not been published.
// Holding the exact artifacts — not just the parameters — guarantees the bitmap
// the UI highlights is bit-for-bit the one that reaches the chain.
type channelPreview struct {
	ID           string
	Sender       int
	Recipient    int
	Mode         string
	ModeCode     tags.PrivacyMode
	Excluded     []int
	Candidates   []int
	Bitmap       []byte
	sharedSecret []byte
	c1           []byte
	c2           []byte
}

// openChannel is a published channel setup record, keyed by ordered sender →
// recipient. Re-establishing a channel for the same pair replaces this entry;
// payments always use the latest.
type openChannel struct {
	Sender     int
	Recipient  int
	Mode       string
	ModeCode   tags.PrivacyMode
	Excluded   []int
	Candidates []int
	Bitmap     []byte
	// OnChainBitmap is read back from TagChannelRegistry after publication so the
	// UI can show that what was previewed is what was stored.
	OnChainBitmap []byte
	sharedSecret  []byte
	ChannelIdx    uint64
	TxHash        string
	Block         uint64
	Gas           uint64
}

// paymentRecord is everything one guided "send payment" action put on-chain.
type paymentRecord struct {
	Seq       int
	Sender    int
	Recipient int
	Amount    *big.Int
	Change    *big.Int
	TokenID   *big.Int

	Nullifier      *big.Int
	MerkleRootUsed *big.Int
	DestCommitment *big.Int
	ChangeCommit   *big.Int
	CipherTextLen  int
	EncTxDataLen   int

	PayTxHash string
	PayBlock  uint64
	PayGas    uint64

	TagTxHash    string
	TagBlock     uint64
	TagGas       uint64
	PublishedTag string
	TagCtxtLen   int

	ProofMs   int64
	RelayMs   int64
	RootAfter *big.Int
	LeafCount int
	CreatedAt time.Time
}

// channelScanResult is one channel as seen by the scanning party.
type channelScanResult struct {
	ChannelIdx uint64           `json:"channelIdx"`
	Outcome    string           `json:"outcome"` // skipped | decoy | matched
	Detail     string           `json:"detail"`
	Bits       string           `json:"bits"`
	Notes      []scanNoteResult `json:"notes,omitempty"`
}

type scanNoteResult struct {
	Tag          string `json:"tag"`
	Block        uint64 `json:"block"`
	Amount       string `json:"amount"`
	TokenID      string `json:"tokenId"`
	Salt         string `json:"salt"`
	Commitment   string `json:"commitment"`
	Verified     bool   `json:"verified"`
	Detail       string `json:"detail"`
	AlreadyKnown bool   `json:"alreadyKnown"`
}

type scanRecord struct {
	Actor    int                 `json:"actor"`
	At       string              `json:"at"`
	Channels []channelScanResult `json:"channels"`
	Summary  string              `json:"summary"`
}

// ── Session ───────────────────────────────────────────────────────────────────

type session struct {
	mu     sync.Mutex
	broker *Broker

	root   string
	client *ethclient.Client
	gnark  *dvpcore.GnarkClient
	reader *chainReader

	vaultAddr           common.Address
	erc20Addr           common.Address
	registryAddr        common.Address
	dvpAddr             common.Address
	tagRegistryAddr     common.Address
	channelRegistryAddr common.Address
	relayerAddr         string
	relayerWarning      string

	vaultABI    abi.ABI
	erc20ABI    abi.ABI
	registryABI abi.ABI

	// minter signs RaylsERC20.mint — the contracts were deployed by Hardhat
	// account 0, which is the only holder of the minter role.
	minter *bind.TransactOpts

	parties  []*partyState
	actor    int // registry index of the party being acted as; -1 when none
	notes    []*note
	channels map[string]*openChannel
	preview  *channelPreview
	payments []*paymentRecord
	lastScan *scanRecord

	registered bool
	busy       string
	noteSeq    int
	paySeq     int
	previewSeq int
}

func channelKey(sender, recipient int) string { return fmt.Sprintf("%d>%d", sender, recipient) }

// newSession wires the session to the chain and the deployed contracts. It
// deliberately does not touch the relayer or gnark — those are probed per action
// so the UI can report a dependency that goes away mid-demo.
func newSession(broker *Broker) (*session, error) {
	root, err := projectRoot()
	if err != nil {
		return nil, err
	}
	receipts, err := loadReceipts(root)
	if err != nil {
		return nil, err
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", rpcURL, err)
	}

	vaultABI, err := loadContractABI(filepath.Join(root, "contracts", "abis", "Erc20CoinVault.json"))
	if err != nil {
		return nil, err
	}
	erc20ABI, err := loadContractABI(filepath.Join(root, "contracts", "abis", "RaylsERC20.json"))
	if err != nil {
		return nil, err
	}
	registryABI, err := loadContractABI(filepath.Join(root, "contracts", "user_registry", "UserRegistry.json"))
	if err != nil {
		return nil, err
	}
	channelABI, err := loadContractABI(filepath.Join(root, "private_tags", "contracts", "TagChannelRegistry.json"))
	if err != nil {
		return nil, err
	}

	s := &session{
		broker:              broker,
		root:                root,
		client:              client,
		gnark:               rpcore.NewPaymentClient(gnarkURL),
		vaultAddr:           common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress),
		erc20Addr:           common.HexToAddress(receipts["ERC20"].ContractAddress),
		registryAddr:        common.HexToAddress(receipts["UserRegistry"].ContractAddress),
		dvpAddr:             common.HexToAddress(receipts["EnygmaDvp"].ContractAddress),
		tagRegistryAddr:     common.HexToAddress(receipts["TagRegistry"].ContractAddress),
		channelRegistryAddr: common.HexToAddress(receipts["TagChannelRegistry"].ContractAddress),
		vaultABI:            vaultABI,
		erc20ABI:            erc20ABI,
		registryABI:         registryABI,
		actor:               -1,
		channels:            map[string]*openChannel{},
	}
	s.reader = &chainReader{
		client:      client,
		channelABI:  channelABI,
		channelAddr: s.channelRegistryAddr,
	}

	deployerKey, _, err := accountKey(0)
	if err != nil {
		return nil, fmt.Errorf("derive the deploying account: %w", err)
	}
	if s.minter, err = makeAuth(deployerKey); err != nil {
		return nil, fmt.Errorf("transactor for the deploying account: %w", err)
	}

	for i, bp := range partyBlueprints {
		privHex, addr, err := accountKey(bp.Account)
		if err != nil {
			return nil, fmt.Errorf("derive Hardhat account %d for %s: %w", bp.Account, bp.Name, err)
		}
		auth, err := makeAuth(privHex)
		if err != nil {
			return nil, fmt.Errorf("transactor for %s: %w", bp.Name, err)
		}
		s.parties = append(s.parties, &partyState{
			Index:        i,
			Name:         bp.Name,
			Account:      bp.Account,
			Controllable: bp.Controllable,
			Address:      addr,
			auth:         auth,
		})
	}
	return s, nil
}

// ── Event helpers ─────────────────────────────────────────────────────────────

func (s *session) log(cat, msg string) {
	s.broker.publish(Event{Type: "log", Cat: cat, Msg: msg, TS: time.Now().Format("15:04:05.000")})
}

func (s *session) progress(action, step, status, label, msg string) {
	s.broker.publish(Event{Type: "progress", Action: action, Step: step, Status: status, Label: label, Msg: msg})
}

func (s *session) pushState() { s.broker.publish(Event{Type: "state"}) }

// ── Action gate ───────────────────────────────────────────────────────────────

// begin claims the session for one long-running action. Registration, proving,
// and mining all mutate shared state, so a second action is refused rather than
// interleaved.
func (s *session) begin(action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy != "" {
		return fmt.Errorf("%s is still running — wait for it to finish", s.busy)
	}
	s.busy = action
	s.broker.publish(Event{Type: "busy", Action: action})
	return nil
}

func (s *session) end() {
	s.mu.Lock()
	s.busy = ""
	s.mu.Unlock()
	s.broker.publish(Event{Type: "busy", Action: ""})
	s.pushState()
}

// ── Lookups ───────────────────────────────────────────────────────────────────

func (s *session) party(idx int) (*partyState, error) {
	if idx < 0 || idx >= len(s.parties) {
		return nil, fmt.Errorf("no party with index %d", idx)
	}
	return s.parties[idx], nil
}

// spendableNotes returns the actor's unspent, discovered notes, smallest first,
// so note selection is deterministic and the smallest sufficient note wins.
func (s *session) spendableNotes(owner int) []*note {
	var out []*note
	for _, n := range s.notes {
		if n.Owner == owner && !n.Spent && n.Discovered && n.Amount.Sign() > 0 {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Amount.Cmp(out[j].Amount) < 0 })
	return out
}

func (s *session) balanceOf(owner int) int64 {
	total := int64(0)
	for _, n := range s.spendableNotes(owner) {
		total += n.Amount.Int64()
	}
	return total
}

func (s *session) pendingOf(owner int) int64 {
	total := int64(0)
	for _, n := range s.notes {
		if n.Owner == owner && !n.Spent && !n.Discovered {
			total += n.Amount.Int64()
		}
	}
	return total
}

func (s *session) addNote(n *note) {
	s.noteSeq++
	n.ID = fmt.Sprintf("n%d", s.noteSeq)
	s.notes = append(s.notes, n)
}

// ── Registration ──────────────────────────────────────────────────────────────

// registerAll generates fresh keypairs for all eight parties, registers them in
// order (registration order is the row index shared by both tables), then seeds
// the three controllable parties with one private note each.
func (s *session) registerAll() error {
	if err := s.begin("registration"); err != nil {
		return err
	}
	defer s.end()

	s.mu.Lock()
	already := s.registered
	s.mu.Unlock()
	if already {
		return fmt.Errorf("parties are already registered — restart the launcher for a clean chain")
	}
	if err := s.requireChain(); err != nil {
		return err
	}

	ctx := context.Background()
	s.progress("register", "keys", "running", "Generate keypairs", "BabyJubJub spend keys + ML-KEM-768 view keys for 8 parties…")

	for _, p := range s.parties {
		spend, err := rpcore.NewSpendKeyPair()
		if err != nil {
			s.progress("register", "keys", "error", "Generate keypairs", err.Error())
			return fmt.Errorf("%s spend keypair: %w", p.Name, err)
		}
		view, err := rpcore.NewViewKeyPair()
		if err != nil {
			s.progress("register", "keys", "error", "Generate keypairs", err.Error())
			return fmt.Errorf("%s view keypair: %w", p.Name, err)
		}
		s.mu.Lock()
		p.spend, p.view = spend, view
		s.mu.Unlock()
	}
	s.log("key", "8 spend keypairs (Poseidon/BabyJubJub) + 8 ML-KEM-768 view keypairs generated locally")
	s.progress("register", "keys", "success", "Generate keypairs", "16 keypairs generated")

	// The auditor path is out of scope for this demo, but UserRegistry enforces
	// the exact ciphertext sizes, so register correctly-sized placeholders.
	stubMlKemCt := make([]byte, 1088)
	stubAesCt := make([]byte, 92)

	s.progress("register", "submit", "running", "Register on-chain", "Submitting 8 UserRegistry.register() transactions…")
	registry := bind.NewBoundContract(s.registryAddr, s.registryABI, s.client, s.client, s.client)

	for _, p := range s.parties {
		s.broker.publish(Event{Type: "row", Index: p.Index, Phase: "pending"})

		tx, err := registry.Transact(p.auth, "register", p.spend.PublicKey, p.view.EncapsKey, stubMlKemCt, stubAesCt)
		if err != nil {
			if strings.Contains(err.Error(), "AlreadyRegistered") || strings.Contains(err.Error(), "45ed80e9") {
				err = fmt.Errorf("%s is already registered on this chain — the demo needs a clean chain, so restart the launcher", p.Name)
			}
			s.progress("register", "submit", "error", "Register on-chain", err.Error())
			return fmt.Errorf("UserRegistry.register(%s): %w", p.Name, err)
		}
		receipt, err := waitMined(ctx, s.client, tx)
		if err != nil {
			s.progress("register", "submit", "error", "Register on-chain", err.Error())
			return fmt.Errorf("register(%s): %w", p.Name, err)
		}

		// Read the keys back from chain — the table shows what the registry
		// holds, not what the demo believes it wrote.
		pkSpend, pkView, err := s.readRegistryKeys(p.Address)
		if err != nil {
			return fmt.Errorf("read back keys for %s: %w", p.Name, err)
		}
		idx, err := tags.GetRecipientIndex(s.client, s.registryAddr, p.Address)
		if err != nil {
			return fmt.Errorf("registry index for %s: %w", p.Name, err)
		}

		s.mu.Lock()
		p.Registered = true
		p.RegTxHash = receipt.TxHash.Hex()
		p.RegBlock = receipt.BlockNumber.Uint64()
		p.RegGas = receipt.GasUsed
		p.OnChainPkSpend = pkSpend
		p.OnChainPkView = pkView
		p.Index = idx
		s.mu.Unlock()

		s.broker.publish(Event{Type: "row", Index: p.Index, Phase: "mined"})
		s.log("chain", fmt.Sprintf("UserRegistry.register(%s) → index %d  block %d  gas %s",
			p.Name, idx, receipt.BlockNumber.Uint64(), formatNum(receipt.GasUsed)))
		s.pushState()
	}
	s.progress("register", "submit", "success", "Register on-chain", "8 parties registered in order")

	if err := s.seedControllableParties(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.registered = true
	s.mu.Unlock()
	s.log("", "Registry complete — Alice, Bob and Charlie each hold one private 100-token note")
	return nil
}

func (s *session) readRegistryKeys(addr common.Address) (*big.Int, []byte, error) {
	c := bind.NewBoundContract(s.registryAddr, s.registryABI, s.client, s.client, s.client)
	var out []interface{}
	if err := c.Call(&bind.CallOpts{}, &out, "getKeys", addr); err != nil {
		return nil, nil, fmt.Errorf("UserRegistry.getKeys(%s): %w", addr.Hex(), err)
	}
	if len(out) < 2 {
		return nil, nil, fmt.Errorf("getKeys returned %d values, want 2", len(out))
	}
	pkSpend, _ := out[0].(*big.Int)
	pkView, _ := out[1].([]byte)
	return pkSpend, pkView, nil
}

// seedControllableParties gives Alice, Bob and Charlie one spendable note each
// so any of them can pay either peer from the first click.
func (s *session) seedControllableParties(ctx context.Context) error {
	s.progress("register", "seed", "running", "Fund private balances",
		fmt.Sprintf("Depositing %d tokens for each controllable party…", seedAmount))

	tokenID := big.NewInt(demoTokenID)
	amount := big.NewInt(seedAmount)
	vault := bind.NewBoundContract(s.vaultAddr, s.vaultABI, s.client, s.client, s.client)
	erc20 := bind.NewBoundContract(s.erc20Addr, s.erc20ABI, s.client, s.client, s.client)

	for _, p := range s.parties {
		if !p.Controllable {
			continue
		}
		// RaylsERC20 gates mint() behind a role held only by the deploying
		// account, so the tokens are minted by the deployer and then deposited
		// by the party itself.
		mintTx, err := erc20.Transact(s.minter, "mint", p.Address, amount)
		if err != nil {
			s.progress("register", "seed", "error", "Fund private balances", err.Error())
			return fmt.Errorf("ERC20.mint(%s): %w", p.Name, err)
		}
		if _, err := waitMined(ctx, s.client, mintTx); err != nil {
			return fmt.Errorf("ERC20.mint(%s): %w", p.Name, err)
		}
		approveTx, err := erc20.Transact(p.auth, "approve", s.vaultAddr, amount)
		if err != nil {
			return fmt.Errorf("ERC20.approve(%s): %w", p.Name, err)
		}
		if _, err := waitMined(ctx, s.client, approveTx); err != nil {
			return fmt.Errorf("ERC20.approve(%s): %w", p.Name, err)
		}

		// Deposit salt and payload key come from an ML-KEM encapsulation against
		// the party's own view key — the same construction a real deposit uses.
		ss, capsule, err := rpcore.Encapsulate(p.view.EncapsKey)
		if err != nil {
			return fmt.Errorf("encapsulate for %s: %w", p.Name, err)
		}
		saltBytes, err := rpcore.DerivePaymentSalt(ss)
		if err != nil {
			return fmt.Errorf("derive deposit salt for %s: %w", p.Name, err)
		}
		encKey, err := rpcore.DerivePaymentKey(ss)
		if err != nil {
			return fmt.Errorf("derive deposit key for %s: %w", p.Name, err)
		}
		salt := rpcore.SaltBToField(saltBytes)

		commitment, err := rpcore.Erc20CommitmentV2(p.spend.PublicKey, salt, amount, tokenID)
		if err != nil {
			return fmt.Errorf("commitment for %s: %w", p.Name, err)
		}
		ctxt, err := rpcore.EncryptPayload(encKey, tokenID, amount)
		if err != nil {
			return fmt.Errorf("encrypt deposit payload for %s: %w", p.Name, err)
		}

		depositTx, err := vault.Transact(p.auth, "depositV2",
			[]*big.Int{amount, p.spend.PublicKey, salt, tokenID}, capsule, ctxt)
		if err != nil {
			s.progress("register", "seed", "error", "Fund private balances", err.Error())
			return fmt.Errorf("Erc20CoinVault.depositV2(%s): %w", p.Name, err)
		}
		receipt, err := waitMined(ctx, s.client, depositTx)
		if err != nil {
			return fmt.Errorf("depositV2(%s): %w", p.Name, err)
		}

		s.mu.Lock()
		s.addNote(&note{
			Owner:      p.Index,
			Amount:     new(big.Int).Set(amount),
			TokenID:    new(big.Int).Set(tokenID),
			Salt:       salt,
			Commitment: commitment,
			Origin:     "seed",
			Discovered: true,
			Block:      receipt.BlockNumber.Uint64(),
		})
		s.mu.Unlock()

		s.broker.publish(Event{Type: "leaf", Value: hexBig(commitment)})
		s.log("chain", fmt.Sprintf("Erc20CoinVault.depositV2(%s, %d tokens) → block %d  gas %s",
			p.Name, seedAmount, receipt.BlockNumber.Uint64(), formatNum(receipt.GasUsed)))
		s.pushState()
	}

	s.progress("register", "seed", "success", "Fund private balances",
		fmt.Sprintf("Alice, Bob and Charlie each hold a %d-token note", seedAmount))
	return nil
}

// ── Actor selection ───────────────────────────────────────────────────────────

// actorSecrets is the only place private key material leaves the session, and
// only for the party currently being acted as.
type actorSecrets struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	SkSpend    string `json:"skSpend"`
	SkView     string `json:"skView"`
	SkViewNote string `json:"skViewNote"`
}

func (s *session) setActor(idx int) (*actorSecrets, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy != "" {
		return nil, fmt.Errorf("%s is still running — wait for it to finish", s.busy)
	}
	p, err := s.party(idx)
	if err != nil {
		return nil, err
	}
	if !p.Controllable {
		return nil, fmt.Errorf("%s is a background anonymity-set user and cannot be acted as", p.Name)
	}
	if !p.Registered {
		return nil, fmt.Errorf("register the parties before acting as %s", p.Name)
	}
	s.actor = idx
	return s.secretsFor(p), nil
}

func (s *session) secretsFor(p *partyState) *actorSecrets {
	if p == nil || p.spend == nil || p.view == nil {
		return nil
	}
	return &actorSecrets{
		Index:      p.Index,
		Name:       p.Name,
		SkSpend:    hexBig(p.spend.PrivateKey),
		SkView:     "0x" + hex.EncodeToString(p.view.DecapsKey.Bytes()),
		SkViewNote: "ML-KEM-768 decapsulation seed (64 bytes)",
	}
}

// ── Channel preview and publication ───────────────────────────────────────────

func parsePrivacyMode(mode string) (tags.PrivacyMode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		return tags.PrivacyNone, nil
	case "subset":
		return tags.PrivacySubset, nil
	case "rift":
		return tags.PrivacyRift, nil
	case "full":
		return tags.PrivacyFull, nil
	}
	return 0, fmt.Errorf("unknown privacy mode %q (want none, subset, rift or full)", mode)
}

// previewChannel computes — but does not publish — the channel artifacts for the
// current actor paying `recipient` under `mode`. The returned bitmap is the one
// that will be sent to the relayer verbatim if the user confirms.
func (s *session) previewChannel(recipient int, mode string, excluded []int) (*channelPreview, error) {
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("%s is still running — wait for it to finish", s.busy)
	}
	sender := s.actor
	s.mu.Unlock()

	if sender < 0 {
		return nil, fmt.Errorf("choose which party you are acting as first")
	}
	if err := s.requireRegistered(); err != nil {
		return nil, err
	}
	senderParty, err := s.party(sender)
	if err != nil {
		return nil, err
	}
	recipientParty, err := s.party(recipient)
	if err != nil {
		return nil, err
	}
	if sender == recipient {
		return nil, fmt.Errorf("%s cannot open a channel to themselves", senderParty.Name)
	}
	if !recipientParty.Registered {
		return nil, fmt.Errorf("%s is not registered yet", recipientParty.Name)
	}
	modeCode, err := parsePrivacyMode(mode)
	if err != nil {
		return nil, err
	}

	totalUsers, err := tags.GetUserCount(s.client, s.registryAddr)
	if err != nil {
		return nil, fmt.Errorf("UserRegistry.getUserCount: %w", err)
	}
	if totalUsers != len(s.parties) {
		return nil, fmt.Errorf("registry holds %d users but the demo expects %d — restart the launcher for a clean chain", totalUsers, len(s.parties))
	}

	// The recipient can never be excluded, and Rift is the only mode where an
	// exclusion list means anything at all.
	cleanExcluded := []int{}
	if modeCode == tags.PrivacyRift {
		seen := map[int]bool{}
		for _, e := range excluded {
			if e < 0 || e >= totalUsers || e == recipient || seen[e] {
				continue
			}
			seen[e] = true
			cleanExcluded = append(cleanExcluded, e)
		}
		sort.Ints(cleanExcluded)
		if len(cleanExcluded) >= totalUsers-1 {
			return nil, fmt.Errorf("excluding %d of %d rows leaves only the recipient — that is the None mode, pick it explicitly", len(cleanExcluded), totalUsers)
		}
	}

	ss, c1, c2, bitmap, err := tags.PrepareChannelSetup(
		recipientParty.OnChainPkView,
		[]byte(fmt.Sprintf("Enygma retail payment channel: %s → %s", senderParty.Name, recipientParty.Name)),
		[]byte(senderParty.Address.Hex()),
		modeCode,
		totalUsers, recipient, cleanExcluded,
	)
	if err != nil {
		return nil, fmt.Errorf("PrepareChannelSetup: %w", err)
	}
	if !bitmapHas(bitmap, recipient) {
		return nil, fmt.Errorf("internal error: bitmap does not include the recipient")
	}

	s.mu.Lock()
	s.previewSeq++
	p := &channelPreview{
		ID:           fmt.Sprintf("pv%d", s.previewSeq),
		Sender:       sender,
		Recipient:    recipient,
		Mode:         strings.ToLower(mode),
		ModeCode:     modeCode,
		Excluded:     cleanExcluded,
		Candidates:   bitmapIndices(bitmap, totalUsers),
		Bitmap:       bitmap,
		sharedSecret: ss,
		c1:           c1,
		c2:           c2,
	}
	s.preview = p
	s.mu.Unlock()

	s.log("tag", fmt.Sprintf("Channel preview %s → %s  mode=%s  bitmap=%s  candidates=%d/%d (not yet published)",
		senderParty.Name, recipientParty.Name, p.Mode, bitmapBits(bitmap, totalUsers), len(p.Candidates), totalUsers))
	s.pushState()
	return p, nil
}

// openChannelFromPreview publishes the retained preview through the relayer so
// msg.sender is the relayer, not the sender.
func (s *session) openChannelFromPreview(previewID string) (*openChannel, error) {
	if err := s.begin("channel setup"); err != nil {
		return nil, err
	}
	defer s.end()

	s.mu.Lock()
	p := s.preview
	s.mu.Unlock()

	if p == nil {
		return nil, fmt.Errorf("no channel preview is prepared — choose a recipient and privacy mode first")
	}
	if previewID != "" && previewID != p.ID {
		return nil, fmt.Errorf("this preview is stale — the channel options changed, review the highlighted rows and try again")
	}
	if err := s.requireRelayer(); err != nil {
		return nil, err
	}
	senderParty, _ := s.party(p.Sender)
	recipientParty, _ := s.party(p.Recipient)

	s.progress("channel", "publish", "running", "Establish channel",
		fmt.Sprintf("Publishing (c1, c2, bitmap) for %s → %s via the relayer…", senderParty.Name, recipientParty.Name))

	var resp relayChannelResponse
	err := postRelayer("/relay/channel", relayChannelRequest{
		C1:     toHex(p.c1),
		C2:     toHex(p.c2),
		Bitmap: toHex(p.Bitmap),
	}, &resp)
	if err != nil {
		s.progress("channel", "publish", "error", "Establish channel", err.Error())
		return nil, err
	}

	// Read the record back so the UI shows the stored bitmap, not the local one.
	rec, err := s.reader.channel(resp.ChannelIdx)
	if err != nil {
		s.progress("channel", "publish", "error", "Establish channel", err.Error())
		return nil, fmt.Errorf("read back channel %d: %w", resp.ChannelIdx, err)
	}

	ch := &openChannel{
		Sender:        p.Sender,
		Recipient:     p.Recipient,
		Mode:          p.Mode,
		ModeCode:      p.ModeCode,
		Excluded:      p.Excluded,
		Candidates:    bitmapIndices(rec.Bitmap, len(s.parties)),
		Bitmap:        p.Bitmap,
		OnChainBitmap: rec.Bitmap,
		sharedSecret:  p.sharedSecret,
		ChannelIdx:    resp.ChannelIdx,
		TxHash:        resp.TxHash,
		Block:         resp.BlockNumber,
		Gas:           resp.GasUsed,
	}

	s.mu.Lock()
	s.channels[channelKey(p.Sender, p.Recipient)] = ch
	s.preview = nil
	s.mu.Unlock()

	for _, idx := range ch.Candidates {
		s.broker.publish(Event{Type: "bit", Index: idx, Phase: "confirmed"})
	}
	s.log("chain", fmt.Sprintf("TagChannelRegistry.openChannel() → channel #%d  block %d  gas %s  tx %s",
		ch.ChannelIdx, ch.Block, formatNum(ch.Gas), shortHex(ch.TxHash, 18)))
	s.log("tag", fmt.Sprintf("Published bitmap %s — %d of %d rows are candidates",
		bitmapBits(rec.Bitmap, len(s.parties)), len(ch.Candidates), len(s.parties)))
	s.progress("channel", "publish", "success", "Establish channel",
		fmt.Sprintf("Channel #%d live — %d candidate rows", ch.ChannelIdx, len(ch.Candidates)))
	return ch, nil
}

// ── Payment ───────────────────────────────────────────────────────────────────

func (s *session) sendPayment(recipient int, amount int64) (*paymentRecord, error) {
	if err := s.begin("payment"); err != nil {
		return nil, err
	}
	defer s.end()

	s.mu.Lock()
	sender := s.actor
	s.mu.Unlock()

	if sender < 0 {
		return nil, fmt.Errorf("choose which party you are acting as first")
	}
	if err := s.requireRegistered(); err != nil {
		return nil, err
	}
	senderParty, err := s.party(sender)
	if err != nil {
		return nil, err
	}
	recipientParty, err := s.party(recipient)
	if err != nil {
		return nil, err
	}
	if sender == recipient {
		return nil, fmt.Errorf("%s cannot pay themselves in this demo", senderParty.Name)
	}
	if !recipientParty.Controllable {
		return nil, fmt.Errorf("%s is a background anonymity-set user — pay Alice, Bob or Charlie so the payment can be scanned for", recipientParty.Name)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be a positive whole number of tokens")
	}

	s.mu.Lock()
	ch := s.channels[channelKey(sender, recipient)]
	s.mu.Unlock()
	if ch == nil {
		return nil, fmt.Errorf("no channel from %s to %s — establish one first (channels are directional)", senderParty.Name, recipientParty.Name)
	}

	// Smallest sufficient note keeps change small and leaves large notes intact.
	var input *note
	s.mu.Lock()
	for _, n := range s.spendableNotes(sender) {
		if n.Amount.Int64() >= amount {
			input = n
			break
		}
	}
	available := s.balanceOf(sender)
	s.mu.Unlock()
	if input == nil {
		return nil, fmt.Errorf("%s has no single unspent note worth %d or more (spendable balance %d across separate notes; this demo spends one note per payment)",
			senderParty.Name, amount, available)
	}
	if err := s.requireGnark(); err != nil {
		return nil, err
	}
	if err := s.requireRelayer(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	tokenID := new(big.Int).Set(input.TokenID)
	payAmt := big.NewInt(amount)
	changeAmt := new(big.Int).Sub(input.Amount, payAmt)

	// 1 — Merkle proof.
	s.progress("payment", "merkle", "running", "Rebuild Merkle proof", "Replaying vault Commitment events to locate the note's leaf…")
	mt, leaves, err := buildVaultMerkleTree(ctx, s.client, s.vaultAddr)
	if err != nil {
		s.progress("payment", "merkle", "error", "Rebuild Merkle proof", err.Error())
		return nil, err
	}
	proof, err := mt.GenerateProof(input.Commitment)
	if err != nil {
		s.progress("payment", "merkle", "error", "Rebuild Merkle proof", err.Error())
		return nil, fmt.Errorf("no on-chain leaf matches %s's selected note: %w", senderParty.Name, err)
	}
	s.progress("payment", "merkle", "success", "Rebuild Merkle proof",
		fmt.Sprintf("Inclusion proof at depth %d over %d leaves (root %s)", merkleDepth, len(leaves), shortHex(hexBig(proof.Root), 14)))
	s.log("zk", fmt.Sprintf("Merkle root %s · %d leaves · spending a %s-token note", shortHex(hexBig(proof.Root), 18), len(leaves), input.Amount))

	// 2 — Groth16 proof.
	s.progress("payment", "prove", "running", "Generate Groth16 proof",
		fmt.Sprintf("Proving %s → %d tokens → %s with %s change…", senderParty.Name, amount, recipientParty.Name, changeAmt))
	proofStart := time.Now()
	result, err := s.gnark.BoundPaymentProof(
		new(big.Int).SetBytes(s.vaultAddr.Bytes()),
		big.NewInt(0),
		[]*big.Int{new(big.Int).Set(input.Amount)},
		[]rpcore.KeyPair{{PrivateKey: senderParty.spend.PrivateKey, PublicKey: senderParty.spend.PublicKey}},
		[]*big.Int{input.Salt},
		[]*big.Int{payAmt, changeAmt},
		[]*big.Int{recipientParty.OnChainPkSpend, senderParty.spend.PublicKey},
		[][]byte{recipientParty.OnChainPkView, senderParty.view.EncapsKey},
		merkleDepth,
		[]*rpcore.MerkleProof{proof},
		[]*big.Int{big.NewInt(0)},
		tokenID,
	)
	proofMs := time.Since(proofStart).Milliseconds()
	if err != nil {
		s.progress("payment", "prove", "error", "Generate Groth16 proof", err.Error())
		return nil, fmt.Errorf("gnark BoundPaymentProof: %w", err)
	}
	stmt := result.ContractStatement()
	nullifier, destCommitment, changeCommitment := stmt[3], stmt[4], stmt[5]
	s.progress("payment", "prove", "success", "Generate Groth16 proof",
		fmt.Sprintf("256-byte proof in %d ms — nullifier and two output commitments are the only public outputs", proofMs))
	s.log("zk", fmt.Sprintf("nullifier %s · dest commitment %s · change commitment %s",
		shortHex(hexBig(nullifier), 18), shortHex(hexBig(destCommitment), 18), shortHex(hexBig(changeCommitment), 18)))

	// 3 — Relay and mine.
	s.progress("payment", "relay", "running", "Relay payment", "POST /relay/payment — the relayer signs and submits as msg.sender…")
	relayStart := time.Now()
	var payResp relayPaymentResponse
	if err := postRelayer("/relay/payment", buildRelayPaymentRequest(result), &payResp); err != nil {
		s.progress("payment", "relay", "error", "Relay payment", err.Error())
		return nil, err
	}
	relayMs := time.Since(relayStart).Milliseconds()
	s.progress("payment", "relay", "success", "Relay payment",
		fmt.Sprintf("EnygmaDvp.payment() mined in block %d — gas %s", payResp.BlockNumber, formatNum(payResp.GasUsed)))
	s.log("chain", fmt.Sprintf("EnygmaDvp.payment() → block %d  gas %s  tx %s",
		payResp.BlockNumber, formatNum(payResp.GasUsed), shortHex(payResp.TxHash, 18)))

	s.broker.publish(Event{Type: "leaf", Value: hexBig(destCommitment)})
	s.broker.publish(Event{Type: "leaf", Value: hexBig(changeCommitment)})

	// 4 — Publish the payment tag on the established channel.
	s.progress("payment", "tag", "running", "Publish tag",
		fmt.Sprintf("Deriving the tag window and encrypting the note for channel #%d…", ch.ChannelIdx))
	startBlock, window, noteCtxt, err := tags.PreparePaymentTag(
		s.client, tagWindowSize,
		recipientParty.OnChainPkSpend,
		ch.sharedSecret,
		payAmt, tokenID, result.SaltB,
	)
	if err != nil {
		s.progress("payment", "tag", "error", "Publish tag", err.Error())
		return nil, fmt.Errorf("PreparePaymentTag: %w", err)
	}
	hexTags := make([]string, len(window))
	for i := range window {
		hexTags[i] = toHex(window[i][:])
	}
	var tagResp relayTagResponse
	if err := postRelayer("/relay/tag", relayTagRequest{
		Tags: hexTags, StartBlock: startBlock, Ctxt: toHex(noteCtxt),
	}, &tagResp); err != nil {
		s.progress("payment", "tag", "error", "Publish tag", err.Error())
		return nil, err
	}

	// Exactly one tag from the window is published — the one for the landing block.
	publishedTag := ""
	if tagResp.BlockNumber >= startBlock && int(tagResp.BlockNumber-startBlock) < len(hexTags) {
		publishedTag = hexTags[tagResp.BlockNumber-startBlock]
	}
	s.progress("payment", "tag", "success", "Publish tag",
		fmt.Sprintf("TagRegistry.publishTag() in block %d — 1 of %d window tags used", tagResp.BlockNumber, len(hexTags)))
	s.log("chain", fmt.Sprintf("TagRegistry.publishTag() → block %d  gas %s  tx %s",
		tagResp.BlockNumber, formatNum(tagResp.GasUsed), shortHex(tagResp.TxHash, 18)))
	s.log("tag", fmt.Sprintf("published tag %s (window covered blocks %d–%d)",
		shortHex(publishedTag, 18), startBlock, startBlock+uint64(len(hexTags))-1))

	// 5 — Update the local note set and re-read the tree.
	mtAfter, leavesAfter, err := buildVaultMerkleTree(ctx, s.client, s.vaultAddr)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	input.Spent = true
	if changeAmt.Sign() > 0 {
		s.addNote(&note{
			Owner:      sender,
			Amount:     new(big.Int).Set(changeAmt),
			TokenID:    new(big.Int).Set(tokenID),
			Salt:       result.SaltA,
			Commitment: changeCommitment,
			Origin:     "change",
			Discovered: true,
			Block:      payResp.BlockNumber,
		})
	}
	// The recipient's note is on-chain but not yet knowable to them: it stays
	// undiscovered until they scan and decrypt the tag.
	s.addNote(&note{
		Owner:      recipient,
		Amount:     new(big.Int).Set(payAmt),
		TokenID:    new(big.Int).Set(tokenID),
		Salt:       result.SaltB,
		Commitment: destCommitment,
		Origin:     "received",
		Discovered: false,
		Block:      payResp.BlockNumber,
	})

	s.paySeq++
	rec := &paymentRecord{
		Seq:            s.paySeq,
		Sender:         sender,
		Recipient:      recipient,
		Amount:         new(big.Int).Set(payAmt),
		Change:         new(big.Int).Set(changeAmt),
		TokenID:        new(big.Int).Set(tokenID),
		Nullifier:      nullifier,
		MerkleRootUsed: proof.Root,
		DestCommitment: destCommitment,
		ChangeCommit:   changeCommitment,
		CipherTextLen:  len(result.CipherText),
		EncTxDataLen:   len(result.EncTxData),
		PayTxHash:      payResp.TxHash,
		PayBlock:       payResp.BlockNumber,
		PayGas:         payResp.GasUsed,
		TagTxHash:      tagResp.TxHash,
		TagBlock:       tagResp.BlockNumber,
		TagGas:         tagResp.GasUsed,
		PublishedTag:   publishedTag,
		TagCtxtLen:     len(noteCtxt),
		ProofMs:        proofMs,
		RelayMs:        relayMs,
		RootAfter:      mtAfter.Root(),
		LeafCount:      len(leavesAfter),
		CreatedAt:      time.Now(),
	}
	s.payments = append(s.payments, rec)
	s.mu.Unlock()

	s.log("", fmt.Sprintf("Payment #%d complete: %s → %d tokens → %s. Switch to %s and scan to decrypt it.",
		rec.Seq, senderParty.Name, amount, recipientParty.Name, recipientParty.Name))
	return rec, nil
}

// ── Scanning ──────────────────────────────────────────────────────────────────

// scan runs the recipient-side discovery for the active actor against real
// on-chain data: every published channel is examined, the bitmap decides whether
// it is worth trying at all, and AEAD authentication decides whether the actor is
// the true recipient or one of the decoys.
func (s *session) scan() (*scanRecord, error) {
	if err := s.begin("scan"); err != nil {
		return nil, err
	}
	defer s.end()

	s.mu.Lock()
	actorIdx := s.actor
	s.mu.Unlock()
	if actorIdx < 0 {
		return nil, fmt.Errorf("choose which party you are acting as first")
	}
	if err := s.requireRegistered(); err != nil {
		return nil, err
	}
	actor, err := s.party(actorIdx)
	if err != nil {
		return nil, err
	}

	s.progress("scan", "channels", "running", "Scan channels",
		fmt.Sprintf("%s reads every published channel record and checks bit %d…", actor.Name, actorIdx))

	records, err := s.reader.allChannels()
	if err != nil {
		s.progress("scan", "channels", "error", "Scan channels", err.Error())
		return nil, err
	}

	tip, err := s.client.BlockNumber(context.Background())
	if err != nil {
		return nil, fmt.Errorf("BlockNumber: %w", err)
	}

	out := &scanRecord{Actor: actorIdx, At: time.Now().Format("15:04:05")}
	total := len(s.parties)
	matched, decrypted := 0, 0

	for _, rec := range records {
		res := channelScanResult{ChannelIdx: rec.Index, Bits: bitmapBits(rec.Bitmap, total)}

		if !bitmapHas(rec.Bitmap, actorIdx) {
			res.Outcome = "skipped"
			res.Detail = fmt.Sprintf("bit %d is 0 — %s is outside this channel's anonymity set and does no work", actorIdx, actor.Name)
			out.Channels = append(out.Channels, res)
			s.log("scan", fmt.Sprintf("channel #%d: bit %d clear → skipped", rec.Index, actorIdx))
			continue
		}

		ss, ok := tryChannelDecrypt(actor, rec)
		if !ok {
			res.Outcome = "decoy"
			res.Detail = "bit set, ML-KEM decapsulation ran, AEAD authentication failed — this channel is for someone else in the set"
			out.Channels = append(out.Channels, res)
			s.log("scan", fmt.Sprintf("channel #%d: decapsulated, AEAD tag mismatch → decoy candidate", rec.Index))
			continue
		}

		matched++
		res.Outcome = "matched"
		res.Detail = fmt.Sprintf("AEAD authenticated — channel secret recovered (0x%s…)", hex.EncodeToString(ss[:6]))
		s.log("scan", fmt.Sprintf("channel #%d: AEAD authenticated → %s is the true recipient", rec.Index, actor.Name))

		notes, err := s.scanTagsForChannel(actor, ss, tip)
		if err != nil {
			res.Detail += " · tag scan failed: " + err.Error()
		} else {
			res.Notes = notes
			for _, n := range notes {
				if n.Verified {
					decrypted++
				}
			}
		}
		out.Channels = append(out.Channels, res)
	}

	switch {
	case len(records) == 0:
		out.Summary = "No channels have been published yet."
	case matched == 0:
		out.Summary = fmt.Sprintf("%s examined %d channel(s) and matched none — either a bystander or a decoy in every set.", actor.Name, len(records))
	default:
		out.Summary = fmt.Sprintf("%s matched %d channel(s) and recovered %d payment note(s).", actor.Name, matched, decrypted)
	}

	s.mu.Lock()
	s.lastScan = out
	s.mu.Unlock()

	s.progress("scan", "channels", "success", "Scan channels", out.Summary)
	s.log("", out.Summary)
	return out, nil
}

// scanTagsForChannel finds every TagRegistry entry addressed to this party on
// this channel, decrypts the note, and checks the recomputed commitment against
// the one the payment transaction actually published.
func (s *session) scanTagsForChannel(actor *partyState, ss []byte, tip uint64) ([]scanNoteResult, error) {
	matches, _, err := tags.ScanBlocksFromCursor(
		s.client, s.tagRegistryAddr,
		[]tags.Channel{{SharedSecret: ss, PkSpend: actor.OnChainPkSpend}},
		tags.NewScanCursor(), tip,
	)
	if err != nil {
		return nil, fmt.Errorf("ScanBlocksFromCursor: %w", err)
	}

	var out []scanNoteResult
	for _, m := range matches {
		entry := scanNoteResult{
			Tag:   toHex(m.Entry.Tag[:]),
			Block: m.BlockNumber,
		}
		payload, err := tags.DecryptPaymentNote(ss, m.Entry.Ctxt)
		if err != nil {
			entry.Detail = "tag matched but the note payload failed to decrypt: " + err.Error()
			out = append(out, entry)
			continue
		}
		commitment, err := rpcore.Erc20CommitmentV2(actor.OnChainPkSpend, payload.Salt, payload.Amount, payload.TokenId)
		if err != nil {
			entry.Detail = "commitment recomputation failed: " + err.Error()
			out = append(out, entry)
			continue
		}

		entry.Amount = payload.Amount.String()
		entry.TokenID = payload.TokenId.String()
		entry.Salt = hexBig(payload.Salt)
		entry.Commitment = hexBig(commitment)

		s.mu.Lock()
		var target *note
		for _, n := range s.notes {
			if n.Commitment != nil && n.Commitment.Cmp(commitment) == 0 {
				target = n
				break
			}
		}
		if target != nil {
			entry.Verified = true
			entry.AlreadyKnown = target.Discovered
			if !target.Discovered {
				target.Discovered = true
				target.Salt = payload.Salt
			}
			entry.Detail = fmt.Sprintf("Poseidon4(pk_spend, salt, %s, %s) matches the destination commitment published on-chain — the note is now spendable",
				payload.Amount, payload.TokenId)
		} else {
			entry.Detail = "decrypted, but no matching on-chain commitment was found"
		}
		s.mu.Unlock()

		s.log("tag", fmt.Sprintf("%s decrypted a note: %s tokens, salt %s → commitment %s%s",
			actor.Name, payload.Amount, shortHex(hexBig(payload.Salt), 14), shortHex(hexBig(commitment), 14),
			map[bool]string{true: " ✓ verified", false: " (unmatched)"}[entry.Verified]))
		out = append(out, entry)
	}
	return out, nil
}

// tryChannelDecrypt reproduces the recipient side of the channel setup: ML-KEM
// decapsulation followed by an AEAD open with c1 as associated data. A failure
// here is the protocol's "not for me" signal, and is what a decoy candidate sees.
func tryChannelDecrypt(actor *partyState, rec *channelRecord) ([]byte, bool) {
	if actor.view == nil || len(rec.C1) == 0 {
		return nil, false
	}
	ss, err := actor.view.DecapsKey.Decapsulate(rec.C1)
	if err != nil {
		return nil, false
	}
	key := tags.DeriveChannelKey(ss)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(rec.C2) < gcm.NonceSize() {
		return nil, false
	}
	nonce, ct := rec.C2[:gcm.NonceSize()], rec.C2[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, rec.C1)
	if err != nil {
		return nil, false
	}
	// The payload is [4B msgLen][msg][4B senderIdLen][senderId]; a short read
	// means the AEAD passed on something this demo did not write.
	if len(plaintext) < 4 || uint32(len(plaintext)) < 4+binary.BigEndian.Uint32(plaintext[:4]) {
		return nil, false
	}
	return ss, true
}

// ── Dependency probes ─────────────────────────────────────────────────────────

func (s *session) requireChain() error {
	if _, err := s.client.BlockNumber(context.Background()); err != nil {
		return fmt.Errorf("the Hardhat node at %s is not responding: %w", rpcURL, err)
	}
	return nil
}

func (s *session) requireGnark() error {
	if !tcpAvailable("localhost:8082") {
		return fmt.Errorf("the gnark prover on :8082 is not reachable — check demo/logs/gnark.log")
	}
	return nil
}

func (s *session) requireRelayer() error {
	if !tcpAvailable("localhost:8090") {
		return fmt.Errorf("the relayer on :8090 is not reachable — check demo/logs/relayer.log")
	}
	s.mu.Lock()
	known := s.relayerAddr != ""
	s.mu.Unlock()
	if !known {
		s.syncRelayerInfo()
	}
	s.mu.Lock()
	warning := s.relayerWarning
	s.mu.Unlock()
	if warning != "" {
		return fmt.Errorf("%s", warning)
	}
	return nil
}

func (s *session) requireRegistered() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.registered {
		return fmt.Errorf("register the parties first")
	}
	return nil
}

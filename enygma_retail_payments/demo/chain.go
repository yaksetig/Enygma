// Chain and service plumbing for the live demo: locating the project root and
// its deployment receipts, loading ABIs, talking to the relayer, and reading the
// two registries the private-tag layer publishes to.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	rpcore "github.com/raylsnetwork/enygma_retail_payments/src/core"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ── Config ────────────────────────────────────────────────────────────────────

const (
	// listenHost binds to loopback only: /api/actor returns private keys, so the
	// server must never be reachable from another machine.
	listenHost    = "127.0.0.1"
	relayerURL    = "http://localhost:8090"
	gnarkURL      = "http://localhost:8082"
	relayerAPIKey = "test-api-key-dev-only"

	merkleDepth = 8
	// tagWindowSize covers this many consecutive blocks so the relayer can pick
	// the tag matching whichever block the transaction actually lands in.
	tagWindowSize = 3
)

var (
	listenPort = getEnv("DEMO_PORT", "9091")
	rpcURL     = getEnv("RPC_URL", "http://127.0.0.1:8545")
	chainID    = getEnvInt64("CHAIN_ID", 1337)
)

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// ── Project layout ────────────────────────────────────────────────────────────

// projectRoot is the enygma_retail_payments directory — the anchor for receipts,
// ABIs, and the contract artifacts the private_tags package expects to find by
// walking up from the working directory.
func projectRoot() (string, error) {
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(thisFile))
	}
	for _, start := range starts {
		dir := start
		for {
			if _, err := os.Stat(filepath.Join(dir, "enygmapayment.config.json")); err == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("could not locate enygma_retail_payments root (no enygmapayment.config.json found above %v)", starts)
}

type receiptEntry struct {
	ContractAddress string `json:"contractAddress"`
}

func loadReceipts(root string) (map[string]receiptEntry, error) {
	path := filepath.Join(root, "build", "receipts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (deploy the contracts first)", path, err)
	}
	var r map[string]receiptEntry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse receipts.json: %w", err)
	}
	return r, nil
}

func loadContractABI(path string) (abi.ABI, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("read ABI %s: %w", path, err)
	}
	var artifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		return abi.ABI{}, fmt.Errorf("parse artifact %s: %w", path, err)
	}
	return abi.JSON(strings.NewReader(string(artifact.ABI)))
}

// ── Auth / transactions ───────────────────────────────────────────────────────

func makeAuth(privHex string) (*bind.TransactOpts, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("HexToECDSA: %w", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(chainID))
	if err != nil {
		return nil, fmt.Errorf("NewKeyedTransactorWithChainID: %w", err)
	}
	auth.GasLimit = 8_000_000
	return auth, nil
}

func waitMined(ctx context.Context, client *ethclient.Client, tx *ethtypes.Transaction) (*ethtypes.Receipt, error) {
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		return nil, err
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("transaction reverted in block %d (tx %s)", receipt.BlockNumber, receipt.TxHash.Hex())
	}
	return receipt, nil
}

// ── Service probes ────────────────────────────────────────────────────────────

func tcpAvailable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ── Relayer client ────────────────────────────────────────────────────────────

type relayInfoResponse struct {
	RelayerAddr            string `json:"relayerAddr"`
	TagRegistryAddr        string `json:"tagRegistryAddr"`
	TagChannelRegistryAddr string `json:"tagChannelRegistryAddr"`
	ChainID                int64  `json:"chainId"`
}

type relayPaymentRequest struct {
	VaultId      string    `json:"vaultId"`
	Proof        [8]string `json:"proof"`
	PublicSignal [7]string `json:"publicSignal"`
	CipherText   string    `json:"cipherText"`
	EncTxData    string    `json:"encTxData"`
}

type relayPaymentResponse struct {
	TxHash      string `json:"txHash"`
	BlockNumber uint64 `json:"blockNumber"`
	GasUsed     uint64 `json:"gasUsed"`
	Error       string `json:"error,omitempty"`
}

type relayChannelRequest struct {
	C1     string `json:"c1"`
	C2     string `json:"c2"`
	Bitmap string `json:"bitmap"`
}

type relayChannelResponse struct {
	ChannelIdx  uint64 `json:"channelIdx"`
	TxHash      string `json:"txHash"`
	BlockNumber uint64 `json:"blockNumber"`
	GasUsed     uint64 `json:"gasUsed"`
	Error       string `json:"error,omitempty"`
}

type relayTagRequest struct {
	Tags       []string `json:"tags,omitempty"`
	StartBlock uint64   `json:"startBlock,omitempty"`
	Ctxt       string   `json:"ctxt"`
}

type relayTagResponse struct {
	TxHash      string `json:"txHash"`
	BlockNumber uint64 `json:"blockNumber"`
	GasUsed     uint64 `json:"gasUsed"`
	Error       string `json:"error,omitempty"`
}

var relayerHTTP = &http.Client{Timeout: 90 * time.Second}

// postRelayer sends an authenticated POST to the relayer and decodes the reply.
// A non-200 status is returned as an error carrying the relayer's own message so
// the UI can show something actionable rather than a bare status code.
func postRelayer(path string, body, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, relayerURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+relayerAPIKey)

	resp, err := relayerHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w (is the relayer running on :8090?)", relayerURL+path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if result != nil {
		_ = json.Unmarshal(raw, result)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relayer %s returned %d: %s", path, resp.StatusCode, relayerErrText(raw))
	}
	return nil
}

// relayerErrText pulls the "error" field out of a relayer reply, falling back to
// a trimmed copy of the raw body.
func relayerErrText(raw []byte) string {
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) == nil {
		if e, ok := m["error"].(string); ok && e != "" {
			return e
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	if s == "" {
		return "no response body"
	}
	return s
}

func fetchRelayInfo() (*relayInfoResponse, error) {
	resp, err := relayerHTTP.Get(relayerURL + "/relay/info")
	if err != nil {
		return nil, fmt.Errorf("GET /relay/info: %w", err)
	}
	defer resp.Body.Close()
	var info relayInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode /relay/info: %w", err)
	}
	return &info, nil
}

func buildRelayPaymentRequest(result *dvpPaymentResult) relayPaymentRequest {
	var proof [8]string
	copy(proof[:], result.Proof)
	var sig [7]string
	for i, v := range result.ContractStatement() {
		if i < len(sig) {
			sig[i] = v.String()
		}
	}
	return relayPaymentRequest{
		VaultId:      "0",
		Proof:        proof,
		PublicSignal: sig,
		CipherText:   toHex(result.CipherText),
		EncTxData:    toHex(result.EncTxData),
	}
}

// dvpPaymentResult aliases the prover result so this file does not need the
// dvp core import purely for a type name.
type dvpPaymentResult = rpcore.PaymentResult

// ── Vault Merkle tree ─────────────────────────────────────────────────────────

var commitmentEventSig = crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))

// vaultCommitments returns every commitment the vault has emitted, in insertion
// order. The tree is rebuilt from this on demand — the chain, not the demo's own
// bookkeeping, is the source of truth for leaf positions and therefore for the
// nullifier each proof commits to.
func vaultCommitments(ctx context.Context, client *ethclient.Client, vaultAddr common.Address) ([]*big.Int, error) {
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
		Addresses: []common.Address{vaultAddr},
		Topics:    [][]common.Hash{{commitmentEventSig}},
	})
	if err != nil {
		return nil, fmt.Errorf("FilterLogs (vault Commitment): %w", err)
	}
	out := make([]*big.Int, 0, len(logs))
	for _, l := range logs {
		if len(l.Topics) >= 3 {
			out = append(out, l.Topics[2].Big())
		}
	}
	return out, nil
}

func buildVaultMerkleTree(ctx context.Context, client *ethclient.Client, vaultAddr common.Address) (*rpcore.MerkleTree, []*big.Int, error) {
	leaves, err := vaultCommitments(ctx, client, vaultAddr)
	if err != nil {
		return nil, nil, err
	}
	mt := rpcore.NewMerkleTree(merkleDepth)
	for _, leaf := range leaves {
		mt.InsertLeaf(leaf)
	}
	return mt, leaves, nil
}

// ── TagChannelRegistry reads ──────────────────────────────────────────────────

// channelRecord is one published (c1, c2, bitmap) tuple, read back from chain so
// the scan step works against what an outside observer would actually see.
type channelRecord struct {
	Index  uint64
	Sender common.Address
	C1     []byte
	C2     []byte
	Bitmap []byte
}

type chainReader struct {
	client      *ethclient.Client
	channelABI  abi.ABI
	channelAddr common.Address
}

func (r *chainReader) channelCount() (uint64, error) {
	c := bind.NewBoundContract(r.channelAddr, r.channelABI, r.client, r.client, r.client)
	var out []interface{}
	if err := c.Call(&bind.CallOpts{}, &out, "getChannelCount"); err != nil {
		return 0, fmt.Errorf("TagChannelRegistry.getChannelCount: %w", err)
	}
	n, ok := out[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("getChannelCount: unexpected type %T", out[0])
	}
	return n.Uint64(), nil
}

func (r *chainReader) channel(idx uint64) (*channelRecord, error) {
	c := bind.NewBoundContract(r.channelAddr, r.channelABI, r.client, r.client, r.client)
	var out []interface{}
	if err := c.Call(&bind.CallOpts{}, &out, "getChannel", new(big.Int).SetUint64(idx)); err != nil {
		return nil, fmt.Errorf("TagChannelRegistry.getChannel(%d): %w", idx, err)
	}
	if len(out) < 4 {
		return nil, fmt.Errorf("getChannel(%d): expected 4 return values, got %d", idx, len(out))
	}
	sender, _ := out[0].(common.Address)
	c1, _ := out[1].([]byte)
	c2, _ := out[2].([]byte)
	bitmap, _ := out[3].([]byte)
	return &channelRecord{Index: idx, Sender: sender, C1: c1, C2: c2, Bitmap: bitmap}, nil
}

// allChannels reads every published channel record.
func (r *chainReader) allChannels() ([]*channelRecord, error) {
	total, err := r.channelCount()
	if err != nil {
		return nil, err
	}
	out := make([]*channelRecord, 0, total)
	for i := uint64(0); i < total; i++ {
		rec, err := r.channel(i)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ── Bitmap helpers ────────────────────────────────────────────────────────────

// bitmapHas reports whether the bit for registry index idx is set. Bits are
// packed little-endian within each byte, matching tags.buildBitmap.
func bitmapHas(bitmap []byte, idx int) bool {
	if idx < 0 || idx/8 >= len(bitmap) {
		return false
	}
	return bitmap[idx/8]&(1<<(uint(idx)%8)) != 0
}

// bitmapIndices lists every set index in a bitmap, bounded by totalUsers.
func bitmapIndices(bitmap []byte, totalUsers int) []int {
	out := []int{}
	for i := 0; i < totalUsers; i++ {
		if bitmapHas(bitmap, i) {
			out = append(out, i)
		}
	}
	return out
}

// bitmapBits renders a bitmap as a "10110000"-style string, index 0 leftmost,
// so the UI can show the raw published value next to the aligned rows.
func bitmapBits(bitmap []byte, totalUsers int) string {
	var sb strings.Builder
	for i := 0; i < totalUsers; i++ {
		if bitmapHas(bitmap, i) {
			sb.WriteByte('1')
		} else {
			sb.WriteByte('0')
		}
	}
	return sb.String()
}

// ── Formatting ────────────────────────────────────────────────────────────────

func toHex(b []byte) string { return "0x" + hex.EncodeToString(b) }

// hexBig renders a field element as fixed-width 0x hex.
func hexBig(n *big.Int) string {
	if n == nil {
		return ""
	}
	return "0x" + n.Text(16)
}

// fingerprint condenses a long public key into a short, comparable identifier.
// Used for pk_view, which is 1184 bytes and unreadable in full.
func fingerprint(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	h := crypto.Keccak256(b)
	return "0x" + hex.EncodeToString(h[:6])
}

func shortHex(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatNum(n uint64) string {
	s := strconv.FormatUint(n, 10)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

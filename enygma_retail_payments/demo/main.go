// Enygma Retail Payments · Live Demo
//
// A loopback-only HTTP server that drives the real retail-payment stack —
// Hardhat contracts, the gnark Groth16 prover, the relayer, ML-KEM channel
// setup, the tag registry, and the vault's commitment tree — from a guided,
// presentation-first UI.
//
// The demo is a persistent session rather than a one-shot script: you register
// eight parties, act as Alice, Bob or Charlie, open a directional channel under
// one of four privacy modes, send repeatable payments, then switch parties to
// scan, decrypt and verify what you received. A clean chain is a launcher
// restart.
//
// Usage:
//
//	cd demo && bash run.sh          # starts every dependency, then this server
//	open http://localhost:9091
//
// SECURITY: /api/actor returns the acting party's private keys so the demo can
// show them on screen. The listener is bound to 127.0.0.1 for that reason, and
// every key in play is derived from the repo's public Hardhat mnemonic. Never
// point this at a network holding value.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	tags "github.com/raylsnetwork/enygma_retail_payments/private_tags/src"
)

//go:embed index.html
var indexHTML string

// ── SSE broker ────────────────────────────────────────────────────────────────

// Event is one server-sent message. Only public data ever travels here: private
// keys reach the browser exclusively through the authenticated-by-loopback
// /api/actor response, on explicit user action.
type Event struct {
	Type   string `json:"type"`
	Action string `json:"action,omitempty"`
	Step   string `json:"step,omitempty"`
	Status string `json:"status,omitempty"`
	Label  string `json:"label,omitempty"`
	Msg    string `json:"msg,omitempty"`
	Cat    string `json:"cat,omitempty"`
	TS     string `json:"ts,omitempty"`
	Index  int    `json:"index"`
	Phase  string `json:"phase,omitempty"`
	Value  string `json:"value,omitempty"`
}

// sseClient is one connected /events stream.
type sseClient struct {
	ch   chan string
	done chan struct{}
}

// maxSSEClients bounds how many event streams the demo keeps open at once.
//
// A browser allows six concurrent HTTP/1.1 connections per origin, and an SSE
// stream holds one for as long as the server believes it is alive. A reloaded
// page can leave its old stream behind — the socket is not always torn down
// promptly — so without a cap, a handful of reloads consumes the whole budget
// and every subsequent API call queues forever with no socket to use. Retiring
// the oldest stream keeps sockets free for the actions the operator is taking.
const maxSSEClients = 3

type Broker struct {
	mu      sync.Mutex
	clients []*sseClient // oldest first
}

func newBroker() *Broker { return &Broker{} }

func (b *Broker) subscribe() *sseClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.clients) >= maxSSEClients {
		close(b.clients[0].done)
		b.clients = b.clients[1:]
	}
	c := &sseClient{ch: make(chan string, 512), done: make(chan struct{})}
	b.clients = append(b.clients, c)
	return c
}

func (b *Broker) unsubscribe(c *sseClient) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, existing := range b.clients {
		if existing == c {
			b.clients = append(b.clients[:i], b.clients[i+1:]...)
			return
		}
	}
}

func (b *Broker) publish(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	line := "data: " + string(data) + "\n\n"
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.clients {
		select {
		case c.ch <- line:
		default: // a stalled browser must not block the protocol run
		}
	}
}

// ── Public state DTOs ─────────────────────────────────────────────────────────

type partyDTO struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Initial      string `json:"initial"`
	Address      string `json:"address"`
	Account      int    `json:"account"`
	Controllable bool   `json:"controllable"`
	Registered   bool   `json:"registered"`
	PkSpend      string `json:"pkSpend"`
	PkViewFp     string `json:"pkViewFp"`
	PkViewLen    int    `json:"pkViewLen"`
	RegTxHash    string `json:"regTxHash"`
	RegBlock     uint64 `json:"regBlock"`
	RegGas       uint64 `json:"regGas"`
	Balance      int64  `json:"balance"`
	Pending      int64  `json:"pending"`
	NoteCount    int    `json:"noteCount"`
}

type channelDTO struct {
	Sender        int    `json:"sender"`
	Recipient     int    `json:"recipient"`
	Mode          string `json:"mode"`
	ModeLabel     string `json:"modeLabel"`
	Excluded      []int  `json:"excluded"`
	Candidates    []int  `json:"candidates"`
	Bits          string `json:"bits"`
	BitmapHex     string `json:"bitmapHex"`
	OnChainBits   string `json:"onChainBits"`
	BitmapMatches bool   `json:"bitmapMatches"`
	ChannelIdx    uint64 `json:"channelIdx"`
	TxHash        string `json:"txHash"`
	Block         uint64 `json:"block"`
	Gas           uint64 `json:"gas"`
	Description   string `json:"description"`
}

type previewDTO struct {
	ID          string `json:"id"`
	Sender      int    `json:"sender"`
	Recipient   int    `json:"recipient"`
	Mode        string `json:"mode"`
	Excluded    []int  `json:"excluded"`
	Candidates  []int  `json:"candidates"`
	Bits        string `json:"bits"`
	BitmapHex   string `json:"bitmapHex"`
	C1Len       int    `json:"c1Len"`
	C2Len       int    `json:"c2Len"`
	Description string `json:"description"`
}

type paymentDTO struct {
	Seq            int    `json:"seq"`
	Sender         int    `json:"sender"`
	Recipient      int    `json:"recipient"`
	Amount         string `json:"amount"`
	Change         string `json:"change"`
	Nullifier      string `json:"nullifier"`
	RootUsed       string `json:"rootUsed"`
	DestCommitment string `json:"destCommitment"`
	ChangeCommit   string `json:"changeCommitment"`
	CipherTextLen  int    `json:"cipherTextLen"`
	EncTxDataLen   int    `json:"encTxDataLen"`
	PayTxHash      string `json:"payTxHash"`
	PayBlock       uint64 `json:"payBlock"`
	PayGas         uint64 `json:"payGas"`
	TagTxHash      string `json:"tagTxHash"`
	TagBlock       uint64 `json:"tagBlock"`
	TagGas         uint64 `json:"tagGas"`
	PublishedTag   string `json:"publishedTag"`
	TagCtxtLen     int    `json:"tagCtxtLen"`
	ProofMs        int64  `json:"proofMs"`
	RelayMs        int64  `json:"relayMs"`
	RootAfter      string `json:"rootAfter"`
	LeafCount      int    `json:"leafCount"`
	At             string `json:"at"`
}

type noteDTO struct {
	ID         string `json:"id"`
	Amount     string `json:"amount"`
	Origin     string `json:"origin"`
	Commitment string `json:"commitment"`
	Spent      bool   `json:"spent"`
	Discovered bool   `json:"discovered"`
	Block      uint64 `json:"block"`
}

type stateDTO struct {
	Ready        bool          `json:"ready"`
	InitError    string        `json:"initError"`
	Busy         string        `json:"busy"`
	Registered   bool          `json:"registered"`
	Actor        int           `json:"actor"`
	ActorSecrets *actorSecrets `json:"actorSecrets"`
	Parties      []partyDTO    `json:"parties"`
	Channels     []channelDTO  `json:"channels"`
	Preview      *previewDTO   `json:"preview"`
	Payments     []paymentDTO  `json:"payments"`
	Notes        []noteDTO     `json:"notes"`
	LastScan     *scanRecord   `json:"lastScan"`
	Tree         treeDTO       `json:"tree"`
	Health       healthDTO     `json:"health"`
	Contracts    contractsDTO  `json:"contracts"`
	Config       configDTO     `json:"config"`
}

type treeDTO struct {
	Depth     int      `json:"depth"`
	LeafCount int      `json:"leafCount"`
	Root      string   `json:"root"`
	Leaves    []string `json:"leaves"`
}

type healthDTO struct {
	Chain   bool   `json:"chain"`
	Gnark   bool   `json:"gnark"`
	Relayer bool   `json:"relayer"`
	Warning string `json:"warning,omitempty"`
}

type contractsDTO struct {
	Vault              string `json:"vault"`
	UserRegistry       string `json:"userRegistry"`
	EnygmaDvp          string `json:"enygmaDvp"`
	TagRegistry        string `json:"tagRegistry"`
	TagChannelRegistry string `json:"tagChannelRegistry"`
	Relayer            string `json:"relayer"`
	ChainID            int64  `json:"chainId"`
}

type configDTO struct {
	TokenID        int64 `json:"tokenId"`
	SeedAmount     int64 `json:"seedAmount"`
	DefaultPayment int64 `json:"defaultPayment"`
	MerkleDepth    int   `json:"merkleDepth"`
	PartyCount     int   `json:"partyCount"`
}

// ── State assembly ────────────────────────────────────────────────────────────

func modeLabel(mode string) string {
	switch mode {
	case "none":
		return "None · recipient only"
	case "subset":
		return "Subset · recipient + ⌊√N⌋ decoys"
	case "rift":
		return "Rift · everyone except exclusions"
	case "full":
		return "Full · all registered users"
	}
	return mode
}

func (s *session) snapshot() stateDTO {
	tree := s.treeSnapshot()

	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.parties)
	st := stateDTO{
		Ready:      true,
		Busy:       s.busy,
		Registered: s.registered,
		Actor:      s.actor,
		Config: configDTO{
			TokenID:        demoTokenID,
			SeedAmount:     seedAmount,
			DefaultPayment: defaultPayment,
			MerkleDepth:    merkleDepth,
			PartyCount:     total,
		},
		Contracts: contractsDTO{
			Vault:              s.vaultAddr.Hex(),
			UserRegistry:       s.registryAddr.Hex(),
			EnygmaDvp:          s.dvpAddr.Hex(),
			TagRegistry:        s.tagRegistryAddr.Hex(),
			TagChannelRegistry: s.channelRegistryAddr.Hex(),
			Relayer:            s.relayerAddr,
			ChainID:            chainID,
		},
		Parties:  []partyDTO{},
		Channels: []channelDTO{},
		Payments: []paymentDTO{},
		Notes:    []noteDTO{},
		LastScan: s.lastScan,
	}

	for _, p := range s.parties {
		dto := partyDTO{
			Index:        p.Index,
			Name:         p.Name,
			Initial:      string([]rune(p.Name)[:1]),
			Address:      p.Address.Hex(),
			Account:      p.Account,
			Controllable: p.Controllable,
			Registered:   p.Registered,
			RegTxHash:    p.RegTxHash,
			RegBlock:     p.RegBlock,
			RegGas:       p.RegGas,
		}
		if p.Registered {
			dto.PkSpend = hexBig(p.OnChainPkSpend)
			dto.PkViewFp = fingerprint(p.OnChainPkView)
			dto.PkViewLen = len(p.OnChainPkView)
		}
		if p.Controllable {
			dto.Balance = s.balanceOf(p.Index)
			dto.Pending = s.pendingOf(p.Index)
			for _, n := range s.notes {
				if n.Owner == p.Index && !n.Spent && n.Discovered {
					dto.NoteCount++
				}
			}
		}
		st.Parties = append(st.Parties, dto)
	}

	for _, ch := range s.channels {
		onChain := bitmapBits(ch.OnChainBitmap, total)
		local := bitmapBits(ch.Bitmap, total)
		st.Channels = append(st.Channels, channelDTO{
			Sender:        ch.Sender,
			Recipient:     ch.Recipient,
			Mode:          ch.Mode,
			ModeLabel:     modeLabel(ch.Mode),
			Excluded:      ch.Excluded,
			Candidates:    ch.Candidates,
			Bits:          local,
			BitmapHex:     toHex(ch.Bitmap),
			OnChainBits:   onChain,
			BitmapMatches: onChain == local,
			ChannelIdx:    ch.ChannelIdx,
			TxHash:        ch.TxHash,
			Block:         ch.Block,
			Gas:           ch.Gas,
			Description:   tags.BitmapDescription(ch.ModeCode),
		})
	}
	sortChannels(st.Channels)

	if p := s.preview; p != nil {
		st.Preview = &previewDTO{
			ID:          p.ID,
			Sender:      p.Sender,
			Recipient:   p.Recipient,
			Mode:        p.Mode,
			Excluded:    p.Excluded,
			Candidates:  p.Candidates,
			Bits:        bitmapBits(p.Bitmap, total),
			BitmapHex:   toHex(p.Bitmap),
			C1Len:       len(p.c1),
			C2Len:       len(p.c2),
			Description: tags.BitmapDescription(p.ModeCode),
		}
	}

	for _, r := range s.payments {
		st.Payments = append(st.Payments, paymentDTO{
			Seq:            r.Seq,
			Sender:         r.Sender,
			Recipient:      r.Recipient,
			Amount:         r.Amount.String(),
			Change:         r.Change.String(),
			Nullifier:      hexBig(r.Nullifier),
			RootUsed:       hexBig(r.MerkleRootUsed),
			DestCommitment: hexBig(r.DestCommitment),
			ChangeCommit:   hexBig(r.ChangeCommit),
			CipherTextLen:  r.CipherTextLen,
			EncTxDataLen:   r.EncTxDataLen,
			PayTxHash:      r.PayTxHash,
			PayBlock:       r.PayBlock,
			PayGas:         r.PayGas,
			TagTxHash:      r.TagTxHash,
			TagBlock:       r.TagBlock,
			TagGas:         r.TagGas,
			PublishedTag:   r.PublishedTag,
			TagCtxtLen:     r.TagCtxtLen,
			ProofMs:        r.ProofMs,
			RelayMs:        r.RelayMs,
			RootAfter:      hexBig(r.RootAfter),
			LeafCount:      r.LeafCount,
			At:             r.CreatedAt.Format("15:04:05"),
		})
	}

	// Notes and secrets are scoped to the acting party.
	if s.actor >= 0 && s.actor < len(s.parties) {
		p := s.parties[s.actor]
		st.ActorSecrets = s.secretsFor(p)
		for _, n := range s.notes {
			if n.Owner != s.actor {
				continue
			}
			st.Notes = append(st.Notes, noteDTO{
				ID:         n.ID,
				Amount:     n.Amount.String(),
				Origin:     n.Origin,
				Commitment: hexBig(n.Commitment),
				Spent:      n.Spent,
				Discovered: n.Discovered,
				Block:      n.Block,
			})
		}
	}

	st.Tree = tree
	st.Health = healthDTO{
		Chain:   tcpAvailable("127.0.0.1:8545"),
		Gnark:   tcpAvailable("localhost:8082"),
		Relayer: tcpAvailable("localhost:8090"),
		Warning: s.relayerWarning,
	}
	return st
}

// treeSnapshot re-reads the vault's commitment leaves. The tree is small in this
// demo, so reading it fresh keeps the panel honest without any cache to stale.
func (s *session) treeSnapshot() treeDTO {
	t := treeDTO{Depth: merkleDepth, Leaves: []string{}}
	mt, leaves, err := buildVaultMerkleTree(contextTODO(), s.client, s.vaultAddr)
	if err != nil {
		return t
	}
	t.LeafCount = len(leaves)
	if len(leaves) > 0 {
		t.Root = hexBig(mt.Root())
	}
	for _, l := range leaves {
		t.Leaves = append(t.Leaves, hexBig(l))
	}
	return t
}

func sortChannels(ch []channelDTO) {
	sort.Slice(ch, func(i, j int) bool {
		if ch[i].Sender != ch[j].Sender {
			return ch[i].Sender < ch[j].Sender
		}
		return ch[i].Recipient < ch[j].Recipient
	})
}

// ── HTTP server ───────────────────────────────────────────────────────────────

type server struct {
	broker  *Broker
	sess    *session
	initErr string
}

func (srv *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

func (srv *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	c := srv.broker.subscribe()
	defer srv.broker.unsubscribe(c)

	fmt.Fprint(w, "retry: 1500\n\n")
	fl.Flush()

	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.done: // retired to free a connection for a newer page
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case msg := <-c.ch:
			fmt.Fprint(w, msg)
			fl.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// decodeBody reads a JSON request body, tolerating an empty one.
func decodeBody(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil && err.Error() != "EOF" {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func (srv *server) requireSession(w http.ResponseWriter) bool {
	if srv.sess == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("demo not initialised: %s", srv.initErr))
		return false
	}
	return true
}

func (srv *server) handleState(w http.ResponseWriter, r *http.Request) {
	if srv.sess == nil {
		writeJSON(w, http.StatusOK, stateDTO{Ready: false, InitError: srv.initErr, Actor: -1})
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

func (srv *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	if err := srv.sess.registerAll(); err != nil {
		srv.broker.publish(Event{Type: "error", Action: "register", Msg: err.Error()})
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

func (srv *server) handleActor(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	var body struct {
		Index int `json:"index"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	secrets, err := srv.sess.setActor(body.Index)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	srv.sess.log("", fmt.Sprintf("Acting as %s — this is a local demo identity switch, not authentication", secrets.Name))
	srv.sess.pushState()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secrets": secrets,
		"state":   srv.sess.snapshot(),
	})
}

func (srv *server) handleChannelPreview(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	var body struct {
		Recipient int    `json:"recipient"`
		Mode      string `json:"mode"`
		Excluded  []int  `json:"excluded"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := srv.sess.previewChannel(body.Recipient, body.Mode, body.Excluded); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

func (srv *server) handleChannelOpen(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	var body struct {
		PreviewID string `json:"previewId"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := srv.sess.openChannelFromPreview(body.PreviewID); err != nil {
		srv.broker.publish(Event{Type: "error", Action: "channel", Msg: err.Error()})
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

func (srv *server) handlePayment(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	var body struct {
		Recipient int   `json:"recipient"`
		Amount    int64 `json:"amount"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := srv.sess.sendPayment(body.Recipient, body.Amount); err != nil {
		srv.broker.publish(Event{Type: "error", Action: "payment", Msg: err.Error()})
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

func (srv *server) handleScan(w http.ResponseWriter, r *http.Request) {
	if !srv.requireSession(w) {
		return
	}
	if _, err := srv.sess.scan(); err != nil {
		srv.broker.publish(Event{Type: "error", Action: "scan", Msg: err.Error()})
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, srv.sess.snapshot())
}

// postOnly rejects anything but POST so a stray browser navigation cannot fire
// an action that spends gas.
func postOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("POST only"))
			return
		}
		h(w, r)
	}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	broker := newBroker()
	srv := &server{broker: broker}

	sess, err := newSession(broker)
	if err != nil {
		srv.initErr = err.Error()
		log.Printf("demo session unavailable: %v", err)
	} else {
		srv.sess = sess
		sess.syncRelayerInfo()
		if sess.relayerWarning != "" {
			log.Printf("warning: %s", sess.relayerWarning)
		}
		log.Printf("vault=%s registry=%s tagRegistry=%s tagChannelRegistry=%s",
			sess.vaultAddr.Hex(), sess.registryAddr.Hex(), sess.tagRegistryAddr.Hex(), sess.channelRegistryAddr.Hex())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/events", srv.handleEvents)
	mux.HandleFunc("/api/state", srv.handleState)
	mux.HandleFunc("/api/register", postOnly(srv.handleRegister))
	mux.HandleFunc("/api/actor", postOnly(srv.handleActor))
	mux.HandleFunc("/api/channels/preview", postOnly(srv.handleChannelPreview))
	mux.HandleFunc("/api/channels/open", postOnly(srv.handleChannelOpen))
	mux.HandleFunc("/api/payments", postOnly(srv.handlePayment))
	mux.HandleFunc("/api/scan", postOnly(srv.handleScan))

	addr := listenHost + ":" + listenPort
	log.Printf("Enygma Retail Payments demo listening on http://%s (loopback only — it serves demo private keys)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// syncRelayerInfo reads the relayer's advertised address and registries, and
// records a warning when it was pointed at different contracts than the ones the
// demo deployed against — that mismatch silently breaks channel setup and tag
// publication, so it belongs in front of the user rather than in a log file.
func (s *session) syncRelayerInfo() {
	info, err := fetchRelayInfo()
	if err != nil {
		s.mu.Lock()
		s.relayerWarning = "the relayer on :8090 is not reachable — check demo/logs/relayer.log"
		s.mu.Unlock()
		return
	}
	warning := ""
	if err := s.checkRelayerRegistries(info); err != nil {
		warning = err.Error()
	}
	s.mu.Lock()
	s.relayerAddr = info.RelayerAddr
	s.relayerWarning = warning
	s.mu.Unlock()
}

// checkRelayerRegistries compares the relayer's configured tag registries with
// the ones in build/receipts.json.
func (s *session) checkRelayerRegistries(info *relayInfoResponse) error {
	want := map[string]string{
		"TagRegistry":        s.tagRegistryAddr.Hex(),
		"TagChannelRegistry": s.channelRegistryAddr.Hex(),
	}
	got := map[string]string{
		"TagRegistry":        info.TagRegistryAddr,
		"TagChannelRegistry": info.TagChannelRegistryAddr,
	}
	for name, w := range want {
		if !strings.EqualFold(got[name], w) {
			return fmt.Errorf("relayer is configured with %s=%s but the demo deployed %s — restart the relayer with the addresses from build/receipts.json",
				name, got[name], w)
		}
	}
	return nil
}

// contextTODO keeps chain reads in the snapshot path short and cancel-free; the
// calls are local RPC and complete in milliseconds.
func contextTODO() context.Context { return context.Background() }

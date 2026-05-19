package utils

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	poseidonLib "github.com/iden3/go-iden3-crypto/poseidon"
	"golang.org/x/crypto/sha3"
)

const (
	defaultMerkleRPCURL = "http://127.0.0.1:8545"
	rpcAllowlistEnv     = "ENYGMA_RPC_ALLOWLIST"
	fileRootsEnv        = "ENYGMA_ALLOWED_FILE_ROOTS"
)

var defaultAllowedRPCOrigins = []string{
	"http://127.0.0.1:8545",
	"http://localhost:8545",
	"http://[::1]:8545",
}

// ─── Local Merkle Tree ────────────────────────────────────────────────────────
// Mirrors src/core/merkle.go so we can reconstruct the tree from on-chain events.

var merkleSnarkField, _ = new(big.Int).SetString(
	"21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)

type localMerkleTree struct {
	depth int
	zeros []*big.Int
	tree  [][]*big.Int
}

func localPoseidon2(left, right *big.Int) *big.Int {
	h, err := poseidonLib.Hash([]*big.Int{left, right})
	if err != nil {
		panic(fmt.Sprintf("poseidon2: %v", err))
	}
	return h
}

func localZeroValue() *big.Int {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte("ZkDvp"))
	b := h.Sum(nil)
	v := new(big.Int).SetBytes(b)
	v.Mod(v, merkleSnarkField)
	return v
}

func buildZeroLevels(depth int) []*big.Int {
	z := make([]*big.Int, depth)
	z[0] = localZeroValue()
	for i := 1; i < depth; i++ {
		z[i] = localPoseidon2(z[i-1], z[i-1])
	}
	return z
}

func newLocalMerkleTree(depth int) *localMerkleTree {
	mt := &localMerkleTree{depth: depth}
	mt.zeros = buildZeroLevels(depth)
	mt.tree = make([][]*big.Int, depth+1)
	for i := 0; i <= depth; i++ {
		mt.tree[i] = make([]*big.Int, 0)
	}
	mt.tree[depth] = []*big.Int{localPoseidon2(mt.zeros[depth-1], mt.zeros[depth-1])}
	return mt
}

func (mt *localMerkleTree) resetToEmpty() {
	mt.tree = make([][]*big.Int, mt.depth+1)
	for i := 0; i <= mt.depth; i++ {
		mt.tree[i] = make([]*big.Int, 0)
	}
	mt.tree[mt.depth] = []*big.Int{localPoseidon2(mt.zeros[mt.depth-1], mt.zeros[mt.depth-1])}
}

func (mt *localMerkleTree) insertLeaf(leaf *big.Int) {
	maxLeaves := 1 << mt.depth
	if len(mt.tree[0])+1 >= maxLeaves {
		mt.resetToEmpty()
	}
	mt.tree[0] = append(mt.tree[0], leaf)
	mt.rebuildSparse()
}

func (mt *localMerkleTree) rebuildSparse() {
	for level := 0; level < mt.depth; level++ {
		mt.tree[level+1] = mt.tree[level+1][:0]
		for pos := 0; pos < len(mt.tree[level]); pos += 2 {
			right := mt.zeros[level]
			if pos+1 < len(mt.tree[level]) {
				right = mt.tree[level][pos+1]
			}
			mt.tree[level+1] = append(mt.tree[level+1],
				localPoseidon2(mt.tree[level][pos], right))
		}
	}
}

func (mt *localMerkleTree) root() string {
	return mt.tree[mt.depth][0].String()
}

// TreeOutput is the serialisable snapshot of the Merkle tree.
// Levels[0] = leaves, Levels[depth] = [root].
// Each level only contains the explicitly computed nodes; implied zero-filled
// siblings are omitted (use Zeros[level] for the zero value at that level).
type TreeOutput struct {
	Depth  int        `json:"depth"`
	Root   string     `json:"root"`
	Levels [][]string `json:"levels"`
	Zeros  []string   `json:"zeros"`
}

func (mt *localMerkleTree) snapshot() TreeOutput {
	levels := make([][]string, mt.depth+1)
	for i, nodes := range mt.tree {
		levels[i] = make([]string, len(nodes))
		for j, v := range nodes {
			levels[i][j] = v.String()
		}
	}
	zeros := make([]string, mt.depth)
	for i, z := range mt.zeros {
		zeros[i] = z.String()
	}
	return TreeOutput{
		Depth:  mt.depth,
		Root:   mt.root(),
		Levels: levels,
		Zeros:  zeros,
	}
}

// ─── JSON-RPC helpers ─────────────────────────────────────────────────────────

// keccak4 returns the first 4 bytes of the keccak256 hash of sig as a hex string
// (no 0x prefix), suitable for use as an ABI function selector.
func keccak4(sig string) string {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	return hex.EncodeToString(h.Sum(nil)[:4])
}

// keccak32Hex returns the full 32-byte keccak256 hash of sig as a 0x-prefixed hex string.
func keccak32Hex(sig string) string {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	return "0x" + hex.EncodeToString(h.Sum(nil))
}

type jsonRPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type jsonRPCResponse struct {
	Result interface{}   `json:"result"`
	Error  *jsonRPCError `json:"error"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func doRPC(rpcURL string, req jsonRPCRequest) (jsonRPCResponse, error) {
	if err := validateRPCURL(rpcURL); err != nil {
		return jsonRPCResponse{}, err
	}
	body, _ := json.Marshal(req)
	request, err := http.NewRequest("POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return jsonRPCResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return jsonRPCResponse{}, err
	}
	defer resp.Body.Close()
	var result jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return jsonRPCResponse{}, err
	}
	if result.Error != nil {
		return jsonRPCResponse{}, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}
	return result, nil
}

// ethCallUint256 calls a no-argument view function and returns the result as *big.Int.
func ethCallUint256(rpcURL, contractAddr, selectorHex string) (*big.Int, error) {
	resp, err := doRPC(rpcURL, jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_call",
		Params: []interface{}{
			map[string]string{"to": contractAddr, "data": "0x" + selectorHex},
			"latest",
		},
		ID: 1,
	})
	if err != nil {
		return nil, err
	}
	hexStr, ok := resp.Result.(string)
	if !ok {
		return nil, fmt.Errorf("eth_call: unexpected result type %T", resp.Result)
	}
	hexStr = strings.TrimPrefix(hexStr, "0x")
	if hexStr == "" {
		return big.NewInt(0), nil
	}
	v, ok2 := new(big.Int).SetString(hexStr, 16)
	if !ok2 {
		return nil, fmt.Errorf("eth_call: cannot parse hex %q", hexStr)
	}
	return v, nil
}

// ethCallAddress calls a function that takes a single uint256 and returns an address.
// selectorHex is 4 bytes (no 0x prefix); arg is ABI-encoded as a 32-byte big-endian word.
func ethCallAddress(rpcURL, contractAddr, selectorHex string, arg uint64) (string, error) {
	data := "0x" + selectorHex + fmt.Sprintf("%064x", arg)
	resp, err := doRPC(rpcURL, jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_call",
		Params: []interface{}{
			map[string]string{"to": contractAddr, "data": data},
			"latest",
		},
		ID: 1,
	})
	if err != nil {
		return "", err
	}
	hexStr, ok := resp.Result.(string)
	if !ok {
		return "", fmt.Errorf("eth_call: unexpected result type %T", resp.Result)
	}
	hexStr = strings.TrimPrefix(hexStr, "0x")
	if len(hexStr) < 40 {
		return "", fmt.Errorf("eth_call: result too short to contain an address: %q", hexStr)
	}
	// ABI-encoded address: 32 bytes, address in the last 20 bytes (rightmost 40 hex chars)
	addr := "0x" + hexStr[len(hexStr)-40:]
	return strings.ToLower(addr), nil
}

type logEntry struct {
	Topics []string `json:"topics"`
}

// ethGetLogs fetches all logs for the given contract matching topic0.
func ethGetLogs(rpcURL, contractAddr, topic0 string) ([]logEntry, error) {
	resp, err := doRPC(rpcURL, jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_getLogs",
		Params: []interface{}{
			map[string]interface{}{
				"fromBlock": "0x0",
				"toBlock":   "latest",
				"address":   contractAddr,
				"topics":    []interface{}{topic0},
			},
		},
		ID: 1,
	})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	var logs []logEntry
	if err := json.Unmarshal(raw, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// ─── Receipts ─────────────────────────────────────────────────────────────────

type contractReceipt struct {
	ContractAddress string `json:"contractAddress"`
}

var vaultNames = []string{"Erc20CoinVault", "Erc721CoinVault", "Erc1155CoinVault", "EnygmaErc20CoinVault"}

func loadVaultAddresses(receiptsPath string) (map[string]string, error) {
	safePath, err := safeReceiptsPath(receiptsPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, err
	}
	var all map[string]contractReceipt
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, name := range vaultNames {
		if r, ok := all[name]; ok {
			out[name] = r.ContractAddress
		}
	}
	return out, nil
}

func validateRPCURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid rpcUrl %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("rpcUrl %q uses unsupported scheme %q", rawURL, parsed.Scheme)
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("rpcUrl %q must include a host", rawURL)
	}
	if parsed.User != nil {
		return fmt.Errorf("rpcUrl %q must not include credentials", rawURL)
	}

	origin := canonicalRPCOrigin(parsed)
	for _, allowed := range allowedRPCOrigins() {
		if origin == allowed {
			return nil
		}
	}
	return fmt.Errorf("rpcUrl origin %q is not allowed; add it to %s to permit it", origin, rpcAllowlistEnv)
}

func allowedRPCOrigins() []string {
	origins := make([]string, 0, len(defaultAllowedRPCOrigins)+4)
	origins = append(origins, defaultAllowedRPCOrigins...)
	for _, raw := range strings.Split(os.Getenv(rpcAllowlistEnv), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			origins = append(origins, canonicalRPCOrigin(parsed))
		}
	}
	return origins
}

func canonicalRPCOrigin(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

func safeReceiptsPath(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("receiptsPath must not be empty")
	}
	if strings.Contains(candidate, "\x00") {
		return "", fmt.Errorf("receiptsPath contains an invalid character")
	}
	if filepath.Base(candidate) != "receipts.json" {
		return "", fmt.Errorf("receiptsPath must point to receipts.json")
	}

	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	absCandidate = filepath.Clean(absCandidate)
	for _, root := range allowedReceiptsRoots() {
		if isPathWithin(absCandidate, root) {
			return absCandidate, nil
		}
	}
	return "", fmt.Errorf("receiptsPath %q is outside the allowed build roots", candidate)
}

func allowedReceiptsRoots() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(cwd, "build"),
		filepath.Join(cwd, "..", "build"),
	}
	for _, root := range strings.Split(os.Getenv(fileRootsEnv), ",") {
		root = strings.TrimSpace(root)
		if root != "" {
			roots = append(roots, root)
		}
	}

	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if absRoot, err := filepath.Abs(root); err == nil {
			out = append(out, filepath.Clean(absRoot))
		}
	}
	return out
}

func isPathWithin(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// ─── Handler ──────────────────────────────────────────────────────────────────

type MerkleStatusRequest struct {
	RpcUrl       string `json:"rpcUrl"`
	ReceiptsPath string `json:"receiptsPath"`
}

type VaultMerkleStatus struct {
	Name        string     `json:"name"`
	Address     string     `json:"address"`
	OnChainRoot string     `json:"onChainRoot"`
	LocalRoot   string     `json:"localRoot"`
	Match       bool       `json:"match"`
	LeafCount   int        `json:"leafCount"`
	TreeNumber  uint64     `json:"treeNumber"`
	Tree        TreeOutput `json:"tree"`
	Error       string     `json:"error,omitempty"`
}

// VaultRegistryEntry is one row of the EnygmaDvP registry cross-check.
type VaultRegistryEntry struct {
	VaultID           uint64 `json:"vaultId"`
	Name              string `json:"name"`
	AddressInDvP      string `json:"addressInDvP"`      // from vaultById(id) on EnygmaDvP
	AddressInReceipts string `json:"addressInReceipts"` // from receipts.json
	Match             bool   `json:"match"`
}

// EnygmaDvPCheck holds the result of comparing receipts.json vault addresses
// against what EnygmaDvP has registered on-chain via vaultById(id).
type EnygmaDvPCheck struct {
	EnygmaDvPAddress string               `json:"enygmaDvpAddress"`
	AllMatch         bool                 `json:"allMatch"`
	Entries          []VaultRegistryEntry `json:"entries"`
	Error            string               `json:"error,omitempty"`
}

type MerkleStatusResponse struct {
	EnygmaDvP EnygmaDvPCheck      `json:"enygmaDvpRegistryCheck"`
	Vaults    []VaultMerkleStatus `json:"vaults"`
}

// MerkleStatusHandler handles POST /util/merkleStatus.
// It reconstructs each vault's Merkle tree locally from on-chain Commitment events
// and compares the computed root against the vault's currentRoot().
//
// Request body (all fields optional):
//
//	{ "rpcUrl": "http://127.0.0.1:8545", "receiptsPath": "../build/receipts.json" }
func MerkleStatusHandler() gin.HandlerFunc {
	const treeDepth = 8

	commitmentTopic := keccak32Hex("Commitment(uint256,uint256)")
	currentRootSel := keccak4("currentRoot()")
	treeNumberSel := keccak4("treeNumber()")
	vaultByIDSel := keccak4("vaultById(uint256)")

	return func(c *gin.Context) {
		var req MerkleStatusRequest
		_ = c.ShouldBindJSON(&req) // all fields optional
		if req.RpcUrl == "" {
			req.RpcUrl = defaultMerkleRPCURL
		}
		if req.ReceiptsPath == "" {
			req.ReceiptsPath = "../build/receipts.json"
		}
		if err := validateRPCURL(req.RpcUrl); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		receiptsPath, err := safeReceiptsPath(req.ReceiptsPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.ReceiptsPath = receiptsPath

		vaultAddrs, err := loadVaultAddresses(req.ReceiptsPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": fmt.Sprintf("load receipts: %v", err)})
			return
		}

		// Cross-check vault addresses registered in EnygmaDvP vs receipts.json.
		dvpCheck := checkEnygmaDvPRegistry(req.RpcUrl, req.ReceiptsPath, vaultAddrs, vaultByIDSel)

		statuses := make([]VaultMerkleStatus, 0, len(vaultNames))
		for _, name := range vaultNames {
			addr, ok := vaultAddrs[name]
			if !ok {
				statuses = append(statuses, VaultMerkleStatus{
					Name:  name,
					Error: "not found in receipts.json",
				})
				continue
			}
			s := checkVault(req.RpcUrl, name, addr, treeDepth,
				commitmentTopic, currentRootSel, treeNumberSel)
			statuses = append(statuses, s)
		}

		c.JSON(http.StatusOK, MerkleStatusResponse{EnygmaDvP: dvpCheck, Vaults: statuses})
	}
}

// vaultIDByName maps each vault contract name to its on-chain vaultId (position
// in EnygmaDvP._coinVaults[], assigned in registration order by deploy/init).
var vaultIDByName = map[string]uint64{
	"Erc20CoinVault":       0,
	"Erc721CoinVault":      1,
	"Erc1155CoinVault":     2,
	"EnygmaErc20CoinVault": 3,
}

func checkEnygmaDvPRegistry(rpcURL, receiptsPath string, receiptsAddrs map[string]string, vaultByIDSel string) EnygmaDvPCheck {
	// Load EnygmaDvP address from receipts.
	safePath, err := safeReceiptsPath(receiptsPath)
	if err != nil {
		return EnygmaDvPCheck{Error: fmt.Sprintf("validate receipts path: %v", err)}
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return EnygmaDvPCheck{Error: fmt.Sprintf("read receipts: %v", err)}
	}
	var all map[string]contractReceipt
	if err := json.Unmarshal(data, &all); err != nil {
		return EnygmaDvPCheck{Error: fmt.Sprintf("parse receipts: %v", err)}
	}
	dvpReceipt, ok := all["EnygmaDvp"]
	if !ok {
		return EnygmaDvPCheck{Error: "EnygmaDvp not found in receipts.json"}
	}
	dvpAddr := dvpReceipt.ContractAddress

	entries := make([]VaultRegistryEntry, 0, len(vaultNames))
	allMatch := true

	for _, name := range vaultNames {
		id := vaultIDByName[name]
		onChainAddr, err := ethCallAddress(rpcURL, dvpAddr, vaultByIDSel, id)
		if err != nil {
			entries = append(entries, VaultRegistryEntry{
				VaultID:           id,
				Name:              name,
				AddressInDvP:      fmt.Sprintf("error: %v", err),
				AddressInReceipts: strings.ToLower(receiptsAddrs[name]),
				Match:             false,
			})
			allMatch = false
			continue
		}
		receiptAddr := strings.ToLower(receiptsAddrs[name])
		match := onChainAddr == receiptAddr
		if !match {
			allMatch = false
		}
		entries = append(entries, VaultRegistryEntry{
			VaultID:           id,
			Name:              name,
			AddressInDvP:      onChainAddr,
			AddressInReceipts: receiptAddr,
			Match:             match,
		})
	}

	return EnygmaDvPCheck{
		EnygmaDvPAddress: dvpAddr,
		AllMatch:         allMatch,
		Entries:          entries,
	}
}

func checkVault(rpcURL, name, addr string, depth int,
	commitmentTopic, currentRootSel, treeNumberSel string,
) VaultMerkleStatus {
	s := VaultMerkleStatus{Name: name, Address: addr}

	// 1. On-chain current root
	onChainRoot, err := ethCallUint256(rpcURL, addr, currentRootSel)
	if err != nil {
		s.Error = fmt.Sprintf("currentRoot(): %v", err)
		return s
	}
	s.OnChainRoot = onChainRoot.String()

	// 2. On-chain tree number (how many times the tree has rolled over)
	treeNum, err := ethCallUint256(rpcURL, addr, treeNumberSel)
	if err != nil {
		s.Error = fmt.Sprintf("treeNumber(): %v", err)
		return s
	}
	s.TreeNumber = treeNum.Uint64()

	// 3. Collect all Commitment events in order
	//    event Commitment(uint256 indexed vaultId, uint256 indexed commitment)
	//    topics[0] = event signature, topics[1] = vaultId, topics[2] = commitment
	logs, err := ethGetLogs(rpcURL, addr, commitmentTopic)
	if err != nil {
		s.Error = fmt.Sprintf("eth_getLogs: %v", err)
		return s
	}
	s.LeafCount = len(logs)

	// 4. Replay insertions into a local Merkle tree and compute root
	mt := newLocalMerkleTree(depth)
	for i, lg := range logs {
		if len(lg.Topics) < 3 {
			s.Error = fmt.Sprintf("log[%d]: expected 3 topics, got %d", i, len(lg.Topics))
			return s
		}
		hexVal := strings.TrimPrefix(lg.Topics[2], "0x")
		leaf, ok := new(big.Int).SetString(hexVal, 16)
		if !ok {
			s.Error = fmt.Sprintf("log[%d]: cannot parse commitment %q", i, lg.Topics[2])
			return s
		}
		mt.insertLeaf(leaf)
	}

	s.LocalRoot = mt.root()
	s.Match = s.LocalRoot == s.OnChainRoot
	s.Tree = mt.snapshot()
	return s
}

// ─── Per-vault handler ────────────────────────────────────────────────────────

// vaultNameByID is the reverse of vaultIDByName.
var vaultNameByID = func() map[uint64]string {
	m := make(map[uint64]string, len(vaultIDByName))
	for name, id := range vaultIDByName {
		m[id] = name
	}
	return m
}()

type MerkleVaultRequest struct {
	// Identify the vault by name OR by id — one is required.
	Vault        string  `json:"vault"`   // e.g. "Erc20CoinVault"
	VaultID      *uint64 `json:"vaultId"` // e.g. 0  (pointer so 0 is distinguishable from absent)
	RpcUrl       string  `json:"rpcUrl"`
	ReceiptsPath string  `json:"receiptsPath"`
}

// MerkleVaultHandler handles POST /util/merkleVault.
// Returns the full Merkle tree status for a single vault identified by name or vaultId.
//
// Request examples:
//
//	{ "vault": "Erc20CoinVault" }
//	{ "vaultId": 0 }
//	{ "vault": "Erc20CoinVault", "rpcUrl": "http://127.0.0.1:8545" }
func MerkleVaultHandler() gin.HandlerFunc {
	const treeDepth = 8

	commitmentTopic := keccak32Hex("Commitment(uint256,uint256)")
	currentRootSel := keccak4("currentRoot()")
	treeNumberSel := keccak4("treeNumber()")

	return func(c *gin.Context) {
		var req MerkleVaultRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.RpcUrl == "" {
			req.RpcUrl = defaultMerkleRPCURL
		}
		if req.ReceiptsPath == "" {
			req.ReceiptsPath = "../build/receipts.json"
		}
		if err := validateRPCURL(req.RpcUrl); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		receiptsPath, err := safeReceiptsPath(req.ReceiptsPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.ReceiptsPath = receiptsPath

		// Resolve vault name from either field.
		vaultName := req.Vault
		if vaultName == "" && req.VaultID != nil {
			name, ok := vaultNameByID[*req.VaultID]
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":       fmt.Sprintf("unknown vaultId %d", *req.VaultID),
					"validIds":    []uint64{0, 1, 2, 3},
					"validVaults": vaultNames,
				})
				return
			}
			vaultName = name
		}
		if vaultName == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       "provide either \"vault\" (name) or \"vaultId\" (0–3)",
				"validVaults": vaultNames,
			})
			return
		}

		// Validate name.
		found := false
		for _, n := range vaultNames {
			if n == vaultName {
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       fmt.Sprintf("unknown vault %q", vaultName),
				"validVaults": vaultNames,
			})
			return
		}

		// Load address from receipts.
		vaultAddrs, err := loadVaultAddresses(req.ReceiptsPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": fmt.Sprintf("load receipts: %v", err)})
			return
		}
		addr, ok := vaultAddrs[vaultName]
		if !ok {
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": fmt.Sprintf("%s not found in receipts.json", vaultName)})
			return
		}

		status := checkVault(req.RpcUrl, vaultName, addr, treeDepth,
			commitmentTopic, currentRootSel, treeNumberSel)
		c.JSON(http.StatusOK, status)
	}
}

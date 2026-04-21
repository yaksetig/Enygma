package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raylsnetwork/enygma_dvp/src/core"
)

// --- Utility Function Tests ---

func TestChunkArray_BasicChunking(t *testing.T) {
	arr := []interface{}{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	chunks := core.ChunkArray(arr, 4)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	// First chunk: a, b, c, d
	if chunks[0][0] != "a" || chunks[0][3] != "d" {
		t.Errorf("first chunk mismatch: %v", chunks[0])
	}
	// Second chunk: e, f, g, h
	if chunks[1][0] != "e" || chunks[1][3] != "h" {
		t.Errorf("second chunk mismatch: %v", chunks[1])
	}
	// Third chunk: i, j, nil, nil (padded)
	if chunks[2][0] != "i" || chunks[2][1] != "j" {
		t.Errorf("third chunk mismatch: %v", chunks[2])
	}
	if chunks[2][2] != nil || chunks[2][3] != nil {
		t.Errorf("expected nil padding in third chunk, got: %v", chunks[2])
	}
}

func TestChunkArray_DefaultSize(t *testing.T) {
	arr := make([]interface{}, 16)
	for i := range arr {
		arr[i] = i
	}
	chunks := core.ChunkArray(arr, 8)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0][0] != 0 || chunks[0][7] != 7 {
		t.Errorf("first chunk: %v", chunks[0])
	}
	if chunks[1][0] != 8 || chunks[1][7] != 15 {
		t.Errorf("second chunk: %v", chunks[1])
	}
}

func TestChunkArray_Empty(t *testing.T) {
	chunks := core.ChunkArray([]interface{}{}, 8)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty array, got %d", len(chunks))
	}
}

func TestChunkArray_ZeroChunkSize(t *testing.T) {
	arr := []interface{}{1, 2, 3}
	// chunkSize <= 0 defaults to 8
	chunks := core.ChunkArray(arr, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with default size, got %d", len(chunks))
	}
}

func TestSplitPathElements(t *testing.T) {
	elements := make([]interface{}, 16)
	for i := range elements {
		elements[i] = i
	}
	inputs := map[string]interface{}{
		"wt_pathElements": elements,
	}
	chunks := core.SplitPathElements(inputs)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0][0] != 0 || chunks[0][7] != 7 {
		t.Errorf("first chunk: %v", chunks[0])
	}
	if chunks[1][0] != 8 || chunks[1][7] != 15 {
		t.Errorf("second chunk: %v", chunks[1])
	}
}

func TestSplitPathElements_NilInput(t *testing.T) {
	inputs := map[string]interface{}{}
	chunks := core.SplitPathElements(inputs)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for nil input, got %d", len(chunks))
	}
}

// --- Client Construction Tests ---

func TestNewGnarkClient_DefaultURL(t *testing.T) {
	client := core.NewGnarkClient("")
	if client.BaseURL != "http://localhost:8081" {
		t.Errorf("expected default URL, got %s", client.BaseURL)
	}
	if client.Client == nil {
		t.Error("expected non-nil http.Client")
	}
}

func TestNewGnarkClient_CustomURL(t *testing.T) {
	client := core.NewGnarkClient("http://example.com:9090")
	if client.BaseURL != "http://example.com:9090" {
		t.Errorf("expected custom URL, got %s", client.BaseURL)
	}
}

// --- GnarkProver Router Tests ---

func TestGnarkProver_DispatchesErc721(t *testing.T) {
	server := newMockServer(t, "/proof/ownershipERC721", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc721Inputs()

	resp, privateMint, err := client.GnarkProver(inputs, "some/path/OwnershipErc721.zkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if privateMint != nil {
		t.Error("expected nil PrivateMintProofResponse")
	}
	if resp.Status != 200 || resp.Message != "ok" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGnarkProver_DispatchesErc20(t *testing.T) {
	server := newMockServer(t, "/proof/joinSplitERC20", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc20Inputs()

	resp, _, err := client.GnarkProver(inputs, "some/path/JoinSplitErc20.zkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestGnarkProver_DispatchesErc20_10_2(t *testing.T) {
	server := newMockServer(t, "/proof/joinSplitERC20_10_2", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc20Inputs()

	resp, _, err := client.GnarkProver(inputs, "./build/JoinSplitErc20_10_2.zkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestGnarkProver_DispatchesAuctionNotWinningBid(t *testing.T) {
	server := newMockServer(t, "/proof/auctionNotWinning", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalAuctionNotWinningInputs()

	resp, _, err := client.GnarkProver(inputs, "path/AuctionNotWinningBid.zkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestGnarkProver_UnknownPath(t *testing.T) {
	client := core.NewGnarkClient("http://localhost:9999")
	_, _, err := client.GnarkProver(map[string]interface{}{}, "unknown/path.zkey")
	if err == nil {
		t.Fatal("expected error for unknown zkeyPath")
	}
}

// --- HTTP Proof Endpoint Tests ---

func TestErc20Proof_CorrectEndpointAndPayload(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proof/joinSplitERC20" {
			t.Errorf("expected path /proof/joinSplitERC20, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"status":200,"message":"ok"}`))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc20Inputs()

	resp, err := client.Erc20Proof(inputs, "./build/JoinSplitErc20.zkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	// Verify key fields in the payload
	if receivedBody["StMessage"] != "test_message" {
		t.Errorf("expected StMessage=test_message, got %v", receivedBody["StMessage"])
	}
	if receivedBody["WtErc20ContractAddress"] != "0xABC" {
		t.Errorf("expected WtErc20ContractAddress=0xABC, got %v", receivedBody["WtErc20ContractAddress"])
	}
}

func TestErc721Proof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/ownershipERC721", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.Erc721Proof(makeMinimalErc721Inputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestErc1155FungibleProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/erc155Fungible", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.Erc1155FungibleProof(makeMinimalErc1155FungibleInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestErc1155FungibleAuditorProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/erc1155FungibleAuditor", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc1155FungibleInputs()
	// Add auditor fields
	inputs["st_auditor_publicKey"] = "auditor_pk"
	inputs["st_auditor_authKey"] = "auditor_ak"
	inputs["st_auditor_nonce"] = "1"
	inputs["st_auditor_encryptedValues"] = "encrypted"
	inputs["wt_auditor_random"] = "random"

	resp, err := client.Erc1155FungibleAuditorProof(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestErc1155NonFungibleProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/erc1155NonFungible", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.Erc1155NonFungibleProof(makeMinimalErc1155NonFungibleInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestErc1155NonFungibleWithAuditorProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/erc1155NonFungibleAuditor", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalErc1155NonFungibleInputs()
	inputs["wt_erc1155TokenIds"] = inputs["wt_erc1155TokenIds"]
	inputs["st_auditor_publicKey"] = "auditor_pk"
	inputs["st_auditor_authKey"] = "auditor_ak"
	inputs["st_auditor_nonce"] = "1"
	inputs["st_auditor_encryptedValues"] = "encrypted"
	inputs["wt_auditor_random"] = "random"

	resp, err := client.Erc1155NonFungibleWithAuditorProof(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestAuctionBidAuditorProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/auctionBidAuditor", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.AuctionBidAuditorProof(makeMinimalAuctionBidAuditorInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestAuctionInitAuditorProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/auctionInitAuditor", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.AuctionInitAuditorProof(makeMinimalAuctionInitAuditorInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestAuctionPrivateOpeningProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/auctionPrivateOpening", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := map[string]interface{}{
		"st_auctionId":  "42",
		"st_blindedBid": "12345",
		"wt_bidAmount":  "100",
		"wt_bidRandom":  "999",
	}

	resp, err := client.AuctionPrivateOpeningProof(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestAuctionNotWinningBidProof_CorrectEndpoint(t *testing.T) {
	server := newMockServer(t, "/proof/auctionNotWinning", `{"status":200,"message":"ok"}`)
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	resp, err := client.AuctionNotWinningBidProof(makeMinimalAuctionNotWinningInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestPrivateMintProof_CorrectEndpointAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proof/privateMint" {
			t.Errorf("expected path /proof/privateMint, got %s", r.URL.Path)
		}
		var body map[string]interface{}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)

		// Verify salt defaults to "0" when nil
		if body["salt"] != "0" {
			t.Errorf("expected salt=0, got %v", body["salt"])
		}

		w.WriteHeader(200)
		w.Write([]byte(`{"proof":["0x1","0x2"],"publicSignal":["0xa","0xb"]}`))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := map[string]interface{}{
		"commitment":      "0xcommit",
		"contractAddress": "0xcontract",
		"tokenId":         "1",
		"salt":            nil,
		"amount":          "100",
		"publicKey":       "0xpk",
		"cipherText":      "0xcipher",
	}

	resp, err := client.PrivateMintProof(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Proof) != 2 || resp.Proof[0] != "0x1" {
		t.Errorf("unexpected proof: %v", resp.Proof)
	}
	if len(resp.PublicSignal) != 2 || resp.PublicSignal[0] != "0xa" {
		t.Errorf("unexpected publicSignal: %v", resp.PublicSignal)
	}
}

// --- Error Handling Tests ---

func TestPostProof_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal server error`))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := map[string]interface{}{
		"st_auctionId":  "1",
		"st_blindedBid": "2",
		"wt_bidAmount":  "3",
		"wt_bidRandom":  "4",
	}

	_, err := client.AuctionPrivateOpeningProof(inputs)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPostProof_ConnectionRefused(t *testing.T) {
	client := core.NewGnarkClient("http://localhost:1") // unlikely port
	inputs := map[string]interface{}{
		"st_auctionId":  "1",
		"st_blindedBid": "2",
		"wt_bidAmount":  "3",
		"wt_bidRandom":  "4",
	}

	_, err := client.AuctionPrivateOpeningProof(inputs)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

// --- Input Serialization Tests ---

func TestErc20Proof_SerializesPathElements(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"status":200}`))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	elements := make([]interface{}, 16)
	for i := range elements {
		elements[i] = i
	}
	inputs := makeMinimalErc20Inputs()
	inputs["wt_pathElements"] = elements

	client.Erc20Proof(inputs, "./build/JoinSplitErc20.zkey")

	pathElements, ok := receivedBody["WtPathElements"].([]interface{})
	if !ok {
		t.Fatalf("WtPathElements not a slice: %T", receivedBody["WtPathElements"])
	}
	if len(pathElements) != 2 {
		t.Errorf("expected 2 path element chunks, got %d", len(pathElements))
	}
}

func TestAuctionBidAuditor_SerializesPathElementsSplit(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"status":200}`))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	inputs := makeMinimalAuctionBidAuditorInputs()

	client.AuctionBidAuditorProof(inputs)

	pathElements, ok := receivedBody["wtPathElements"].([]interface{})
	if !ok {
		t.Fatalf("wtPathElements not a slice: %T", receivedBody["wtPathElements"])
	}
	if len(pathElements) != 2 {
		t.Errorf("expected 2 path element splits, got %d", len(pathElements))
	}
}

// --- Test helpers ---

func newMockServer(t *testing.T, expectedPath string, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(200)
		w.Write([]byte(response))
	}))
}

func makeMinimalErc20Inputs() map[string]interface{} {
	return map[string]interface{}{
		"st_message":              "test_message",
		"st_treeNumbers":          []interface{}{"0", "1"},
		"st_merkleRoots":          []interface{}{"root0", "root1"},
		"st_nullifiers":           []interface{}{"null0", "null1"},
		"st_commitmentsOut":       []interface{}{"cmt0", "cmt1"},
		"wt_privateKeysIn":        []interface{}{"pk0", "pk1"},
		"wt_publicKeysOut":        []interface{}{"pubk0", "pubk1"},
		"wt_pathElements":         make([]interface{}, 16),
		"wt_pathIndices":          []interface{}{"0", "1"},
		"wt_valuesIn":             []interface{}{"100", "200"},
		"wt_valuesOut":            []interface{}{"150", "150"},
		"wt_erc20ContractAddress": "0xABC",
	}
}

func makeMinimalErc721Inputs() map[string]interface{} {
	return map[string]interface{}{
		"st_message":        "test_message",
		"st_treeNumbers":    []interface{}{"0"},
		"st_merkleRoots":    []interface{}{"root0"},
		"st_nullifiers":     []interface{}{"null0"},
		"st_commitmentsOut": []interface{}{"cmt0"},
		"wt_privateKeysIn":  []interface{}{"pk0"},
		"wt_values":         []interface{}{"1"},
		"wt_pathElements":   []interface{}{"elem0", "elem1", "elem2"},
		"wt_pathIndices":    []interface{}{"0"},
		"wt_publicKeysOut":  []interface{}{"pubk0"},
	}
}

func makeMinimalErc1155FungibleInputs() map[string]interface{} {
	elements := make([]interface{}, 16)
	for i := range elements {
		elements[i] = "el"
	}
	return map[string]interface{}{
		"st_message":                 "test_message",
		"st_treeNumbers":             []interface{}{"0", "1"},
		"st_merkleRoots":             []interface{}{"root0", "root1"},
		"st_commitmentsOut":          []interface{}{"cmt0", "cmt1"},
		"st_nullifiers":              []interface{}{"null0", "null1"},
		"st_assetGroup_merkleRoot":   "agRoot",
		"st_assetGroup_treeNumber":   "0",
		"wt_privateKeysIn":           []interface{}{"pk0", "pk1"},
		"wt_valuesIn":                []interface{}{"100", "200"},
		"wt_pathElements":            elements,
		"wt_pathIndices":             []interface{}{"0", "1"},
		"wt_erc1155ContractAddress":  "0xDEF",
		"wt_erc1155TokenId":          "42",
		"wt_publicKeysOut":           []interface{}{"pubk0", "pubk1"},
		"wt_valuesOut":               []interface{}{"150", "150"},
		"wt_assetGroup_pathElements": []interface{}{"agEl0", "agEl1"},
		"wt_assetGroup_pathIndices":  "0",
	}
}

func makeMinimalErc1155NonFungibleInputs() map[string]interface{} {
	return map[string]interface{}{
		"st_message":                 "test_message",
		"st_treeNumbers":             []interface{}{"0"},
		"st_merkleRoots":             []interface{}{"root0"},
		"st_nullifiers":              []interface{}{"null0"},
		"st_commitmentsOut":          []interface{}{"cmt0"},
		"st_assetGroup_treeNumbers":  []interface{}{"0"},
		"st_assetGroup_merkleRoots":  []interface{}{"agRoot0"},
		"wt_privateKeysIn":           []interface{}{"pk0"},
		"wt_values":                  []interface{}{"1"},
		"wt_pathElements":            []interface{}{"elem0", "elem1"},
		"wt_pathIndices":             []interface{}{"0"},
		"wt_erc1155TokenIds":         []interface{}{"42"},
		"wt_publicKeysOut":           []interface{}{"pubk0"},
		"wt_erc1155ContractAddress":  "0xDEF",
		"wt_assetGroup_pathElements": []interface{}{[]interface{}{"agEl0", "agEl1"}},
		"wt_assetGroup_pathIndices":  []interface{}{"0"},
	}
}

func makeMinimalAuctionBidAuditorInputs() map[string]interface{} {
	elements := make([]interface{}, 16)
	for i := range elements {
		elements[i] = "el"
	}
	return map[string]interface{}{
		"st_beacon":                    "beacon",
		"st_auctionId":                 "1",
		"st_blindedBid":                "bid123",
		"st_vaultId":                   "0",
		"st_treeNumbers":               []interface{}{"0", "1"},
		"st_merkleRoots":               []interface{}{"root0", "root1"},
		"st_nullifiers":                []interface{}{"null0", "null1"},
		"st_commitmentsOut":            []interface{}{"cmt0", "cmt1"},
		"st_assetGroup_treeNumber":     "0",
		"st_assetGroup_merkleRoot":     "agRoot",
		"st_auctioneer_publicKey":      []interface{}{"auc_pk0", "auc_pk1"},
		"st_auctioneer_authKey":        []interface{}{"auc_ak0", "auc_ak1"},
		"st_auctioneer_nonce":          "42",
		"st_auctioneer_encryptedValues": []interface{}{"ev0", "ev1"},
		"wt_auctioneer_random":         "rand_auc",
		"st_auditor_publicKey":         []interface{}{"aud_pk0", "aud_pk1"},
		"st_auditor_authKey":           []interface{}{"aud_ak0", "aud_ak1"},
		"st_auditor_nonce":             "43",
		"st_auditor_encryptedValues":   []interface{}{"aev0", "aev1"},
		"wt_auditor_random":            "rand_aud",
		"wt_bidAmount":                 "100",
		"wt_bidRandom":                 "999",
		"wt_privateKeysIn":             []interface{}{"pk0", "pk1"},
		"wt_pathElements":              elements,
		"wt_pathIndices":               []interface{}{"0", "1"},
		"wt_contractAddress":           "0xABC",
		"wt_publicKeysOut":             []interface{}{"pubk0", "pubk1"},
		"wt_assetGroup_pathElements":   []interface{}{"agEl0", "agEl1"},
		"wt_assetGroup_pathIndices":    "0",
		"wt_idParamsIn":                []interface{}{[]interface{}{"p0", "p1"}, []interface{}{"p2", "p3"}},
		"wt_idParamsOut":               []interface{}{[]interface{}{"q0", "q1"}, []interface{}{"q2", "q3"}},
	}
}

func makeMinimalAuctionInitAuditorInputs() map[string]interface{} {
	return map[string]interface{}{
		"st_beacon":                  "beacon",
		"st_auctionId":               "1",
		"st_vaultId":                 "0",
		"st_treeNumber":              "0",
		"st_merkleRoot":              "root0",
		"st_nullifier":               "null0",
		"st_auditor_publicKey":       []interface{}{"aud_pk0"},
		"st_auditor_authKey":         []interface{}{"aud_ak0"},
		"st_auditor_nonce":           "43",
		"st_auditor_encryptedValues": []interface{}{"aev0"},
		"wt_auditor_random":          "rand_aud",
		"st_assetGroup_treeNumber":   "0",
		"st_assetGroup_merkleRoot":   "agRoot",
		"wt_commitment":              "cmt0",
		"wt_pathElements":            []interface{}{"elem0"},
		"wt_pathIndices":             "0",
		"wt_privateKey":              "pk0",
		"wt_idParams":                []interface{}{"id0"},
		"wt_contractAddress":         "0xABC",
		"wt_assetGroup_pathElements": []interface{}{"agEl0"},
		"wt_assetGroup_pathIndices":  "0",
	}
}

func makeMinimalAuctionNotWinningInputs() map[string]interface{} {
	return map[string]interface{}{
		"st_auctionId":            "1",
		"st_blindedBidDifference": "50",
		"st_bidBlockNumber":       "100",
		"st_winningBidBlockNumber": "101",
		"wt_bidAmount":            "200",
		"wt_bidRandom":            "999",
		"wt_winningBidAmount":     "250",
		"wt_winningBidRandom":     "888",
	}
}

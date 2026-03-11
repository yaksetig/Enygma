package tests

import (
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"enygma_dvp/src_go/core"
)

// --- Helper: create a mock gnark server ---

func newAuctionMockServer(t *testing.T, expectedPath string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"status":200,"message":"ok"}`))
	}))
}

func newPayloadCapturingServer(t *testing.T, expectedPath string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		w.WriteHeader(200)
		w.Write([]byte(`{"status":200,"message":"ok"}`))
	}))
	return server, &captured
}

// --- Helper: build minimal MerkleProof ---

func makeMerkleProof(depth int) *core.MerkleProof {
	elements := make([]*big.Int, depth)
	for i := range elements {
		elements[i] = big.NewInt(int64(i + 100))
	}
	return &core.MerkleProof{
		Element:  big.NewInt(42),
		Elements: elements,
		Indices:  big.NewInt(3),
		Root:     big.NewInt(999),
	}
}

func makeKeyPair(privVal, pubVal int64) core.KeyPair {
	return core.KeyPair{
		PrivateKey: big.NewInt(privVal),
		PublicKey:  big.NewInt(pubVal),
	}
}

// --- AuctionInitProof Tests ---

func TestAuctionInitProof_Success(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	result, err := client.AuctionInitProof(
		big.NewInt(1),           // stBeacon
		big.NewInt(42),          // tokenId
		big.NewInt(0xABC),       // wtContractAddress
		makeKeyPair(10, 20),     // keyIn
		merkleDepth,             // merkleDepth
		makeMerkleProof(merkleDepth), // merkleProof
		big.NewInt(999),         // merkleRoot
		big.NewInt(0),           // stTreeNumber
		0,                       // vaultId (ERC20)
		big.NewInt(555),         // assetGroupMerkleRoot
		makeMerkleProof(merkleDepth), // assetGroupMerkleProof
		[]*big.Int{big.NewInt(100)},  // wtIdParams
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NumberOfInputs != 1 || result.NumberOfOutputs != 1 {
		t.Errorf("expected 1 input, 1 output, got %d, %d", result.NumberOfInputs, result.NumberOfOutputs)
	}
	// Statement: [beacon, vaultId, auctionId, treeNumber, merkleRoot, nullifier, assetGroupMerkleRoot]
	if len(result.Statement) != 7 {
		t.Errorf("expected 7 statement elements, got %d", len(result.Statement))
	}
	// First element should be beacon=1
	if result.Statement[0].Cmp(big.NewInt(1)) != 0 {
		t.Errorf("expected beacon=1, got %s", result.Statement[0])
	}
	// Second element should be vaultId=0
	if result.Statement[1].Cmp(big.NewInt(0)) != 0 {
		t.Errorf("expected vaultId=0, got %s", result.Statement[1])
	}
}

func TestAuctionInitProof_ZeroAssetGroupRoot(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	result, err := client.AuctionInitProof(
		big.NewInt(1),
		big.NewInt(42),
		big.NewInt(0xABC),
		makeKeyPair(10, 20),
		merkleDepth,
		makeMerkleProof(merkleDepth),
		big.NewInt(999),
		big.NewInt(0),
		0,
		big.NewInt(0), // zero asset group root → zeros for path elements
		nil,           // nil assetGroupMerkleProof
		[]*big.Int{big.NewInt(100)},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Last statement element should be assetGroupMerkleRoot=0
	last := result.Statement[len(result.Statement)-1]
	if last.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("expected assetGroupMerkleRoot=0, got %s", last)
	}
}

func TestAuctionInitProof_VaultIdErc721(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	_, err := client.AuctionInitProof(
		big.NewInt(1), big.NewInt(42), big.NewInt(0xABC),
		makeKeyPair(10, 20), merkleDepth, makeMerkleProof(merkleDepth),
		big.NewInt(999), big.NewInt(0),
		1, // vaultId=1 (ERC721)
		big.NewInt(555), makeMerkleProof(merkleDepth),
		[]*big.Int{big.NewInt(100)},
	)
	if err != nil {
		t.Fatalf("unexpected error for ERC721 vaultId: %v", err)
	}
}

func TestAuctionInitProof_VaultIdErc1155(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	_, err := client.AuctionInitProof(
		big.NewInt(1), big.NewInt(42), big.NewInt(0xABC),
		makeKeyPair(10, 20), merkleDepth, makeMerkleProof(merkleDepth),
		big.NewInt(999), big.NewInt(0),
		2, // vaultId=2 (ERC1155)
		big.NewInt(555), makeMerkleProof(merkleDepth),
		[]*big.Int{big.NewInt(100), big.NewInt(200)}, // idParams: [amount, tokenId]
	)
	if err != nil {
		t.Fatalf("unexpected error for ERC1155 vaultId: %v", err)
	}
}

func TestAuctionInitProof_InvalidVaultId(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	_, err := client.AuctionInitProof(
		big.NewInt(1), big.NewInt(42), big.NewInt(0xABC),
		makeKeyPair(10, 20), merkleDepth, makeMerkleProof(merkleDepth),
		big.NewInt(999), big.NewInt(0),
		99, // invalid vaultId
		big.NewInt(555), makeMerkleProof(merkleDepth),
		[]*big.Int{big.NewInt(100)},
	)
	if err == nil {
		t.Fatal("expected error for invalid vaultId")
	}
}

func TestAuctionInitProof_PayloadFields(t *testing.T) {
	server, captured := newPayloadCapturingServer(t, "/proof/auctionInit")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	client.AuctionInitProof(
		big.NewInt(1), big.NewInt(42), big.NewInt(0xABC),
		makeKeyPair(10, 20), merkleDepth, makeMerkleProof(merkleDepth),
		big.NewInt(999), big.NewInt(0), 0,
		big.NewInt(555), makeMerkleProof(merkleDepth),
		[]*big.Int{big.NewInt(100)},
	)

	payload := *captured
	if payload["StBeacon"] != "1" {
		t.Errorf("expected StBeacon=1, got %v", payload["StBeacon"])
	}
	if payload["StVaultId"] != "0" {
		t.Errorf("expected StVaultId=0, got %v", payload["StVaultId"])
	}
	if payload["WtContractAddress"] != "2748" {
		t.Errorf("expected WtContractAddress=2748 (0xABC), got %v", payload["WtContractAddress"])
	}
}

// --- AuctionBidProof Tests ---

func TestAuctionBidProof_Success(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionBid")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	keysIn := []core.KeyPair{makeKeyPair(10, 20), makeKeyPair(30, 40)}
	keysOut := []core.KeyPair{makeKeyPair(50, 60), makeKeyPair(70, 80)}

	result, err := client.AuctionBidProof(
		big.NewInt(1),                                      // stAuctionId
		big.NewInt(100),                                    // wtBidAmount
		big.NewInt(999),                                    // wtBidRandom
		big.NewInt(0xABC),                                  // assetAddress
		[]*big.Int{big.NewInt(50), big.NewInt(50)},         // wtValuesIn
		keysIn,
		[]*big.Int{big.NewInt(40), big.NewInt(60)},         // wtValuesOut
		keysOut,
		merkleDepth,
		MerkleProofPair(merkleDepth, 2),                    // merkleProofs
		[]*big.Int{big.NewInt(999), big.NewInt(999)},       // stMerkleRoots
		[]*big.Int{big.NewInt(0), big.NewInt(0)},           // stTreeNumbers
		0,                                                   // stVaultId
		[][]*big.Int{{big.NewInt(50)}, {big.NewInt(50)}},   // wtIdParamsIn
		[][]*big.Int{{big.NewInt(40)}, {big.NewInt(60)}},   // wtIdParamsOut
		big.NewInt(555),                                     // stAssetGroupMerkleRoot
		makeMerkleProof(merkleDepth),                        // assetGroupMerkleProof
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NumberOfInputs != 2 || result.NumberOfOutputs != 2 {
		t.Errorf("expected 2 inputs/2 outputs, got %d/%d", result.NumberOfInputs, result.NumberOfOutputs)
	}
	// Statement: [auctionId, blindedBid, vaultId, ...treeNumbers, ...merkleRoots, ...nullifiers, ...commitmentsOut, assetGroupMerkleRoot]
	// = 3 + 2 + 2 + 2 + 2 + 1 = 12
	if len(result.Statement) != 12 {
		t.Errorf("expected 12 statement elements, got %d", len(result.Statement))
	}
}

func TestAuctionBidProof_ZeroValueInput(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionBid")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	// One zero-value input should use zero-filled path elements
	result, err := client.AuctionBidProof(
		big.NewInt(1), big.NewInt(100), big.NewInt(999), big.NewInt(0xABC),
		[]*big.Int{big.NewInt(0), big.NewInt(50)}, // first input is zero
		[]core.KeyPair{makeKeyPair(10, 20), makeKeyPair(30, 40)},
		[]*big.Int{big.NewInt(50)},
		[]core.KeyPair{makeKeyPair(50, 60)},
		merkleDepth,
		MerkleProofPair(merkleDepth, 2),
		[]*big.Int{big.NewInt(999), big.NewInt(999)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		0,
		[][]*big.Int{{big.NewInt(0)}, {big.NewInt(50)}},
		[][]*big.Int{{big.NewInt(50)}},
		big.NewInt(555),
		makeMerkleProof(merkleDepth),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- AuctionPrivateOpeningProof2 Tests ---

func TestAuctionPrivateOpeningProof2_Success(t *testing.T) {
	server, captured := newPayloadCapturingServer(t, "/proof/auctionPrivateOpening")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	result, err := client.AuctionPrivateOpeningProof2(
		big.NewInt(42),    // stAuctionId
		big.NewInt(12345), // stBlindedBid
		big.NewInt(100),   // wtBidAmount
		big.NewInt(999),   // wtBidRandom
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := *captured
	if payload["StVaultId"] != "42" {
		t.Errorf("expected StVaultId=42, got %v", payload["StVaultId"])
	}
	if payload["StBlindedBid"] != "12345" {
		t.Errorf("expected StBlindedBid=12345, got %v", payload["StBlindedBid"])
	}

	if len(result.Statement) != 2 {
		t.Errorf("expected 2 statement elements, got %d", len(result.Statement))
	}
	if result.Statement[0].Cmp(big.NewInt(42)) != 0 {
		t.Errorf("expected auctionId=42 in statement, got %s", result.Statement[0])
	}
	if result.NumberOfInputs != 1 || result.NumberOfOutputs != 1 {
		t.Errorf("expected 1/1, got %d/%d", result.NumberOfInputs, result.NumberOfOutputs)
	}
}

// --- AuctionNotWinningBidProof2 Tests ---

func TestAuctionNotWinningBidProof2_Success(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/auctionNotWinning")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	result, err := client.AuctionNotWinningBidProof2(
		big.NewInt(1),   // stAuctionId
		big.NewInt(100), // stBidBlockNumber
		big.NewInt(101), // stWinningBidBlockNumber
		big.NewInt(200), // wtBidAmount
		big.NewInt(999), // wtBidRandom
		big.NewInt(250), // wtWinningBidAmount
		big.NewInt(888), // wtWinningBidRandom
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Statement: [auctionId, blindedBidDifference, bidBlockNumber, winningBidBlockNumber]
	if len(result.Statement) != 4 {
		t.Errorf("expected 4 statement elements, got %d", len(result.Statement))
	}
	if result.Statement[0].Cmp(big.NewInt(1)) != 0 {
		t.Errorf("expected auctionId=1, got %s", result.Statement[0])
	}
	// blindedBidDifference should be non-negative (computed mod SNARK_SCALAR_FIELD)
	if result.Statement[1].Sign() < 0 {
		t.Errorf("blindedBidDifference should be non-negative, got %s", result.Statement[1])
	}
}

func TestAuctionNotWinningBidProof2_BlindedBidDifference(t *testing.T) {
	server, captured := newPayloadCapturingServer(t, "/proof/auctionNotWinning")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	client.AuctionNotWinningBidProof2(
		big.NewInt(1), big.NewInt(100), big.NewInt(101),
		big.NewInt(200), big.NewInt(999),
		big.NewInt(250), big.NewInt(888),
	)

	payload := *captured
	// StBlindedBidDifference should be present and non-empty
	diff, ok := payload["StBlindedBidDifference"].(string)
	if !ok || diff == "" {
		t.Errorf("expected StBlindedBidDifference to be a non-empty string, got %v", payload["StBlindedBidDifference"])
	}
}

// --- LegitBrokerProof Tests ---

func TestLegitBrokerProof_Success(t *testing.T) {
	server, captured := newPayloadCapturingServer(t, "/proof/legitBroker")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	result, err := client.LegitBrokerProof(
		big.NewInt(42),  // stBeacon
		big.NewInt(123), // wtPrivateKey
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := *captured
	if payload["StBeacon"] != "42" {
		t.Errorf("expected StBeacon=42, got %v", payload["StBeacon"])
	}
	// WtPrivateKey should be the raw private key
	if payload["WtPrivateKey"] != "123" {
		t.Errorf("expected WtPrivateKey=123, got %v", payload["WtPrivateKey"])
	}
	// StBlindedPublicKey should be derived (not empty)
	if payload["StBlindedPublicKey"] == "" {
		t.Error("expected non-empty StBlindedPublicKey")
	}

	if len(result.Statement) != 2 {
		t.Errorf("expected 2 statement elements, got %d", len(result.Statement))
	}
	if result.NumberOfInputs != 0 || result.NumberOfOutputs != 0 {
		t.Errorf("expected 0/0, got %d/%d", result.NumberOfInputs, result.NumberOfOutputs)
	}
}

// --- BrokerRegistrationProof Tests ---

func TestBrokerRegistrationProof_Success(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/brokerRegistration")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	delegatorKeys := []core.KeyPair{makeKeyPair(10, 20), makeKeyPair(30, 40)}
	delegatorProofs := []*core.MerkleProof{makeMerkleProof(merkleDepth), makeMerkleProof(merkleDepth)}

	result, err := client.BrokerRegistrationProof(
		big.NewInt(1),   // stBeacon
		0,               // stVaultId
		big.NewInt(100), // stGroupId
		delegatorKeys,
		merkleDepth,
		[]*big.Int{big.NewInt(0), big.NewInt(0)},               // stDelegatorTreeNumbers
		delegatorProofs,
		[][]*big.Int{{big.NewInt(50)}, {big.NewInt(50)}},        // wtDelegatorIdParams
		big.NewInt(777),                                          // wtBrokerPublicKey
		big.NewInt(0xABC),                                        // wtContractAddress
		big.NewInt(0),                                            // stAssetGroupTreeNumber
		makeMerkleProof(merkleDepth),                             // assetGroupMerkleProof
		big.NewInt(5),                                            // stBrokerMinCommissionRate
		big.NewInt(10),                                           // stBrokerMaxCommissionRate
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NumberOfInputs != 2 {
		t.Errorf("expected 2 inputs (delegators), got %d", result.NumberOfInputs)
	}
	if result.NumberOfOutputs != 0 {
		t.Errorf("expected 0 outputs, got %d", result.NumberOfOutputs)
	}
	// Statement should include: beacon, vaultId, groupId, delegatorTreeNumbers(2), delegatorMerkleRoots(2),
	// delegatorNullifiers(2), blindedPK, minRate, maxRate, assetGroupTreeNumber, assetGroupMerkleRoot
	// = 3 + 2 + 2 + 2 + 5 = 14
	if len(result.Statement) != 14 {
		t.Errorf("expected 14 statement elements, got %d", len(result.Statement))
	}
}

func TestBrokerRegistrationProof_ZeroDelegatorIdParam(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/brokerRegistration")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	// Delegator with zero idParam should use zero-filled path elements
	delegatorKeys := []core.KeyPair{makeKeyPair(10, 20)}
	delegatorProofs := []*core.MerkleProof{makeMerkleProof(merkleDepth)}

	result, err := client.BrokerRegistrationProof(
		big.NewInt(1), 0, big.NewInt(100),
		delegatorKeys, merkleDepth,
		[]*big.Int{big.NewInt(0)},
		delegatorProofs,
		[][]*big.Int{{big.NewInt(0)}}, // zero idParam
		big.NewInt(777), big.NewInt(0xABC), big.NewInt(0),
		makeMerkleProof(merkleDepth), big.NewInt(5), big.NewInt(10),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NumberOfInputs != 1 {
		t.Errorf("expected 1 input, got %d", result.NumberOfInputs)
	}
}

// --- Erc20WithBrokerV1Proof Tests ---
// NOTE: Erc20WithBrokerV1Proof is unsupported because the gnark server does not
// have the erc20JoinSplitWithBrokerV1 circuit. These tests verify it returns an error.

func TestErc20WithBrokerV1Proof_ReturnsUnsupportedError(t *testing.T) {
	client := core.NewGnarkClient("http://localhost:9999")
	merkleDepth := 8

	keysIn := []core.KeyPair{makeKeyPair(10, 20), makeKeyPair(30, 40)}
	keysOut := []core.KeyPair{makeKeyPair(50, 60), makeKeyPair(70, 80)}

	_, err := client.Erc20WithBrokerV1Proof(
		big.NewInt(1),
		[]*big.Int{big.NewInt(100), big.NewInt(200)},
		keysIn,
		[]*big.Int{big.NewInt(150), big.NewInt(150)},
		keysOut,
		merkleDepth,
		[]*core.MerkleProof{makeMerkleProof(merkleDepth), makeMerkleProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		big.NewInt(0xABC),
		big.NewInt(777),
	)
	if err == nil {
		t.Fatal("expected error for unsupported Erc20WithBrokerV1Proof")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected 'not supported' in error, got: %v", err)
	}
}

// --- Erc1155FungibleWithBrokerV1Proof Tests ---

func TestErc1155FungibleWithBrokerV1Proof_Success(t *testing.T) {
	server := newAuctionMockServer(t, "/proof/erc1155FungibleWithBroker")
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	keysIn := []core.KeyPair{makeKeyPair(10, 20), makeKeyPair(30, 40)}
	keysOut := []core.KeyPair{makeKeyPair(50, 60), makeKeyPair(70, 80)}

	result, err := client.Erc1155FungibleWithBrokerV1Proof(
		big.NewInt(1),                                    // message
		[]*big.Int{big.NewInt(100), big.NewInt(200)},     // valuesIn
		keysIn,
		[]*big.Int{big.NewInt(50), big.NewInt(50)},       // saltsIn
		[]*big.Int{big.NewInt(150), big.NewInt(150)},     // valuesOut
		keysOut,
		merkleDepth,
		[]*big.Int{big.NewInt(0), big.NewInt(0)},         // treeNumbers
		[]*core.MerkleProof{makeMerkleProof(merkleDepth), makeMerkleProof(merkleDepth)},
		big.NewInt(0xDEF),                                 // erc1155ContractAddress
		big.NewInt(42),                                    // erc1155TokenId
		big.NewInt(0),                                     // stAssetGroupTreeNumber
		makeMerkleProof(merkleDepth),                      // assetGroupMerkleProof
		big.NewInt(777),                                   // brokerPublicKey
		big.NewInt(5),                                     // stBrokerCommissionRate
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NumberOfInputs != 2 || result.NumberOfOutputs != 2 {
		t.Errorf("expected 2/2, got %d/%d", result.NumberOfInputs, result.NumberOfOutputs)
	}
	// Statement: message + treeNumbers(2) + merkleRoots(2) + nullifiers(2) + commitmentsOut(2) +
	//   blindedPK + commissionRate + assetGroupTreeNumber + assetGroupMerkleRoot
	// = 1 + 2 + 2 + 2 + 2 + 4 = 13
	if len(result.Statement) != 13 {
		t.Errorf("expected 13 statement elements, got %d", len(result.Statement))
	}
}

// --- Error Handling Tests ---

func TestAuctionInitProof_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := core.NewGnarkClient(server.URL)
	merkleDepth := 8

	_, err := client.AuctionInitProof(
		big.NewInt(1), big.NewInt(42), big.NewInt(0xABC),
		makeKeyPair(10, 20), merkleDepth, makeMerkleProof(merkleDepth),
		big.NewInt(999), big.NewInt(0), 0,
		big.NewInt(555), makeMerkleProof(merkleDepth),
		[]*big.Int{big.NewInt(100)},
	)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestLegitBrokerProof_ConnectionRefused(t *testing.T) {
	client := core.NewGnarkClient("http://localhost:1")
	_, err := client.LegitBrokerProof(big.NewInt(42), big.NewInt(123))
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

// --- Helper to create multiple merkle proofs ---

func MerkleProofPair(depth, count int) []*core.MerkleProof {
	proofs := make([]*core.MerkleProof, count)
	for i := range proofs {
		proofs[i] = makeMerkleProof(depth)
	}
	return proofs
}

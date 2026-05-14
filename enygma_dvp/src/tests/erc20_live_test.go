package tests

// Live integration test for the gnark server's /proof/joinSplitERC20 endpoint.
// Requires the gnark server to be running on localhost:8081.
// Run with: go test ./tests/... -run TestErc20JoinSplitProofLive -v

import (
	"math/big"
	"net"
	"testing"

	"github.com/raylsnetwork/enygma_dvp/src/core"
)

// serverAvailable checks if something is listening on the given addr.
func serverAvailable(addr string) bool {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// TestErc20JoinSplitProofLive generates a valid ERC20 V2 JoinSplit proof
// using the live gnark server. It:
//  1. Generates a BabyJubJub spend key pair for the input note holder
//  2. Computes input commitment: Poseidon(pk_spend, saltIn, amount, tokenId)
//  3. Inserts commitment into a depth-8 Merkle tree and generates the proof
//  4. Generates a recipient spend key and ML-KEM view key
//  5. Calls Erc20JoinSplitProof (which runs KEM encapsulation for output salts)
//  6. Verifies proof and public signals are returned
func TestErc20JoinSplitProofLive(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(0) // ERC20 uses tokenId=0

	// --- Input note 1: real value ---
	sk1, err := core.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField: %v", err)
	}
	pk1, err := core.GetPublicKey(sk1)
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	keyIn1 := core.KeyPair{PrivateKey: sk1, PublicKey: pk1}

	saltIn1, err := core.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField: %v", err)
	}

	amountIn1 := big.NewInt(100)

	commitIn1, err := core.Erc20CommitmentV2(pk1, saltIn1, amountIn1, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 input 1: %v", err)
	}

	// Build Merkle tree and insert input commitment
	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(commitIn1)

	proof1, err := mt.GenerateProof(commitIn1)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	// --- Input note 2: dummy (zero value) ---
	sk2, err := core.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField dummy: %v", err)
	}
	pk2, err := core.GetPublicKey(sk2)
	if err != nil {
		t.Fatalf("GetPublicKey dummy: %v", err)
	}
	keyIn2 := core.KeyPair{PrivateKey: sk2, PublicKey: pk2}

	saltIn2 := big.NewInt(0)
	amountIn2 := big.NewInt(0)

	// Dummy proof (zero path elements)
	proof2 := &core.MerkleProof{
		Element:  big.NewInt(0),
		Elements: make([]*big.Int, merkleDepth),
		Indices:  big.NewInt(0),
		Root:     big.NewInt(0),
	}
	for i := range proof2.Elements {
		proof2.Elements[i] = big.NewInt(0)
	}

	// --- Output note 1: recipient (100 tokens) ---
	skOut1, err := core.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField output1: %v", err)
	}
	pkOut1, err := core.GetPublicKey(skOut1)
	if err != nil {
		t.Fatalf("GetPublicKey output1: %v", err)
	}
	viewKP1, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("NewViewKeyPair output1: %v", err)
	}

	// --- Output note 2: dummy (0 tokens) — circuit requires exactly 2 outputs ---
	skOut2, err := core.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField output2: %v", err)
	}
	pkOut2, err := core.GetPublicKey(skOut2)
	if err != nil {
		t.Fatalf("GetPublicKey output2: %v", err)
	}
	viewKP2, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("NewViewKeyPair output2: %v", err)
	}

	// conservation: in=100+0, out=100+0
	amountOut1 := big.NewInt(100)
	amountOut2 := big.NewInt(0)

	// --- Generate proof ---
	result, err := client.Erc20JoinSplitProof(
		big.NewInt(1), // stMessage
		[]*big.Int{amountIn1, amountIn2},
		[]core.KeyPair{keyIn1, keyIn2},
		[]*big.Int{saltIn1, saltIn2},                    // wtSaltsIn
		[]*big.Int{amountOut1, amountOut2},              // wtValuesOut
		[]*big.Int{pkOut1, pkOut2},                      // recipientSpendPks
		[][]byte{viewKP1.EncapsKey, viewKP2.EncapsKey},  // recipientViewEncapKeys
		merkleDepth,
		[]*core.MerkleProof{proof1, proof2},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // stTreeNumbers
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProof: %v", err)
	}

	// Verify statement structure: [message, tree[0], root[0], null[0], tree[1], root[1], null[1], commit[0], commit[1]]
	// = 1 + 3*2 + 2 = 9
	expectedLen := 1 + 3*2 + 2
	if len(result.Statement) != expectedLen {
		t.Errorf("expected %d statement elements, got %d", expectedLen, len(result.Statement))
	}
	if result.Statement[0].Cmp(big.NewInt(1)) != 0 {
		t.Errorf("expected statement[0]=1 (message), got %s", result.Statement[0])
	}

	t.Logf("Proof generated successfully!")
	t.Logf("Statement: %v", result.Statement)
	t.Logf("CipherText len: %d bytes", len(result.CipherText))
	t.Logf("EncTxData len: %d bytes", len(result.EncTxData))

	// Only Bob's ciphertext is published; Alice's change uses a random salt stored locally.
	if len(result.CipherText) == 0 {
		t.Error("expected non-empty CipherText (Bob's ML-KEM capsule)")
	}
	if len(result.EncTxData) == 0 {
		t.Error("expected non-empty EncTxData (Bob's AES-GCM ciphertext)")
	}
	if result.SaltA == nil {
		t.Error("expected non-nil SaltA (Alice's random change salt)")
	}
}

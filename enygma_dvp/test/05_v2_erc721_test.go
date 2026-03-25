package tests

// End-to-end V2 ERC721 ownership proof test: Deposit → OwnershipProof (Alice→Bob) → re-prove (Bob→Carol).
// Non-interactive: recipients discover their notes via ML-KEM scan rather than out-of-band salt delivery.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc721_OwnershipProof -v

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc721_OwnershipProof(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(9999)
	contractAddress := big.NewInt(0xC0FFEE)

	// ── Step 1: Alice deposits ─────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (salt): %v", err)
	}

	aliceCommitment, err := core.Erc721Commitment(tokenId, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc721Commitment (Alice input): %v", err)
	}
	t.Logf("Step 1 — Alice's input commitment: %s", aliceCommitment)

	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(aliceCommitment)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice's commitment: %v", err)
	}

	// ── Step 2: Alice proves ownership to Bob ─────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	aliceResult, err := client.Erc721OwnershipProof(
		big.NewInt(1),
		tokenId,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0),
		contractAddress,
	)
	if err != nil {
		t.Fatalf("Erc721OwnershipProof (Alice→Bob): %v", err)
	}

	const expectedStatementLen = 5
	if len(aliceResult.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(aliceResult.Statement), expectedStatementLen)
	}

	bobCommitmentOnChain := aliceResult.Statement[4]
	t.Logf("Step 2 — Bob's on-chain commitment: %s", bobCommitmentOnChain)

	// ── Step 3: Verify nullifier and Bob's note via scan ──────────────────
	expectedNullifier, err := core.GetNullifier(aliceSpend.PrivateKey, aliceProof.Indices)
	if err != nil {
		t.Fatalf("GetNullifier: %v", err)
	}
	if aliceResult.Statement[3].Cmp(expectedNullifier) != 0 {
		t.Errorf("nullifier mismatch: got %s, want %s", aliceResult.Statement[3], expectedNullifier)
	}
	t.Logf("Step 3a — Nullifier verified: %s", expectedNullifier)

	// Bob scans for his note using his view key (non-interactive delivery)
	bobEvents := []core.OnChainErc721Event{{
		Commitment:   bobCommitmentOnChain,
		CiphertextI:  aliceResult.CiphertextI[0],
		CiphertextII: aliceResult.CiphertextII[0],
	}}
	bobNotes, err := core.ScanForErc721Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc721Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob: expected 1 note, got %d", len(bobNotes))
	}
	bobSalt := bobNotes[0].SaltBField
	t.Logf("Step 3b — Bob scanned his note (salt=%s)", bobSalt)

	// ── Step 4: Bob re-proves ownership to Carol ───────────────────────────
	mt.InsertLeaf(bobCommitmentOnChain)
	bobProof, err := mt.GenerateProof(bobCommitmentOnChain)
	if err != nil {
		t.Fatalf("GenerateProof for Bob's commitment: %v", err)
	}

	carolSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Carol NewSpendKeyPair: %v", err)
	}
	carolView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Carol NewViewKeyPair: %v", err)
	}

	bobResult, err := client.Erc721OwnershipProof(
		big.NewInt(2),
		tokenId,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobSalt,
		core.KeyPair{PrivateKey: carolSpend.PrivateKey, PublicKey: carolSpend.PublicKey},
		carolView.EncapsKey,
		merkleDepth,
		bobProof,
		big.NewInt(0),
		contractAddress,
	)
	if err != nil {
		t.Fatalf("Erc721OwnershipProof (Bob→Carol): %v", err)
	}

	if len(bobResult.Statement) != expectedStatementLen {
		t.Errorf("Bob→Carol statement length: got %d, want %d", len(bobResult.Statement), expectedStatementLen)
	}

	carolCommitmentOnChain := bobResult.Statement[4]
	t.Logf("Step 4 — Carol's on-chain commitment: %s", carolCommitmentOnChain)

	bobNullifier := bobResult.Statement[3]
	if bobNullifier.Cmp(expectedNullifier) == 0 {
		t.Errorf("Bob's nullifier should differ from Alice's")
	}
	t.Logf("Step 4 — Bob's nullifier: %s", bobNullifier)

	if carolCommitmentOnChain.Cmp(bobCommitmentOnChain) == 0 {
		t.Errorf("Carol's commitment should differ from Bob's")
	}

	// Carol scans for her note
	carolEvents := []core.OnChainErc721Event{{
		Commitment:   carolCommitmentOnChain,
		CiphertextI:  bobResult.CiphertextI[0],
		CiphertextII: bobResult.CiphertextII[0],
	}}
	carolNotes, err := core.ScanForErc721Notes(carolView.DecapsKey, carolSpend.PublicKey, carolEvents)
	if err != nil {
		t.Fatalf("ScanForErc721Notes (Carol): %v", err)
	}
	if len(carolNotes) != 1 {
		t.Fatalf("Carol: expected 1 note, got %d", len(carolNotes))
	}
	t.Logf("Step 4 — Carol scanned her note (salt=%s)", carolNotes[0].SaltBField)
}

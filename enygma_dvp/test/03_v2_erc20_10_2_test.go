package tests

// V2 ERC20 consolidation test using the 10-input / 2-output circuit.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc20_10_2_Consolidation -v

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

// TestV2Erc20_10_2_Consolidation verifies that multiple small notes can be
// consolidated into a single larger note using the 10-input / 2-output circuit:
//
//	Step 1 — Bob computes 5 deposit commitments (10 tokens each, total 50)
//	  Each note uses a fresh spend key pair and random salt.
//	  Commitments are computed locally (Erc20CommitmentV2) and inserted into a
//	  shared Merkle tree — no prover call needed for the deposits themselves.
//
//	Step 2 — Bob consolidates all 5 notes into one (JoinSplit, 10-in / 2-out)
//	  Input slots 0–4 carry Bob's real notes (10 tokens each).
//	  Input slots 5–9 are dummy zero-value notes (circuit skips their Merkle check).
//	  Output 0 is addressed to Alice (50 tokens).
//	  Output 1 is a dummy zero-value output.
//
//	Step 3 — Alice scans and recovers her consolidated note
//	  Alice must find exactly one note with tokenId=0 and amount=50.
//
//	Step 4 — Alice verifies spend-readiness
//	  Recomputes Erc20CommitmentV2 from the scanned note and checks it matches
//	  the on-chain commitment from the statement.
func TestV2Erc20_10_2_Consolidation(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(0)          // ERC20: tokenId=0
	perNoteAmount := big.NewInt(10)   // 10 tokens per note
	numRealInputs := 5                // 5 real deposits
	totalAmount := big.NewInt(50)     // 5 * 10

	// -----------------------------------------------------------------------
	// Step 1: Bob computes 5 deposit commitments
	// -----------------------------------------------------------------------
	mt := core.NewMerkleTree(merkleDepth)

	bobSpends := make([]*core.SpendKeyPair, numRealInputs)
	bobSalts := make([]*big.Int, numRealInputs)
	bobCommitments := make([]*big.Int, numRealInputs)

	for i := 0; i < numRealInputs; i++ {
		sp, err := core.NewSpendKeyPair()
		if err != nil {
			t.Fatalf("bob[%d] NewSpendKeyPair: %v", i, err)
		}
		bobSpends[i] = sp

		salt, err := core.RandomInField()
		if err != nil {
			t.Fatalf("bob[%d] RandomInField (salt): %v", i, err)
		}
		bobSalts[i] = salt

		cmt, err := core.Erc20CommitmentV2(sp.PublicKey, salt, perNoteAmount, tokenId)
		if err != nil {
			t.Fatalf("bob[%d] Erc20CommitmentV2: %v", i, err)
		}
		bobCommitments[i] = cmt
		mt.InsertLeaf(cmt)
		t.Logf("Step 1 — Bob deposit[%d] commitment: %s", i, cmt)
	}

	// Generate Merkle proofs for the 5 real notes.
	bobProofs := make([]*core.MerkleProof, numRealInputs)
	for i := 0; i < numRealInputs; i++ {
		proof, err := mt.GenerateProof(bobCommitments[i])
		if err != nil {
			t.Fatalf("GenerateProof bob[%d]: %v", i, err)
		}
		bobProofs[i] = proof
	}

	// -----------------------------------------------------------------------
	// Step 2: Bob consolidates (JoinSplit, 10-in / 2-out)
	// -----------------------------------------------------------------------
	// The 10-input circuit requires exactly 10 input slots.
	// Slots 0–4: Bob's real notes. Slots 5–9: dummy zero-value notes.
	const totalInputs = 10

	wtValuesIn := make([]*big.Int, totalInputs)
	keysIn := make([]core.KeyPair, totalInputs)
	wtSaltsIn := make([]*big.Int, totalInputs)
	merkleProofs := make([]*core.MerkleProof, totalInputs)
	stTreeNumbers := make([]*big.Int, totalInputs)

	for i := 0; i < numRealInputs; i++ {
		wtValuesIn[i] = new(big.Int).Set(perNoteAmount)
		keysIn[i] = core.KeyPair{
			PrivateKey: bobSpends[i].PrivateKey,
			PublicKey:  bobSpends[i].PublicKey,
		}
		wtSaltsIn[i] = bobSalts[i]
		merkleProofs[i] = bobProofs[i]
		stTreeNumbers[i] = big.NewInt(0)
	}

	// Fill dummy input slots 5–9.
	for i := numRealInputs; i < totalInputs; i++ {
		dummySpend, err := core.NewSpendKeyPair()
		if err != nil {
			t.Fatalf("dummy[%d] NewSpendKeyPair: %v", i, err)
		}
		wtValuesIn[i] = big.NewInt(0)
		keysIn[i] = core.KeyPair{
			PrivateKey: dummySpend.PrivateKey,
			PublicKey:  dummySpend.PublicKey,
		}
		wtSaltsIn[i] = big.NewInt(0)
		merkleProofs[i] = makeDummyProof(merkleDepth)
		stTreeNumbers[i] = big.NewInt(0)
	}

	// Alice receives the consolidated 50 tokens.
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}

	// Dummy second output (zero value).
	dummyOutSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummyOutSpend NewSpendKeyPair: %v", err)
	}
	dummyOutView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyOutView NewViewKeyPair: %v", err)
	}

	consolidateResult, err := client.Erc20JoinSplitProof(
		big.NewInt(1),
		wtValuesIn,
		keysIn,
		wtSaltsIn,
		[]*big.Int{totalAmount, big.NewInt(0)},                        // wtValuesOut
		[]*big.Int{aliceSpend.PublicKey, dummyOutSpend.PublicKey},     // recipientSpendPks
		[][]byte{aliceView.EncapsKey, dummyOutView.EncapsKey},         // recipientViewEncapKeys
		merkleDepth,
		merkleProofs,
		stTreeNumbers,
		tokenId,
		true, // use the 10-in / 2-out circuit
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProof (10-in): %v", err)
	}

	// Statement layout (10-in / 2-out):
	//   [msg, tree0, root0, null0, …, tree9, root9, null9, cmt0, cmt1]
	//   = 1 + 3*10 + 2 = 33 elements
	expectedStatementLen := 1 + 3*totalInputs + 2
	if len(consolidateResult.Statement) != expectedStatementLen {
		t.Errorf("statement: expected %d elements, got %d", expectedStatementLen, len(consolidateResult.Statement))
	}

	// Alice's commitment is the first output → index 1 + 3*10 = 31.
	aliceCommitmentIdx := 1 + 3*totalInputs
	aliceCommitmentOnChain := consolidateResult.Statement[aliceCommitmentIdx]
	t.Logf("Step 2 — Alice's consolidated commitment (index %d): %s", aliceCommitmentIdx, aliceCommitmentOnChain)

	if len(consolidateResult.CiphertextI) != 2 || len(consolidateResult.CiphertextII) != 2 {
		t.Errorf("expected 2 ciphertext pairs, got I=%d II=%d",
			len(consolidateResult.CiphertextI), len(consolidateResult.CiphertextII))
	}

	// -----------------------------------------------------------------------
	// Step 3: Alice scans
	// -----------------------------------------------------------------------
	events := []core.OnChainErc20Event{
		{
			Commitment:   consolidateResult.Statement[aliceCommitmentIdx],
			CiphertextI:  consolidateResult.CiphertextI[0],
			CiphertextII: consolidateResult.CiphertextII[0],
		},
		{
			Commitment:   consolidateResult.Statement[aliceCommitmentIdx+1],
			CiphertextI:  consolidateResult.CiphertextI[1],
			CiphertextII: consolidateResult.CiphertextII[1],
		},
	}

	aliceNotes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, events)
	if err != nil {
		t.Fatalf("Alice ScanForErc20Notes: %v", err)
	}
	if len(aliceNotes) != 1 {
		t.Fatalf("Alice expected 1 owned note, got %d", len(aliceNotes))
	}
	aliceNote := aliceNotes[0]
	t.Logf("Step 3 — Alice recovered note: tokenId=%s amount=%s", aliceNote.TokenId, aliceNote.Amount)

	if aliceNote.TokenId.Cmp(tokenId) != 0 {
		t.Errorf("tokenId: got %s, want %s", aliceNote.TokenId, tokenId)
	}
	if aliceNote.Amount.Cmp(totalAmount) != 0 {
		t.Errorf("amount: got %s, want %s", aliceNote.Amount, totalAmount)
	}
	if aliceNote.Commitment.Cmp(aliceCommitmentOnChain) != 0 {
		t.Errorf("commitment: got %s, want %s", aliceNote.Commitment, aliceCommitmentOnChain)
	}

	// -----------------------------------------------------------------------
	// Step 4: Alice verifies spend-readiness
	// -----------------------------------------------------------------------
	recomputed, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceNote.SaltBField, aliceNote.Amount, aliceNote.TokenId)
	if err != nil {
		t.Fatalf("Alice Erc20CommitmentV2 recompute: %v", err)
	}
	if recomputed.Cmp(aliceNote.Commitment) != 0 {
		t.Errorf("Alice spend-readiness check failed: recomputed %s != on-chain %s", recomputed, aliceNote.Commitment)
	}
	t.Logf("Step 4 — Alice's consolidated note is spend-ready. Commitment: %s", recomputed)
	t.Logf("Alice's WtSaltsIn for future JoinSplit: %s", aliceNote.SaltBField)
}

package tests

// Three-hop V2 ERC20 transfer chain: Alice → Bob → Carol → Withdraw.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc20_ChainOfTransfers_AliceBobCarol -v

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

// TestV2Erc20_ChainOfTransfers_AliceBobCarol exercises a three-hop transfer chain
// to verify that each recipient can scan, spend-verify, and re-transfer a note:
//
//	Step 1 — Alice mints 30 tokens (PrivateMint)
//	Step 2 — Alice → Bob  (JoinSplit, 2-in / 2-out)
//	Step 3 — Bob scans; verifies he owns 30 tokens
//	Step 4 — Bob → Carol  (JoinSplit, 2-in / 2-out using Bob's recovered SaltBField)
//	Step 5 — Carol scans; verifies she owns 30 tokens
//	Step 6 — Carol withdraws to a recipient address
func TestV2Erc20_ChainOfTransfers_AliceBobCarol(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(0)   // ERC20: tokenId=0
	amount := big.NewInt(30)   // 30 tokens passed along the chain

	// Shared Merkle tree — each output commitment is inserted so the next sender
	// can generate a valid inclusion proof.
	mt := core.NewMerkleTree(merkleDepth)

	// -----------------------------------------------------------------------
	// Step 1: Alice mints 30 tokens
	// -----------------------------------------------------------------------
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (salt): %v", err)
	}

	mintResult, err := client.Erc20PrivateMintProof(
		aliceSpend.PublicKey,
		aliceSalt,
		amount,
		tokenId,
		big.NewInt(0xC0FFEE),
	)
	if err != nil {
		t.Fatalf("Erc20PrivateMintProof: %v", err)
	}
	t.Logf("Step 1 — Alice minted commitment: %s", mintResult.Commitment)

	mt.InsertLeaf(mintResult.Commitment)
	aliceProof, err := mt.GenerateProof(mintResult.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof (Alice mint): %v", err)
	}

	// -----------------------------------------------------------------------
	// Step 2: Alice → Bob (JoinSplit, 2-in / 2-out)
	// -----------------------------------------------------------------------
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	dummySpend1, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummy1 NewSpendKeyPair: %v", err)
	}
	dummyView1, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummy1 NewViewKeyPair: %v", err)
	}

	aliceToBobResult, err := client.Erc20JoinSplitProof(
		big.NewInt(1),
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend1.PrivateKey, PublicKey: dummySpend1.PublicKey},
		},
		[]*big.Int{mintResult.Salt, big.NewInt(0)},
		[]*big.Int{amount, big.NewInt(0)},
		[]*big.Int{bobSpend.PublicKey, dummySpend1.PublicKey},
		[][]byte{bobView.EncapsKey, dummyView1.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Alice→Bob Erc20JoinSplitProof: %v", err)
	}

	// Statement: [msg, tree0, root0, null0, tree1, root1, null1, cmt0, cmt1]
	expectedLen := 1 + 3*2 + 2
	if len(aliceToBobResult.Statement) != expectedLen {
		t.Errorf("Alice→Bob statement: expected %d elements, got %d", expectedLen, len(aliceToBobResult.Statement))
	}

	bobCommitment := aliceToBobResult.Statement[7]
	t.Logf("Step 2 — Bob's commitment: %s", bobCommitment)
	mt.InsertLeaf(bobCommitment)

	// -----------------------------------------------------------------------
	// Step 3: Bob scans
	// -----------------------------------------------------------------------
	events1 := []core.OnChainErc20Event{
		{
			Commitment:   aliceToBobResult.Statement[7],
			CiphertextI:  aliceToBobResult.CiphertextI[0],
			CiphertextII: aliceToBobResult.CiphertextII[0],
		},
		{
			Commitment:   aliceToBobResult.Statement[8],
			CiphertextI:  aliceToBobResult.CiphertextI[1],
			CiphertextII: aliceToBobResult.CiphertextII[1],
		},
	}

	bobNotes, err := core.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, events1)
	if err != nil {
		t.Fatalf("Bob ScanForErc20Notes: %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 owned note, got %d", len(bobNotes))
	}
	bobNote := bobNotes[0]
	t.Logf("Step 3 — Bob recovered note: tokenId=%s amount=%s", bobNote.TokenId, bobNote.Amount)

	if bobNote.Amount.Cmp(amount) != 0 {
		t.Errorf("Bob amount: got %s, want %s", bobNote.Amount, amount)
	}
	if bobNote.Commitment.Cmp(bobCommitment) != 0 {
		t.Errorf("Bob commitment mismatch")
	}

	// Spend-readiness check for Bob
	bobRecomputed, err := core.Erc20CommitmentV2(bobSpend.PublicKey, bobNote.SaltBField, bobNote.Amount, bobNote.TokenId)
	if err != nil {
		t.Fatalf("Bob Erc20CommitmentV2 recompute: %v", err)
	}
	if bobRecomputed.Cmp(bobNote.Commitment) != 0 {
		t.Errorf("Bob spend-readiness check failed")
	}

	// -----------------------------------------------------------------------
	// Step 4: Bob → Carol (JoinSplit, 2-in / 2-out)
	// -----------------------------------------------------------------------
	carolSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Carol NewSpendKeyPair: %v", err)
	}
	carolView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Carol NewViewKeyPair: %v", err)
	}
	dummySpend2, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummy2 NewSpendKeyPair: %v", err)
	}
	dummyView2, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummy2 NewViewKeyPair: %v", err)
	}

	bobProof, err := mt.GenerateProof(bobNote.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof (Bob's note): %v", err)
	}

	bobToCarolResult, err := client.Erc20JoinSplitProof(
		big.NewInt(2),
		[]*big.Int{bobNote.Amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend2.PrivateKey, PublicKey: dummySpend2.PublicKey},
		},
		[]*big.Int{bobNote.SaltBField, big.NewInt(0)}, // SaltBField is Bob's wtSaltsIn
		[]*big.Int{amount, big.NewInt(0)},
		[]*big.Int{carolSpend.PublicKey, dummySpend2.PublicKey},
		[][]byte{carolView.EncapsKey, dummyView2.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Bob→Carol Erc20JoinSplitProof: %v", err)
	}

	if len(bobToCarolResult.Statement) != expectedLen {
		t.Errorf("Bob→Carol statement: expected %d elements, got %d", expectedLen, len(bobToCarolResult.Statement))
	}

	carolCommitment := bobToCarolResult.Statement[7]
	t.Logf("Step 4 — Carol's commitment: %s", carolCommitment)
	mt.InsertLeaf(carolCommitment)

	// -----------------------------------------------------------------------
	// Step 5: Carol scans
	// -----------------------------------------------------------------------
	events2 := []core.OnChainErc20Event{
		{
			Commitment:   bobToCarolResult.Statement[7],
			CiphertextI:  bobToCarolResult.CiphertextI[0],
			CiphertextII: bobToCarolResult.CiphertextII[0],
		},
		{
			Commitment:   bobToCarolResult.Statement[8],
			CiphertextI:  bobToCarolResult.CiphertextI[1],
			CiphertextII: bobToCarolResult.CiphertextII[1],
		},
	}

	carolNotes, err := core.ScanForErc20Notes(carolView.DecapsKey, carolSpend.PublicKey, events2)
	if err != nil {
		t.Fatalf("Carol ScanForErc20Notes: %v", err)
	}
	if len(carolNotes) != 1 {
		t.Fatalf("Carol expected 1 owned note, got %d", len(carolNotes))
	}
	carolNote := carolNotes[0]
	t.Logf("Step 5 — Carol recovered note: tokenId=%s amount=%s", carolNote.TokenId, carolNote.Amount)

	if carolNote.Amount.Cmp(amount) != 0 {
		t.Errorf("Carol amount: got %s, want %s", carolNote.Amount, amount)
	}
	if carolNote.Commitment.Cmp(carolCommitment) != 0 {
		t.Errorf("Carol commitment mismatch")
	}

	// Spend-readiness check for Carol
	carolRecomputed, err := core.Erc20CommitmentV2(carolSpend.PublicKey, carolNote.SaltBField, carolNote.Amount, carolNote.TokenId)
	if err != nil {
		t.Fatalf("Carol Erc20CommitmentV2 recompute: %v", err)
	}
	if carolRecomputed.Cmp(carolNote.Commitment) != 0 {
		t.Errorf("Carol spend-readiness check failed")
	}
	t.Logf("Step 5 — Carol's note is spend-ready")

	// -----------------------------------------------------------------------
	// Step 6: Carol withdraws
	// -----------------------------------------------------------------------
	carolProof, err := mt.GenerateProof(carolNote.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof (Carol's note): %v", err)
	}

	dummyWithdrawSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummyWithdrawSpend NewSpendKeyPair: %v", err)
	}

	recipientAddr := big.NewInt(0xCAFEBABE)

	withdrawResult, err := client.Erc20WithdrawProof(
		big.NewInt(3),
		[]*big.Int{carolNote.Amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: carolSpend.PrivateKey, PublicKey: carolSpend.PublicKey},
			{PrivateKey: dummyWithdrawSpend.PrivateKey, PublicKey: dummyWithdrawSpend.PublicKey},
		},
		[]*big.Int{carolNote.SaltBField, big.NewInt(0)},
		carolNote.Amount,
		recipientAddr,
		dummyWithdrawSpend.PublicKey,
		merkleDepth,
		[]*core.MerkleProof{carolProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Carol Erc20WithdrawProof: %v", err)
	}

	if len(withdrawResult.Statement) != expectedLen {
		t.Errorf("Carol withdraw statement: expected %d elements, got %d", expectedLen, len(withdrawResult.Statement))
	}

	// Withdrawal commitment: Poseidon(recipient, 0, amount, tokenId)
	expectedWithdrawCommit, err := core.Erc20CommitmentV2(recipientAddr, big.NewInt(0), amount, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 (withdraw verify): %v", err)
	}
	if withdrawResult.Statement[7].Cmp(expectedWithdrawCommit) != 0 {
		t.Errorf("Carol withdrawal commitment mismatch: got %s, want %s",
			withdrawResult.Statement[7], expectedWithdrawCommit)
	}
	t.Logf("Step 6 — Carol's withdrawal commitment verified: %s", expectedWithdrawCommit)
}

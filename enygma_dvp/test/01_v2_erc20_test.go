package tests



import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

// TestV2Erc20_DepositTransferWithdraw exercises the complete V2 non-interactive flow:
//
//	Step 1 — Alice mints (PrivateMint)
//	  Alice generates a spend key pair and a random salt, then calls Erc20PrivateMintProof.
//	  The resulting commitment is inserted into the local Merkle tree.
//
//	Step 2 — Alice transfers to Bob (JoinSplit, 2-in / 2-out)
//	  Alice consumes her minted note as input 0; input 1 is a dummy (zero value).
//	  Output 0 is addressed to Bob's spend key + ML-KEM view key.
//	  Output 1 is a dummy zero-value output to satisfy the 2-output circuit requirement.
//
//	Step 3 — Bob scans on-chain events
//	  Bob calls ScanForErc20Notes and must recover exactly one note with the correct
//	  tokenId, amount, and commitment.
//
//	Step 4 — Bob verifies spend-readiness
//	  Bob recomputes Erc20CommitmentV2(pkSpend, saltBField, amount, tokenId) from the
//	  scanned note and confirms it matches the on-chain commitment.
//
//	Step 5 — Bob withdraws (JoinSplit with withdrawal output)
//	  Bob calls Erc20WithdrawProof.  The first circuit output encodes the recipient
//	  address as pk_spend with salt=0: Poseidon(recipient, 0, amount, tokenId).
//	  The test verifies the statement carries the expected withdrawal commitment.
func TestV2Erc20_DepositTransferWithdraw(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(0)  // ERC20: tokenId=0
	amount := big.NewInt(50)  // 50 tokens

	// -----------------------------------------------------------------------
	// Step 1: Alice mints
	// -----------------------------------------------------------------------
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (salt): %v", err)
	}

	contractAddress := big.NewInt(0xC0FFEE) // placeholder ERC20 contract address
	mintResult, err := client.Erc20PrivateMintProof(
		aliceSpend.PublicKey,
		aliceSalt,
		amount,
		tokenId,
		contractAddress,
	)
	if err != nil {
		t.Fatalf("Erc20PrivateMintProof: %v", err)
	}
	t.Logf("Step 1 — Alice minted commitment: %s", mintResult.Commitment)

	// Build local Merkle tree for Alice's minted note.
	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(mintResult.Commitment)
	aliceProof, err := mt.GenerateProof(mintResult.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice's minted note: %v", err)
	}

	// -----------------------------------------------------------------------
	// Step 2: Alice JoinSplits → Bob (2-in / 2-out)
	// -----------------------------------------------------------------------
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	// Dummy second input (zero value) and dummy second output view key.
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummy NewSpendKeyPair: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummy NewViewKeyPair: %v", err)
	}

	joinSplitResult, err := client.Erc20JoinSplitProof(
		big.NewInt(1), // stMessage
		[]*big.Int{amount, big.NewInt(0)},  // wtValuesIn
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{mintResult.Salt, big.NewInt(0)},  // wtSaltsIn
		[]*big.Int{amount, big.NewInt(0)},            // wtValuesOut
		[]*big.Int{bobSpend.PublicKey, dummySpend.PublicKey}, // recipientSpendPks
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},     // recipientViewEncapKeys
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // stTreeNumbers
		tokenId,
		false, // 2-in / 2-out circuit
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProof: %v", err)
	}

	// Statement layout (2-in / 2-out):
	//   [msg, tree0, root0, null0, tree1, root1, null1, cmt0, cmt1]  → 9 elements
	expectedStatementLen := 1 + 3*2 + 2
	if len(joinSplitResult.Statement) != expectedStatementLen {
		t.Errorf("expected %d statement elements, got %d", expectedStatementLen, len(joinSplitResult.Statement))
	}

	// Bob's output commitment is at index 7 (first output).
	bobCommitmentOnChain := joinSplitResult.Statement[7]
	t.Logf("Step 2 — Bob's on-chain commitment: %s", bobCommitmentOnChain)

	// Insert Bob's commitment into the tree so he can later prove inclusion.
	mt.InsertLeaf(bobCommitmentOnChain)

	// -----------------------------------------------------------------------
	// Step 3: Bob scans on-chain events
	// -----------------------------------------------------------------------
	events := []core.OnChainErc20Event{
		{
			Commitment:   joinSplitResult.Statement[7], // Bob's output
			CiphertextI:  joinSplitResult.CiphertextI[0],
			CiphertextII: joinSplitResult.CiphertextII[0],
		},
		{
			Commitment:   joinSplitResult.Statement[8], // dummy output
			CiphertextI:  joinSplitResult.CiphertextI[1],
			CiphertextII: joinSplitResult.CiphertextII[1],
		},
	}

	ownedNotes, err := core.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, events)
	if err != nil {
		t.Fatalf("ScanForErc20Notes: %v", err)
	}
	if len(ownedNotes) != 1 {
		t.Fatalf("Bob expected 1 owned note, got %d", len(ownedNotes))
	}
	note := ownedNotes[0]
	t.Logf("Step 3 — Bob recovered note: tokenId=%s amount=%s", note.TokenId, note.Amount)

	if note.TokenId.Cmp(tokenId) != 0 {
		t.Errorf("tokenId: got %s, want %s", note.TokenId, tokenId)
	}
	if note.Amount.Cmp(amount) != 0 {
		t.Errorf("amount: got %s, want %s", note.Amount, amount)
	}
	if note.Commitment.Cmp(bobCommitmentOnChain) != 0 {
		t.Errorf("commitment: got %s, want %s", note.Commitment, bobCommitmentOnChain)
	}

	// -----------------------------------------------------------------------
	// Step 4: Bob verifies spend-readiness (commitment round-trip)
	// -----------------------------------------------------------------------
	recomputed, err := core.Erc20CommitmentV2(bobSpend.PublicKey, note.SaltBField, note.Amount, note.TokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 recompute: %v", err)
	}
	if recomputed.Cmp(note.Commitment) != 0 {
		t.Errorf("spend-readiness check failed: recomputed %s != on-chain %s", recomputed, note.Commitment)
	}
	t.Logf("Step 4 — Bob's note is spend-ready. Commitment: %s", recomputed)

	// -----------------------------------------------------------------------
	// Step 5: Bob withdraws to a recipient address
	// -----------------------------------------------------------------------
	bobProof, err := mt.GenerateProof(note.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof for Bob's note: %v", err)
	}

	// Dummy second input key pair (zero-value input for the 2-in circuit).
	dummyWithdrawSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummyWithdrawSpend NewSpendKeyPair: %v", err)
	}

	recipientAddr := big.NewInt(0xDEADBEEF) // uint160 recipient address

	withdrawResult, err := client.Erc20WithdrawProof(
		big.NewInt(1), // stMessage
		[]*big.Int{note.Amount, big.NewInt(0)}, // wtValuesIn
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummyWithdrawSpend.PrivateKey, PublicKey: dummyWithdrawSpend.PublicKey},
		},
		[]*big.Int{note.SaltBField, big.NewInt(0)}, // wtSaltsIn
		note.Amount,                                  // withdrawAmount
		recipientAddr,                                // recipient uint160
		dummyWithdrawSpend.PublicKey,                 // dummySpendPk for second output
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // stTreeNumbers
		tokenId,
		false, // 2-in circuit
	)
	if err != nil {
		t.Fatalf("Erc20WithdrawProof: %v", err)
	}

	// Statement layout (2-in, 2-out):
	//   [msg, tree0, root0, null0, tree1, root1, null1, withdrawCmt, dummyCmt]  → 9 elements
	if len(withdrawResult.Statement) != expectedStatementLen {
		t.Errorf("withdraw statement: expected %d elements, got %d", expectedStatementLen, len(withdrawResult.Statement))
	}

	// Verify the withdrawal commitment: Poseidon(recipient, 0, amount, tokenId)
	expectedWithdrawCommit, err := core.Erc20CommitmentV2(recipientAddr, big.NewInt(0), amount, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 withdraw: %v", err)
	}
	if withdrawResult.Statement[7].Cmp(expectedWithdrawCommit) != 0 {
		t.Errorf("withdrawal commitment mismatch: got %s, want %s",
			withdrawResult.Statement[7], expectedWithdrawCommit)
	}
	t.Logf("Step 5 — Bob's withdrawal commitment verified: %s", expectedWithdrawCommit)
}

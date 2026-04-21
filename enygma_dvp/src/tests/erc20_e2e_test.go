package tests

// End-to-end integration test for the complete non-interactive ERC20 swap flow.
// Requires the gnark server to be running on localhost:8081.
// Run with: go test ./tests/... -run TestErc20Swap_E2E -v

import (
	"math/big"
	"testing"

	"github.com/raylsnetwork/enygma_dvp/src/core"
)

// TestErc20Swap_E2E exercises the full Alice→Bob swap:
//
//  Step 1 — Alice mints (PrivateMint)
//    Alice deposits 5 USDT (tokenId=10) into the vault.
//    She generates a fresh spend key pair and a random salt, then calls
//    Erc20PrivateMintProof. The resulting commitment is inserted into
//    the Merkle tree on-chain.
//
//  Step 2 — Alice sends to Bob (JoinSplit)
//    Alice consumes her minted note as an input and creates one output note
//    addressed to Bob (using Bob's spend public key + ML-KEM view key).
//    A second dummy output (zero value) satisfies the 2-output circuit requirement.
//    The proof is submitted on-chain together with (cipherText, encTxData)
//    for each output.
//
//  Step 3 — Bob scans
//    Bob calls ScanForErc20Notes with the on-chain events.
//    He must recover exactly one note with tokenId=10 and amount=5,
//    and the recovered note's commitment must match what Alice posted.
//
//  Step 4 — Bob verifies spend-readiness
//    Bob recomputes Erc20CommitmentV2(pkSpendB, saltB, amount, tokenId)
//    from the scan result and confirms it equals the on-chain commitment.
//    This is the same check the JoinSplit circuit performs on the input side,
//    so a match here means Bob can spend the note.
func TestErc20Swap_E2E(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId := big.NewInt(10)  // USDT token_id
	amount := big.NewInt(5)    // 5 USDT
	contractAddress := big.NewInt(0xC0FFEE) // placeholder ERC20 contract address

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

	// Alice's input note for the JoinSplit is the mint commitment.
	// Build a local Merkle tree to generate an inclusion proof.
	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(mintResult.Commitment)

	aliceProof, err := mt.GenerateProof(mintResult.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice's minted note: %v", err)
	}

	// -----------------------------------------------------------------------
	// Step 2: Alice sends to Bob (JoinSplit)
	// -----------------------------------------------------------------------
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	// Dummy second input (zero value — circuit requires exactly 2 inputs).
	aliceDummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice dummy NewSpendKeyPair: %v", err)
	}
	dummyProof := &core.MerkleProof{
		Element:  big.NewInt(0),
		Elements: make([]*big.Int, merkleDepth),
		Indices:  big.NewInt(0),
		Root:     big.NewInt(0),
	}
	for i := range dummyProof.Elements {
		dummyProof.Elements[i] = big.NewInt(0)
	}

	// Dummy second output view key (zero value output).
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummy NewViewKeyPair: %v", err)
	}

	joinSplitResult, err := client.Erc20JoinSplitProof(
		big.NewInt(1), // stMessage
		[]*big.Int{amount, big.NewInt(0)},                           // wtValuesIn
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: aliceDummySpend.PrivateKey, PublicKey: aliceDummySpend.PublicKey},
		},
		[]*big.Int{mintResult.Salt, big.NewInt(0)},                  // wtSaltsIn
		[]*big.Int{amount, big.NewInt(0)},                           // wtValuesOut
		[]*big.Int{bobSpend.PublicKey, aliceDummySpend.PublicKey},   // recipientSpendPks
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},            // recipientViewEncapKeys
		merkleDepth,
		[]*core.MerkleProof{aliceProof, dummyProof},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},                    // stTreeNumbers
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProof: %v", err)
	}

	// Verify statement structure: [message, tree[0], root[0], null[0], tree[1], root[1], null[1], commit[0], commit[1]]
	expectedStatementLen := 1 + 3*2 + 2
	if len(joinSplitResult.Statement) != expectedStatementLen {
		t.Errorf("expected %d statement elements, got %d", expectedStatementLen, len(joinSplitResult.Statement))
	}
	if len(joinSplitResult.CipherText) != 2 || len(joinSplitResult.EncTxData) != 2 {
		t.Errorf("expected 2 ciphertext pairs, got I=%d II=%d",
			len(joinSplitResult.CipherText), len(joinSplitResult.EncTxData))
	}

	// The on-chain commitment for Bob's note is at stCommitmentsOut[0].
	// In the statement: [msg, tree0, root0, null0, tree1, root1, null1, commit0, commit1]
	// commit0 is at index 7.
	bobCommitmentOnChain := joinSplitResult.Statement[7]
	t.Logf("Step 2 — Bob's on-chain commitment: %s", bobCommitmentOnChain)

	// -----------------------------------------------------------------------
	// Step 3: Bob scans the on-chain events
	// -----------------------------------------------------------------------
	// Bob sees all output events. Each output note produces one (cipherText, encTxData, commitment) triple.
	events := []core.OnChainErc20Event{
		{
			Commitment:   joinSplitResult.Statement[7], // Bob's output
			CipherText:  joinSplitResult.CipherText[0],
			EncTxData: joinSplitResult.EncTxData[0],
		},
		{
			Commitment:   joinSplitResult.Statement[8], // dummy output
			CipherText:  joinSplitResult.CipherText[1],
			EncTxData: joinSplitResult.EncTxData[1],
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

	// Verify recovered values match what Alice sent
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
	// Step 4: Bob verifies he can spend the note (commitment round-trip)
	// -----------------------------------------------------------------------
	// The JoinSplit circuit checks: Erc20CommitmentV2(pkSpend, saltIn, amount, tokenId) == commitment
	// If this passes, Bob's (pkSpend, saltB, amount, tokenId) tuple is valid for spending.
	recomputed, err := core.Erc20CommitmentV2(bobSpend.PublicKey, note.SaltBField, note.Amount, note.TokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 recompute: %v", err)
	}
	if recomputed.Cmp(note.Commitment) != 0 {
		t.Errorf("spend-readiness check failed: recomputed %s != on-chain %s", recomputed, note.Commitment)
	}

	t.Logf("Step 4 — Bob's note is spend-ready. Commitment matches: %s", recomputed)
	t.Logf("Bob's WtSaltsIn for future JoinSplit: %s", note.SaltBField)
}

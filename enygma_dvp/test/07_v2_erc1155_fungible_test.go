package tests

// End-to-end V2 ERC1155 fungible transfer test.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc1155Fungible_Transfer -v -timeout 300s
//
// ERC1155 FUNGIBLE vs ERC20 — KEY DIFFERENCES
// ────────────────────────────────────────────
// ERC20 commitment:   Poseidon(pk_spend, saltBField, amount, tokenId=0)
// ERC1155 commitment: Poseidon(contractAddress, tokenId, amount, pk_spend, salt)
//   ↑ contractAddress and tokenId are baked directly into the commitment,
//     binding it to a specific ERC1155 token type.
//
// ASSET GROUP TREE
// ────────────────
// The ERC1155 circuit requires a second Merkle tree — the "asset group tree".
// Its leaves are token type IDs: Erc1155UniqueId(contractAddr, tokenId, 0).
// The circuit checks that the token being transferred is registered in this
// tree, preventing proofs over unregistered/fake token types.
//
// FLOW
// ────
//   Step 1 — Register token type
//     Compute uid = Erc1155UniqueId(contractAddr, tokenId, 0)
//     Insert uid into the asset group tree.
//
//   Step 2 — Alice deposits tokens
//     Commitment = Poseidon(contractAddr, tokenId, amount, alicePk, aliceSalt)
//     Insert into the fungible token Merkle tree.
//
//   Step 3 — Alice transfers to Bob (2-in / 2-out JoinSplit)
//     Input 0: Alice's real note (50 tokens)
//     Input 1: Dummy note (0 tokens, skips Merkle check)
//     Output 0: Bob's note (50 tokens)
//     Output 1: Dummy note (0 tokens)
//     Circuit enforces: sum(inputs) == sum(outputs)
//
//   Step 4 — Verify statement
//     Statement: [msg, tree0, root0, null0, tree1, root1, null1, cmt0, cmt1]
//     Nullifier for Alice's note: Poseidon(aliceSk, leafIndex)
//     Bob's note recovered via ML-KEM scan
//
//   Step 5 — Bob transfers to Carol (chain of custody)
//     Reuses Bob's note (from step 3) as input, sends to Carol.
//     Verifies the transfer chain: Alice → Bob → Carol.

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc1155Fungible_Transfer(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// Token parameters
	contractAddress := big.NewInt(0x1155) // ERC1155 contract address
	tokenId         := big.NewInt(42)     // fungible token type ID
	amount          := big.NewInt(50)     // Alice holds 50 tokens

	// ── Step 1: Register the token type in the asset group tree ──────────────
	// uid = Erc1155UniqueId(contractAddr, tokenId, 0)
	// This is what the circuit looks for in the asset group tree.
	uid, err := core.Erc1155UniqueId(contractAddress, tokenId, big.NewInt(0))
	if err != nil {
		t.Fatalf("Erc1155UniqueId: %v", err)
	}
	t.Logf("Step 1 — Token UID (asset group leaf): %s", uid)

	assetGroupTree := core.NewMerkleTree(merkleDepth)
	assetGroupTree.InsertLeaf(uid)
	assetGroupProof, err := assetGroupTree.GenerateProof(uid)
	if err != nil {
		t.Fatalf("GenerateProof (asset group): %v", err)
	}
	t.Logf("Step 1 — Asset group root: %s", assetGroupProof.Root)

	// ── Step 2: Alice deposits tokens ────────────────────────────────────────
	// ERC1155 commitment: Poseidon(contractAddr, tokenId, amount, pk_spend, salt)
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	aliceCommitment, err := core.Erc1155Commitment(tokenId, amount, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment (Alice): %v", err)
	}
	t.Logf("Step 2 — Alice's commitment: %s", aliceCommitment)

	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(aliceCommitment)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof (Alice): %v", err)
	}

	// ── Step 3: Alice transfers to Bob (2-in / 2-out) ────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend NewSpendKeyPair: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyView NewViewKeyPair: %v", err)
	}

	aliceResult, err := client.Erc1155FungibleJoinSplitProof(
		big.NewInt(1), // stMessage
		// inputs: Alice's real note + dummy
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSalt, big.NewInt(0)}, // wtSaltsIn
		// outputs: Bob's note + dummy
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey}, // recipient view encap keys
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // stTreeNumbers
		contractAddress,
		tokenId,
		big.NewInt(0),    // stAssetGroupTreeNumber
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleJoinSplitProof (Alice→Bob): %v", err)
	}

	// Statement: [msg, tree0, root0, null0, tree1, root1, null1, cmt0, cmt1]
	const expectedStatementLen = 9
	if len(aliceResult.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(aliceResult.Statement), expectedStatementLen)
	}

	bobCommitmentOnChain  := aliceResult.Statement[7]
	dummyCommitmentOnChain := aliceResult.Statement[8]
	aliceNullifier        := aliceResult.Statement[3]
	t.Logf("Step 3 — Alice's nullifier:       %s", aliceNullifier)
	t.Logf("Step 3 — Bob's commitment:        %s", bobCommitmentOnChain)
	t.Logf("Step 3 — Dummy commitment:        %s", dummyCommitmentOnChain)

	// ── Step 4: Verify statement correctness ─────────────────────────────────

	// 4a. Alice's nullifier: Poseidon(aliceSk, leafIndex)
	expectedNullifier, err := core.GetNullifier(aliceSpend.PrivateKey, aliceProof.Indices)
	if err != nil {
		t.Fatalf("GetNullifier: %v", err)
	}
	if aliceResult.Statement[3].Cmp(expectedNullifier) != 0 {
		t.Errorf("Alice nullifier mismatch: got %s, want %s",
			aliceResult.Statement[3], expectedNullifier)
	}
	t.Logf("Step 4a — Alice's nullifier verified")

	// 4b. Bob scans for his note using his view key (non-interactive delivery)
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitmentOnChain,
		ContractAddress: contractAddress,
		CiphertextI:     aliceResult.CiphertextI[0],
		CiphertextII:    aliceResult.CiphertextII[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob: expected 1 note, got %d", len(bobNotes))
	}
	bobSalt := bobNotes[0].SaltBField
	t.Logf("Step 4b — Bob scanned his note (amount=%s, salt=%s)", bobNotes[0].Amount, bobSalt)

	// 4c. Dummy output: scan should return 0 notes for dummyView (value=0, salt mismatch vs dummy key)
	// We just verify the dummy commitment is different from Bob's.
	if dummyCommitmentOnChain.Cmp(bobCommitmentOnChain) == 0 {
		t.Errorf("dummy commitment should differ from Bob's")
	}
	t.Logf("Step 4c — Dummy commitment differs from Bob's (as expected)")

	// 4d. Merkle root in statement matches the tree root
	if aliceResult.Statement[2].Cmp(aliceProof.Root) != 0 {
		t.Errorf("Merkle root mismatch: got %s, want %s", aliceResult.Statement[2], aliceProof.Root)
	}
	t.Logf("Step 4d — Merkle root verified: %s", aliceProof.Root)

	// ── Step 5: Bob transfers to Carol (chain of custody) ────────────────────
	// Bob's commitment from step 3 is now a valid input note.
	mt.InsertLeaf(bobCommitmentOnChain)
	bobProof, err := mt.GenerateProof(bobCommitmentOnChain)
	if err != nil {
		t.Fatalf("GenerateProof (Bob): %v", err)
	}

	carolSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Carol NewSpendKeyPair: %v", err)
	}
	carolView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Carol NewViewKeyPair: %v", err)
	}

	bobResult, err := client.Erc1155FungibleJoinSplitProof(
		big.NewInt(2), // fresh stMessage
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobSalt, big.NewInt(0)}, // bobSalt recovered via scan
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: carolSpend.PrivateKey, PublicKey: carolSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[][]byte{carolView.EncapsKey, dummyView.EncapsKey}, // recipient view encap keys
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		contractAddress,
		tokenId,
		big.NewInt(0),
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleJoinSplitProof (Bob→Carol): %v", err)
	}

	carolCommitmentOnChain := bobResult.Statement[7]
	bobNullifier           := bobResult.Statement[3]
	t.Logf("Step 5 — Bob's nullifier (burns his note): %s", bobNullifier)
	t.Logf("Step 5 — Carol's commitment:               %s", carolCommitmentOnChain)

	// Bob's nullifier must differ from Alice's
	if bobNullifier.Cmp(expectedNullifier) == 0 {
		t.Errorf("Bob's nullifier should differ from Alice's")
	}

	// Carol scans for her note
	carolEvents := []core.OnChainErc1155Event{{
		Commitment:      carolCommitmentOnChain,
		ContractAddress: contractAddress,
		CiphertextI:     bobResult.CiphertextI[0],
		CiphertextII:    bobResult.CiphertextII[0],
	}}
	carolNotes, err := core.ScanForErc1155Notes(carolView.DecapsKey, carolSpend.PublicKey, carolEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Carol): %v", err)
	}
	if len(carolNotes) != 1 {
		t.Fatalf("Carol: expected 1 note, got %d", len(carolNotes))
	}
	carolSalt := carolNotes[0].SaltBField
	t.Logf("Step 5 — Carol scanned her note (amount=%s, salt=%s)", carolNotes[0].Amount, carolSalt)

	t.Logf("=== ERC1155 FUNGIBLE TRANSFER CHAIN COMPLETE ===")
	t.Logf("Alice→Bob: nullifier=%s, bobCmt=%s", aliceNullifier, bobCommitmentOnChain)
	t.Logf("Bob→Carol: nullifier=%s, carolCmt=%s", bobNullifier, carolCommitmentOnChain)
}

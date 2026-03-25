package tests

// End-to-end V2 ERC1155 non-fungible ownership transfer test.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc1155NonFungible_Transfer -v -timeout 300s
//
// ERC1155 NON-FUNGIBLE vs FUNGIBLE — KEY DIFFERENCES
// ────────────────────────────────────────────────────
// Non-fungible commitment: Poseidon(contractAddress, tokenId, uniqueId, pk_spend, salt)
//   where uniqueId = Erc1155UniqueId(contractAddr, tokenId, value)
// This binds each note to a specific NFT instance.
//
// Unlike ERC1155 Fungible (JoinSplit / 2-in 2-out), the non-fungible circuit
// is a simple ownership transfer: 1-in / 1-out.
//
// ASSET GROUP TREE
// ────────────────
// Same as fungible: uid = Erc1155UniqueId(contractAddr, tokenId, 0) is the
// leaf in the asset group tree, proving the token type is registered.
//
// FLOW
// ────
//   Step 1 — Register token type
//     uid = Erc1155UniqueId(contractAddr, tokenId, 0)
//     Insert uid into the asset group tree.
//
//   Step 2 — Alice deposits NFT
//     Commitment = Poseidon(contractAddr, tokenId, uniqueId, alicePk, aliceSalt)
//       where uniqueId = Erc1155UniqueId(contractAddr, tokenId, value)
//     Insert into the Merkle tree.
//
//   Step 3 — Alice transfers to Bob
//     Input: Alice's note
//     Output: Bob's note (salt derived via ML-KEM)
//     Nullifier = Poseidon(aliceSk, leafIndex)
//
//   Step 4 — Verify statement
//     Statement: [msg, tree, root, null, cmt, assetGroupTree, assetGroupRoot]
//     Bob's note recovered via ML-KEM scan.
//
//   Step 5 — Bob transfers to Carol (chain of custody)
//     Reuses Bob's note as input.

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc1155NonFungible_Transfer(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// Token parameters
	contractAddress := big.NewInt(0x1155) // ERC1155 contract address
	tokenId         := big.NewInt(99)     // NFT token ID (non-fungible)
	value           := big.NewInt(1)      // NFTs always have value=1

	// ── Step 1: Register the token type in the asset group tree ───────────────
	// For non-fungible, uniqueId uses value=0 (same as fungible) for the asset group leaf.
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

	// ── Step 2: Alice deposits NFT ────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	aliceCommitment, err := core.Erc1155Commitment(tokenId, value, aliceSpend.PublicKey, aliceSalt)
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

	// ── Step 3: Alice transfers to Bob ────────────────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	aliceResult, err := client.Erc1155NonFungibleOwnershipProof(
		big.NewInt(1), // stMessage
		value,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0), // stTreeNumber
		contractAddress,
		tokenId,
		big.NewInt(0), // stAssetGroupTreeNumber
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155NonFungibleOwnershipProof (Alice→Bob): %v", err)
	}

	// Statement: [msg, tree, root, null, cmt, assetGroupTree, assetGroupRoot]
	const expectedStatementLen = 7
	if len(aliceResult.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(aliceResult.Statement), expectedStatementLen)
	}

	bobCommitmentOnChain := aliceResult.Statement[4]
	aliceNullifier       := aliceResult.Statement[3]
	t.Logf("Step 3 — Alice's nullifier:   %s", aliceNullifier)
	t.Logf("Step 3 — Bob's commitment:    %s", bobCommitmentOnChain)

	// ── Step 4: Verify statement correctness ──────────────────────────────────

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
	t.Logf("Step 4b — Bob scanned his note (tokenId=%s, amount=%s, salt=%s)",
		bobNotes[0].TokenId, bobNotes[0].Amount, bobSalt)

	// 4c. Merkle root in statement matches the tree root
	if aliceResult.Statement[2].Cmp(aliceProof.Root) != 0 {
		t.Errorf("Merkle root mismatch: got %s, want %s", aliceResult.Statement[2], aliceProof.Root)
	}
	t.Logf("Step 4c — Merkle root verified: %s", aliceProof.Root)

	// 4d. Asset group root in statement matches the tree root
	if aliceResult.Statement[6].Cmp(assetGroupProof.Root) != 0 {
		t.Errorf("Asset group root mismatch: got %s, want %s", aliceResult.Statement[6], assetGroupProof.Root)
	}
	t.Logf("Step 4d — Asset group root verified: %s", assetGroupProof.Root)

	// ── Step 5: Bob transfers to Carol (chain of custody) ─────────────────────
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

	bobResult, err := client.Erc1155NonFungibleOwnershipProof(
		big.NewInt(2), // fresh stMessage
		value,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobSalt, // bobSalt recovered via scan
		core.KeyPair{PrivateKey: carolSpend.PrivateKey, PublicKey: carolSpend.PublicKey},
		carolView.EncapsKey,
		merkleDepth,
		bobProof,
		big.NewInt(0),
		contractAddress,
		tokenId,
		big.NewInt(0),
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155NonFungibleOwnershipProof (Bob→Carol): %v", err)
	}

	carolCommitmentOnChain := bobResult.Statement[4]
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
	t.Logf("Step 5 — Carol scanned her note (tokenId=%s, amount=%s, salt=%s)",
		carolNotes[0].TokenId, carolNotes[0].Amount, carolSalt)

	t.Logf("=== ERC1155 NON-FUNGIBLE TRANSFER CHAIN COMPLETE ===")
	t.Logf("Alice→Bob: nullifier=%s, bobCmt=%s", aliceNullifier, bobCommitmentOnChain)
	t.Logf("Bob→Carol: nullifier=%s, carolCmt=%s", bobNullifier, carolCommitmentOnChain)
}

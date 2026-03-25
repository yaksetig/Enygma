package tests

// End-to-end V2 ERC1155 non-fungible auditor ownership transfer test.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc1155NonFungibleAuditor_Transfer -v -timeout 300s
//
// AUDITOR FLOW OVERVIEW
// ─────────────────────
// Same circuit structure as ERC1155 non-fungible ownership, with an additional
// AuditorAccessCircuit that validates Poseidon-encrypted audit data.
//
// The prover encrypts these 3 values for the auditor:
//   [value[0], tokenId[0], contractAddr]
//
// Circuit config: TmNumOfTokens=1, TmMerkleTreeDepth=8, TmAssetGroupMerkleTree=8
// plainLength = 1*2 + 1 = 3, encLength = 4 (3 cipher values + 1 MAC)
//
// Recipients have ML-KEM view keys for non-interactive note delivery.

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc1155NonFungibleAuditor_Transfer(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// Token parameters
	contractAddress := big.NewInt(0x1155)
	tokenId         := big.NewInt(99) // non-fungible unique token ID
	value           := big.NewInt(1)  // non-fungible: always value=1

	// ── Step 1: Register token type in asset group tree ───────────────────────
	uid, err := core.Erc1155UniqueId(contractAddress, tokenId, big.NewInt(0))
	if err != nil {
		t.Fatalf("Erc1155UniqueId: %v", err)
	}
	assetGroupTree := core.NewMerkleTree(merkleDepth)
	assetGroupTree.InsertLeaf(uid)
	assetGroupProof, err := assetGroupTree.GenerateProof(uid)
	if err != nil {
		t.Fatalf("GenerateProof (asset group): %v", err)
	}
	t.Logf("Step 1 — Token UID (asset group leaf): %s", uid)
	t.Logf("Step 1 — Asset group root: %s", assetGroupProof.Root)

	// ── Step 2: Generate auditor key pair ─────────────────────────────────────
	auditorKey, err := core.NewAuditorKeyPair()
	if err != nil {
		t.Fatalf("NewAuditorKeyPair: %v", err)
	}
	t.Logf("Step 2 — Auditor pubKey.X: %s", auditorKey.PublicKeyX)
	t.Logf("Step 2 — Auditor pubKey.Y: %s", auditorKey.PublicKeyY)

	// ── Step 3: Alice deposits non-fungible token ─────────────────────────────
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
	t.Logf("Step 3 — Alice's commitment: %s", aliceCommitment)

	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(aliceCommitment)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof (Alice): %v", err)
	}

	// ── Step 4: Alice transfers to Bob with auditor proof ─────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	result, err := client.Erc1155NonFungibleAuditorProof(
		big.NewInt(1), // stMessage
		value,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0),  // stTreeNumber
		contractAddress,
		tokenId,
		big.NewInt(0),  // stAssetGroupTreeNumber
		assetGroupProof,
		auditorKey.PublicKeyX, auditorKey.PublicKeyY,
	)
	if err != nil {
		t.Fatalf("Erc1155NonFungibleAuditorProof: %v", err)
	}

	// ── Step 5: Verify statement ──────────────────────────────────────────────
	// Statement: [msg, treeNumber, merkleRoot, nullifier, commitmentOut,
	//             assetGroupTreeNumber, assetGroupMerkleRoot]
	const expectedStatementLen = 7
	if len(result.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(result.Statement), expectedStatementLen)
	}

	aliceNullifier := result.Statement[3]
	bobCmtOnChain  := result.Statement[4]

	// Alice's nullifier
	expectedNull, err := core.GetNullifier(aliceSpend.PrivateKey, aliceProof.Indices)
	if err != nil {
		t.Fatalf("GetNullifier: %v", err)
	}
	if aliceNullifier.Cmp(expectedNull) != 0 {
		t.Errorf("Alice nullifier mismatch: got %s, want %s", aliceNullifier, expectedNull)
	}
	t.Logf("Step 5 — Alice nullifier verified: %s", aliceNullifier)

	// Bob scans for his note using his view key (non-interactive delivery)
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCmtOnChain,
		ContractAddress: contractAddress,
		CiphertextI:     result.CiphertextI[0],
		CiphertextII:    result.CiphertextII[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob: expected 1 note, got %d", len(bobNotes))
	}
	t.Logf("Step 5 — Bob commitment verified via scan (tokenId=%s, amount=%s, salt=%s)",
		bobNotes[0].TokenId, bobNotes[0].Amount, bobNotes[0].SaltBField)

	// Merkle root in statement
	if result.Statement[2].Cmp(aliceProof.Root) != 0 {
		t.Errorf("Merkle root mismatch: got %s, want %s", result.Statement[2], aliceProof.Root)
	}
	t.Logf("Step 5 — Merkle root verified: %s", aliceProof.Root)

	// Asset group root in statement
	if result.Statement[6].Cmp(assetGroupProof.Root) != 0 {
		t.Errorf("Asset group root mismatch: got %s, want %s", result.Statement[6], assetGroupProof.Root)
	}
	t.Logf("Step 5 — Asset group root verified: %s", assetGroupProof.Root)

	// ── Step 6: Auditor decrypts the encrypted audit trail ───────────────────
	ad := result.AuditData
	if ad == nil {
		t.Fatal("AuditData missing from ProofResult")
	}
	plaintext, err := client.AuditorDecrypt(ad.AuthKeyX, ad.AuthKeyY, ad.Nonce, ad.Encrypted, ad.RealLength)
	if err != nil {
		t.Fatalf("AuditorDecrypt: %v", err)
	}

	// plaintext = [value[0], tokenId[0], contractAddr]
	if len(plaintext) != 3 {
		t.Fatalf("expected 3 plaintext values, got %d", len(plaintext))
	}
	if plaintext[0].Cmp(value) != 0 {
		t.Errorf("value: got %s, want %s", plaintext[0], value)
	}
	if plaintext[1].Cmp(tokenId) != 0 {
		t.Errorf("tokenId: got %s, want %s", plaintext[1], tokenId)
	}
	if plaintext[2].Cmp(contractAddress) != 0 {
		t.Errorf("contractAddr: got %s, want %s", plaintext[2], contractAddress)
	}
	t.Logf("Step 6 — Auditor decrypted: value=%s tokenId=%s contract=%s",
		plaintext[0], plaintext[1], plaintext[2])

	t.Logf("=== ERC1155 NON-FUNGIBLE AUDITOR TRANSFER COMPLETE ===")
	t.Logf("nullifier=%s, bobCmt=%s", aliceNullifier, bobCmtOnChain)
}

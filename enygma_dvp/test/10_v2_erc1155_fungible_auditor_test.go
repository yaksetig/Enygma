package tests

// End-to-end V2 ERC1155 fungible auditor transfer test.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc1155FungibleAuditor_Transfer -v -timeout 300s
//
// AUDITOR FLOW OVERVIEW
// ─────────────────────
// Same circuit structure as ERC1155 fungible JoinSplit, with an additional
// AuditorAccessCircuit that validates Poseidon-encrypted audit data.
//
// The prover encrypts these 6 values for the auditor:
//   [valIn[0], valIn[1], valOut[0], valOut[1], tokenId, contractAddr]
//
// Encryption: PoseidonEncrypt(authKey=[pubKey*random], nonce, plaintext)
// The auditor can decrypt by computing: authKey = privKey * StAuditorAuthKey[0..1]
// (since StAuditorAuthKey = random * G, so privKey * (random*G) = random * (privKey*G) = random*pubKey)
//
// The circuit verifies:
//   1. All the standard ERC1155 fungible constraints
//   2. PoseidonDecrypt(nonce, ScalarMul(pubKey, random), encrypted) == plaintext
//
// KEYS
// ────
// Both spend keys (Poseidon-based) and the auditor key (BabyJubJub) are needed.
// Recipients also have ML-KEM view keys for non-interactive note delivery.

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc1155FungibleAuditor_Transfer(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// Token parameters
	contractAddress := big.NewInt(0x1155)
	tokenId         := big.NewInt(42)
	amount          := big.NewInt(50)

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

	// ── Step 3: Alice deposits tokens ─────────────────────────────────────────
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
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend NewSpendKeyPair: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyView NewViewKeyPair: %v", err)
	}

	result, err := client.Erc1155FungibleAuditorProof(
		big.NewInt(1), // stMessage
		// inputs: Alice's real note + dummy
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSalt, big.NewInt(0)},
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
		big.NewInt(0),   // stAssetGroupTreeNumber
		assetGroupProof,
		auditorKey.PublicKeyX, auditorKey.PublicKeyY,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleAuditorProof: %v", err)
	}

	// ── Step 5: Verify statement ──────────────────────────────────────────────
	// Statement: [msg, tree0, root0, null0, tree1, root1, null1, cmt0, cmt1]
	const expectedStatementLen = 9
	if len(result.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(result.Statement), expectedStatementLen)
	}

	aliceNullifier  := result.Statement[3]
	bobCmtOnChain   := result.Statement[7]
	dummyCmtOnChain := result.Statement[8]

	// Alice's nullifier: Poseidon(sk, leafIndex)
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
	t.Logf("Step 5 — Bob commitment verified via scan (amount=%s, salt=%s)",
		bobNotes[0].Amount, bobNotes[0].SaltBField)

	// Dummy output: commitment should differ from Bob's
	if dummyCmtOnChain.Cmp(bobCmtOnChain) == 0 {
		t.Errorf("dummy commitment should differ from Bob's")
	}
	t.Logf("Step 5 — Dummy commitment differs from Bob's (as expected)")

	// ── Step 6: Auditor decrypts the encrypted audit trail ───────────────────
	// The auditor has StAuditorAuthKey (= pubKey*random) stored from the proof.
	// They pass it directly as the decryption key.
	ad := result.AuditData
	if ad == nil {
		t.Fatal("AuditData missing from ProofResult")
	}
	plaintext, err := client.AuditorDecrypt(ad.AuthKeyX, ad.AuthKeyY, ad.Nonce, ad.Encrypted, ad.RealLength)
	if err != nil {
		t.Fatalf("AuditorDecrypt: %v", err)
	}

	// plaintext = [valIn[0], valIn[1], valOut[0], valOut[1], tokenId, contractAddr]
	if len(plaintext) != 6 {
		t.Fatalf("expected 6 plaintext values, got %d", len(plaintext))
	}
	if plaintext[0].Cmp(amount) != 0 {
		t.Errorf("valIn[0]: got %s, want %s", plaintext[0], amount)
	}
	if plaintext[1].Sign() != 0 {
		t.Errorf("valIn[1] (dummy): got %s, want 0", plaintext[1])
	}
	if plaintext[2].Cmp(amount) != 0 {
		t.Errorf("valOut[0]: got %s, want %s", plaintext[2], amount)
	}
	if plaintext[3].Sign() != 0 {
		t.Errorf("valOut[1] (dummy): got %s, want 0", plaintext[3])
	}
	if plaintext[4].Cmp(tokenId) != 0 {
		t.Errorf("tokenId: got %s, want %s", plaintext[4], tokenId)
	}
	if plaintext[5].Cmp(contractAddress) != 0 {
		t.Errorf("contractAddr: got %s, want %s", plaintext[5], contractAddress)
	}
	t.Logf("Step 6 — Auditor decrypted: valIn=[%s,%s] valOut=[%s,%s] tokenId=%s contract=%s",
		plaintext[0], plaintext[1], plaintext[2], plaintext[3], plaintext[4], plaintext[5])

	t.Logf("=== ERC1155 FUNGIBLE AUDITOR TRANSFER COMPLETE ===")
	t.Logf("nullifier=%s, bobCmt=%s", aliceNullifier, bobCmtOnChain)
}

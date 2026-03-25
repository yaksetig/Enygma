package tests

// End-to-end V2 ERC1155 fungible with-broker transfer test.
// Requires the gnark server running on localhost:8081.
// Run with: go test -run TestV2Erc1155FungibleBroker_Transfer -v -timeout 300s
//
// WITH-BROKER vs PLAIN FUNGIBLE — KEY DIFFERENCES
// ─────────────────────────────────────────────────
// Plain fungible: 2-in / 2-out (recipient + change)
// With-broker:    2-in / 3-out (recipient + change + broker commission)
//
// Commission formula (in-circuit):
//   valuesOut[2] = valuesOut[0] * (StBrokerCommisionRate / 10^2)
//   e.g. rate=10 means 10%: commission = recipient * 10 / 100
//
// Conservation: sum(inputs) = sum(outputs) = recipient + change + commission
//
// Broker identity: StBrokerBlindedPublicKey = BlindedPublicKey(WtRecipientPk[2])
//   The prover derives the blinded key from brokerPublicKey and asserts it
//   matches the broker's registered key on-chain.
//
// STATEMENT LAYOUT (non-interleaved, from prover)
// ────────────────────────────────────────────────
// [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1, cmt2,
//  brokerBlindedPk, commissionRate, assetGroupTree, assetGroupRoot]
//
// FLOW
// ────
//   Step 1 — Register token type (asset group tree)
//   Step 2 — Alice deposits 110 tokens
//   Step 3 — Alice transfers: Bob gets 100, broker gets 10, change = 0
//             commission = 100 * 10/100 = 10
//             balance:    100 + 0 + 10  = 110 ✓
//   Step 4 — Verify statement, nullifier, commitments via ML-KEM scan

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2Erc1155FungibleBroker_Transfer(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// Token parameters
	contractAddress := big.NewInt(0x1155)
	tokenId         := big.NewInt(7)    // fungible token type

	// Commission: rate=10 means 10% (rate / 10^2 = rate/100)
	commissionRate  := big.NewInt(10)
	// Input amount chosen so commission divides evenly:
	//   recipient=100, commission=100*10/100=10, change=0, total=110
	totalAmount     := big.NewInt(110)
	recipientAmount := big.NewInt(100)
	commissionAmount := big.NewInt(10) // = recipientAmount * 10/100
	changeAmount    := big.NewInt(0)

	// ── Step 1: Register token type ──────────────────────────────────────────
	uid, err := core.Erc1155UniqueId(contractAddress, tokenId, big.NewInt(0))
	if err != nil {
		t.Fatalf("Erc1155UniqueId: %v", err)
	}
	t.Logf("Step 1 — Token UID: %s", uid)

	assetGroupTree := core.NewMerkleTree(merkleDepth)
	assetGroupTree.InsertLeaf(uid)
	assetGroupProof, err := assetGroupTree.GenerateProof(uid)
	if err != nil {
		t.Fatalf("GenerateProof (asset group): %v", err)
	}
	t.Logf("Step 1 — Asset group root: %s", assetGroupProof.Root)

	// ── Step 2: Alice deposits 110 tokens ─────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	aliceCommitment, err := core.Erc1155Commitment(tokenId, totalAmount, aliceSpend.PublicKey, aliceSalt)
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

	// ── Step 3: Alice transfers to Bob via broker ──────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	brokerSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Broker NewSpendKeyPair: %v", err)
	}
	brokerView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Broker NewViewKeyPair: %v", err)
	}
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend NewSpendKeyPair: %v", err)
	}

	// keysOut[0]=Bob (recipient), keysOut[1]=Alice (change), keysOut[2]=Broker
	// valuesOut[0]=100, valuesOut[1]=0, valuesOut[2]=10
	result, err := client.Erc1155FungibleWithBrokerV1Proof(
		big.NewInt(1), // stMessage
		[]*big.Int{totalAmount, big.NewInt(0)}, // valuesIn: Alice's note + dummy
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSalt, big.NewInt(0)}, // saltsIn
		[]*big.Int{recipientAmount, changeAmount, commissionAmount}, // valuesOut
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},      // [0] recipient
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},  // [1] change (back to Alice)
			{PrivateKey: brokerSpend.PrivateKey, PublicKey: brokerSpend.PublicKey}, // [2] broker
		},
		[][]byte{bobView.EncapsKey, aliceView.EncapsKey, brokerView.EncapsKey}, // recipient view encap keys
		merkleDepth,
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // treeNumbers
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		contractAddress,
		tokenId,
		big.NewInt(0), // stAssetGroupTreeNumber
		assetGroupProof,
		brokerSpend.PublicKey,
		commissionRate,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleWithBrokerV1Proof: %v", err)
	}

	// Statement layout (non-interleaved):
	// [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1, cmt2,
	//  brokerBlindedPk, commissionRate, assetGroupTree, assetGroupRoot]
	const expectedStatementLen = 14
	if len(result.Statement) != expectedStatementLen {
		t.Errorf("statement length: got %d, want %d", len(result.Statement), expectedStatementLen)
	}

	aliceNullifier          := result.Statement[5]
	bobCommitmentOnChain    := result.Statement[7]
	changeCommitmentOnChain := result.Statement[8]
	brokerCommitmentOnChain := result.Statement[9]
	t.Logf("Step 3 — Alice's nullifier:     %s", aliceNullifier)
	t.Logf("Step 3 — Bob's commitment:      %s", bobCommitmentOnChain)
	t.Logf("Step 3 — Change commitment:     %s", changeCommitmentOnChain)
	t.Logf("Step 3 — Broker commitment:     %s", brokerCommitmentOnChain)

	// ── Step 4: Verify statement correctness ──────────────────────────────────

	// 4a. Alice's nullifier: Poseidon(aliceSk, leafIndex)
	expectedNullifier, err := core.GetNullifier(aliceSpend.PrivateKey, aliceProof.Indices)
	if err != nil {
		t.Fatalf("GetNullifier: %v", err)
	}
	if aliceNullifier.Cmp(expectedNullifier) != 0 {
		t.Errorf("Alice nullifier mismatch: got %s, want %s", aliceNullifier, expectedNullifier)
	}
	t.Logf("Step 4a — Alice's nullifier verified")

	// 4b. Bob scans for his note
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitmentOnChain,
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
	t.Logf("Step 4b — Bob scanned his note (amount=%s)", bobNotes[0].Amount)
	if bobNotes[0].Amount.Cmp(recipientAmount) != 0 {
		t.Errorf("Bob amount mismatch: got %s, want %s", bobNotes[0].Amount, recipientAmount)
	}

	// 4c. Alice scans for her change output
	changeEvents := []core.OnChainErc1155Event{{
		Commitment:      changeCommitmentOnChain,
		ContractAddress: contractAddress,
		CiphertextI:     result.CiphertextI[1],
		CiphertextII:    result.CiphertextII[1],
	}}
	aliceChangeNotes, err := core.ScanForErc1155Notes(aliceView.DecapsKey, aliceSpend.PublicKey, changeEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Alice change): %v", err)
	}
	if len(aliceChangeNotes) != 1 {
		t.Fatalf("Alice change: expected 1 note, got %d", len(aliceChangeNotes))
	}
	t.Logf("Step 4c — Alice scanned her change note (amount=%s)", aliceChangeNotes[0].Amount)
	if aliceChangeNotes[0].Amount.Cmp(changeAmount) != 0 {
		t.Errorf("Alice change amount mismatch: got %s, want %s", aliceChangeNotes[0].Amount, changeAmount)
	}

	// 4d. Broker scans for its commission
	brokerEvents := []core.OnChainErc1155Event{{
		Commitment:      brokerCommitmentOnChain,
		ContractAddress: contractAddress,
		CiphertextI:     result.CiphertextI[2],
		CiphertextII:    result.CiphertextII[2],
	}}
	brokerNotes, err := core.ScanForErc1155Notes(brokerView.DecapsKey, brokerSpend.PublicKey, brokerEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Broker): %v", err)
	}
	if len(brokerNotes) != 1 {
		t.Fatalf("Broker: expected 1 note, got %d", len(brokerNotes))
	}
	t.Logf("Step 4d — Broker scanned commission note (amount=%s)", brokerNotes[0].Amount)
	if brokerNotes[0].Amount.Cmp(commissionAmount) != 0 {
		t.Errorf("Broker commission amount mismatch: got %s, want %s", brokerNotes[0].Amount, commissionAmount)
	}

	// 4e. Merkle root in statement matches tree root
	if result.Statement[3].Cmp(aliceProof.Root) != 0 {
		t.Errorf("Merkle root mismatch: got %s, want %s", result.Statement[3], aliceProof.Root)
	}
	t.Logf("Step 4e — Merkle root verified: %s", aliceProof.Root)

	// 4f. Asset group root in statement matches tree root
	if result.Statement[13].Cmp(assetGroupProof.Root) != 0 {
		t.Errorf("Asset group root mismatch: got %s, want %s", result.Statement[13], assetGroupProof.Root)
	}
	t.Logf("Step 4f — Asset group root verified: %s", assetGroupProof.Root)

	t.Logf("=== ERC1155 FUNGIBLE WITH-BROKER TRANSFER COMPLETE ===")
	t.Logf("Alice→Bob: nullifier=%s", aliceNullifier)
	t.Logf("Bob's note: cmt=%s (100 tokens)", bobCommitmentOnChain)
	t.Logf("Broker note: cmt=%s (10 tokens commission)", brokerCommitmentOnChain)
}

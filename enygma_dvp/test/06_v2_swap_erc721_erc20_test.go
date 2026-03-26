package tests

// ZkDvP two-phase atomic swap test: Alice swaps 5 USDT (tokenId=10) for Bob's 1 concert ticket (tokenId=25).
//
// Both assets use the ERC20 JoinSplit circuit with Poseidon4(pk, salt, amount, tokenId) commitments.
// Atomicity is enforced by cross-commitment linking, not a shared swapId:
//
//	stMessage(Alice) = C'          (Alice's expected ticket commitment)
//	firstOutput(Alice) = CommitmentB (Alice's USDT payment for Bob)
//	stMessage(Bob)   = CommitmentB  (links back to Alice's pending proof)
//	firstOutput(Bob) = C'          (delivers ticket commitment to Alice)
//
// ALICE INITIATES (Phase 1)
// ──────────────────────────
//
//  1. Encapsulate(encapKey_bob) → saltB, ctI
//     CommitmentB = Poseidon4(pk_bob, saltBField, 5, 10)     [USDT note for Bob]
//
//  2. saltStar = random
//     C' = Poseidon4(pk_alice, saltStarField, 1, 25)          [ticket note for Alice]
//
//  3. ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
//
//  4. JoinSplit proof: StMessage=C', inputs=[USDT note], outputs=[CommitmentB, dummy]
//     → submitPartialSettlement → PENDING
//
// BOB COMPLETES (Phase 2)
// ────────────────────────
//
//  5. saltB'  = Decapsulate(decapKey_bob, ctI)
//     (tokenId=25, amount=1, saltStar) = DecryptSwapPayload(saltB', ctII)
//
//  6. Assert CommitmentB == Poseidon4(pk_bob, saltBField', 5, 10) ✓
//     Assert C'          == Poseidon4(pk_alice, saltStarField, 1, 25) ✓
//
//  7. JoinSplit proof: StMessage=CommitmentB, outputs=[C', dummy]
//     → submitPartialSettlement → SETTLED (cross-commitment match)
//
// CROSS-COMMITMENT VERIFICATION
// ──────────────────────────────
//   stMsg(Alice) = C'          == firstOut(Bob) = C'          ✓
//   stMsg(Bob)   = CommitmentB == firstOut(Alice) = CommitmentB ✓
//
// Run with: go test -run TestV2ZkDvp_TwoPhaseSwap -v -timeout 300s

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2ZkDvp_TwoPhaseSwap(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// ── Swap terms (agreed off-chain) ──────────────────────────────────────
	amountUSDT    := big.NewInt(5)
	tokenIdUSDT   := big.NewInt(10)
	amountTicket  := big.NewInt(1)
	tokenIdTicket := big.NewInt(25)

	t.Logf("Swap: Alice gives %s USDT (tid=%s) ↔ Bob gives %s ticket (tid=%s)",
		amountUSDT, tokenIdUSDT, amountTicket, tokenIdTicket)

	// ── Step 1: Setup — initial notes ─────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
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
		t.Fatalf("dummySpend: %v", err)
	}

	// Alice's USDT note.
	aliceSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Alice GenerateRandomValue: %v", err)
	}
	aliceSaltField := core.SaltBToField(aliceSaltRaw)
	aliceUSDTCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltField, amountUSDT, tokenIdUSDT)
	if err != nil {
		t.Fatalf("Alice Erc20CommitmentV2: %v", err)
	}

	// Bob's concert ticket note.
	bobSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Bob GenerateRandomValue: %v", err)
	}
	bobSaltField := core.SaltBToField(bobSaltRaw)
	bobTicketCommitment, err := core.Erc20CommitmentV2(bobSpend.PublicKey, bobSaltField, amountTicket, tokenIdTicket)
	if err != nil {
		t.Fatalf("Bob Erc20CommitmentV2: %v", err)
	}

	// Build shared Merkle tree and generate proofs.
	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(aliceUSDTCommitment)
	mt.InsertLeaf(bobTicketCommitment)

	aliceProof, err := mt.GenerateProof(aliceUSDTCommitment)
	if err != nil {
		t.Fatalf("Alice GenerateProof: %v", err)
	}
	bobProof, err := mt.GenerateProof(bobTicketCommitment)
	if err != nil {
		t.Fatalf("Bob GenerateProof: %v", err)
	}

	t.Logf("Step 1 — Alice USDT cmt:  %s", aliceUSDTCommitment)
	t.Logf("Step 1 — Bob ticket cmt:  %s", bobTicketCommitment)

	// ── Step 2: Alice initiates the swap (Phase 1) ─────────────────────────
	// ZkDvpInitiateSwap:
	//   (1) Encapsulate(encapKey_bob) → saltB, ctI
	//   (2) CommitmentB = Poseidon4(bobPk, saltBField, 5, 10)
	//   (3) saltStar = random; C' = Poseidon4(alicePk, saltStarField, 1, 25)
	//   (4) ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
	//   (5) JoinSplit proof: StMessage=C', firstOutput=CommitmentB
	aliceResult, err := client.ZkDvpInitiateSwap(
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSaltField,
		amountUSDT,
		tokenIdUSDT,
		bobSpend.PublicKey,
		bobView.EncapsKey,
		tokenIdTicket,
		amountTicket,
		merkleDepth,
		aliceProof,
		big.NewInt(0), // treeNumber
	)
	if err != nil {
		t.Fatalf("Alice ZkDvpInitiateSwap: %v", err)
	}

	commitmentA := aliceResult.CommitmentA // C' — the concert ticket commitment Alice expects
	commitmentB := aliceResult.CommitmentB // CommitmentB — Alice's USDT payment for Bob

	t.Logf("Step 2 — C' (Alice expects ticket): %s", commitmentA)
	t.Logf("Step 2 — CommitmentB (Bob receives USDT): %s", commitmentB)

	// ── Step 3: Bob scans and verifies Alice's pending swap ─────────────────
	events := []core.OnChainZkDvpEvent{
		{
			CommitmentA:  commitmentA,
			CommitmentB:  commitmentB,
			CiphertextI:  aliceResult.CiphertextI,
			CiphertextII: aliceResult.CiphertextII,
		},
	}

	swaps, err := core.ScanForZkDvpSwap(
		bobView.DecapsKey,
		aliceSpend.PublicKey,
		bobSpend.PublicKey,
		amountUSDT,
		tokenIdUSDT,
		events,
	)
	if err != nil {
		t.Fatalf("Bob ScanForZkDvpSwap: %v", err)
	}
	if len(swaps) != 1 {
		t.Fatalf("Bob: expected 1 swap, got %d", len(swaps))
	}
	swap := swaps[0]

	if swap.TokenIdOut.Cmp(tokenIdTicket) != 0 {
		t.Errorf("tokenIdOut: got %s, want %s", swap.TokenIdOut, tokenIdTicket)
	}
	if swap.AmountOut.Cmp(amountTicket) != 0 {
		t.Errorf("amountOut: got %s, want %s", swap.AmountOut, amountTicket)
	}
	if swap.CommitmentA.Cmp(commitmentA) != 0 {
		t.Errorf("CommitmentA: got %s, want %s", swap.CommitmentA, commitmentA)
	}
	if swap.CommitmentB.Cmp(commitmentB) != 0 {
		t.Errorf("CommitmentB: got %s, want %s", swap.CommitmentB, commitmentB)
	}
	t.Logf("Step 3 — Bob verified swap: tokenIdOut=%s, amountOut=%s ✓", swap.TokenIdOut, swap.AmountOut)

	// ── Step 4: Bob completes the swap (Phase 2) ───────────────────────────
	// Bob generates a JoinSplit proof:
	//   StMessage    = CommitmentB          (what Bob expects to receive)
	//   firstOutput  = C'                   (ticket commitment for Alice, using saltStarField)
	saltStarField := core.SaltBToField(swap.SaltStar)

	bobResult, err := client.Erc20JoinSplitProofFromSalts(
		commitmentB, // stMessage = CommitmentB (cross-commitment link back to Alice's proof)
		[]*big.Int{amountTicket, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobSaltField, big.NewInt(0)},
		[]*big.Int{amountTicket, big.NewInt(0)},
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltStarField, big.NewInt(0)}, // saltStarField → firstOutput = C'
		[][]byte{nil, nil},
		[][]byte{nil, nil},
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenIdTicket,
		false,
	)
	if err != nil {
		t.Fatalf("Bob Erc20JoinSplitProofFromSalts: %v", err)
	}

	t.Logf("Step 4 — Bob's proof: stMessage=%s, firstOutput=%s",
		bobResult.Statement[0], bobResult.Statement[7])

	// ── Step 5: Cross-commitment verification ─────────────────────────────
	// On-chain _settleOnGroupPair enforces exactly these two checks.

	// Alice's StMessage (C') must equal Bob's first output (C').
	if commitmentA.Cmp(bobResult.Statement[7]) != 0 {
		t.Errorf("cross-check FAILED: stMsg(Alice)=C'=%s != firstOut(Bob)=%s",
			commitmentA, bobResult.Statement[7])
	} else {
		t.Logf("Step 5a — stMsg(Alice)=C'=%s == firstOut(Bob) ✓", commitmentA)
	}

	// Bob's StMessage (CommitmentB) must equal Alice's first output (CommitmentB).
	if bobResult.Statement[0].Cmp(commitmentB) != 0 {
		t.Errorf("cross-check FAILED: stMsg(Bob)=%s != CommitmentB=%s",
			bobResult.Statement[0], commitmentB)
	} else {
		t.Logf("Step 5b — stMsg(Bob)=CommitmentB=%s == firstOut(Alice) ✓", commitmentB)
	}

	// Alice can spend C' using saltStarField (she generated it — no scanning needed).
	recoveredC, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltStarField, amountTicket, tokenIdTicket)
	if err != nil {
		t.Fatalf("Alice recompute C': %v", err)
	}
	if recoveredC.Cmp(commitmentA) != 0 {
		t.Errorf("Alice recomputed C' mismatch: got %s, want %s", recoveredC, commitmentA)
	}
	t.Logf("Step 5c — Alice can spend C' with saltStarField=%s ✓", saltStarField)

	t.Logf("=== ZkDvP TWO-PHASE SWAP COMPLETE ===")
	t.Logf("Alice burned USDT note  → Bob receives CommitmentB=%s", commitmentB)
	t.Logf("Bob burned ticket note  → Alice receives C'=%s", commitmentA)
}

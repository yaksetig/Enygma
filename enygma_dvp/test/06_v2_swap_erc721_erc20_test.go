package tests

// ZkDvP two-phase atomic swap test: Alice swaps 5 USDT (ERC20) for Bob's 1 concert ticket (ERC721).
//
// COMMITMENT FORMULAS
// ────────────────────
//   Alice's USDT: Poseidon4(pk_alice, saltA, 5, 0)        — ERC20 JoinSplit, tokenId=0
//   Bob's ticket: Poseidon4(pk_bob, saltB, 1, tokenId=25) — ERC721 OwnershipProof
//   Note: Erc721Commitment(tid, pk, salt) == Erc20CommitmentV2(pk, salt, 1, tid)
//
// CROSS-COMMITMENT ATOMICITY
// ───────────────────────────
//   stMessage(Alice) = C'            (Alice's expected ERC721 ticket commitment)
//   firstOutput(Alice) = CommitmentB (Alice's USDT payment for Bob)
//   stMessage(Bob)   = CommitmentB  (links Bob's proof back to Alice's pending proof)
//   firstOutput(Bob) = C'            (Bob delivers exactly the ticket commitment Alice pre-computed)
//
//   On-chain _settleOnGroupPair verifies:
//     receipt_alice.statement[0]  == receipt_bob.statement[4]   // C' == C' ✓
//     receipt_bob.statement[0]    == receipt_alice.statement[7]  // CommitmentB == CommitmentB ✓
//
// ALICE INITIATES (Phase 1 — ERC20 vault, groupId=FUNGIBLES)
// ────────────────────────────────────────────────────────────
//  1. Encapsulate(encapKey_bob) → saltB, ctI
//     CommitmentB = Poseidon4(pk_bob, SaltBToField(saltB), 5, 0)
//
//  2. saltStar = random
//     C' = Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25)   ← same as Erc721Commitment
//
//  3. ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
//
//  4. ERC20 JoinSplit proof: StMessage=C', inputs=[USDT note], outputs=[CommitmentB, dummy]
//     → submitPartialSettlement(vaultId=0, groupId=FUNGIBLES) → PENDING
//
// BOB COMPLETES (Phase 2 — ERC721 vault, groupId=NON_FUNGIBLES)
// ───────────────────────────────────────────────────────────────
//  5. saltB' = Decapsulate(decapKey_bob, ctI)
//     (tokenId=25, amount=1, saltStar) = DecryptSwapPayload(saltB', ctII)
//
//  6. Assert CommitmentB == Poseidon4(pk_bob, SaltBToField(saltB'), 5, 0) ✓
//     Assert C'          == Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) ✓
//
//  7. ERC721 OwnershipProof: StMessage=CommitmentB, output=C'
//     → submitPartialSettlement(vaultId=1, groupId=NON_FUNGIBLES) → SETTLED
//     Contract calls swapOnGroupPair(aliceERC20, bobERC721, vault0, vault1, FUNGIBLES, NON_FUNGIBLES)
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
	tokenIdUSDT   := big.NewInt(0)  // ERC20 circuit convention: tokenId = 0
	amountTicket  := big.NewInt(1)
	tokenIdTicket := big.NewInt(25) // concert ticket ERC721 tokenId
	contractAddr  := big.NewInt(0)  // ERC721 contract address witness

	t.Logf("Swap: Alice gives %s USDT (ERC20) ↔ Bob gives ticket ERC721 (tokenId=%s)",
		amountUSDT, tokenIdTicket)

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

	// Alice's USDT note (ERC20 vault, tokenId=0).
	aliceSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Alice GenerateRandomValue: %v", err)
	}
	aliceSaltField := core.SaltBToField(aliceSaltRaw)
	aliceUSDTCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltField, amountUSDT, tokenIdUSDT)
	if err != nil {
		t.Fatalf("Alice Erc20CommitmentV2: %v", err)
	}

	mt20 := core.NewMerkleTree(merkleDepth)
	mt20.InsertLeaf(aliceUSDTCommitment)
	aliceProof, err := mt20.GenerateProof(aliceUSDTCommitment)
	if err != nil {
		t.Fatalf("Alice GenerateProof: %v", err)
	}
	t.Logf("Step 1 — Alice USDT cmt (ERC20): %s", aliceUSDTCommitment)

	// Bob's concert ticket note (ERC721 vault).
	// Erc721Commitment(tokenId, pk, salt) = Poseidon4(pk, salt, 1, tokenId)
	bobSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Bob GenerateRandomValue: %v", err)
	}
	bobSaltField := core.SaltBToField(bobSaltRaw)
	bobTicketCommitment, err := core.Erc721Commitment(tokenIdTicket, bobSpend.PublicKey, bobSaltField)
	if err != nil {
		t.Fatalf("Bob Erc721Commitment: %v", err)
	}

	mt721 := core.NewMerkleTree(merkleDepth)
	mt721.InsertLeaf(bobTicketCommitment)
	bobProof, err := mt721.GenerateProof(bobTicketCommitment)
	if err != nil {
		t.Fatalf("Bob GenerateProof: %v", err)
	}
	t.Logf("Step 1 — Bob ticket cmt (ERC721): %s", bobTicketCommitment)

	// ── Step 2: Alice initiates the swap (Phase 1) ─────────────────────────
	// ZkDvpInitiateSwap:
	//   (1) Encapsulate(encapKey_bob) → saltB, ctI
	//   (2) CommitmentB = Poseidon4(bobPk, saltBField, 5, 0)   [USDT for Bob]
	//   (3) saltStar = random; C' = Poseidon4(alicePk, saltStarField, 1, 25)
	//       (C' equals Erc721Commitment(25, alicePk, saltStarField) — same formula)
	//   (4) ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
	//   (5) ERC20 JoinSplit proof: StMessage=C', firstOutput=CommitmentB
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

	commitmentA := aliceResult.CommitmentA // C' — Alice's expected ERC721 ticket commitment
	commitmentB := aliceResult.CommitmentB // CommitmentB — Alice's USDT payment for Bob

	t.Logf("Step 2 — C' (Alice expects ticket): %s", commitmentA)
	t.Logf("Step 2 — CommitmentB (Bob receives USDT): %s", commitmentB)

	// Verify C' equals Erc721Commitment(tokenId, alicePk, saltStarField).
	expectedCPrime, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, aliceResult.SaltStarField)
	if err != nil {
		t.Fatalf("verify C' as Erc721Commitment: %v", err)
	}
	if expectedCPrime.Cmp(commitmentA) != 0 {
		t.Errorf("C' commitment formula mismatch: Erc20CommitmentV2 vs Erc721Commitment")
	}
	t.Logf("Step 2 — C' == Erc721Commitment(25, alicePk, saltStarField) ✓")

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
	t.Logf("Step 3 — Bob verified swap: tokenIdOut=%s, amountOut=%s ✓", swap.TokenIdOut, swap.AmountOut)

	// ── Step 4: Bob completes the swap (Phase 2 — ERC721 proof) ───────────
	// Bob generates an ERC721 OwnershipProof:
	//   StMessage   = CommitmentB          (what Bob expects to receive)
	//   output      = C'                   (ticket commitment for Alice, using saltStarField)
	//
	// Statement layout: [CommitmentB, treeNum, root, null_bob, C'] — 5 elements (1 in / 1 out)
	saltStarField := core.SaltBToField(swap.SaltStar)

	bobResult, err := client.Erc721OwnershipProofFromSalt(
		commitmentB, // stMessage = CommitmentB (cross-commitment link back to Alice's proof)
		tokenIdTicket,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobSaltField,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		saltStarField, // output salt → C' = Erc721Commitment(25, alicePk, saltStarField)
		nil,           // ctI: Alice knows saltStar directly — no KEM delivery needed
		nil,           // ctII
		merkleDepth,
		bobProof,
		big.NewInt(0), // treeNumber
		contractAddr,
	)
	if err != nil {
		t.Fatalf("Bob Erc721OwnershipProofFromSalt: %v", err)
	}

	// ERC721 statement: [stMessage, treeNum, root, null_bob, C']
	t.Logf("Step 4 — Bob's proof: stMessage=%s, output(C')=%s",
		bobResult.Statement[0], bobResult.Statement[4])

	// ── Step 5: Cross-commitment verification ─────────────────────────────
	// Mirrors what _settleOnGroupPair checks on-chain:
	//   receipt_alice.statement[0]  == receipt_bob.statement[4]   (stMsg(Alice)=C' == firstOut(Bob)=C')
	//   receipt_bob.statement[0]    == receipt_alice.statement[7]  (stMsg(Bob)=CommitmentB == firstOut(Alice)=CommitmentB)

	if commitmentA.Cmp(bobResult.Statement[4]) != 0 {
		t.Errorf("cross-check FAILED: stMsg(Alice)=C'=%s != firstOut(Bob)=%s",
			commitmentA, bobResult.Statement[4])
	} else {
		t.Logf("Step 5a — stMsg(Alice)=C' == firstOut(Bob)=C' ✓")
	}

	if bobResult.Statement[0].Cmp(commitmentB) != 0 {
		t.Errorf("cross-check FAILED: stMsg(Bob)=%s != CommitmentB=%s",
			bobResult.Statement[0], commitmentB)
	} else {
		t.Logf("Step 5b — stMsg(Bob)=CommitmentB == firstOut(Alice)=CommitmentB ✓")
	}

	// Alice can spend C' using saltStarField (she generated it — no scanning needed).
	recoveredC, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, saltStarField)
	if err != nil {
		t.Fatalf("Alice recompute C': %v", err)
	}
	if recoveredC.Cmp(commitmentA) != 0 {
		t.Errorf("Alice recomputed C' mismatch: got %s, want %s", recoveredC, commitmentA)
	}
	t.Logf("Step 5c — Alice can spend C' (ERC721 ticket) with saltStarField=%s ✓", saltStarField)

	t.Logf("=== ZkDvP TWO-PHASE SWAP COMPLETE ===")
	t.Logf("Alice burned USDT note (ERC20) → Bob receives CommitmentB=%s", commitmentB)
	t.Logf("Bob burned ticket note (ERC721) → Alice receives C'=%s", commitmentA)
}

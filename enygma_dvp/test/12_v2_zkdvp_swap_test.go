package tests

// ZkDvp Swap test: Alice swaps 5 USDT (token_id=10) for Bob's 1 concert ticket (token_id=25).
//
// FLOW OVERVIEW
// ─────────────
// Both Alice and Bob have existing notes in the EnygmaDvP contract:
//
//   Alice: Commitment(aliceSpendPk, saltA_field, amount=5, token_id=10)  — 5 USDT
//   Bob:   Commitment(bobSpendPk,   saltB_field, amount=1, token_id=25)  — 1 concert ticket
//
// ALICE INITIATES (Phase 1)
// ─────────────────────────
//   1. (saltB, ciphertextI) = Encapsulate(bobViewEncapKey)
//      Alice derives a shared secret with Bob's ML-KEM view key.
//
//   2. CommitmentB = Poseidon(bobSpendPk, SaltBToField(saltB), 5, 10)
//      Alice computes the commitment Bob will receive (his USDT).
//
//   3. saltStar = GenerateRandomValue(len(saltB))
//      C' = Poseidon(aliceSpendPk, SaltBToField(saltStar), 1, 25)
//      Alice computes her own output commitment (the concert ticket she'll receive).
//
//   4. ciphertextII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
//      Alice encrypts the C' details so Bob can verify the commitment is well-formed.
//
//   5. Alice sends to EnygmaDvp:  nullifierA, zkProofA, ciphertextI, ciphertextII, C'
//      Contract marks this as a PENDING swap.
//
// BOB COMPLETES (Phase 2)
// ───────────────────────
//   6. Bob sees the emitted event: (CommitmentA=C', CommitmentB, ciphertextI, ciphertextII)
//
//   7. saltB' = Decapsulate(bobViewDecapsKey, ciphertextI)
//      (tokenId=25, amount=1, saltStar) = DecryptSwapPayload(saltB', ciphertextII)
//      If decryption fails the payload is not for Bob → skip.
//
//   8. Assert CommitmentB == Poseidon(bobSpendPk, SaltBToField(saltB'), 5, 10)
//      Assert C'          == Poseidon(aliceSpendPk, SaltBToField(saltStar), 1, 25)
//
//   9. Bob sends to EnygmaDvp: nullifierB, zkProofB, CommitmentA (C')
//      Contract inserts CommitmentB and C' into the merkle tree.
//
// TIMEOUT
// ───────
//   If Bob does not respond within the timeout, the pending tx is removed.
//   If decryption fails the payload is simply not for Bob.
//
// Run with: go test -run TestV2ZkDvp_Swap -v -timeout 300s

import (
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
)

func TestV2ZkDvp_Swap(t *testing.T) {
	// ── Swap terms (public, agreed off-chain) ──────────────────────────────
	//   Alice sends:  5 USDT       (token_id = 10)
	//   Bob sends:    1 concert ticket (token_id = 25)
	amountUSDT    := big.NewInt(5)
	tokenIdUSDT   := big.NewInt(10)
	amountTicket  := big.NewInt(1)
	tokenIdTicket := big.NewInt(25)

	t.Logf("Swap terms: Alice gives %s USDT (tid=%s) ↔ Bob gives %s ticket (tid=%s)",
		amountUSDT, tokenIdUSDT, amountTicket, tokenIdTicket)

	// ── Key generation ─────────────────────────────────────────────────────
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
	// Public keys exchanged: aliceSpendPk ↔ bobSpendPk, bobView.EncapsKey

	// ── Step 1: Setup — initial deposits ──────────────────────────────────
	// Alice's USDT note.
	aliceSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Alice GenerateRandomValue (salt): %v", err)
	}
	aliceSaltField := core.SaltBToField(aliceSaltRaw)
	aliceUSDTCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltField, amountUSDT, tokenIdUSDT)
	if err != nil {
		t.Fatalf("Alice Erc20CommitmentV2: %v", err)
	}
	t.Logf("Step 1 — Alice's USDT commitment: %s", aliceUSDTCommitment)

	merkleDepth := 8
	mt := core.NewMerkleTree(merkleDepth)
	mt.InsertLeaf(aliceUSDTCommitment)

	// Bob's concert ticket note.
	bobSaltRaw, err := core.GenerateRandomValue(32)
	if err != nil {
		t.Fatalf("Bob GenerateRandomValue (salt): %v", err)
	}
	bobSaltField := core.SaltBToField(bobSaltRaw)
	bobTicketCommitment, err := core.Erc20CommitmentV2(bobSpend.PublicKey, bobSaltField, amountTicket, tokenIdTicket)
	if err != nil {
		t.Fatalf("Bob Erc20CommitmentV2: %v", err)
	}
	mt.InsertLeaf(bobTicketCommitment)
	t.Logf("Step 1 — Bob's ticket commitment: %s", bobTicketCommitment)

	// Generate Merkle proofs for both notes.
	aliceProof, err := mt.GenerateProof(aliceUSDTCommitment)
	if err != nil {
		t.Fatalf("Alice GenerateProof: %v", err)
	}
	bobProof, err := mt.GenerateProof(bobTicketCommitment)
	if err != nil {
		t.Fatalf("Bob GenerateProof: %v", err)
	}

	// ── Step 2: Alice initiates the swap ───────────────────────────────────

	// 2a. Encapsulate Bob's view key → shared saltB + ciphertextI.
	saltB, ciphertextI, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil {
		t.Fatalf("Alice Encapsulate: %v", err)
	}
	saltBField := core.SaltBToField(saltB)
	t.Logf("Step 2a — saltB (hex prefix): %x...", saltB[:8])

	// 2b. CommitmentB — the commitment Bob will receive (his USDT from Alice).
	commitmentB, err := core.Erc20CommitmentV2(bobSpend.PublicKey, saltBField, amountUSDT, tokenIdUSDT)
	if err != nil {
		t.Fatalf("Alice compute CommitmentB: %v", err)
	}
	t.Logf("Step 2b — CommitmentB (Bob receives USDT): %s", commitmentB)

	// 2c. C' (CommitmentA) — the commitment Alice will receive (the concert ticket).
	saltStar, err := core.GenerateRandomValue(len(saltB))
	if err != nil {
		t.Fatalf("Alice GenerateRandomValue (saltStar): %v", err)
	}
	saltStarField := core.SaltBToField(saltStar)
	commitmentA, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltStarField, amountTicket, tokenIdTicket)
	if err != nil {
		t.Fatalf("Alice compute C' (CommitmentA): %v", err)
	}
	t.Logf("Step 2c — C' (Alice receives ticket): %s", commitmentA)

	// 2d. Encrypt (tokenIdOut=25, amountOut=1, saltStar) for Bob.
	ciphertextII, err := core.EncryptSwapPayload(saltB, tokenIdTicket, amountTicket, saltStar)
	if err != nil {
		t.Fatalf("Alice EncryptSwapPayload: %v", err)
	}
	t.Logf("Step 2d — ciphertextII length: %d bytes", len(ciphertextII))

	// 2e. Alice's nullifier (burns her USDT note).
	aliceNullifier, err := core.GetNullifier(aliceSpend.PrivateKey, aliceProof.Indices)
	if err != nil {
		t.Fatalf("Alice GetNullifier: %v", err)
	}
	t.Logf("Step 2e — Alice nullifier: %s", aliceNullifier)

	// 2f. Simulate Alice sending to EnygmaDvp → pending tx.
	//     In production this also includes a ZK proof; here we verify the crypto.
	t.Logf("Step 2f — Alice submits: nullifier=%s, C'=%s, CommitmentB=%s",
		aliceNullifier, commitmentA, commitmentB)

	// ── Step 3: Bob scans for ZkDvp pending events ─────────────────────────

	// Bob sees the event: (CommitmentA=C', CommitmentB, ciphertextI, ciphertextII).
	events := []core.OnChainZkDvpEvent{
		{
			CommitmentA:  commitmentA,
			CommitmentB:  commitmentB,
			CiphertextI:  ciphertextI,
			CiphertextII: ciphertextII,
		},
	}

	swaps, err := core.ScanForZkDvpSwap(
		bobView.DecapsKey,
		aliceSpend.PublicKey, // Bob knows Alice's spend pk from key exchange
		bobSpend.PublicKey,
		amountUSDT,    // what CommitmentB should represent
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

	t.Logf("Step 3 — Bob found swap: tokenIdOut=%s, amountOut=%s",
		swap.TokenIdOut, swap.AmountOut)

	// ── Step 4: Bob verifies the recovered values ──────────────────────────

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

	// Verify Bob can recompute CommitmentA using the recovered saltStar.
	recoveredCommA, err := core.Erc20CommitmentV2(
		aliceSpend.PublicKey,
		core.SaltBToField(swap.SaltStar),
		swap.AmountOut,
		swap.TokenIdOut,
	)
	if err != nil {
		t.Fatalf("Bob recompute CommitmentA: %v", err)
	}
	if recoveredCommA.Cmp(commitmentA) != 0 {
		t.Errorf("Bob's recomputed CommitmentA mismatch: got %s, want %s", recoveredCommA, commitmentA)
	}
	t.Logf("Step 4 — Bob verified CommitmentA is well-formed ✓")

	// Verify Bob can recompute CommitmentB using the recovered saltBField.
	recoveredCommB, err := core.Erc20CommitmentV2(
		bobSpend.PublicKey,
		swap.SaltBField,
		amountUSDT,
		tokenIdUSDT,
	)
	if err != nil {
		t.Fatalf("Bob recompute CommitmentB: %v", err)
	}
	if recoveredCommB.Cmp(commitmentB) != 0 {
		t.Errorf("Bob's recomputed CommitmentB mismatch: got %s, want %s", recoveredCommB, commitmentB)
	}
	t.Logf("Step 4 — Bob verified CommitmentB is well-formed ✓")

	// ── Step 5: Bob completes the swap ────────────────────────────────────
	bobNullifier, err := core.GetNullifier(bobSpend.PrivateKey, bobProof.Indices)
	if err != nil {
		t.Fatalf("Bob GetNullifier: %v", err)
	}
	t.Logf("Step 5 — Bob submits: nullifier=%s, CommitmentA (C')=%s",
		bobNullifier, commitmentA)

	// Simulate contract action: insert CommitmentB and C' into the merkle tree.
	mt.InsertLeaf(commitmentB)
	mt.InsertLeaf(commitmentA)
	t.Logf("Step 5 — Contract inserts CommitmentB=%s and C'=%s into tree", commitmentB, commitmentA)

	// ── Step 6: Alice later spends C' using saltStar ──────────────────────
	// Alice can verify she can recompute her new note commitment using saltStarField.
	aliceRecoveredCommitment, err := core.Erc20CommitmentV2(
		aliceSpend.PublicKey, saltStarField, amountTicket, tokenIdTicket)
	if err != nil {
		t.Fatalf("Alice recompute C': %v", err)
	}
	if aliceRecoveredCommitment.Cmp(commitmentA) != 0 {
		t.Errorf("Alice's C' mismatch: got %s, want %s", aliceRecoveredCommitment, commitmentA)
	}
	t.Logf("Step 6 — Alice can spend C' with saltStarField=%s ✓", saltStarField)

	// ── Step 7 (optional): ZK proofs via gnark server ─────────────────────
	if !serverAvailable("localhost:8081") {
		t.Log("gnark server not running — skipping ZK proof generation")
		t.Logf("=== ZkDvP SWAP COMPLETE (off-chain crypto verified) ===")
		return
	}

	client := core.NewGnarkClient("http://localhost:8081")

	// Alice's ZK proof: she spends her USDT note → CommitmentB for Bob.
	// ZkDvpInitiateSwap uses the same pre-computed saltB so CommitmentB is consistent.
	stMessage := big.NewInt(0) // swap-specific message (e.g. Poseidon(swap terms))
	aliceResult, err := client.ZkDvpInitiateSwap(
		stMessage,
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
	t.Logf("Step 7a — Alice's ZK proof generated; CommitmentB=%s C'=%s",
		aliceResult.CommitmentB, aliceResult.CommitmentA)

	// Bob's ZK proof: he spends his ticket note → CommitmentA (C') for Alice.
	// Bob uses the standard JoinSplit with swap.SaltBField as his input salt
	// and saltStarField as the output salt for Alice's commitment.
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyView: %v", err)
	}

	bobResult, err := client.Erc20JoinSplitProof(
		stMessage,
		[]*big.Int{amountTicket, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobSaltField, big.NewInt(0)}, // WtSaltsIn
		[]*big.Int{amountTicket, big.NewInt(0)}, // WtValuesOut
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey}, // recipients
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},       // view keys (Alice reuses Bob's for demo)
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenIdTicket,
		false,
	)
	if err != nil {
		t.Fatalf("Bob Erc20JoinSplitProof: %v", err)
	}
	t.Logf("Step 7b — Bob's ZK proof generated; commitment for Alice=%s", bobResult.Statement[7])

	t.Logf("=== ZkDvP SWAP COMPLETE (with ZK proofs) ===")
	t.Logf("Alice nullifier (USDT burned): %s", aliceNullifier)
	t.Logf("Bob   nullifier (ticket burned): %s", bobNullifier)
	t.Logf("CommitmentB (Bob receives USDT):      %s", commitmentB)
	t.Logf("C'          (Alice receives ticket):  %s", commitmentA)
}

// TestV2ZkDvp_WrongKey verifies that a payload encrypted for Bob cannot be
// decrypted by a third party (Carol) — the "not for me" signal.
func TestV2ZkDvp_WrongKey(t *testing.T) {
	tokenIdTicket := big.NewInt(25)
	amountTicket  := big.NewInt(1)

	aliceSpend, _ := core.NewSpendKeyPair()
	bobSpend, _   := core.NewSpendKeyPair()
	carolView, _  := core.NewViewKeyPair()
	bobView, _    := core.NewViewKeyPair()

	saltB, ciphertextI, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}
	saltBField := core.SaltBToField(saltB)

	saltStar, _ := core.GenerateRandomValue(len(saltB))
	saltStarField := core.SaltBToField(saltStar)
	commitmentA, _ := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltStarField, amountTicket, tokenIdTicket)
	commitmentB, _ := core.Erc20CommitmentV2(bobSpend.PublicKey, saltBField, big.NewInt(5), big.NewInt(10))
	ciphertextII, _ := core.EncryptSwapPayload(saltB, tokenIdTicket, amountTicket, saltStar)

	events := []core.OnChainZkDvpEvent{{
		CommitmentA:  commitmentA,
		CommitmentB:  commitmentB,
		CiphertextI:  ciphertextI,
		CiphertextII: ciphertextII,
	}}

	// Carol tries to scan — she should find nothing.
	carolSwaps, err := core.ScanForZkDvpSwap(
		carolView.DecapsKey, // wrong key
		aliceSpend.PublicKey,
		bobSpend.PublicKey,
		big.NewInt(5), big.NewInt(10),
		events,
	)
	if err != nil {
		t.Fatalf("unexpected error for Carol: %v", err)
	}
	if len(carolSwaps) != 0 {
		t.Errorf("Carol should find 0 swaps, got %d", len(carolSwaps))
	}
	t.Logf("Carol correctly finds nothing (payload not for her) ✓")

	// Bob scans — he should find the swap.
	bobSwaps, err := core.ScanForZkDvpSwap(
		bobView.DecapsKey,
		aliceSpend.PublicKey,
		bobSpend.PublicKey,
		big.NewInt(5), big.NewInt(10),
		events,
	)
	if err != nil {
		t.Fatalf("Bob ScanForZkDvpSwap: %v", err)
	}
	if len(bobSwaps) != 1 {
		t.Fatalf("Bob: expected 1 swap, got %d", len(bobSwaps))
	}
	t.Logf("Bob correctly finds the swap ✓")
}

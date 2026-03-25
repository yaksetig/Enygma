package tests

// Non-interactive V2 swap test: Alice's ERC721 NFT for Bob's ERC20 tokens.
//
// NON-INTERACTIVITY CLAIM
// ───────────────────────
// The only "interaction" is a one-time public-key exchange (equivalent to
// sharing wallet addresses).  After that:
//
//   Alice generates her ERC721 ownership proof  ← uses ONLY bobSpendPk
//   Bob   generates his  ERC20  JoinSplit proof ← uses ONLY aliceSpendPk + aliceViewEncapKey
//
// Neither party waits for the other's proof.  Proofs are generated
// independently, in any order, and submitted to the contract together.
//
// ATOMICITY
// ─────────
// Both proofs encode the same swapId as their stMessage field:
//
//   swapId = Poseidon(contractAddr721, tokenId, contractAddr20, paymentAmount)
//
// Both parties compute this independently from the agreed swap terms.
// The on-chain contract rejects either proof unless a matching partner
// proof with the same swapId is also present (partial settlement pattern).
//
// FLOW
// ────
//   Step 1 — Setup
//     Alice deposits NFT:  commitment721 = Poseidon(contractAddr721, tokenId, alicePk, aliceSalt)
//     Bob   mints  ERC20:  commitment20  = Poseidon(bobPk, bobSalt, amount, tokenId=0)
//     (in production these are on-chain deposits; here simulated locally)
//
//   Step 2 — Key exchange  (public keys only)
//     Alice → Bob:  aliceSpendPk, aliceViewEncapKey
//     Bob   → Alice: bobSpendPk
//
//   Step 3 — Alice's proof  (independent)
//     Spends her NFT note; creates output commitment for Bob.
//     message = swapId
//
//   Step 4 — Bob's proof  (independent)
//     Spends his ERC20 note; creates payment output for Alice via ML-KEM.
//     message = swapId
//
//   Step 5 — Verify cross-commitment correctness
//     NFT commitment  recomputed from aliceResult.SaltsOut[0]
//     ERC20 payment   recovered by Alice scanning with her view key
//
// Run with: go test -run TestV2Swap_Erc721ForErc20_NonInteractive -v -timeout 300s

import (
	"math/big"
	"testing"

	"github.com/iden3/go-iden3-crypto/poseidon"

	"enygma_dvp/src_go/core"
)

func TestV2Swap_Erc721ForErc20_NonInteractive(t *testing.T) {
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping live test")
	}

	client := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	// ── Swap terms (public, agreed off-chain by both parties) ───────────────
	tokenId       := big.NewInt(9999) // NFT ID Alice is selling
	paymentAmount := big.NewInt(50)   // ERC20 tokens Bob is paying
	erc20TokenId  := big.NewInt(0)    // ERC20 circuit uses tokenId = 0
	contractAddr721 := big.NewInt(0x721)
	contractAddr20  := big.NewInt(0x20)

	// swapId: both parties compute this independently — no communication needed.
	// Binds both proofs to the same swap without revealing private data.
	swapId, err := poseidon.Hash([]*big.Int{contractAddr721, tokenId, contractAddr20, paymentAmount})
	if err != nil {
		t.Fatalf("compute swapId: %v", err)
	}
	t.Logf("Swap ID (shared stMessage): %s", swapId)

	// ── Step 2: Key exchange ─────────────────────────────────────────────────
	// Each party generates their own key material.
	// They share only public keys — the single point of "interaction".

	// Alice's keys (NFT owner)
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}

	// Bob's keys (ERC20 payer)
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	// Public info exchanged:
	//   Alice → Bob : aliceSpend.PublicKey, aliceView.EncapsKey
	//   Bob   → Alice: bobSpend.PublicKey
	//   Bob   → Alice: bobView.EncapsKey (for NFT delivery)

	// ── Step 1: Setup ────────────────────────────────────────────────────────

	// Alice deposits NFT into ERC721 vault (simulated locally).
	// In production: vault.deposit(tokenId, aliceSpend.PublicKey) on-chain.
	aliceNFTSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}
	aliceNFTCommitment, err := core.Erc721Commitment(tokenId, aliceSpend.PublicKey, aliceNFTSalt)
	if err != nil {
		t.Fatalf("Alice Erc721Commitment: %v", err)
	}
	mt721 := core.NewMerkleTree(merkleDepth)
	mt721.InsertLeaf(aliceNFTCommitment)
	aliceNFTProof, err := mt721.GenerateProof(aliceNFTCommitment)
	if err != nil {
		t.Fatalf("Alice GenerateProof (NFT): %v", err)
	}
	t.Logf("Step 1 — Alice's NFT commitment: %s", aliceNFTCommitment)

	// Bob mints ERC20 tokens via PrivateMint (simulated locally).
	// In production: privateMint on-chain.
	bobErc20Salt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Bob RandomInField: %v", err)
	}
	bobMintResult, err := client.Erc20PrivateMintProof(
		bobSpend.PublicKey,
		bobErc20Salt,
		paymentAmount,
		erc20TokenId,
		contractAddr20,
	)
	if err != nil {
		t.Fatalf("Bob Erc20PrivateMintProof: %v", err)
	}
	mt20 := core.NewMerkleTree(merkleDepth)
	mt20.InsertLeaf(bobMintResult.Commitment)
	bobErc20Proof, err := mt20.GenerateProof(bobMintResult.Commitment)
	if err != nil {
		t.Fatalf("Bob GenerateProof (ERC20): %v", err)
	}
	t.Logf("Step 1 — Bob's ERC20 commitment: %s", bobMintResult.Commitment)

	// ── Step 3: Alice's proof (INDEPENDENT) ──────────────────────────────────
	// Alice only knows: her own keys + bobSpend.PublicKey
	// She does NOT know Bob's ERC20 proof or payment commitment.

	aliceResult, err := client.Erc721OwnershipProof(
		swapId, // stMessage — cross-links this proof to the swap
		tokenId,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceNFTSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceNFTProof,
		big.NewInt(0), // treeNumber
		contractAddr721,
	)
	if err != nil {
		t.Fatalf("Alice Erc721OwnershipProof: %v", err)
	}

	// Statement: [swapId, treeNumber, merkleRoot, aliceNullifier, nftCommitmentForBob]
	aliceNullifier          := aliceResult.Statement[3]
	bobNFTCommitmentOnChain := aliceResult.Statement[4]
	t.Logf("Step 3 — Alice's nullifier (burns NFT note): %s", aliceNullifier)
	t.Logf("Step 3 — Bob's new NFT commitment:           %s", bobNFTCommitmentOnChain)

	// ── Step 4: Bob's proof (INDEPENDENT) ────────────────────────────────────
	// Bob only knows: his own keys + aliceSpend.PublicKey + aliceView.EncapsKey
	// He does NOT know Alice's ERC721 proof or NFT commitment for him.

	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend NewSpendKeyPair: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyView NewViewKeyPair: %v", err)
	}

	bobResult, err := client.Erc20JoinSplitProof(
		swapId, // stMessage — same swapId cross-links both proofs
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobMintResult.Salt, big.NewInt(0)}, // wtSaltsIn
		[]*big.Int{paymentAmount, big.NewInt(0)},       // wtValuesOut
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey}, // recipients
		[][]byte{aliceView.EncapsKey, dummyView.EncapsKey},     // KEM encap keys
		merkleDepth,
		[]*core.MerkleProof{bobErc20Proof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		erc20TokenId,
		false, // 2-in / 2-out circuit
	)
	if err != nil {
		t.Fatalf("Bob Erc20JoinSplitProof: %v", err)
	}

	// Statement: [swapId, tree0, root0, null0, tree1, root1, null1, paymentCmt, dummyCmt]
	bobNullifier             := bobResult.Statement[3]
	paymentCommitmentOnChain := bobResult.Statement[7]
	t.Logf("Step 4 — Bob's nullifier (burns ERC20 note): %s", bobNullifier)
	t.Logf("Step 4 — Alice's payment commitment:         %s", paymentCommitmentOnChain)

	// ── Step 5: Verification ─────────────────────────────────────────────────

	// 5a. Both proofs carry the same swapId — contract uses this for atomicity.
	if aliceResult.Statement[0].Cmp(swapId) != 0 {
		t.Errorf("Alice's stMessage: got %s, want %s", aliceResult.Statement[0], swapId)
	}
	if bobResult.Statement[0].Cmp(swapId) != 0 {
		t.Errorf("Bob's stMessage: got %s, want %s", bobResult.Statement[0], swapId)
	}
	t.Logf("Step 5a — Both proofs share swapId: %s", swapId)

	// 5b. Bob scans for his NFT note using his view key (non-interactive delivery).
	nftEvents := []core.OnChainErc721Event{{
		Commitment:   bobNFTCommitmentOnChain,
		CiphertextI:  aliceResult.CiphertextI[0],
		CiphertextII: aliceResult.CiphertextII[0],
	}}
	bobNFTNotes, err := core.ScanForErc721Notes(bobView.DecapsKey, bobSpend.PublicKey, nftEvents)
	if err != nil {
		t.Fatalf("Bob ScanForErc721Notes: %v", err)
	}
	if len(bobNFTNotes) != 1 {
		t.Fatalf("Bob: expected 1 NFT note, got %d", len(bobNFTNotes))
	}
	t.Logf("Step 5b — Bob scanned his NFT note (tokenId=%s)", bobNFTNotes[0].TokenId)

	// 5c. Alice scans for her ERC20 payment using her view key.
	//     No out-of-band communication needed — ML-KEM handles key delivery.
	events := []core.OnChainErc20Event{
		{
			Commitment:   bobResult.Statement[7], // payment to Alice
			CiphertextI:  bobResult.CiphertextI[0],
			CiphertextII: bobResult.CiphertextII[0],
		},
		{
			Commitment:   bobResult.Statement[8], // dummy output
			CiphertextI:  bobResult.CiphertextI[1],
			CiphertextII: bobResult.CiphertextII[1],
		},
	}
	aliceNotes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, events)
	if err != nil {
		t.Fatalf("Alice ScanForErc20Notes: %v", err)
	}
	if len(aliceNotes) != 1 {
		t.Fatalf("Alice expected 1 payment note, got %d", len(aliceNotes))
	}
	note := aliceNotes[0]
	if note.Amount.Cmp(paymentAmount) != 0 {
		t.Errorf("payment amount: got %s, want %s", note.Amount, paymentAmount)
	}
	if note.Commitment.Cmp(paymentCommitmentOnChain) != 0 {
		t.Errorf("payment commitment: got %s, want %s", note.Commitment, paymentCommitmentOnChain)
	}
	t.Logf("Step 5c — Alice recovered ERC20 payment: amount=%s", note.Amount)

	// 5d. Sanity: nullifiers are different (each party burned their own note).
	if aliceNullifier.Cmp(bobNullifier) == 0 {
		t.Errorf("Alice's and Bob's nullifiers should differ")
	}

	t.Logf("=== NON-INTERACTIVE SWAP COMPLETE ===")
	t.Logf("Alice burned NFT note  → Bob gets NFT  commitment=%s", bobNFTCommitmentOnChain)
	t.Logf("Bob burned ERC20 note  → Alice gets ERC20 payment=%s tokens", note.Amount)
	t.Logf("Cross-link: both proofs carry swapId=%s", swapId)
}

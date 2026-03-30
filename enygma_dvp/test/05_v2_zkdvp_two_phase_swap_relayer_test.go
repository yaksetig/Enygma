package tests

// Relayer variant of 05_v2_zkdvp_two_phase_swap_test.go.
//
// Protocol logic is identical to TestV2ZkDvp_TwoPhaseSwapOnChain.
// The only difference is Step 6: instead of calling dvp.Transact("swap", ...)
// directly, this test uses endpoints.Swap() — the relayer library — to submit
// both proofs. This exercises the endpoints.ProofReceipt type and validates
// that it is ABI-compatible with the on-chain EnygmaDvp.swap() function.
//
// Run with:
//    go test -run TestV2ZkDvp_TwoPhaseSwapOnChain_WithRelayer -v -timeout 600s

import (
	"context"
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
	"enygma_dvp/src_go/core/endpoints"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// toEndpointsSnarkProof converts the 8-element decimal proof strings from the
// gnark server into an endpoints.SnarkProof ready for ABI encoding via the
// relayer package.
//
// Gnark handler output order (proofRemix):
//
//	[Ax, Ay, BX_A1(imag), BX_A0(real), BY_A1(imag), BY_A0(real), Cx, Cy]
func toEndpointsSnarkProof(t *testing.T, proof []string) endpoints.SnarkProof {
	t.Helper()
	if len(proof) != 8 {
		t.Fatalf("expected 8 proof elements, got %d", len(proof))
	}
	vals := make([]*big.Int, 8)
	for i, s := range proof {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("invalid proof element %d: %q", i, s)
		}
		vals[i] = n
	}
	return endpoints.SnarkProof{
		A: endpoints.G1Point{X: vals[0], Y: vals[1]},
		B: endpoints.G2Point{
			X: [2]*big.Int{vals[2], vals[3]},
			Y: [2]*big.Int{vals[4], vals[5]},
		},
		C: endpoints.G1Point{X: vals[6], Y: vals[7]},
	}
}

func TestV2ZkDvp_TwoPhaseSwapOnChain_WithRelayer(t *testing.T) {
	if !chainAvailable() {
		t.Skip("Hardhat node not running on localhost:8545 — skipping on-chain test")
	}
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping on-chain test")
	}

	ctx := context.Background()

	client, err := ethclient.Dial(hardhatRPC)
	if err != nil {
		t.Fatalf("ethclient.Dial: %v", err)
	}
	defer client.Close()

	receipts := loadOnchainReceipts(t)
	erc20VaultAddr  := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc721VaultAddr := common.HexToAddress(receipts["Erc721CoinVault"].ContractAddress)
	dvpAddr         := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)
	erc20Addr       := common.HexToAddress(receipts["ERC20"].ContractAddress)
	erc721Addr      := common.HexToAddress(receipts["ERC721"].ContractAddress)

	erc20VaultABI  := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc721VaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc721CoinVault.sol/Erc721CoinVault.json")
	erc20ABI       := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")
	erc721ABI      := loadOnchainABI(t, "erc721/contracts/RaylsERC721.sol/RaylsERC721.json")
	dvpABI         := loadOnchainABI(t, "core/contracts/EnygmaDvp.sol/EnygmaDvp.json")

	erc20Vault  := bind.NewBoundContract(erc20VaultAddr, erc20VaultABI, client, client, client)
	erc721Vault := bind.NewBoundContract(erc721VaultAddr, erc721VaultABI, client, client, client)
	erc20Token  := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)
	erc721Token := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient   := core.NewGnarkClient("http://localhost:8081")
	merkleDepth   := 8
	tokenIdUSDT   := big.NewInt(0)
	amountUSDT    := big.NewInt(5)
	tokenIdTicket := big.NewInt(25)
	amountTicket  := big.NewInt(1)
	contractAddr  := big.NewInt(0)

	t.Logf("Swap (relayer): Alice gives %s USDT (ERC20) ↔ Bob gives ticket ERC721 (tokenId=%s)",
		amountUSDT, tokenIdTicket)

	// ── Key pairs ─────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Alice NewSpendKeyPair: %v", err) }
	aliceView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Alice NewViewKeyPair: %v", err) }
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Bob NewSpendKeyPair: %v", err) }
	bobView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Bob NewViewKeyPair: %v", err) }
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("dummySpend: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: Alice mints ERC20 + depositV2 (5 USDT)
	// ─────────────────────────────────────────────────────────────────────────
	mintERC20Tx, err := erc20Token.Transact(auth, "mint", alice, new(big.Int).Mul(amountUSDT, big.NewInt(10)))
	if err != nil { t.Fatalf("ERC20.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintERC20Tx); err != nil { t.Fatalf("wait ERC20 mint: %v", err) }

	approveERC20Tx, err := erc20Token.Transact(auth, "approve", erc20VaultAddr, amountUSDT)
	if err != nil { t.Fatalf("ERC20.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveERC20Tx); err != nil { t.Fatalf("wait ERC20 approve: %v", err) }

	aliceSaltB, aliceCapsule, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil { t.Fatalf("Alice Encapsulate (deposit): %v", err) }
	aliceSaltField := core.SaltBToField(aliceSaltB)

	aliceUSDTCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltField, amountUSDT, tokenIdUSDT)
	if err != nil { t.Fatalf("Alice Erc20CommitmentV2: %v", err) }

	aliceCtII, err := core.EncryptPayload(aliceSaltB, tokenIdUSDT, amountUSDT)
	if err != nil { t.Fatalf("Alice EncryptPayload: %v", err) }

	depositERC20Tx, err := erc20Vault.Transact(auth, "depositV2",
		[]*big.Int{amountUSDT, aliceUSDTCommitment}, aliceCapsule, aliceCtII)
	if err != nil { t.Fatalf("erc20Vault.depositV2: %v", err) }
	depositERC20Receipt, err := bind.WaitMined(ctx, client, depositERC20Tx)
	if err != nil { t.Fatalf("wait ERC20 depositV2: %v", err) }
	t.Logf("Step 1 — ERC20 depositV2 mined (block %d, gas %d)", depositERC20Receipt.BlockNumber, depositERC20Receipt.GasUsed)

	mt20 := loadVaultMerkleTree(t, client, erc20VaultAddr, merkleDepth)
	aliceProof, err := mt20.GenerateProof(aliceUSDTCommitment)
	if err != nil { t.Fatalf("GenerateProof for Alice ERC20: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Bob mints ERC721 + deposit (concert ticket)
	// ─────────────────────────────────────────────────────────────────────────
	mintNFTTx, err := erc721Token.Transact(auth, "mint", alice, tokenIdTicket)
	if err != nil { t.Fatalf("ERC721.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintNFTTx); err != nil { t.Fatalf("wait ERC721 mint: %v", err) }

	approveNFTTx, err := erc721Token.Transact(auth, "approve", erc721VaultAddr, tokenIdTicket)
	if err != nil { t.Fatalf("ERC721.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveNFTTx); err != nil { t.Fatalf("wait ERC721 approve: %v", err) }

	bobTicketSalt, err := core.RandomInField()
	if err != nil { t.Fatalf("Bob RandomInField (ticket salt): %v", err) }

	bobTicketCommitment, err := core.Erc721Commitment(tokenIdTicket, bobSpend.PublicKey, bobTicketSalt)
	if err != nil { t.Fatalf("Bob Erc721Commitment: %v", err) }

	depositNFTTx, err := erc721Vault.Transact(auth, "deposit",
		[]*big.Int{tokenIdTicket, bobTicketCommitment})
	if err != nil { t.Fatalf("erc721Vault.deposit: %v", err) }
	depositNFTReceipt, err := bind.WaitMined(ctx, client, depositNFTTx)
	if err != nil { t.Fatalf("wait ERC721 deposit: %v", err) }
	t.Logf("Step 2 — ERC721 deposit mined (block %d, gas %d)", depositNFTReceipt.BlockNumber, depositNFTReceipt.GasUsed)

	mt721 := loadVaultMerkleTree(t, client, erc721VaultAddr, merkleDepth)
	bobProof, err := mt721.GenerateProof(bobTicketCommitment)
	if err != nil { t.Fatalf("GenerateProof for Bob ERC721: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Alice computes ZkDvP artefacts and generates ERC20 JoinSplit proof
	// ─────────────────────────────────────────────────────────────────────────
	saltB, ctI, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil { t.Fatalf("Alice Encapsulate (KEM for Bob): %v", err) }
	saltBField := core.SaltBToField(saltB)

	commitmentB, err := core.Erc20CommitmentV2(bobSpend.PublicKey, saltBField, amountUSDT, tokenIdUSDT)
	if err != nil { t.Fatalf("Alice compute CommitmentB: %v", err) }

	saltStar, err := core.GenerateRandomValue(len(saltB))
	if err != nil { t.Fatalf("Alice GenerateRandomValue (saltStar): %v", err) }
	saltStarField := core.SaltBToField(saltStar)
	commitmentA, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, saltStarField)
	if err != nil { t.Fatalf("Alice compute C': %v", err) }

	ctII, err := core.EncryptSwapPayload(saltB, tokenIdTicket, amountTicket, saltStar)
	if err != nil { t.Fatalf("Alice EncryptSwapPayload: %v", err) }

	t.Logf("Step 3 — C' (Alice expects ticket): %s", commitmentA)
	t.Logf("Step 3 — CommitmentB (Bob receives USDT): %s", commitmentB)

	aliceERC20Result, err := gnarkClient.Erc20JoinSplitProofFromSalts(
		commitmentA,
		[]*big.Int{amountUSDT, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSaltField, big.NewInt(0)},
		[]*big.Int{amountUSDT, big.NewInt(0)},
		[]*big.Int{bobSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltBField, big.NewInt(0)},
		[][]byte{ctI, nil},
		[][]byte{ctII, nil},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenIdUSDT,
		false,
	)
	if err != nil { t.Fatalf("Alice Erc20JoinSplitProofFromSalts: %v", err) }

	if aliceERC20Result.Statement[0].Cmp(commitmentA) != 0 {
		t.Fatalf("Alice statement[0] != C': got %s", aliceERC20Result.Statement[0])
	}
	if aliceERC20Result.Statement[7].Cmp(commitmentB) != 0 {
		t.Fatalf("Alice statement[7] != CommitmentB: got %s", aliceERC20Result.Statement[7])
	}
	t.Logf("Step 3 — Alice ERC20 proof generated ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Bob scans the ZkDvP event and recovers his swap details
	// ─────────────────────────────────────────────────────────────────────────
	events := []core.OnChainZkDvpEvent{
		{
			CommitmentA:  commitmentA,
			CommitmentB:  commitmentB,
			CiphertextI:  ctI,
			CiphertextII: ctII,
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
	if err != nil { t.Fatalf("Bob ScanForZkDvpSwap: %v", err) }
	if len(swaps) != 1 { t.Fatalf("Bob: expected 1 swap, got %d", len(swaps)) }
	swap := swaps[0]
	t.Logf("Step 4 — Bob verified swap: tokenIdOut=%s, amountOut=%s ✓", swap.TokenIdOut, swap.AmountOut)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Bob generates ERC721 OwnershipProof
	// ─────────────────────────────────────────────────────────────────────────
	recvSaltStarField := core.SaltBToField(swap.SaltStar)

	bobERC721Result, err := gnarkClient.Erc721OwnershipProofFromSalt(
		commitmentB,
		tokenIdTicket,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobTicketSalt,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		recvSaltStarField,
		nil,
		nil,
		merkleDepth,
		bobProof,
		big.NewInt(0),
		contractAddr,
	)
	if err != nil { t.Fatalf("Bob Erc721OwnershipProofFromSalt: %v", err) }

	if aliceERC20Result.Statement[0].Cmp(bobERC721Result.Statement[4]) != 0 {
		t.Fatalf("cross-check FAILED: Alice stMsg C' != Bob output C'")
	}
	if bobERC721Result.Statement[0].Cmp(aliceERC20Result.Statement[7]) != 0 {
		t.Fatalf("cross-check FAILED: Bob stMsg CommitmentB != Alice output CommitmentB")
	}
	t.Logf("Step 5 — Bob ERC721 proof generated; cross-commitment consistency verified ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Relayer submits swap via endpoints.Swap()
	//
	// This replaces the direct dvp.Transact("swap", ...) call in the original
	// test. The relayer collects both ProofReceipts and submits them in one
	// atomic transaction using its own Ethereum key (auth).
	// ─────────────────────────────────────────────────────────────────────────
	alicePaymentReceipt := endpoints.ProofReceipt{
		Proof:           toEndpointsSnarkProof(t, aliceERC20Result.Proof),
		Statement:       aliceERC20Result.ContractStatement(),
		NumberOfInputs:  big.NewInt(int64(aliceERC20Result.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(aliceERC20Result.NumberOfOutputs)),
	}

	bobDeliveryReceipt := endpoints.ProofReceipt{
		Proof:           toEndpointsSnarkProof(t, bobERC721Result.Proof),
		Statement:       bobERC721Result.Statement,
		NumberOfInputs:  big.NewInt(int64(bobERC721Result.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(bobERC721Result.NumberOfOutputs)),
	}

	t.Logf("Step 6 — Relayer calling endpoints.Swap(alicePaymentReceipt, bobDeliveryReceipt, 0, 1)")
	relayerCommitments, err := endpoints.Swap(
		client, auth, dvpABI, dvpAddr,
		alicePaymentReceipt,
		bobDeliveryReceipt,
		big.NewInt(0), // paymentVaultId = ERC20 vault
		big.NewInt(1), // deliveryVaultId = ERC721 vault
	)
	if err != nil { t.Fatalf("endpoints.Swap: %v", err) }
	t.Logf("Step 6 — Relayer swap submitted ✓")
	t.Logf("  Relayer commitments[0] CommitmentB (Bob's USDT note): %s", relayerCommitments[0])
	t.Logf("  Relayer commitments[1] change note:                   %s", relayerCommitments[1])
	t.Logf("  Relayer commitments[2] C' (Alice's ticket note):      %s", relayerCommitments[2])

	// ─────────────────────────────────────────────────────────────────────────
	// Step 7: Verify commitments returned by the relayer
	// ─────────────────────────────────────────────────────────────────────────

	// endpoints.Swap returns [paymentStatement[7], paymentStatement[8], deliveryStatement[4]]
	// = [CommitmentB, change, C']
	if relayerCommitments[0].Cmp(commitmentB) != 0 {
		t.Errorf("relayerCommitments[0] != CommitmentB: got %s, want %s",
			relayerCommitments[0], commitmentB)
	}
	if relayerCommitments[2].Cmp(commitmentA) != 0 {
		t.Errorf("relayerCommitments[2] != C': got %s, want %s",
			relayerCommitments[2], commitmentA)
	}

	// Bob verifies he can reconstruct CommitmentB from the relayer output.
	recoveredCommB, err := core.Erc20CommitmentV2(bobSpend.PublicKey, swap.SaltBField, amountUSDT, tokenIdUSDT)
	if err != nil { t.Fatalf("Bob recompute CommitmentB: %v", err) }
	if recoveredCommB.Cmp(relayerCommitments[0]) != 0 {
		t.Errorf("Bob's recomputed CommitmentB mismatch: got %s, want %s", recoveredCommB, relayerCommitments[0])
	}
	t.Logf("Step 7a — Bob can spend CommitmentB (USDT note) ✓")

	// Alice verifies she can reconstruct C' from the relayer output.
	recoveredC, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, saltStarField)
	if err != nil { t.Fatalf("Alice recompute C': %v", err) }
	if recoveredC.Cmp(relayerCommitments[2]) != 0 {
		t.Errorf("Alice recomputed C' mismatch: got %s, want %s", recoveredC, relayerCommitments[2])
	}
	t.Logf("Step 7b — Alice can spend C' (ERC721 ticket note) ✓")

	t.Logf("=== ZkDvP TWO-PHASE SWAP WITH RELAYER COMPLETE ===")
	t.Logf("Alice burned USDT note → Bob receives CommitmentB=%s", relayerCommitments[0])
	t.Logf("Bob burned ticket note → Alice receives C'=%s", relayerCommitments[2])
}

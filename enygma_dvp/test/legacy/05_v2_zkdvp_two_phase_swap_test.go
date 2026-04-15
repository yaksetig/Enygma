// Deprecated: This file is legacy and will not be used in the current version.
package tests

// On-chain ZkDvP two-phase atomic swap: Alice swaps 5 USDT (ERC20) for Bob's concert ticket (ERC721).
//
// CROSS-COMMITMENT ATOMICITY
// ───────────────────────────
//   stMessage(Alice) = C'           (Alice's expected ERC721 ticket commitment)
//   firstOutput(Alice) = CommitmentB (Alice's USDT payment for Bob)
//   stMessage(Bob)   = CommitmentB  (links Bob's proof to Alice's pending proof)
//   firstOutput(Bob) = C'           (Bob delivers exactly the ticket commitment Alice pre-computed)
//
//   On-chain _settleOnGroupPair verifies:
//     alicePaymentReceipt.statement[0]  == bobDeliveryReceipt.statement[4]   // C' == C'
//     bobDeliveryReceipt.statement[0]   == alicePaymentReceipt.statement[7]  // CommitmentB == CommitmentB
//
// FLOW
// ────
//   Step 1 — Alice deposits 5 USDT into ERC20 vault (depositV2).
//   Step 2 — Bob deposits concert ticket into ERC721 vault (deposit).
//   Step 3 — Alice computes ZkDvP artefacts:
//               Encapsulate(bobView) → saltB, ctI
//               CommitmentB = Erc20CommitmentV2(bobPk, saltBField, 5, 0)
//               saltStar → C' = Erc721Commitment(25, alicePk, saltStarField)
//               ctII = EncryptSwapPayload(saltB, tokenId=25, 1, saltStar)
//               Alice ERC20 JoinSplit proof (stMsg=C', output0=CommitmentB)
//   Step 4 — Bob scans the ZkDvP event, decapsulates ctI, decrypts ctII.
//   Step 5 — Bob generates ERC721 OwnershipProof (stMsg=CommitmentB, output=C').
//   Step 6 — EnygmaDvp.swap(alicePaymentReceipt, bobDeliveryReceipt, 0, 1).
//   Step 7 — Verify on-chain events; both parties verify their new notes.
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2ZkDvp_TwoPhaseSwapOnChain -v -timeout 600s

import (
	"context"
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestV2ZkDvp_TwoPhaseSwapOnChain(t *testing.T) {
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
	dvp         := bind.NewBoundContract(dvpAddr, dvpABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient   := core.NewGnarkClient("http://localhost:8081")
	merkleDepth   := 8
	tokenIdUSDT   := big.NewInt(0)   // ERC20 circuit convention: tokenId=0
	amountUSDT    := big.NewInt(5)
	tokenIdTicket := big.NewInt(25)  // concert ticket ERC721 tokenId
	amountTicket  := big.NewInt(1)
	contractAddr  := big.NewInt(0)   // ERC721 circuit witness

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	nullifierSig  := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))

	t.Logf("Swap: Alice gives %s USDT (ERC20) ↔ Bob gives ticket ERC721 (tokenId=%s)",
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
	t.Logf("  Alice USDT commitment: %s", aliceUSDTCommitment)

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
	t.Logf("  Bob ticket commitment: %s", bobTicketCommitment)

	mt721 := loadVaultMerkleTree(t, client, erc721VaultAddr, merkleDepth)
	bobProof, err := mt721.GenerateProof(bobTicketCommitment)
	if err != nil { t.Fatalf("GenerateProof for Bob ERC721: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Alice computes ZkDvP artefacts and generates ERC20 JoinSplit proof
	//   stMessage = C' (Alice's expected ticket commitment)
	//   output[0] = CommitmentB (Alice's USDT payment for Bob)
	// ─────────────────────────────────────────────────────────────────────────
	saltB, ctI, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil { t.Fatalf("Alice Encapsulate (KEM for Bob): %v", err) }
	saltBField := core.SaltBToField(saltB)

	// CommitmentB = Erc20CommitmentV2(bobPk, saltBField, amountUSDT, tokenIdUSDT)
	commitmentB, err := core.Erc20CommitmentV2(bobSpend.PublicKey, saltBField, amountUSDT, tokenIdUSDT)
	if err != nil { t.Fatalf("Alice compute CommitmentB: %v", err) }

	// C' = Erc721Commitment(tokenIdTicket, alicePk, saltStarField)
	saltStar, err := core.GenerateRandomValue(len(saltB))
	if err != nil { t.Fatalf("Alice GenerateRandomValue (saltStar): %v", err) }
	saltStarField := core.SaltBToField(saltStar)
	commitmentA, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, saltStarField)
	if err != nil { t.Fatalf("Alice compute C': %v", err) }

	ctII, err := core.EncryptSwapPayload(saltB, tokenIdTicket, amountTicket, saltStar)
	if err != nil { t.Fatalf("Alice EncryptSwapPayload: %v", err) }

	t.Logf("Step 3 — C' (Alice expects ticket): %s", commitmentA)
	t.Logf("Step 3 — CommitmentB (Bob receives USDT): %s", commitmentB)

	// Alice's ERC20 JoinSplit proof: stMessage=C', output0=CommitmentB.
	// Uses Erc20JoinSplitProofFromSalts so the output commitment is pre-determined.
	aliceERC20Result, err := gnarkClient.Erc20JoinSplitProofFromSalts(
		commitmentA,  // stMessage = C'
		[]*big.Int{amountUSDT, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSaltField, big.NewInt(0)},
		[]*big.Int{amountUSDT, big.NewInt(0)},
		[]*big.Int{bobSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltBField, big.NewInt(0)},  // → CommitmentB for Bob
		[][]byte{ctI, nil},
		[][]byte{ctII, nil},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenIdUSDT,
		false,
	)
	if err != nil { t.Fatalf("Alice Erc20JoinSplitProofFromSalts: %v", err) }
	if len(aliceERC20Result.Proof) != 8 {
		t.Fatalf("expected 8-element ERC20 proof, got %d", len(aliceERC20Result.Proof))
	}

	// Verify cross-commitment layout (non-interleaved 2-in/2-out: idx 0 = msg, idx 7 = cmt0)
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
			CipherText:  ctI,
			EncTxData: ctII,
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

	if swap.TokenIdOut.Cmp(tokenIdTicket) != 0 {
		t.Errorf("tokenIdOut: got %s, want %s", swap.TokenIdOut, tokenIdTicket)
	}
	if swap.AmountOut.Cmp(amountTicket) != 0 {
		t.Errorf("amountOut: got %s, want %s", swap.AmountOut, amountTicket)
	}
	t.Logf("Step 4 — Bob verified swap: tokenIdOut=%s, amountOut=%s ✓", swap.TokenIdOut, swap.AmountOut)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Bob generates ERC721 OwnershipProof
	//   stMessage = CommitmentB  (what Bob expects to receive)
	//   output    = C'           (ticket commitment for Alice, using saltStarField)
	// ─────────────────────────────────────────────────────────────────────────
	recvSaltStarField := core.SaltBToField(swap.SaltStar)

	bobERC721Result, err := gnarkClient.Erc721OwnershipProofFromSalt(
		commitmentB,   // stMessage = CommitmentB
		tokenIdTicket,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobTicketSalt,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		recvSaltStarField, // output salt → C' = Erc721Commitment(25, alicePk, recvSaltStarField)
		nil,               // ctI: Alice knows saltStar directly
		nil,               // ctII
		merkleDepth,
		bobProof,
		big.NewInt(0),
		contractAddr,
	)
	if err != nil { t.Fatalf("Bob Erc721OwnershipProofFromSalt: %v", err) }
	if len(bobERC721Result.Proof) != 8 {
		t.Fatalf("expected 8-element ERC721 proof, got %d", len(bobERC721Result.Proof))
	}

	// ERC721 statement: [stMessage, treeNum, root, null, C']
	if bobERC721Result.Statement[0].Cmp(commitmentB) != 0 {
		t.Fatalf("Bob statement[0] != CommitmentB: got %s", bobERC721Result.Statement[0])
	}
	if bobERC721Result.Statement[4].Cmp(commitmentA) != 0 {
		t.Fatalf("Bob statement[4] != C': got %s", bobERC721Result.Statement[4])
	}

	// Cross-commitment consistency (mirrors _settleOnGroupPair checks).
	if aliceERC20Result.Statement[0].Cmp(bobERC721Result.Statement[4]) != 0 {
		t.Fatalf("cross-check FAILED: Alice stMsg C' != Bob output C'")
	}
	if bobERC721Result.Statement[0].Cmp(aliceERC20Result.Statement[7]) != 0 {
		t.Fatalf("cross-check FAILED: Bob stMsg CommitmentB != Alice output CommitmentB")
	}
	t.Logf("Step 5 — Bob ERC721 proof generated; cross-commitment consistency verified ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: EnygmaDvp.swap(alicePaymentReceipt, bobDeliveryReceipt, 0, 1)
	//   paymentVaultId=0  → Erc20CoinVault  (Alice pays USDT)
	//   deliveryVaultId=1 → Erc721CoinVault (Bob delivers ticket)
	// ─────────────────────────────────────────────────────────────────────────
	aliceSnarkProof := proofStringsToOnchain(t, aliceERC20Result.Proof)
	alicePaymentReceipt := buildReceipt(aliceERC20Result)
	alicePaymentReceipt.Proof = aliceSnarkProof

	bobSnarkProof := proofStringsToOnchain(t, bobERC721Result.Proof)
	bobDeliveryReceipt := onchainProofReceipt{
		Proof:           bobSnarkProof,
		Statement:       bobERC721Result.Statement, // ERC721: 1-in/1-out, interleaved == non-interleaved
		NumberOfInputs:  big.NewInt(int64(bobERC721Result.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(bobERC721Result.NumberOfOutputs)),
	}

	t.Logf("Step 6 — Calling EnygmaDvp.swap(alicePaymentReceipt, bobDeliveryReceipt, 0, 1)")
	swapTx, err := dvp.Transact(auth, "swap",
		alicePaymentReceipt,
		bobDeliveryReceipt,
		big.NewInt(0), // paymentVaultId = ERC20 vault
		big.NewInt(1), // deliveryVaultId = ERC721 vault
	)
	if err != nil { t.Fatalf("dvp.swap: %v", err) }
	swapTxReceipt, err := bind.WaitMined(ctx, client, swapTx)
	if err != nil { t.Fatalf("wait swap: %v", err) }
	t.Logf("Step 6 — swap mined (block %d, gas %d)", swapTxReceipt.BlockNumber, swapTxReceipt.GasUsed)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 7: Verify on-chain events and note recovery
	// ─────────────────────────────────────────────────────────────────────────
	foundCommitmentB := false
	foundCommitmentA := false
	nullifierCount   := 0

	for _, log := range swapTxReceipt.Logs {
		switch log.Topics[0] {
		case commitmentSig:
			cmt := log.Topics[2].Big()
			if cmt.Cmp(commitmentB) == 0 {
				foundCommitmentB = true
				t.Logf("  CommitmentB (Bob's USDT note) event: %s", cmt)
			} else if cmt.Cmp(commitmentA) == 0 {
				foundCommitmentA = true
				t.Logf("  CommitmentA/C' (Alice's ticket note) event: %s", cmt)
			}
		case nullifierSig:
			nullifierCount++
			t.Logf("  Nullifier event: %s", log.Topics[3].Big())
		}
	}
	if !foundCommitmentB { t.Errorf("CommitmentB event not found in swap receipt") }
	if !foundCommitmentA { t.Errorf("C' (CommitmentA) event not found in swap receipt") }
	if nullifierCount == 0 { t.Errorf("expected at least one Nullifier event in swap receipt") }

	// Bob verifies his CommitmentB is well-formed (he knows saltBField from the scan).
	recoveredCommB, err := core.Erc20CommitmentV2(bobSpend.PublicKey, swap.SaltBField, amountUSDT, tokenIdUSDT)
	if err != nil { t.Fatalf("Bob recompute CommitmentB: %v", err) }
	if recoveredCommB.Cmp(commitmentB) != 0 {
		t.Errorf("Bob's recomputed CommitmentB mismatch: got %s, want %s", recoveredCommB, commitmentB)
	}
	t.Logf("Step 7a — Bob can spend CommitmentB (USDT note) ✓")

	// Alice verifies she can spend C' using saltStarField (she generated it).
	recoveredC, err := core.Erc721Commitment(tokenIdTicket, aliceSpend.PublicKey, saltStarField)
	if err != nil { t.Fatalf("Alice recompute C': %v", err) }
	if recoveredC.Cmp(commitmentA) != 0 {
		t.Errorf("Alice recomputed C' mismatch: got %s, want %s", recoveredC, commitmentA)
	}
	t.Logf("Step 7b — Alice can spend C' (ERC721 ticket note) ✓")

	t.Logf("=== ZkDvP TWO-PHASE SWAP ON-CHAIN COMPLETE ===")
	t.Logf("Alice burned USDT note → Bob receives CommitmentB=%s", commitmentB)
	t.Logf("Bob burned ticket note → Alice receives C'=%s", commitmentA)
}

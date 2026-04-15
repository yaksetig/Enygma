// Deprecated: This file is legacy and will not be used in the current version.
package tests

// Relayer variant of 08_v2_swap_erc721_erc20_onchain_test.go.
//
// Protocol logic is identical to TestV2Swap_Erc721ForErc20_OnChain.
// The only difference is Step 6: instead of calling dvp.Transact("swap", ...)
// directly, this test uses endpoints.Swap() — the relayer library — to submit
// both proofs atomically.
//
// Run with:
//
//	go test -run TestV2Swap_Erc721ForErc20_OnChain_WithRelayer -v -timeout 600s
import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"
	"enygma_dvp/src_go/core/endpoints"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestV2Swap_Erc721ForErc20_OnChain_WithRelayer(t *testing.T) {
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
	erc721VaultAddr := common.HexToAddress(receipts["Erc721CoinVault"].ContractAddress)
	erc20VaultAddr  := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	dvpAddr         := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)
	erc721Addr      := common.HexToAddress(receipts["ERC721"].ContractAddress)
	erc20Addr       := common.HexToAddress(receipts["ERC20"].ContractAddress)

	erc721VaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc721CoinVault.sol/Erc721CoinVault.json")
	erc20VaultABI  := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc721ABI      := loadOnchainABI(t, "erc721/contracts/RaylsERC721.sol/RaylsERC721.json")
	erc20ABI       := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")
	dvpABI         := loadOnchainABI(t, "core/contracts/EnygmaDvp.sol/EnygmaDvp.json")

	erc721Vault := bind.NewBoundContract(erc721VaultAddr, erc721VaultABI, client, client, client)
	erc20Vault  := bind.NewBoundContract(erc20VaultAddr, erc20VaultABI, client, client, client)
	erc721      := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)
	erc20       := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient   := core.NewGnarkClient("http://localhost:8081")
	merkleDepth   := 8
	erc20TokenId  := big.NewInt(0)
	paymentAmount := big.NewInt(100)

	tokenIdRand, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		t.Fatalf("rand.Int: %v", err)
	}
	tokenId := new(big.Int).Add(tokenIdRand, big.NewInt(2000))

	contractAddr721 := big.NewInt(0)

	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Alice NewSpendKeyPair: %v", err) }
	aliceView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Alice NewViewKeyPair: %v", err) }
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Bob NewSpendKeyPair: %v", err) }
	bobView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Bob NewViewKeyPair: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: Alice deposits ERC721 NFT
	// ─────────────────────────────────────────────────────────────────────────
	aliceNFTSalt, err := core.RandomInField()
	if err != nil { t.Fatalf("Alice RandomInField (NFT salt): %v", err) }

	mintNFTTx, err := erc721.Transact(auth, "mint", alice, tokenId)
	if err != nil { t.Fatalf("ERC721.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintNFTTx); err != nil { t.Fatalf("wait ERC721 mint: %v", err) }
	t.Logf("Minted ERC721 tokenId=%s to Alice", tokenId)

	approveTx, err := erc721.Transact(auth, "approve", erc721VaultAddr, tokenId)
	if err != nil { t.Fatalf("ERC721.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil { t.Fatalf("wait ERC721 approve: %v", err) }

	aliceNFTCommitment, err := core.Erc721Commitment(tokenId, aliceSpend.PublicKey, aliceNFTSalt)
	if err != nil { t.Fatalf("Erc721Commitment: %v", err) }

	depositNFTTx, err := erc721Vault.Transact(auth, "deposit", []*big.Int{tokenId, aliceNFTCommitment})
	if err != nil { t.Fatalf("erc721Vault.deposit: %v", err) }
	depositNFTReceipt, err := bind.WaitMined(ctx, client, depositNFTTx)
	if err != nil { t.Fatalf("wait ERC721 deposit: %v", err) }
	t.Logf("Step 1 — ERC721 deposit mined (block %d, gas %d)", depositNFTReceipt.BlockNumber, depositNFTReceipt.GasUsed)

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	foundNFTCmt := false
	for _, log := range depositNFTReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundNFTCmt = true
			t.Logf("  ERC721 Commitment event: %s", log.Topics[2].Big())
		}
	}
	if !foundNFTCmt {
		t.Errorf("ERC721 Commitment event not found in deposit receipt")
	}

	mt721 := loadVaultMerkleTree(t, client, erc721VaultAddr, merkleDepth)
	aliceNFTProof, err := mt721.GenerateProof(aliceNFTCommitment)
	if err != nil { t.Fatalf("GenerateProof for Alice's NFT: %v", err) }
	t.Logf("Step 1 — ERC721 Merkle root: %s", aliceNFTProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Alice deposits ERC20 tokens with Bob's commitment (depositV2)
	// ─────────────────────────────────────────────────────────────────────────
	mintERC20Tx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(paymentAmount, big.NewInt(10)))
	if err != nil { t.Fatalf("ERC20.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintERC20Tx); err != nil { t.Fatalf("wait ERC20 mint: %v", err) }

	bobDepositSaltB, bobDepositCapsule, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil { t.Fatalf("Encapsulate (Bob's deposit salt): %v", err) }
	bobDepositSaltField := core.SaltBToField(bobDepositSaltB)

	bobInputCommitment, err := core.Erc20CommitmentV2(bobSpend.PublicKey, bobDepositSaltField, paymentAmount, erc20TokenId)
	if err != nil { t.Fatalf("Erc20CommitmentV2 (Bob's input note): %v", err) }

	bobDepositCtII, err := core.EncryptPayload(bobDepositSaltB, erc20TokenId, paymentAmount)
	if err != nil { t.Fatalf("EncryptPayload (Bob's deposit): %v", err) }

	approveERC20Tx, err := erc20.Transact(auth, "approve", erc20VaultAddr, paymentAmount)
	if err != nil { t.Fatalf("ERC20.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveERC20Tx); err != nil { t.Fatalf("wait ERC20 approve: %v", err) }

	depositERC20Tx, err := erc20Vault.Transact(auth, "depositV2",
		[]*big.Int{paymentAmount, bobInputCommitment}, bobDepositCapsule, bobDepositCtII)
	if err != nil { t.Fatalf("erc20Vault.depositV2: %v", err) }
	depositERC20Receipt, err := bind.WaitMined(ctx, client, depositERC20Tx)
	if err != nil { t.Fatalf("wait ERC20 depositV2: %v", err) }
	t.Logf("Step 2 — ERC20 depositV2 mined (block %d, gas %d)", depositERC20Receipt.BlockNumber, depositERC20Receipt.GasUsed)

	mt20 := loadVaultMerkleTree(t, client, erc20VaultAddr, merkleDepth)
	bobErc20MerkleProof, err := mt20.GenerateProof(bobInputCommitment)
	if err != nil { t.Fatalf("GenerateProof for Bob's ERC20 note: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Pre-compute cross-commitments
	// ─────────────────────────────────────────────────────────────────────────
	saltBForNFT, ctINFT, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil { t.Fatalf("Encapsulate (NFT salt for Bob): %v", err) }
	saltBForNFTField := core.SaltBToField(saltBForNFT)
	ctIINFT, err := core.EncryptPayload(saltBForNFT, contractAddr721, tokenId)
	if err != nil { t.Fatalf("EncryptPayload (NFT ciphertext): %v", err) }
	bobNFTCommitment, err := core.Erc721Commitment(tokenId, bobSpend.PublicKey, saltBForNFTField)
	if err != nil { t.Fatalf("Erc721Commitment (Bob's output): %v", err) }
	t.Logf("Step 3 — Bob's pre-computed NFT commitment: %s", bobNFTCommitment)

	saltBForPayment, ctIPayment, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil { t.Fatalf("Encapsulate (ERC20 salt for Alice): %v", err) }
	saltBForPaymentField := core.SaltBToField(saltBForPayment)
	ctIIPayment, err := core.EncryptPayload(saltBForPayment, erc20TokenId, paymentAmount)
	if err != nil { t.Fatalf("EncryptPayload (ERC20 ciphertext): %v", err) }
	aliceERC20Commitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltBForPaymentField, paymentAmount, erc20TokenId)
	if err != nil { t.Fatalf("Erc20CommitmentV2 (Alice's output): %v", err) }
	t.Logf("Step 3 — Alice's pre-computed ERC20 commitment: %s", aliceERC20Commitment)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Alice generates ERC721 ownership proof
	// ─────────────────────────────────────────────────────────────────────────
	aliceResult, err := gnarkClient.Erc721OwnershipProofFromSalt(
		aliceERC20Commitment,
		tokenId,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceNFTSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		saltBForNFTField,
		ctINFT,
		ctIINFT,
		merkleDepth,
		aliceNFTProof,
		big.NewInt(0),
		contractAddr721,
	)
	if err != nil { t.Fatalf("Erc721OwnershipProofFromSalt: %v", err) }
	if aliceResult.Statement[4].Cmp(bobNFTCommitment) != 0 {
		t.Fatalf("ERC721 output commitment mismatch: got %s, want %s",
			aliceResult.Statement[4], bobNFTCommitment)
	}
	t.Logf("Step 4 — Alice's ERC721 proof generated ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Bob generates ERC20 JoinSplit proof
	// ─────────────────────────────────────────────────────────────────────────
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("dummySpend: %v", err) }

	bobResult, err := gnarkClient.Erc20JoinSplitProofFromSalts(
		bobNFTCommitment,
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobDepositSaltField, big.NewInt(0)},
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltBForPaymentField, big.NewInt(0)},
		[][]byte{ctIPayment, nil},
		[][]byte{ctIIPayment, nil},
		merkleDepth,
		[]*core.MerkleProof{bobErc20MerkleProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		erc20TokenId,
		false,
	)
	if err != nil { t.Fatalf("Erc20JoinSplitProofFromSalts: %v", err) }
	if bobResult.Statement[7].Cmp(aliceERC20Commitment) != 0 {
		t.Fatalf("ERC20 first output commitment mismatch: got %s, want %s",
			bobResult.Statement[7], aliceERC20Commitment)
	}
	if aliceResult.Statement[0].Cmp(bobResult.Statement[7]) != 0 {
		t.Fatalf("cross-check: Alice stMessage != Bob's first output")
	}
	if bobResult.Statement[0].Cmp(aliceResult.Statement[4]) != 0 {
		t.Fatalf("cross-check: Bob stMessage != Alice's NFT output")
	}
	t.Logf("Step 5 — Cross-commitment consistency verified ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Relayer submits swap via endpoints.Swap()
	//
	// Replaces the direct dvp.Transact("swap", ...) call. The relayer collects
	// both ProofReceipts and submits them atomically using its own Ethereum key.
	// ─────────────────────────────────────────────────────────────────────────
	bobPaymentReceipt := endpoints.ProofReceipt{
		Proof:           toEndpointsSnarkProof(t, bobResult.Proof),
		Statement:       bobResult.ContractStatement(),
		NumberOfInputs:  big.NewInt(int64(bobResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(bobResult.NumberOfOutputs)),
	}

	// ERC721 statement is already [msg, treeNum, root, null, cmt] — no reordering needed.
	aliceDeliveryReceipt := endpoints.ProofReceipt{
		Proof:           toEndpointsSnarkProof(t, aliceResult.Proof),
		Statement:       aliceResult.Statement,
		NumberOfInputs:  big.NewInt(int64(aliceResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(aliceResult.NumberOfOutputs)),
	}

	t.Logf("Step 6 — Relayer calling endpoints.Swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 1)")
	relayerCommitments, err := endpoints.Swap(
		client, auth, dvpABI, dvpAddr,
		bobPaymentReceipt,
		aliceDeliveryReceipt,
		big.NewInt(0), // paymentVaultId = ERC20 vault
		big.NewInt(1), // deliveryVaultId = ERC721 vault
	)
	if err != nil { t.Fatalf("endpoints.Swap: %v", err) }
	t.Logf("Step 6 — Relayer swap submitted ✓")
	t.Logf("  relayerCommitments[0] Alice's ERC20 payment: %s", relayerCommitments[0])
	t.Logf("  relayerCommitments[2] Bob's NFT note:        %s", relayerCommitments[2])

	// ─────────────────────────────────────────────────────────────────────────
	// Step 7: Verify commitments returned by the relayer and scan notes
	// ─────────────────────────────────────────────────────────────────────────

	// endpoints.Swap returns [paymentStatement[7], paymentStatement[8], deliveryStatement[4]]
	// = [Alice's ERC20 payment, Bob's ERC20 change, Bob's NFT]
	if relayerCommitments[0].Cmp(aliceERC20Commitment) != 0 {
		t.Errorf("relayerCommitments[0] != aliceERC20Commitment: got %s, want %s",
			relayerCommitments[0], aliceERC20Commitment)
	}
	if relayerCommitments[2].Cmp(bobNFTCommitment) != 0 {
		t.Errorf("relayerCommitments[2] != bobNFTCommitment: got %s, want %s",
			relayerCommitments[2], bobNFTCommitment)
	}

	// Bob scans for his NFT note.
	nftEvents := []core.OnChainErc721Event{{
		Commitment:   bobNFTCommitment,
		CipherText:  ctINFT,
		EncTxData: ctIINFT,
	}}
	bobNFTNotes, err := core.ScanForErc721Notes(bobView.DecapsKey, bobSpend.PublicKey, nftEvents)
	if err != nil { t.Fatalf("ScanForErc721Notes: %v", err) }
	if len(bobNFTNotes) != 1 {
		t.Fatalf("Bob expected 1 NFT note, got %d", len(bobNFTNotes))
	}
	t.Logf("Step 7 — Bob scanned his NFT note (tokenId=%s) ✓", bobNFTNotes[0].TokenId)

	// Alice scans for her ERC20 payment note.
	erc20Events := []core.OnChainErc20Event{{
		Commitment:   aliceERC20Commitment,
		CipherText:  ctIPayment,
		EncTxData: ctIIPayment,
	}}
	aliceERC20Notes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, erc20Events)
	if err != nil { t.Fatalf("ScanForErc20Notes: %v", err) }
	if len(aliceERC20Notes) != 1 {
		t.Fatalf("Alice expected 1 ERC20 note, got %d", len(aliceERC20Notes))
	}
	if aliceERC20Notes[0].Amount.Cmp(paymentAmount) != 0 {
		t.Errorf("Alice's payment amount: got %s, want %s", aliceERC20Notes[0].Amount, paymentAmount)
	}
	t.Logf("Step 7 — Alice scanned her ERC20 payment note (amount=%s) ✓", aliceERC20Notes[0].Amount)

	t.Logf("=== ERC721 ↔ ERC20 DVP SWAP WITH RELAYER COMPLETE ===")
	t.Logf("Alice burned NFT note → Bob gets NFT  (tokenId=%s)", bobNFTNotes[0].TokenId)
	t.Logf("Bob burned ERC20 note → Alice gets %s tokens", aliceERC20Notes[0].Amount)
}

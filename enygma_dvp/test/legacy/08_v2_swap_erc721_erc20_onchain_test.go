// Deprecated: This file is legacy and will not be used in the current version.
package tests

// On-chain integration test for the V2 ERC721 ↔ ERC20 atomic DVP swap flow:
//
//	Alice deposits an ERC721 NFT.
//	Alice also deposits ERC20 tokens *with Bob's commitment* into the ERC20 vault (depositV2),
//	which gives Bob a spendable ERC20 note without needing privateMint.
//	Both parties pre-compute cross-commitments, then generate proofs independently.
//	The test calls EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 1).
//
// Cross-commitment atomicity constraints (enforced by _settleOnGroupPair):
//
//	bobPaymentReceipt.statement[0]  (Bob's ERC20 stMessage)  == aliceDeliveryReceipt.statement[4] (Bob's new NFT)
//	aliceDeliveryReceipt.statement[0] (Alice's ERC721 stMessage) == bobPaymentReceipt.statement[7] (Alice's ERC20 payment)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//
//	 go test -run TestV2Swap_Erc721ForErc20_OnChain -v -timeout 600s

import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/raylsnetwork/enygma_dvp/src/core"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// TestV2Swap_Erc721ForErc20_OnChain exercises the full DVP swap flow on a live Hardhat node:
//
//	Step 1 — Alice deposits an ERC721 NFT.
//	Step 2 — Alice deposits ERC20 tokens with Bob's commitment (depositV2), giving Bob a spendable note.
//	Step 3 — Pre-compute cross-commitments so both stMessages satisfy the on-chain contract.
//	Step 4 — Alice generates ERC721 ownership proof (stMessage = Alice's incoming ERC20 commitment).
//	Step 5 — Bob generates ERC20 JoinSplit proof (stMessage = Bob's incoming NFT commitment).
//	Step 6 — Call EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 1).
//	Step 7 — Verify on-chain events; Bob scans for NFT, Alice scans for ERC20 payment.
func TestV2Swap_Erc721ForErc20_OnChain(t *testing.T) {
	if !chainAvailable() {
		t.Skip("Hardhat node not running on localhost:8545 — skipping on-chain test")
	}
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping on-chain test")
	}

	ctx := context.Background()

	// ── Connect to Hardhat ────────────────────────────────────────────────────
	client, err := ethclient.Dial(hardhatRPC)
	if err != nil {
		t.Fatalf("ethclient.Dial: %v", err)
	}
	defer client.Close()

	// ── Load deployed contract addresses ─────────────────────────────────────
	receipts := loadOnchainReceipts(t)
	erc721VaultAddr := common.HexToAddress(receipts["Erc721CoinVault"].ContractAddress)
	erc20VaultAddr  := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	dvpAddr         := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)
	erc721Addr      := common.HexToAddress(receipts["ERC721"].ContractAddress)
	erc20Addr       := common.HexToAddress(receipts["ERC20"].ContractAddress)

	// ── Load ABIs ─────────────────────────────────────────────────────────────
	erc721VaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc721CoinVault.sol/Erc721CoinVault.json")
	erc20VaultABI  := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc721ABI      := loadOnchainABI(t, "erc721/contracts/RaylsERC721.sol/RaylsERC721.json")
	erc20ABI       := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")
	dvpABI         := loadOnchainABI(t, "core/contracts/EnygmaDvp.sol/EnygmaDvp.json")

	erc721Vault := bind.NewBoundContract(erc721VaultAddr, erc721VaultABI, client, client, client)
	erc20Vault  := bind.NewBoundContract(erc20VaultAddr, erc20VaultABI, client, client, client)
	erc721      := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)
	erc20       := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)
	dvp         := bind.NewBoundContract(dvpAddr, dvpABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient   := core.NewGnarkClient("http://localhost:8081")
	merkleDepth   := 8
	erc20TokenId  := big.NewInt(0)    // ERC20 circuit uses tokenId = 0
	paymentAmount := big.NewInt(100)  // Bob pays 100 ERC20 tokens to Alice

	// Random ERC721 tokenId in [2000, 2999] to avoid collision with tests 13 and 16.
	tokenIdRand, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		t.Fatalf("rand.Int: %v", err)
	}
	tokenId := new(big.Int).Add(tokenIdRand, big.NewInt(2000))

	contractAddr721 := big.NewInt(0) // ERC721 circuit witness (unused in commitment hash)

	// ── Key generation ────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: Alice deposits ERC721 NFT
	// ─────────────────────────────────────────────────────────────────────────
	aliceNFTSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (NFT salt): %v", err)
	}

	// Mint ERC721 token to Alice.
	mintNFTTx, err := erc721.Transact(auth, "mint", alice, tokenId)
	if err != nil {
		t.Fatalf("ERC721.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintNFTTx); err != nil {
		t.Fatalf("wait ERC721 mint: %v", err)
	}
	t.Logf("Minted ERC721 tokenId=%s to Alice", tokenId)

	// Approve ERC721 vault.
	approveTx, err := erc721.Transact(auth, "approve", erc721VaultAddr, tokenId)
	if err != nil {
		t.Fatalf("ERC721.approve: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait ERC721 approve: %v", err)
	}

	// V2 commitment: Poseidon(pk_spend, salt, 1, tokenId).
	aliceNFTCommitment, err := core.Erc721Commitment(tokenId, aliceSpend.PublicKey, aliceNFTSalt)
	if err != nil {
		t.Fatalf("Erc721Commitment: %v", err)
	}

	depositNFTParams := []*big.Int{tokenId, aliceNFTCommitment}
	depositNFTTx, err := erc721Vault.Transact(auth, "deposit", depositNFTParams)
	if err != nil {
		t.Fatalf("erc721Vault.deposit: %v", err)
	}
	depositNFTReceipt, err := bind.WaitMined(ctx, client, depositNFTTx)
	if err != nil {
		t.Fatalf("wait ERC721 deposit: %v", err)
	}
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

	// Build ERC721 vault Merkle tree.
	mt721 := loadVaultMerkleTree(t, client, erc721VaultAddr, merkleDepth)
	aliceNFTProof, err := mt721.GenerateProof(aliceNFTCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice's NFT: %v", err)
	}
	t.Logf("Step 1 — ERC721 Merkle root: %s", aliceNFTProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Alice deposits ERC20 tokens with Bob's commitment (depositV2).
	//
	// Alice encapsulates with Bob's view key to derive Bob's note salt,
	// then deposits with Bob's commitment so Bob has a spendable ERC20 note.
	// This avoids privateMint (whose on-chain verifier has a stale VK).
	// ─────────────────────────────────────────────────────────────────────────

	// Mint ERC20 tokens to Alice.
	mintERC20Tx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(paymentAmount, big.NewInt(10)))
	if err != nil {
		t.Fatalf("ERC20.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintERC20Tx); err != nil {
		t.Fatalf("wait ERC20 mint: %v", err)
	}

	// Alice encapsulates with Bob's view key — Bob will decapsulate to get his salt.
	bobDepositSaltB, bobDepositCapsule, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (Bob's deposit salt): %v", err)
	}
	bobDepositSaltField := core.SaltBToField(bobDepositSaltB)

	// Bob's input commitment: Poseidon(bobSpend.PublicKey, salt, paymentAmount, tokenId=0)
	bobInputCommitment, err := core.Erc20CommitmentV2(bobSpend.PublicKey, bobDepositSaltField, paymentAmount, erc20TokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 (Bob's input note): %v", err)
	}

	// Encrypt payload so Bob can scan.
	bobDepositCtII, err := core.EncryptPayload(bobDepositSaltB, erc20TokenId, paymentAmount)
	if err != nil {
		t.Fatalf("EncryptPayload (Bob's deposit): %v", err)
	}

	// Approve ERC20 vault.
	approveERC20Tx, err := erc20.Transact(auth, "approve", erc20VaultAddr, paymentAmount)
	if err != nil {
		t.Fatalf("ERC20.approve: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveERC20Tx); err != nil {
		t.Fatalf("wait ERC20 approve: %v", err)
	}

	depositERC20Params := []*big.Int{paymentAmount, bobInputCommitment}
	depositERC20Tx, err := erc20Vault.Transact(auth, "depositV2",
		depositERC20Params,
		bobDepositCapsule,
		bobDepositCtII,
	)
	if err != nil {
		t.Fatalf("erc20Vault.depositV2: %v", err)
	}
	depositERC20Receipt, err := bind.WaitMined(ctx, client, depositERC20Tx)
	if err != nil {
		t.Fatalf("wait ERC20 depositV2: %v", err)
	}
	t.Logf("Step 2 — ERC20 depositV2 mined (block %d, gas %d)", depositERC20Receipt.BlockNumber, depositERC20Receipt.GasUsed)
	t.Logf("  Bob's input ERC20 commitment: %s", bobInputCommitment)

	// Build ERC20 vault Merkle tree.
	mt20 := loadVaultMerkleTree(t, client, erc20VaultAddr, merkleDepth)
	bobErc20MerkleProof, err := mt20.GenerateProof(bobInputCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Bob's ERC20 note: %v", err)
	}
	t.Logf("Step 2 — ERC20 Merkle root: %s", bobErc20MerkleProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Pre-compute cross-commitments.
	//
	// The on-chain contract (_settleOnGroupPair) requires:
	//   bobPaymentReceipt.statement[0]   == aliceDeliveryReceipt.statement[4]  (Bob's new NFT)
	//   aliceDeliveryReceipt.statement[0] == bobPaymentReceipt.statement[7]    (Alice's ERC20)
	//
	// Solution: each party pre-computes the other's output commitment and uses
	// it as their own stMessage.  The proofs are then generated with pre-determined
	// output salts (Erc721OwnershipProofFromSalt / Erc20JoinSplitProofFromSalts).
	// ─────────────────────────────────────────────────────────────────────────

	// Bob pre-computes his incoming NFT commitment (Alice will put this as her output).
	saltBForNFT, ctINFT, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (NFT salt for Bob): %v", err)
	}
	saltBForNFTField := core.SaltBToField(saltBForNFT)
	ctIINFT, err := core.EncryptPayload(saltBForNFT, contractAddr721, tokenId)
	if err != nil {
		t.Fatalf("EncryptPayload (NFT ciphertext): %v", err)
	}
	bobNFTCommitment, err := core.Erc721Commitment(tokenId, bobSpend.PublicKey, saltBForNFTField)
	if err != nil {
		t.Fatalf("Erc721Commitment (Bob's output): %v", err)
	}
	t.Logf("Step 3 — Bob's pre-computed NFT commitment: %s", bobNFTCommitment)

	// Alice pre-computes her incoming ERC20 commitment (Bob will put this as his first output).
	saltBForPayment, ctIPayment, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (ERC20 salt for Alice): %v", err)
	}
	saltBForPaymentField := core.SaltBToField(saltBForPayment)
	ctIIPayment, err := core.EncryptPayload(saltBForPayment, erc20TokenId, paymentAmount)
	if err != nil {
		t.Fatalf("EncryptPayload (ERC20 ciphertext): %v", err)
	}
	aliceERC20Commitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltBForPaymentField, paymentAmount, erc20TokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 (Alice's output): %v", err)
	}
	t.Logf("Step 3 — Alice's pre-computed ERC20 commitment: %s", aliceERC20Commitment)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Alice generates ERC721 ownership proof.
	//   stMessage = aliceERC20Commitment (what Alice expects to receive)
	//   output    = bobNFTCommitment     (pre-computed, passed as wtSaltOut)
	// ─────────────────────────────────────────────────────────────────────────
	aliceResult, err := gnarkClient.Erc721OwnershipProofFromSalt(
		aliceERC20Commitment, // stMessage: what Alice expects to receive
		tokenId,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceNFTSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		saltBForNFTField, // pre-computed output salt → produces bobNFTCommitment
		ctINFT,
		ctIINFT,
		merkleDepth,
		aliceNFTProof,
		big.NewInt(0), // treeNumber
		contractAddr721,
	)
	if err != nil {
		t.Fatalf("Erc721OwnershipProofFromSalt: %v", err)
	}
	if len(aliceResult.Proof) != 8 {
		t.Fatalf("expected 8-element ERC721 proof, got %d", len(aliceResult.Proof))
	}
	t.Logf("Step 4 — Alice's ERC721 proof generated")
	t.Logf("  aliceResult.statement[0] (stMessage)  = %s", aliceResult.Statement[0])
	t.Logf("  aliceResult.statement[4] (NFT output) = %s", aliceResult.Statement[4])

	if aliceResult.Statement[4].Cmp(bobNFTCommitment) != 0 {
		t.Fatalf("ERC721 output commitment mismatch: got %s, want %s",
			aliceResult.Statement[4], bobNFTCommitment)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Bob generates ERC20 JoinSplit proof.
	//   stMessage    = bobNFTCommitment      (what Bob expects to receive)
	//   first output = aliceERC20Commitment  (pre-computed, passed as wtSaltsOut[0])
	// ─────────────────────────────────────────────────────────────────────────
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend: %v", err)
	}

	bobResult, err := gnarkClient.Erc20JoinSplitProofFromSalts(
		bobNFTCommitment, // stMessage: what Bob expects to receive
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobDepositSaltField, big.NewInt(0)},          // Bob's input salt from depositV2
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltBForPaymentField, big.NewInt(0)},         // pre-computed output salts
		[][]byte{ctIPayment, nil},
		[][]byte{ctIIPayment, nil},
		merkleDepth,
		[]*core.MerkleProof{bobErc20MerkleProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		erc20TokenId,
		false, // 2-in / 2-out circuit
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProofFromSalts: %v", err)
	}
	if len(bobResult.Proof) != 8 {
		t.Fatalf("expected 8-element ERC20 proof, got %d", len(bobResult.Proof))
	}
	t.Logf("Step 5 — Bob's ERC20 proof generated")
	t.Logf("  bobResult.statement[0] (stMessage)         = %s", bobResult.Statement[0])
	t.Logf("  bobResult.statement[7] (Alice's ERC20 cmt) = %s", bobResult.Statement[7])

	if bobResult.Statement[7].Cmp(aliceERC20Commitment) != 0 {
		t.Fatalf("ERC20 first output commitment mismatch: got %s, want %s",
			bobResult.Statement[7], aliceERC20Commitment)
	}

	// Verify cross-commitment consistency (mirrors _settleOnGroupPair checks).
	if aliceResult.Statement[0].Cmp(bobResult.Statement[7]) != 0 {
		t.Fatalf("cross-check: Alice stMessage (%s) != Bob's first output (%s)",
			aliceResult.Statement[0], bobResult.Statement[7])
	}
	if bobResult.Statement[0].Cmp(aliceResult.Statement[4]) != 0 {
		t.Fatalf("cross-check: Bob stMessage (%s) != Alice's NFT output (%s)",
			bobResult.Statement[0], aliceResult.Statement[4])
	}
	t.Logf("Step 5 — Cross-commitment consistency verified ✓")

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Build receipts and call EnygmaDvp.swap(bobPayment, aliceDelivery, 0, 1)
	// ─────────────────────────────────────────────────────────────────────────

	// Bob's ERC20 payment receipt (paymentVaultId=0 → Erc20CoinVault).
	bobPaymentSnarkProof := proofStringsToOnchain(t, bobResult.Proof)
	bobPaymentReceipt := buildReceipt(bobResult)
	bobPaymentReceipt.Proof = bobPaymentSnarkProof

	// Alice's ERC721 delivery receipt (deliveryVaultId=1 → Erc721CoinVault).
	// ERC721 statement is already: [msg, treeNum, root, null, cmtOut] — no reordering needed.
	aliceDeliverySnarkProof := proofStringsToOnchain(t, aliceResult.Proof)
	aliceDeliveryReceipt := onchainProofReceipt{
		Proof:           aliceDeliverySnarkProof,
		Statement:       aliceResult.Statement,
		NumberOfInputs:  big.NewInt(int64(aliceResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(aliceResult.NumberOfOutputs)),
	}

	t.Logf("Step 6 — Calling EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 1)")
	swapTx, err := dvp.Transact(auth, "swap",
		bobPaymentReceipt,
		aliceDeliveryReceipt,
		big.NewInt(0), // paymentVaultId = ERC20 vault
		big.NewInt(1), // deliveryVaultId = ERC721 vault
	)
	if err != nil {
		t.Fatalf("dvp.swap: %v", err)
	}
	swapReceipt, err := bind.WaitMined(ctx, client, swapTx)
	if err != nil {
		t.Fatalf("wait swap: %v", err)
	}
	t.Logf("Step 6 — swap mined (block %d, gas %d)", swapReceipt.BlockNumber, swapReceipt.GasUsed)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 7: Verify on-chain events; Bob scans for NFT, Alice scans for ERC20
	// ─────────────────────────────────────────────────────────────────────────
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundNFTCmtInSwap := false
	foundERC20CmtInSwap := false
	nullifierCount := 0

	for _, log := range swapReceipt.Logs {
		switch log.Topics[0] {
		case commitmentSig:
			cmt := log.Topics[2].Big()
			if cmt.Cmp(bobNFTCommitment) == 0 {
				foundNFTCmtInSwap = true
				t.Logf("  NFT Commitment event: %s", cmt)
			} else if cmt.Cmp(aliceERC20Commitment) == 0 {
				foundERC20CmtInSwap = true
				t.Logf("  ERC20 Commitment event: %s", cmt)
			} else {
				t.Logf("  Commitment event (other): %s", cmt)
			}
		case nullifierSig:
			nullifierCount++
			t.Logf("  Nullifier event: %s", log.Topics[3].Big())
		}
	}
	if !foundNFTCmtInSwap {
		t.Errorf("Bob's NFT Commitment event not found in swap receipt")
	}
	if !foundERC20CmtInSwap {
		t.Errorf("Alice's ERC20 Commitment event not found in swap receipt")
	}
	if nullifierCount == 0 {
		t.Errorf("Expected at least one Nullifier event in swap receipt")
	}

	// Bob scans for his NFT note.
	nftEvents := []core.OnChainErc721Event{{
		Commitment:   bobNFTCommitment,
		CipherText:  ctINFT,
		EncTxData: ctIINFT,
	}}
	bobNFTNotes, err := core.ScanForErc721Notes(bobView.DecapsKey, bobSpend.PublicKey, nftEvents)
	if err != nil {
		t.Fatalf("ScanForErc721Notes: %v", err)
	}
	if len(bobNFTNotes) != 1 {
		t.Fatalf("Bob expected 1 NFT note, got %d", len(bobNFTNotes))
	}
	t.Logf("Step 7 — Bob scanned his NFT note (tokenId=%s)", bobNFTNotes[0].TokenId)

	// Alice scans for her ERC20 payment note.
	erc20Events := []core.OnChainErc20Event{{
		Commitment:   aliceERC20Commitment,
		CipherText:  ctIPayment,
		EncTxData: ctIIPayment,
	}}
	aliceERC20Notes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, erc20Events)
	if err != nil {
		t.Fatalf("ScanForErc20Notes: %v", err)
	}
	if len(aliceERC20Notes) != 1 {
		t.Fatalf("Alice expected 1 ERC20 note, got %d", len(aliceERC20Notes))
	}
	if aliceERC20Notes[0].Amount.Cmp(paymentAmount) != 0 {
		t.Errorf("Alice's payment amount: got %s, want %s", aliceERC20Notes[0].Amount, paymentAmount)
	}
	t.Logf("Step 7 — Alice scanned her ERC20 payment note (amount=%s)", aliceERC20Notes[0].Amount)

	t.Logf("=== ERC721 ↔ ERC20 DVP SWAP ON-CHAIN COMPLETE ===")
	t.Logf("Alice burned NFT note  → Bob gets NFT  (tokenId=%s)", bobNFTNotes[0].TokenId)
	t.Logf("Bob burned ERC20 note  → Alice gets %s tokens", aliceERC20Notes[0].Amount)
}

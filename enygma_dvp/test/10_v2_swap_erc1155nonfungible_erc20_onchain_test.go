package tests

// On-chain integration test for the V2 ERC1155 Non-Fungible ↔ ERC20 atomic DVP swap flow:
//
//	Alice deposits an ERC1155 NFT (value=1).
//	Alice also deposits ERC20 tokens *with Bob's commitment* into the ERC20 vault (depositV2),
//	giving Bob a spendable ERC20 note without needing privateMint.
//	Both parties pre-compute cross-commitments, then generate proofs independently.
//	The test calls EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 2).
//
// Cross-commitment atomicity constraints (enforced by _settleOnGroupPair):
//
//	bobPaymentReceipt.statement[0]    (Bob's ERC20 stMessage)      == aliceDeliveryReceipt.statement[4] (Bob's new NFT)
//	aliceDeliveryReceipt.statement[0] (Alice's ERC1155 stMessage)  == bobPaymentReceipt.statement[7]   (Alice's ERC20 payment)
//
// Statement layouts:
//
//	ERC20 payment (2-in/2-out, non-interleaved):
//	  [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1]   (9 elements)
//	  → first output at index 7
//
//	ERC1155 NFT delivery (1-in/1-out):
//	  [msg, treeNum, merkleRoot, nullifier, cmt, agTreeNum, agRoot]  (7 elements)
//	  → output commitment at index 4 (same as ERC721)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//
//	go test -run TestV2Swap_Erc1155NonFungibleForErc20_OnChain -v -timeout 600s

import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"

	"enygma_dvp/src_go/core"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// TestV2Swap_Erc1155NonFungibleForErc20_OnChain exercises the full DVP swap flow on a live Hardhat node:
//
//	Step 1 — Alice deposits an ERC1155 NFT (value=1).
//	Step 2 — Alice deposits ERC20 tokens with Bob's commitment (depositV2), giving Bob a spendable note.
//	Step 3 — Pre-compute cross-commitments so both stMessages satisfy the on-chain contract.
//	Step 4 — Alice generates ERC1155 non-fungible ownership proof (stMessage = Alice's incoming ERC20 commitment).
//	Step 5 — Bob generates ERC20 JoinSplit proof (stMessage = Bob's incoming NFT commitment).
//	Step 6 — Call EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 2).
//	Step 7 — Verify on-chain events; Bob scans for NFT, Alice scans for ERC20 payment.
func TestV2Swap_Erc1155NonFungibleForErc20_OnChain(t *testing.T) {
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
	erc1155VaultAddr := common.HexToAddress(receipts["Erc1155CoinVault"].ContractAddress)
	erc20VaultAddr   := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	dvpAddr          := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)
	erc1155Addr      := common.HexToAddress(receipts["ERC1155"].ContractAddress)
	erc20Addr        := common.HexToAddress(receipts["ERC20"].ContractAddress)

	// ── Load ABIs ─────────────────────────────────────────────────────────────
	erc1155VaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc1155CoinVault.sol/Erc1155CoinVault.json")
	erc20VaultABI   := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc1155ABI      := loadOnchainABI(t, "erc1155/contracts/RaylsERC1155.sol/RaylsERC1155.json")
	erc20ABI        := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")
	dvpABI          := loadOnchainABI(t, "core/contracts/EnygmaDvp.sol/EnygmaDvp.json")

	erc1155Vault := bind.NewBoundContract(erc1155VaultAddr, erc1155VaultABI, client, client, client)
	erc20Vault   := bind.NewBoundContract(erc20VaultAddr, erc20VaultABI, client, client, client)
	erc1155      := bind.NewBoundContract(erc1155Addr, erc1155ABI, client, client, client)
	erc20        := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)
	dvp          := bind.NewBoundContract(dvpAddr, dvpABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient     := core.NewGnarkClient("http://localhost:8081")
	merkleDepth     := 8
	erc20TokenId    := big.NewInt(0) // ERC20 circuit uses tokenId = 0
	paymentAmount   := big.NewInt(100)
	nftValue        := big.NewInt(1) // ERC1155 NFTs always have value=1
	contractAddress := new(big.Int).SetBytes(erc1155Addr.Bytes())

	// Random ERC1155 tokenId in [3000, 3999] to avoid collision with other tests.
	tokenIdRand, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		t.Fatalf("rand.Int: %v", err)
	}
	tokenId := new(big.Int).Add(tokenIdRand, big.NewInt(3000))

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
	// Step 1: Alice deposits ERC1155 NFT (value=1)
	// ─────────────────────────────────────────────────────────────────────────
	aliceNFTSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (NFT salt): %v", err)
	}

	// Mint 1 ERC1155 token to Alice.
	mintNFTTx, err := erc1155.Transact(auth, "mint", alice, tokenId, nftValue, []byte{})
	if err != nil {
		t.Fatalf("ERC1155.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintNFTTx); err != nil {
		t.Fatalf("wait ERC1155 mint: %v", err)
	}
	t.Logf("Minted ERC1155 tokenId=%s (value=1) to Alice", tokenId)

	// Approve ERC1155 vault.
	approveTx, err := erc1155.Transact(auth, "setApprovalForAll", erc1155VaultAddr, true)
	if err != nil {
		t.Fatalf("ERC1155.setApprovalForAll: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait setApprovalForAll: %v", err)
	}

	// V2 commitment: Poseidon(pk_spend, salt, value=1, tokenId)
	aliceNFTCommitment, err := core.Erc1155Commitment(tokenId, nftValue, aliceSpend.PublicKey, aliceNFTSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment: %v", err)
	}

	depositNFTParams := []*big.Int{nftValue, tokenId, aliceNFTCommitment}
	depositNFTTx, err := erc1155Vault.Transact(auth, "deposit", depositNFTParams)
	if err != nil {
		t.Fatalf("erc1155Vault.deposit: %v", err)
	}
	depositNFTReceipt, err := bind.WaitMined(ctx, client, depositNFTTx)
	if err != nil {
		t.Fatalf("wait ERC1155 deposit: %v", err)
	}
	t.Logf("Step 1 — ERC1155 NFT deposit mined (block %d, gas %d)", depositNFTReceipt.BlockNumber, depositNFTReceipt.GasUsed)

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	foundNFTCmt := false
	for _, log := range depositNFTReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundNFTCmt = true
			t.Logf("  ERC1155 Commitment event: %s", log.Topics[2].Big())
		}
	}
	if !foundNFTCmt {
		t.Errorf("ERC1155 Commitment event not found in deposit receipt")
	}

	// Build ERC1155 vault Merkle tree.
	mt1155 := loadVaultMerkleTree(t, client, erc1155VaultAddr, merkleDepth)
	aliceNFTProof, err := mt1155.GenerateProof(aliceNFTCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice's ERC1155 NFT: %v", err)
	}
	t.Logf("Step 1 — ERC1155 Merkle root: %s", aliceNFTProof.Root)

	// Build asset group tree for the ERC1155 token.
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
	stAssetGroupTreeNumber := big.NewInt(0)
	t.Logf("Step 1 — Asset group root: %s", assetGroupProof.Root)

	// Register the ERC1155 tokenId in GROUP_ID_NON_FUNGIBLES (groupId=1) so that
	// isMemberFromProofReceipt passes during the swap.
	// uniqueIdParams = [amountOrOne=0, tokenId] matches Erc1155CoinVault.generateUniqueId.
	addTokenTx, err := dvp.Transact(auth, "addTokenToGroup",
		big.NewInt(2),                          // vaultId = ERC1155 vault
		[]*big.Int{big.NewInt(0), tokenId},     // uniqueIdParams: [amountOrOne=0, tokenId]
		big.NewInt(1),                          // groupId = NON_FUNGIBLES
	)
	if err != nil {
		t.Fatalf("dvp.addTokenToGroup: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, addTokenTx); err != nil {
		t.Fatalf("wait addTokenToGroup: %v", err)
	}
	t.Logf("Step 1 — ERC1155 tokenId=%s registered in NonFungibleAssetGroup", tokenId)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Alice deposits ERC20 tokens with Bob's commitment (depositV2).
	//
	// Alice encapsulates with Bob's view key to derive Bob's note salt,
	// then deposits with Bob's commitment so Bob has a spendable ERC20 note.
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

	depositERC20Tx, err := erc20Vault.Transact(auth, "depositV2",
		[]*big.Int{paymentAmount, bobInputCommitment},
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
	//   bobPaymentReceipt.statement[0]    == aliceDeliveryReceipt.statement[4]  (Bob's new NFT)
	//   aliceDeliveryReceipt.statement[0] == bobPaymentReceipt.statement[7]     (Alice's ERC20)
	//
	// Solution: each party pre-computes the other's output commitment and uses
	// it as their own stMessage.  The proofs are then generated with pre-determined
	// output salts (FromSalt variants).
	// ─────────────────────────────────────────────────────────────────────────

	// Bob pre-computes his incoming ERC1155 NFT commitment (Alice will put this as her output).
	saltBForNFT, ctINFT, err := core.Encapsulate(bobView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (NFT salt for Bob): %v", err)
	}
	saltBForNFTField := core.SaltBToField(saltBForNFT)
	ctIINFT, err := core.EncryptPayload(saltBForNFT, tokenId, nftValue)
	if err != nil {
		t.Fatalf("EncryptPayload (NFT ciphertext): %v", err)
	}
	bobNFTCommitment, err := core.Erc1155Commitment(tokenId, nftValue, bobSpend.PublicKey, saltBForNFTField)
	if err != nil {
		t.Fatalf("Erc1155Commitment (Bob's output): %v", err)
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
	// Step 4: Alice generates ERC1155 non-fungible ownership proof.
	//   stMessage = aliceERC20Commitment  (what Alice expects to receive)
	//   output    = bobNFTCommitment      (pre-computed, passed as wtSaltOut)
	// ─────────────────────────────────────────────────────────────────────────
	aliceResult, err := gnarkClient.Erc1155NonFungibleOwnershipProofFromSalt(
		aliceERC20Commitment, // stMessage: what Alice expects to receive
		nftValue,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceNFTSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		saltBForNFTField, // pre-computed output salt → produces bobNFTCommitment
		ctINFT,
		ctIINFT,
		merkleDepth,
		aliceNFTProof,
		big.NewInt(0), // stTreeNumber
		contractAddress,
		tokenId,
		stAssetGroupTreeNumber,
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155NonFungibleOwnershipProofFromSalt: %v", err)
	}
	if len(aliceResult.Proof) != 8 {
		t.Fatalf("expected 8-element ERC1155 proof, got %d", len(aliceResult.Proof))
	}
	if len(aliceResult.Statement) != 7 {
		t.Fatalf("expected 7-element ERC1155 statement, got %d", len(aliceResult.Statement))
	}
	t.Logf("Step 4 — Alice's ERC1155 proof generated")
	t.Logf("  aliceResult.statement[0] (stMessage)      = %s", aliceResult.Statement[0])
	t.Logf("  aliceResult.statement[4] (NFT output cmt) = %s", aliceResult.Statement[4])

	if aliceResult.Statement[4].Cmp(bobNFTCommitment) != 0 {
		t.Fatalf("ERC1155 output commitment mismatch: got %s, want %s",
			aliceResult.Statement[4], bobNFTCommitment)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Bob generates ERC20 JoinSplit proof.
	//   stMessage    = bobNFTCommitment       (what Bob expects to receive)
	//   first output = aliceERC20Commitment   (pre-computed, passed as wtSaltsOut[0])
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
		[]*big.Int{bobDepositSaltField, big.NewInt(0)},         // Bob's input salt from depositV2
		[]*big.Int{paymentAmount, big.NewInt(0)},
		[]*big.Int{aliceSpend.PublicKey, dummySpend.PublicKey},
		[]*big.Int{saltBForPaymentField, big.NewInt(0)},        // pre-computed output salts
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
	t.Logf("  bobResult.statement[0] (stMessage)          = %s", bobResult.Statement[0])
	t.Logf("  bobResult.statement[7] (Alice's ERC20 cmt)  = %s", bobResult.Statement[7])

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
	// Step 6: Build receipts and call EnygmaDvp.swap(bobPayment, aliceDelivery, 0, 2)
	// ─────────────────────────────────────────────────────────────────────────

	// Bob's ERC20 payment receipt (paymentVaultId=0 → Erc20CoinVault).
	bobPaymentSnarkProof := proofStringsToOnchain(t, bobResult.Proof)
	bobPaymentReceipt := buildReceipt(bobResult)
	bobPaymentReceipt.Proof = bobPaymentSnarkProof

	// Alice's ERC1155 delivery receipt (deliveryVaultId=2 → Erc1155CoinVault).
	// Statement is already the correct 7-element layout from the prover.
	// Do NOT use buildReceipt() / ContractStatement() — those only give 5 elements.
	aliceDeliverySnarkProof := proofStringsToOnchain(t, aliceResult.Proof)
	aliceDeliveryReceipt := onchainProofReceipt{
		Proof:           aliceDeliverySnarkProof,
		Statement:       aliceResult.Statement,
		NumberOfInputs:  big.NewInt(int64(aliceResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(aliceResult.NumberOfOutputs)),
	}

	t.Logf("Step 6 — Calling EnygmaDvp.swap(bobPaymentReceipt, aliceDeliveryReceipt, 0, 2)")
	swapTx, err := dvp.Transact(auth, "swap",
		bobPaymentReceipt,
		aliceDeliveryReceipt,
		big.NewInt(0), // paymentVaultId = ERC20 vault
		big.NewInt(2), // deliveryVaultId = ERC1155 vault
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
	foundNFTCmtInSwap  := false
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

	// Bob scans for his ERC1155 NFT note.
	nftEvents := []core.OnChainErc1155Event{{
		Commitment:      bobNFTCommitment,
		ContractAddress: contractAddress,
		CipherText:     ctINFT,
		EncTxData:    ctIINFT,
	}}
	bobNFTNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, nftEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes: %v", err)
	}
	if len(bobNFTNotes) != 1 {
		t.Fatalf("Bob expected 1 NFT note, got %d", len(bobNFTNotes))
	}
	if bobNFTNotes[0].TokenId.Cmp(tokenId) != 0 {
		t.Errorf("Bob NFT tokenId: got %s, want %s", bobNFTNotes[0].TokenId, tokenId)
	}
	if bobNFTNotes[0].Amount.Cmp(nftValue) != 0 {
		t.Errorf("Bob NFT value: got %s, want 1", bobNFTNotes[0].Amount)
	}
	t.Logf("Step 7 — Bob scanned his NFT note (tokenId=%s, value=%s)", bobNFTNotes[0].TokenId, bobNFTNotes[0].Amount)

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

	t.Logf("=== ERC1155 NFT ↔ ERC20 DVP SWAP ON-CHAIN COMPLETE ===")
	t.Logf("Alice burned ERC1155 NFT note → Bob gets ERC1155 NFT  (tokenId=%s)", bobNFTNotes[0].TokenId)
	t.Logf("Bob burned ERC20 note         → Alice gets %s tokens", aliceERC20Notes[0].Amount)
}

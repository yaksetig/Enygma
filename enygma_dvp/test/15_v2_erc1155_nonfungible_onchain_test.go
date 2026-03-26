package tests

// On-chain integration test for the V2 ERC1155 non-fungible ownership proof flow:
//   deposit → transfer (Alice→Bob)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2Erc1155NonFungibleOnChain -v -timeout 300s

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

// TestV2Erc1155NonFungibleOnChain_DepositTransfer exercises the V2 ERC1155 non-fungible
// ownership proof flow against a live Hardhat node + gnark proof server:
//
//	Step 1 — deposit: Alice deposits tokenId=99 (amount=1 non-fungible).
//	Step 2 — transfer: Alice proves ownership and transfers to Bob.
func TestV2Erc1155NonFungibleOnChain_DepositTransfer(t *testing.T) {
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
	vaultAddr := common.HexToAddress(receipts["Erc1155CoinVault"].ContractAddress)
	erc1155Addr := common.HexToAddress(receipts["ERC1155"].ContractAddress)

	// ── Load ABIs ─────────────────────────────────────────────────────────────
	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc1155CoinVault.sol/Erc1155CoinVault.json")
	erc1155ABI := loadOnchainABI(t, "erc1155/contracts/RaylsERC1155.sol/RaylsERC1155.json")

	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc1155 := bind.NewBoundContract(erc1155Addr, erc1155ABI, client, client, client)

	auth := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8

	contractAddress := new(big.Int).SetBytes(common.HexToAddress(receipts["ERC1155"].ContractAddress).Bytes())
	tokenId := big.NewInt(99)
	amount := big.NewInt(1) // non-fungible: amount = 1

	// ── Mint ERC1155 tokenId=99 to Alice (amount=1) ──────────────────────────
	mintTx, err := erc1155.Transact(auth, "mint", alice, tokenId, amount, []byte{})
	if err != nil {
		t.Fatalf("ERC1155.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted ERC1155 tokenId=%s (amount=1) to Alice", tokenId)

	// ── Alice sets approval for vault ─────────────────────────────────────────
	approvalTx, err := erc1155.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		t.Fatalf("ERC1155.setApprovalForAll: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approvalTx); err != nil {
		t.Fatalf("wait setApprovalForAll: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: deposit — Alice deposits tokenId=99 (amount=1)
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	// Non-fungible commitment: Poseidon(pk_spend, salt, amount=1, tokenId)
	aliceCommitment, err := core.Erc1155Commitment(tokenId, amount, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s", aliceCommitment)

	// deposit([amount, tokenId, commitment]) — single deposit (params.length == 3)
	depositParams := []*big.Int{amount, tokenId, aliceCommitment}
	depositTx, err := vault.Transact(auth, "deposit", depositParams)
	if err != nil {
		t.Fatalf("vault.deposit (ERC1155 non-fungible): %v", err)
	}
	depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
	if err != nil {
		t.Fatalf("wait deposit: %v", err)
	}
	t.Logf("Step 1 — deposit mined (block %d, gas %d)", depositReceipt.BlockNumber, depositReceipt.GasUsed)

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	foundCommitment := false
	for _, log := range depositReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundCommitment = true
			t.Logf("  Commitment event: %s", log.Topics[2].Big())
		}
	}
	if !foundCommitment {
		t.Errorf("Commitment event not found in deposit receipt")
	}

	// Build Merkle tree from all on-chain vault commitment events (matches on-chain state).
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice: %v", err)
	}
	t.Logf("Step 1 — Merkle root: %s", aliceProof.Root)

	// Build asset group tree
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
	t.Logf("Step 1 — asset group root: %s", assetGroupProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: transfer — Alice proves non-fungible ownership and transfers to Bob
	// ─────────────────────────────────────────────────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	ownershipResult, err := gnarkClient.Erc1155NonFungibleOwnershipProof(
		big.NewInt(1),
		amount,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0), // treeNumber
		contractAddress,
		tokenId,
		stAssetGroupTreeNumber,
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155NonFungibleOwnershipProof: %v", err)
	}
	if len(ownershipResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(ownershipResult.Proof))
	}
	t.Logf("Step 2 — gnark proof generated")

	// Build the on-chain receipt.
	// For ERC1155 non-fungible (1-in/1-out), the prover already returns a 7-element Statement:
	// [msg, treeNum, root, null, cmtOut, agTreeNum, agRoot]
	// The vault expects 3 + 3*1 + 1 = 7 elements — exact match.
	// We use Statement directly (already non-interleaved for 1-in/1-out).
	snarkProof := proofStringsToOnchain(t, ownershipResult.Proof)
	onchainReceipt := onchainProofReceipt{
		Proof:           snarkProof,
		Statement:       ownershipResult.Statement, // 7 elements: correct for ERC1155 non-fungible
		NumberOfInputs:  big.NewInt(int64(ownershipResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(ownershipResult.NumberOfOutputs)),
	}

	transferTx, err := vault.Transact(auth, "transfer", onchainReceipt)
	if err != nil {
		t.Fatalf("vault.transfer (ERC1155 non-fungible): %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transfer: %v", err)
	}
	t.Logf("Step 2 — transfer mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundBobCmt := false
	foundNullifier := false
	for _, log := range transferReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundBobCmt = true
			t.Logf("  Commitment event: %s", log.Topics[2].Big())
		}
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: %s", log.Topics[3].Big())
		}
	}
	if !foundBobCmt {
		t.Errorf("Bob's Commitment event not found in transfer receipt")
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in transfer receipt")
	}

	bobCommitment := ownershipResult.Statement[4]
	t.Logf("Step 2 — Bob's commitment: %s", bobCommitment)

	// Bob scans for his note
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitment,
		ContractAddress: contractAddress,
		CiphertextI:     ownershipResult.CiphertextI[0],
		CiphertextII:    ownershipResult.CiphertextII[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes: %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	t.Logf("Step 2 — Bob scanned his note (amount=%s, tokenId=%s)", bobNotes[0].Amount, bobNotes[0].TokenId)

	t.Logf("=== ERC1155 NON-FUNGIBLE ON-CHAIN TRANSFER COMPLETE ===")
}

// Deprecated: This file is legacy and will not be used in the current version.
package tests

// On-chain integration test for the V2 ERC1155 non-fungible ownership transfer:
//   deposit → transfer (Alice→Bob)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2Erc1155NonFungibleOnChain -v -timeout 300s
//
// ERC1155 NON-FUNGIBLE vs FUNGIBLE — KEY DIFFERENCES
// ────────────────────────────────────────────────────
// Non-fungible: 1-in / 1-out ownership transfer, value=1.
// Statement layout (7 elements, already non-interleaved from prover):
//   [msg, treeNum, merkleRoot, nullifier, commitment, assetGroupTree, assetGroupRoot]
// Use result.Statement directly — do NOT call ContractStatement() (only gives 5 elems).

import (
	"context"
	"math/big"
	"testing"

	"enygma_dvp/github.com/raylsnetwork/enygma_dvp/src"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// TestV2Erc1155NonFungibleOnChain_DepositTransfer exercises the V2 ERC1155
// non-fungible flow against a live Hardhat node + gnark proof server:
//
//	Step 1 — deposit: Alice deposits NFT tokenId=99 (value=1).
//	Step 2 — transfer: Alice proves ownership and transfers to Bob.
func TestV2Erc1155NonFungibleOnChain_DepositTransferV2(t *testing.T) {
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
	value := big.NewInt(1) // NFTs always have value=1

	// ── Mint 1 ERC1155 NFT to Alice ───────────────────────────────────────────
	mintTx, err := erc1155.Transact(auth, "mint", alice, tokenId, value, []byte{})
	if err != nil {
		t.Fatalf("ERC1155.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted ERC1155 NFT tokenId=%s (value=1) to Alice", tokenId)

	// ── Alice sets approval for vault ─────────────────────────────────────────
	approvalTx, err := erc1155.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		t.Fatalf("ERC1155.setApprovalForAll: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approvalTx); err != nil {
		t.Fatalf("wait setApprovalForAll: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: deposit — Alice deposits NFT tokenId=99
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	// V2 commitment: Poseidon(pk_spend, salt, value=1, tokenId)
	aliceCommitment, err := core.Erc1155Commitment(tokenId, value, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s", aliceCommitment)

	// deposit([amountOrOne=1, tokenId, commitment])
	depositParams := []*big.Int{value, tokenId, aliceCommitment}
	depositTx, err := vault.Transact(auth, "deposit", depositParams)
	if err != nil {
		t.Fatalf("vault.deposit (ERC1155 NFT): %v", err)
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

	// Build Merkle tree from all on-chain vault commitment events
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice: %v", err)
	}
	t.Logf("Step 1 — Merkle root: %s", aliceProof.Root)

	// Build asset group tree (uid uses value=0 per convention)
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

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: transfer — Alice proves ownership and transfers to Bob
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
		big.NewInt(1), // stMessage
		value,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0), // stTreeNumber
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
	if len(ownershipResult.Statement) != 7 {
		t.Fatalf("expected 7-element statement, got %d", len(ownershipResult.Statement))
	}
	t.Logf("Step 2 — gnark proof generated")

	// Build on-chain receipt.
	// Statement is already non-interleaved (7 elements, prover layout matches vault):
	//   [msg, treeNum, merkleRoot, nullifier, cmt, assetGroupTree, assetGroupRoot]
	// Do NOT use ContractStatement() — it only returns 5 elements.
	snarkProof := proofStringsToOnchain(t, ownershipResult.Proof)
	onchainReceipt := onchainProofReceipt{
		Proof:           snarkProof,
		Statement:       ownershipResult.Statement,
		NumberOfInputs:  big.NewInt(int64(ownershipResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(ownershipResult.NumberOfOutputs)),
	}

	transferTx, err := vault.Transact(auth, "transfer", onchainReceipt)
	if err != nil {
		t.Fatalf("vault.transfer (ERC1155 NFT): %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transfer: %v", err)
	}
	t.Logf("Step 2 — transfer mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	// Verify Commitment (Bob's new note) and Nullifier (Alice's note spent) events
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundBobCmt := false
	foundNullifier := false
	for _, log := range transferReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundBobCmt = true
			t.Logf("  Bob's Commitment event: %s", log.Topics[2].Big())
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

	// Statement indices: [0]=msg, [1]=treeNum, [2]=merkleRoot, [3]=nullifier,
	//                    [4]=cmt(Bob), [5]=assetGroupTree, [6]=assetGroupRoot
	bobCommitment := ownershipResult.Statement[4]
	t.Logf("Step 2 — Bob's commitment: %s", bobCommitment)

	// Bob scans for his note
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitment,
		ContractAddress: contractAddress,
		CipherText:     ownershipResult.CipherText[0],
		EncTxData:    ownershipResult.EncTxData[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes: %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	if bobNotes[0].Amount.Cmp(value) != 0 {
		t.Errorf("Bob amount mismatch: got %s, want 1", bobNotes[0].Amount)
	}
	if bobNotes[0].TokenId.Cmp(tokenId) != 0 {
		t.Errorf("Bob tokenId mismatch: got %s, want %s", bobNotes[0].TokenId, tokenId)
	}
	t.Logf("Step 2 — Bob scanned his note (tokenId=%s, amount=%s, salt=%s)",
		bobNotes[0].TokenId, bobNotes[0].Amount, bobNotes[0].SaltBField)

	t.Logf("=== ERC1155 NON-FUNGIBLE ON-CHAIN TRANSFER COMPLETE ===")
}

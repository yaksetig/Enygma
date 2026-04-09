package tests

// On-chain integration test for the V2 ERC1155 fungible JoinSplit flow:
//   deposit → transfer (Alice→Bob)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2Erc1155FungibleOnChain -v -timeout 300s

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

// buildErc1155FungibleReceipt builds the on-chain receipt for an ERC1155 fungible proof.
// The vault's checkReceiptConditions expects a (3 + 3*nIn + nOut)-element statement,
// which includes asset group tree number and root appended after the commitments.
func buildErc1155FungibleReceipt(
	result *onchainProofReceipt,
	stAssetGroupTreeNumber *big.Int,
	stAssetGroupMerkleRoot *big.Int,
) onchainProofReceipt {
	// result.Statement already has the non-interleaved 9-element layout from ContractStatement().
	// We need to append agRoot and agTreeNum to match the 11-element circuit public input order:
	// [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1, agRoot, agTreeNum]
	extended := make([]*big.Int, len(result.Statement)+2)
	copy(extended, result.Statement)
	extended[len(result.Statement)] = stAssetGroupMerkleRoot
	extended[len(result.Statement)+1] = stAssetGroupTreeNumber
	return onchainProofReceipt{
		Proof:           result.Proof,
		Statement:       extended,
		NumberOfInputs:  result.NumberOfInputs,
		NumberOfOutputs: result.NumberOfOutputs,
	}
}

// TestV2Erc1155FungibleOnChain_DepositTransfer exercises the V2 ERC1155 fungible flow
// against a live Hardhat node + gnark proof server:
//
//	Step 1 — deposit: Alice deposits 50 tokens of tokenId=42.
//	Step 2 — transfer: Alice JoinSplits to Bob (2-in/2-out with dummy second input).
func TestV2Erc1155FungibleOnChain_DepositTransfer(t *testing.T) {
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
	tokenId := big.NewInt(42)
	amount := big.NewInt(50)

	// ── Mint ERC1155 tokens to Alice ──────────────────────────────────────────
	mintTx, err := erc1155.Transact(auth, "mint", alice, tokenId, amount, []byte{})
	if err != nil {
		t.Fatalf("ERC1155.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted %s ERC1155 tokenId=%s to Alice", amount, tokenId)

	// ── Alice sets approval for vault ─────────────────────────────────────────
	approvalTx, err := erc1155.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		t.Fatalf("ERC1155.setApprovalForAll: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approvalTx); err != nil {
		t.Fatalf("wait setApprovalForAll: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: deposit — Alice deposits 50 tokens of tokenId=42
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	// V2 commitment: Poseidon(pk_spend, salt, amount, tokenId)
	aliceCommitment, err := core.Erc1155Commitment(tokenId, amount, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s", aliceCommitment)

	// deposit([amount, tokenId, commitment])
	depositParams := []*big.Int{amount, tokenId, aliceCommitment}
	depositTx, err := vault.Transact(auth, "deposit", depositParams)
	if err != nil {
		t.Fatalf("vault.deposit (ERC1155 fungible): %v", err)
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
	t.Logf("Step 1 — fungible Merkle root: %s", aliceProof.Root)

	// Build asset group tree (token UID for ERC1155)
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
	// Step 2: transfer — Alice JoinSplits to Bob (2-in/2-out with dummy)
	// ─────────────────────────────────────────────────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummyView: %v", err)
	}

	joinSplitResult, err := gnarkClient.Erc1155FungibleJoinSplitProof(
		big.NewInt(1),
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSalt, big.NewInt(0)},
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		contractAddress,
		tokenId,
		stAssetGroupTreeNumber,
		assetGroupProof,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleJoinSplitProof: %v", err)
	}
	if len(joinSplitResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(joinSplitResult.Proof))
	}
	t.Logf("Step 2 — gnark proof generated")

	// Build the on-chain receipt with extended 11-element statement.
	// ContractStatement() gives [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1] (9 elements).
	// ERC1155 vault checkReceiptConditions expects 3 + 3*nIn + nOut = 11 for 2in/2out.
	// We append [agRoot, agTreeNum] to satisfy both the size check and the verifier.
	snarkProof := proofStringsToOnchain(t, joinSplitResult.Proof)
	baseReceipt := buildReceipt(joinSplitResult)
	baseReceipt.Proof = snarkProof
	onchainReceipt := buildErc1155FungibleReceipt(&baseReceipt, stAssetGroupTreeNumber, assetGroupProof.Root)

	transferTx, err := vault.Transact(auth, "transfer", onchainReceipt)
	if err != nil {
		t.Fatalf("vault.transfer (ERC1155 fungible): %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transfer: %v", err)
	}
	t.Logf("Step 2 — transfer mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	// Verify Commitment (Bob's note) and Nullifier (Alice's note spent)
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
		t.Errorf("Commitment event not found in transfer receipt")
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in transfer receipt")
	}

	bobCommitment := joinSplitResult.Statement[7]
	t.Logf("Step 2 — Bob's commitment (from statement): %s", bobCommitment)

	// Bob scans for his note
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitment,
		ContractAddress: contractAddress,
		CipherText:     joinSplitResult.CipherText[0],
		EncTxData:    joinSplitResult.EncTxData[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes: %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	t.Logf("Step 2 — Bob scanned his note (amount=%s, salt=%s)", bobNotes[0].Amount, bobNotes[0].SaltBField)

	t.Logf("=== ERC1155 FUNGIBLE ON-CHAIN TRANSFER COMPLETE ===")
}

package tests

// On-chain integration test for the V2 ERC1155 fungible with-broker flow:
//   deposit → transfer (Alice→Bob with broker commission)
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
// The Erc1155CoinVault contract must have broker size detection enabled
// (expectedBrokerSize / receiptType=3 branch un-commented).
//
// Run with:
//   CC=/usr/bin/clang go test -run TestV2Erc1155FungibleBrokerOnChain -v -timeout 300s

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

// buildBrokerReceipt wraps a broker ProofResult into an onchainProofReceipt.
// The broker statement is already non-interleaved (built by the prover as
// [msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1, cmt2, blindedPk, commRate, agTreeNum, agRoot]),
// so we use result.Statement directly — do NOT call ContractStatement().
func buildBrokerReceipt(result *core.ProofResult) onchainProofReceipt {
	return onchainProofReceipt{
		Statement:       result.Statement,
		NumberOfInputs:  big.NewInt(int64(result.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(result.NumberOfOutputs)),
	}
}

// TestV2Erc1155FungibleBrokerOnChain_DepositTransfer exercises the ERC1155
// fungible with-broker flow against a live Hardhat node + gnark proof server:
//
//	Step 1 — deposit: Alice deposits 110 tokens of tokenId=7.
//	Step 2 — transfer with broker: Bob gets 100, change=0, broker commission=10.
func TestV2Erc1155FungibleBrokerOnChain_DepositTransfer(t *testing.T) {
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
	tokenId := big.NewInt(7)

	// Commission: rate=10 means 10% (rate / 10^2)
	// 110 in: Bob=100, change=0, broker=10; 100 * 10/100 = 10 ✓
	commissionRate  := big.NewInt(10)
	totalAmount     := big.NewInt(110)
	recipientAmount := big.NewInt(100)
	changeAmount    := big.NewInt(0)
	commissionAmount := big.NewInt(10)

	// ── Mint ERC1155 tokens to Alice ──────────────────────────────────────────
	mintTx, err := erc1155.Transact(auth, "mint", alice, tokenId, totalAmount, []byte{})
	if err != nil {
		t.Fatalf("ERC1155.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted %s ERC1155 tokenId=%s to Alice", totalAmount, tokenId)

	// ── Alice sets approval for vault ─────────────────────────────────────────
	approvalTx, err := erc1155.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		t.Fatalf("ERC1155.setApprovalForAll: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approvalTx); err != nil {
		t.Fatalf("wait setApprovalForAll: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: deposit — Alice deposits 110 tokens of tokenId=7
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField: %v", err)
	}

	aliceCommitment, err := core.Erc1155Commitment(tokenId, totalAmount, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc1155Commitment: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s", aliceCommitment)

	// deposit([amount, tokenId, commitment])
	depositParams := []*big.Int{totalAmount, tokenId, aliceCommitment}
	depositTx, err := vault.Transact(auth, "deposit", depositParams)
	if err != nil {
		t.Fatalf("vault.deposit (ERC1155 broker): %v", err)
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

	// Build Merkle tree from on-chain vault commitment events
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
	t.Logf("Step 1 — Asset group root: %s", assetGroupProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: transfer with broker — Bob=100, change=0, broker=10
	// ─────────────────────────────────────────────────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	brokerSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Broker NewSpendKeyPair: %v", err)
	}
	brokerView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Broker NewViewKeyPair: %v", err)
	}
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummySpend: %v", err)
	}

	// keysOut[0]=Bob (recipient), keysOut[1]=Alice (change), keysOut[2]=Broker
	brokerResult, err := gnarkClient.Erc1155FungibleWithBrokerV1Proof(
		big.NewInt(1), // stMessage
		[]*big.Int{totalAmount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSalt, big.NewInt(0)},
		[]*big.Int{recipientAmount, changeAmount, commissionAmount},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: brokerSpend.PrivateKey, PublicKey: brokerSpend.PublicKey},
		},
		[][]byte{bobView.EncapsKey, aliceView.EncapsKey, brokerView.EncapsKey},
		merkleDepth,
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		contractAddress,
		tokenId,
		stAssetGroupTreeNumber,
		assetGroupProof,
		brokerSpend.PublicKey,
		commissionRate,
	)
	if err != nil {
		t.Fatalf("Erc1155FungibleWithBrokerV1Proof: %v", err)
	}
	if len(brokerResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(brokerResult.Proof))
	}
	if len(brokerResult.Statement) != 14 {
		t.Fatalf("expected 14-element statement, got %d", len(brokerResult.Statement))
	}
	t.Logf("Step 2 — gnark proof generated")

	// Build the on-chain receipt.
	// Statement is already non-interleaved (prover builds it in vault layout),
	// so we use result.Statement directly — do NOT call ContractStatement().
	snarkProof := proofStringsToOnchain(t, brokerResult.Proof)
	onchainReceipt := buildBrokerReceipt(brokerResult)
	onchainReceipt.Proof = snarkProof

	transferTx, err := vault.Transact(auth, "transfer", onchainReceipt)
	if err != nil {
		t.Fatalf("vault.transfer (ERC1155 broker): %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transfer: %v", err)
	}
	t.Logf("Step 2 — transfer mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	// Verify events: 3 Commitment events + 1 Nullifier event
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	cmtCount := 0
	foundNullifier := false
	for _, log := range transferReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			cmtCount++
			t.Logf("  Commitment event: %s", log.Topics[2].Big())
		}
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: %s", log.Topics[3].Big())
		}
	}
	if cmtCount != 3 {
		t.Errorf("expected 3 Commitment events, got %d", cmtCount)
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in transfer receipt")
	}

	// Statement indices (non-interleaved, nIn=2, nOut=3):
	// [0]=msg, [1]=tree0, [2]=tree1, [3]=root0, [4]=root1, [5]=null0, [6]=null1,
	// [7]=cmt0(Bob), [8]=cmt1(change), [9]=cmt2(broker), [10]=blindedPk, [11]=commRate,
	// [12]=agTreeNum, [13]=agRoot
	bobCommitment    := brokerResult.Statement[7]
	changeCommitment := brokerResult.Statement[8]
	brokerCommitment := brokerResult.Statement[9]
	t.Logf("Step 2 — Bob's commitment: %s", bobCommitment)
	t.Logf("Step 2 — Change commitment: %s", changeCommitment)
	t.Logf("Step 2 — Broker commitment: %s", brokerCommitment)

	// Bob scans for his note (100 tokens)
	bobEvents := []core.OnChainErc1155Event{{
		Commitment:      bobCommitment,
		ContractAddress: contractAddress,
		CiphertextI:     brokerResult.CiphertextI[0],
		CiphertextII:    brokerResult.CiphertextII[0],
	}}
	bobNotes, err := core.ScanForErc1155Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	if bobNotes[0].Amount.Cmp(recipientAmount) != 0 {
		t.Errorf("Bob amount mismatch: got %s, want %s", bobNotes[0].Amount, recipientAmount)
	}
	t.Logf("Step 2 — Bob scanned his note (amount=%s)", bobNotes[0].Amount)

	// Broker scans for its commission (10 tokens)
	brokerEvents := []core.OnChainErc1155Event{{
		Commitment:      brokerCommitment,
		ContractAddress: contractAddress,
		CiphertextI:     brokerResult.CiphertextI[2],
		CiphertextII:    brokerResult.CiphertextII[2],
	}}
	brokerNotes, err := core.ScanForErc1155Notes(brokerView.DecapsKey, brokerSpend.PublicKey, brokerEvents)
	if err != nil {
		t.Fatalf("ScanForErc1155Notes (Broker): %v", err)
	}
	if len(brokerNotes) != 1 {
		t.Fatalf("Broker expected 1 note, got %d", len(brokerNotes))
	}
	if brokerNotes[0].Amount.Cmp(commissionAmount) != 0 {
		t.Errorf("Broker commission mismatch: got %s, want %s", brokerNotes[0].Amount, commissionAmount)
	}
	t.Logf("Step 2 — Broker scanned commission note (amount=%s)", brokerNotes[0].Amount)

	t.Logf("=== ERC1155 FUNGIBLE WITH-BROKER ON-CHAIN TRANSFER COMPLETE ===")
}

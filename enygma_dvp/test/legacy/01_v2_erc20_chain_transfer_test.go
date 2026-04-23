// Deprecated: This file is legacy and will not be used in the current version.
package tests

// On-chain V2 ERC20 three-hop transfer chain:
//   depositV2 → Alice→Bob transferV2 → Bob→Carol transferV2 → Carol withdrawV2
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2Erc20ChainOnChain_AliceBobCarolWithdraw -v -timeout 600s

import (
	"context"
	"math/big"
	"testing"

	"github.com/raylsnetwork/enygma_dvp/src/core"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestV2Erc20ChainOnChain_AliceBobCarolWithdraw(t *testing.T) {
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
	vaultAddr    := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc20Addr    := common.HexToAddress(receipts["ERC20"].ContractAddress)
	recipientAddr := common.HexToAddress(hardhatRecipientHex)

	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc20ABI  := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")

	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc20 := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient  := core.NewGnarkClient("http://localhost:8081")
	merkleDepth  := 8
	tokenId      := big.NewInt(0)
	amount       := big.NewInt(30)

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	nullifierSig  := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))

	// ── Key pairs ─────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Alice NewSpendKeyPair: %v", err) }
	aliceView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Alice NewViewKeyPair: %v", err) }
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Bob NewSpendKeyPair: %v", err) }
	bobView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Bob NewViewKeyPair: %v", err) }
	carolSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Carol NewSpendKeyPair: %v", err) }
	carolView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Carol NewViewKeyPair: %v", err) }
	dummySpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("dummySpend: %v", err) }
	dummyView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("dummyView: %v", err) }

	// ── Mint ERC20 to Alice and approve vault ─────────────────────────────────
	mintTx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(amount, big.NewInt(10)))
	if err != nil { t.Fatalf("ERC20.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	approveTx, err := erc20.Transact(auth, "approve", vaultAddr, amount)
	if err != nil { t.Fatalf("ERC20.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait approve: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: Alice depositV2 — 30 tokens
	// ─────────────────────────────────────────────────────────────────────────
	aliceSaltB, aliceCapsule, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil { t.Fatalf("Alice Encapsulate: %v", err) }
	aliceSaltField := core.SaltBToField(aliceSaltB)

	aliceCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltField, amount, tokenId)
	if err != nil { t.Fatalf("Alice Erc20CommitmentV2: %v", err) }

	aliceCtII, err := core.EncryptPayload(aliceSaltB, tokenId, amount)
	if err != nil { t.Fatalf("Alice EncryptPayload: %v", err) }

	depositTx, err := vault.Transact(auth, "depositV2",
		[]*big.Int{amount, aliceCommitment}, aliceCapsule, aliceCtII)
	if err != nil { t.Fatalf("vault.depositV2: %v", err) }
	depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
	if err != nil { t.Fatalf("wait depositV2: %v", err) }
	t.Logf("Step 1 — depositV2 mined (block %d, gas %d)", depositReceipt.BlockNumber, depositReceipt.GasUsed)
	t.Logf("  Alice commitment: %s", aliceCommitment)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Alice → Bob transferV2
	// ─────────────────────────────────────────────────────────────────────────
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil { t.Fatalf("GenerateProof (Alice): %v", err) }

	aliceToBobResult, err := gnarkClient.Erc20JoinSplitProof(
		big.NewInt(1),
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSaltField, big.NewInt(0)},
		[]*big.Int{amount, big.NewInt(0)},
		[]*big.Int{bobSpend.PublicKey, dummySpend.PublicKey},
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil { t.Fatalf("Alice→Bob Erc20JoinSplitProof: %v", err) }

	snarkProof1 := proofStringsToOnchain(t, aliceToBobResult.Proof)
	receipt1 := buildReceipt(aliceToBobResult)
	receipt1.Proof = snarkProof1

	transfer1Tx, err := vault.Transact(auth, "transferV2",
		receipt1, aliceToBobResult.CipherText, aliceToBobResult.EncTxData)
	if err != nil { t.Fatalf("vault.transferV2 (Alice→Bob): %v", err) }
	transfer1Receipt, err := bind.WaitMined(ctx, client, transfer1Tx)
	if err != nil { t.Fatalf("wait transferV2 (Alice→Bob): %v", err) }
	t.Logf("Step 2 — Alice→Bob transferV2 mined (block %d, gas %d)", transfer1Receipt.BlockNumber, transfer1Receipt.GasUsed)

	foundCmt1, foundNull1 := false, false
	for _, log := range transfer1Receipt.Logs {
		if log.Topics[0] == commitmentSig { foundCmt1 = true }
		if log.Topics[0] == nullifierSig  { foundNull1 = true }
	}
	if !foundCmt1  { t.Errorf("Commitment event not found in Alice→Bob receipt") }
	if !foundNull1 { t.Errorf("Nullifier event not found in Alice→Bob receipt") }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Bob scans his note
	// ─────────────────────────────────────────────────────────────────────────
	bobEvents := []core.OnChainErc20Event{
		{Commitment: aliceToBobResult.Statement[7], CipherText: aliceToBobResult.CipherText[0], EncTxData: aliceToBobResult.EncTxData[0]},
		{Commitment: aliceToBobResult.Statement[8], CipherText: aliceToBobResult.CipherText[1], EncTxData: aliceToBobResult.EncTxData[1]},
	}
	bobNotes, err := core.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil { t.Fatalf("Bob ScanForErc20Notes: %v", err) }
	if len(bobNotes) != 1 { t.Fatalf("Bob expected 1 note, got %d", len(bobNotes)) }
	bobNote := bobNotes[0]
	if bobNote.Amount.Cmp(amount) != 0 {
		t.Errorf("Bob amount: got %s, want %s", bobNote.Amount, amount)
	}
	t.Logf("Step 3 — Bob scanned note: amount=%s", bobNote.Amount)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Bob → Carol transferV2
	// ─────────────────────────────────────────────────────────────────────────
	mt = loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	bobProof, err := mt.GenerateProof(bobNote.Commitment)
	if err != nil { t.Fatalf("GenerateProof (Bob): %v", err) }

	bobToCarolResult, err := gnarkClient.Erc20JoinSplitProof(
		big.NewInt(2),
		[]*big.Int{bobNote.Amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{bobNote.SaltBField, big.NewInt(0)},
		[]*big.Int{amount, big.NewInt(0)},
		[]*big.Int{carolSpend.PublicKey, dummySpend.PublicKey},
		[][]byte{carolView.EncapsKey, dummyView.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil { t.Fatalf("Bob→Carol Erc20JoinSplitProof: %v", err) }

	snarkProof2 := proofStringsToOnchain(t, bobToCarolResult.Proof)
	receipt2 := buildReceipt(bobToCarolResult)
	receipt2.Proof = snarkProof2

	transfer2Tx, err := vault.Transact(auth, "transferV2",
		receipt2, bobToCarolResult.CipherText, bobToCarolResult.EncTxData)
	if err != nil { t.Fatalf("vault.transferV2 (Bob→Carol): %v", err) }
	transfer2Receipt, err := bind.WaitMined(ctx, client, transfer2Tx)
	if err != nil { t.Fatalf("wait transferV2 (Bob→Carol): %v", err) }
	t.Logf("Step 4 — Bob→Carol transferV2 mined (block %d, gas %d)", transfer2Receipt.BlockNumber, transfer2Receipt.GasUsed)

	foundCmt2, foundNull2 := false, false
	for _, log := range transfer2Receipt.Logs {
		if log.Topics[0] == commitmentSig { foundCmt2 = true }
		if log.Topics[0] == nullifierSig  { foundNull2 = true }
	}
	if !foundCmt2  { t.Errorf("Commitment event not found in Bob→Carol receipt") }
	if !foundNull2 { t.Errorf("Nullifier event not found in Bob→Carol receipt") }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: Carol scans her note
	// ─────────────────────────────────────────────────────────────────────────
	carolEvents := []core.OnChainErc20Event{
		{Commitment: bobToCarolResult.Statement[7], CipherText: bobToCarolResult.CipherText[0], EncTxData: bobToCarolResult.EncTxData[0]},
		{Commitment: bobToCarolResult.Statement[8], CipherText: bobToCarolResult.CipherText[1], EncTxData: bobToCarolResult.EncTxData[1]},
	}
	carolNotes, err := core.ScanForErc20Notes(carolView.DecapsKey, carolSpend.PublicKey, carolEvents)
	if err != nil { t.Fatalf("Carol ScanForErc20Notes: %v", err) }
	if len(carolNotes) != 1 { t.Fatalf("Carol expected 1 note, got %d", len(carolNotes)) }
	carolNote := carolNotes[0]
	if carolNote.Amount.Cmp(amount) != 0 {
		t.Errorf("Carol amount: got %s, want %s", carolNote.Amount, amount)
	}
	t.Logf("Step 5 — Carol scanned note: amount=%s", carolNote.Amount)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Carol withdrawV2 → recipient
	// ─────────────────────────────────────────────────────────────────────────
	mt = loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	carolProof, err := mt.GenerateProof(carolNote.Commitment)
	if err != nil { t.Fatalf("GenerateProof (Carol): %v", err) }

	dummyWithdrawSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("dummyWithdrawSpend: %v", err) }

	recipientBig := new(big.Int).SetBytes(recipientAddr.Bytes())

	var balanceBefore []interface{}
	if err := erc20.Call(nil, &balanceBefore, "balanceOf", recipientAddr); err != nil {
		t.Fatalf("balanceOf before: %v", err)
	}
	balBeforeBig := balanceBefore[0].(*big.Int)

	withdrawResult, err := gnarkClient.Erc20WithdrawProof(
		big.NewInt(3),
		[]*big.Int{carolNote.Amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: carolSpend.PrivateKey, PublicKey: carolSpend.PublicKey},
			{PrivateKey: dummyWithdrawSpend.PrivateKey, PublicKey: dummyWithdrawSpend.PublicKey},
		},
		[]*big.Int{carolNote.SaltBField, big.NewInt(0)},
		carolNote.Amount,
		recipientBig,
		dummyWithdrawSpend.PublicKey,
		merkleDepth,
		[]*core.MerkleProof{carolProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil { t.Fatalf("Carol Erc20WithdrawProof: %v", err) }

	withdrawSnarkProof := proofStringsToOnchain(t, withdrawResult.Proof)
	withdrawReceipt := buildReceipt(withdrawResult)
	withdrawReceipt.Proof = withdrawSnarkProof

	withdrawTx, err := vault.Transact(auth, "withdrawV2",
		[]*big.Int{carolNote.Amount, tokenId},
		recipientAddr,
		withdrawReceipt,
	)
	if err != nil { t.Fatalf("vault.withdrawV2: %v", err) }
	withdrawTxReceipt, err := bind.WaitMined(ctx, client, withdrawTx)
	if err != nil { t.Fatalf("wait withdrawV2: %v", err) }
	t.Logf("Step 6 — withdrawV2 mined (block %d, gas %d)", withdrawTxReceipt.BlockNumber, withdrawTxReceipt.GasUsed)

	foundNullifier := false
	for _, log := range withdrawTxReceipt.Logs {
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: %s", log.Topics[3].Big())
		}
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in withdrawV2 receipt")
	}

	var balanceAfter []interface{}
	if err := erc20.Call(nil, &balanceAfter, "balanceOf", recipientAddr); err != nil {
		t.Fatalf("balanceOf after: %v", err)
	}
	balAfterBig := balanceAfter[0].(*big.Int)
	gained := new(big.Int).Sub(balAfterBig, balBeforeBig)
	if gained.Cmp(amount) != 0 {
		t.Errorf("recipient balance gain: got %s, want %s", gained, amount)
	}
	t.Logf("Step 6 — recipient received %s tokens ✓", gained)
	t.Logf("=== ERC20 CHAIN TRANSFER ON-CHAIN COMPLETE ===")
	t.Logf("Alice→Bob: commitment=%s", bobNote.Commitment)
	t.Logf("Bob→Carol: commitment=%s", carolNote.Commitment)
	t.Logf("Carol→recipient: %s tokens withdrawn", gained)
}

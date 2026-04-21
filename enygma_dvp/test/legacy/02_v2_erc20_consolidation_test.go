// Deprecated: This file is legacy and will not be used in the current version.
package tests

// On-chain V2 ERC20 consolidation using the 10-input / 2-output circuit:
//   5 × depositV2 (10 tokens each) → transferV2 (10-in/2-out) → Alice receives 50 tokens
//
// Prerequisites: same as other on-chain tests (Hardhat node, gnark server, deploy+init).
//
// Run with:
//    go test -run TestV2Erc20ConsolidationOnChain -v -timeout 600s

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

func TestV2Erc20ConsolidationOnChain(t *testing.T) {
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
	vaultAddr := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc20Addr := common.HexToAddress(receipts["ERC20"].ContractAddress)

	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc20ABI  := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")

	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc20 := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	auth  := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient   := core.NewGnarkClient("http://localhost:8081")
	merkleDepth   := 8
	tokenId       := big.NewInt(0)
	perNoteAmount := big.NewInt(10)
	numRealInputs := 5
	totalAmount   := big.NewInt(50)
	totalInputs   := 10

	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	nullifierSig  := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))

	// ── Mint 50 tokens to Alice and approve vault ─────────────────────────────
	mintTx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(totalAmount, big.NewInt(2)))
	if err != nil { t.Fatalf("ERC20.mint: %v", err) }
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil { t.Fatalf("wait mint: %v", err) }

	approveTx, err := erc20.Transact(auth, "approve", vaultAddr, totalAmount)
	if err != nil { t.Fatalf("ERC20.approve: %v", err) }
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil { t.Fatalf("wait approve: %v", err) }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: Deposit 5 notes of 10 tokens each (separate spend keys per note)
	// ─────────────────────────────────────────────────────────────────────────
	inputSpends := make([]*core.SpendKeyPair, numRealInputs)
	inputSalts  := make([]*big.Int, numRealInputs)
	inputCmts   := make([]*big.Int, numRealInputs)

	for i := 0; i < numRealInputs; i++ {
		sp, err := core.NewSpendKeyPair()
		if err != nil { t.Fatalf("spend[%d] NewSpendKeyPair: %v", i, err) }
		inputSpends[i] = sp

		vw, err := core.NewViewKeyPair()
		if err != nil { t.Fatalf("view[%d] NewViewKeyPair: %v", i, err) }

		saltB, capsule, err := core.Encapsulate(vw.EncapsKey)
		if err != nil { t.Fatalf("Encapsulate[%d]: %v", i, err) }
		saltField := core.SaltBToField(saltB)
		inputSalts[i] = saltField

		cmt, err := core.Erc20CommitmentV2(sp.PublicKey, saltField, perNoteAmount, tokenId)
		if err != nil { t.Fatalf("Erc20CommitmentV2[%d]: %v", i, err) }
		inputCmts[i] = cmt

		ctII, err := core.EncryptPayload(saltB, tokenId, perNoteAmount)
		if err != nil { t.Fatalf("EncryptPayload[%d]: %v", i, err) }

		depositTx, err := vault.Transact(auth, "depositV2",
			[]*big.Int{perNoteAmount, cmt}, capsule, ctII)
		if err != nil { t.Fatalf("vault.depositV2[%d]: %v", i, err) }
		depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
		if err != nil { t.Fatalf("wait depositV2[%d]: %v", i, err) }
		t.Logf("Step 1 — deposit[%d] mined (block %d, gas %d, cmt=%s)",
			i, depositReceipt.BlockNumber, depositReceipt.GasUsed, cmt)
	}

	// Build Merkle tree from all vault commitments and generate proofs for all 5 inputs.
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	inputProofs := make([]*core.MerkleProof, numRealInputs)
	for i := 0; i < numRealInputs; i++ {
		proof, err := mt.GenerateProof(inputCmts[i])
		if err != nil { t.Fatalf("GenerateProof[%d]: %v", i, err) }
		inputProofs[i] = proof
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: 10-in / 2-out consolidation — all 5 notes to Alice
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("Alice NewSpendKeyPair: %v", err) }
	aliceView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("Alice NewViewKeyPair: %v", err) }
	dummyOutSpend, err := core.NewSpendKeyPair()
	if err != nil { t.Fatalf("dummyOutSpend: %v", err) }
	dummyOutView, err := core.NewViewKeyPair()
	if err != nil { t.Fatalf("dummyOutView: %v", err) }

	wtValuesIn    := make([]*big.Int, totalInputs)
	keysIn        := make([]core.KeyPair, totalInputs)
	wtSaltsIn     := make([]*big.Int, totalInputs)
	merkleProofs  := make([]*core.MerkleProof, totalInputs)
	stTreeNumbers := make([]*big.Int, totalInputs)

	for i := 0; i < numRealInputs; i++ {
		wtValuesIn[i]    = new(big.Int).Set(perNoteAmount)
		keysIn[i]        = core.KeyPair{PrivateKey: inputSpends[i].PrivateKey, PublicKey: inputSpends[i].PublicKey}
		wtSaltsIn[i]     = inputSalts[i]
		merkleProofs[i]  = inputProofs[i]
		stTreeNumbers[i] = big.NewInt(0)
	}
	for i := numRealInputs; i < totalInputs; i++ {
		dummyIn, err := core.NewSpendKeyPair()
		if err != nil { t.Fatalf("dummyIn[%d]: %v", i, err) }
		wtValuesIn[i]    = big.NewInt(0)
		keysIn[i]        = core.KeyPair{PrivateKey: dummyIn.PrivateKey, PublicKey: dummyIn.PublicKey}
		wtSaltsIn[i]     = big.NewInt(0)
		merkleProofs[i]  = makeDummyProof(merkleDepth)
		stTreeNumbers[i] = big.NewInt(0)
	}

	consolidateResult, err := gnarkClient.Erc20JoinSplitProof(
		big.NewInt(1),
		wtValuesIn,
		keysIn,
		wtSaltsIn,
		[]*big.Int{totalAmount, big.NewInt(0)},
		[]*big.Int{aliceSpend.PublicKey, dummyOutSpend.PublicKey},
		[][]byte{aliceView.EncapsKey, dummyOutView.EncapsKey},
		merkleDepth,
		merkleProofs,
		stTreeNumbers,
		tokenId,
		true, // 10-in / 2-out circuit
	)
	if err != nil { t.Fatalf("Erc20JoinSplitProof (10-in): %v", err) }
	if len(consolidateResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(consolidateResult.Proof))
	}

	// Statement layout (10-in / 2-out, non-interleaved): 1 + 3*10 + 2 = 33 elements.
	aliceCmtIdx := 1 + 3*totalInputs // = 31
	t.Logf("Step 2 — gnark proof generated; Alice commitment (idx %d): %s",
		aliceCmtIdx, consolidateResult.Statement[aliceCmtIdx])

	snarkProof := proofStringsToOnchain(t, consolidateResult.Proof)
	receipt := buildReceipt(consolidateResult)
	receipt.Proof = snarkProof

	transferTx, err := vault.Transact(auth, "transferV2",
		receipt, consolidateResult.CipherText, consolidateResult.EncTxData)
	if err != nil { t.Fatalf("vault.transferV2 (consolidation): %v", err) }
	transferTxReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil { t.Fatalf("wait transferV2: %v", err) }
	t.Logf("Step 2 — consolidation transferV2 mined (block %d, gas %d)",
		transferTxReceipt.BlockNumber, transferTxReceipt.GasUsed)

	foundCmt, foundNull := false, false
	for _, log := range transferTxReceipt.Logs {
		if log.Topics[0] == commitmentSig { foundCmt = true }
		if log.Topics[0] == nullifierSig  { foundNull = true }
	}
	if !foundCmt  { t.Errorf("Commitment event not found in consolidation receipt") }
	if !foundNull { t.Errorf("Nullifier event not found in consolidation receipt") }

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Alice scans for her consolidated note
	// ─────────────────────────────────────────────────────────────────────────
	aliceEvents := []core.OnChainErc20Event{
		{
			Commitment:   consolidateResult.Statement[aliceCmtIdx],
			CipherText:  consolidateResult.CipherText[0],
			EncTxData: consolidateResult.EncTxData[0],
		},
		{
			Commitment:   consolidateResult.Statement[aliceCmtIdx+1],
			CipherText:  consolidateResult.CipherText[1],
			EncTxData: consolidateResult.EncTxData[1],
		},
	}
	aliceNotes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, aliceEvents)
	if err != nil { t.Fatalf("Alice ScanForErc20Notes: %v", err) }
	if len(aliceNotes) != 1 {
		t.Fatalf("Alice expected 1 consolidated note, got %d", len(aliceNotes))
	}
	aliceNote := aliceNotes[0]

	if aliceNote.TokenId.Cmp(tokenId) != 0 {
		t.Errorf("tokenId: got %s, want %s", aliceNote.TokenId, tokenId)
	}
	if aliceNote.Amount.Cmp(totalAmount) != 0 {
		t.Errorf("amount: got %s, want %s", aliceNote.Amount, totalAmount)
	}
	if aliceNote.Commitment.Cmp(consolidateResult.Statement[aliceCmtIdx]) != 0 {
		t.Errorf("commitment mismatch")
	}

	// Verify spend-readiness
	recomputed, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceNote.SaltBField, aliceNote.Amount, aliceNote.TokenId)
	if err != nil { t.Fatalf("Erc20CommitmentV2 recompute: %v", err) }
	if recomputed.Cmp(aliceNote.Commitment) != 0 {
		t.Errorf("Alice spend-readiness check failed")
	}

	t.Logf("Step 3 — Alice consolidated note: amount=%s, spend-ready ✓", aliceNote.Amount)
	t.Logf("=== ERC20 CONSOLIDATION ON-CHAIN COMPLETE ===")
	t.Logf("5 notes (10 each) → 1 consolidated note (50) for Alice")
}

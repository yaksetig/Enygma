package tests

// On-chain integration test for the private ERC20 Payment flow with change:
//
//   Alice has 40 USDT in a private note.
//   She wants to pay 30 USDT to Bob and receive 10 USDT change.
//
//   Step 1 — depositV2:  Alice deposits 40 USDT; commitment inserted on-chain.
//   Step 2 — payment:    Alice proves ownership and sends 30 to Bob / 10 change to herself.
//             - Payment circuit: 2 inputs (Alice's note + dummy), 2 outputs (Bob + Alice).
//             - Each output encrypted with ML-KEM + HKDF + AES-GCM.
//             - On-chain: EnygmaDvp.payment() emits a Payment event per output.
//   Step 3 — Bob scans:  Bob decapsulates, derives key, decrypts, verifies commitment.
//   Step 4 — Alice scans her change note the same way.
//
// Prerequisites (all must be running/completed before this test):
//   1. Hardhat node:        npx hardhat node
//   2. Deploy contracts:    cd scripts && CC=/usr/bin/clang go build -o /tmp/deploy deploy.go enygma.go && cd .. && /tmp/deploy
//   3. Export VKs:          cd gnark_circuits && go run ./cmd/export_vk_init/ ../build
//   4. Init contracts:      cd scripts && CC=/usr/bin/clang go build -o /tmp/init init.go enygma.go && cd .. && /tmp/init
//   5. Generate Payment keys: cd gnark_circuits && go run generation.go  (generates PaymentPK/VK.key)
//   6. Gnark server:        cd gnark_circuits && go run main.go
//
// Run with:
//   cd test && CC=/usr/bin/clang go test -run TestV2Erc20Payment -v -timeout 600s

import (
	"context"
	"math/big"
	"testing"

	"enygma_dvp/github.com/raylsnetwork/enygma_dvp/src"
	endpoints "enygma_dvp/github.com/raylsnetwork/enygma_dvp/src/endpoints"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestV2Erc20Payment(t *testing.T) {
	if !chainAvailable() {
		t.Skip("Hardhat node not running on localhost:8545 — skipping on-chain test")
	}
	if !serverAvailable("localhost:8081") {
		t.Skip("gnark server not running on localhost:8081 — skipping on-chain test")
	}

	ctx := context.Background()

	// ── Connect ───────────────────────────────────────────────────────────────────
	client, err := ethclient.Dial(hardhatRPC)
	if err != nil {
		t.Fatalf("ethclient.Dial: %v", err)
	}
	defer client.Close()

	// ── Load contract addresses from deploy receipts ───────────────────────────────
	receipts := loadOnchainReceipts(t)
	vaultAddr := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc20Addr := common.HexToAddress(receipts["ERC20"].ContractAddress)
	dvpAddr   := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)

	// ── ABIs ──────────────────────────────────────────────────────────────────────
	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc20ABI := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")
	dvpABI   := loadOnchainABI(t, "core/contracts/EnygmaDvp.sol/EnygmaDvp.json")

	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc20 := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	auth := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	tokenId     := big.NewInt(0) // ERC20: tokenId = 0
	depositAmt  := big.NewInt(40)
	paymentAmt  := big.NewInt(30)
	changeAmt   := big.NewInt(10)

	// ── Mint & approve ────────────────────────────────────────────────────────────
	mintTx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(depositAmt, big.NewInt(10)))
	if err != nil {
		t.Fatalf("ERC20.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted tokens to Alice (%s)", alice.Hex())

	approveTx, err := erc20.Transact(auth, "approve", vaultAddr, depositAmt)
	if err != nil {
		t.Fatalf("ERC20.approve: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait approve: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// Step 1: depositV2 — Alice deposits 40 USDT into the private vault
	// ─────────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}

	// Alice encapsulates her own view key to deposit to herself.
	ss, capsule, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (deposit): %v", err)
	}
	aliceSaltB, err := core.DerivePaymentSalt(ss)
	if err != nil {
		t.Fatalf("DerivePaymentSalt (deposit): %v", err)
	}
	aliceEncKey, err := core.DerivePaymentKey(ss)
	if err != nil {
		t.Fatalf("DerivePaymentKey (deposit): %v", err)
	}
	aliceSaltBField := core.SaltBToField(aliceSaltB)

	aliceCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltBField, depositAmt, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2: %v", err)
	}

	aliceDepositCtxtII, err := core.EncryptPayload(aliceEncKey, tokenId, depositAmt)
	if err != nil {
		t.Fatalf("EncryptPayload (deposit): %v", err)
	}

	depositParams := []*big.Int{depositAmt, aliceCommitment}
	depositTx, err := vault.Transact(auth, "depositV2", depositParams, capsule, aliceDepositCtxtII)
	if err != nil {
		t.Fatalf("vault.depositV2: %v", err)
	}
	depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
	if err != nil {
		t.Fatalf("wait depositV2: %v", err)
	}
	t.Logf("Step 1 — depositV2 mined (block %d, gas %d, commitment %s)",
		depositReceipt.BlockNumber, depositReceipt.GasUsed, aliceCommitment)

	// Build Merkle tree from all on-chain commitment events.
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice: %v", err)
	}
	t.Logf("Step 1 — Merkle root: %s", aliceProof.Root)

	// ─────────────────────────────────────────────────────────────────────────────
	// Step 2: payment — Alice sends 30 USDT to Bob, keeps 10 as change
	//
	// Inputs:  [Alice's 40 USDT note, dummy (0 USDT)]
	// Outputs: [30 USDT to Bob, 10 USDT change to Alice]
	// ─────────────────────────────────────────────────────────────────────────────
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
		t.Fatalf("dummy NewSpendKeyPair: %v", err)
	}
	paymentResult, err := gnarkClient.PaymentProof(
		big.NewInt(0), // stMessage = 0 for standalone payment
		[]*big.Int{depositAmt, big.NewInt(0)}, // values in: [40, dummy=0]
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSaltBField, big.NewInt(0)}, // salts in: [Alice's deposit salt, dummy=0]
		[]*big.Int{paymentAmt, changeAmt},           // values out: [30 to Bob, 10 change]
		[]*big.Int{bobSpend.PublicKey, aliceSpend.PublicKey}, // recipients
		[][]byte{bobView.EncapsKey, aliceView.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // tree numbers
		tokenId,
	)
	if err != nil {
		t.Fatalf("PaymentProof: %v", err)
	}
	if len(paymentResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(paymentResult.Proof))
	}
	t.Logf("Step 2 — gnark Payment proof generated")
	t.Logf("  Bob's commitment    (output 0): %s", paymentResult.Statement[7])
	t.Logf("  Alice's change cmt  (output 1): %s", paymentResult.Statement[8])

	// Build on-chain ProofReceipt using the non-interleaved ContractStatement layout.
	snarkProof := proofStringsToOnchain(t, paymentResult.Proof)
	onchainReceipt := endpoints.ProofReceipt{
		Proof:           endpoints.SnarkProof{
			A: endpoints.G1Point{X: snarkProof.A.X, Y: snarkProof.A.Y},
			B: endpoints.G2Point{X: snarkProof.B.X, Y: snarkProof.B.Y},
			C: endpoints.G1Point{X: snarkProof.C.X, Y: snarkProof.C.Y},
		},
		Statement:       paymentResult.ContractStatement(),
		NumberOfInputs:  big.NewInt(int64(paymentResult.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(paymentResult.NumberOfOutputs)),
	}

	t.Logf("Step 2 — contract statement (%d elements): %v",
		len(onchainReceipt.Statement), onchainReceipt.Statement)

	// Submit via EnygmaDvp.payment()
	vaultId := big.NewInt(0) // VAULT_ID_ERC20 = 0
	onchainTx, err := endpoints.SubmitPayment(
		client, auth, dvpABI, dvpAddr,
		onchainReceipt,
		vaultId,
		paymentResult.CipherText,
		paymentResult.EncTxData,
	)
	if err != nil {
		t.Fatalf("SubmitPayment: %v", err)
	}
	t.Logf("Step 2 — payment() mined (block %d, gas %d)",
		onchainTx.BlockNumber, onchainTx.GasUsed)

	// Verify Payment events — one per output
	paymentSig := crypto.Keccak256Hash([]byte("Payment(uint256,uint256,bytes,bytes)"))
	var paymentEvents int
	for _, log := range onchainTx.Logs {
		if log.Topics[0] == paymentSig {
			paymentEvents++
			t.Logf("  Payment event: commitment=%s", log.Topics[2].Big())
		}
	}
	if paymentEvents != paymentResult.NumberOfOutputs {
		t.Errorf("expected %d Payment events, got %d", paymentResult.NumberOfOutputs, paymentEvents)
	}

	// Verify Nullifier events
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundNullifier := false
	for _, log := range onchainTx.Logs {
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: nullifier=%s", log.Topics[3].Big())
		}
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in payment receipt")
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// Step 3: Bob scans the Payment event for his 30 USDT note
	// ─────────────────────────────────────────────────────────────────────────────
	// Output 0 of the payment is Bob's note.
	bobEvents := []core.OnChainErc20Event{{
		Commitment:   paymentResult.Statement[7], // interleaved index 7 = cmt[0]
		CipherText:  paymentResult.CipherText[0],
		EncTxData: paymentResult.EncTxData[0],
	}}
	bobNotes, err := core.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc20Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	bobNote := bobNotes[0]
	if bobNote.Amount.Cmp(paymentAmt) != 0 {
		t.Errorf("Bob's note amount: got %s, want %s", bobNote.Amount, paymentAmt)
	}
	t.Logf("Step 3 — Bob scanned his note: amount=%s tokenId=%s saltBField=%s",
		bobNote.Amount, bobNote.TokenId, bobNote.SaltBField)

	// ─────────────────────────────────────────────────────────────────────────────
	// Step 4: Alice scans the Payment event for her 10 USDT change note
	// ─────────────────────────────────────────────────────────────────────────────
	// Output 1 of the payment is Alice's change.
	aliceChangeEvents := []core.OnChainErc20Event{{
		Commitment:   paymentResult.Statement[8], // interleaved index 8 = cmt[1]
		CipherText:  paymentResult.CipherText[1],
		EncTxData: paymentResult.EncTxData[1],
	}}
	aliceChangeNotes, err := core.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, aliceChangeEvents)
	if err != nil {
		t.Fatalf("ScanForErc20Notes (Alice change): %v", err)
	}
	if len(aliceChangeNotes) != 1 {
		t.Fatalf("Alice expected 1 change note, got %d", len(aliceChangeNotes))
	}
	aliceChangeNote := aliceChangeNotes[0]
	if aliceChangeNote.Amount.Cmp(changeAmt) != 0 {
		t.Errorf("Alice's change amount: got %s, want %s", aliceChangeNote.Amount, changeAmt)
	}
	t.Logf("Step 4 — Alice scanned her change note: amount=%s tokenId=%s saltBField=%s",
		aliceChangeNote.Amount, aliceChangeNote.TokenId, aliceChangeNote.SaltBField)

	t.Logf("=== PAYMENT COMPLETE: Alice paid %s USDT to Bob, kept %s USDT change ===",
		paymentAmt, changeAmt)
}

package tests

// On-chain integration tests for the ERC20 retail payment flow.
//
// TestRetailErc20_PrivateMint: exercises the PrivateMint flow.
//   - Issuer mints a private note directly to Alice's spend key.
//
// TestRetailErc20_Payment: full deposit → payment → scan cycle.
//   - Alice deposits 40 tokens.
//   - Alice pays 30 to Bob, keeps 10 as change.
//   - Bob and Alice scan their respective notes.
//
// Prerequisites:
//   1. Hardhat node:            npx hardhat node
//   2. Deploy contracts:        cd scripts && CC=/usr/bin/clang go build -o /tmp/rp_deploy deploy.go && cd .. && /tmp/rp_deploy
//   3. Export VKs:              cd gnark_circuits && go run generation.go  (generates PaymentPK/VK.key)
//   4. Init contracts:          cd scripts && CC=/usr/bin/clang go build -o /tmp/rp_init init.go && cd .. && /tmp/rp_init
//   5. Gnark server (port 8082): cd gnark_circuits && go run main.go
//
// Run with:
//   cd test && CC=/usr/bin/clang go test ./... -v -timeout 600s

import (
	"context"
	"math/big"
	"testing"

	rpcore "github.com/raylsnetwork/enygma_retail_payments/src/core"
	dvpcore "github.com/raylsnetwork/enygma_dvp/src/core"
	endpoints "github.com/raylsnetwork/enygma_dvp/src/core/endpoints"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ── PrivateMint ABI types ──────────────────────────────────────────────────────

type onchainPrivateMintProof struct {
	Proof        [8]*big.Int `abi:"proof"`
	PublicSignal [4]*big.Int `abi:"public_signal"`
}

func proofStringsToPrivateMint(t *testing.T, proofStrs []string, sigStrs []string) onchainPrivateMintProof {
	t.Helper()
	if len(proofStrs) != 8 {
		t.Fatalf("expected 8 proof elements, got %d", len(proofStrs))
	}
	if len(sigStrs) != 4 {
		t.Fatalf("expected 4 public signal elements, got %d", len(sigStrs))
	}
	var p onchainPrivateMintProof
	for i, s := range proofStrs {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("invalid proof element %d: %q", i, s)
		}
		p.Proof[i] = n
	}
	for i, s := range sigStrs {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("invalid public_signal element %d: %q", i, s)
		}
		p.PublicSignal[i] = n
	}
	return p
}

// ── TestRetailErc20_PrivateMint ────────────────────────────────────────────────

func TestRetailErc20_PrivateMint(t *testing.T) {
	if !chainAvailable() {
		t.Skip("Hardhat node not running on localhost:8545 — skipping on-chain test")
	}
	if !serverAvailable("localhost:8082") {
		t.Skip("gnark server not running on localhost:8082 — skipping on-chain test")
	}

	ctx := context.Background()

	client, err := ethclient.Dial(hardhatRPC)
	if err != nil {
		t.Fatalf("ethclient.Dial: %v", err)
	}
	defer client.Close()

	receipts := loadOnchainReceipts(t)
	dvpAddr := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)

	dvpABI := loadOnchainABI(t, "EnygmaDvp")
	dvp := bind.NewBoundContract(dvpAddr, dvpABI, client, client, client)
	auth := hardhatAuth(t, client)

	gnarkClient := rpcore.NewPaymentClient("")
	tokenId             := big.NewInt(0)
	mintAmount          := big.NewInt(100)
	vaultId             := big.NewInt(0)
	contractAddressBig  := new(big.Int).SetBytes(dvpAddr.Bytes())

	// Step 1 — Alice generates her spend key pair.
	aliceSpend, err := rpcore.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	t.Logf("Step 1 — Alice pk_spend: %s", aliceSpend.PublicKey)

	// Step 2 — Issuer requests ZK proof from gnark server.
	issuedSalt, err := rpcore.RandomInField()
	if err != nil {
		t.Fatalf("RandomInField (salt): %v", err)
	}

	mintResult, err := gnarkClient.Erc20PrivateMintProof(
		aliceSpend.PublicKey,
		issuedSalt,
		mintAmount,
		tokenId,
		contractAddressBig,
	)
	if err != nil {
		t.Fatalf("Erc20PrivateMintProof: %v", err)
	}
	t.Logf("Step 2 — commitment: %s", mintResult.Commitment)
	t.Logf("Step 2 — cipherText: %s", mintResult.CipherText)

	proof := proofStringsToPrivateMint(t,
		mintResult.ProofResponse.Proof,
		mintResult.ProofResponse.PublicSignal,
	)

	// Step 3 — Issuer calls EnygmaDvp.privateMint on-chain.
	mintTx, err := dvp.Transact(auth, "privateMint", vaultId, mintResult.Commitment, proof)
	if err != nil {
		t.Fatalf("EnygmaDvp.privateMint: %v", err)
	}
	mintReceipt, err := bind.WaitMined(ctx, client, mintTx)
	if err != nil {
		t.Fatalf("wait privateMint: %v", err)
	}
	t.Logf("Step 3 — privateMint mined (block %d, gas %d)",
		mintReceipt.BlockNumber, mintReceipt.GasUsed)

	privateMintSig := crypto.Keccak256Hash([]byte("PrivateMint(uint256,uint256,uint256)"))
	var eventCommitment, eventCipherText *big.Int
	for _, log := range mintReceipt.Logs {
		if log.Topics[0] == privateMintSig {
			eventCommitment = log.Topics[2].Big()
			eventCipherText = log.Topics[3].Big()
			t.Logf("  PrivateMint event: commitment=%s cipherText=%s", eventCommitment, eventCipherText)
		}
	}
	if eventCommitment == nil {
		t.Fatalf("PrivateMint event not found in transaction receipt")
	}
	if eventCommitment.Cmp(mintResult.Commitment) != 0 {
		t.Errorf("event commitment: got %s, want %s", eventCommitment, mintResult.Commitment)
	}

	// Step 4 — Alice confirms the note is hers.
	recomputedCommitment, err := rpcore.Erc20CommitmentV2(
		aliceSpend.PublicKey, issuedSalt, mintAmount, tokenId,
	)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2 recompute: %v", err)
	}
	if recomputedCommitment.Cmp(eventCommitment) != 0 {
		t.Errorf("Alice commitment round-trip failed: got %s, want %s",
			recomputedCommitment, eventCommitment)
	}
	t.Logf("Step 4 — Alice confirmed note: commitment=%s amount=%s salt=%s",
		eventCommitment, mintAmount, issuedSalt)
}

// ── TestRetailErc20_Payment ────────────────────────────────────────────────────

func TestRetailErc20_Payment(t *testing.T) {
	if !chainAvailable() {
		t.Skip("Hardhat node not running on localhost:8545 — skipping on-chain test")
	}
	if !serverAvailable("localhost:8082") {
		t.Skip("gnark server not running on localhost:8082 — skipping on-chain test")
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
	dvpAddr   := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)

	vaultABI := loadOnchainABI(t, "Erc20CoinVault")
	erc20ABI := loadOnchainABI(t, "RaylsERC20")
	dvpABI   := loadOnchainABI(t, "EnygmaDvp")

	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc20 := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	auth := hardhatAuth(t, client)
	alice := auth.From

	gnarkClient := rpcore.NewPaymentClient("")
	merkleDepth := 8
	tokenId     := big.NewInt(0)
	depositAmt  := big.NewInt(40)
	paymentAmt  := big.NewInt(30)
	changeAmt   := big.NewInt(10)

	// ── Mint & approve ERC20 ──────────────────────────────────────────────────
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

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: depositV2 — Alice deposits 40 tokens into the private vault.
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := rpcore.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := rpcore.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}

	ss, capsule, err := rpcore.Encapsulate(aliceView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate (deposit): %v", err)
	}
	aliceSaltB, err := rpcore.DerivePaymentSalt(ss)
	if err != nil {
		t.Fatalf("DerivePaymentSalt (deposit): %v", err)
	}
	aliceEncKey, err := rpcore.DerivePaymentKey(ss)
	if err != nil {
		t.Fatalf("DerivePaymentKey (deposit): %v", err)
	}
	aliceSaltBField := rpcore.SaltBToField(aliceSaltB)

	aliceCommitment, err := rpcore.Erc20CommitmentV2(aliceSpend.PublicKey, aliceSaltBField, depositAmt, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2: %v", err)
	}

	aliceDepositCtxtII, err := rpcore.EncryptPayload(aliceEncKey, tokenId, depositAmt)
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

	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice: %v", err)
	}
	t.Logf("Step 1 — Merkle root: %s", aliceProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: payment — Alice pays 30 to Bob, keeps 10 as change.
	//
	// Inputs:  [Alice's 40 token note, dummy (0)]
	// Outputs: [30 to Bob, 10 change to Alice]
	// ─────────────────────────────────────────────────────────────────────────
	bobSpend, err := rpcore.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := rpcore.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}
	dummySpend, err := rpcore.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummy NewSpendKeyPair: %v", err)
	}

	paymentResult, err := gnarkClient.PaymentProof(
		big.NewInt(0),
		[]*big.Int{depositAmt, big.NewInt(0)},
		[]rpcore.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{aliceSaltBField, big.NewInt(0)},
		[]*big.Int{paymentAmt, changeAmt},
		[]*big.Int{bobSpend.PublicKey, aliceSpend.PublicKey},
		[][]byte{bobView.EncapsKey, aliceView.EncapsKey},
		merkleDepth,
		[]*rpcore.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
	)
	if err != nil {
		t.Fatalf("PaymentProof: %v", err)
	}
	t.Logf("Step 2 — gnark Payment proof generated")
	t.Logf("  Bob's commitment   (output 0): %s", paymentResult.Statement[7])
	t.Logf("  Alice's change cmt (output 1): %s", paymentResult.Statement[8])

	snarkProof := proofStringsToOnchain(t, paymentResult.Proof)
	onchainReceipt := endpoints.ProofReceipt{
		Proof: endpoints.SnarkProof{
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

	vaultId := big.NewInt(0)
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

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Bob scans the Payment event for his 30 token note.
	// ─────────────────────────────────────────────────────────────────────────
	bobEvents := []dvpcore.OnChainErc20Event{{
		Commitment: paymentResult.Statement[7],
		CipherText: paymentResult.CipherText[0],
		EncTxData:  paymentResult.EncTxData[0],
	}}
	bobNotes, err := dvpcore.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc20Notes (Bob): %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	if bobNotes[0].Amount.Cmp(paymentAmt) != 0 {
		t.Errorf("Bob's note amount: got %s, want %s", bobNotes[0].Amount, paymentAmt)
	}
	t.Logf("Step 3 — Bob scanned his note: amount=%s tokenId=%s saltBField=%s",
		bobNotes[0].Amount, bobNotes[0].TokenId, bobNotes[0].SaltBField)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Alice scans the Payment event for her 10 token change note.
	// ─────────────────────────────────────────────────────────────────────────
	aliceChangeEvents := []dvpcore.OnChainErc20Event{{
		Commitment: paymentResult.Statement[8],
		CipherText: paymentResult.CipherText[1],
		EncTxData:  paymentResult.EncTxData[1],
	}}
	aliceChangeNotes, err := dvpcore.ScanForErc20Notes(aliceView.DecapsKey, aliceSpend.PublicKey, aliceChangeEvents)
	if err != nil {
		t.Fatalf("ScanForErc20Notes (Alice change): %v", err)
	}
	if len(aliceChangeNotes) != 1 {
		t.Fatalf("Alice expected 1 change note, got %d", len(aliceChangeNotes))
	}
	if aliceChangeNotes[0].Amount.Cmp(changeAmt) != 0 {
		t.Errorf("Alice's change amount: got %s, want %s", aliceChangeNotes[0].Amount, changeAmt)
	}
	t.Logf("Step 4 — Alice scanned her change note: amount=%s tokenId=%s saltBField=%s",
		aliceChangeNotes[0].Amount, aliceChangeNotes[0].TokenId, aliceChangeNotes[0].SaltBField)

	t.Logf("=== PAYMENT COMPLETE: Alice paid %s tokens to Bob, kept %s tokens change ===",
		paymentAmt, changeAmt)
}

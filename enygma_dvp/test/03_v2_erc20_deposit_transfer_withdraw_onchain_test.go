package tests

// On-chain integration test for the V2 ERC20 non-interactive flow:
//   depositV2 → transferV2 → withdrawV2
//
// Prerequisites (all must be running/completed before this test):
//   1. Hardhat node:        npx hardhat node
//   2. Deploy contracts:    cd scripts &&  go build -o /tmp/deploy deploy.go enygma.go && cd .. && /tmp/deploy
//   3. Export VKs:         cd gnark_circuits && go run ./cmd/export_vk_init/ ../build
//   4. Init contracts:     cd scripts &&  go build -o /tmp/init init.go enygma.go && cd .. && /tmp/init
//   5. Gnark server:       cd gnark_circuits && go run main.go
//
// Run with:
//    go test -run TestV2Erc20OnChain -v -timeout 300s

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"enygma_dvp/src_go/core"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ── Hardhat constants ──────────────────────────────────────────────────────────

const (
	hardhatRPC         = "http://localhost:8545"
	hardhatChainID     = 1337
	// Account[0] from enygmadvp.config.json — the deployer and ERC20 owner
	hardhatPrivKeyHex  = "34d091c661db4c814d65c8ae9277b7055c0dde5a752ce5a3fdfd4ea11a8f7154"
	// Account[1] from enygmadvp.config.json — withdrawal recipient
	hardhatRecipientHex = "0xD2C3b34Abae5664986C8cf0F14d1D434Ac894768"
)

// ── ABI struct types mirroring IEnygmaDvp.sol ──────────────────────────────────

type onchainG1Point struct {
	X *big.Int `abi:"x"`
	Y *big.Int `abi:"y"`
}

type onchainG2Point struct {
	X [2]*big.Int `abi:"x"`
	Y [2]*big.Int `abi:"y"`
}

type onchainSnarkProof struct {
	A onchainG1Point `abi:"a"`
	B onchainG2Point `abi:"b"`
	C onchainG1Point `abi:"c"`
}

type onchainProofReceipt struct {
	Proof           onchainSnarkProof `abi:"proof"`
	Statement       []*big.Int        `abi:"statement"`
	NumberOfInputs  *big.Int          `abi:"numberOfInputs"`
	NumberOfOutputs *big.Int          `abi:"numberOfOutputs"`
}

// ── Test helpers ───────────────────────────────────────────────────────────────

func chainAvailable() bool {
	return serverAvailable("localhost:8545")
}

type onchainReceiptEntry struct {
	ContractAddress string `json:"contractAddress"`
}

func loadOnchainReceipts(t *testing.T) map[string]onchainReceiptEntry {
	t.Helper()
	data, err := os.ReadFile("../build/receipts.json")
	if err != nil {
		t.Fatalf("read build/receipts.json: %v — run deploy+init first", err)
	}
	var r map[string]onchainReceiptEntry
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("parse receipts.json: %v", err)
	}
	return r
}

func loadOnchainABI(t *testing.T, artifactRelPath string) abi.ABI {
	t.Helper()
	data, err := os.ReadFile("../artifacts/contracts/" + artifactRelPath)
	if err != nil {
		t.Fatalf("read artifact %s: %v", artifactRelPath, err)
	}
	var artifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("parse artifact JSON: %v", err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		t.Fatalf("parse ABI: %v", err)
	}
	return parsed
}

func hardhatAuth(t *testing.T, client *ethclient.Client) *bind.TransactOpts {
	t.Helper()
	privKey, err := crypto.HexToECDSA(hardhatPrivKeyHex)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privKey, big.NewInt(hardhatChainID))
	if err != nil {
		t.Fatalf("NewKeyedTransactorWithChainID: %v", err)
	}
	auth.GasLimit = 6_000_000
	return auth
}

// proofStringsToOnchain converts the 8-element decimal proof strings from the
// gnark server into an onchainSnarkProof ready for ABI encoding.
//
// Gnark handler output order (proofRemix):
//
//	[Ax, Ay, BX_A1(imag), BX_A0(real), BY_A1(imag), BY_A0(real), Cx, Cy]
//
// EIP-197 / IEnygmaDvp G2Point convention: x[0]=imaginary, x[1]=real.
func proofStringsToOnchain(t *testing.T, proof []string) onchainSnarkProof {
	t.Helper()
	if len(proof) != 8 {
		t.Fatalf("expected 8 proof elements, got %d", len(proof))
	}
	vals := make([]*big.Int, 8)
	for i, s := range proof {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("invalid proof element %d: %q", i, s)
		}
		vals[i] = n
	}
	return onchainSnarkProof{
		A: onchainG1Point{X: vals[0], Y: vals[1]},
		B: onchainG2Point{
			X: [2]*big.Int{vals[2], vals[3]}, // [imag (A1), real (A0)]
			Y: [2]*big.Int{vals[4], vals[5]},
		},
		C: onchainG1Point{X: vals[6], Y: vals[7]},
	}
}

// contractStatement converts the interleaved ProofResult.Statement produced by
// the Go prover into the non-interleaved layout expected by the Solidity vault.
// Delegates to core.ProofResult.ContractStatement().
func buildReceipt(result *core.ProofResult) onchainProofReceipt {
	stmt := result.ContractStatement()
	// Convert []*big.Int to []*big.Int (no-op type assertion)
	return onchainProofReceipt{
		Statement:       stmt,
		NumberOfInputs:  big.NewInt(int64(result.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(result.NumberOfOutputs)),
	}
}

// ── Main on-chain test ─────────────────────────────────────────────────────────

// TestV2Erc20OnChain_DepositTransferWithdraw exercises the complete V2 ERC20
// non-interactive flow against a live Hardhat node + gnark proof server:
//
//	Step 1 — depositV2:   Alice deposits 50 tokens; commitment inserted on-chain.
//	Step 2 — transferV2:  Alice transfers to Bob via JoinSplit proof.
//	Step 3 — withdrawV2:  Bob withdraws to a recipient address; ERC20 balance verified.
func TestV2Erc20OnChain_DepositTransferWithdraw(t *testing.T) {
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
	vaultAddr   := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc20Addr   := common.HexToAddress(receipts["ERC20"].ContractAddress)
	recipientAddr := common.HexToAddress(hardhatRecipientHex)

	// ── Load ABIs ─────────────────────────────────────────────────────────────
	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json")
	erc20ABI := loadOnchainABI(t, "erc20/contracts/RaylsERC20.sol/RaylsERC20.json")

	// ── Create bound contracts ────────────────────────────────────────────────
	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc20 := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)

	// ── Create signer (Hardhat account[0] = deployer = ERC20 owner) ──────────
	auth := hardhatAuth(t, client)
	alice := auth.From // account[0] address

	// ── Test parameters ───────────────────────────────────────────────────────
	gnarkClient  := core.NewGnarkClient("http://localhost:8081")
	merkleDepth  := 8
	tokenId      := big.NewInt(0)  // ERC20: tokenId=0
	depositAmount := big.NewInt(50)

	// ── Mint ERC20 tokens to Alice (account[0] = ERC20 DEFAULT_OWNER_ROLE) ──
	mintTx, err := erc20.Transact(auth, "mint", alice, new(big.Int).Mul(depositAmount, big.NewInt(10)))
	if err != nil {
		t.Fatalf("ERC20.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted %s tokens to Alice (%s)", new(big.Int).Mul(depositAmount, big.NewInt(10)), alice.Hex())

	// ── Approve vault to spend Alice's tokens ────────────────────────────────
	approveTx, err := erc20.Transact(auth, "approve", vaultAddr, depositAmount)
	if err != nil {
		t.Fatalf("ERC20.approve: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait approve: %v", err)
	}
	t.Logf("Alice approved vault %s for %s tokens", vaultAddr.Hex(), depositAmount)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: depositV2 — Alice deposits using ML-KEM non-interactive flow
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Alice NewViewKeyPair: %v", err)
	}

	// Alice encapsulates with her own view key (depositing to herself)
	saltB, capsule, err := core.Encapsulate(aliceView.EncapsKey)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}
	saltBField := core.SaltBToField(saltB)

	// V2 commitment: Poseidon(pk_spend, saltB_field, amount, tokenId)
	aliceCommitment, err := core.Erc20CommitmentV2(aliceSpend.PublicKey, saltBField, depositAmount, tokenId)
	if err != nil {
		t.Fatalf("Erc20CommitmentV2: %v", err)
	}

	// Encrypt (tokenId, amount) so Alice can scan her own deposit
	encTxData, err := core.EncryptPayload(saltB, tokenId, depositAmount)
	if err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	depositParams := []*big.Int{depositAmount, aliceCommitment}
	depositTx, err := vault.Transact(auth, "depositV2", depositParams, capsule, encTxData)
	if err != nil {
		t.Fatalf("vault.depositV2: %v", err)
	}
	depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
	if err != nil {
		t.Fatalf("wait depositV2: %v", err)
	}
	t.Logf("Step 1 — depositV2 mined (block %d, gas %d)", depositReceipt.BlockNumber, depositReceipt.GasUsed)

	// Verify Commitment event was emitted
	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	foundCommitmentEvent := false
	for _, log := range depositReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundCommitmentEvent = true
			t.Logf("  Commitment event: vaultId=%s commitment=%s",
				log.Topics[1].Big(), log.Topics[2].Big())
		}
	}
	if !foundCommitmentEvent {
		t.Errorf("Commitment event not found in depositV2 receipt")
	}

	// Verify EncryptedNote event was emitted
	encryptedNoteSig := crypto.Keccak256Hash([]byte("EncryptedNote(uint256,uint256,bytes,bytes)"))
	foundEncryptedNote := false
	for _, log := range depositReceipt.Logs {
		if log.Topics[0] == encryptedNoteSig {
			foundEncryptedNote = true
			t.Logf("  EncryptedNote event emitted for commitment %s", log.Topics[2].Big())
		}
	}
	if !foundEncryptedNote {
		t.Errorf("EncryptedNote event not found in depositV2 receipt")
	}

	// Build Merkle tree from all on-chain vault commitment events (matches on-chain state).
	mt := loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)
	aliceProof, err := mt.GenerateProof(aliceCommitment)
	if err != nil {
		t.Fatalf("GenerateProof for Alice: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s  tree root: %s", aliceCommitment, aliceProof.Root)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: transferV2 — Alice JoinSplits to Bob (2-in / 2-out)
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
		t.Fatalf("dummy NewSpendKeyPair: %v", err)
	}
	dummyView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("dummy NewViewKeyPair: %v", err)
	}

	joinSplitResult, err := gnarkClient.Erc20JoinSplitProof(
		big.NewInt(1),
		[]*big.Int{depositAmount, big.NewInt(0)}, // values in
		[]core.KeyPair{
			{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
			{PrivateKey: dummySpend.PrivateKey, PublicKey: dummySpend.PublicKey},
		},
		[]*big.Int{saltBField, big.NewInt(0)}, // Alice's salt from Step 1; dummy=0
		[]*big.Int{depositAmount, big.NewInt(0)}, // values out
		[]*big.Int{bobSpend.PublicKey, dummySpend.PublicKey},
		[][]byte{bobView.EncapsKey, dummyView.EncapsKey},
		merkleDepth,
		[]*core.MerkleProof{aliceProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)}, // tree numbers
		tokenId,
		false, // 2-in / 2-out circuit
	)
	if err != nil {
		t.Fatalf("Erc20JoinSplitProof: %v", err)
	}
	if len(joinSplitResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof from gnark server, got %d", len(joinSplitResult.Proof))
	}

	// Build the ProofReceipt for on-chain submission
	snarkProof := proofStringsToOnchain(t, joinSplitResult.Proof)
	receipt := buildReceipt(joinSplitResult)
	receipt.Proof = snarkProof

	transferTx, err := vault.Transact(auth, "transferV2",
		receipt,
		joinSplitResult.CipherText,
		joinSplitResult.EncTxData,
	)
	if err != nil {
		t.Fatalf("vault.transferV2: %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transferV2: %v", err)
	}
	t.Logf("Step 2 — transferV2 mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	// Verify EncryptedNote events for Bob's output commitment
	bobCommitmentOnChain := joinSplitResult.Statement[7] // interleaved layout index 7 = first output
	t.Logf("  Bob's commitment (from statement): %s", bobCommitmentOnChain)

	foundBobNote := false
	for _, log := range transferReceipt.Logs {
		if log.Topics[0] == encryptedNoteSig {
			foundBobNote = true
			t.Logf("  EncryptedNote event: commitment=%s", log.Topics[2].Big())
		}
	}
	if !foundBobNote {
		t.Errorf("EncryptedNote event not found in transferV2 receipt")
	}

	// Reload the tree from on-chain state after transfer (two new leaves were inserted).
	mt = loadVaultMerkleTree(t, client, vaultAddr, merkleDepth)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: Bob scans events and verifies his note
	// ─────────────────────────────────────────────────────────────────────────
	events := []core.OnChainErc20Event{
		{
			Commitment:   joinSplitResult.Statement[7],
			CipherText:  joinSplitResult.CipherText[0],
			EncTxData: joinSplitResult.EncTxData[0],
		},
		{
			Commitment:   joinSplitResult.Statement[8],
			CipherText:  joinSplitResult.CipherText[1],
			EncTxData: joinSplitResult.EncTxData[1],
		},
	}
	ownedNotes, err := core.ScanForErc20Notes(bobView.DecapsKey, bobSpend.PublicKey, events)
	if err != nil {
		t.Fatalf("ScanForErc20Notes: %v", err)
	}
	if len(ownedNotes) != 1 {
		t.Fatalf("Bob expected 1 owned note, got %d", len(ownedNotes))
	}
	note := ownedNotes[0]
	t.Logf("Step 3 — Bob scanned note: tokenId=%s amount=%s saltBField=%s",
		note.TokenId, note.Amount, note.SaltBField)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: withdrawV2 — Bob withdraws to recipient address
	// ─────────────────────────────────────────────────────────────────────────
	bobProof, err := mt.GenerateProof(note.Commitment)
	if err != nil {
		t.Fatalf("GenerateProof for Bob: %v", err)
	}

	dummyWithdrawSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("dummyWithdrawSpend: %v", err)
	}

	// Balance before withdrawal
	var balanceBefore []interface{}
	if err := erc20.Call(nil, &balanceBefore, "balanceOf", recipientAddr); err != nil {
		t.Fatalf("balanceOf before: %v", err)
	}
	balBeforeBig := balanceBefore[0].(*big.Int)
	t.Logf("Step 4 — recipient balance before: %s", balBeforeBig)

	// recipient as uint160 big.Int (for the circuit)
	recipientBig := new(big.Int).SetBytes(recipientAddr.Bytes())

	withdrawResult, err := gnarkClient.Erc20WithdrawProof(
		big.NewInt(1),
		[]*big.Int{note.Amount, big.NewInt(0)},
		[]core.KeyPair{
			{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
			{PrivateKey: dummyWithdrawSpend.PrivateKey, PublicKey: dummyWithdrawSpend.PublicKey},
		},
		[]*big.Int{note.SaltBField, big.NewInt(0)},
		note.Amount,
		recipientBig,
		dummyWithdrawSpend.PublicKey,
		merkleDepth,
		[]*core.MerkleProof{bobProof, makeDummyProof(merkleDepth)},
		[]*big.Int{big.NewInt(0), big.NewInt(0)},
		tokenId,
		false,
	)
	if err != nil {
		t.Fatalf("Erc20WithdrawProof: %v", err)
	}
	if len(withdrawResult.Proof) != 8 {
		t.Fatalf("expected 8-element withdraw proof, got %d", len(withdrawResult.Proof))
	}

	withdrawSnarkProof := proofStringsToOnchain(t, withdrawResult.Proof)
	withdrawReceipt := buildReceipt(withdrawResult)
	withdrawReceipt.Proof = withdrawSnarkProof

	withdrawParams := []*big.Int{note.Amount, tokenId}
	withdrawTx, err := vault.Transact(auth, "withdrawV2",
		withdrawParams,
		recipientAddr,
		withdrawReceipt,
	)
	if err != nil {
		t.Fatalf("vault.withdrawV2: %v", err)
	}
	withdrawTxReceipt, err := bind.WaitMined(ctx, client, withdrawTx)
	if err != nil {
		t.Fatalf("wait withdrawV2: %v", err)
	}
	t.Logf("Step 4 — withdrawV2 mined (block %d, gas %d)",
		withdrawTxReceipt.BlockNumber, withdrawTxReceipt.GasUsed)

	// Verify Nullifier event was emitted
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundNullifier := false
	for _, log := range withdrawTxReceipt.Logs {
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: nullifier=%s", log.Topics[3].Big())
		}
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in withdrawV2 receipt")
	}

	// Verify recipient's ERC20 balance increased by depositAmount
	var balanceAfter []interface{}
	if err := erc20.Call(nil, &balanceAfter, "balanceOf", recipientAddr); err != nil {
		t.Fatalf("balanceOf after: %v", err)
	}
	balAfterBig := balanceAfter[0].(*big.Int)
	t.Logf("Step 4 — recipient balance after:  %s", balAfterBig)

	gained := new(big.Int).Sub(balAfterBig, balBeforeBig)
	if gained.Cmp(depositAmount) != 0 {
		t.Errorf("recipient balance gain: got %s, want %s", gained, depositAmount)
	}
	t.Logf("Step 4 — recipient received %s tokens. On-chain flow verified!", gained)
}

// Ensure fmt is used (satisfies import if needed)
var _ = fmt.Sprintf

package tests

// On-chain integration test for the V2 ERC721 ownership proof flow:
//   deposit → transfer (Alice→Bob ownership proof)
//
// Prerequisites (all must be running/completed before this test):
//   1. Hardhat node:        npx hardhat node
//   2. Deploy contracts:    cd scripts &&  go build -o /tmp/deploy deploy.go enygma.go && cd .. && /tmp/deploy
//   3. Export VKs:         cd gnark_circuits && go run ./cmd/export_vk_init/ ../build
//   4. Init contracts:     cd scripts &&  go build -o /tmp/init init.go enygma.go && cd .. && /tmp/init
//   5. Gnark server:       cd gnark_circuits && go run main.go
//
// Run with:
//    go test -run TestV2Erc721OnChain -v -timeout 300s


  
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

// TestV2Erc721OnChain_DepositTransfer exercises the V2 ERC721 flow against a live
// Hardhat node + gnark proof server:
//
//	Step 1 — depositERC721: Alice deposits tokenId=42; commitment inserted on-chain.
//	Step 2 — transferERC721: Alice proves ownership and transfers to Bob.
func TestV2Erc721OnChain_DepositTransfer(t *testing.T) {
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
	vaultAddr := common.HexToAddress(receipts["Erc721CoinVault"].ContractAddress)
	erc721Addr := common.HexToAddress(receipts["ERC721"].ContractAddress)

	// ── Load ABIs ─────────────────────────────────────────────────────────────
	vaultABI := loadOnchainABI(t, "core/contracts/vaults/Erc721CoinVault.sol/Erc721CoinVault.json")
	erc721ABI := loadOnchainABI(t, "erc721/contracts/RaylsERC721.sol/RaylsERC721.json")

	// ── Create bound contracts ────────────────────────────────────────────────
	vault := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	erc721 := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)

	// ── Create signer (Hardhat account[0] = deployer = ERC721 owner) ─────────
	auth := hardhatAuth(t, client)
	alice := auth.From

	// ── Test parameters ───────────────────────────────────────────────────────
	gnarkClient := core.NewGnarkClient("http://localhost:8081")
	merkleDepth := 8
	// Use a random tokenId in [1000, 1999] to avoid collision with previously minted tokens.
	tokenIdRand, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		t.Fatalf("rand.Int: %v", err)
	}
	tokenId := new(big.Int).Add(tokenIdRand, big.NewInt(1000))
	contractAddress := big.NewInt(0) // used in circuit for contractAddress witness

	// ── Mint ERC721 token to Alice ────────────────────────────────────────────
	mintTx, err := erc721.Transact(auth, "mint", alice, tokenId)
	if err != nil {
		t.Fatalf("ERC721.mint: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, mintTx); err != nil {
		t.Fatalf("wait mint: %v", err)
	}
	t.Logf("Minted ERC721 tokenId=%s to Alice (%s)", tokenId, alice.Hex())

	// ── Approve vault to transfer Alice's token ───────────────────────────────
	approveTx, err := erc721.Transact(auth, "approve", vaultAddr, tokenId)
	if err != nil {
		t.Fatalf("ERC721.approve: %v", err)
	}
	if _, err := bind.WaitMined(ctx, client, approveTx); err != nil {
		t.Fatalf("wait approve: %v", err)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: depositERC721 — Alice deposits her NFT
	// ─────────────────────────────────────────────────────────────────────────
	aliceSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Alice NewSpendKeyPair: %v", err)
	}
	aliceSalt, err := core.RandomInField()
	if err != nil {
		t.Fatalf("Alice RandomInField (salt): %v", err)
	}

	// V2 commitment: Poseidon(pk_spend, salt, 1, tokenId)
	aliceCommitment, err := core.Erc721Commitment(tokenId, aliceSpend.PublicKey, aliceSalt)
	if err != nil {
		t.Fatalf("Erc721Commitment: %v", err)
	}
	t.Logf("Step 1 — Alice commitment: %s", aliceCommitment)

	depositParams := []*big.Int{tokenId, aliceCommitment}
	depositTx, err := vault.Transact(auth, "deposit", depositParams)
	if err != nil {
		t.Fatalf("vault.deposit (ERC721): %v", err)
	}
	depositReceipt, err := bind.WaitMined(ctx, client, depositTx)
	if err != nil {
		t.Fatalf("wait deposit: %v", err)
	}
	t.Logf("Step 1 — deposit mined (block %d, gas %d)", depositReceipt.BlockNumber, depositReceipt.GasUsed)

	// Verify Commitment event
	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	foundCommitment := false
	for _, log := range depositReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundCommitment = true
			t.Logf("  Commitment event: vaultId=%s commitment=%s",
				log.Topics[1].Big(), log.Topics[2].Big())
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

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: transferERC721 — Alice proves ownership and transfers to Bob
	// ─────────────────────────────────────────────────────────────────────────
	bobSpend, err := core.NewSpendKeyPair()
	if err != nil {
		t.Fatalf("Bob NewSpendKeyPair: %v", err)
	}
	bobView, err := core.NewViewKeyPair()
	if err != nil {
		t.Fatalf("Bob NewViewKeyPair: %v", err)
	}

	ownershipResult, err := gnarkClient.Erc721OwnershipProof(
		big.NewInt(1),
		tokenId,
		core.KeyPair{PrivateKey: aliceSpend.PrivateKey, PublicKey: aliceSpend.PublicKey},
		aliceSalt,
		core.KeyPair{PrivateKey: bobSpend.PrivateKey, PublicKey: bobSpend.PublicKey},
		bobView.EncapsKey,
		merkleDepth,
		aliceProof,
		big.NewInt(0), // treeNumber
		contractAddress,
	)
	if err != nil {
		t.Fatalf("Erc721OwnershipProof: %v", err)
	}
	if len(ownershipResult.Proof) != 8 {
		t.Fatalf("expected 8-element proof, got %d", len(ownershipResult.Proof))
	}
	t.Logf("Step 2 — gnark proof generated (%d elements)", len(ownershipResult.Proof))

	// Build on-chain receipt
	// ERC721: statement = [msg, treeNum, root, null, cmtOut] (5 elements, 1-in/1-out)
	snarkProof := proofStringsToOnchain(t, ownershipResult.Proof)
	receipt := buildReceipt(ownershipResult)
	receipt.Proof = snarkProof

	t.Logf("Step 2 — statement: %v", ownershipResult.ContractStatement())

	transferTx, err := vault.Transact(auth, "transfer", receipt)
	if err != nil {
		t.Fatalf("vault.transfer (ERC721): %v", err)
	}
	transferReceipt, err := bind.WaitMined(ctx, client, transferTx)
	if err != nil {
		t.Fatalf("wait transfer: %v", err)
	}
	t.Logf("Step 2 — transfer mined (block %d, gas %d)", transferReceipt.BlockNumber, transferReceipt.GasUsed)

	// Verify Commitment (Bob's new note) and Nullifier (Alice's old note spent) events
	nullifierSig := crypto.Keccak256Hash([]byte("Nullifier(uint256,uint256,uint256)"))
	foundBobCmt := false
	foundNullifier := false
	for _, log := range transferReceipt.Logs {
		if log.Topics[0] == commitmentSig {
			foundBobCmt = true
			t.Logf("  Bob's Commitment event: commitment=%s", log.Topics[2].Big())
		}
		if log.Topics[0] == nullifierSig {
			foundNullifier = true
			t.Logf("  Nullifier event: nullifier=%s", log.Topics[3].Big())
		}
	}
	if !foundBobCmt {
		t.Errorf("Bob's Commitment event not found in transfer receipt")
	}
	if !foundNullifier {
		t.Errorf("Nullifier event not found in transfer receipt")
	}

	bobCommitment := ownershipResult.Statement[4]
	t.Logf("Step 2 — Bob's commitment (from statement): %s", bobCommitment)

	// Bob scans for his note
	bobEvents := []core.OnChainErc721Event{{
		Commitment:   bobCommitment,
		CipherText:  ownershipResult.CipherText[0],
		EncTxData: ownershipResult.EncTxData[0],
	}}
	bobNotes, err := core.ScanForErc721Notes(bobView.DecapsKey, bobSpend.PublicKey, bobEvents)
	if err != nil {
		t.Fatalf("ScanForErc721Notes: %v", err)
	}
	if len(bobNotes) != 1 {
		t.Fatalf("Bob expected 1 note, got %d", len(bobNotes))
	}
	t.Logf("Step 2 — Bob scanned his note (tokenId=%s, salt=%s)", bobNotes[0].TokenId, bobNotes[0].SaltBField)

	t.Logf("=== ERC721 ON-CHAIN TRANSFER COMPLETE ===")
}

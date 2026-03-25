package main

/*
Port of scripts/swapDemo.js

End-to-end swap demonstration: Alice owns an ERC-721 NFT and Bob holds ERC-20
tokens. The demo swaps Alice's NFT for Bob's ERC-20 payment via the EnygmaDvp
contract:
  1. Admin mints NFT for Alice and ERC-20 for Bob.
  2. Alice deposits the NFT into Erc721CoinVault.
  3. Bob deposits ERC-20 twice into Erc20CoinVault.
  4. Alice generates an OwnershipErc721 proof (stMessage = paymentCommitment).
  5. Bob generates a JoinSplitErc20 proof (stMessage = nftCommitment).
  6. A relayer submits both proofs via EnygmaDvp.swap().
  7. Alice withdraws her ERC-20 payment from Erc20CoinVault.
  8. Bob withdraws Alice's NFT from Erc721CoinVault.
  9. Ownership is verified on-chain and Merkle trees are saved to disk.

JS mapping:
  - hre.ethers.getSigners()       → accounts from enygmadvp.config.json
  - MerkleTree(depth, name)       → core.NewMerkleTreeWithPath(depth, name, "")
  - jsUtils.newKeyPair()          → core.NewKeyPair()
  - jsUtils.getCommitment(u, pk)  → core.GetCommitment(u, pk)
  - jsUtils.erc721UniqueId(a, id) → core.Erc721UniqueId(addrBig, id)
  - jsUtils.erc20UniqueId(a, amt) → core.Erc20UniqueId(addrBig, amt)
  - adminActions.*                → endpoints.MintErc721 / endpoints.MintErc20
  - userActions.*                 → endpoints.Deposit* / endpoints.Withdraw* / endpoints.Generate*
  - relayerActions.swap(...)      → endpoints.Swap(...)

Notes:
  - The vault ABI may not include the "Commitment" event (see user.go). Commitment
    values are therefore computed directly from the deposit inputs rather than from
    transaction logs.
  - receipts["ZkDvp"] in the JS (old name) → receipts["EnygmaDvp"] in Go.
  - zkdvp.config.json in the JS (old name) → enygmadvp.config.json in Go.

Run with:
  go run scripts_go/swapDemo.go

Note: this file defines its own func main() and is intended to be run in
isolation. It cannot be built together with deploy.go, init.go, or
initCircuits.go, which also define func main().
*/

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"enygma_dvp/src_go/core"
	"enygma_dvp/src_go/core/endpoints"
)

// swapProjectRoot is the project root, set in init().
var swapProjectRoot string

func init() {
	execPath, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get working directory:", err)
	}
	// Support being run from either the project root or the scripts_go/ subdirectory.
	if filepath.Base(execPath) == "scripts_go" {
		swapProjectRoot = filepath.Dir(execPath)
	} else {
		swapProjectRoot = execPath
	}
}

func main() {
	if err := runSwapDemo(); err != nil {
		log.Fatal("Swap demo failed:", err)
	}
}

// --- Config / receipt types (local to this file) ---

type swapConfig struct {
	Network struct {
		Host    string `json:"host"`
		Port    string `json:"port"`
		ChainID string `json:"chain-id"`
		Accounts []struct {
			Address string `json:"address"`
			Private string `json:"private"`
		} `json:"accounts"`
	} `json:"network"`
	Circom struct {
		MetaParameters struct {
			TreeDepth int `json:"tree-depth"`
		} `json:"meta-parameters"`
	} `json:"circom"`
}

type swapReceiptData struct {
	ContractAddress string `json:"contractAddress"`
}

type swapReceipts map[string]swapReceiptData

// --- Helpers ---

// loadSwapConfig reads enygmadvp.config.json.
func loadSwapConfig() (*swapConfig, error) {
	data, err := os.ReadFile(filepath.Join(swapProjectRoot, "enygmadvp.config.json"))
	if err != nil {
		return nil, err
	}
	var cfg swapConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadSwapReceipts reads build/receipts.json.
func loadSwapReceipts() (swapReceipts, error) {
	data, err := os.ReadFile(filepath.Join(swapProjectRoot, "build", "receipts.json"))
	if err != nil {
		return nil, err
	}
	var r swapReceipts
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// loadSwapContractABI reads a Hardhat artifact from artifacts/contracts/<path>.json.
func loadSwapContractABI(contractPath string) (abi.ABI, error) {
	artifactPath := filepath.Join(swapProjectRoot, "artifacts", "contracts", contractPath+".json")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return abi.ABI{}, err
	}
	var artifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		return abi.ABI{}, err
	}
	return abi.JSON(strings.NewReader(string(artifact.ABI)))
}

// makeSwapTransactOpts creates a signer from a hex private key and chain ID.
func makeSwapTransactOpts(privateKeyHex string, chainID *big.Int) (*bind.TransactOpts, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	pk, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return bind.NewKeyedTransactorWithChainID(pk, chainID)
}

// makeKeyPair wraps core.NewKeyPair() into a core.KeyPair value.
func makeKeyPair() (core.KeyPair, error) {
	priv, pub, err := core.NewKeyPair()
	if err != nil {
		return core.KeyPair{}, err
	}
	return core.KeyPair{PrivateKey: priv, PublicKey: pub}, nil
}

// addrToBig converts a common.Address to *big.Int for use as a circuit input.
func addrToBig(addr common.Address) *big.Int {
	return new(big.Int).SetBytes(addr.Bytes())
}

// toProofReceipt converts a *core.ProofResult to an endpoints.ProofReceipt
// suitable for on-chain submission.
// NOTE: The SnarkProof (a, b, c) is left zero-valued because ProofResult.Proof
// is not currently populated by the gnark server response parser.
func toProofReceipt(r *core.ProofResult) endpoints.ProofReceipt {
	return endpoints.ProofReceipt{
		Statement:       r.Statement,
		NumberOfInputs:  big.NewInt(int64(r.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(r.NumberOfOutputs)),
	}
}

// randomBigInt reads n random bytes and returns them as a *big.Int.
func randomBigInt(nBytes int) (*big.Int, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(buf), nil
}

// --- Main demo ---

// runSwapDemo corresponds to demo() in scripts/swapDemo.js.
func runSwapDemo() error {
	// ---- Config & Merkle trees ----

	cfg, err := loadSwapConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	treeDepth := cfg.Circom.MetaParameters.TreeDepth

	// MerkleTree(TREE_DEPTH, "erc20") / ("erc721")
	// Using NewMerkleTreeWithPath so SaveToFile() works.
	erc20Tree, err := core.NewMerkleTreeWithPath(treeDepth, "erc20", "")
	if err != nil {
		return fmt.Errorf("failed to create erc20 tree: %w", err)
	}
	erc721Tree, err := core.NewMerkleTreeWithPath(treeDepth, "erc721", "")
	if err != nil {
		return fmt.Errorf("failed to create erc721 tree: %w", err)
	}

	// ---- Demo parameters ----

	// nft_id = randomBytes(3) % SNARK_SCALAR_FIELD
	rawNFT, err := randomBigInt(3)
	if err != nil {
		return fmt.Errorf("failed to generate nft_id: %w", err)
	}
	nftID := new(big.Int).Mod(rawNFT, core.SNARK_SCALAR_FIELD)

	// paymentAmount = randomBytes(2) * 2; depositAmount = paymentAmount / 2
	rawPmt, err := randomBigInt(2)
	if err != nil {
		return fmt.Errorf("failed to generate paymentAmount: %w", err)
	}
	paymentAmount := new(big.Int).Mul(rawPmt, big.NewInt(2))
	depositAmount := new(big.Int).Div(paymentAmount, big.NewInt(2))

	// ---- Ethereum accounts ----

	// accounts[0]=owner, [1]=alice, [2]=bob
	chainID, ok := new(big.Int).SetString(cfg.Network.ChainID, 10)
	if !ok {
		return fmt.Errorf("invalid chain-id: %s", cfg.Network.ChainID)
	}
	ownerAuth, err := makeSwapTransactOpts(cfg.Network.Accounts[0].Private, chainID)
	if err != nil {
		return fmt.Errorf("owner auth: %w", err)
	}
	aliceAuth, err := makeSwapTransactOpts(cfg.Network.Accounts[1].Private, chainID)
	if err != nil {
		return fmt.Errorf("alice auth: %w", err)
	}
	bobAuth, err := makeSwapTransactOpts(cfg.Network.Accounts[2].Private, chainID)
	if err != nil {
		return fmt.Errorf("bob auth: %w", err)
	}
	aliceAddr := common.HexToAddress(cfg.Network.Accounts[1].Address)
	bobAddr := common.HexToAddress(cfg.Network.Accounts[2].Address)

	fmt.Printf("owner: %s\n", cfg.Network.Accounts[0].Address)
	fmt.Printf("alice: %s\n", cfg.Network.Accounts[1].Address)
	fmt.Printf("bob:   %s\n", cfg.Network.Accounts[2].Address)

	// ---- Connect to node ----

	rpcURL := fmt.Sprintf("http://%s:%s", cfg.Network.Host, cfg.Network.Port)
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to node: %w", err)
	}
	defer client.Close()

	// ---- Load contract addresses ----

	receipts, err := loadSwapReceipts()
	if err != nil {
		return fmt.Errorf("failed to load receipts: %w", err)
	}
	erc721VaultAddr := common.HexToAddress(receipts["Erc721CoinVault"].ContractAddress)
	erc721Addr := common.HexToAddress(receipts["ERC721"].ContractAddress)
	erc20VaultAddr := common.HexToAddress(receipts["Erc20CoinVault"].ContractAddress)
	erc20Addr := common.HexToAddress(receipts["ERC20"].ContractAddress)
	dvpAddr := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)

	// ---- Load ABIs ----

	erc721ABI, err := loadSwapContractABI("erc721/contracts/RaylsERC721.sol/RaylsERC721")
	if err != nil {
		return fmt.Errorf("failed to load ERC721 ABI: %w", err)
	}
	erc721VaultABI, err := loadSwapContractABI("core/contracts/vaults/Erc721CoinVault.sol/Erc721CoinVault")
	if err != nil {
		return fmt.Errorf("failed to load Erc721CoinVault ABI: %w", err)
	}
	erc20ABI, err := loadSwapContractABI("erc20/contracts/RaylsERC20.sol/RaylsERC20")
	if err != nil {
		return fmt.Errorf("failed to load ERC20 ABI: %w", err)
	}
	erc20VaultABI, err := loadSwapContractABI("core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault")
	if err != nil {
		return fmt.Errorf("failed to load Erc20CoinVault ABI: %w", err)
	}
	dvpABI, err := loadSwapContractABI("core/contracts/EnygmaDvp.sol/EnygmaDvp")
	if err != nil {
		return fmt.Errorf("failed to load EnygmaDvp ABI: %w", err)
	}

	// ---- Gnark prover client ----

	gnarkClient := core.NewGnarkClient("http://localhost:8081")

	// ---- Step 1: Admin mints ERC-721 for Alice ----

	fmt.Println("Minting ERC721")
	if _, err := endpoints.MintErc721(client, ownerAuth, erc721ABI, erc721Addr,
		aliceAddr, nftID); err != nil {
		return fmt.Errorf("MintErc721: %w", err)
	}

	// ---- Step 2: Alice deposits NFT ----

	nftKeyDeposit, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("nftKeyDeposit: %w", err)
	}

	fmt.Println("Depositing ERC721")
	// We call DepositErc721 for the on-chain side-effect (approve + deposit).
	// The returned commitments may be nil if the vault ABI lacks the "Commitment"
	// event, so we compute the commitment value directly from the deposit inputs.
	if _, err := endpoints.DepositErc721(client, aliceAuth,
		erc721VaultABI, erc721VaultAddr,
		erc721ABI, erc721Addr,
		nftID, nftKeyDeposit.PublicKey); err != nil {
		return fmt.Errorf("DepositErc721: %w", err)
	}
	erc721AddrBig := addrToBig(erc721Addr)
	nftUID, err := core.Erc721UniqueId(erc721AddrBig, nftID)
	if err != nil {
		return fmt.Errorf("Erc721UniqueId: %w", err)
	}
	erc721Commitment, err := core.GetCommitment(nftUID, nftKeyDeposit.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (NFT deposit): %w", err)
	}

	erc721Tree.InsertLeaf(erc721Commitment)
	aliceNFTProof, err := erc721Tree.GenerateProof(erc721Commitment)
	if err != nil {
		return fmt.Errorf("GenerateProof (alice NFT): %w", err)
	}
	aliceNFTTreeNum := big.NewInt(int64(erc721Tree.LastTreeNumber()))

	// ---- Step 3: Bob deposits ERC-20 twice ----

	fundKey0, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("fundKey0: %w", err)
	}
	fundKey1, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("fundKey1: %w", err)
	}

	fmt.Println("Minting ERC20")
	totalMint := new(big.Int).Mul(depositAmount, big.NewInt(2))
	if _, err := endpoints.MintErc20(client, ownerAuth, erc20ABI, erc20Addr,
		bobAddr, totalMint); err != nil {
		return fmt.Errorf("MintErc20: %w", err)
	}

	erc20AddrBig := addrToBig(erc20Addr)

	fmt.Println("Depositing first ERC20 coin")
	if _, err := endpoints.DepositErc20(client, bobAuth,
		erc20VaultABI, erc20VaultAddr,
		erc20ABI, erc20Addr,
		depositAmount, fundKey0.PublicKey); err != nil {
		return fmt.Errorf("DepositErc20 (1st): %w", err)
	}
	erc20UID0, err := core.Erc20UniqueId(erc20AddrBig, depositAmount)
	if err != nil {
		return fmt.Errorf("Erc20UniqueId (1st): %w", err)
	}
	erc20Commitment1, err := core.GetCommitment(erc20UID0, fundKey0.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (ERC20 1st): %w", err)
	}

	erc20Tree.InsertLeaf(erc20Commitment1)
	bobCoin1Proof, err := erc20Tree.GenerateProof(erc20Commitment1)
	if err != nil {
		return fmt.Errorf("GenerateProof (bob coin 1): %w", err)
	}
	bobCoin1TreeNum := big.NewInt(int64(erc20Tree.LastTreeNumber()))

	fmt.Println("Depositing second ERC20 coin")
	if _, err := endpoints.DepositErc20(client, bobAuth,
		erc20VaultABI, erc20VaultAddr,
		erc20ABI, erc20Addr,
		depositAmount, fundKey1.PublicKey); err != nil {
		return fmt.Errorf("DepositErc20 (2nd): %w", err)
	}
	// Second deposit has the same amount, so the same uid as the first deposit.
	erc20Commitment2, err := core.GetCommitment(erc20UID0, fundKey1.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (ERC20 2nd): %w", err)
	}

	erc20Tree.InsertLeaf(erc20Commitment2)
	bobCoin2Proof, err := erc20Tree.GenerateProof(erc20Commitment2)
	if err != nil {
		return fmt.Errorf("GenerateProof (bob coin 2): %w", err)
	}
	bobCoin2TreeNum := big.NewInt(int64(erc20Tree.LastTreeNumber()))

	// ---- Step 4: Generate cross-party commitments ----

	fmt.Println("Alice generates NFT commitment for Bob")

	bobNFTKey, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("bobNFTKey: %w", err)
	}
	bobChangeKey, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("bobChangeKey: %w", err)
	}
	alicePaymentKey, err := makeKeyPair()
	if err != nil {
		return fmt.Errorf("alicePaymentKey: %w", err)
	}

	// nftCommitment will be used as stMessage by Bob's JoinSplit proof.
	fmt.Println("Generating NFT commitment for Bob.")
	nftCommitment, err := core.GetCommitment(nftUID, bobNFTKey.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (nftCommitment): %w", err)
	}

	// paymentCommitment will be used as stMessage by Alice's Ownership proof.
	fmt.Println("Generating Payment commitment for Alice.")
	changeAmount := new(big.Int).Sub(totalMint, paymentAmount)
	erc20PaymentUID, err := core.Erc20UniqueId(erc20AddrBig, paymentAmount)
	if err != nil {
		return fmt.Errorf("Erc20UniqueId (payment): %w", err)
	}
	paymentCommitment, err := core.GetCommitment(erc20PaymentUID, alicePaymentKey.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (paymentCommitment): %w", err)
	}

	// ---- Step 5: Generate ZK proofs ----

	fmt.Println("Alice generates SNARK proof of ownership to send her NFT to Bob")
	// GenerateOwnershipProof takes raw nftID (Go computes uniqueId internally).
	ownParams, err := endpoints.GenerateOwnershipProof(
		gnarkClient,
		paymentCommitment,
		nftID,
		nftKeyDeposit,
		bobNFTKey,
		treeDepth,
		aliceNFTProof,
		aliceNFTTreeNum,
		erc721Addr,
	)
	if err != nil {
		return fmt.Errorf("GenerateOwnershipProof: %w", err)
	}

	fmt.Println("Bob generates a tx to send payment to Alice")
	jsParams, err := endpoints.GenerateErc20JoinSplitProof(
		gnarkClient,
		nftCommitment,
		[]*big.Int{depositAmount, depositAmount},
		[]core.KeyPair{fundKey0, fundKey1},
		[]*big.Int{paymentAmount, changeAmount},
		[]core.KeyPair{alicePaymentKey, bobChangeKey},
		treeDepth,
		[]*core.MerkleProof{bobCoin1Proof, bobCoin2Proof},
		[]*big.Int{bobCoin1TreeNum, bobCoin2TreeNum},
		erc20Addr,
		false, // use10_2
	)
	if err != nil {
		return fmt.Errorf("GenerateErc20JoinSplitProof: %w", err)
	}

	fmt.Println(jsParams)
	fmt.Println(ownParams)

	// ---- Step 6: Relayer submits swap ----

	fmt.Println("Swapping")
	jsReceipt := toProofReceipt(jsParams)
	ownReceipt := toProofReceipt(ownParams)

	swapCommitments, err := endpoints.Swap(
		client, ownerAuth, dvpABI, dvpAddr,
		jsReceipt,        // paymentReceipt (Bob's ERC-20 JoinSplit)
		ownReceipt,       // deliveryReceipt (Alice's NFT Ownership)
		big.NewInt(0),    // paymentVaultId  (Erc20CoinVault = vault 0)
		big.NewInt(1),    // deliveryVaultId (Erc721CoinVault = vault 1)
	)
	if err != nil {
		return fmt.Errorf("Swap: %w", err)
	}

	fmt.Println("Swap was successful, updating local merkleTrees")
	fmt.Println(swapCommitments)

	// ---- Step 7: Update Merkle trees ----

	// swapCommitments: [paymentReceipt.Statement[7], paymentReceipt.Statement[8], deliveryReceipt.Statement[4]]
	// [0] = Alice's ERC-20 payment note, [1] = Bob's ERC-20 change note, [2] = Bob's new NFT note
	erc20Tree.InsertLeaves([]*big.Int{swapCommitments[0], swapCommitments[1]})
	alicePaymentProof, err := erc20Tree.GenerateProof(swapCommitments[0])
	if err != nil {
		return fmt.Errorf("GenerateProof (alice payment): %w", err)
	}
	alicePaymentTreeNum := big.NewInt(int64(erc20Tree.LastTreeNumber()))

	erc721Tree.InsertLeaf(swapCommitments[2])
	bobNFTProof, err := erc721Tree.GenerateProof(swapCommitments[2])
	if err != nil {
		return fmt.Errorf("GenerateProof (bob NFT): %w", err)
	}
	bobNFTTreeNum := big.NewInt(int64(erc721Tree.LastTreeNumber()))

	// ---- Step 8: Alice withdraws ERC-20 payment ----

	fmt.Println("Alice withdraws fund")
	_, err = endpoints.WithdrawErc20(
		client, aliceAuth,
		erc20VaultABI, erc20VaultAddr,
		gnarkClient,
		paymentAmount,
		alicePaymentKey,
		aliceAddr,
		erc20Addr,
		treeDepth,
		alicePaymentProof,
		alicePaymentTreeNum,
		false, // use10_2
	)
	if err != nil {
		return fmt.Errorf("WithdrawErc20: %w", err)
	}

	// ---- Step 9: Bob withdraws ERC-721 NFT ----

	fmt.Println("Bob withdraws bought NFT")
	_, err = endpoints.WithdrawErc721(
		client, bobAuth,
		erc721VaultABI, erc721VaultAddr,
		gnarkClient,
		nftID,
		bobNFTKey,
		bobAddr,
		erc721Addr,
		treeDepth,
		bobNFTProof,
		bobNFTTreeNum,
	)
	if err != nil {
		return fmt.Errorf("WithdrawErc721: %w", err)
	}

	// ---- Step 10: Verify ownership ----

	isOwner, err := endpoints.CheckOwnership(client, erc721ABI, erc721Addr, bobAddr, nftID)
	if err != nil {
		return fmt.Errorf("CheckOwnership: %w", err)
	}
	fmt.Printf("nft_id = %s, Bob is owner = %v\n", nftID.String(), isOwner)

	// ---- Step 11: Save Merkle trees ----

	if err := erc20Tree.SaveToFile(); err != nil {
		fmt.Printf("warning: failed to save erc20 tree: %v\n", err)
	}
	if err := erc721Tree.SaveToFile(); err != nil {
		fmt.Printf("warning: failed to save erc721 tree: %v\n", err)
	}

	return nil
}

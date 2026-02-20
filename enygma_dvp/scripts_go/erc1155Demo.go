package main

/*
Port of scripts/erc1155Demo.js

End-to-end ERC-1155 swap demonstration:
  - Alice holds a non-fungible ERC-1155 token (nonFungId).
  - Bob holds fungible ERC-1155 tokens (fungId).
  - The demo swaps Alice's non-fungible token for Bob's fungible payment via EnygmaDvp.

Flow:
  1. Register fungible and non-fungible token types in RaylsERC1155.
  2. Add them to their respective asset groups on EnygmaDvp.
  3. Admin mints non-fungible ERC-1155 for Alice and fungible for Bob.
  4. Alice and Bob each deposit into Erc1155CoinVault.
  5. Alice generates a SingleErc1155 proof (non-fungible ownership, isFungible=false).
  6. Bob generates a JoinSplitErc1155 proof (fungible payment, isFungible=true).
  7. Relayer submits both proofs via EnygmaDvp.swap().
  8. Alice withdraws fungible payment from the vault.
  9. Bob withdraws the non-fungible token from the vault.

JS mapping:
  - MerkleTree(depth, name)         → core.NewMerkleTreeWithPath(depth, name, "")
  - jsUtils.erc1155UniqueId(a,t,x)  → core.Erc1155UniqueId(addrBig, tokenId, x)
  - jsUtils.getCommitment(u, pk)    → core.GetCommitment(u, pk)
  - adminActions.mintErc1155(...)   → endpoints.MintErc1155(...)
  - userActions.depositErc1155(...) → endpoints.DepositErc1155(...)
  - generateSingleErc1155Proof(...) → endpoints.GenerateSingleErc1155Proof(...)
  - generateErc1155JoinSplitProof(.)→ endpoints.GenerateErc1155JoinSplitProof(...)
  - relayerActions.swap(...)        → endpoints.Swap(...)
  - userActions.withdrawERC1155(...)→ endpoints.WithdrawERC1155(...)

Notes:
  - dvpConf["tree-depth"] in the JS is likely a bug; Go uses the correct config
    path cfg.Circom.MetaParameters.TreeDepth.
  - The "Commitment" event may be absent from vault ABI. Commitments are computed
    directly from deposit inputs (Erc1155UniqueId + GetCommitment).
  - The TokenAddedToGroup event is not parsed; uidFung / uidNonFung are computed
    off-chain as noted in the JS comment.
  - registerNewToken and addTokenToGroup are called directly on the contracts via
    bind.NewBoundContract (no helper wrapper exists for these methods).

Run with:
  go run scripts_go/erc1155Demo.go

Note: this file defines its own func main() and is intended to be run in
isolation. It cannot be built together with deploy.go, init.go, initCircuits.go,
or swapDemo.go, which also define func main().
*/

import (
	"context"
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

const vaultIDErc1155 = 2

// erc1155ProjectRoot is set in init().
var erc1155ProjectRoot string

func init() {
	execPath, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get working directory:", err)
	}
	if filepath.Base(execPath) == "scripts_go" {
		erc1155ProjectRoot = filepath.Dir(execPath)
	} else {
		erc1155ProjectRoot = execPath
	}
}

func main() {
	// erc1155Demo([777n, 111n], [1n, 1000n], 100n)
	tokenIds := []*big.Int{big.NewInt(777), big.NewInt(111)}
	depositAmounts := []*big.Int{big.NewInt(1), big.NewInt(1000)}
	paymentAmount := big.NewInt(100)

	if err := runErc1155Demo(tokenIds, depositAmounts, paymentAmount); err != nil {
		log.Fatal("ERC-1155 demo failed:", err)
	}
}

// --- Config / receipt types (local to this file) ---

type erc1155Config struct {
	Network struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		ChainID  string `json:"chain-id"`
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

type erc1155ReceiptData struct {
	ContractAddress string `json:"contractAddress"`
}

type erc1155Receipts map[string]erc1155ReceiptData

// --- Helpers ---

func loadErc1155Config() (*erc1155Config, error) {
	data, err := os.ReadFile(filepath.Join(erc1155ProjectRoot, "enygmadvp.config.json"))
	if err != nil {
		return nil, err
	}
	var cfg erc1155Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadErc1155Receipts() (erc1155Receipts, error) {
	data, err := os.ReadFile(filepath.Join(erc1155ProjectRoot, "build", "receipts.json"))
	if err != nil {
		return nil, err
	}
	var r erc1155Receipts
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return r, nil
}

func loadErc1155ContractABI(contractPath string) (abi.ABI, error) {
	artifactPath := filepath.Join(erc1155ProjectRoot, "artifacts", "contracts", contractPath+".json")
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

func makeErc1155TransactOpts(privateKeyHex string, chainID *big.Int) (*bind.TransactOpts, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	pk, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return bind.NewKeyedTransactorWithChainID(pk, chainID)
}

func makeErc1155KeyPair() (core.KeyPair, error) {
	priv, pub, err := core.NewKeyPair()
	if err != nil {
		return core.KeyPair{}, err
	}
	return core.KeyPair{PrivateKey: priv, PublicKey: pub}, nil
}

// erc1155ToProofReceipt converts a ProofResult to a ProofReceipt.
// SnarkProof is left zero-valued — not populated by current gnark server parser.
func erc1155ToProofReceipt(r *core.ProofResult) endpoints.ProofReceipt {
	return endpoints.ProofReceipt{
		Statement:       r.Statement,
		NumberOfInputs:  big.NewInt(int64(r.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(r.NumberOfOutputs)),
	}
}

// transactAndWait submits a method call on a bound contract and waits for the receipt.
func transactAndWait(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	method string,
	args ...interface{},
) error {
	contract := bind.NewBoundContract(contractAddr, contractABI, client, client, client)
	tx, err := contract.Transact(auth, method, args...)
	if err != nil {
		return fmt.Errorf("%s failed: %w", method, err)
	}
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return fmt.Errorf("waiting for %s receipt failed: %w", method, err)
	}
	if receipt.Status == 0 {
		return fmt.Errorf("%s transaction reverted", method)
	}
	return nil
}

// --- Main demo ---

// runErc1155Demo corresponds to erc1155Demo(tokenIds, depositAmounts, paymentAmount) in JS.
func runErc1155Demo(tokenIds []*big.Int, depositAmounts []*big.Int, paymentAmount *big.Int) error {
	// tokenIds[0]=nonFungId, tokenIds[1]=fungId
	// depositAmounts[0]=nonFungAmount, depositAmounts[1]=fungAmount
	nonFungId := tokenIds[0]
	fungId := tokenIds[1]
	nonFungAmount := depositAmounts[0]
	fungAmount := depositAmounts[1]

	// ---- Config & Merkle trees ----

	cfg, err := loadErc1155Config()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	// JS: dvpConf["tree-depth"] — correct path in Go:
	treeDepth := cfg.Circom.MetaParameters.TreeDepth

	// Three trees: coin commitments, fungible asset group, non-fungible asset group.
	erc1155Tree, err := core.NewMerkleTreeWithPath(treeDepth, "erc1155", "")
	if err != nil {
		return fmt.Errorf("failed to create erc1155 tree: %w", err)
	}
	fungibleTree, err := core.NewMerkleTreeWithPath(treeDepth, "fungibleAssetGroup", "")
	if err != nil {
		return fmt.Errorf("failed to create fungibleAssetGroup tree: %w", err)
	}
	nonFungibleTree, err := core.NewMerkleTreeWithPath(treeDepth, "nonFungibleAssetGroup", "")
	if err != nil {
		return fmt.Errorf("failed to create nonFungibleAssetGroup tree: %w", err)
	}

	// ---- Ethereum accounts ----

	chainID, ok := new(big.Int).SetString(cfg.Network.ChainID, 10)
	if !ok {
		return fmt.Errorf("invalid chain-id: %s", cfg.Network.ChainID)
	}
	ownerAuth, err := makeErc1155TransactOpts(cfg.Network.Accounts[0].Private, chainID)
	if err != nil {
		return fmt.Errorf("owner auth: %w", err)
	}
	aliceAuth, err := makeErc1155TransactOpts(cfg.Network.Accounts[1].Private, chainID)
	if err != nil {
		return fmt.Errorf("alice auth: %w", err)
	}
	bobAuth, err := makeErc1155TransactOpts(cfg.Network.Accounts[2].Private, chainID)
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

	receipts, err := loadErc1155Receipts()
	if err != nil {
		return fmt.Errorf("failed to load receipts: %w", err)
	}
	erc1155Addr := common.HexToAddress(receipts["ERC1155"].ContractAddress)
	erc1155VaultAddr := common.HexToAddress(receipts["Erc1155CoinVault"].ContractAddress)
	dvpAddr := common.HexToAddress(receipts["EnygmaDvp"].ContractAddress)

	// ---- Load ABIs ----

	erc1155ABI, err := loadErc1155ContractABI("erc1155/contracts/RaylsERC1155.sol/RaylsERC1155")
	if err != nil {
		return fmt.Errorf("failed to load ERC1155 ABI: %w", err)
	}
	erc1155VaultABI, err := loadErc1155ContractABI("core/contracts/vaults/Erc1155CoinVault.sol/Erc1155CoinVault")
	if err != nil {
		return fmt.Errorf("failed to load Erc1155CoinVault ABI: %w", err)
	}
	dvpABI, err := loadErc1155ContractABI("core/contracts/EnygmaDvp.sol/EnygmaDvp")
	if err != nil {
		return fmt.Errorf("failed to load EnygmaDvp ABI: %w", err)
	}

	// ---- Gnark prover client ----

	gnarkClient := core.NewGnarkClient("http://localhost:8081")

	// ---- Step 1: Register fungible token type ----

	fmt.Println("Registering Fungible ERC-1155 tokenId to RaylsERC1155")
	if err := transactAndWait(client, ownerAuth, erc1155ABI, erc1155Addr,
		"registerNewToken",
		big.NewInt(0),             // type
		big.NewInt(0),             // fungibility (0 = fungible)
		"Test Fungible",           // name
		"TFT",                     // symbol
		fungId,                    // offchainId
		big.NewInt(1000000000000000), // maxSupply
		big.NewInt(18),            // decimals
		[]*big.Int{},              // subTokenIds
		[]*big.Int{},              // subTokenValues
		big.NewInt(0),             // data
		[]*big.Int{},              // additionalAttrs
	); err != nil {
		return fmt.Errorf("registerNewToken (fungible): %w", err)
	}

	fmt.Println("Registering Fungible ERC-1155 tokenId to fungible AssetGroup")
	if err := transactAndWait(client, ownerAuth, dvpABI, dvpAddr,
		"addTokenToGroup",
		big.NewInt(vaultIDErc1155),
		[]*big.Int{big.NewInt(0), fungId}, // uniqueIdParams: [0, fungId]
		big.NewInt(0),                     // groupId (fungible group = 0)
	); err != nil {
		return fmt.Errorf("addTokenToGroup (fungible): %w", err)
	}

	// Compute uidFung off-chain (fungibility flag = 0 for fungible).
	// JS: jsUtils.erc1155UniqueId(BigInt(erc1155Address), fungId, 0n)
	erc1155AddrBig := new(big.Int).SetBytes(erc1155Addr.Bytes())
	uidFung, err := core.Erc1155UniqueId(erc1155AddrBig, fungId, big.NewInt(0))
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (fungible): %w", err)
	}
	fungibleTree.InsertLeaf(uidFung)

	// ---- Step 2: Register non-fungible token type ----

	fmt.Println("Registering non-Fungible ERC-1155 tokenId to RaylsERC1155")
	if err := transactAndWait(client, ownerAuth, erc1155ABI, erc1155Addr,
		"registerNewToken",
		big.NewInt(0),  // type
		big.NewInt(1),  // fungibility (1 = non-fungible)
		"Test Non-fungible", // name
		"TNFT",         // symbol
		nonFungId,      // offchainId
		big.NewInt(0),  // maxSupply
		big.NewInt(0),  // decimals
		[]*big.Int{},   // subTokenIds
		[]*big.Int{},   // subTokenValues
		big.NewInt(0),  // data
		[]*big.Int{},   // additionalAttrs
	); err != nil {
		return fmt.Errorf("registerNewToken (non-fungible): %w", err)
	}

	fmt.Println("Registering non-Fungible ERC-1155 tokenId to Non-fungible AssetGroup")
	if err := transactAndWait(client, ownerAuth, dvpABI, dvpAddr,
		"addTokenToGroup",
		big.NewInt(vaultIDErc1155),
		[]*big.Int{big.NewInt(0), nonFungId}, // uniqueIdParams: [0, nonFungId]
		big.NewInt(1),                         // groupId (non-fungible group = 1)
	); err != nil {
		return fmt.Errorf("addTokenToGroup (non-fungible): %w", err)
	}

	// Compute uidNonFung off-chain (fungibility flag = 1 for non-fungible).
	// JS: jsUtils.erc1155UniqueId(BigInt(erc1155Address), nonFungId, 1n)
	uidNonFung, err := core.Erc1155UniqueId(erc1155AddrBig, nonFungId, big.NewInt(1))
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (non-fungible): %w", err)
	}
	nonFungibleTree.InsertLeaf(uidNonFung)

	// ---- Step 3: Mint non-fungible ERC-1155 for Alice ----

	fmt.Println("Minting non-Fungible ERC-1155")
	if _, err := endpoints.MintErc1155(client, ownerAuth, erc1155ABI, erc1155Addr,
		aliceAddr, nonFungId, nonFungAmount); err != nil {
		return fmt.Errorf("MintErc1155 (non-fungible for Alice): %w", err)
	}

	// ---- Step 4: Alice deposits non-fungible ERC-1155 ----

	nftKeyDeposit, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("nftKeyDeposit: %w", err)
	}

	fmt.Println("Depositing non-fungible ERC-1155")
	if _, err := endpoints.DepositErc1155(client, aliceAuth,
		erc1155VaultABI, erc1155VaultAddr,
		erc1155ABI, erc1155Addr,
		nonFungId, nonFungAmount, nftKeyDeposit.PublicKey); err != nil {
		return fmt.Errorf("DepositErc1155 (Alice non-fungible): %w", err)
	}
	// Compute Alice's deposit commitment directly (coin uid uses actual amount).
	aliceCoinUID, err := core.Erc1155UniqueId(erc1155AddrBig, nonFungId, nonFungAmount)
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (Alice coin): %w", err)
	}
	aliceNFTCmt, err := core.GetCommitment(aliceCoinUID, nftKeyDeposit.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (Alice NFT deposit): %w", err)
	}

	erc1155Tree.InsertLeaf(aliceNFTCmt)
	aliceNFTCoinProof, err := erc1155Tree.GenerateProof(aliceNFTCmt)
	if err != nil {
		return fmt.Errorf("GenerateProof (Alice NFT coin): %w", err)
	}
	aliceNFTCoinTreeNum := big.NewInt(int64(erc1155Tree.LastTreeNumber()))
	aliceNFTGroupProof, err := nonFungibleTree.GenerateProof(uidNonFung)
	if err != nil {
		return fmt.Errorf("GenerateProof (Alice NFT group): %w", err)
	}
	aliceNFTGroupTreeNum := big.NewInt(int64(nonFungibleTree.LastTreeNumber()))

	// ---- Step 5: Mint and deposit fungible ERC-1155 for Bob (twice) ----

	fundKey0, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("fundKey0: %w", err)
	}
	fundKey1, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("fundKey1: %w", err)
	}

	fmt.Println("Minting fungible ERC-1155")
	totalFungMint := new(big.Int).Mul(fungAmount, big.NewInt(2))
	if _, err := endpoints.MintErc1155(client, ownerAuth, erc1155ABI, erc1155Addr,
		bobAddr, fungId, totalFungMint); err != nil {
		return fmt.Errorf("MintErc1155 (fungible for Bob): %w", err)
	}

	// Bob coin uid uses the actual deposit amount (fungAmount), not the fungibility flag.
	bobCoinUID, err := core.Erc1155UniqueId(erc1155AddrBig, fungId, fungAmount)
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (Bob coin): %w", err)
	}

	fmt.Println("Depositing fungible ERC-1155")
	if _, err := endpoints.DepositErc1155(client, bobAuth,
		erc1155VaultABI, erc1155VaultAddr,
		erc1155ABI, erc1155Addr,
		fungId, fungAmount, fundKey0.PublicKey); err != nil {
		return fmt.Errorf("DepositErc1155 (Bob 1st): %w", err)
	}
	bobCmt1, err := core.GetCommitment(bobCoinUID, fundKey0.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (Bob 1st): %w", err)
	}

	erc1155Tree.InsertLeaf(bobCmt1)
	bobCoin1Proof, err := erc1155Tree.GenerateProof(bobCmt1)
	if err != nil {
		return fmt.Errorf("GenerateProof (Bob coin 1): %w", err)
	}
	bobCoin1TreeNum := big.NewInt(int64(erc1155Tree.LastTreeNumber()))
	// Bob's fungible group proof is the same for both deposits.
	bobFungGroupProof, err := fungibleTree.GenerateProof(uidFung)
	if err != nil {
		return fmt.Errorf("GenerateProof (Bob fungible group): %w", err)
	}
	bobFungGroupTreeNum := big.NewInt(int64(fungibleTree.LastTreeNumber()))

	fmt.Println("Depositing second amount for fungible ERC-1155")
	if _, err := endpoints.DepositErc1155(client, bobAuth,
		erc1155VaultABI, erc1155VaultAddr,
		erc1155ABI, erc1155Addr,
		fungId, fungAmount, fundKey1.PublicKey); err != nil {
		return fmt.Errorf("DepositErc1155 (Bob 2nd): %w", err)
	}
	bobCmt2, err := core.GetCommitment(bobCoinUID, fundKey1.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (Bob 2nd): %w", err)
	}

	erc1155Tree.InsertLeaf(bobCmt2)
	bobCoin2Proof, err := erc1155Tree.GenerateProof(bobCmt2)
	if err != nil {
		return fmt.Errorf("GenerateProof (Bob coin 2): %w", err)
	}
	bobCoin2TreeNum := big.NewInt(int64(erc1155Tree.LastTreeNumber()))

	// ---- Step 6: Generate cross-party commitments ----

	fmt.Println("Alice generates commitment of non-fungible token for Bob")

	bobNFTKey, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("bobNFTKey: %w", err)
	}
	bobChangeKey, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("bobChangeKey: %w", err)
	}
	alicePaymentKey, err := makeErc1155KeyPair()
	if err != nil {
		return fmt.Errorf("alicePaymentKey: %w", err)
	}

	// uid2: Alice's non-fungible coin uid (amount-based, used for nftCommitment)
	// JS: erc1155UniqueId(BigInt(erc1155Address), nonFungId, nonFungAmount)
	uid2, err := core.Erc1155UniqueId(erc1155AddrBig, nonFungId, nonFungAmount)
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (uid2): %w", err)
	}
	fmt.Println("Generating NFT commitment for Bob.")
	nftCommitment, err := core.GetCommitment(uid2, bobNFTKey.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (nftCommitment): %w", err)
	}

	// uid4: Alice's payment coin uid (amount-based, used for paymentCommitment)
	// JS: erc1155UniqueId(BigInt(erc1155Address), fungId, paymentAmount)
	fmt.Println("Generating Payment commitment for Alice.")
	changeAmount := new(big.Int).Sub(totalFungMint, paymentAmount)
	uid4, err := core.Erc1155UniqueId(erc1155AddrBig, fungId, paymentAmount)
	if err != nil {
		return fmt.Errorf("Erc1155UniqueId (uid4): %w", err)
	}
	paymentCommitment, err := core.GetCommitment(uid4, alicePaymentKey.PublicKey)
	if err != nil {
		return fmt.Errorf("GetCommitment (paymentCommitment): %w", err)
	}

	// ---- Step 7: Generate ZK proofs ----

	// Alice: non-fungible ownership proof (isFungible=false)
	fmt.Println("Alice generates a tx to send her NFT to Bob")
	params1, err := endpoints.GenerateSingleErc1155Proof(
		gnarkClient,
		paymentCommitment,   // stMessage
		nonFungAmount,       // amountOrOne
		nftKeyDeposit,       // keyIn
		bobNFTKey,           // keyOut
		treeDepth,
		aliceNFTCoinProof,   // coin tree proof
		aliceNFTCoinTreeNum, // coin tree number
		erc1155Addr,
		nonFungId,
		aliceNFTGroupTreeNum,  // non-fungible group tree number
		aliceNFTGroupProof,    // non-fungible group proof
		false,                 // isFungible
	)
	if err != nil {
		return fmt.Errorf("GenerateSingleErc1155Proof: %w", err)
	}

	// Bob: fungible JoinSplit proof (isFungible=true)
	fmt.Println("Bob generates a tx to send payment to Alice")
	params2, err := endpoints.GenerateErc1155JoinSplitProof(
		gnarkClient,
		nftCommitment,                          // stMessage
		[]*big.Int{fungAmount, fungAmount},      // wtValuesIn
		[]core.KeyPair{fundKey0, fundKey1},      // keysIn
		[]*big.Int{paymentAmount, changeAmount}, // wtValuesOut
		[]core.KeyPair{alicePaymentKey, bobChangeKey}, // keysOut
		treeDepth,
		[]*core.MerkleProof{bobCoin1Proof, bobCoin2Proof}, // coin tree proofs
		[]*big.Int{bobCoin1TreeNum, bobCoin2TreeNum},       // coin tree numbers
		erc1155Addr,
		fungId,
		bobFungGroupTreeNum, // fungible group tree number (same for both inputs)
		bobFungGroupProof,   // fungible group proof (same for both inputs)
	)
	if err != nil {
		return fmt.Errorf("GenerateErc1155JoinSplitProof: %w", err)
	}

	// ---- Step 8: Relayer submits swap ----

	fmt.Println("Swapping")
	// params2 = payment (Bob's fungible JoinSplit), params1 = delivery (Alice's non-fungible)
	p2Receipt := erc1155ToProofReceipt(params2)
	p1Receipt := erc1155ToProofReceipt(params1)

	swapCommitments, err := endpoints.Swap(
		client, ownerAuth, dvpABI, dvpAddr,
		p2Receipt,                      // paymentReceipt
		p1Receipt,                      // deliveryReceipt
		big.NewInt(vaultIDErc1155),     // paymentVaultId
		big.NewInt(vaultIDErc1155),     // deliveryVaultId
	)
	if err != nil {
		return fmt.Errorf("Swap: %w", err)
	}

	// swapCommitments: [paymentReceipt.Statement[7], paymentReceipt.Statement[8], deliveryReceipt.Statement[4]]
	// [0] = Alice's fungible payment note
	// [1] = Bob's fungible change note
	// [2] = Bob's non-fungible note
	fmt.Println(swapCommitments)

	// ---- Step 9: Update Merkle trees ----

	// All 3 commitments go into the single ERC-1155 coin tree.
	erc1155Tree.InsertLeaves(swapCommitments)

	// Alice's payment (fungible)
	alicePaymentProof, err := erc1155Tree.GenerateProof(swapCommitments[0])
	if err != nil {
		return fmt.Errorf("GenerateProof (alice payment): %w", err)
	}
	alicePaymentTreeNum := big.NewInt(int64(erc1155Tree.LastTreeNumber()))
	// Alice's fungible group proof (fungible group tree unchanged after swap)
	aliceFungGroupProof, err := fungibleTree.GenerateProof(uidFung)
	if err != nil {
		return fmt.Errorf("GenerateProof (alice fungible group): %w", err)
	}
	aliceFungGroupTreeNum := big.NewInt(int64(fungibleTree.LastTreeNumber()))

	// Bob's non-fungible NFT from swap (bobCoins[3] in JS)
	bobNFTSwapProof, err := erc1155Tree.GenerateProof(swapCommitments[2])
	if err != nil {
		return fmt.Errorf("GenerateProof (bob NFT): %w", err)
	}
	bobNFTSwapTreeNum := big.NewInt(int64(erc1155Tree.LastTreeNumber()))
	bobNonFungGroupProof, err := nonFungibleTree.GenerateProof(uidNonFung)
	if err != nil {
		return fmt.Errorf("GenerateProof (bob non-fungible group): %w", err)
	}
	bobNonFungGroupTreeNum := big.NewInt(int64(nonFungibleTree.LastTreeNumber()))

	// ---- Step 10: Alice withdraws fungible payment ----

	fmt.Println("Alice withdraws fund")
	_, err = endpoints.WithdrawERC1155(
		client, aliceAuth,
		erc1155VaultABI, erc1155VaultAddr,
		gnarkClient,
		paymentAmount,
		fungId,
		alicePaymentKey,
		aliceAddr,
		erc1155Addr,
		treeDepth,
		alicePaymentProof,
		alicePaymentTreeNum,
		aliceFungGroupTreeNum,
		aliceFungGroupProof,
		true, // isFungible
	)
	if err != nil {
		return fmt.Errorf("WithdrawERC1155 (Alice): %w", err)
	}
	fmt.Println("Alice withdrew!")

	// ---- Step 11: Bob withdraws non-fungible token ----

	fmt.Println("Bob withdraws bought Non-fungible ERC1155 token....")
	_, err = endpoints.WithdrawERC1155(
		client, bobAuth,
		erc1155VaultABI, erc1155VaultAddr,
		gnarkClient,
		nonFungAmount,
		nonFungId,
		bobNFTKey,
		bobAddr,
		erc1155Addr,
		treeDepth,
		bobNFTSwapProof,
		bobNFTSwapTreeNum,
		bobNonFungGroupTreeNum,
		bobNonFungGroupProof,
		false, // isFungible (non-fungible)
	)
	if err != nil {
		return fmt.Errorf("WithdrawERC1155 (Bob): %w", err)
	}
	fmt.Println("Bob withdrew bought Non-fungible Erc1155")

	// ---- Step 12: Verify balance ----

	erc1155Contract := bind.NewBoundContract(erc1155Addr, erc1155ABI, client, client, client)
	var balOut []interface{}
	if err := erc1155Contract.Call(nil, &balOut, "balanceOf", bobAddr, nonFungId); err != nil {
		fmt.Printf("balanceOf check failed: %v\n", err)
	} else if len(balOut) > 0 {
		fmt.Printf("Checking whether Bob is the owner of Non-fungible Erc1155 token %s = %v\n",
			nonFungId.String(), balOut[0])
	}

	// ---- Step 13: Save Merkle tree ----

	fmt.Println("---------------------------------")
	fmt.Println("---------------------------------")
	fmt.Println("---------------------------------")
	fmt.Println("Testing EREC1155 batch mode:")

	if err := erc1155Tree.SaveToFile(); err != nil {
		fmt.Printf("warning: failed to save erc1155 tree: %v\n", err)
	}

	fmt.Println("Erc1155 Swap demo: DONE.")
	return nil
}

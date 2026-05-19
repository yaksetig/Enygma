package web3

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/crypto/sha3"
)

// Contract artifact paths (relative to project artifacts/contracts/ directory)
const (
	EnygmaDvpArtifact      = "core/contracts/EnygmaDvp.sol/EnygmaDvp"
	RaylsERC20Artifact     = "erc20/contracts/RaylsERC20.sol/RaylsERC20"
	RaylsERC721Artifact    = "erc721/contracts/RaylsERC721.sol/RaylsERC721"
	RaylsERC1155Artifact   = "erc1155/contracts/RaylsERC1155.sol/RaylsERC1155"
	Erc20CoinVaultArtifact = "core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault"
)

// ContractArtifact represents a Hardhat artifact JSON file
type ContractArtifact struct {
	ContractName string          `json:"contractName"`
	ABI          json.RawMessage `json:"abi"`
	Bytecode     string          `json:"bytecode"`
}

// ContractFactory holds a parsed ABI and bytecode for deploying or interacting with contracts
type ContractFactory struct {
	ABI      abi.ABI
	Bytecode []byte
}

// CustomErrors maps contract names to their custom error signatures
var CustomErrors = map[string][]string{
	"RaylsERC1155": {
		"SubTokenTransferNotAllowed()",
		"ZeroIdNotAllowed()",
		"ZeroValueMintNotAllowed()",
		"IdReservedForMetaToken()",
		"NotImplemented()",
		"ValueFungibilityInconsistency()",
		"InvalidMintData()",
		"IdsValuesMismatch()",
		"NotEnoughSubTokens()",
		"MaxSupplyExceeded()",
		"TokenAlreadyRegistered()",
		"InvalidMaxSupply()",
		"IdsValuesLengthMismatch()",
	},
	"EnygmaDvp": {
		"AuctionIdExists()",
		"AuctionIdMismatch()",
		"BlindedBidMismatch()",
		"WinningBidOpeningMismatch()",
		"NotWinningBidsCountMismath()",
		"BidStateMismatch()",
		"RottenChallenge()",
		"InvalidOpening()",
		"InvalidChallenge()",
		"InvalidErc721Transfer()",
		"InvalidErc20Transfer()",
		"InvalidErc1155Transfer()",
		"InvalidErc1155BatchTransfer()",
		"JoinSplitWithSameCommitments()",
		"InvalidMerkleRoot()",
		"InvalidNullifier()",
		"InvalidNumberOfInputs()",
		"NotImplemented()",
		"GroupMembershipMismatch()",
	},
	"FungibilityMerkle": {
		"InvalidFungibility()",
		"InvalidMerkleRoot()",
		"InvalidNumberOfInputs()",
		"WrongNumberOfIdentifiers()",
		"NotImplemented()",
		"VaultAddressMismatch()",
	},
	"AbstractCoinVault": {
		"RottenChallenge()",
		"InvalidOpening()",
		"InvalidErc721Transfer()",
		"InvalidErc20Transfer()",
		"InvalidErc1155Transfer()",
		"InvalidErc1155BatchTransfer()",
		"JoinSplitWithSameCommitments()",
		"InvalidMerkleRoot()",
		"InvalidNullifier()",
		"InvalidNumberOfInputs()",
		"WrongNumberOfIdentifiers()",
		"NotImplemented()",
		"FungibilityMismatch()",
	},
}

// customErrorDecoder maps contract name -> error selector (0x prefix, 4 bytes hex) -> error string
var customErrorDecoder map[string]map[string]string

func init() {
	loadCustomErrorsHash()
}

// loadCustomErrorsHash builds the selector-to-error-string lookup table
func loadCustomErrorsHash() {
	customErrorDecoder = make(map[string]map[string]string)
	for contractName, errors := range CustomErrors {
		customErrorDecoder[contractName] = make(map[string]string)
		for _, errString := range errors {
			hasher := sha3.NewLegacyKeccak256()
			hasher.Write([]byte(errString))
			hash := hasher.Sum(nil)
			selector := fmt.Sprintf("0x%x", hash[:4])
			customErrorDecoder[contractName][selector] = errString
		}
	}
}

// ParseCustomError attempts to decode a revert error selector into a human-readable error name
func ParseCustomError(errorData string, contractName string) string {
	if len(errorData) < 10 {
		return "CustomErrorNotFound"
	}

	selector := errorData[:10] // "0x" + 4 bytes hex
	decoderMap, ok := customErrorDecoder[contractName]
	if !ok {
		return "CustomErrorNotFound"
	}

	errString, ok := decoderMap[selector]
	if !ok {
		return "CustomErrorNotFound"
	}

	return errString
}

// --- Client and wallet ---

// GetClient creates an Ethereum JSON-RPC client
func GetClient(rpcURL string) (*ethclient.Client, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client at %s: %w", rpcURL, err)
	}
	return client, nil
}

// GetTransactOpts creates transaction signing options from a private key hex string
func GetTransactOpts(privateKeyHex string, chainID *big.Int) (*bind.TransactOpts, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	return auth, nil
}

// --- Artifact loading ---

// LoadArtifact loads a Hardhat contract artifact from the project artifacts directory
func LoadArtifact(projectRoot, contractPath string) (*ContractArtifact, error) {
	artifactPath, err := safeArtifactFile(projectRoot, contractPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact %s: %w", artifactPath, err)
	}

	var artifact ContractArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("failed to parse artifact JSON: %w", err)
	}

	return &artifact, nil
}

func safeArtifactFile(projectRoot, contractPath string) (string, error) {
	if strings.TrimSpace(contractPath) == "" {
		return "", fmt.Errorf("contract path must not be empty")
	}
	if strings.Contains(contractPath, "\x00") || filepath.IsAbs(contractPath) {
		return "", fmt.Errorf("contract path %q is invalid", contractPath)
	}

	artifactRoot := filepath.Join(projectRoot, "artifacts", "contracts")
	candidate := filepath.Join(artifactRoot, contractPath+".json")
	return safePathWithin(artifactRoot, candidate)
}

func safePathWithin(root, candidate string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	absRoot = filepath.Clean(absRoot)
	absCandidate = filepath.Clean(absCandidate)

	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return "", err
	}
	if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
		return absCandidate, nil
	}
	return "", fmt.Errorf("path %q is outside %q", candidate, root)
}

// LoadContractABI loads and parses only the ABI from a contract artifact
func LoadContractABI(projectRoot, contractPath string) (abi.ABI, error) {
	artifact, err := LoadArtifact(projectRoot, contractPath)
	if err != nil {
		return abi.ABI{}, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return abi.ABI{}, fmt.Errorf("failed to parse ABI: %w", err)
	}

	return parsedABI, nil
}

// --- Contract factory ---

// NewContractFactory creates a ContractFactory from a Hardhat artifact
func NewContractFactory(projectRoot, contractPath string) (*ContractFactory, error) {
	artifact, err := LoadArtifact(projectRoot, contractPath)
	if err != nil {
		return nil, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	bytecode := common.FromHex(artifact.Bytecode)

	return &ContractFactory{
		ABI:      parsedABI,
		Bytecode: bytecode,
	}, nil
}

// Deploy deploys the contract with the given constructor arguments
func (f *ContractFactory) Deploy(client *ethclient.Client, auth *bind.TransactOpts, args ...interface{}) (common.Address, *types.Receipt, error) {
	address, tx, _, err := bind.DeployContract(auth, f.ABI, f.Bytecode, client, args...)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("failed to deploy contract: %w", err)
	}

	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("failed to wait for deployment: %w", err)
	}

	return address, receipt, nil
}

// --- Named factory constructors (matching JS API) ---

// GetEnygmaDvpFactory returns a factory for the EnygmaDvp contract
func GetEnygmaDvpFactory(projectRoot string) (*ContractFactory, error) {
	return NewContractFactory(projectRoot, EnygmaDvpArtifact)
}

// GetRayls20Factory returns a factory for the RaylsERC20 contract
func GetRayls20Factory(projectRoot string) (*ContractFactory, error) {
	return NewContractFactory(projectRoot, RaylsERC20Artifact)
}

// GetRayls721Factory returns a factory for the RaylsERC721 contract
func GetRayls721Factory(projectRoot string) (*ContractFactory, error) {
	return NewContractFactory(projectRoot, RaylsERC721Artifact)
}

// GetRayls1155Factory returns a factory for the RaylsERC1155 contract
func GetRayls1155Factory(projectRoot string) (*ContractFactory, error) {
	return NewContractFactory(projectRoot, RaylsERC1155Artifact)
}

// GetErc20CoinVaultFactory returns a factory for the Erc20CoinVault contract
func GetErc20CoinVaultFactory(projectRoot string) (*ContractFactory, error) {
	return NewContractFactory(projectRoot, Erc20CoinVaultArtifact)
}

package core

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
	"github.com/ethereum/go-ethereum/ethclient"
)

// UserKeys holds the two public keys a sender needs to pay a recipient.
type UserKeys struct {
	SpendKey *big.Int // pk_spend — Poseidon(sk_spend), used to build output commitments
	ViewKey  []byte   // pk_view  — ML-KEM-768 encapsulation key (1184 bytes)
}

// loadRegistryABI reads the UserRegistry ABI from contracts/abis/UserRegistry.json.
// Walks up from the working directory to find the project root
// (identified by enygmapayment.config.json).
func loadRegistryABI() (abi.ABI, error) {
	dir, err := os.Getwd()
	if err != nil {
		return abi.ABI{}, err
	}
	for {
		candidate := filepath.Join(dir, "contracts", "abis", "UserRegistry.json")
		if data, err := os.ReadFile(candidate); err == nil {
			var artifact struct {
				ABI json.RawMessage `json:"abi"`
			}
			if err := json.Unmarshal(data, &artifact); err != nil {
				return abi.ABI{}, fmt.Errorf("parse UserRegistry artifact: %w", err)
			}
			return abi.JSON(strings.NewReader(string(artifact.ABI)))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return abi.ABI{}, fmt.Errorf("UserRegistry.json not found — compile UserRegistry.sol and copy the artifact to contracts/abis/")
}

// Register calls UserRegistry.register(pkSpend, pkView) on-chain, storing both
// keys in contract state. This is a one-time operation per address.
//
//   - pkSpend: caller's spend public key — Poseidon(sk_spend)
//   - pkView:  caller's ML-KEM-768 encapsulation key (1184 bytes)
func Register(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	registryAddr common.Address,
	pkSpend *big.Int,
	pkView []byte,
) error {
	registryABI, err := loadRegistryABI()
	if err != nil {
		return err
	}
	contract := bind.NewBoundContract(registryAddr, registryABI, client, client, client)
	tx, err := contract.Transact(auth, "register", pkSpend, pkView)
	if err != nil {
		return fmt.Errorf("UserRegistry.register: %w", err)
	}
	_, err = bind.WaitMined(context.Background(), client, tx)
	return err
}

// LookupKeys reads both the spend key and view key for a registered address
// directly from contract state via getKeys().
//
// Returns an error if the address has not registered.
func LookupKeys(
	client *ethclient.Client,
	registryAddr common.Address,
	user common.Address,
) (*UserKeys, error) {
	registryABI, err := loadRegistryABI()
	if err != nil {
		return nil, err
	}
	contract := bind.NewBoundContract(registryAddr, registryABI, client, client, client)

	var result []interface{}
	if err := contract.Call(&bind.CallOpts{}, &result, "getKeys", user); err != nil {
		return nil, fmt.Errorf("UserRegistry.getKeys(%s): %w", user.Hex(), err)
	}

	pkSpend, ok := result[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected type for pkSpend")
	}
	pkView, ok := result[1].([]byte)
	if !ok {
		return nil, fmt.Errorf("unexpected type for pkView")
	}

	return &UserKeys{
		SpendKey: pkSpend,
		ViewKey:  pkView,
	}, nil
}

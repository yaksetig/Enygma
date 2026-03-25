package endpoints

/*
Port of src/core/endpoints/admin.js

Admin-level operations: minting ERC-20/721/1155 tokens and registering
assets in groups on the EnygmaDvp contract.

JS used ethers.js `contract.connect(signer).method(...)` which couples the
signer to the contract instance. The Go equivalent separates concerns:
  - auth  *bind.TransactOpts  — carries the signing key and chain config
  - contractABI abi.ABI       — the parsed contract interface
  - contractAddr common.Address — the deployed address to call

Each function returns *types.Receipt (gas used, logs, etc.) so callers can
inspect the result without an extra wait call.
*/

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// transact is a thin helper that submits a contract call and waits for the receipt.
func transact(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	method string,
	args ...interface{},
) (*types.Receipt, error) {
	contract := bind.NewBoundContract(contractAddr, contractABI, client, client, client)
	tx, err := contract.Transact(auth, method, args...)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", method, err)
	}
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return nil, fmt.Errorf("waiting for %s receipt failed: %w", method, err)
	}
	return receipt, nil
}

// encodeUint256Slice ABI-encodes a uint256[] value.
// Equivalent to JS: web3.eth.abi.encodeParameter('uint256[]', values)
func encodeUint256Slice(values []*big.Int) ([]byte, error) {
	uint256SliceType, err := abi.NewType("uint256[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create uint256[] ABI type: %w", err)
	}
	args := abi.Arguments{{Type: uint256SliceType}}
	return args.Pack(values)
}

// AddAssetToGroup registers a token asset in the given group on the EnygmaDvp contract.
// Corresponds to: zkDvpContract.connect(admin).addAssetToGroup(vaultId, uniqueIdParams, groupId)
func AddAssetToGroup(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	vaultId *big.Int,
	uniqueIdParams []*big.Int,
	groupId *big.Int,
) (*types.Receipt, error) {
	receipt, err := transact(client, auth, contractABI, contractAddr,
		"addAssetToGroup", vaultId, uniqueIdParams, groupId)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Asset has been added to group %s\n", groupId.String())
	return receipt, nil
}

// MintErc20 mints ERC-20 tokens to accountAddress.
// Corresponds to: erc20Contract.connect(admin).mint(accountAddress, depositAmount)
func MintErc20(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	depositAmount *big.Int,
) (*types.Receipt, error) {
	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mint", accountAddress, depositAmount)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted ERC20 token for %s\n", accountAddress.Hex())
	return receipt, nil
}

// MintErc721 mints an ERC-721 NFT to accountAddress.
// Corresponds to: erc721Contract.connect(admin).mint(accountAddress, nft_id)
func MintErc721(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	nftID *big.Int,
) (*types.Receipt, error) {
	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mint", accountAddress, nftID)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted NFT for %s\n", accountAddress.Hex())
	return receipt, nil
}

// MintErc1155 mints an ERC-1155 token to accountAddress.
// Corresponds to: erc1155Contract.connect(admin).mint(accountAddress, token_id, amount, 0)
// Note: the JS passes literal 0 (not the data parameter) to the contract — empty bytes is used here.
func MintErc1155(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	tokenID *big.Int,
	amount *big.Int,
) (*types.Receipt, error) {
	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mint", accountAddress, tokenID, amount, []byte{})
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted Erc1155 for %s\n", accountAddress.Hex())
	return receipt, nil
}

// MintErc1155Batch batch-mints ERC-1155 tokens to accountAddress.
// Corresponds to: erc1155Contract.connect(admin).mintBatch(accountAddress, tokenIds, amounts, data)
func MintErc1155Batch(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	tokenIds []*big.Int,
	amounts []*big.Int,
	data []byte,
) (*types.Receipt, error) {
	fmt.Println("Admin: minting batch of Erc1155 ...")
	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mintBatch", accountAddress, tokenIds, amounts, data)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted Batch Erc1155 for %s\n", accountAddress.Hex())
	return receipt, nil
}

// MintErc1155Fungible mints a fungible ERC-1155 token to accountAddress.
// The data field is ABI-encoded as uint256[]([0]) to mark the token as fungible.
// Corresponds to: erc1155Contract.connect(admin).mint(accountAddress, token_id, amount, data)
// where data = web3.eth.abi.encodeParameter('uint256[]', [0n])
func MintErc1155Fungible(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	tokenID *big.Int,
	amount *big.Int,
) (*types.Receipt, error) {
	fmt.Println("Admin: minting fungible Erc1155 ...")

	data, err := encodeUint256Slice([]*big.Int{big.NewInt(0)})
	if err != nil {
		return nil, err
	}

	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mint", accountAddress, tokenID, amount, data)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted fungible Erc1155 with id=%s, amount=%s for %s\n",
		tokenID.String(), amount.String(), accountAddress.Hex())
	return receipt, nil
}

// MintErc1155NonFungible mints a non-fungible ERC-1155 token (amount = 1) to accountAddress.
// The data field is ABI-encoded as uint256[]([1]) to mark the token as non-fungible.
// Corresponds to: erc1155Contract.connect(admin).mint(accountAddress, token_id, 1n, data)
// where data = web3.eth.abi.encodeParameter('uint256[]', [1n])
func MintErc1155NonFungible(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	accountAddress common.Address,
	tokenID *big.Int,
) (*types.Receipt, error) {
	fmt.Println("Admin: minting non-fungible Erc1155 ...")

	data, err := encodeUint256Slice([]*big.Int{big.NewInt(1)})
	if err != nil {
		return nil, err
	}

	receipt, err := transact(client, auth, contractABI, contractAddr,
		"mint", accountAddress, tokenID, big.NewInt(1), data)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Minted non-fungible Erc1155 with id=%s for %s\n",
		tokenID.String(), accountAddress.Hex())
	return receipt, nil
}

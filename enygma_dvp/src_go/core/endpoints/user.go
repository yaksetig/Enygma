package endpoints

/*
Port of src/core/endpoints/user.js

User-level operations: depositing tokens into vault contracts, generating ZK
proofs for transfer/ownership, and withdrawing tokens using those proofs.

JS mapping:
  - JS `contract.connect(account).method(...)` → Go: bind.NewBoundContract + auth
  - JS `prover.Erc20Proof(...)`                → Go: gnarkClient.Erc20JoinSplitProof(...)
  - JS `prover.Erc721Proof(...)`               → Go: gnarkClient.Erc721OwnershipProof(...)
  - JS `prover.Erc1155FungibleProof(...)`      → Go: gnarkClient.Erc1155FungibleJoinSplitProof(...)
  - JS `prover.Erc1155NonFungibleProof(...)`   → Go: gnarkClient.Erc1155NonFungibleOwnershipProof(...)

Notes:
  - ProofResult.Proof is not populated by the current gnark response parser; callers
    submitting proofs on-chain receive a ProofReceipt with a zero-valued SnarkProof.
    Populate Receipt.Proof from the server's raw response before submitting.
  - getCommitmentsFromTx scans for a "Commitment" event absent from the current ABI
    (see relayer.go). It will always return an error on current contracts.
  - MixErc20's JS source passes erc20Address as the treeDepth argument (a bug).
    The Go signature requires correct argument types; see the function comment.
  - GenerateOwnershipProof takes a raw tokenId, not a pre-hashed uniqueId.
    Erc721OwnershipProof computes the uniqueId internally (JS did it externally).
  - GenerateErc1155BatchProof is not yet implemented: Erc1155BatchProof is missing
    from the Go gnark client.
  - withdrawERC1155Batch is unexported and non-functional: the JS source references
    undefined variables (amount, withdrawKey, merkleProof, etc.).
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

	"enygma_dvp/src_go/core"
	"enygma_dvp/src_go/web3"
)

// --- Internal helpers ---

// buildProofReceipt converts a ProofResult to a ProofReceipt for ABI encoding.
// NOTE: The SnarkProof (a, b, c) is left zero-valued because ProofResult.Proof is
// not currently populated by the gnark server response parser.
func buildProofReceipt(r *core.ProofResult) ProofReceipt {
	return ProofReceipt{
		Statement:       r.Statement,
		NumberOfInputs:  big.NewInt(int64(r.NumberOfInputs)),
		NumberOfOutputs: big.NewInt(int64(r.NumberOfOutputs)),
	}
}

// addressToBigInt converts a common.Address to *big.Int for use as a circuit input.
func addressToBigInt(addr common.Address) *big.Int {
	return new(big.Int).SetBytes(addr.Bytes())
}

// getCommitmentsFromTx scans a transaction receipt for all "Commitment" events and
// returns the commitment values as a slice.
// NOTE: the "Commitment" event is absent from the current EnygmaDvp ABI; this
// function is a port of the JS helper and will error on current contracts.
func getCommitmentsFromTx(contractABI abi.ABI, receipt *types.Receipt) ([]*big.Int, error) {
	fmt.Printf("Gas used: %d\n", receipt.GasUsed)

	event, ok := contractABI.Events["Commitment"]
	if !ok {
		return nil, fmt.Errorf("Commitment event not found in ABI (may have been renamed in current contract version)")
	}

	var commitments []*big.Int
	for _, log := range receipt.Logs {
		if len(log.Topics) == 0 {
			continue
		}
		if log.Topics[0] != event.ID {
			continue
		}

		values := make(map[string]interface{})
		if err := contractABI.UnpackIntoMap(values, event.Name, log.Data); err != nil {
			continue
		}

		if cmt, ok := values["commitment"].(*big.Int); ok {
			commitments = append(commitments, cmt)
		}
	}

	if len(commitments) == 0 {
		return nil, fmt.Errorf("no Commitment events found in transaction logs")
	}
	fmt.Printf("new commitments: %v\n", commitments)
	return commitments, nil
}

// --- Deposit functions ---

// DepositErc20 approves the vault to spend ERC-20 tokens, then deposits them.
// Returns the commitment values emitted by the "Commitment" event.
// Corresponds to: depositErc20(account, depositAmount, depositKey, erc20VaultContract, erc20Contract, merkleTree)
func DepositErc20(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	erc20ABI abi.ABI,
	erc20Addr common.Address,
	depositAmount *big.Int,
	depositPublicKey *big.Int,
) ([]*big.Int, error) {
	// Step 1: erc20.approve(vaultAddr, depositAmount)
	erc20Contract := bind.NewBoundContract(erc20Addr, erc20ABI, client, client, client)
	approveTx, err := erc20Contract.Transact(auth, "approve", vaultAddr, depositAmount)
	if err != nil {
		return nil, fmt.Errorf("approve failed: %w", err)
	}
	approveReceipt, err := bind.WaitMined(context.Background(), client, approveTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for approve receipt failed: %w", err)
	}
	fmt.Printf("...approves the transfer of Erc20 to ZkDvp\n")
	fmt.Printf("gasUsed: %d\n", approveReceipt.GasUsed)

	// Step 2: vault.deposit([depositAmount, depositPublicKey])
	params := []*big.Int{depositAmount, depositPublicKey}
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	depositTx, err := vaultContract.Transact(auth, "deposit", params)
	if err != nil {
		return nil, fmt.Errorf("deposit failed: %w", err)
	}
	depositReceipt, err := bind.WaitMined(context.Background(), client, depositTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for deposit receipt failed: %w", err)
	}

	return getCommitmentsFromTx(vaultABI, depositReceipt)
}

// WithdrawErc20 generates a JoinSplit proof and withdraws tokens from the ERC-20 vault.
// Returns statement[1 + numberOfInputs*3], the first output commitment index.
// Corresponds to: withdrawErc20(account, amount, withdrawKey, erc20VaultContract, erc20Contract,
//
//	merkleDepth, merkleProof, merkleRoot, treeNumber)
func WithdrawErc20(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	gnarkClient *core.GnarkClient,
	amount *big.Int,
	withdrawKey core.KeyPair,
	accountAddr common.Address,
	erc20Addr common.Address,
	merkleDepth int,
	merkleProof *core.MerkleProof,
	stTreeNumber *big.Int,
	use10_2 bool,
) (*big.Int, error) {
	dummyPriv, dummyPub, err := core.NewKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate dummy key pair: %w", err)
	}
	dummyKey := core.KeyPair{PrivateKey: dummyPriv, PublicKey: dummyPub}
	outKey := core.KeyPair{PublicKey: addressToBigInt(accountAddr)}

	// Corresponds to JS: generateErc20JoinSplitProof(0n, [amount, 0n], [withdrawKey, dummyKey],
	//   [amount, 0n], [{publicKey: BigInt(accountAddress)}, dummyKey], ...)
	result, err := gnarkClient.Erc20JoinSplitProof(
		big.NewInt(0),
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{withdrawKey, dummyKey},
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{outKey, dummyKey},
		merkleDepth,
		[]*core.MerkleProof{merkleProof, nil}, // nil safe: dummy input has value=0
		[]*big.Int{stTreeNumber, big.NewInt(0)},
		addressToBigInt(erc20Addr),
		use10_2,
	)
	if err != nil {
		return nil, fmt.Errorf("generateErc20JoinSplitProof failed: %w", err)
	}

	proofReceipt := buildProofReceipt(result)

	// vault.withdraw([amount], accountAddr, proof)
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	withdrawTx, err := vaultContract.Transact(auth, "withdraw",
		[]*big.Int{amount}, accountAddr, proofReceipt)
	if err != nil {
		return nil, fmt.Errorf("withdraw failed: %w", err)
	}
	if _, err := bind.WaitMined(context.Background(), client, withdrawTx); err != nil {
		return nil, fmt.Errorf("waiting for withdraw receipt failed: %w", err)
	}

	// JS: return proof.statement[1 + proof.numberOfInputs * 3]
	idx := 1 + result.NumberOfInputs*3
	if idx >= len(result.Statement) {
		return nil, fmt.Errorf("statement too short: need index %d, got length %d",
			idx, len(result.Statement))
	}
	return result.Statement[idx], nil
}

// DepositErc721 approves the vault to transfer an ERC-721 NFT, then deposits it.
// Returns the commitment values emitted by the "Commitment" event.
// Corresponds to: depositErc721(account, nft_id, depositKey, erc721VaultContract, erc721Contract, merkleTree)
func DepositErc721(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	erc721ABI abi.ABI,
	erc721Addr common.Address,
	nftID *big.Int,
	depositPublicKey *big.Int,
) ([]*big.Int, error) {
	// Step 1: erc721.approve(vaultAddr, nftID)
	erc721Contract := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)
	approveTx, err := erc721Contract.Transact(auth, "approve", vaultAddr, nftID)
	if err != nil {
		return nil, fmt.Errorf("approve failed: %w", err)
	}
	approveReceipt, err := bind.WaitMined(context.Background(), client, approveTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for approve receipt failed: %w", err)
	}
	fmt.Printf("...approves the transfer of NFT to ZkDvp\n")
	fmt.Printf("gasUsed: %d\n", approveReceipt.GasUsed)

	// Step 2: vault.deposit([nftID, depositPublicKey])
	params := []*big.Int{nftID, depositPublicKey}
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	depositTx, err := vaultContract.Transact(auth, "deposit", params)
	if err != nil {
		return nil, fmt.Errorf("deposit failed: %w", err)
	}
	depositReceipt, err := bind.WaitMined(context.Background(), client, depositTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for deposit receipt failed: %w", err)
	}

	return getCommitmentsFromTx(vaultABI, depositReceipt)
}

// DepositErc1155 approves the vault as operator for an ERC-1155 token, then deposits it.
// If the contract reverts with a "FungibilityMerkle" custom error, the error is logged
// and nil is returned (matching the JS try/catch behaviour).
// Corresponds to: depositErc1155(account, tokenId, amount, data, depositKey,
//
//	erc1155VaultContract, erc1155Contract)
func DepositErc1155(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	erc1155ABI abi.ABI,
	erc1155Addr common.Address,
	tokenID *big.Int,
	amount *big.Int,
	depositPublicKey *big.Int,
) ([]*big.Int, error) {
	// Step 1: erc1155.setApprovalForAll(vaultAddr, true)
	erc1155Contract := bind.NewBoundContract(erc1155Addr, erc1155ABI, client, client, client)
	approveTx, err := erc1155Contract.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		return nil, fmt.Errorf("setApprovalForAll failed: %w", err)
	}
	if _, err := bind.WaitMined(context.Background(), client, approveTx); err != nil {
		return nil, fmt.Errorf("waiting for setApprovalForAll receipt failed: %w", err)
	}
	fmt.Printf("...approved the transfer of Erc1155 to ZkDvp\n")

	// Step 2: vault.deposit([amount, tokenID, depositPublicKey])
	params := []*big.Int{amount, tokenID, depositPublicKey}
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	depositTx, err := vaultContract.Transact(auth, "deposit", params)
	if err != nil {
		// Mirror JS try/catch: parse and log the FungibilityMerkle custom error,
		// then return nil (no error propagated) matching the JS behaviour.
		// Note: go-ethereum encodes revert data differently from JS err.error;
		// ParseCustomError may not match on all clients.
		errorText := web3.ParseCustomError(err.Error(), "FungibilityMerkle")
		fmt.Printf("Error Text: %s\n", errorText)
		return nil, nil //nolint:nilerr
	}
	depositReceipt, err := bind.WaitMined(context.Background(), client, depositTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for deposit receipt failed: %w", err)
	}

	return getCommitmentsFromTx(vaultABI, depositReceipt)
}

// DepositErc1155Batch approves the vault as operator and batch-deposits ERC-1155 tokens.
// The deposit params are packed as: [...tokenIds, ...amounts, ...publicKeys].
// Corresponds to: depositErc1155Batch(account, tokenIds, amounts, data, depositKeys,
//
//	erc1155VaultContract, erc1155Contract)
func DepositErc1155Batch(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	erc1155ABI abi.ABI,
	erc1155Addr common.Address,
	tokenIDs []*big.Int,
	amounts []*big.Int,
	depositPublicKeys []*big.Int,
) ([]*big.Int, error) {
	// Step 1: erc1155.setApprovalForAll(vaultAddr, true)
	erc1155Contract := bind.NewBoundContract(erc1155Addr, erc1155ABI, client, client, client)
	approveTx, err := erc1155Contract.Transact(auth, "setApprovalForAll", vaultAddr, true)
	if err != nil {
		return nil, fmt.Errorf("setApprovalForAll failed: %w", err)
	}
	if _, err := bind.WaitMined(context.Background(), client, approveTx); err != nil {
		return nil, fmt.Errorf("waiting for setApprovalForAll receipt failed: %w", err)
	}
	fmt.Printf("...approves the BatchTransfer of Erc1155 to ZkDvp\n")

	// Pack params as JS: params = tokenIds.concat(amounts).concat(publickeys)
	params := make([]*big.Int, 0, len(tokenIDs)+len(amounts)+len(depositPublicKeys))
	params = append(params, tokenIDs...)
	params = append(params, amounts...)
	params = append(params, depositPublicKeys...)

	fmt.Printf("tokenIds: %v amounts: %v publicKeys: %v\n", tokenIDs, amounts, depositPublicKeys)

	// Step 2: vault.deposit(params)
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	depositTx, err := vaultContract.Transact(auth, "deposit", params)
	if err != nil {
		return nil, fmt.Errorf("deposit failed: %w", err)
	}
	depositReceipt, err := bind.WaitMined(context.Background(), client, depositTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for deposit receipt failed: %w", err)
	}

	return getCommitmentsFromTx(vaultABI, depositReceipt)
}

// --- Withdrawal functions ---

// WithdrawERC1155 generates an ERC-1155 proof (fungible or non-fungible) and withdraws.
// Returns statement[4], the output commitment (matches JS proof.commitment).
// Corresponds to: withdrawERC1155(account, amount, tokenId, withdrawKey, vaultContract,
//
//	erc1155Contract, merkleDepth, merkleProof, merkleRoot, treeNumber,
//	assetGroup_merkleRoot, assetGroup_merkleProof, isFungible)
func WithdrawERC1155(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	gnarkClient *core.GnarkClient,
	amount *big.Int,
	tokenID *big.Int,
	withdrawKey core.KeyPair,
	accountAddr common.Address,
	erc1155Addr common.Address,
	merkleDepth int,
	merkleProof *core.MerkleProof,
	stTreeNumber *big.Int,
	assetGroupTreeNumber *big.Int,
	assetGroupMerkleProof *core.MerkleProof,
	isFungible bool,
) (*big.Int, error) {
	addrBig := addressToBigInt(erc1155Addr)
	outKey := core.KeyPair{PublicKey: addressToBigInt(accountAddr)}

	var result *core.ProofResult
	var err error

	if isFungible {
		result, err = gnarkClient.Erc1155FungibleJoinSplitProof(
			big.NewInt(0),
			[]*big.Int{amount},
			[]core.KeyPair{withdrawKey},
			[]*big.Int{amount},
			[]core.KeyPair{outKey},
			merkleDepth,
			[]*core.MerkleProof{merkleProof},
			[]*big.Int{stTreeNumber},
			addrBig,
			tokenID,
			assetGroupTreeNumber,
			assetGroupMerkleProof,
		)
	} else {
		result, err = gnarkClient.Erc1155NonFungibleOwnershipProof(
			big.NewInt(0),
			amount,
			withdrawKey,
			outKey,
			merkleDepth,
			merkleProof,
			stTreeNumber,
			addrBig,
			tokenID,
			assetGroupTreeNumber,
			assetGroupMerkleProof,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("generateSingleErc1155Proof failed: %w", err)
	}

	proofReceipt := buildProofReceipt(result)

	// vault.withdraw([amount, tokenId], accountAddr, proof)
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	withdrawTx, err := vaultContract.Transact(auth, "withdraw",
		[]*big.Int{amount, tokenID}, accountAddr, proofReceipt)
	if err != nil {
		return nil, fmt.Errorf("withdraw failed: %w", err)
	}
	withdrawReceipt, err := bind.WaitMined(context.Background(), client, withdrawTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for withdraw receipt failed: %w", err)
	}
	fmt.Printf("gasUsed: %d\n", withdrawReceipt.GasUsed)

	// JS: return proof.commitment = statement[4] (commitmentOut)
	if len(result.Statement) <= 4 {
		return nil, fmt.Errorf("statement too short: need index 4, got length %d",
			len(result.Statement))
	}
	return result.Statement[4], nil
}

// WithdrawErc721 generates an ERC-721 ownership proof and withdraws the NFT from the vault.
// Returns statement[4], the output commitment (matches JS proof.commitment).
// Corresponds to: withdrawErc721(account, nft_id, withdrawKey, erc721VaultContract,
//
//	erc721Contract, merkleDepth, merkleProof, merkleRoot, treeNumber)
func WithdrawErc721(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	vaultABI abi.ABI,
	vaultAddr common.Address,
	gnarkClient *core.GnarkClient,
	nftID *big.Int,
	withdrawKey core.KeyPair,
	accountAddr common.Address,
	erc721Addr common.Address,
	merkleDepth int,
	merkleProof *core.MerkleProof,
	stTreeNumber *big.Int,
) (*big.Int, error) {
	outKey := core.KeyPair{PublicKey: addressToBigInt(accountAddr)}

	// JS computes uid = erc721UniqueId(erc721Address, nft_id) externally, then passes uid.
	// Go's Erc721OwnershipProof takes the raw nftID and computes uniqueId internally.
	result, err := gnarkClient.Erc721OwnershipProof(
		big.NewInt(0),
		nftID,
		withdrawKey,
		outKey,
		merkleDepth,
		merkleProof,
		stTreeNumber,
		addressToBigInt(erc721Addr),
	)
	if err != nil {
		return nil, fmt.Errorf("generateOwnershipProof failed: %w", err)
	}

	proofReceipt := buildProofReceipt(result)

	// vault.withdraw([nftID], accountAddr, proof)
	vaultContract := bind.NewBoundContract(vaultAddr, vaultABI, client, client, client)
	withdrawTx, err := vaultContract.Transact(auth, "withdraw",
		[]*big.Int{nftID}, accountAddr, proofReceipt)
	if err != nil {
		return nil, fmt.Errorf("withdraw failed: %w", err)
	}
	withdrawReceipt, err := bind.WaitMined(context.Background(), client, withdrawTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for withdraw receipt failed: %w", err)
	}
	fmt.Printf("gasUsed: %d\n", withdrawReceipt.GasUsed)

	// JS: return proof.commitment = statement[4] (commitmentOut)
	if len(result.Statement) <= 4 {
		return nil, fmt.Errorf("statement too short: need index 4, got length %d",
			len(result.Statement))
	}
	return result.Statement[4], nil
}

// withdrawERC1155Batch is unexported because the JS source did not export it.
// NOTE: the JS source has undefined-variable bugs (references amount, withdrawKey,
// merkleProof, merkleRoot, treeNumber, tokenId which are not in scope). This Go
// function documents the intended signature and returns an error until the bugs
// are resolved upstream.
func withdrawERC1155Batch(
	_ *ethclient.Client,
	_ *bind.TransactOpts,
	_ abi.ABI,
	_ common.Address,
	_ *core.GnarkClient,
	_ []*big.Int, // amounts
	_ []*big.Int, // tokenIds
	_ []core.KeyPair, // withdrawKeys
	_ common.Address, // accountAddr
	_ common.Address, // erc1155Addr
	_ int, // merkleDepth
	_ []*core.MerkleProof,
	_ []*big.Int, // stTreeNumbers
	_ []*big.Int, // assetGroupTreeNumbers
	_ []*core.MerkleProof,
) (*big.Int, error) {
	return nil, fmt.Errorf("withdrawERC1155Batch: not implemented (JS source has undefined-variable bugs)")
}

// --- Ownership check ---

// CheckOwnership returns true if accountAddr owns the given ERC-721 token.
// Corresponds to: checkOwnership(account, nft_id, erc721Contract)
func CheckOwnership(
	client *ethclient.Client,
	erc721ABI abi.ABI,
	erc721Addr common.Address,
	accountAddr common.Address,
	nftID *big.Int,
) (bool, error) {
	erc721Contract := bind.NewBoundContract(erc721Addr, erc721ABI, client, client, client)

	var out []interface{}
	if err := erc721Contract.Call(nil, &out, "ownerOf", nftID); err != nil {
		fmt.Printf("error in CheckOwnership: %v\n", err)
		return false, nil
	}

	tokenOwner, ok := out[0].(common.Address)
	if !ok {
		return false, fmt.Errorf("unexpected ownerOf return type")
	}

	fmt.Printf("nft %s belongs to %s %s\n", nftID.String(), tokenOwner.Hex(), accountAddr.Hex())
	return tokenOwner == accountAddr, nil
}

// --- Proof generation wrappers ---

// MixErc20 generates a JoinSplit proof to re-randomize keys without an on-chain call.
// If fewer than 2 inputs are provided, a dummy key pair is appended to reach 2 inputs.
// NOTE: the JS source has a parameter-ordering bug (passes erc20Address as treeDepth
// and merkleTree as proofs). The Go signature requires correct argument types.
// Corresponds to: mixErc20(account, inAmounts, inKeys, outAmounts, outKeys,
//
//	zkDvpContract, erc20Contract, merkleTree)
func MixErc20(
	gnarkClient *core.GnarkClient,
	inAmounts []*big.Int,
	inKeys []core.KeyPair,
	outAmounts []*big.Int,
	outKeys []core.KeyPair,
	erc20Addr common.Address,
	merkleDepth int,
	merkleProofs []*core.MerkleProof,
	stTreeNumbers []*big.Int,
	use10_2 bool,
) (*core.ProofResult, error) {
	erc20AddrBig := addressToBigInt(erc20Addr)

	if len(inAmounts) < 2 || len(inKeys) < 2 {
		dummyPriv, dummyPub, err := core.NewKeyPair()
		if err != nil {
			return nil, fmt.Errorf("failed to generate dummy key pair: %w", err)
		}
		dummyKey := core.KeyPair{PrivateKey: dummyPriv, PublicKey: dummyPub}

		return gnarkClient.Erc20JoinSplitProof(
			big.NewInt(0),
			[]*big.Int{inAmounts[0], big.NewInt(0)},
			[]core.KeyPair{inKeys[0], dummyKey},
			[]*big.Int{inAmounts[0], big.NewInt(0)},
			[]core.KeyPair{outKeys[0], dummyKey},
			merkleDepth,
			merkleProofs,
			stTreeNumbers,
			erc20AddrBig,
			use10_2,
		)
	}

	return gnarkClient.Erc20JoinSplitProof(
		big.NewInt(0),
		inAmounts,
		inKeys,
		outAmounts,
		outKeys,
		merkleDepth,
		merkleProofs,
		stTreeNumbers,
		erc20AddrBig,
		use10_2,
	)
}

// GenerateErc20JoinSplitProof generates an ERC-20 JoinSplit proof.
// Corresponds to: generateErc20JoinSplitProof(nftCommitment, inAmounts, inKeys,
//
//	outAmounts, outKeys, treeDepth, proofs, roots, treeNumbers, erc20ContractAddress)
func GenerateErc20JoinSplitProof(
	gnarkClient *core.GnarkClient,
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []core.KeyPair,
	wtValuesOut []*big.Int,
	keysOut []core.KeyPair,
	merkleDepth int,
	merkleProofs []*core.MerkleProof,
	stTreeNumbers []*big.Int,
	erc20ContractAddr common.Address,
	use10_2 bool,
) (*core.ProofResult, error) {
	return gnarkClient.Erc20JoinSplitProof(
		stMessage,
		wtValuesIn,
		keysIn,
		wtValuesOut,
		keysOut,
		merkleDepth,
		merkleProofs,
		stTreeNumbers,
		addressToBigInt(erc20ContractAddr),
		use10_2,
	)
}

// GenerateOwnershipProof generates an ERC-721 ownership proof.
// Takes the raw tokenId (nftID); Erc721OwnershipProof computes the uniqueId internally.
// Note: the JS computed uniqueId = erc721UniqueId(address, tokenId) externally and
// passed it to the prover directly; Go internalises that step.
// Corresponds to: generateOwnershipProof(paymentCommitment, uid, inKey, outKey,
//
//	merkleDepth, merkleProof, merkleRoot, treeNumber)
func GenerateOwnershipProof(
	gnarkClient *core.GnarkClient,
	stMessage *big.Int,
	nftID *big.Int,
	keyIn core.KeyPair,
	keyOut core.KeyPair,
	merkleDepth int,
	merkleProof *core.MerkleProof,
	stTreeNumber *big.Int,
	erc721ContractAddr common.Address,
) (*core.ProofResult, error) {
	return gnarkClient.Erc721OwnershipProof(
		stMessage,
		nftID,
		keyIn,
		keyOut,
		merkleDepth,
		merkleProof,
		stTreeNumber,
		addressToBigInt(erc721ContractAddr),
	)
}

// GenerateSingleErc1155Proof generates a single ERC-1155 proof (fungible or non-fungible).
// Corresponds to: generateSingleErc1155Proof(message, amountOrOne, inKey, outKey,
//
//	merkleDepth, merkleProof, merkleRoot, treeNumber, erc1155ContractAddress, erc1155TokenId,
//	assetGroup_treeNumber, assetGroup_merkleProof, isFungible)
func GenerateSingleErc1155Proof(
	gnarkClient *core.GnarkClient,
	stMessage *big.Int,
	amountOrOne *big.Int,
	keyIn core.KeyPair,
	keyOut core.KeyPair,
	merkleDepth int,
	merkleProof *core.MerkleProof,
	stTreeNumber *big.Int,
	erc1155ContractAddr common.Address,
	erc1155TokenID *big.Int,
	assetGroupTreeNumber *big.Int,
	assetGroupMerkleProof *core.MerkleProof,
	isFungible bool,
) (*core.ProofResult, error) {
	addrBig := addressToBigInt(erc1155ContractAddr)

	if isFungible {
		return gnarkClient.Erc1155FungibleJoinSplitProof(
			stMessage,
			[]*big.Int{amountOrOne},
			[]core.KeyPair{keyIn},
			[]*big.Int{amountOrOne},
			[]core.KeyPair{keyOut},
			merkleDepth,
			[]*core.MerkleProof{merkleProof},
			[]*big.Int{stTreeNumber},
			addrBig,
			erc1155TokenID,
			assetGroupTreeNumber,
			assetGroupMerkleProof,
		)
	}

	return gnarkClient.Erc1155NonFungibleOwnershipProof(
		stMessage,
		amountOrOne,
		keyIn,
		keyOut,
		merkleDepth,
		merkleProof,
		stTreeNumber,
		addrBig,
		erc1155TokenID,
		assetGroupTreeNumber,
		assetGroupMerkleProof,
	)
}

// GenerateErc1155JoinSplitProof generates a fungible ERC-1155 JoinSplit proof.
// Corresponds to: generateErc1155JoinSplitProof(message, inAmounts, inKeys,
//
//	outAmounts, outKeys, merkleDepth, merkleProofs, merkleRoots, treeNumbers,
//	erc1155ContractAddress, erc1155TokenId, fungibilityMerkleTreeNumber, fungibilityMerkleProof)
func GenerateErc1155JoinSplitProof(
	gnarkClient *core.GnarkClient,
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []core.KeyPair,
	wtValuesOut []*big.Int,
	keysOut []core.KeyPair,
	merkleDepth int,
	merkleProofs []*core.MerkleProof,
	stTreeNumbers []*big.Int,
	erc1155ContractAddr common.Address,
	erc1155TokenID *big.Int,
	assetGroupTreeNumber *big.Int,
	assetGroupMerkleProof *core.MerkleProof,
) (*core.ProofResult, error) {
	return gnarkClient.Erc1155FungibleJoinSplitProof(
		stMessage,
		wtValuesIn,
		keysIn,
		wtValuesOut,
		keysOut,
		merkleDepth,
		merkleProofs,
		stTreeNumbers,
		addressToBigInt(erc1155ContractAddr),
		erc1155TokenID,
		assetGroupTreeNumber,
		assetGroupMerkleProof,
	)
}

// GenerateErc1155BatchProof generates an ERC-1155 batch proof.
// NOTE: the underlying gnark client method (Erc1155BatchProof) is not yet ported to Go.
// Corresponds to: generateErc1155BatchProof(message, amounts, inKeys, outKeys,
//
//	merkleDepth, merkleProofs, merkleRoots, treeNumbers, erc1155ContractAddress,
//	erc1155TokenIds, assetGroup_treeNumbers, assetGroup_merkleProofs)
func GenerateErc1155BatchProof(
	_ *core.GnarkClient,
	_ *big.Int, // stMessage
	_ []*big.Int, // amounts
	_ []core.KeyPair, // keysIn
	_ []core.KeyPair, // keysOut
	_ int, // merkleDepth
	_ []*core.MerkleProof,
	_ []*big.Int, // stTreeNumbers
	_ common.Address, // erc1155ContractAddr
	_ []*big.Int, // erc1155TokenIDs
	_ []*big.Int, // assetGroupTreeNumbers
	_ []*core.MerkleProof,
) (*core.ProofResult, error) {
	// TODO: implement when Erc1155BatchProof is ported to the gnark client.
	return nil, fmt.Errorf("GenerateErc1155BatchProof: Erc1155BatchProof not yet implemented in Go")
}

// WithdrawEnygma generates an ERC-20 JoinSplit proof for an Enygma withdrawal.
// Corresponds to: withdrawEnygma(publicKey, amount, withdrawKey, enygmaAddress,
//
//	commitmentProof, merkleTree)
func WithdrawEnygma(
	gnarkClient *core.GnarkClient,
	publicKey *big.Int,
	amount *big.Int,
	withdrawKey core.KeyPair,
	enygmaAddr common.Address,
	commitmentProof *core.MerkleProof,
	merkleDepth int,
	stTreeNumber *big.Int,
	use10_2 bool,
) (*core.ProofResult, error) {
	dummyPriv, dummyPub, err := core.NewKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate dummy key pair: %w", err)
	}
	dummyKey := core.KeyPair{PrivateKey: dummyPriv, PublicKey: dummyPub}

	// JS: generateErc20JoinSplitProof(0n,
	//   [amount, 0n], [withdrawKey, dummyKey],
	//   [amount, 0n], [{publicKey: BigInt(publicKey)}, dummyKey],
	//   merkleTree.depth, [commitmentProof, 0n],
	//   [merkleTree.tree[depth][0], 0n], [BigInt(merkleTree.treeNumber), 0n], enygmaAddress)
	return gnarkClient.Erc20JoinSplitProof(
		big.NewInt(0),
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{withdrawKey, dummyKey},
		[]*big.Int{amount, big.NewInt(0)},
		[]core.KeyPair{{PublicKey: publicKey}, dummyKey},
		merkleDepth,
		[]*core.MerkleProof{commitmentProof, nil}, // nil safe: dummy input has value=0
		[]*big.Int{stTreeNumber, big.NewInt(0)},
		addressToBigInt(enygmaAddr),
		use10_2,
	)
}

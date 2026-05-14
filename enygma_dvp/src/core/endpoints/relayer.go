package endpoints


/*
Port of src/core/endpoints/relayer.js

Relayer-level operations: submitting proofs and orchestrating swaps/exchanges
on the EnygmaDvp contract.

Note on deprecated functions:
  - SendMixFundsToChain calls dvp.mixFunds(), which no longer exists in the
    current EnygmaDvp ABI. The function is preserved for backward compatibility.
  - SendUnspentErc20 calls dvp.submitUnspentErc20(), which also no longer
    exists in the current ABI. It was defined in the JS but not exported.
  - getCommitmentFromTx looks for a "Commitment" event that is absent from the
    current ABI; it is included for completeness but will error on current contracts.

JS mapping:
  - JS `contract.connect(relayer)` → Go: pass `auth *bind.TransactOpts` + `client`
  - JS `tx.wait()` → Go: `bind.WaitMined(...)`
  - JS `receipt.events.find(...)` → Go: scan `receipt.Logs` with parsed ABI
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

// --- Solidity struct types ---
// These must match the IEnygmaDvp ABI tuple definitions exactly so that
// go-ethereum's reflection-based ABI encoder produces the correct calldata.

// G1Point matches Solidity: struct IEnygmaDvp.G1Point { uint256 x; uint256 y; }
type G1Point struct {
	X *big.Int `abi:"x"`
	Y *big.Int `abi:"y"`
}

// G2Point matches Solidity: struct IEnygmaDvp.G2Point { uint256[2] x; uint256[2] y; }
type G2Point struct {
	X [2]*big.Int `abi:"x"`
	Y [2]*big.Int `abi:"y"`
}

// SnarkProof matches Solidity: struct IEnygmaDvp.SnarkProof { G1Point a; G2Point b; G1Point c; }
type SnarkProof struct {
	A G1Point `abi:"a"`
	B G2Point `abi:"b"`
	C G1Point `abi:"c"`
}

// ProofReceipt matches Solidity: struct IEnygmaDvp.ProofReceipt {
//   SnarkProof proof; uint256[] statement; uint256 numberOfInputs; uint256 numberOfOutputs;
// }
type ProofReceipt struct {
	Proof           SnarkProof `abi:"proof"`
	Statement       []*big.Int `abi:"statement"`
	NumberOfInputs  *big.Int   `abi:"numberOfInputs"`
	NumberOfOutputs *big.Int   `abi:"numberOfOutputs"`
}

// --- Internal helpers ---

// relayTransact submits a contract call and waits for the receipt.
// Shared by all relayer functions — same pattern as admin.go's transact helper
// but kept private to this file to avoid a duplicate-symbol issue.
func relayTransact(
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

// getCommitmentFromTx scans a transaction receipt for a "Commitment" event and
// returns the commitment value.
// NOTE: the "Commitment" event is absent from the current EnygmaDvp ABI; this
// function is a port of the JS helper for backward compatibility with older
// contract versions.
func getCommitmentFromTx(contractABI abi.ABI, receipt *types.Receipt) (*big.Int, error) {
	fmt.Printf("Gas used: %d\n", receipt.GasUsed)

	event, ok := contractABI.Events["Commitment"]
	if !ok {
		return nil, fmt.Errorf("Commitment event not found in ABI (may have been renamed in current contract version)")
	}

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

		if commitment, ok := values["commitment"].(*big.Int); ok {
			return commitment, nil
		}
	}

	return nil, fmt.Errorf("Commitment event not found in transaction logs")
}

// --- Exported relayer functions ---

// SendMixFundsToChain submits a mixFunds proof to the EnygmaDvp contract and
// returns the commitments from the proof.
// DEPRECATED: dvp.mixFunds() no longer exists in the current EnygmaDvp ABI.
// commitments is passed explicitly because the JS proof object carried a
// non-standard .commitments field that is not part of ProofReceipt.
func SendMixFundsToChain(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	receipt ProofReceipt,
	commitments []*big.Int,
) ([]*big.Int, error) {
	_, err := relayTransact(client, auth, contractABI, contractAddr, "mixFunds", receipt)
	if err != nil {
		return nil, err
	}
	return commitments, nil
}

// SendUnspentErc20 submits an unspent ERC-20 proof to the EnygmaDvp contract.
// DEPRECATED: dvp.submitUnspentErc20() no longer exists in the current ABI.
// This function was defined in the JS but not exported from the module.
func SendUnspentErc20(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	receipt ProofReceipt,
) error {
	_, err := relayTransact(client, auth, contractABI, contractAddr, "submitUnspentErc20", receipt)
	return err
}

// SubmitPartialSettlement submits a partial settlement proof to the EnygmaDvp contract.
// Corresponds to: dvpRelayer.submitPartialSettlement(proof, vaultId, groupId)
func SubmitPartialSettlement(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	receipt ProofReceipt,
	vaultId *big.Int,
	groupId *big.Int,
) (*types.Receipt, error) {
	return relayTransact(client, auth, contractABI, contractAddr,
		"submitPartialSettlement", receipt, vaultId, groupId)
}

// Swap executes a payment-vs-delivery swap on the EnygmaDvp contract.
// Returns the three output commitments that identify the new on-chain notes:
//   [paymentReceipt.Statement[7], paymentReceipt.Statement[8], deliveryReceipt.Statement[4]]
//
// Corresponds to: dvpRelayer.swap(paymentReceipt, deliveryReceipt, paymentVaultId, deliveryVaultId)
func Swap(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	paymentReceipt ProofReceipt,
	deliveryReceipt ProofReceipt,
	paymentVaultId *big.Int,
	deliveryVaultId *big.Int,
) ([]*big.Int, error) {
	_, err := relayTransact(client, auth, contractABI, contractAddr,
		"swap", paymentReceipt, deliveryReceipt, paymentVaultId, deliveryVaultId)
	if err != nil {
		return nil, err
	}

	if len(paymentReceipt.Statement) <= 8 {
		return nil, fmt.Errorf("paymentReceipt.Statement too short: need index 8, got length %d",
			len(paymentReceipt.Statement))
	}
	if len(deliveryReceipt.Statement) <= 4 {
		return nil, fmt.Errorf("deliveryReceipt.Statement too short: need index 4, got length %d",
			len(deliveryReceipt.Statement))
	}

	commitments := []*big.Int{
		paymentReceipt.Statement[7],
		paymentReceipt.Statement[8],
		deliveryReceipt.Statement[4],
	}
	return commitments, nil
}

// SubmitPayment submits a Payment circuit proof to the EnygmaDvp.payment() function.
// ctxt and encTxData are Bob's ML-KEM capsule and AES-GCM ciphertext (output 0 only);
// Alice's change ciphertext is not published — she holds saltA locally.
func SubmitPayment(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	receipt ProofReceipt,
	vaultId *big.Int,
	ctxt []byte,
	encTxData []byte,
) (*types.Receipt, error) {
	return relayTransact(client, auth, contractABI, contractAddr,
		"payment", receipt, vaultId, ctxt, encTxData)
}

// Exchange executes a payment-vs-payment exchange on the EnygmaDvp contract.
// Returns the three output commitments:
//   [paymentReceipt1.Statement[7], paymentReceipt1.Statement[8], paymentReceipt2.Statement[4]]
//
// Corresponds to: dvpRelayer.exchange(paymentReceipt1, paymentReceipt2, paymentVaultId1, paymentVaultId2)
func Exchange(
	client *ethclient.Client,
	auth *bind.TransactOpts,
	contractABI abi.ABI,
	contractAddr common.Address,
	paymentReceipt1 ProofReceipt,
	paymentReceipt2 ProofReceipt,
	paymentVaultId1 *big.Int,
	paymentVaultId2 *big.Int,
) ([]*big.Int, error) {
	_, err := relayTransact(client, auth, contractABI, contractAddr,
		"exchange", paymentReceipt1, paymentReceipt2, paymentVaultId1, paymentVaultId2)
	if err != nil {
		return nil, err
	}

	if len(paymentReceipt1.Statement) <= 8 {
		return nil, fmt.Errorf("paymentReceipt1.Statement too short: need index 8, got length %d",
			len(paymentReceipt1.Statement))
	}
	if len(paymentReceipt2.Statement) <= 4 {
		return nil, fmt.Errorf("paymentReceipt2.Statement too short: need index 4, got length %d",
			len(paymentReceipt2.Statement))
	}

	commitments := []*big.Int{
		paymentReceipt1.Statement[7],
		paymentReceipt1.Statement[8],
		paymentReceipt2.Statement[4],
	}
	return commitments, nil
}

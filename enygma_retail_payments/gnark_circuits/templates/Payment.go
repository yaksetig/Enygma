package templates

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/math/cmp"
	"gnark_server/primitives"
)

// PaymentCircuitConfig holds the compile-time parameters for the Payment circuit.
type PaymentCircuitConfig struct {
	TmNInputs       int // number of input notes Alice spends (>= 1)
	TmMOutputs      int // number of output notes created (>= 2: at least payment + change)
	TmMerkleTreeDepth int
	TmRange         frontend.Variable // upper bound for range checks (e.g. 2^64)
}

type PaymentCircuit struct {
	Config PaymentCircuitConfig

	// --- public inputs (statement) ---
	// Layout (non-interleaved, matching ContractStatement):
	//   [StMessage, StTreeNumbers[0..N-1], StMerkleRoots[0..N-1],
	//    StNullifiers[0..N-1], StCommitmentsOut[0..M-1]]
	StMessage        frontend.Variable   `gnark:",public"` // domain-separation / DVP link (0 = standalone)
	StTreeNumbers    []frontend.Variable `gnark:",public"` // TmNInputs — sub-tree index per input
	StMerkleRoots    []frontend.Variable `gnark:",public"` // TmNInputs — Merkle root per input
	StNullifiers     []frontend.Variable `gnark:",public"` // TmNInputs — nf[i] = Poseidon(sk[i], leafIndex[i])
	StCommitmentsOut []frontend.Variable `gnark:",public"` // TmMOutputs — output commitment per output

	// --- private witnesses: inputs ---
	WtPrivateKeysIn []frontend.Variable   // TmNInputs — sk_spend per input
	WtValuesIn      []frontend.Variable   // TmNInputs — amount per input
	WtSaltsIn       []frontend.Variable   // TmNInputs — saltIn[i] (from when Alice received this note)
	WtPathElements  [][]frontend.Variable // TmNInputs x TmMerkleTreeDepth
	WtPathIndices   []frontend.Variable   // TmNInputs — leaf position per input

	// Shared across all inputs and outputs (single token per proof)
	WtTokenId frontend.Variable

	// --- private witnesses: outputs ---
	WtSpendPublicKeysOut []frontend.Variable // TmMOutputs — pk_spend of each recipient
	WtValuesOut          []frontend.Variable // TmMOutputs — amount per output
	WtSaltsOut           []frontend.Variable // TmMOutputs — salt per output (HKDF-derived for output 0; random for change outputs)
}

func (circuit *PaymentCircuit) Define(api frontend.API) error {
	inputsTotal := frontend.Variable(0)
	outputsTotal := frontend.Variable(0)

	// --- verify input notes ---
	for i := 0; i < circuit.Config.TmNInputs; i++ {
		// Range check: 0 <= valueIn[i] < TmRange
		isValid0 := cmp.IsLess(api, circuit.WtValuesIn[i], circuit.Config.TmRange)
		api.AssertIsEqual(isValid0, 1)
		isValid1 := cmp.IsLessOrEqual(api, 0, circuit.WtValuesIn[i])
		api.AssertIsEqual(isValid1, 1)

		// Derive pk_spend from sk_spend inside the circuit
		pkIn := primitives.PublicKey(api, circuit.WtPrivateKeysIn[i])

		// Nullifier: nf[i] = Poseidon(sk[i], leafIndex[i])
		nullifier := primitives.Nullifier(api, circuit.WtPrivateKeysIn[i], circuit.WtPathIndices[i])
		api.AssertIsEqual(nullifier, circuit.StNullifiers[i])

		// Input commitment: Poseidon(pk[i], saltIn[i], valueIn[i], tokenId)
		commitment := primitives.Erc20CommitmentV2(api,
			pkIn,
			circuit.WtSaltsIn[i],
			circuit.WtValuesIn[i],
			circuit.WtTokenId,
		)

		// Merkle proof: enforce root match only when value > 0 (zero-value inputs are ignored).
		pathElements := make([]frontend.Variable, circuit.Config.TmMerkleTreeDepth)
		for j := 0; j < circuit.Config.TmMerkleTreeDepth; j++ {
			pathElements[j] = circuit.WtPathElements[i][j]
		}
		root := primitives.MerkleProof(api, commitment, circuit.WtPathIndices[i], pathElements)
		isZero := api.IsZero(circuit.WtValuesIn[i])
		enable := api.Sub(1, isZero)
		diff := api.Sub(circuit.StMerkleRoots[i], root)
		api.AssertIsEqual(api.Mul(diff, enable), 0)

		inputsTotal = api.Add(inputsTotal, circuit.WtValuesIn[i])
	}

	// --- verify output notes ---
	for j := 0; j < circuit.Config.TmMOutputs; j++ {
		// Range check: 0 <= valueOut[j] < TmRange
		isValid0 := cmp.IsLess(api, circuit.WtValuesOut[j], circuit.Config.TmRange)
		api.AssertIsEqual(isValid0, 1)
		isValid1 := cmp.IsLessOrEqual(api, 0, circuit.WtValuesOut[j])
		api.AssertIsEqual(isValid1, 1)

		// Output commitment: Poseidon(pkOut[j], saltOut[j], valueOut[j], tokenId)
		commitment := primitives.Erc20CommitmentV2(api,
			circuit.WtSpendPublicKeysOut[j],
			circuit.WtSaltsOut[j],
			circuit.WtValuesOut[j],
			circuit.WtTokenId,
		)
		api.AssertIsEqual(commitment, circuit.StCommitmentsOut[j])

		outputsTotal = api.Add(outputsTotal, circuit.WtValuesOut[j])
	}

	// Conservation: total inputs must equal total outputs
	api.AssertIsEqual(outputsTotal, inputsTotal)

	return nil
}

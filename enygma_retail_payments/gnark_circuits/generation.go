// Key generation entry point. Run with: go run generation.go
// Note: main.go also declares func main (the gnark server). Both files share
// package main intentionally — run them individually, never with go build ./...
package main

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/constraint/solver"

	"gnark_server/templates"
	"gnark_server/primitives"
	script "gnark_server/scripts"
)

func GenerationVkPk() {
	solver.RegisterHint(primitives.ModHint)
	solver.RegisterHint(primitives.PoseidonNative)
	solver.RegisterHint(primitives.PoseidonPrivateKeyNative)

	payment_config := templates.PaymentCircuitConfig{
		TmNInputs:         1,
		TmMOutputs:        2,
		TmMerkleTreeDepth: 8,
		TmRange:           frontend.Variable("1000000000000000000000000000000000000"),
	}

	script.SetupPayment(payment_config, "Payment")
	// PrivateMint keys are NOT regenerated here — the PrivateMintVerifier contract
	// bytecode has dvp's VK baked in. Copy PrivateMintPK/VK.key from enygma_dvp.
}

func main() {
	GenerationVkPk()
}

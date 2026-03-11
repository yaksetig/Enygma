// export_vk reads a gnark BN254 Groth16 verifying-key (.key) file and writes a
// JSON file whose structure matches IEnygmaDvp.VerifyingKey, suitable for
// registering the VK on-chain via Verifier.addVerificationKey().
//
// Usage:
//
//	go run ./cmd/export_vk/ <vk.key> [output.json]
//
// If output.json is omitted the JSON is printed to stdout.
//
// G2 coordinate convention (EIP-197 / alt_bn128 pairing precompile):
//
//	G2Point.x[0] = imaginary part of X  (gnark: X.A1)
//	G2Point.x[1] = real      part of X  (gnark: X.A0)
//	G2Point.y[0] = imaginary part of Y  (gnark: Y.A1)
//	G2Point.y[1] = real      part of Y  (gnark: Y.A0)
//
// See PrivateMintVerifier.sol lines 41-44 for the authoritative note on this
// ordering.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	groth16bn254 "github.com/consensys/gnark/backend/groth16/bn254"
)

// ---- JSON output types (mirror IEnygmaDvp structs) -------------------------

// G1PointJSON holds a G1 affine point with coordinates as decimal strings.
type G1PointJSON struct {
	X string `json:"x"`
	Y string `json:"y"`
}

// G2PointJSON holds a G2 affine point with Fp2 coordinates.
// Index 0 is the imaginary part (A1), index 1 is the real part (A0) — the
// order expected by the EIP-197 alt_bn128 pairing precompile.
type G2PointJSON struct {
	X [2]string `json:"x"`
	Y [2]string `json:"y"`
}

// VerifyingKeyJSON mirrors IEnygmaDvp.VerifyingKey.
type VerifyingKeyJSON struct {
	Alpha1 G1PointJSON   `json:"alpha1"`
	Beta2  G2PointJSON   `json:"beta2"`
	Gamma2 G2PointJSON   `json:"gamma2"`
	Delta2 G2PointJSON   `json:"delta2"`
	IC     []G1PointJSON `json:"ic"`
}

// ---- helpers ----------------------------------------------------------------

// g1Affine converts a gnark BN254 G1Affine to its JSON representation.
func g1Affine(x, y *big.Int) G1PointJSON {
	return G1PointJSON{
		X: x.String(),
		Y: y.String(),
	}
}

// g2Affine converts a gnark BN254 G2Affine to its JSON representation.
// Per EIP-197: x[0] = imaginary (A1), x[1] = real (A0).
func g2Affine(xA0, xA1, yA0, yA1 *big.Int) G2PointJSON {
	return G2PointJSON{
		X: [2]string{xA1.String(), xA0.String()}, // [imaginary, real]
		Y: [2]string{yA1.String(), yA0.String()},
	}
}

// ---- main -------------------------------------------------------------------

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: export_vk <vk.key> [output.json]")
		os.Exit(1)
	}

	vkPath := os.Args[1]
	var outPath string
	if len(os.Args) >= 3 {
		outPath = os.Args[2]
	}

	// Load the verifying key.
	f, err := os.Open(vkPath)
	if err != nil {
		log.Fatalf("open vk file: %v", err)
	}
	defer f.Close()

	vkIface := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vkIface.ReadFrom(f); err != nil {
		log.Fatalf("read vk: %v", err)
	}

	vk, ok := vkIface.(*groth16bn254.VerifyingKey)
	if !ok {
		log.Fatalf("unexpected VK type %T (expected *groth16bn254.VerifyingKey)", vkIface)
	}

	// Extract alpha1 (G1).
	var axB, ayB big.Int
	vk.G1.Alpha.X.BigInt(&axB)
	vk.G1.Alpha.Y.BigInt(&ayB)

	// Extract beta2, gamma2, delta2 (G2).
	var bxA0, bxA1, byA0, byA1 big.Int
	vk.G2.Beta.X.A0.BigInt(&bxA0)
	vk.G2.Beta.X.A1.BigInt(&bxA1)
	vk.G2.Beta.Y.A0.BigInt(&byA0)
	vk.G2.Beta.Y.A1.BigInt(&byA1)

	var gxA0, gxA1, gyA0, gyA1 big.Int
	vk.G2.Gamma.X.A0.BigInt(&gxA0)
	vk.G2.Gamma.X.A1.BigInt(&gxA1)
	vk.G2.Gamma.Y.A0.BigInt(&gyA0)
	vk.G2.Gamma.Y.A1.BigInt(&gyA1)

	var dxA0, dxA1, dyA0, dyA1 big.Int
	vk.G2.Delta.X.A0.BigInt(&dxA0)
	vk.G2.Delta.X.A1.BigInt(&dxA1)
	vk.G2.Delta.Y.A0.BigInt(&dyA0)
	vk.G2.Delta.Y.A1.BigInt(&dyA1)

	// Extract IC points (G1.K): K[0] = constant term, K[1..n] = public inputs.
	ic := make([]G1PointJSON, len(vk.G1.K))
	for i, pt := range vk.G1.K {
		var kx, ky big.Int
		pt.X.BigInt(&kx)
		pt.Y.BigInt(&ky)
		ic[i] = g1Affine(&kx, &ky)
	}

	out := VerifyingKeyJSON{
		Alpha1: g1Affine(&axB, &ayB),
		Beta2:  g2Affine(&bxA0, &bxA1, &byA0, &byA1),
		Gamma2: g2Affine(&gxA0, &gxA1, &gyA0, &gyA1),
		Delta2: g2Affine(&dxA0, &dxA1, &dyA0, &dyA1),
		IC:     ic,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatalf("marshal JSON: %v", err)
	}

	if outPath == "" {
		fmt.Println(string(data))
		return
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		log.Fatalf("write output: %v", err)
	}
	fmt.Printf("VK exported to %s (%d IC points)\n", outPath, len(ic))
}

// patch_verifier reads the current PrivateMintVK.key, computes the 14 Groth16
// constants that PrivateMintVerifier.sol needs, patches the Solidity source,
// recompiles it with solc, and updates the Hardhat artifact JSON — all in one
// step.
//
// Usage (run from gnark_circuits/):
//
//	go run ./cmd/patch_verifier/
//
// Paths are resolved relative to the project root (one level up from gnark_circuits/).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	groth16bn254 "github.com/consensys/gnark/backend/groth16/bn254"
)

// BN254 base field modulus P
var P, _ = new(big.Int).SetString(
	"21888242871839275222246405745257275088696311157297823662689037894645226208583", 10)

func neg(v *big.Int) *big.Int {
	r := new(big.Int).Mod(v, P)
	if r.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(P, r)
}

func str(v *big.Int) string { return v.String() }

func main() {
	// Paths relative to project root
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	// When run via "go run", the executable is in a temp dir; resolve via cwd instead.
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	_ = exe

	projectRoot := filepath.Join(cwd, "..")
	vkPath := filepath.Join(cwd, "scripts", "keys", "PrivateMintVK.key")
	solPath := filepath.Join(projectRoot, "contracts", "core", "contracts", "PrivateMintVerifier.sol")
	artifactPath := filepath.Join(projectRoot, "artifacts", "contracts", "core", "contracts",
		"PrivateMintVerifier.sol", "PrivateMintVerifier.json")

	// ── 1. Load VK ──────────────────────────────────────────────────────────────
	fmt.Println("Loading VK from", vkPath)
	f, err := os.Open(vkPath)
	if err != nil {
		log.Fatalf("open vk: %v", err)
	}
	defer f.Close()

	vkIface := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vkIface.ReadFrom(f); err != nil {
		log.Fatalf("read vk: %v", err)
	}
	vk, ok := vkIface.(*groth16bn254.VerifyingKey)
	if !ok {
		log.Fatalf("unexpected vk type %T", vkIface)
	}

	// ── 2. Extract coordinates ───────────────────────────────────────────────────
	var (
		alphaX, alphaY                         big.Int
		betaXA0, betaXA1, betaYA0, betaYA1     big.Int
		gammaXA0, gammaXA1, gammaYA0, gammaYA1 big.Int
		deltaXA0, deltaXA1, deltaYA0, deltaYA1 big.Int
	)
	vk.G1.Alpha.X.BigInt(&alphaX)
	vk.G1.Alpha.Y.BigInt(&alphaY)

	vk.G2.Beta.X.A0.BigInt(&betaXA0)
	vk.G2.Beta.X.A1.BigInt(&betaXA1)
	vk.G2.Beta.Y.A0.BigInt(&betaYA0)
	vk.G2.Beta.Y.A1.BigInt(&betaYA1)

	vk.G2.Gamma.X.A0.BigInt(&gammaXA0)
	vk.G2.Gamma.X.A1.BigInt(&gammaXA1)
	vk.G2.Gamma.Y.A0.BigInt(&gammaYA0)
	vk.G2.Gamma.Y.A1.BigInt(&gammaYA1)

	vk.G2.Delta.X.A0.BigInt(&deltaXA0)
	vk.G2.Delta.X.A1.BigInt(&deltaXA1)
	vk.G2.Delta.Y.A0.BigInt(&deltaYA0)
	vk.G2.Delta.Y.A1.BigInt(&deltaYA1)

	// IC: K[0] = constant term, K[1..4] = public inputs
	if len(vk.G1.K) < 5 {
		log.Fatalf("expected at least 5 IC points, got %d", len(vk.G1.K))
	}
	icX := make([]big.Int, 5)
	icY := make([]big.Int, 5)
	for i := 0; i < 5; i++ {
		vk.G1.K[i].X.BigInt(&icX[i])
		vk.G1.K[i].Y.BigInt(&icY[i])
	}

	// ── 3. Build replacement map ─────────────────────────────────────────────────
	// Solidity convention:
	//   _0 suffix = real part (A0)     — NOT negated for X, negated for Y
	//   _1 suffix = imag part (A1)     — NOT negated for X, negated for Y
	//   ALPHA/IC  = G1 points          — not negated at all
	replacements := map[string]string{
		"ALPHA_X": str(&alphaX),
		"ALPHA_Y": str(&alphaY),

		"BETA_NEG_X_0": str(&betaXA0),
		"BETA_NEG_X_1": str(&betaXA1),
		"BETA_NEG_Y_0": str(neg(&betaYA0)),
		"BETA_NEG_Y_1": str(neg(&betaYA1)),

		"GAMMA_NEG_X_0": str(&gammaXA0),
		"GAMMA_NEG_X_1": str(&gammaXA1),
		"GAMMA_NEG_Y_0": str(neg(&gammaYA0)),
		"GAMMA_NEG_Y_1": str(neg(&gammaYA1)),

		"DELTA_NEG_X_0": str(&deltaXA0),
		"DELTA_NEG_X_1": str(&deltaXA1),
		"DELTA_NEG_Y_0": str(neg(&deltaYA0)),
		"DELTA_NEG_Y_1": str(neg(&deltaYA1)),

		"CONSTANT_X": str(&icX[0]),
		"CONSTANT_Y": str(&icY[0]),
		"PUB_0_X":    str(&icX[1]),
		"PUB_0_Y":    str(&icY[1]),
		"PUB_1_X":    str(&icX[2]),
		"PUB_1_Y":    str(&icY[2]),
		"PUB_2_X":    str(&icX[3]),
		"PUB_2_Y":    str(&icY[3]),
		"PUB_3_X":    str(&icX[4]),
		"PUB_3_Y":    str(&icY[4]),
	}

	// ── 4. Patch PrivateMintVerifier.sol ─────────────────────────────────────────
	fmt.Println("Patching", solPath)
	solSrc, err := os.ReadFile(solPath)
	if err != nil {
		log.Fatalf("read sol: %v", err)
	}
	patched := string(solSrc)
	// Match lines like:   uint256 constant FOO_BAR = 12345;
	re := regexp.MustCompile(`(uint256 constant (\w+) = )\d+;`)
	patched = re.ReplaceAllStringFunc(patched, func(line string) string {
		m := re.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		name := m[2]
		if val, ok := replacements[name]; ok {
			return m[1] + val + ";"
		}
		return line
	})
	if err := os.WriteFile(solPath, []byte(patched), 0644); err != nil {
		log.Fatalf("write sol: %v", err)
	}
	fmt.Println("  constants updated")

	// ── 5. Recompile with solc ───────────────────────────────────────────────────
	fmt.Println("Compiling with solc...")
	solcOut, err := exec.Command("solc",
		"--optimize", "--optimize-runs", "1600", "--via-ir",
		"--bin", "--bin-runtime",
		solPath,
	).Output()
	if err != nil {
		log.Fatalf("solc: %v\noutput: %s", err, solcOut)
	}

	// Parse "Binary:\n<hex>" and "Binary of the runtime part:\n<hex>"
	lines := strings.Split(string(solcOut), "\n")
	creation, runtime := parseSolcBinaries(lines)
	if creation == "" || runtime == "" {
		log.Fatalf("could not parse solc output:\n%s", solcOut)
	}
	fmt.Println("  solc ok")

	// ── 6. Update Hardhat artifact JSON ─────────────────────────────────────────
	fmt.Println("Updating artifact", artifactPath)
	artifactRaw, err := os.ReadFile(artifactPath)
	if err != nil {
		log.Fatalf("read artifact: %v", err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(artifactRaw, &artifact); err != nil {
		log.Fatalf("parse artifact: %v", err)
	}
	artifact["bytecode"] = "0x" + creation
	artifact["deployedBytecode"] = "0x" + runtime

	updated, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		log.Fatalf("marshal artifact: %v", err)
	}
	if err := os.WriteFile(artifactPath, updated, 0644); err != nil {
		log.Fatalf("write artifact: %v", err)
	}
	fmt.Println("  artifact updated")
	fmt.Println("Done. PrivateMintVerifier is ready to deploy.")
}

// parseSolcBinaries extracts the creation bytecode and runtime bytecode from
// the line-by-line solc --bin --bin-runtime output.
func parseSolcBinaries(lines []string) (creation, runtime string) {
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "Binary:":
			if i+1 < len(lines) {
				creation = strings.TrimSpace(lines[i+1])
			}
		case "Binary of the runtime part:":
			if i+1 < len(lines) {
				runtime = strings.TrimSpace(lines[i+1])
			}
		}
	}
	return
}

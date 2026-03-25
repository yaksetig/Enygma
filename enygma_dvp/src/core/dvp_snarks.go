package core

/*
Port of src/core/dvpSnarks.js

Handles snarkjs circuit ceremony setup (key generation and contribution)
and verification key loading/formatting.

Note: snarkjs has no native Go equivalent. The ceremony setup functions
(GenerateSnarkKeyForCircuit, ContributeToCeremony) shell out to the
snarkjs CLI via os/exec. Node.js and snarkjs must be installed for
those functions to work.

GetVerificationKeys / FormatVKey are pure Go and have no external deps.
*/

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
)

const ptauPath = "./build/powersOfTau28_hez_final_20.ptau"

// CircuitConf represents a single circuit entry from enygmadvp.config.json.
type CircuitConf struct {
	ID       int      `json:"id"`
	Filename string   `json:"filename"`
	Tags     []string `json:"tags"`
}

// VKeyJSON mirrors the snarkjs verification key JSON format.
type VKeyJSON struct {
	Protocol string     `json:"protocol"`
	Curve    string     `json:"curve"`
	VkAlpha1 []string   `json:"vk_alpha_1"`
	VkBeta2  [][]string `json:"vk_beta_2"`
	VkGamma2 [][]string `json:"vk_gamma_2"`
	VkDelta2 [][]string `json:"vk_delta_2"`
	IC       [][]string `json:"IC"`
}

// DvpG1Point represents a BN254 G1 curve point.
type DvpG1Point struct {
	X *big.Int
	Y *big.Int
}

// DvpG2Point represents a BN254 G2 curve point.
// Each coordinate is a pair of field elements (X[1], X[0]) matching the
// coordinate swap done in the JS formatVKey function.
type DvpG2Point struct {
	X [2]*big.Int
	Y [2]*big.Int
}

// DvpVerifyingKey is the formatted verification key ready for contract registration.
type DvpVerifyingKey struct {
	Alpha1 DvpG1Point
	Beta2  DvpG2Point
	Gamma2 DvpG2Point
	Delta2 DvpG2Point
	IC     []DvpG1Point
}

// GenerateSnarkKeyForCircuit reads the .r1cs file for the given circuit,
// logs its constraint info, and produces a new zkey (.tmp file).
// Requires Node.js + snarkjs installed and callable via npx.
func GenerateSnarkKeyForCircuit(filename string) error {
	filePath := "build/" + filename

	fmt.Printf("\nFilename: %s\n", filename)

	// Print R1CS info
	infoCmd := exec.Command("npx", "snarkjs", "r1cs", "info", filePath+".r1cs")
	infoCmd.Stdout = os.Stdout
	infoCmd.Stderr = os.Stderr
	if err := infoCmd.Run(); err != nil {
		return fmt.Errorf("r1cs info failed for %s: %w", filename, err)
	}

	// Generate new zkey from R1CS + ptau
	newKeyCmd := exec.Command("npx", "snarkjs", "zkey", "new",
		filePath+".r1cs", ptauPath, filePath+".tmp")
	newKeyCmd.Stdout = os.Stdout
	newKeyCmd.Stderr = os.Stderr
	if err := newKeyCmd.Run(); err != nil {
		return fmt.Errorf("zkey new failed for %s: %w", filename, err)
	}

	return nil
}

// GenerateSnarkKeys generates zkeys for all circuits in circuitConfs.
// tags is reserved for future filtering (currently unused, matching JS behaviour).
func GenerateSnarkKeys(circuitConfs []CircuitConf, tags []string) error {
	fmt.Println("Generating Snark keys ... ")

	for _, conf := range circuitConfs {
		if err := GenerateSnarkKeyForCircuit(conf.Filename); err != nil {
			return err
		}
	}

	fmt.Println("zkeys have been generated for all circuits... waiting for initialization task.")
	return nil
}

// ContributeToCeremony contributes random entropy to the trusted setup for
// the given circuit, exports the verification key to JSON, and removes the
// temporary .tmp zkey file.
// Requires Node.js + snarkjs installed and callable via npx.
func ContributeToCeremony(circuitName string) error {
	jsPK := fmt.Sprintf("./build/%s.zkey", circuitName)
	jsVK := fmt.Sprintf("./build/%s.json", circuitName)
	jsTmp := fmt.Sprintf("./build/%s.tmp", circuitName)

	// Generate random entropy (32 bytes hex-encoded, same as JS crypto.randomBytes(32).toString("hex"))
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("failed to generate random entropy: %w", err)
	}
	random := hex.EncodeToString(randomBytes)

	// Contribute randomness to ceremony
	contributeCmd := exec.Command("npx", "snarkjs", "zkey", "contribute",
		jsTmp, jsPK, "--name=Alice", "-e="+random)
	contributeCmd.Stdout = os.Stdout
	contributeCmd.Stderr = os.Stderr
	if err := contributeCmd.Run(); err != nil {
		return fmt.Errorf("zkey contribute failed for %s: %w", circuitName, err)
	}

	// Export verification key to JSON
	exportCmd := exec.Command("npx", "snarkjs", "zkey", "export", "verificationkey",
		jsPK, jsVK)
	exportCmd.Stdout = os.Stdout
	exportCmd.Stderr = os.Stderr
	if err := exportCmd.Run(); err != nil {
		return fmt.Errorf("zkey export verificationkey failed for %s: %w", circuitName, err)
	}

	// Remove temporary zkey file
	if err := os.Remove(jsTmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove temp file %s: %w", jsTmp, err)
	}

	return nil
}

// ContributeToCeremonies runs ContributeToCeremony for each circuit in circuitConfs.
// tags is reserved for future filtering (currently unused, matching JS behaviour).
func ContributeToCeremonies(circuitConfs []CircuitConf, tags []string) error {
	for _, conf := range circuitConfs {
		if err := ContributeToCeremony(conf.Filename); err != nil {
			return err
		}
	}
	return nil
}

// FormatVKey converts a raw VKeyJSON (as exported by snarkjs) into a DvpVerifyingKey
// with *big.Int fields. The G2 coordinate pairs are stored as [X[1], X[0]] to match
// the swap done in the original JS formatVKey function.
func FormatVKey(vkey VKeyJSON) DvpVerifyingKey {
	parseG1 := func(coords []string) DvpG1Point {
		x, _ := new(big.Int).SetString(coords[0], 10)
		y, _ := new(big.Int).SetString(coords[1], 10)
		return DvpG1Point{X: x, Y: y}
	}

	parseG2 := func(coords [][]string) DvpG2Point {
		x0, _ := new(big.Int).SetString(coords[0][0], 10)
		x1, _ := new(big.Int).SetString(coords[0][1], 10)
		y0, _ := new(big.Int).SetString(coords[1][0], 10)
		y1, _ := new(big.Int).SetString(coords[1][1], 10)
		// Swap: store as [index1, index0] matching JS: x: [BigInt(vk_beta_2[0][1]), BigInt(vk_beta_2[0][0])]
		return DvpG2Point{
			X: [2]*big.Int{x1, x0},
			Y: [2]*big.Int{y1, y0},
		}
	}

	ic := make([]DvpG1Point, len(vkey.IC))
	for i, point := range vkey.IC {
		ic[i] = parseG1(point)
	}

	return DvpVerifyingKey{
		Alpha1: parseG1(vkey.VkAlpha1),
		Beta2:  parseG2(vkey.VkBeta2),
		Gamma2: parseG2(vkey.VkGamma2),
		Delta2: parseG2(vkey.VkDelta2),
		IC:     ic,
	}
}

// GetVerificationKeys loads and formats verification keys for all circuits.
// buildDir is the path to the directory containing the <filename>.json vkey files.
func GetVerificationKeys(circuitConfs []CircuitConf, buildDir string) ([]DvpVerifyingKey, error) {
	verificationKeys := make([]DvpVerifyingKey, 0, len(circuitConfs))

	for _, conf := range circuitConfs {
		filePath := filepath.Join(buildDir, conf.Filename+".json")
		fmt.Println(filePath)

		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read vkey file %s: %w", filePath, err)
		}

		var vkJSON VKeyJSON
		if err := json.Unmarshal(data, &vkJSON); err != nil {
			return nil, fmt.Errorf("failed to parse vkey JSON %s: %w", filePath, err)
		}

		verificationKeys = append(verificationKeys, FormatVKey(vkJSON))
	}

	return verificationKeys, nil
}

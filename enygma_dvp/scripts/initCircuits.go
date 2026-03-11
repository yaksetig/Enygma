package main

/*
Port of scripts/initCircuits.js

Initializes Circom ZK circuits by:
  1. Generating an initial zkey for each circuit (r1cs info + zkey new).
  2. Contributing random entropy to the trusted-setup ceremony for each circuit
     (zkey contribute + zkey export verificationkey), then removing the .tmp file.

Requires Node.js and snarkjs to be installed (invoked via npx).

Run with:
  go run scripts_go/initCircuits.go

JS mapping:
  - dvpSnarks.generateSnarkKeys(circuitConfs)    → generateSnarkKeys(circuits)
  - dvpSnarks.contributeToCeremonies(circuitConfs) → contributeToCeremonies(circuits)

Note: this file defines its own func main() and is intended to be compiled and
run in isolation. It cannot be built together with deploy.go or init.go, which
also define func main().
*/

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// CircuitsConfig is the minimal config subset needed for circuit initialization.
type CircuitsConfig struct {
	Circom struct {
		Circuits []CircuitEntry `json:"circuits"`
	} `json:"circom"`
}

// CircuitEntry mirrors a single entry in enygmadvp.config.json's circom.circuits array.
type CircuitEntry struct {
	ID       int      `json:"id"`
	Filename string   `json:"filename"`
	Tags     []string `json:"tags"`
}

var circuitsProjectRoot string

func init() {
	execPath, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get working directory:", err)
	}
	// Support being run from either the project root or the scripts_go/ subdirectory.
	if filepath.Base(execPath) == "scripts_go" {
		circuitsProjectRoot = filepath.Dir(execPath)
	} else {
		circuitsProjectRoot = execPath
	}
}

func main() {
	if err := initializeDvpCircuits(); err != nil {
		log.Fatal("Circuit initialization failed:", err)
	}
}

// initializeDvpCircuits loads the circuit config and runs key generation +
// ceremony contribution for every circuit.
// Corresponds to: initializeDvpCircuits() in scripts/initCircuits.js
func initializeDvpCircuits() error {
	fmt.Println("initializing Circom circuits...")

	config, err := loadCircuitsConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	circuits := config.Circom.Circuits

	if err := generateSnarkKeys(circuits); err != nil {
		return err
	}

	if err := contributeToCeremonies(circuits); err != nil {
		return err
	}

	fmt.Println("Circom circuits have been initialized.")
	return nil
}

// loadCircuitsConfig reads enygmadvp.config.json and returns the circom section.
func loadCircuitsConfig() (*CircuitsConfig, error) {
	configPath := filepath.Join(circuitsProjectRoot, "enygmadvp.config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config CircuitsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// generateSnarkKeys generates initial zkeys for all circuits.
// Corresponds to: dvpSnarks.generateSnarkKeys(circuitConfs)
func generateSnarkKeys(circuits []CircuitEntry) error {
	fmt.Println("Generating Snark keys ... ")

	for _, c := range circuits {
		if err := generateSnarkKeyForCircuit(c.Filename); err != nil {
			return err
		}
	}

	fmt.Println("zkeys have been generated for all circuits... waiting for initialization task.")
	return nil
}

// generateSnarkKeyForCircuit prints R1CS constraint info and generates a new .tmp zkey.
// Corresponds to: dvpSnarks.generateSnarkKeyForCircuit(filename)
func generateSnarkKeyForCircuit(filename string) error {
	buildDir := filepath.Join(circuitsProjectRoot, "build")
	filePath := filepath.Join(buildDir, filename)
	ptau := filepath.Join(buildDir, "powersOfTau28_hez_final_20.ptau")

	fmt.Printf("\nFilename: %s\n", filename)

	// Print R1CS constraint info
	infoCmd := exec.Command("npx", "snarkjs", "r1cs", "info", filePath+".r1cs")
	infoCmd.Stdout = os.Stdout
	infoCmd.Stderr = os.Stderr
	if err := infoCmd.Run(); err != nil {
		return fmt.Errorf("r1cs info failed for %s: %w", filename, err)
	}

	// Generate new zkey from R1CS + ptau
	newKeyCmd := exec.Command("npx", "snarkjs", "zkey", "new",
		filePath+".r1cs", ptau, filePath+".tmp")
	newKeyCmd.Stdout = os.Stdout
	newKeyCmd.Stderr = os.Stderr
	if err := newKeyCmd.Run(); err != nil {
		return fmt.Errorf("zkey new failed for %s: %w", filename, err)
	}

	return nil
}

// contributeToCeremonies runs the trusted-setup ceremony for all circuits.
// Corresponds to: dvpSnarks.contributeToCeremonies(circuitConfs)
func contributeToCeremonies(circuits []CircuitEntry) error {
	for _, c := range circuits {
		if err := contributeToCeremony(c.Filename); err != nil {
			return err
		}
	}
	return nil
}

// contributeToCeremony contributes random entropy to the ceremony for one circuit,
// exports the verification key to JSON, and removes the .tmp zkey file.
// Corresponds to: dvpSnarks.contributeToCeremony(circuitName)
func contributeToCeremony(circuitName string) error {
	buildDir := filepath.Join(circuitsProjectRoot, "build")
	jsPK := filepath.Join(buildDir, circuitName+".zkey")
	jsVK := filepath.Join(buildDir, circuitName+".json")
	jsTmp := filepath.Join(buildDir, circuitName+".tmp")

	// Generate 32 bytes of random entropy (same as JS crypto.randomBytes(32).toString("hex"))
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

package core

import (
	"fmt"
	"math/big"
	"testing"
)

func TestNewMerkleTree(t *testing.T) {
	depth := 8
	tree := NewMerkleTree(depth)

	if tree.Depth() != depth {
		t.Errorf("Expected depth %d, got %d", depth, tree.Depth())
	}

	if tree.Root() == nil {
		t.Error("Root should not be nil")
	}

	fmt.Printf("Initial root: %s\n", tree.Root().String())
}

func TestInsertLeaf(t *testing.T) {
	tree := NewMerkleTree(8)

	leaf1 := big.NewInt(12345)
	tree.InsertLeaf(leaf1)

	leaves := tree.GetLeaves()
	if len(leaves) != 1 {
		t.Errorf("Expected 1 leaf, got %d", len(leaves))
	}

	if leaves[0].Cmp(leaf1) != 0 {
		t.Errorf("Leaf mismatch: expected %s, got %s", leaf1.String(), leaves[0].String())
	}

	fmt.Printf("Root after inserting leaf: %s\n", tree.Root().String())
}

func TestInsertMultipleLeaves(t *testing.T) {
	tree := NewMerkleTree(8)

	leaves := []*big.Int{
		big.NewInt(100),
		big.NewInt(200),
		big.NewInt(300),
	}

	tree.InsertLeaves(leaves)

	storedLeaves := tree.GetLeaves()
	if len(storedLeaves) != 3 {
		t.Errorf("Expected 3 leaves, got %d", len(storedLeaves))
	}

	fmt.Printf("Root after inserting 3 leaves: %s\n", tree.Root().String())
}

func TestGenerateProof(t *testing.T) {
	tree := NewMerkleTree(8)

	// Insert some leaves
	leaf1 := big.NewInt(111)
	leaf2 := big.NewInt(222)
	leaf3 := big.NewInt(333)

	tree.InsertLeaves([]*big.Int{leaf1, leaf2, leaf3})

	// Generate proof for leaf2
	proof, err := tree.GenerateProof(leaf2)
	if err != nil {
		t.Fatalf("Failed to generate proof: %v", err)
	}

	if proof.Element.Cmp(leaf2) != 0 {
		t.Errorf("Proof element mismatch")
	}

	if proof.Root.Cmp(tree.Root()) != 0 {
		t.Errorf("Proof root mismatch")
	}

	fmt.Printf("Proof for leaf %s:\n", leaf2.String())
	fmt.Printf("  Root: %s\n", proof.Root.String())
	fmt.Printf("  Indices: %s\n", proof.Indices.String())
	fmt.Printf("  Elements count: %d\n", len(proof.Elements))
}

func TestHashLeftRight(t *testing.T) {
	left := big.NewInt(1)
	right := big.NewInt(2)

	hash := HashLeftRight(left, right)

	if hash == nil {
		t.Error("Hash should not be nil")
	}

	fmt.Printf("Poseidon hash of (1, 2): %s\n", hash.String())

	// Hash should be deterministic
	hash2 := HashLeftRight(left, right)
	if hash.Cmp(hash2) != 0 {
		t.Error("Hash should be deterministic")
	}
}

func TestGetZeroValue(t *testing.T) {
	zeroValue := GetZeroValue()

	if zeroValue == nil {
		t.Error("Zero value should not be nil")
	}

	// Should be less than SNARK_SCALAR_FIELD
	if zeroValue.Cmp(SNARK_SCALAR_FIELD) >= 0 {
		t.Error("Zero value should be less than SNARK_SCALAR_FIELD")
	}

	fmt.Printf("Zero value (keccak256('ZkDvp') %% SNARK_SCALAR_FIELD): %s\n", zeroValue.String())
}

func TestProofNotFound(t *testing.T) {
	tree := NewMerkleTree(8)

	tree.InsertLeaf(big.NewInt(100))

	// Try to generate proof for non-existent element
	_, err := tree.GenerateProof(big.NewInt(999))
	if err == nil {
		t.Error("Expected error for non-existent element")
	}

	fmt.Printf("Expected error: %v\n", err)
}

func TestTreeNumber(t *testing.T) {
	tree := NewMerkleTree(8)

	if tree.TreeNumber() != 0 {
		t.Errorf("Initial tree number should be 0, got %d", tree.TreeNumber())
	}

	if tree.LastTreeNumber() != 0 {
		t.Errorf("Initial last tree number should be 0, got %d", tree.LastTreeNumber())
	}
}

// Run with: go test -v ./core/
func TestAll(t *testing.T) {
	fmt.Println("=== Merkle Tree Tests ===")
	fmt.Println()
}

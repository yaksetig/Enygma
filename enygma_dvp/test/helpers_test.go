package tests

import (
	"context"
	"math/big"
	"net"
	"testing"

	"enygma_dvp/src_go/core"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// serverAvailable checks whether something is listening on addr.
func serverAvailable(addr string) bool {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// loadVaultMerkleTree queries all historical Commitment events emitted by the
// vault contract and reconstructs the full Merkle tree, matching on-chain state.
// Call this AFTER the deposit transaction is mined so the deposited leaf is included.
func loadVaultMerkleTree(t *testing.T, client *ethclient.Client, vaultAddr common.Address, merkleDepth int) *core.MerkleTree {
	t.Helper()
	commitmentSig := crypto.Keccak256Hash([]byte("Commitment(uint256,uint256)"))
	query := ethereum.FilterQuery{
		Addresses: []common.Address{vaultAddr},
		Topics:    [][]common.Hash{{commitmentSig}},
	}
	logs, err := client.FilterLogs(context.Background(), query)
	if err != nil {
		t.Fatalf("FilterLogs (vault Commitment events): %v", err)
	}
	mt := core.NewMerkleTree(merkleDepth)
	for _, log := range logs {
		if len(log.Topics) < 3 {
			continue
		}
		mt.InsertLeaf(log.Topics[2].Big())
	}
	t.Logf("loadVaultMerkleTree: loaded %d commitment leaves from vault %s", len(logs), vaultAddr.Hex())
	return mt
}

// makeDummyProof returns a zero-valued MerkleProof used as a dummy (zero-value) input.
func makeDummyProof(depth int) *core.MerkleProof {
	p := &core.MerkleProof{
		Element:  big.NewInt(0),
		Elements: make([]*big.Int, depth),
		Indices:  big.NewInt(0),
		Root:     big.NewInt(0),
	}
	for i := range p.Elements {
		p.Elements[i] = big.NewInt(0)
	}
	return p
}

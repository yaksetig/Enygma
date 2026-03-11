package tests

import (
	"math/big"
	"net"

	"enygma_dvp/src_go/core"
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

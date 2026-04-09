package templates

import (
	"github.com/consensys/gnark/frontend"
	"gnark_server/primitives"
)

type DvPDestinationCircuitConfig struct {
	TmMerkleTreeDepth int
}

// DvPDestinationCircuit proves Bob's side of a DvP (Delivery vs Payment) swap.
//
// Bob spends his current note (asset 2) and proves that COMMIT_A —
// which Alice created in her DvPInitiatorCircuit proof — correctly
// encodes the same amount and token that Bob is delivering.
//
// This is the key linking constraint of the protocol:
//
//	COMMIT_A = Poseidon4(spendPkAlice, saltA, valueIn, tokenIdIn)
//
// where valueIn/tokenIdIn are Bob's own asset values (private witnesses),
// and StCommitA is Alice's public commitment copied from her on-chain TX.
//
// Off-chain Bob decapsulates CTXT to recover ss_B, then derives:
//
//	saltA  = HKDF(ss_B, "Init Salt")  → WtSaltA
//	saltB  = HKDF(ss_B, "note salt")
//	encKey = HKDF(ss_B, "encryption key")
//
// Bob decrypts ENC_TX_DATA to get (tokenIdIn_alice, valueIn_alice) and
// verifies COMMIT_B matches before submitting his proof.
//
// Public statement (5 elements):
//
//	[msg, treeNum, root, nf_B, commitA]
type DvPDestinationCircuit struct {
	Config DvPDestinationCircuitConfig

	// --- public inputs ---
	StMessage    frontend.Variable `gnark:",public"` // swap_id (computed on-chain by initDvP)
	StTreeNumber frontend.Variable `gnark:",public"` // sub-tree index for Bob's input note
	StMerkleRoot frontend.Variable `gnark:",public"` // Merkle root
	StNullifier  frontend.Variable `gnark:",public"` // nf_B = Poseidon(sk_B, leafIndex)
	StCommitA    frontend.Variable `gnark:",public"` // COMMIT_A from Alice's initiator proof

	// --- private witnesses: Bob's input note ---
	WtSpendKeyIn   frontend.Variable   // Bob's spend secret key
	WtValueIn      frontend.Variable   // amount_2 (Bob's asset)
	WtSaltIn       frontend.Variable   // saltBField of Bob's current note
	WtTokenIdIn    frontend.Variable   // token_id_2
	WtPathElements []frontend.Variable // Merkle path (TmMerkleTreeDepth elements)
	WtPathIndex    frontend.Variable   // leaf index in the tree

	// --- private witnesses: cross-commitment ---
	WtSpendPkAlice frontend.Variable // Alice's spend public key
	WtSaltA        frontend.Variable // HKDF(ss_B, "Init Salt") — Bob derived this by decapsulating
}

func (circuit *DvPDestinationCircuit) Define(api frontend.API) error {
	// 1. Derive Bob's spend public key from his secret key.
	pkBob := primitives.PublicKey(api, circuit.WtSpendKeyIn)

	// 2. Nullifier: nf_B = Poseidon(sk_B, leafIndex).
	nullifier := primitives.Nullifier(api, circuit.WtSpendKeyIn, circuit.WtPathIndex)
	api.AssertIsEqual(nullifier, circuit.StNullifier)

	// 3. Input commitment: Poseidon4(pk_B, saltIn, valueIn, tokenIdIn).
	commitIn := primitives.Erc20CommitmentV2(api,
		pkBob,
		circuit.WtSaltIn,
		circuit.WtValueIn,
		circuit.WtTokenIdIn,
	)

	// 4. Merkle membership: commitIn is in the tree at WtPathIndex.
	pathElems := make([]frontend.Variable, circuit.Config.TmMerkleTreeDepth)
	for j := 0; j < circuit.Config.TmMerkleTreeDepth; j++ {
		pathElems[j] = circuit.WtPathElements[j]
	}
	root := primitives.MerkleProof(api, commitIn, circuit.WtPathIndex, pathElems)
	api.AssertIsEqual(root, circuit.StMerkleRoot)

	// 5. COMMIT_A must encode the same amount and token that Bob is delivering.
	//    Bob provides spendPkAlice and saltA as witnesses; the circuit verifies
	//    the commitment is correctly formed with his own valueIn and tokenIdIn.
	commitA := primitives.Erc20CommitmentV2(api,
		circuit.WtSpendPkAlice,
		circuit.WtSaltA,
		circuit.WtValueIn,
		circuit.WtTokenIdIn,
	)
	api.AssertIsEqual(commitA, circuit.StCommitA)

	return nil
}

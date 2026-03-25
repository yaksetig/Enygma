package templates

import(
	"github.com/consensys/gnark/frontend"
	"gnark_server/primitives"
)

// const nInputsERC721 = 1
// const mOutputsERC721 = 1
// const MerkleTreeDepthERC721=8

type Erc721CircuitConfig struct{
	TmNumOfTokens int
	TmMerkleTreeDepth int
}

type Erc721Circuit struct {

	Config Erc721CircuitConfig

	// --- public inputs (statement) ---
	StMessage       frontend.Variable   `gnark:",public"` // domain-separation tag / operation identifier
	StTreeNumbers   []frontend.Variable `gnark:",public"` // TmNumOfTokens — tree index for each input note
	StMerkleRoots   []frontend.Variable `gnark:",public"` // TmNumOfTokens — Merkle root each input note must belong to
	StNullifiers    []frontend.Variable `gnark:",public"` // TmNumOfTokens — Nullifier(sk_spend, leafIndex), prevents double-spend
	StCommitmentOut []frontend.Variable `gnark:",public"` // TmNumOfTokens — new output commitments inserted on-chain

	// --- private witnesses: inputs (NFT notes being transferred) ---
	WtPrivateKeysIn         []frontend.Variable   // TmNumOfTokens — sk_spend, proves ownership of each input note
	WtValues                []frontend.Variable   // TmNumOfTokens — raw ERC721 tokenId
	WtSaltsIn               []frontend.Variable   // TmNumOfTokens — saltB from when this note was received
	WtPathElements          [][]frontend.Variable // TmNumOfTokens x TmMerkleTreeDepth — sibling nodes for Merkle proof
	WtPathIndices           []frontend.Variable   // TmNumOfTokens — leaf position in the tree
	WtErc721ContractAddress frontend.Variable     // ERC721 contract address — binds the commitment to a specific token contract

	// --- private witnesses: outputs (new notes being created) ---
	WtPublicKeysOut []frontend.Variable // TmNumOfTokens — pk_spend of each recipient
	WtSaltsOut      []frontend.Variable // TmNumOfTokens — saltB for each output note
}


func (circuit *Erc721Circuit) Define(api frontend.API) error{
	
	

	//verify input notes
	for i:=0; i< circuit.Config.TmNumOfTokens;i++{

		publicKey := primitives.PublicKey(api, circuit.WtPrivateKeysIn[i])

		nullifier := primitives.Nullifier(api,circuit.WtPrivateKeysIn[i],circuit.WtPathIndices[i])
		api.AssertIsEqual(nullifier,circuit.StNullifiers[i])

		commitment := primitives.Erc721Commitment(api, circuit.WtValues[i], publicKey, circuit.WtSaltsIn[i])
		
		pathElement := make([]frontend.Variable, circuit.Config.TmMerkleTreeDepth)

		for j:=0 ; j< circuit.Config.TmMerkleTreeDepth;j++{
			pathElement[j] = circuit.WtPathElements[i][j]
		}
		root := primitives.MerkleProof(api, commitment,circuit.WtPathIndices[i],pathElement)

		isZero := api.IsZero(circuit.WtValues[i]) // ValueIn[i] ?0 =  1:0
		Enable := api.Mul(1,isZero)  

		Diff   := api.Sub(circuit.StMerkleRoots[i], root)

		api.AssertIsEqual(api.Mul(Diff, Enable), 0)
	
		commitmentOut := primitives.Erc721Commitment(api, circuit.WtValues[i], circuit.WtPublicKeysOut[i], circuit.WtSaltsOut[i])
		api.AssertIsEqual(commitmentOut, circuit.StCommitmentOut[i])
		
	}

	return nil
}

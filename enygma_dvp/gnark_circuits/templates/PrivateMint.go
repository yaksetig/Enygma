package templates

import(
	"github.com/consensys/gnark/frontend"
	"gnark_server/primitives"
)
// const maxCommissionPercentage = 4
// const commissionPercentageDecimals = 4

type PrivateMintConfig struct{

}

type PrivateMintCircuit struct {
	Commitment      	     	 frontend.Variable  `gnark:",public"` 
	ContractAddress				 frontend.Variable  `gnark:",public"`
	TokenId						 frontend.Variable  `gnark:",public"` 
	CipherText				     frontend.Variable  `gnark:",public"`

	Salt				 		 frontend.Variable 
	Amount						 frontend.Variable
	PublicKey				     frontend.Variable
	
	
}


func (circuit *PrivateMintCircuit) Define(api frontend.API) error{

	// V2 commitment: Poseidon(pk_spend, salt, amount, tokenId)
	// Matches the format expected by the JoinSplit circuit's input-side check.
	calculatedCommitment := primitives.Erc20CommitmentV2(api, circuit.PublicKey, circuit.Salt, circuit.Amount, circuit.TokenId)
	api.AssertIsEqual(calculatedCommitment, circuit.Commitment)

	// Note tag: lets Alice find her own mint on chain when scanning.
	calculatedCipherText := primitives.Commitment(api, circuit.PublicKey, circuit.Salt)
	api.AssertIsEqual(calculatedCipherText, circuit.CipherText)

	return nil
}

package primitives

import (
	"github.com/consensys/gnark/frontend"
	pos "gnark_server/poseidon"
)

func Erc1155Commitment(api frontend.API, contractAddress, tokenId, amount, publicKey, salt frontend.Variable) frontend.Variable {
	commit := pos.Poseidon(api, []frontend.Variable{contractAddress, tokenId, amount, publicKey, salt})
	out, _ := api.NewHint(ModHint, 2, commit)
	return out[0]
}

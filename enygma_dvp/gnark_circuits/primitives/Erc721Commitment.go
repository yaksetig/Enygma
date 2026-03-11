package primitives

import (
	"github.com/consensys/gnark/frontend"
	pos "gnark_server/poseidon"
)

func Erc721Commitment(api frontend.API, contractAddress, tokenId, publicKey, salt frontend.Variable) frontend.Variable {
	commit := pos.Poseidon(api, []frontend.Variable{contractAddress, tokenId, publicKey, salt})
	out, _ := api.NewHint(ModHint, 2, commit)
	return out[0]
}

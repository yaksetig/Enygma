package primitives

import (
	"github.com/consensys/gnark/frontend"
)

// Erc1155Commitment computes the ERC1155 commitment using the unified V2 formula:
//
//	C = Poseidon(pk_spend, salt, amount, tokenId)
//
// contractAddress is no longer part of the commitment; it is handled separately
// via Erc1155UniqueId2 in the asset-group Merkle proof.
func Erc1155Commitment(api frontend.API, tokenId, amount, publicKey, salt frontend.Variable) frontend.Variable {
	return Erc20CommitmentV2(api, publicKey, salt, amount, tokenId)
}

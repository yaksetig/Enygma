// cmd/regen_erc regenerates proving/verifying keys for all ERC721 and ERC1155
// circuits after the commitment formula was unified to Poseidon(pk_spend, salt, amount, tokenId).
package main

import (
	"fmt"

	"github.com/consensys/gnark/constraint/solver"
	"github.com/consensys/gnark/frontend"
	"gnark_server/primitives"
	script "gnark_server/scripts"
	"gnark_server/templates"
)

func main() {
	solver.RegisterHint(primitives.ModHint)
	solver.RegisterHint(primitives.ERC155UniqueIdNative)
	solver.RegisterHint(primitives.PoseidonNative)
	solver.RegisterHint(primitives.PoseidonPrivateKeyNative)
	solver.RegisterHint(primitives.Erc1155CommitmentNative)

	circuits := []struct {
		name string
		run  func()
	}{
		{"OwnershipERC721", func() {
			script.SetupOwnershipERC721(templates.Erc721CircuitConfig{
				TmNumOfTokens:    1,
				TmMerkleTreeDepth: 8,
			}, "OwnershipERC721")
		}},
		{"OwnershipERC721Auditor", func() {
			script.SetupOwnershipERC721Auditor(templates.Erc721WithAuditorCircuitConfig{
				TmNumOfTokens:    1,
				TmMerkleTreeDepth: 8,
			}, "OwnershipERC721Auditor")
		}},
		{"OwnershipERC1155Fungible", func() {
			script.SetupOwnershipERC1155Fungible(templates.ERC1155FungibleCircuitConfig{
				TmNInputs:              1,
				TmMOutputs:             1,
				TmMerkleTreeDepth:      8,
				TmAssetGroupMerkleTree: 8,
				TmRange:                frontend.Variable("1000000000000000000000000000000000000"),
			}, "OwnershipERC1155Fungible")
		}},
		{"JoiSplitERC1155", func() {
			script.SetupJoinSplitERC1155(templates.ERC1155FungibleCircuitConfig{
				TmNInputs:              2,
				TmMOutputs:             2,
				TmMerkleTreeDepth:      8,
				TmAssetGroupMerkleTree: 8,
				TmRange:                frontend.Variable("1000000000000000000000000000000000000"),
			}, "JoiSplitERC1155")
		}},
		{"JoinSplitERC1155Auditor", func() {
			script.SetupJoinSplitERC1155Auditor(templates.ERC1155FungibleWithAuditorCircuitConfig{
				TmNInputs:              2,
				TmMOutputs:             2,
				TmMerkleTreeDepth:      8,
				TmAssetGroupMerkleTree: 8,
				TmRange:                frontend.Variable("1000000000000000000000000000000000000"),
			}, "JoinSplitERC1155Auditor")
		}},
		{"JoiSplitERC1155WithBroker", func() {
			script.SetupJoinSplitERC1155WithBroker(templates.ERC1155FungibleWithBrokerCircuitConfig{
				NInputs:                   2,
				MOutputs:                  3,
				MerkleTreeDepth:           8,
				AssetGroupMerkleTreeDepth: 8,
				MaxPermittedCommissionRate: 10,
				ComissionRateDecimals:     2,
				Range:                     frontend.Variable("1000000000000000000000000000000000000"),
			}, "JoiSplitERC1155WithBroker")
		}},
		{"OwnershipERC1155NonFungible", func() {
			script.SetupOwnershipERC1155NonFungible(templates.ERC1155NonFungibleCircuitConfig{
				TmNumOfTokens:              1,
				TmMerkleTreeDepth:          8,
				TmAssetGroupMerkleTreeDepth: 8,
			}, "OwnershipERC1155NonFungible")
		}},
		{"OwnershipERC1155NonFungibleAuditor", func() {
			script.SetupOwnershipERC1155NonFungibleAuditor(templates.ERC1155NonFungibleWithAuditorCircuitConfig{
				TmNumOfTokens:    1,
				TmMerkleTreeDepth: 8,
				TmAssetGroupMerkleTree: 8,
			}, "OwnershipERC1155NonFungibleAuditor")
		}},
		{"ERC1155Batch", func() {
			script.SetupBatchERC1155(templates.ERC1155NonFungibleCircuitConfig{
				TmNumOfTokens:              10,
				TmMerkleTreeDepth:          8,
				TmAssetGroupMerkleTreeDepth: 8,
			}, "ERC1155Batch")
		}},
		{"ERC1155BatchAuditor", func() {
			script.SetupBatchErc1155WithAuditor(templates.ERC1155NonFungibleWithAuditorCircuitConfig{
				TmNumOfTokens:    10,
				TmMerkleTreeDepth: 8,
				TmAssetGroupMerkleTree: 8,
			}, "ERC1155BatchAuditor")
		}},
	}

	for _, c := range circuits {
		fmt.Printf("=== Generating keys for %s ===\n", c.name)
		c.run()
		fmt.Printf("=== Done: %s ===\n\n", c.name)
	}

	fmt.Println("All ERC721/ERC1155 keys regenerated successfully.")
}

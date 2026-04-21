package main

import (

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/constraint/solver"

	"gnark_server/templates"
	"gnark_server/primitives"
	script "gnark_server/scripts"
)


func GenerationVkPk (){

	solver.RegisterHint(primitives.ModHint)
	solver.RegisterHint(primitives.ERC155UniqueIdNative)
	solver.RegisterHint(primitives.PoseidonNative)
	solver.RegisterHint(primitives.PoseidonPrivateKeyNative)
	solver.RegisterHint(primitives.Erc1155CommitmentNative)

	// NOT covered by integration tests (test/01–04)
	// auctionbidConfig := templates.AuctionBidCircuitConfig{
	// 	TmNInputs:    2,
	// 	TmMOutputs:   2,
	// 	TmNumOfIdParams:5,
	// 	TmDepthMerkle: 8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// 	TmGroupMerkleTreeDepth: 8,
	// }

	// NOT covered by integration tests (test/01–04)
	// auctionBidAuditorConfig := templates.TmAuctionBidAuditorCircuitConfig{
	// 	TmInputs:	2,
	// 	TmOutputs:	2,
	// 	TmNumOfIdParms:5,
	// 	TmMerkleTreeDepth: 8,
	// 	TmRange:frontend.Variable("1000000000000000000000000000000000000"),
	// 	TmAssetGroupMerkleTreeDepth:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// auctionInitAuditorConfig := templates.TmAuctionInitAuditorCircuitConfig{
	// 	TmNumOfIdParms :5,
	// 	TmMerkleTreeDepth:    8,
	// 	TmAssetGroupMerkleTreeDepth:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// auctionInitConfig := templates.AuctionInitCircuitConfig{
	// 	TmNumOfIdParms:5,
	// 	TmMerkleTreeDepth:8,
	// 	TmGroupMerkleTreeDepth:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// auctionNotWinningConfig := templates.AuctionNotWinningCircuitConfig{
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// privateOpeningConfig := templates.AuctionPrivateOpeningCircuitConfig{
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// brokerRegistration := templates.BrokerageRegistrationConfig{
	// 	TmNumOfInputs :		      2,
	// 	TmMerkleTreeDepth: 	  8,
	// 	TmGroupMerkleTreeDepth:  8,
	// 	TmMaxPermittedCommissionRate:10,
	// 	TmComissionRateDecimals:2,
	// 	TmRange: "1000000000000000000000000000000000000",
	// }

	// NOT covered by integration tests (test/01–04)
	// erc1155Batch := templates.ERC1155NonFungibleCircuitConfig{
	// 	TmNumOfTokens: 10,
	// 	TmMerkleTreeDepth: 8,
	// 	TmAssetGroupMerkleTreeDepth:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// erc1155BatchNonFungible := templates.ERC1155NonFungibleWithAuditorCircuitConfig{
	// 	TmNumOfTokens: 10,
	// 	TmMerkleTreeDepth: 8,
	// 	TmAssetGroupMerkleTree:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// erc20_join_split := templates.Erc20CircuitConfig{
	// 	TmNInputs: 2,
	// 	TmMOutputs:  2,
	// 	TmMerkleTreeDepth:8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc20Auditor_join_split_10_2 := templates.Erc20WithAuditorConfig{
	// 	TmNInputs: 10,
	// 	TmMOutputs: 2,
	// 	TmMerkleTreeDepth:8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc20_join_split_10_2 := templates.Erc20CircuitConfig{
	// 	TmNInputs: 10,
	// 	TmMOutputs:  2,
	// 	TmMerkleTreeDepth:8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc20_auditor_join_split := templates.Erc20WithAuditorConfig{
	// 	TmNInputs: 2,
	// 	TmMOutputs:  2,
	// 	TmMerkleTreeDepth:8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc20_join_with_broker := templates.Erc20BrokerConfig{
	// 	TmNInputs: 2,
	// 	TmMOutputs: 3,
	// 	TmMerkleTree:8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// 	TmMaxComissionPercentage:10,
	// 	TmComissionPercentageDecimals:0,
	// }

	// NOT covered by integration tests (test/01–04)
	// erc1155_join_split := templates.ERC1155FungibleCircuitConfig{
	// 	TmNInputs: 2,
	// 	TmMOutputs: 2,
	// 	TmMerkleTreeDepth:8,
	// 	TmAssetGroupMerkleTree: 8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc1155_join_split_with_auditor := templates.ERC1155FungibleWithAuditorCircuitConfig{
	// 	TmNInputs: 2,
	// 	TmMOutputs: 2,
	// 	TmMerkleTreeDepth:8,
	// 	TmAssetGroupMerkleTree: 8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// erc1155_join_split_with_broker := templates.ERC1155FungibleWithBrokerCircuitConfig{
	// 	NInputs: 2,
	// 	MOutputs: 3,
	// 	MerkleTreeDepth:8,
	// 	AssetGroupMerkleTreeDepth: 8,
	// 	MaxPermittedCommissionRate:10,
	// 	ComissionRateDecimals:2,
	// 	Range: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// legit_broker := templates.LegitBrokerCircuitConfig{}

	// NOT covered by integration tests (test/01–04)
	// ownership_erc721_config := templates.Erc721CircuitConfig{
	// 	TmNumOfTokens: 1,
	// 	TmMerkleTreeDepth: 8,
	// }

	// NOT covered by integration tests (test/01–04)
	// ownership_erc721_config_with_auditor := templates.Erc721WithAuditorCircuitConfig{
	// 	TmNumOfTokens: 1,
	// 	TmMerkleTreeDepth: 8,
	// }

	// NOT covered by integration tests (test/01–04)
	// ownership_erc1155_Fungible_config := templates.ERC1155FungibleCircuitConfig{
	// 	TmNInputs: 1,
	// 	TmMOutputs: 1,
	// 	TmMerkleTreeDepth:8,
	// 	TmAssetGroupMerkleTree: 8,
	// 	TmRange: frontend.Variable("1000000000000000000000000000000000000"),
	// }

	// NOT covered by integration tests (test/01–04)
	// ownership_erc1155_Non_Fungible_config := templates.ERC1155NonFungibleCircuitConfig{
	// 	TmNumOfTokens:1,
	// 	TmMerkleTreeDepth:8,
	// 	TmAssetGroupMerkleTreeDepth:8,
	// }

	// NOT covered by integration tests (test/01–04)
	// ownership_erc1155_Non_Fungible_config_with_auditor := templates.ERC1155NonFungibleWithAuditorCircuitConfig{
	// 	TmNumOfTokens:1,
	// 	TmMerkleTreeDepth:8,
	// 	TmAssetGroupMerkleTree:8,
	// }

	// covered by test/01_v2_erc20_private_mint_test.go (TestV2Erc20OnChain_PrivateMint)
	private_mint_config := templates.PrivateMintConfig{}

	// covered by test/02_v2_erc20_payment_test.go (TestV2Erc20Payment)
	payment_config := templates.PaymentCircuitConfig{
		TmNInputs:         2,
		TmMOutputs:        2,
		TmMerkleTreeDepth: 8,
		TmRange:           frontend.Variable("1000000000000000000000000000000000000"),
	}

	// covered by test/03_v2_dvp_test.go and test/04_v2_dvp_deadline_test.go
	dvp_initiator_config := templates.DvPInitiatorCircuitConfig{
		TmMerkleTreeDepth: 8,
	}

	// covered by test/03_v2_dvp_test.go and test/04_v2_dvp_deadline_test.go
	dvp_destination_config := templates.DvPDestinationCircuitConfig{
		TmMerkleTreeDepth: 8,
	}

	script.SetupPrivateMint(private_mint_config, "PrivateMint")
	script.SetupPayment(payment_config, "Payment")
	script.SetupDvPInitiator(dvp_initiator_config, "DvPInitiator")
	script.SetupDvPDestination(dvp_destination_config, "DvPDestination")
}

func main(){
	GenerationVkPk()
}

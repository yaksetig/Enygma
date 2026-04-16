package api

import (
    "github.com/gin-gonic/gin"
    "gnark_server/server/config"

    // NOT covered by integration tests (test/01–04)
    // "gnark_server/server/circuits/ownershipERC721"
    // "gnark_server/server/circuits/joinSplitERC20"
    // "gnark_server/server/circuits/joinSplitERC20_10_2"
    // "gnark_server/server/circuits/auctionInit"
    // "gnark_server/server/circuits/auctionInitAuditor"
    // acutionBid "gnark_server/server/circuits/auctionBid"
    // "gnark_server/server/circuits/auctionBidAuditor"
    // "gnark_server/server/circuits/auctionPrivateOpening"
    // "gnark_server/server/circuits/auctionNotWinning"
    // "gnark_server/server/circuits/brokerRegistration"
    // "gnark_server/server/circuits/erc1155FungbileWithBroker"
    // "gnark_server/server/circuits/erc1155NonFungible"
    // "gnark_server/server/circuits/erc1155NonFungibleAuditor"
    // "gnark_server/server/circuits/legitBroker"
    // "gnark_server/server/circuits/erc1155Fungible"
    // "gnark_server/server/circuits/erc1155FungibleAuditor"
    // serverutils "gnark_server/server/utils"

    // covered by test/01–04
    "gnark_server/server/circuits/privateMint"
    "gnark_server/server/circuits/payment"
    "gnark_server/server/circuits/dvpInit"
    "gnark_server/server/circuits/dvpDestination"
)


func NewServer(cfg *config.Config) *gin.Engine {
    r := gin.Default()

    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/ownershipERC721", ownershipERC721.NewHandler(cfg.OwnershipERC721Pk, cfg.OwnershipERC721Vk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/joinSplitERC20",  joinSplitERC20.NewHandler(cfg.JoinSplitERC20Pk,    cfg.JoinSplitERC20Vk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/joinSplitERC20_10_2",  joinSplitERC20_10_2.NewHandler(cfg.JoinSplitERC20_10_2Pk,    cfg.JoinSplitERC20_10_2Vk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/auctionInit",    auctionInit.NewHandler(cfg.AuctionInitPk, cfg.AuctionInitVk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/auctionInitAuditor",   auctionInitAuditor.NewHandler(cfg.AuctionInitAuditorPk, cfg.AuctionInitAuditorVk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/auctionBid",  acutionBid.NewHandler(cfg.AuctionBidPk, cfg.AuctionBidVk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/auctionBidAuditor", auctionBidAuditor.NewHandler(cfg.AuctionBidAuditorPk, cfg.AuctionBidAuditorVk))
    // NOT covered by integration tests (test/01–04)
    //r.POST("/proof/auctionPrivateOpening", auctionPrivateOpening.NewHandler(cfg.AuctionPrivateOpeningPk, cfg.AuctionPrivateOpeningVk))
    // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/auctionNotWinning", auctionNotWinning.NewHandler(cfg.AuctionNotOpeningPk, cfg.AuctionNotOpeningVk))
    // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/brokerRegistration", brokerRegistration.NewHandler(cfg.BrokerRegistrationPk, cfg.BrokerRegistrationVk))
    // NOT covered by integration tests (test/01–04); note: path has typo — "erc155" missing a '1'
    // r.POST("/proof/erc155Fungible", erc1155Fungible.NewHandler(cfg.ERC1155FungiblePk, cfg.ERC1155FungibleVk))
    // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/erc1155FungibleWithBroker",  erc1155FungibleWithBroker.NewHandler(cfg.ERC1155FungibleWithBrokerPk, cfg.ERC1155FungibleWithBrokerVk))
      // // NOT covered by integration tests (test/01–04)
    // r.POST("/util/poseidonEncrypt", serverutils.PoseidonEncryptHandler())
    // // NOT covered by integration tests (test/01–04)
    // r.POST("/util/poseidonDecrypt", serverutils.PoseidonDecryptHandler())
    // // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/erc1155NonFungible",erc1155NonFungible.NewHandler(cfg.ERC1155NonFungiblePk, cfg.ERC1155NonFungibleVk))
    // // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/erc1155NonFungibleAuditor",erc1155NonFungibleAuditor.NewHandler(cfg.ERC1155NonFungibleAuditorPk, cfg.ERC1155NonFungibleAuditorVk))
    // // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/legitBroker",legitBroker.NewHandler(cfg.LegitBrokerPk, cfg.LegitBrokerVk))
    // // NOT covered by integration tests (test/01–04)
    // r.POST("/proof/erc1155FungibleAuditor", erc1155FungibleAuditor.NewHandler(cfg.ERC1155FungibleAuditorPk, cfg.ERC1155FungibleAuditorVk))
    
    
    // covered by test/01_v2_erc20_private_mint_test.go (TestV2Erc20OnChain_PrivateMint)
    r.POST("/proof/privateMint", privateMint.NewHandler(cfg.PrivateMintPk, cfg.PrivateMintVk))
    // covered by test/02_v2_erc20_payment_test.go (TestV2Erc20Payment)
    r.POST("/proof/payment",         payment.NewHandler(cfg.PaymentPk, cfg.PaymentVk))
    // covered by test/03_v2_dvp_test.go (TestV2DvP) and test/04_v2_dvp_deadline_test.go (TestV2DvP_WithDeadline)
    r.POST("/proof/dvpInitiator",    dvpInit.NewHandler(cfg.DvPInitiatorPk, cfg.DvPInitiatorVk))
    // covered by test/03_v2_dvp_test.go (TestV2DvP) and test/04_v2_dvp_deadline_test.go (TestV2DvP_WithDeadline)
    r.POST("/proof/dvpDestination",  dvpDestination.NewHandler(cfg.DvPDestinationPk, cfg.DvPDestinationVk))
  
    return r
}


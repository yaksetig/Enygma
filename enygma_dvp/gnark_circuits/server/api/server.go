package api

import (
    "github.com/gin-gonic/gin"
    "gnark_server/server/config"


    serverutils "gnark_server/server/utils"

    // covered by test/01–04
    "gnark_server/server/circuits/privateMint"

    "gnark_server/server/circuits/dvpInit"
    "gnark_server/server/circuits/dvpDestination"
)


func NewServer(cfg *config.Config) *gin.Engine {
    r := gin.Default()
   
    r.POST("/proof/privateMint", privateMint.NewHandler(cfg.PrivateMintPk, cfg.PrivateMintVk))
    r.POST("/proof/dvpInitiator",    dvpInit.NewHandler(cfg.DvPInitiatorPk, cfg.DvPInitiatorVk))
    r.POST("/proof/dvpDestination",  dvpDestination.NewHandler(cfg.DvPDestinationPk, cfg.DvPDestinationVk))

    // Merkle tree 
    r.POST("/util/merkleStatus", serverutils.MerkleStatusHandler())
    r.POST("/util/merkleVault", serverutils.MerkleVaultHandler())

    return r
}


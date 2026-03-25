package utils

import (
	"math/big"
	"net/http"

	"github.com/gin-gonic/gin"
	pos "gnark_server/poseidon"
)

type PoseidonDecryptRequest struct {
	Key        [2]string `json:"key" binding:"required,len=2"`
	Nonce      string    `json:"nonce" binding:"required"`
	RealLength int       `json:"realLength" binding:"required"`
	Encrypted  []string  `json:"encrypted" binding:"required"`
}

type PoseidonDecryptResponse struct {
	Plaintext []string `json:"plaintext"`
}

func PoseidonDecryptHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req PoseidonDecryptRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		key0, ok0 := new(big.Int).SetString(req.Key[0], 10)
		key1, ok1 := new(big.Int).SetString(req.Key[1], 10)
		nonce, okN := new(big.Int).SetString(req.Nonce, 10)
		if !ok0 || !ok1 || !okN {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid big integer in key or nonce"})
			return
		}

		encrypted := make([]*big.Int, len(req.Encrypted))
		for i, s := range req.Encrypted {
			v, ok := new(big.Int).SetString(s, 10)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid big integer in encrypted"})
				return
			}
			encrypted[i] = v
		}

		key := [2]*big.Int{key0, key1}
		plaintext, err := pos.NativePoseidonDecrypt(key, nonce, req.RealLength, encrypted)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ptStrs := make([]string, len(plaintext))
		for i, v := range plaintext {
			ptStrs[i] = v.String()
		}

		c.JSON(http.StatusOK, PoseidonDecryptResponse{Plaintext: ptStrs})
	}
}

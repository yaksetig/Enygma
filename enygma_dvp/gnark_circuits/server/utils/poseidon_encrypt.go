package utils

import (
	"math/big"
	"net/http"

	"github.com/gin-gonic/gin"
	pos "gnark_server/poseidon"
)

type PoseidonEncryptRequest struct {
	Key        [2]string `json:"key" binding:"required,len=2"`
	Nonce      string    `json:"nonce" binding:"required"`
	RealLength int       `json:"realLength" binding:"required"`
	Plaintext  []string  `json:"plaintext" binding:"required"`
}

type PoseidonEncryptResponse struct {
	Encrypted []string `json:"encrypted"`
}

func PoseidonEncryptHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req PoseidonEncryptRequest
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

		if len(req.Plaintext) != req.RealLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": "plaintext length must match realLength"})
			return
		}

		plaintext := make([]*big.Int, req.RealLength)
		for i, s := range req.Plaintext {
			v, ok := new(big.Int).SetString(s, 10)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid big integer in plaintext"})
				return
			}
			plaintext[i] = v
		}

		key := [2]*big.Int{key0, key1}
		encrypted := pos.NativePoseidonEncrypt(key, nonce, req.RealLength, plaintext)

		encStrs := make([]string, len(encrypted))
		for i, v := range encrypted {
			encStrs[i] = v.String()
		}

		c.JSON(http.StatusOK, PoseidonEncryptResponse{Encrypted: encStrs})
	}
}

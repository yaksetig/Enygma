package utils

import (
	"math/big"
	"os"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
)

func LoadProvingKey(curve ecc.ID, filename string) (groth16.ProvingKey, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pk := groth16.NewProvingKey(curve)
	_, err = pk.ReadFrom(file)
	return pk, err
}

func LoadVerifyingKey(curve ecc.ID, filename string) (groth16.VerifyingKey, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	vk := groth16.NewVerifyingKey(curve)
	_, err = vk.ReadFrom(file)
	return vk, err
}

func ParseBigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

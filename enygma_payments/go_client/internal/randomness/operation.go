package randomness

import (
	"math/big"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/poseidon"

	"enygma/internal/curve"

	enygma "enygma/contracts"
)

func GetRValues(senderId int, s []*big.Int, block_hash *big.Int, k_index []*big.Int) []*big.Int {
	var rValues []*big.Int
	randomInt := big.NewInt(21)
	HashRandom, _ := poseidon.Hash([]*big.Int{randomInt})

	for i := 0; i < len(s); i++ {
		if ContainsBigInt(k_index, i) {
			inputs := []*big.Int{HashRandom, s[i], block_hash}
			block_hash.Mod(block_hash, curve.P)
			PoseidonHash, _ := poseidon.Hash(inputs)
			PoseidonHash.Mod(PoseidonHash, curve.P)
			rValues = append(rValues, PoseidonHash)
		}
	}
	return rValues
}

func HashArrayGen(s []*big.Int, k_index []*big.Int) []*big.Int {
	hashArray := make([]*big.Int, len(k_index))

	for j := 0; j < len(k_index); j++ {
		inputs := []*big.Int{s[j], s[j]}
		poseidonHash, err := poseidon.Hash(inputs)
		if err != nil {
			panic(err)
		}
		poseidonHash.Mod(poseidonHash, curve.P)
		hashArray[j] = poseidonHash
	}

	return hashArray
}

func TagMessageGen(senderId int, s []*big.Int, block_hash *big.Int, k_index []*big.Int) []*big.Int {
	var tagMessage []*big.Int
	tag := big.NewInt(12)
	HashTag, _ := poseidon.Hash([]*big.Int{tag})
	block_hash.Mod(block_hash, curve.P)

	for i := 0; i < len(s); i++ {
		if ContainsBigInt(k_index, i) {
			inputs := []*big.Int{HashTag, s[i], block_hash}
			PoseidonHash, _ := poseidon.Hash(inputs)
			PoseidonHash.Mod(PoseidonHash, curve.P)
			tagMessage = append(tagMessage, PoseidonHash)
		}
	}

	return tagMessage
}

func GetRSum(senderId int, s []*big.Int, block_hash *big.Int, k_index []*big.Int) *big.Int {
	sum := big.NewInt(0)
	randomInt := big.NewInt(21)
	HashRandom, _ := poseidon.Hash([]*big.Int{randomInt})

	for i := 0; i < len(s); i++ {
		if senderId != i {
			if ContainsBigInt(k_index, i) {
				inputs := []*big.Int{HashRandom, s[i], block_hash}
				PoseidonHash, _ := poseidon.Hash(inputs)
				PoseidonHash.Mod(PoseidonHash, curve.P)
				sum.Add(sum, PoseidonHash)
				sum.Mod(sum, curve.P)
			}
		}
	}
	return sum
}

func ContainsBigInt(s []*big.Int, val int) bool {
	valBig := big.NewInt(int64(val))
	for _, v := range s {
		if v.Cmp(valBig) == 0 {
			return true
		}
	}
	return false
}

func GenCommitmentAndRandom(qtyBanks int, v *big.Int, senderId int, txValues []*big.Int, blockHash *big.Int, kIndex []*big.Int, secrets []*big.Int) ([]enygma.IEnygmaPoint, []*big.Int) {
	txRandom := GetRValues(senderId, secrets, blockHash, kIndex)
	rSum := GetRSum(senderId, secrets, blockHash, kIndex)
	txRandom[senderId] = rSum
	var txCommit []*babyjub.Point

	for i := 0; i < len(kIndex); i++ {
		if i == senderId {
			txCommit = append(txCommit, curve.PedersenCommitment(curve.GetNegative(v), txRandom[i]))
		} else {
			txCommit = append(txCommit, curve.PedersenCommitment(txValues[i], curve.GetNegative(txRandom[i])))
		}
	}

	// Negate receiver random values to match circuit expectation:
	// circuit asserts TxRandomValues[receiver] = p - hashModP[receiver]
	for i := 0; i < len(kIndex); i++ {
		if i != senderId {
			txRandom[i] = curve.GetNegative(txRandom[i])
		}
	}

	commitments := make([]enygma.IEnygmaPoint, len(kIndex))
	for i := 0; i < len(kIndex); i++ {
		commit := enygma.IEnygmaPoint{C1: txCommit[i].X, C2: txCommit[i].Y}
		commitments[i] = commit
	}

	return commitments, txRandom
}

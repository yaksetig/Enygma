package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"golang.org/x/crypto/sha3"
)

// BASE_POINT_ORDER is the order of the BabyJubJub base point subgroup
var BASE_POINT_ORDER, _ = new(big.Int).SetString(
	"2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)

// --- BabyJubJub helpers ---

// mulPointEscalar performs scalar multiplication: result = scalar * point
func mulPointEscalar(point *babyjub.Point, scalar *big.Int) *babyjub.Point {
	res := babyjub.NewPoint()
	res.Mul(scalar, point)
	return res
}

// addPoints adds two BabyJubJub points using projective coordinates
func addPoints(p1, p2 *babyjub.Point) *babyjub.Point {
	res := babyjub.NewPointProjective()
	res.Add(p1.Projective(), p2.Projective())
	return res.Affine()
}

// negatePoint returns the negation of a BabyJubJub point (-x, y)
func negatePoint(p *babyjub.Point) *babyjub.Point {
	negX := new(big.Int).Sub(SNARK_SCALAR_FIELD, p.X)
	return &babyjub.Point{
		X: negX,
		Y: new(big.Int).Set(p.Y),
	}
}

// --- File utilities ---

// WriteToJSON writes data to a JSON file with indentation
func WriteToJSON(filePath string, data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return os.WriteFile(filePath, jsonData, 0644)
}

// --- Conversion utilities ---

// Buffer2BigInt converts a byte slice to *big.Int
func Buffer2BigInt(buf []byte) *big.Int {
	if len(buf) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(buf)
}

// StringifyBigInts converts a map with *big.Int values to string representations
func StringifyBigInts(input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range input {
		switch val := v.(type) {
		case *big.Int:
			result[k] = val.String()
		case []*big.Int:
			strs := make([]string, len(val))
			for i, bi := range val {
				strs[i] = bi.String()
			}
			result[k] = strs
		case map[string]interface{}:
			result[k] = StringifyBigInts(val)
		default:
			result[k] = v
		}
	}
	return result
}

// --- Random generation ---

// RandomInField returns a random value in the SNARK scalar field
func RandomInField() (*big.Int, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	result := new(big.Int).SetBytes(b)
	result.Mod(result, SNARK_SCALAR_FIELD)
	return result, nil
}

// RandomNonce generates a random 128-bit non-zero nonce
func RandomNonce() (*big.Int, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	result := new(big.Int).SetBytes(b)
	result.Add(result, big.NewInt(1)) // ensure non-zero
	return result, nil
}

// --- Poseidon-based key operations ---

// NewKeyPair generates a new Poseidon-based key pair
func NewKeyPair() (privateKey, publicKey *big.Int, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, nil, err
	}

	privateKey, err = poseidon.Hash([]*big.Int{new(big.Int).SetBytes(b)})
	if err != nil {
		return nil, nil, err
	}

	publicKey, err = poseidon.Hash([]*big.Int{privateKey})
	if err != nil {
		return nil, nil, err
	}

	return privateKey, publicKey, nil
}

// GetNullifier computes the nullifier from a private key and path indices
func GetNullifier(privateKey, pathIndices *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{privateKey, pathIndices})
}

// GetCommitment computes a commitment from a unique ID and public key
func GetCommitment(uniqueId, publicKey *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{uniqueId, publicKey})
}

// GetPublicKey derives a public key from a private key using Poseidon
func GetPublicKey(privateKey *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{privateKey})
}

// GetAuctionId computes an auction ID from a commitment
func GetAuctionId(commitment *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{commitment})
}

// BlindedPublicKey computes a blinded public key
func BlindedPublicKey(publicKey *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{publicKey})
}

// --- BabyJubJub encryption (ElGamal-like) ---

// EncodeMessage encodes an integer as a BabyJubJub point: m * Base8
func EncodeMessage(m *big.Int) *babyjub.Point {
	return mulPointEscalar(babyjub.B8, m)
}

// DecodeMessage recovers the integer from a BabyJubJub point by brute-force search
func DecodeMessage(point *babyjub.Point, maxM *big.Int) (*big.Int, error) {
	fmt.Printf("Allowed range: %s\n", maxM.String())
	m := new(big.Int)
	for m.Cmp(maxM) < 0 {
		candidate := EncodeMessage(m)
		if candidate.X.Cmp(point.X) == 0 && candidate.Y.Cmp(point.Y) == 0 {
			return new(big.Int).Set(m), nil
		}
		m.Add(m, big.NewInt(1))
	}
	return nil, fmt.Errorf("message not found")
}

// BabyEncrypt performs ElGamal-like encryption on BabyJubJub
// Returns (c1, c2) where c1 = r*G, c2 = m*G + r*pubKey
func BabyEncrypt(m, r *big.Int, pubKey *babyjub.Point) (c1, c2 *babyjub.Point) {
	// c1 = r * G
	c1 = mulPointEscalar(babyjub.B8, r)

	// rPub = r * publicKey
	rPub := mulPointEscalar(pubKey, r)

	// mG = m * G
	mG := EncodeMessage(m)

	// c2 = mG + rPub
	c2 = addPoints(mG, rPub)

	return c1, c2
}

// BabyDecrypt performs ElGamal-like decryption on BabyJubJub
// Returns the decrypted message or nil if not found in allowed range
func BabyDecrypt(c1, c2 *babyjub.Point, privateKey *big.Int, allowedRange *big.Int) *big.Int {
	if allowedRange == nil {
		allowedRange = big.NewInt(1000)
	}

	// rPub = privateKey * c1
	rPub := mulPointEscalar(c1, privateKey)

	// M_dec = c2 + (-rPub) = c2 - rPub
	negRPub := negatePoint(rPub)
	mDec := addPoints(c2, negRPub)

	decrypted, err := DecodeMessage(mDec, allowedRange)
	if err != nil {
		fmt.Println("Invalid value")
		return nil
	}
	return decrypted
}

// BabyKeyPair generates a BabyJubJub key pair
func BabyKeyPair() (privateKey *big.Int, publicKey *babyjub.Point, err error) {
	privateKey, err = RandomInField()
	if err != nil {
		return nil, nil, err
	}
	privateKey.Mod(privateKey, babyjub.SubOrder)

	publicKey = mulPointEscalar(babyjub.B8, privateKey)
	return privateKey, publicKey, nil
}

// --- Poseidon encryption (maci-crypto compatible) ---

// PoseidonEncryptResult holds the result of Poseidon encryption
type PoseidonEncryptResult struct {
	Encrypted  []*big.Int
	Nonce      *big.Int
	RandomVal  *big.Int
	SharedKey  *babyjub.Point
	AuthKey    *babyjub.Point
}

// PoseidonEncryptWrapper encrypts inputs using Poseidon sponge encryption
// TODO: Requires a Go port of maci-crypto's poseidonEncrypt
func PoseidonEncryptWrapper(inputs []*big.Int, publicKey *babyjub.Point) (*PoseidonEncryptResult, error) {
	nonce, err := RandomNonce()
	if err != nil {
		return nil, err
	}

	randomValue, err := RandomInField()
	if err != nil {
		return nil, err
	}
	randomValue.Div(randomValue, big.NewInt(10))

	authKey := mulPointEscalar(babyjub.B8, randomValue)
	sharedKey := mulPointEscalar(publicKey, randomValue)

	// TODO: implement poseidonEncrypt(inputs, sharedKey, nonce)
	// This requires porting maci-crypto's Poseidon sponge encryption to Go.
	// When implemented, replace the return below with:
	//   encrypted := poseidonEncrypt(inputs, sharedKey, nonce)
	//   return &PoseidonEncryptResult{Encrypted: encrypted, Nonce: nonce,
	//     RandomVal: randomValue, SharedKey: sharedKey, AuthKey: authKey}, nil
	_, _, _ = nonce, sharedKey, authKey
	return nil, fmt.Errorf("poseidonEncrypt not yet implemented: requires maci-crypto Go port")
}

// PoseidonDecryptWrapper decrypts data using Poseidon sponge decryption
// TODO: Requires a Go port of maci-crypto's poseidonDecrypt
func PoseidonDecryptWrapper(encrypted []*big.Int, authKey *babyjub.Point, nonce, privateKey *big.Int, length int) ([]*big.Int, error) {
	sharedKey := mulPointEscalar(authKey, privateKey)

	// TODO: implement poseidonDecrypt(encrypted, sharedKey, nonce, length)
	// This requires porting maci-crypto's Poseidon sponge decryption to Go
	_ = sharedKey
	return nil, fmt.Errorf("poseidonDecrypt not yet implemented: requires maci-crypto Go port")
}

// --- Token unique IDs ---

// Erc20UniqueId computes the unique ID for an ERC-20 token commitment
func Erc20UniqueId(contractAddress, amount *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{contractAddress, amount})
}

// Erc721UniqueId computes the unique ID for an ERC-721 token commitment
func Erc721UniqueId(contractAddress, tokenId *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{contractAddress, tokenId})
}

// Erc1155UniqueId computes the unique ID for an ERC-1155 token commitment
func Erc1155UniqueId(contractAddress, tokenId, amount *big.Int) (*big.Int, error) {
	uid1, err := poseidon.Hash([]*big.Int{contractAddress, tokenId})
	if err != nil {
		return nil, err
	}
	return poseidon.Hash([]*big.Int{uid1, amount})
}

// --- Hash functions ---

// Pedersen computes a Pedersen-like commitment using Poseidon: H(amount, random)
func Pedersen(amount, random *big.Int) (*big.Int, error) {
	return poseidon.Hash([]*big.Int{amount, random})
}

// Keccak256 computes keccak256(preimage) mod SNARK_SCALAR_FIELD
func Keccak256(preimage []byte) *big.Int {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(preimage)
	hash := hasher.Sum(nil)

	result := new(big.Int).SetBytes(hash)
	result.Mod(result, SNARK_SCALAR_FIELD)
	return result
}

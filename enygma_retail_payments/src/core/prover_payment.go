package core

import dvpcore "github.com/raylsnetwork/enygma_dvp/src/core"

// NewPaymentClient creates a GnarkClient targeting the retail payments gnark server.
// Defaults to http://localhost:8082 if baseURL is empty.
func NewPaymentClient(baseURL string) *dvpcore.GnarkClient {
	if baseURL == "" {
		baseURL = "http://localhost:8082"
	}
	return dvpcore.NewGnarkClient(baseURL)
}

// Type aliases — callers only need to import this package.
type (
	KeyPair       = dvpcore.KeyPair
	SpendKeyPair  = dvpcore.SpendKeyPair
	ViewKeyPair   = dvpcore.ViewKeyPair
	MerkleProof   = dvpcore.MerkleProof
	MerkleTree    = dvpcore.MerkleTree
	PaymentResult = dvpcore.PaymentResult
)

// Function re-exports — key generation, Merkle tree, and cryptographic helpers.
// PaymentProof itself lives in enygma_dvp/src/core/prover_erc.go and is accessed
// via GnarkClient.PaymentProof (see NewPaymentClient above).
var (
	NewSpendKeyPair   = dvpcore.NewSpendKeyPair
	NewViewKeyPair    = dvpcore.NewViewKeyPair
	NewMerkleTree     = dvpcore.NewMerkleTree
	GetNullifier      = dvpcore.GetNullifier
	Erc20CommitmentV2 = dvpcore.Erc20CommitmentV2
	Encapsulate       = dvpcore.Encapsulate
	Decapsulate       = dvpcore.Decapsulate
	DerivePaymentSalt = dvpcore.DerivePaymentSalt
	DerivePaymentKey  = dvpcore.DerivePaymentKey
	SaltBToField      = dvpcore.SaltBToField
	EncryptPayload    = dvpcore.EncryptPayload
	DecryptPayload    = dvpcore.DecryptPayload
	RandomInField     = dvpcore.RandomInField
)

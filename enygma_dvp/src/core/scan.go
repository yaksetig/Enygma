package core

import (
	"crypto/mlkem"
	"fmt"
	"math/big"
)

// OnChainErc20Event represents a single on-chain note-creation event published
// by the ERC20 vault after a successful JoinSplit proof.
//
// The sender posts (Commitment, CiphertextI, CiphertextII) for each output note.
// Everything else (token address, tree number, Merkle root) comes from the chain
// but is not needed for note scanning — the recipient only needs these three fields
// to determine ownership.
type OnChainErc20Event struct {
	// Commitment is the output note commitment:
	//   Poseidon(pk_spendRecipient, saltB_field, amount, tokenId)
	Commitment *big.Int

	// CiphertextI is the ML-KEM capsule (1088 bytes) produced by the sender via
	// Encapsulate(pk_viewRecipient). The recipient decapsulates it with sk_view to
	// recover saltB.
	CiphertextI []byte

	// CiphertextII is the ChaCha20-Poly1305 ciphertext of (tokenId || amount),
	// keyed by saltB. A decryption failure means this note is not addressed to
	// the scanning view key.
	CiphertextII []byte
}

// OwnedErc20Note is a note that Bob has confirmed belongs to him after scanning.
// It contains everything Bob needs to later spend the note (build a JoinSplit proof).
type OwnedErc20Note struct {
	// Commitment is the on-chain commitment value.
	Commitment *big.Int

	// TokenId recovered from ciphertextII.
	TokenId *big.Int

	// Amount recovered from ciphertextII.
	Amount *big.Int

	// SaltBField is saltB reduced mod SNARK_SCALAR_FIELD.
	// This is the witness value (WtSaltsIn) needed when spending this note.
	SaltBField *big.Int
}

// ScanForErc20Notes scans a list of on-chain ERC20 events and returns all notes
// addressed to the given view/spend key pair.
//
// For each event it:
//  1. Decapsulates ciphertextI using dk_view to recover saltB.
//  2. Attempts to decrypt ciphertextII with saltB; a failure means the note is not
//     for this recipient and it is silently skipped.
//  3. Recomputes Poseidon(pk_spend, saltB_field, amount, tokenId) and checks it
//     matches the on-chain commitment. A mismatch is returned as an error (it
//     indicates tampered or inconsistent data).
//
// dk is the recipient's ML-KEM decapsulation key (sk_view).
// pkSpend is the recipient's spend public key (pk_spend = Poseidon(sk_spend)).
func ScanForErc20Notes(
	dk *mlkem.DecapsulationKey768,
	pkSpend *big.Int,
	events []OnChainErc20Event,
) ([]OwnedErc20Note, error) {
	var owned []OwnedErc20Note

	for i, ev := range events {
		// Step 1: recover saltB from the ML-KEM capsule.
		saltB, err := Decapsulate(dk, ev.CiphertextI)
		if err != nil {
			// Internal KEM failure — unexpected, surface it.
			return nil, fmt.Errorf("event %d: decapsulation failed: %w", i, err)
		}

		// Step 2: try to decrypt the payload. Failure = note is not for us.
		tokenId, amount, err := DecryptPayload(saltB, ev.CiphertextII)
		if err != nil {
			// AEAD auth failure: note addressed to a different recipient; skip.
			continue
		}

		// Step 3: recompute commitment and verify it matches the chain.
		saltBField := SaltBToField(saltB)
		expected, err := Erc20CommitmentV2(pkSpend, saltBField, amount, tokenId)
		if err != nil {
			return nil, fmt.Errorf("event %d: failed to compute commitment: %w", i, err)
		}
		if expected.Cmp(ev.Commitment) != 0 {
			return nil, fmt.Errorf(
				"event %d: commitment mismatch — decrypted payload does not match on-chain commitment"+
					" (got %s, want %s)", i, expected.String(), ev.Commitment.String(),
			)
		}

		owned = append(owned, OwnedErc20Note{
			Commitment: new(big.Int).Set(ev.Commitment),
			TokenId:    tokenId,
			Amount:     amount,
			SaltBField: saltBField,
		})
	}

	return owned, nil
}

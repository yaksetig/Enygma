// Deterministic derivation of the local Hardhat node's demo accounts.
//
// The Hardhat node this demo talks to is configured (enygma_dvp/hardhat.config.js)
// with a fixed BIP-39 mnemonic and derivation path m/44'/60'/0'/0. Deriving the
// keys here — instead of hardcoding another copy of them — keeps the demo in
// lockstep with whatever the node hands out, and lets accountKey() serve any
// index the demo needs (0–9) rather than only the three in the config file.
//
// WARNING: this mnemonic is the repo's well-known local development mnemonic.
// The keys below are public. Never point this demo at a network holding value.
package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// hardhatMnemonic matches enygma_dvp/hardhat.config.js.
const hardhatMnemonic = "federal unhappy avoid mistake life barrel beauty raccoon recycle unknown review link"

// hardenedOffset is BIP-32's 2^31 boundary between hardened and normal children.
const hardenedOffset uint32 = 0x80000000

// extKey is a BIP-32 extended private key.
type extKey struct {
	key   []byte // 32-byte private key
	chain []byte // 32-byte chain code
}

// masterKey derives the BIP-32 master key from a BIP-39 mnemonic (empty passphrase).
func masterKey(mnemonic string) *extKey {
	seed, err := pbkdf2.Key(sha512.New, mnemonic, []byte("mnemonic"), 2048, 64)
	if err != nil {
		panic(fmt.Sprintf("pbkdf2: %v", err))
	}
	h := hmac.New(sha512.New, []byte("Bitcoin seed"))
	h.Write(seed)
	sum := h.Sum(nil)
	return &extKey{key: sum[:32], chain: sum[32:]}
}

// child derives the i-th child of k. Indices >= hardenedOffset are hardened.
func (k *extKey) child(i uint32) (*extKey, error) {
	data := make([]byte, 0, 37)
	if i >= hardenedOffset {
		data = append(data, 0x00)
		data = append(data, k.key...)
	} else {
		priv, err := crypto.ToECDSA(k.key)
		if err != nil {
			return nil, fmt.Errorf("parent key: %w", err)
		}
		data = append(data, crypto.CompressPubkey(&priv.PublicKey)...)
	}
	data = binary.BigEndian.AppendUint32(data, i)

	h := hmac.New(sha512.New, k.chain)
	h.Write(data)
	sum := h.Sum(nil)

	n := crypto.S256().Params().N
	childInt := new(big.Int).SetBytes(sum[:32])
	if childInt.Cmp(n) >= 0 {
		return nil, fmt.Errorf("derived key out of range at index %d", i)
	}
	childInt.Add(childInt, new(big.Int).SetBytes(k.key))
	childInt.Mod(childInt, n)
	if childInt.Sign() == 0 {
		return nil, fmt.Errorf("derived zero key at index %d", i)
	}
	out := make([]byte, 32)
	childInt.FillBytes(out)
	return &extKey{key: out, chain: sum[32:]}, nil
}

// accountKey returns the hex private key and address for Hardhat account `index`
// on the standard m/44'/60'/0'/0/<index> path.
func accountKey(index int) (privHex string, addr common.Address, err error) {
	k := masterKey(hardhatMnemonic)
	path := []uint32{44 + hardenedOffset, 60 + hardenedOffset, 0 + hardenedOffset, 0, uint32(index)}
	for _, step := range path {
		k, err = k.child(step)
		if err != nil {
			return "", common.Address{}, err
		}
	}
	priv, err := crypto.ToECDSA(k.key)
	if err != nil {
		return "", common.Address{}, err
	}
	return hex.EncodeToString(k.key), crypto.PubkeyToAddress(priv.PublicKey), nil
}

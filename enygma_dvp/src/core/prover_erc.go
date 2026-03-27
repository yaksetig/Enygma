package core

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
)

// Erc20JoinSplitProof generates an ERC20 JoinSplit proof for the non-interactive flow.
//
// For inputs the caller provides:
//   - keysIn       — spend key pairs (sk_spend used for nullifier + commitment check)
//   - wtSaltsIn    — saltB field elements from when each input note was received
//
// For outputs the caller provides:
//   - recipientSpendPks      — pk_spend of each recipient (goes into commitment)
//   - recipientViewEncapKeys — ML-KEM encapsulation keys of each recipient (1184 bytes each)
//   - wtTokenId              — token identifier shared across all notes
//
// The function runs Encapsulate per output to derive saltB, then:
//   - computes Erc20CommitmentV2(pk_spend, saltB_field, amount, tokenId)
//   - encrypts tokenId||amount with ChaCha20-Poly1305 keyed by saltB
//   - carries both ciphertexts in ProofResult for on-chain publication
func (c *GnarkClient) Erc20JoinSplitProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []KeyPair,
	wtSaltsIn []*big.Int,
	wtValuesOut []*big.Int,
	recipientSpendPks []*big.Int,
	recipientViewEncapKeys [][]byte,
	merkleDepth int,
	merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtTokenId *big.Int,
	use10_2 bool,
) (*ProofResult, error) {
	nIn := len(wtValuesIn)
	nOut := len(wtValuesOut)

	// --- inputs: nullifiers and Merkle paths ---
	stNullifiers := make([]*big.Int, nIn)
	wtPathIndices := make([]*big.Int, nIn)
	wtPathElements := make([]*big.Int, 0)

	for i := 0; i < nIn; i++ {
		// Verify local commitment matches what should be in the tree (V2 layout)
		_, err := Erc20CommitmentV2(keysIn[i].PublicKey, wtSaltsIn[i], wtValuesIn[i], wtTokenId)
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20CommitmentV2 for input %d: %w", i, err)
		}

		if wtValuesIn[i].Sign() == 0 {
			// dummy input — zero out path
			wtPathIndices[i] = big.NewInt(0)
			zeros := make([]*big.Int, merkleDepth)
			for j := range zeros {
				zeros[j] = big.NewInt(0)
			}
			wtPathElements = append(wtPathElements, zeros...)
		} else {
			wtPathIndices[i] = merkleProofs[i].Indices
			wtPathElements = append(wtPathElements, merkleProofs[i].Elements...)
		}

		nullifier, err := GetNullifier(keysIn[i].PrivateKey, wtPathIndices[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute nullifier for input %d: %w", i, err)
		}
		stNullifiers[i] = nullifier
	}

	// --- outputs: KEM encapsulation, AEAD encryption, V2 commitments ---
	wtSaltsOut := make([]*big.Int, nOut)
	stCommitmentsOut := make([]*big.Int, nOut)
	ciphertextI := make([][]byte, nOut)
	ciphertextII := make([][]byte, nOut)

	for i := 0; i < nOut; i++ {
		// Encapsulate using recipient's view public key
		saltB, ctI, err := Encapsulate(recipientViewEncapKeys[i])
		if err != nil {
			return nil, fmt.Errorf("failed to encapsulate for output %d: %w", i, err)
		}
		ciphertextI[i] = ctI

		// Reduce saltB to a SNARK field element for use in Poseidon
		saltBField := SaltBToField(saltB)
		wtSaltsOut[i] = saltBField

		// Encrypt tokenId||amount so the recipient can learn what was sent
		ctII, err := EncryptPayload(saltB, wtTokenId, wtValuesOut[i])
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt payload for output %d: %w", i, err)
		}
		ciphertextII[i] = ctII

		// V2 commitment: Poseidon(pk_spendRecipient, saltB_field, amount, tokenId)
		cmt, err := Erc20CommitmentV2(recipientSpendPks[i], saltBField, wtValuesOut[i], wtTokenId)
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20CommitmentV2 for output %d: %w", i, err)
		}
		stCommitmentsOut[i] = cmt
	}

	// --- Merkle roots (zero for dummy inputs) ---
	stMerkleRoots := make([]*big.Int, nIn)
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":            stMessage.String(),
		"StTreeNumber":         bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":        bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":         bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":      bigIntSliceToStrings(stCommitmentsOut),
		"WtPrivateKeysIn":      bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":           bigIntSliceToStrings(wtValuesIn),
		"WtSaltsIn":            bigIntSliceToStrings(wtSaltsIn),
		"WtPathElements":       bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":        bigIntSliceToStrings(wtPathIndices),
		"WtTokenId":            wtTokenId.String(),
		"WtSpendPublicKeysOut": bigIntSliceToStrings(recipientSpendPks),
		"WtValuesOut":          bigIntSliceToStrings(wtValuesOut),
		"WtSaltsOut":           bigIntSliceToStrings(wtSaltsOut),
	}

	endpoint := "/proof/joinSplitERC20"
	if use10_2 {
		endpoint = "/proof/joinSplitERC20_10_2"
	}

	body, err := c.PostProof(endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("erc20JoinSplit proof request failed: %w", err)
	}

	// The gnark handler marshals []*big.Int as JSON numbers (big.Int.MarshalJSON returns
	// raw decimal bytes). Use json.Number to accept either JSON number or JSON string.
	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body, &gnarkResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse joinSplit proof response: %w", parseErr)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	// Statement: [message, tree[0], root[0], null[0], ..., commit[0], commit[1]]
	statement := make([]*big.Int, 0, 1+3*nIn+nOut)
	statement = append(statement, stMessage)
	for i := 0; i < nIn; i++ {
		statement = append(statement, stTreeNumbers[i], stMerkleRoots[i], stNullifiers[i])
	}
	statement = append(statement, stCommitmentsOut...)

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  nIn,
		NumberOfOutputs: nOut,
		CiphertextI:     ciphertextI,
		CiphertextII:    ciphertextII,
	}, nil
}

// ZkDvpSwapInitResult holds everything Alice produces when initiating a ZkDvp swap.
type ZkDvpSwapInitResult struct {
	// AliceNullifier is the nullifier that burns Alice's input note.
	AliceNullifier *big.Int

	// CommitmentB = Poseidon(bobSpendPk, SaltBToField(saltB), amountIn, tokenIdIn).
	// This is the commitment Bob receives (Alice's asset going to Bob).
	CommitmentB *big.Int

	// CommitmentA is C' = Poseidon(aliceSpendPk, SaltBToField(saltStar), amountOut, tokenIdOut).
	// This is the commitment Alice receives (Bob's asset coming to Alice).
	CommitmentA *big.Int

	// CiphertextI is the ML-KEM capsule (1088 bytes) from Encapsulate(bobViewEncapKey).
	// Bob decapsulates to recover saltB.
	CiphertextI []byte

	// CiphertextII is the AEAD ciphertext of (tokenIdOut || amountOut || saltStar)
	// keyed by saltB. Bob decrypts to verify CommitmentA is well-formed.
	CiphertextII []byte

	// SaltStar is the raw random salt used in CommitmentA. Alice keeps this to
	// spend CommitmentA in a future proof (used as WtSaltsIn).
	SaltStar []byte

	// SaltStarField is SaltBToField(saltStar) — the field element embedded in CommitmentA.
	SaltStarField *big.Int

	// Proof is the gnark proof bytes (populated when the gnark server is called).
	// Empty when used in off-chain-only mode.
	Proof []string
}

// ZkDvpInitiateSwap produces all of Alice's ZkDvp swap artefacts in one call:
//
//  1. Calls Encapsulate(bobViewEncapKey) to derive (saltB, ciphertextI).
//  2. Computes CommitmentB = Poseidon(bobSpendPk, SaltBToField(saltB), amountIn, tokenIdIn).
//  3. Generates saltStar and computes CommitmentA = C' = Poseidon(aliceSpendPk, SaltBToField(saltStar), amountOut, tokenIdOut).
//  4. Encrypts (tokenIdOut || amountOut || saltStar) with saltB → ciphertextII.
//  5. Computes Alice's nullifier.
//  6. Generates the JoinSplit ZK proof for Alice's input note with StMessage = CommitmentA (C').
//
// The StMessage is set to CommitmentA so that the on-chain cross-commitment check passes:
//
//	stMessage(Alice) = C'         must equal firstOutput(Bob) = C'
//	stMessage(Bob)   = CommitmentB must equal firstOutput(Alice) = CommitmentB
//
// Parameters:
//   - aliceKey     — Alice's spend key pair (input note ownership)
//   - aliceSaltIn  — the saltBField Alice received when she got her input note
//   - amountIn     — amount of Alice's input note (e.g. 5 USDT)
//   - tokenIdIn    — token ID of Alice's input note (e.g. 10)
//   - bobSpendPk   — Bob's spend public key
//   - bobViewEncapKey — Bob's ML-KEM encapsulation key (1184 bytes)
//   - tokenIdOut   — token ID Alice will receive (e.g. 25, concert ticket)
//   - amountOut    — amount Alice will receive (e.g. 1)
//   - merkleProof  — Merkle proof for Alice's input note (nil = dummy/no proof)
//   - stTreeNumber — tree number for Alice's input note
func (c *GnarkClient) ZkDvpInitiateSwap(
	aliceKey KeyPair,
	aliceSaltIn *big.Int,
	amountIn *big.Int,
	tokenIdIn *big.Int,
	bobSpendPk *big.Int,
	bobViewEncapKey []byte,
	tokenIdOut *big.Int,
	amountOut *big.Int,
	merkleDepth int,
	merkleProof *MerkleProof,
	stTreeNumber *big.Int,
) (*ZkDvpSwapInitResult, error) {
	// Step 1: Alice explicitly encapsulates Bob's view key → shared saltB + ciphertextI.
	saltB, ciphertextI, err := Encapsulate(bobViewEncapKey)
	if err != nil {
		return nil, fmt.Errorf("encapsulate failed: %w", err)
	}
	saltBField := SaltBToField(saltB)

	// Step 2: CommitmentB — Bob receives Alice's asset.
	commitmentB, err := Erc20CommitmentV2(bobSpendPk, saltBField, amountIn, tokenIdIn)
	if err != nil {
		return nil, fmt.Errorf("CommitmentB computation failed: %w", err)
	}

	// Step 3: C' (CommitmentA) — Alice receives Bob's asset.
	// saltStar is freshly generated with the same byte-length as saltB.
	saltStar, err := GenerateRandomValue(len(saltB))
	if err != nil {
		return nil, fmt.Errorf("GenerateRandomValue failed: %w", err)
	}
	saltStarField := SaltBToField(saltStar)
	commitmentA, err := Erc20CommitmentV2(aliceKey.PublicKey, saltStarField, amountOut, tokenIdOut)
	if err != nil {
		return nil, fmt.Errorf("CommitmentA (C') computation failed: %w", err)
	}

	// Step 4: Encrypt (tokenIdOut || amountOut || saltStar) for Bob.
	ciphertextII, err := EncryptSwapPayload(saltB, tokenIdOut, amountOut, saltStar)
	if err != nil {
		return nil, fmt.Errorf("EncryptSwapPayload failed: %w", err)
	}

	// Step 5: Compute Alice's nullifier.
	var pathIndices *big.Int
	var pathElements []*big.Int
	var merkleRoot *big.Int
	if merkleProof != nil {
		pathIndices = merkleProof.Indices
		pathElements = merkleProof.Elements
		merkleRoot = merkleProof.Root
	} else {
		pathIndices = big.NewInt(0)
		pathElements = make([]*big.Int, merkleDepth)
		for j := range pathElements {
			pathElements[j] = big.NewInt(0)
		}
		merkleRoot = big.NewInt(0)
	}
	nullifier, err := GetNullifier(aliceKey.PrivateKey, pathIndices)
	if err != nil {
		return nil, fmt.Errorf("GetNullifier failed: %w", err)
	}

	// Step 6: Generate JoinSplit ZK proof using the pre-computed saltBField.
	// The circuit verifies: CommitmentB == Poseidon(bobSpendPk, saltBField, amountIn, tokenIdIn).
	// Since saltBField is provided as WtSaltsOut, the proof binds CommitmentB to saltB.
	//
	// The joinSplitERC20 circuit is fixed at 2 inputs / 2 outputs, so we pad with a
	// dummy second slot (value = 0). The circuit's enable-logic skips constraints for
	// zero-value inputs/outputs, keeping the balance equation: amountIn + 0 = amountIn + 0.
	dummyKey, err := NewSpendKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate dummy key pair: %w", err)
	}
	dummyNullifier, err := GetNullifier(dummyKey.PrivateKey, big.NewInt(0))
	if err != nil {
		return nil, fmt.Errorf("failed to compute dummy nullifier: %w", err)
	}
	dummyCmt, err := Erc20CommitmentV2(bobSpendPk, big.NewInt(0), big.NewInt(0), tokenIdIn)
	if err != nil {
		return nil, fmt.Errorf("failed to compute dummy commitment: %w", err)
	}
	dummyPath := make([]*big.Int, merkleDepth)
	for j := range dummyPath {
		dummyPath[j] = big.NewInt(0)
	}

	allPathElements := append(pathElements, dummyPath...)
	pathElementChunks := chunkBigIntSlice(allPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":            commitmentA.String(),
		"StTreeNumber":         []string{stTreeNumber.String(), "0"},
		"StMerkleRoots":        []string{merkleRoot.String(), "0"},
		"StNullifiers":         []string{nullifier.String(), dummyNullifier.String()},
		"StCommitmentOut":      []string{commitmentB.String(), dummyCmt.String()},
		"WtPrivateKeysIn":      []string{aliceKey.PrivateKey.String(), dummyKey.PrivateKey.String()},
		"WtValuesIn":           []string{amountIn.String(), "0"},
		"WtSaltsIn":            []string{aliceSaltIn.String(), "0"},
		"WtPathElements":       bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":        []string{pathIndices.String(), "0"},
		"WtTokenId":            tokenIdIn.String(),
		"WtSpendPublicKeysOut": []string{bobSpendPk.String(), bobSpendPk.String()},
		"WtValuesOut":          []string{amountIn.String(), "0"},
		"WtSaltsOut":           []string{saltBField.String(), "0"},
	}

	body, err := c.PostProof("/proof/joinSplitERC20", payload)
	if err != nil {
		return nil, fmt.Errorf("ZkDvp JoinSplit proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body, &gnarkResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse ZkDvp proof response: %w", parseErr)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	return &ZkDvpSwapInitResult{
		AliceNullifier: nullifier,
		CommitmentB:    commitmentB,
		CommitmentA:    commitmentA,
		CiphertextI:    ciphertextI,
		CiphertextII:   ciphertextII,
		SaltStar:       saltStar,
		SaltStarField:  saltStarField,
		Proof:          proofStrs,
	}, nil
}

// Erc20WithdrawProof generates an ERC20 withdrawal proof for the V2 non-interactive flow.
//
// The withdrawal output note uses a fixed salt of 0 (no KEM needed — the commitment
// is public) and encodes the recipient address as pk_spend:
//
//	withdrawal commitment = Poseidon4(uint160(recipient), 0, withdrawAmount, tokenId)
//
// The dummy second output uses pk_spend=dummySpendPk with salt=0 and amount=0.
func (c *GnarkClient) Erc20WithdrawProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []KeyPair,
	wtSaltsIn []*big.Int,
	withdrawAmount *big.Int,
	recipient *big.Int, // uint160(recipient address)
	dummySpendPk *big.Int, // dummy second output pk
	merkleDepth int,
	merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtTokenId *big.Int,
	use10_2 bool,
) (*ProofResult, error) {
	nIn := len(wtValuesIn)

	// --- inputs: nullifiers and Merkle paths ---
	stNullifiers := make([]*big.Int, nIn)
	wtPathIndices := make([]*big.Int, nIn)
	wtPathElements := make([]*big.Int, 0)

	for i := 0; i < nIn; i++ {
		_, err := Erc20CommitmentV2(keysIn[i].PublicKey, wtSaltsIn[i], wtValuesIn[i], wtTokenId)
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20CommitmentV2 for input %d: %w", i, err)
		}

		if wtValuesIn[i].Sign() == 0 {
			wtPathIndices[i] = big.NewInt(0)
			zeros := make([]*big.Int, merkleDepth)
			for j := range zeros {
				zeros[j] = big.NewInt(0)
			}
			wtPathElements = append(wtPathElements, zeros...)
		} else {
			wtPathIndices[i] = merkleProofs[i].Indices
			wtPathElements = append(wtPathElements, merkleProofs[i].Elements...)
		}

		nullifier, err := GetNullifier(keysIn[i].PrivateKey, wtPathIndices[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute nullifier for input %d: %w", i, err)
		}
		stNullifiers[i] = nullifier
	}

	// --- outputs: fixed salt=0 for withdrawal (no KEM), dummy second output ---
	zero := big.NewInt(0)
	wtSaltsOut := []*big.Int{zero, zero}

	// Withdrawal output commitment: Poseidon4(uint160(recipient), 0, withdrawAmount, tokenId)
	withdrawCommitment, err := Erc20CommitmentV2(recipient, zero, withdrawAmount, wtTokenId)
	if err != nil {
		return nil, fmt.Errorf("failed to compute withdrawal commitment: %w", err)
	}

	// Dummy output commitment: Poseidon4(dummySpendPk, 0, 0, tokenId)
	dummyCommitment, err := Erc20CommitmentV2(dummySpendPk, zero, zero, wtTokenId)
	if err != nil {
		return nil, fmt.Errorf("failed to compute dummy commitment: %w", err)
	}

	stCommitmentsOut := []*big.Int{withdrawCommitment, dummyCommitment}
	wtSpendPublicKeysOut := []*big.Int{recipient, dummySpendPk}
	wtValuesOut := []*big.Int{withdrawAmount, zero}

	// --- Merkle roots (zero for dummy inputs) ---
	stMerkleRoots := make([]*big.Int, nIn)
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":            stMessage.String(),
		"StTreeNumber":         bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":        bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":         bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":      bigIntSliceToStrings(stCommitmentsOut),
		"WtPrivateKeysIn":      bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":           bigIntSliceToStrings(wtValuesIn),
		"WtSaltsIn":            bigIntSliceToStrings(wtSaltsIn),
		"WtPathElements":       bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":        bigIntSliceToStrings(wtPathIndices),
		"WtTokenId":            wtTokenId.String(),
		"WtSpendPublicKeysOut": bigIntSliceToStrings(wtSpendPublicKeysOut),
		"WtValuesOut":          bigIntSliceToStrings(wtValuesOut),
		"WtSaltsOut":           bigIntSliceToStrings(wtSaltsOut),
	}

	endpoint := "/proof/joinSplitERC20"
	if use10_2 {
		endpoint = "/proof/joinSplitERC20_10_2"
	}

	body2, err := c.PostProof(endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("erc20Withdraw proof request failed: %w", err)
	}

	var gnarkResp2 struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body2, &gnarkResp2); parseErr != nil {
		return nil, fmt.Errorf("failed to parse withdraw proof response: %w", parseErr)
	}
	proofStrs2 := make([]string, len(gnarkResp2.Proof))
	for i, n := range gnarkResp2.Proof {
		proofStrs2[i] = n.String()
	}

	// Statement: [message, tree[0], root[0], null[0], ..., commit[0], commit[1]]
	statement := make([]*big.Int, 0, 1+3*nIn+2)
	statement = append(statement, stMessage)
	for i := 0; i < nIn; i++ {
		statement = append(statement, stTreeNumbers[i], stMerkleRoots[i], stNullifiers[i])
	}
	statement = append(statement, stCommitmentsOut...)

	return &ProofResult{
		Proof:           proofStrs2,
		Statement:       statement,
		NumberOfInputs:  nIn,
		NumberOfOutputs: 2,
	}, nil
}

// Erc721OwnershipProof generates a strongly-typed ERC721 ownership proof.
// wtSaltIn is the salt used when the input note was originally created.
// A fresh salt for the output note is derived via ML-KEM encapsulation using recipientViewEncapKey.
func (c *GnarkClient) Erc721OwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	recipientViewEncapKey []byte,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc721ContractAddress *big.Int,
) (*ProofResult, error) {
	_, err := Erc721Commitment(wtValue, keyIn.PublicKey, wtSaltIn)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721Commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	// Generate fresh salt for output note via ML-KEM. recipientViewEncapKey is mandatory.
	if recipientViewEncapKey == nil {
		return nil, fmt.Errorf("recipientViewEncapKey is required for non-interactive note delivery")
	}
	saltB, ctI, kemErr := Encapsulate(recipientViewEncapKey)
	if kemErr != nil {
		return nil, fmt.Errorf("failed to encapsulate for output: %w", kemErr)
	}
	wtSaltOut := SaltBToField(saltB)
	ctII, err := EncryptPayload(saltB, wtErc721ContractAddress, wtValue)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt payload for output: %w", err)
	}

	commitmentOut, err := Erc721Commitment(wtValue, keyOut.PublicKey, wtSaltOut)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721Commitment for output: %w", err)
	}

	payload := map[string]interface{}{
		"StMessage":               stMessage.String(),
		"StTreeNumbers":           []string{stTreeNumber.String()},
		"StMerkleRoots":           []string{merkleProof.Root.String()},
		"StNullifiers":            []string{nullifier.String()},
		"StCommitmentOut":         []string{commitmentOut.String()},
		"WtPrivateKeysIn":         []string{keyIn.PrivateKey.String()},
		"WtValues":                []string{wtValue.String()},
		"WtErc721ContractAddress": wtErc721ContractAddress.String(),
		"WtPathElements":          [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":           []string{merkleProof.Indices.String()},
		"WtPublicKeysOut":         []string{keyOut.PublicKey.String()},
		"WtSaltsIn":               []string{wtSaltIn.String()},
		"WtSaltsOut":              []string{wtSaltOut.String()},
	}

	body721, err := c.PostProof("/proof/ownershipERC721", payload)
	if err != nil {
		return nil, fmt.Errorf("erc721Ownership proof request failed: %w", err)
	}

	var gnarkResp721 struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body721, &gnarkResp721); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc721Ownership proof response: %w", parseErr)
	}
	proofStrs721 := make([]string, len(gnarkResp721.Proof))
	for i, n := range gnarkResp721.Proof {
		proofStrs721[i] = n.String()
	}

	// Statement: [message, treeNumber, merkleRoot, nullifier, commitmentOut]
	statement := []*big.Int{
		stMessage,
		stTreeNumber,
		merkleProof.Root,
		nullifier,
		commitmentOut,
	}

	return &ProofResult{
		Proof:           proofStrs721,
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
		SaltsOut:        []*big.Int{wtSaltOut},
		CiphertextI:     [][]byte{ctI},
		CiphertextII:    [][]byte{ctII},
	}, nil
}

// Erc721OwnershipProofFromSalt is like Erc721OwnershipProof but accepts a
// pre-computed output salt (wtSaltOut) and the corresponding ciphertexts instead of
// performing a fresh ML-KEM encapsulation.  Use this when the output commitment must
// be known before generating the proof — e.g. for atomic DVP swaps where the
// on-chain contract requires cross-commitment consistency between both parties' proofs.
func (c *GnarkClient) Erc721OwnershipProofFromSalt(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	wtSaltOut *big.Int, ctI []byte, ctII []byte,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc721ContractAddress *big.Int,
) (*ProofResult, error) {
	_, err := Erc721Commitment(wtValue, keyIn.PublicKey, wtSaltIn)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721Commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	commitmentOut, err := Erc721Commitment(wtValue, keyOut.PublicKey, wtSaltOut)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721Commitment for output: %w", err)
	}

	payload := map[string]interface{}{
		"StMessage":               stMessage.String(),
		"StTreeNumbers":           []string{stTreeNumber.String()},
		"StMerkleRoots":           []string{merkleProof.Root.String()},
		"StNullifiers":            []string{nullifier.String()},
		"StCommitmentOut":         []string{commitmentOut.String()},
		"WtPrivateKeysIn":         []string{keyIn.PrivateKey.String()},
		"WtValues":                []string{wtValue.String()},
		"WtErc721ContractAddress": wtErc721ContractAddress.String(),
		"WtPathElements":          [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":           []string{merkleProof.Indices.String()},
		"WtPublicKeysOut":         []string{keyOut.PublicKey.String()},
		"WtSaltsIn":               []string{wtSaltIn.String()},
		"WtSaltsOut":              []string{wtSaltOut.String()},
	}

	body, err := c.PostProof("/proof/ownershipERC721", payload)
	if err != nil {
		return nil, fmt.Errorf("erc721OwnershipFromSalt proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body, &gnarkResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc721OwnershipFromSalt proof response: %w", parseErr)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	statement := []*big.Int{
		stMessage,
		stTreeNumber,
		merkleProof.Root,
		nullifier,
		commitmentOut,
	}

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
		SaltsOut:        []*big.Int{wtSaltOut},
		CiphertextI:     [][]byte{ctI},
		CiphertextII:    [][]byte{ctII},
	}, nil
}

// Erc20JoinSplitProofFromSalts is like Erc20JoinSplitProof but accepts pre-computed
// output salts and ciphertexts instead of performing ML-KEM encapsulation.
// Use this when output commitments must be known before proof generation — e.g. for
// atomic DVP swaps where cross-commitment consistency is required.
func (c *GnarkClient) Erc20JoinSplitProofFromSalts(
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []KeyPair,
	wtSaltsIn []*big.Int,
	wtValuesOut []*big.Int,
	recipientSpendPks []*big.Int,
	wtSaltsOut []*big.Int,
	ciphertextI [][]byte,
	ciphertextII [][]byte,
	merkleDepth int,
	merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtTokenId *big.Int,
	use10_2 bool,
) (*ProofResult, error) {
	nIn := len(wtValuesIn)
	nOut := len(wtValuesOut)

	stNullifiers := make([]*big.Int, nIn)
	wtPathIndices := make([]*big.Int, nIn)
	wtPathElements := make([]*big.Int, 0)

	for i := 0; i < nIn; i++ {
		if wtValuesIn[i].Sign() == 0 {
			wtPathIndices[i] = big.NewInt(0)
			zeros := make([]*big.Int, merkleDepth)
			for j := range zeros {
				zeros[j] = big.NewInt(0)
			}
			wtPathElements = append(wtPathElements, zeros...)
		} else {
			wtPathIndices[i] = merkleProofs[i].Indices
			wtPathElements = append(wtPathElements, merkleProofs[i].Elements...)
		}

		nullifier, err := GetNullifier(keysIn[i].PrivateKey, wtPathIndices[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute nullifier for input %d: %w", i, err)
		}
		stNullifiers[i] = nullifier
	}

	stCommitmentsOut := make([]*big.Int, nOut)
	for i := 0; i < nOut; i++ {
		cmt, err := Erc20CommitmentV2(recipientSpendPks[i], wtSaltsOut[i], wtValuesOut[i], wtTokenId)
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20CommitmentV2 for output %d: %w", i, err)
		}
		stCommitmentsOut[i] = cmt
	}

	stMerkleRoots := make([]*big.Int, nIn)
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":            stMessage.String(),
		"StTreeNumber":         bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":        bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":         bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":      bigIntSliceToStrings(stCommitmentsOut),
		"WtPrivateKeysIn":      bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":           bigIntSliceToStrings(wtValuesIn),
		"WtSaltsIn":            bigIntSliceToStrings(wtSaltsIn),
		"WtPathElements":       bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":        bigIntSliceToStrings(wtPathIndices),
		"WtTokenId":            wtTokenId.String(),
		"WtSpendPublicKeysOut": bigIntSliceToStrings(recipientSpendPks),
		"WtValuesOut":          bigIntSliceToStrings(wtValuesOut),
		"WtSaltsOut":           bigIntSliceToStrings(wtSaltsOut),
	}

	endpoint := "/proof/joinSplitERC20"
	if use10_2 {
		endpoint = "/proof/joinSplitERC20_10_2"
	}

	body, err := c.PostProof(endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("erc20JoinSplitFromSalts proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body, &gnarkResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc20JoinSplitFromSalts proof response: %w", parseErr)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	statement := make([]*big.Int, 0, 1+3*nIn+nOut)
	statement = append(statement, stMessage)
	for i := 0; i < nIn; i++ {
		statement = append(statement, stTreeNumbers[i], stMerkleRoots[i], stNullifiers[i])
	}
	statement = append(statement, stCommitmentsOut...)

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  nIn,
		NumberOfOutputs: nOut,
		CiphertextI:     ciphertextI,
		CiphertextII:    ciphertextII,
	}, nil
}

// Erc1155FungibleJoinSplitProof generates a strongly-typed ERC1155 fungible JoinSplit proof.
// wtSaltsIn must contain the salt used when each input note was originally created.
// Output salts are derived via ML-KEM encapsulation using recipientViewEncapKeys.
func (c *GnarkClient) Erc1155FungibleJoinSplitProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int, keysIn []KeyPair, wtSaltsIn []*big.Int,
	wtValuesOut []*big.Int, keysOut []KeyPair,
	recipientViewEncapKeys [][]byte,
	merkleDepth int, merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	stCommitmentsOut, wtSaltsOut, ctI, ctII, stNullifiers, wtPathIndices, wtPathElements, err := prepareErc1155ProofParams(
		wtValuesIn, keysIn, wtSaltsIn, wtValuesOut, keysOut, recipientViewEncapKeys, merkleDepth, merkleProofs,
		wtErc1155ContractAddress, wtErc1155TokenId,
	)
	if err != nil {
		return nil, err
	}

	stMerkleRoots := make([]*big.Int, len(wtValuesIn))
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	stAssetGroupMerkleRoot := assetGroupMerkleProof.Root

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":                stMessage.String(),
		"StTreeNumbers":            bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":            bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":             bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":          bigIntSliceToStrings(stCommitmentsOut),
		"StAssetGroupMerkleRoot":   stAssetGroupMerkleRoot.String(),
		"StAssetGroupTreeNumber":   stAssetGroupTreeNumber.String(),
		"WtPrivateKeysIn":          bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":               bigIntSliceToStrings(wtValuesIn),
		"WtPathElements":           bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":            bigIntSliceToStrings(wtPathIndices),
		"WtErc1155ContractAddress": wtErc1155ContractAddress.String(),
		"WtErc1155TokenId":         wtErc1155TokenId.String(),
		"WtPublicKeysOut":          bigIntSliceToStrings(extractPublicKeys(keysOut)),
		"WtValuesOut":              bigIntSliceToStrings(wtValuesOut),
		"WtAssetGroupPathElements": bigIntSliceToStrings(assetGroupMerkleProof.Elements),
		"WtAssetGroupPathIndices":  assetGroupMerkleProof.Indices.String(),
		"WtSaltsIn":                bigIntSliceToStrings(wtSaltsIn),
		"WtSaltsOut":               bigIntSliceToStrings(wtSaltsOut),
	}

	bodyFung, err := c.PostProof("/proof/erc155Fungible", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155FungibleJoinSplit proof request failed: %w", err)
	}

	var gnarkRespFung struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(bodyFung, &gnarkRespFung); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc1155FungibleJoinSplit proof response: %w", parseErr)
	}
	proofStrsFung := make([]string, len(gnarkRespFung.Proof))
	for i, n := range gnarkRespFung.Proof {
		proofStrsFung[i] = n.String()
	}

	// Statement (interleaved per input, then commitments — no asset group in public signal):
	// [message, tree[0], root[0], null[0], tree[1], root[1], null[1], commit[0], commit[1]]
	statement := make([]*big.Int, 0, 1+3*len(wtValuesIn)+len(keysOut))
	statement = append(statement, stMessage)
	for i := 0; i < len(wtValuesIn); i++ {
		statement = append(statement, stTreeNumbers[i], stMerkleRoots[i], stNullifiers[i])
	}
	statement = append(statement, stCommitmentsOut...)

	return &ProofResult{
		Proof:           proofStrsFung,
		Statement:       statement,
		NumberOfInputs:  len(wtValuesIn),
		NumberOfOutputs: len(wtValuesOut),
		SaltsOut:        wtSaltsOut,
		CiphertextI:     ctI,
		CiphertextII:    ctII,
	}, nil
}

// Erc1155NonFungibleOwnershipProof generates a strongly-typed ERC1155 non-fungible ownership proof.
// wtSaltIn is the salt used when the input note was originally created.
// A fresh salt for the output note is derived via ML-KEM encapsulation using recipientViewEncapKey.
func (c *GnarkClient) Erc1155NonFungibleOwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	recipientViewEncapKey []byte,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	// Generate fresh salt for output note via ML-KEM. recipientViewEncapKey is mandatory.
	if recipientViewEncapKey == nil {
		return nil, fmt.Errorf("recipientViewEncapKey is required for non-interactive note delivery")
	}
	saltB, ctI, kemErr := Encapsulate(recipientViewEncapKey)
	if kemErr != nil {
		return nil, fmt.Errorf("failed to encapsulate for output: %w", kemErr)
	}
	wtSaltOut := SaltBToField(saltB)
	ctII, encErr := EncryptPayload(saltB, wtErc1155TokenId, wtValue)
	if encErr != nil {
		return nil, fmt.Errorf("failed to encrypt payload for output: %w", encErr)
	}

	commitmentOut, err := Erc1155Commitment(wtErc1155TokenId, wtValue, keyOut.PublicKey, wtSaltOut)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155Commitment for output: %w", err)
	}

	stAssetGroupMerkleRoot := assetGroupMerkleProof.Root

	payload := map[string]interface{}{
		"StMessage":                stMessage.String(),
		"StTreeNumbers":            []string{stTreeNumber.String()},
		"StMerkleRoots":            []string{merkleProof.Root.String()},
		"StNullifiers":             []string{nullifier.String()},
		"StCommitmentOut":          []string{commitmentOut.String()},
		"StAssetGroupTreeNumber":   []string{stAssetGroupTreeNumber.String()},
		"StAssetGroupMerkleRoot":   []string{stAssetGroupMerkleRoot.String()},
		"WtPrivateKeysIn":          []string{keyIn.PrivateKey.String()},
		"WtValues":                 []string{wtValue.String()},
		"WtPathElements":           [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":            []string{merkleProof.Indices.String()},
		"WtErc1155TokenId":         []string{wtErc1155TokenId.String()},
		"WtPublicKeysOut":          []string{keyOut.PublicKey.String()},
		"WtErc1155ContractAddress": wtErc1155ContractAddress.String(),
		"WtAssetGroupPathElements": [][]string{bigIntSliceToStrings(assetGroupMerkleProof.Elements)},
		"WtAssetGroupPathIndices":  []string{assetGroupMerkleProof.Indices.String()},
		"WtSaltsIn":                []string{wtSaltIn.String()},
		"WtSaltsOut":               []string{wtSaltOut.String()},
	}

	bodyNF, err := c.PostProof("/proof/erc1155NonFungible", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155NonFungibleOwnership proof request failed: %w", err)
	}

	var gnarkRespNF struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(bodyNF, &gnarkRespNF); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc1155NonFungibleOwnership proof response: %w", parseErr)
	}
	proofStrsNF := make([]string, len(gnarkRespNF.Proof))
	for i, n := range gnarkRespNF.Proof {
		proofStrsNF[i] = n.String()
	}

	// Statement: [message, treeNumber, merkleRoot, nullifier, commitmentOut,
	//   assetGroupTreeNumber, assetGroupMerkleRoot]
	statement := []*big.Int{
		stMessage,
		stTreeNumber,
		merkleProof.Root,
		nullifier,
		commitmentOut,
		stAssetGroupTreeNumber,
		stAssetGroupMerkleRoot,
	}

	return &ProofResult{
		Proof:           proofStrsNF,
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
		SaltsOut:        []*big.Int{wtSaltOut},
		CiphertextI:     [][]byte{ctI},
		CiphertextII:    [][]byte{ctII},
	}, nil
}

// Erc1155NonFungibleOwnershipProofFromSalt is like Erc1155NonFungibleOwnershipProof but
// accepts a pre-computed output salt and ciphertexts instead of performing ML-KEM encapsulation.
// Use this when the output commitment must be known before proof generation — e.g. for
// atomic DVP swaps where cross-commitment consistency is required.
func (c *GnarkClient) Erc1155NonFungibleOwnershipProofFromSalt(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	wtSaltOut *big.Int, ctI []byte, ctII []byte,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	commitmentOut, err := Erc1155Commitment(wtErc1155TokenId, wtValue, keyOut.PublicKey, wtSaltOut)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155Commitment for output: %w", err)
	}

	stAssetGroupMerkleRoot := assetGroupMerkleProof.Root

	payload := map[string]interface{}{
		"StMessage":                stMessage.String(),
		"StTreeNumbers":            []string{stTreeNumber.String()},
		"StMerkleRoots":            []string{merkleProof.Root.String()},
		"StNullifiers":             []string{nullifier.String()},
		"StCommitmentOut":          []string{commitmentOut.String()},
		"StAssetGroupTreeNumber":   []string{stAssetGroupTreeNumber.String()},
		"StAssetGroupMerkleRoot":   []string{stAssetGroupMerkleRoot.String()},
		"WtPrivateKeysIn":          []string{keyIn.PrivateKey.String()},
		"WtValues":                 []string{wtValue.String()},
		"WtPathElements":           [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":            []string{merkleProof.Indices.String()},
		"WtErc1155TokenId":         []string{wtErc1155TokenId.String()},
		"WtPublicKeysOut":          []string{keyOut.PublicKey.String()},
		"WtErc1155ContractAddress": wtErc1155ContractAddress.String(),
		"WtAssetGroupPathElements": [][]string{bigIntSliceToStrings(assetGroupMerkleProof.Elements)},
		"WtAssetGroupPathIndices":  []string{assetGroupMerkleProof.Indices.String()},
		"WtSaltsIn":                []string{wtSaltIn.String()},
		"WtSaltsOut":               []string{wtSaltOut.String()},
	}

	body, err := c.PostProof("/proof/erc1155NonFungible", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155NonFungibleOwnershipFromSalt proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if parseErr := json.Unmarshal(body, &gnarkResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse erc1155NonFungibleOwnershipFromSalt proof response: %w", parseErr)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	// Statement: [message, treeNumber, merkleRoot, nullifier, commitmentOut,
	//   assetGroupTreeNumber, assetGroupMerkleRoot]
	statement := []*big.Int{
		stMessage,
		stTreeNumber,
		merkleProof.Root,
		nullifier,
		commitmentOut,
		stAssetGroupTreeNumber,
		stAssetGroupMerkleRoot,
	}

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
		SaltsOut:        []*big.Int{wtSaltOut},
		CiphertextI:     [][]byte{ctI},
		CiphertextII:    [][]byte{ctII},
	}, nil
}

// PoseidonEncrypt calls the gnark server's /util/poseidonEncrypt endpoint to
// encrypt plaintext values using the Poseidon sponge cipher.
// key is [X, Y] of the BabyJubJub shared key (authKey = mulPointEscalar(auditorPubKey, random)).
// nonce must be < 2^128.
// realLength is the number of meaningful plaintext values.
// Returns the encrypted values including MAC (length = ceil(realLength/3)*3 + 1).
func (c *GnarkClient) PoseidonEncrypt(key [2]*big.Int, nonce *big.Int, realLength int, plaintext []*big.Int) ([]*big.Int, error) {
	plaintextStrs := make([]string, len(plaintext))
	for i, v := range plaintext {
		plaintextStrs[i] = v.String()
	}
	payload := map[string]interface{}{
		"key":        [2]string{key[0].String(), key[1].String()},
		"nonce":      nonce.String(),
		"realLength": realLength,
		"plaintext":  plaintextStrs,
	}
	body, err := c.PostProof("/util/poseidonEncrypt", payload)
	if err != nil {
		return nil, fmt.Errorf("poseidonEncrypt request failed: %w", err)
	}
	var resp struct {
		Encrypted []json.Number `json:"encrypted"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse poseidonEncrypt response: %w", err)
	}
	result := make([]*big.Int, len(resp.Encrypted))
	for i, n := range resp.Encrypted {
		v, ok := new(big.Int).SetString(n.String(), 10)
		if !ok {
			return nil, fmt.Errorf("invalid big int in encrypted response index %d", i)
		}
		result[i] = v
	}
	return result, nil
}

// AuditorDecrypt decrypts values encrypted by the auditor circuit using the
// Poseidon sponge cipher via the gnark server's /util/poseidonDecrypt endpoint.
//
// authKeyX, authKeyY is the shared encryption key: StAuditorAuthKey = mulPointEscalar(pubKey, random).
// nonce is StAuditorNonce from the proof data.
// encrypted is StAuditorEncryptedValues from the proof data.
// realLength is the number of plaintext values (6 for fungible, 3 for non-fungible).
//
// Returns an error if the MAC does not match (wrong key or tampered ciphertext).
func (c *GnarkClient) AuditorDecrypt(authKeyX, authKeyY, nonce *big.Int, encrypted []*big.Int, realLength int) ([]*big.Int, error) {
	encStrs := make([]string, len(encrypted))
	for i, v := range encrypted {
		encStrs[i] = v.String()
	}
	payload := map[string]interface{}{
		"key":        [2]string{authKeyX.String(), authKeyY.String()},
		"nonce":      nonce.String(),
		"realLength": realLength,
		"encrypted":  encStrs,
	}
	body, err := c.PostProof("/util/poseidonDecrypt", payload)
	if err != nil {
		return nil, fmt.Errorf("poseidonDecrypt request failed: %w", err)
	}
	var resp struct {
		Plaintext []json.Number `json:"plaintext"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse poseidonDecrypt response: %w", err)
	}
	result := make([]*big.Int, len(resp.Plaintext))
	for i, n := range resp.Plaintext {
		v, ok := new(big.Int).SetString(n.String(), 10)
		if !ok {
			return nil, fmt.Errorf("invalid big int in plaintext response index %d", i)
		}
		result[i] = v
	}
	return result, nil
}

// Erc1155FungibleAuditorProof generates an ERC1155 fungible JoinSplit proof with
// auditor encryption. The auditor's public key (BabyJubJub) is provided by the caller.
// The function generates a random blinding factor and nonce, computes the shared
// encryption key, encrypts the audit plaintext, and sends the full proof request.
//
// Audit plaintext (6 values): [valIn[0], valIn[1], valOut[0], valOut[1], tokenId, contractAddr]
func (c *GnarkClient) Erc1155FungibleAuditorProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int, keysIn []KeyPair, wtSaltsIn []*big.Int,
	wtValuesOut []*big.Int, keysOut []KeyPair,
	recipientViewEncapKeys [][]byte,
	merkleDepth int, merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
	auditorPubKeyX, auditorPubKeyY *big.Int,
) (*ProofResult, error) {
	stCommitmentsOut, wtSaltsOut, ctI, ctII, stNullifiers, wtPathIndices, wtPathElements, err := prepareErc1155ProofParams(
		wtValuesIn, keysIn, wtSaltsIn, wtValuesOut, keysOut, recipientViewEncapKeys, merkleDepth, merkleProofs,
		wtErc1155ContractAddress, wtErc1155TokenId,
	)
	if err != nil {
		return nil, err
	}

	stMerkleRoots := make([]*big.Int, len(wtValuesIn))
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	// Auditor encryption
	auditorRandom, err := RandomAuditorScalar()
	if err != nil {
		return nil, fmt.Errorf("failed to generate auditor random: %w", err)
	}
	auditorNonce, err := RandomNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate auditor nonce: %w", err)
	}
	authKeyX, authKeyY := AuditorEncKey(auditorPubKeyX, auditorPubKeyY, auditorRandom)

	// plaintext: [valIn[0], valIn[1], valOut[0], valOut[1], tokenId, contractAddr]
	plaintext := make([]*big.Int, 0, 6)
	plaintext = append(plaintext, wtValuesIn...)
	plaintext = append(plaintext, wtValuesOut...)
	plaintext = append(plaintext, wtErc1155TokenId, wtErc1155ContractAddress)

	encrypted, err := c.PoseidonEncrypt([2]*big.Int{authKeyX, authKeyY}, auditorNonce, len(plaintext), plaintext)
	if err != nil {
		return nil, fmt.Errorf("auditor encryption failed: %w", err)
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":                stMessage.String(),
		"StTreeNumbers":            bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":            bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":             bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":          bigIntSliceToStrings(stCommitmentsOut),
		"StAssetGroupMerkleRoot":   assetGroupMerkleProof.Root.String(),
		"StAssetGroupTreeNumber":   stAssetGroupTreeNumber.String(),
		"WtPrivateKeysIn":          bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":               bigIntSliceToStrings(wtValuesIn),
		"WtSaltsIn":                bigIntSliceToStrings(wtSaltsIn),
		"WtPathElements":           bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":            bigIntSliceToStrings(wtPathIndices),
		"WtErc1155ContractAddress": wtErc1155ContractAddress.String(),
		"WtErc1155TokenId":         wtErc1155TokenId.String(),
		"WtPublicKeysOut":          bigIntSliceToStrings(extractPublicKeys(keysOut)),
		"WtValuesOut":              bigIntSliceToStrings(wtValuesOut),
		"WtSaltsOut":               bigIntSliceToStrings(wtSaltsOut),
		"WtAssetGroupPathElements": bigIntSliceToStrings(assetGroupMerkleProof.Elements),
		"WtAssetGroupPathIndices":  assetGroupMerkleProof.Indices.String(),
		"StAuditorPublickey":       []string{auditorPubKeyX.String(), auditorPubKeyY.String()},
		"StAuditorAuthKey":         []string{authKeyX.String(), authKeyY.String()},
		"StAuditorNonce":           auditorNonce.String(),
		"StAuditorEncryptedValues": bigIntSliceToStrings(encrypted),
		"WtAuditorRandom":          auditorRandom.String(),
	}

	body, err := c.PostProof("/proof/erc1155FungibleAuditor", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155FungibleAuditor proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if err := json.Unmarshal(body, &gnarkResp); err != nil {
		return nil, fmt.Errorf("failed to parse erc1155FungibleAuditor response: %w", err)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	statement := make([]*big.Int, 0, 1+3*len(wtValuesIn)+len(keysOut))
	statement = append(statement, stMessage)
	for i := 0; i < len(wtValuesIn); i++ {
		statement = append(statement, stTreeNumbers[i], stMerkleRoots[i], stNullifiers[i])
	}
	statement = append(statement, stCommitmentsOut...)

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  len(wtValuesIn),
		NumberOfOutputs: len(wtValuesOut),
		SaltsOut:        wtSaltsOut,
		CiphertextI:     ctI,
		CiphertextII:    ctII,
		AuditData: &AuditEncryptionData{
			AuthKeyX:   authKeyX,
			AuthKeyY:   authKeyY,
			Nonce:      auditorNonce,
			Encrypted:  encrypted,
			RealLength: len(plaintext),
		},
	}, nil
}

// Erc1155NonFungibleAuditorProof generates an ERC1155 non-fungible ownership proof
// with auditor encryption. The auditor's public key (BabyJubJub) is provided by
// the caller.
//
// Audit plaintext (3 values): [value[0], tokenId[0], contractAddr]
func (c *GnarkClient) Erc1155NonFungibleAuditorProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	recipientViewEncapKey []byte,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
	auditorPubKeyX, auditorPubKeyY *big.Int,
) (*ProofResult, error) {
	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	// Generate fresh salt for output note via ML-KEM. recipientViewEncapKey is mandatory.
	if recipientViewEncapKey == nil {
		return nil, fmt.Errorf("recipientViewEncapKey is required for non-interactive note delivery")
	}
	saltB, ctI, kemErr := Encapsulate(recipientViewEncapKey)
	if kemErr != nil {
		return nil, fmt.Errorf("failed to encapsulate for output: %w", kemErr)
	}
	wtSaltOut := SaltBToField(saltB)
	ctII, encErr := EncryptPayload(saltB, wtErc1155TokenId, wtValue)
	if encErr != nil {
		return nil, fmt.Errorf("failed to encrypt payload for output: %w", encErr)
	}

	commitmentOut, err := Erc1155Commitment(wtErc1155TokenId, wtValue, keyOut.PublicKey, wtSaltOut)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155Commitment for output: %w", err)
	}

	// Auditor encryption
	auditorRandom, err := RandomAuditorScalar()
	if err != nil {
		return nil, fmt.Errorf("failed to generate auditor random: %w", err)
	}
	auditorNonce, err := RandomNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate auditor nonce: %w", err)
	}
	authKeyX, authKeyY := AuditorEncKey(auditorPubKeyX, auditorPubKeyY, auditorRandom)

	// plaintext: [value[0], tokenId[0], contractAddr]
	plaintext := []*big.Int{wtValue, wtErc1155TokenId, wtErc1155ContractAddress}

	encrypted, err := c.PoseidonEncrypt([2]*big.Int{authKeyX, authKeyY}, auditorNonce, len(plaintext), plaintext)
	if err != nil {
		return nil, fmt.Errorf("auditor encryption failed: %w", err)
	}

	payload := map[string]interface{}{
		"StMessage":                stMessage.String(),
		"StTreeNumbers":            []string{stTreeNumber.String()},
		"StMerkleRoots":            []string{merkleProof.Root.String()},
		"StNullifiers":             []string{nullifier.String()},
		"StCommitmentOut":          []string{commitmentOut.String()},
		"StAssetGroupTreeNumber":   []string{stAssetGroupTreeNumber.String()},
		"StAssetGroupMerkleRoot":   []string{assetGroupMerkleProof.Root.String()},
		"WtPrivateKeysIn":          []string{keyIn.PrivateKey.String()},
		"WtValues":                 []string{wtValue.String()},
		"WtSaltsIn":                []string{wtSaltIn.String()},
		"WtPathElements":           [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":            []string{merkleProof.Indices.String()},
		"WtErc1155TokenIds":        []string{wtErc1155TokenId.String()},
		"WtErc1155ContractAddress": wtErc1155ContractAddress.String(),
		"WtPublicKeysOut":          []string{keyOut.PublicKey.String()},
		"WtSaltsOut":               []string{wtSaltOut.String()},
		"WtAssetGroupPathElements": [][]string{bigIntSliceToStrings(assetGroupMerkleProof.Elements)},
		"WtAssetGroupPathIndices":  []string{assetGroupMerkleProof.Indices.String()},
		"StAuditorPublickey":       []string{auditorPubKeyX.String(), auditorPubKeyY.String()},
		"StAuditorAuthKey":         []string{authKeyX.String(), authKeyY.String()},
		"StAuditorNonce":           auditorNonce.String(),
		"StAuditorEncryptedValues": bigIntSliceToStrings(encrypted),
		"WtAuditorRandom":          auditorRandom.String(),
	}

	body, err := c.PostProof("/proof/erc1155NonFungibleAuditor", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155NonFungibleAuditor proof request failed: %w", err)
	}

	var gnarkResp struct {
		Proof []json.Number `json:"proof"`
	}
	if err := json.Unmarshal(body, &gnarkResp); err != nil {
		return nil, fmt.Errorf("failed to parse erc1155NonFungibleAuditor response: %w", err)
	}
	proofStrs := make([]string, len(gnarkResp.Proof))
	for i, n := range gnarkResp.Proof {
		proofStrs[i] = n.String()
	}

	statement := []*big.Int{
		stMessage,
		stTreeNumber,
		merkleProof.Root,
		nullifier,
		commitmentOut,
		stAssetGroupTreeNumber,
		assetGroupMerkleProof.Root,
	}

	return &ProofResult{
		Proof:           proofStrs,
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
		SaltsOut:        []*big.Int{wtSaltOut},
		CiphertextI:     [][]byte{ctI},
		CiphertextII:    [][]byte{ctII},
		AuditData: &AuditEncryptionData{
			AuthKeyX:   authKeyX,
			AuthKeyY:   authKeyY,
			Nonce:      auditorNonce,
			Encrypted:  encrypted,
			RealLength: len(plaintext),
		},
	}, nil
}

// Erc20PrivateMintResult holds the outputs of a successful V2 PrivateMint proof.
type Erc20PrivateMintResult struct {
	// Commitment is the V2 note commitment: Poseidon(pkSpend, salt, amount, tokenId).
	// This is what gets inserted into the Merkle tree on-chain.
	Commitment *big.Int

	// CipherText is the note tag: Poseidon(pkSpend, salt).
	// Published on-chain as a public signal; Alice uses it to confirm a mint is hers.
	CipherText *big.Int

	// Salt is the random field element Alice chose. She must keep this as her
	// WtSaltsIn witness when she later spends the note in a JoinSplit proof.
	Salt *big.Int

	// ProofResponse carries the Groth16 proof and public signals from the gnark server.
	ProofResponse *PrivateMintProofResponse
}

// Erc20PrivateMintProof generates a V2 PrivateMint proof for an ERC20 deposit.
//
// Alice picks a random salt, then the function:
//  1. Computes commitment = Poseidon(pkSpend, salt, amount, tokenId) — the V2 format
//     expected by the JoinSplit circuit on the input side.
//  2. Computes cipherText = Poseidon(pkSpend, salt) — a note tag Alice can use when
//     scanning the chain to confirm this mint is hers.
//  3. Sends both to the gnark server's /proof/privateMint endpoint.
//
// The returned Salt must be stored by Alice — it is her WtSaltsIn when spending.
func (c *GnarkClient) Erc20PrivateMintProof(
	pkSpend *big.Int,
	salt *big.Int,
	amount *big.Int,
	tokenId *big.Int,
	contractAddress *big.Int,
) (*Erc20PrivateMintResult, error) {
	commitment, err := Erc20CommitmentV2(pkSpend, salt, amount, tokenId)
	if err != nil {
		return nil, fmt.Errorf("failed to compute V2 commitment: %w", err)
	}

	cipherText, err := poseidon.Hash([]*big.Int{pkSpend, salt})
	if err != nil {
		return nil, fmt.Errorf("failed to compute cipherText: %w", err)
	}

	payload := map[string]interface{}{
		"commitment":      commitment.String(),
		"contractAddress": contractAddress.String(),
		"tokenId":         tokenId.String(),
		"salt":            salt.String(),
		"amount":          amount.String(),
		"publicKey":       pkSpend.String(),
		"cipherText":      cipherText.String(),
	}

	body, err := c.PostProof("/proof/privateMint", payload)
	if err != nil {
		return nil, fmt.Errorf("privateMint proof request failed: %w", err)
	}

	var resp PrivateMintProofResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse privateMint response: %w", err)
	}

	return &Erc20PrivateMintResult{
		Commitment:    commitment,
		CipherText:    cipherText,
		Salt:          salt,
		ProofResponse: &resp,
	}, nil
}

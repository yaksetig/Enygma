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
// A fresh salt is generated for the output note.
func (c *GnarkClient) Erc721OwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc721ContractAddress *big.Int,
) (*ProofResult, error) {
	_, err := Erc721Commitment(wtErc721ContractAddress, wtValue, keyIn.PublicKey, wtSaltIn)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721Commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	// Generate fresh salt for output note
	wtSaltOut, err := RandomInField()
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt for output: %w", err)
	}

	commitmentOut, err := Erc721Commitment(wtErc721ContractAddress, wtValue, keyOut.PublicKey, wtSaltOut)
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

	_, err = c.PostProof("/proof/ownershipERC721", payload)
	if err != nil {
		return nil, fmt.Errorf("erc721Ownership proof request failed: %w", err)
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
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
	}, nil
}

// Erc1155FungibleJoinSplitProof generates a strongly-typed ERC1155 fungible JoinSplit proof.
// wtSaltsIn must contain the salt used when each input note was originally created.
// Fresh salts are generated for output notes.
func (c *GnarkClient) Erc1155FungibleJoinSplitProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int, keysIn []KeyPair, wtSaltsIn []*big.Int,
	wtValuesOut []*big.Int, keysOut []KeyPair,
	merkleDepth int, merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	stCommitmentsOut, wtSaltsOut, stNullifiers, wtPathIndices, wtPathElements, err := prepareErc1155ProofParams(
		wtValuesIn, keysIn, wtSaltsIn, wtValuesOut, keysOut, merkleDepth, merkleProofs,
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

	_, err = c.PostProof("/proof/erc155Fungible", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155FungibleJoinSplit proof request failed: %w", err)
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
		Statement:       statement,
		NumberOfInputs:  len(wtValuesIn),
		NumberOfOutputs: len(wtValuesOut),
	}, nil
}

// Erc1155NonFungibleOwnershipProof generates a strongly-typed ERC1155 non-fungible ownership proof.
// wtSaltIn is the salt used when the input note was originally created.
// A fresh salt is generated for the output note.
func (c *GnarkClient) Erc1155NonFungibleOwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, wtSaltIn *big.Int, keyOut KeyPair,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	uniqueId, err := Erc1155UniqueId(wtErc1155ContractAddress, wtErc1155TokenId, wtValue)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155UniqueId: %w", err)
	}

	_, err = Erc1155Commitment(wtErc1155ContractAddress, wtErc1155TokenId, wtValue, keyIn.PublicKey, wtSaltIn)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155Commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	// Generate fresh salt for output note
	wtSaltOut, err := RandomInField()
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt for output: %w", err)
	}

	commitmentOut, err := Erc1155Commitment(wtErc1155ContractAddress, wtErc1155TokenId, wtValue, keyOut.PublicKey, wtSaltOut)
	_ = uniqueId // used in payload below
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
		"WtValues":                 []string{uniqueId.String()},
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

	_, err = c.PostProof("/proof/erc1155NonFungible", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155NonFungibleOwnership proof request failed: %w", err)
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
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
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

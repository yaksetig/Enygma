package core

import (
	"fmt"
	"math/big"
)

// Erc20JoinSplitProof generates a strongly-typed ERC20 JoinSplit proof.
// It computes uniqueIds, commitments, and nullifiers locally, then posts
// the full payload to the gnark server.
func (c *GnarkClient) Erc20JoinSplitProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int, keysIn []KeyPair,
	wtValuesOut []*big.Int, keysOut []KeyPair,
	merkleDepth int, merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc20ContractAddress *big.Int,
	use10_2 bool,
) (*ProofResult, error) {
	stNullifiers := make([]*big.Int, len(wtValuesIn))
	wtPathIndices := make([]*big.Int, len(wtValuesIn))
	wtPathElements := make([]*big.Int, 0)

	for i := 0; i < len(wtValuesIn); i++ {
		idIn, err := Erc20UniqueId(wtErc20ContractAddress, wtValuesIn[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20UniqueId for input %d: %w", i, err)
		}
		_, err = GetCommitment(idIn, keysIn[i].PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to compute commitment for input %d: %w", i, err)
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

	stCommitmentsOut := make([]*big.Int, len(keysOut))
	for i := 0; i < len(keysOut); i++ {
		idOut, err := Erc20UniqueId(wtErc20ContractAddress, wtValuesOut[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute erc20UniqueId for output %d: %w", i, err)
		}
		cmt, err := GetCommitment(idOut, keysOut[i].PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to compute commitment for output %d: %w", i, err)
		}
		stCommitmentsOut[i] = cmt
	}

	stMerkleRoots := make([]*big.Int, len(wtValuesIn))
	for i := range wtValuesIn {
		if wtValuesIn[i].Sign() == 0 {
			stMerkleRoots[i] = big.NewInt(0)
		} else {
			stMerkleRoots[i] = merkleProofs[i].Root
		}
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":              stMessage.String(),
		"StTreeNumber":           bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":          bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":           bigIntSliceToStrings(stNullifiers),
		"StCommitmentOut":        bigIntSliceToStrings(stCommitmentsOut),
		"WtPrivateKeysIn":        bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtPublicKeysOut":        bigIntSliceToStrings(extractPublicKeys(keysOut)),
		"WtPathElements":         bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":          bigIntSliceToStrings(wtPathIndices),
		"WtValuesIn":             bigIntSliceToStrings(wtValuesIn),
		"WtValuesOut":            bigIntSliceToStrings(wtValuesOut),
		"WtErc20ContractAddress": wtErc20ContractAddress.String(),
	}

	endpoint := "/proof/joinSplitERC20"
	if use10_2 {
		endpoint = "/proof/joinSplitERC20_10_2"
	}

	_, err := c.PostProof(endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("erc20JoinSplit proof request failed: %w", err)
	}

	// Statement order (interleaved per input, then commitments):
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

// Erc721OwnershipProof generates a strongly-typed ERC721 ownership proof.
// It computes the uniqueId from the contract address and tokenId, then
// uses that as the WtValues field (not the raw tokenId).
func (c *GnarkClient) Erc721OwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, keyOut KeyPair,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc721ContractAddress *big.Int,
) (*ProofResult, error) {
	uniqueId, err := Erc721UniqueId(wtErc721ContractAddress, wtValue)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc721UniqueId: %w", err)
	}

	_, err = GetCommitment(uniqueId, keyIn.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	commitmentOut, err := GetCommitment(uniqueId, keyOut.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment for output: %w", err)
	}

	payload := map[string]interface{}{
		"StMessage":      stMessage.String(),
		"StTreeNumbers":  []string{stTreeNumber.String()},
		"StMerkleRoots":  []string{merkleProof.Root.String()},
		"StNullifiers":   []string{nullifier.String()},
		"StCommitmentOut": []string{commitmentOut.String()},
		"WtPrivateKeysIn": []string{keyIn.PrivateKey.String()},
		"WtValues":        []string{uniqueId.String()},
		"WtPathElements":  [][]string{bigIntSliceToStrings(merkleProof.Elements)},
		"WtPathIndices":   []string{merkleProof.Indices.String()},
		"WtPublicKeysOut": []string{keyOut.PublicKey.String()},
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
// It reuses prepareErc1155ProofParams to compute uniqueIds, commitments, and nullifiers.
func (c *GnarkClient) Erc1155FungibleJoinSplitProof(
	stMessage *big.Int,
	wtValuesIn []*big.Int, keysIn []KeyPair,
	wtValuesOut []*big.Int, keysOut []KeyPair,
	merkleDepth int, merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	stCommitmentsOut, stNullifiers, wtPathIndices, wtPathElements, err := prepareErc1155ProofParams(
		wtValuesIn, keysIn, wtValuesOut, keysOut, merkleDepth, merkleProofs,
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
func (c *GnarkClient) Erc1155NonFungibleOwnershipProof(
	stMessage *big.Int,
	wtValue *big.Int, keyIn KeyPair, keyOut KeyPair,
	merkleDepth int, merkleProof *MerkleProof,
	stTreeNumber *big.Int,
	wtErc1155ContractAddress *big.Int, wtErc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int, assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	uniqueId, err := Erc1155UniqueId(wtErc1155ContractAddress, wtErc1155TokenId, wtValue)
	if err != nil {
		return nil, fmt.Errorf("failed to compute erc1155UniqueId: %w", err)
	}

	_, err = GetCommitment(uniqueId, keyIn.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment for input: %w", err)
	}

	nullifier, err := GetNullifier(keyIn.PrivateKey, merkleProof.Indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	commitmentOut, err := GetCommitment(uniqueId, keyOut.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment for output: %w", err)
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

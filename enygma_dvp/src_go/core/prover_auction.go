package core

import (
	"fmt"
	"log"
	"math/big"
)

// KeyPair holds a private/public key pair used in circuit inputs.
type KeyPair struct {
	PrivateKey *big.Int
	PublicKey  *big.Int
}

// ProofResult holds the output of a proof generation function.
type ProofResult struct {
	Proof           []string
	Statement       []*big.Int
	NumberOfInputs  int
	NumberOfOutputs int
}

// AuctionInitProof generates a proof for AuctionInit.circom.
// It prepares circuit inputs and sends them to the gnark server.
func (c *GnarkClient) AuctionInitProof(
	stBeacon *big.Int,
	tokenId *big.Int,
	wtContractAddress *big.Int,
	keyIn KeyPair,
	merkleDepth int,
	merkleProof *MerkleProof,
	merkleRoot *big.Int,
	stTreeNumber *big.Int,
	vaultId int,
	assetGroupMerkleRoot *big.Int,
	assetGroupMerkleProof *MerkleProof,
	wtIdParams []*big.Int,
) (*ProofResult, error) {
	wtPathElements := merkleProof.Elements
	wtPathIndices := merkleProof.Indices

	stNullifier, err := GetNullifier(keyIn.PrivateKey, wtPathIndices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute nullifier: %w", err)
	}

	uniqueId, err := computeUniqueId(vaultId, wtContractAddress, wtIdParams)
	if err != nil {
		return nil, fmt.Errorf("failed to compute uniqueId: %w", err)
	}

	wtCommitment, err := GetCommitment(uniqueId, keyIn.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute commitment: %w", err)
	}

	stAuctionId, err := GetAuctionId(wtCommitment)
	if err != nil {
		return nil, fmt.Errorf("failed to compute auctionId: %w", err)
	}

	wtAssetGroupPathElements, wtAssetGroupPathIndices := resolveAssetGroupProof(
		assetGroupMerkleRoot, assetGroupMerkleProof, merkleDepth,
	)

	payload := map[string]interface{}{
		"StBeacon":                  stBeacon.String(),
		"StVaultId":                 fmt.Sprintf("%d", vaultId),
		"StAuctionId":               stAuctionId.String(),
		"StNullifier":               stNullifier.String(),
		"StTreeNumber":              stTreeNumber.String(),
		"StMerkleRoot":              merkleRoot.String(),
		"WtPrivateKey":              keyIn.PrivateKey.String(),
		"WtPathElements":            bigIntSliceToStrings(wtPathElements),
		"WtPathIndices":             wtPathIndices.String(),
		"WtCommitment":              wtCommitment.String(),
		"StAssetGroupMerkleRoot":    assetGroupMerkleRoot.String(),
		"WtAssetGroupPathElements":  bigIntSliceToStrings(wtAssetGroupPathElements),
		"WtAssetGroupPathIndices":   bigIntToString(wtAssetGroupPathIndices),
		"WtIdParams":                bigIntSliceToStrings(wtIdParams),
		"WtContractAddress":         wtContractAddress.String(),
	}

	_, err = c.PostProof("/proof/auctionInit", payload)
	if err != nil {
		return nil, fmt.Errorf("auctionInit proof request failed: %w", err)
	}

	statement := []*big.Int{
		stBeacon,
		big.NewInt(int64(vaultId)),
		stAuctionId,
		stTreeNumber,
		merkleRoot,
		stNullifier,
		assetGroupMerkleRoot,
	}

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
	}, nil
}

// AuctionBidProof generates a proof for AuctionBidErc20.circom.
func (c *GnarkClient) AuctionBidProof(
	stAuctionId *big.Int,
	wtBidAmount *big.Int,
	wtBidRandom *big.Int,
	assetAddress *big.Int,
	wtValuesIn []*big.Int,
	keysIn []KeyPair,
	wtValuesOut []*big.Int,
	keysOut []KeyPair,
	merkleDepth int,
	merkleProofs []*MerkleProof,
	stMerkleRoots []*big.Int,
	stTreeNumbers []*big.Int,
	stVaultId int,
	wtIdParamsIn [][]*big.Int,
	wtIdParamsOut [][]*big.Int,
	stAssetGroupMerkleRoot *big.Int,
	assetGroupMerkleProof *MerkleProof,
) (*ProofResult, error) {
	stBlindedBid, err := Pedersen(wtBidAmount, wtBidRandom)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blindedBid: %w", err)
	}

	stCommitmentsOut := make([]*big.Int, len(keysOut))
	stNullifiers := make([]*big.Int, len(wtValuesIn))
	wtPathIndices := make([]*big.Int, len(wtValuesIn))
	wtPathElements := make([]*big.Int, 0)

	for i := 0; i < len(wtValuesIn); i++ {
		_, err := computeUniqueId(stVaultId, assetAddress, wtIdParamsIn[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute uniqueId for input %d: %w", i, err)
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

	for i := 0; i < len(keysOut); i++ {
		uniqueIdOut, err := computeUniqueId(stVaultId, assetAddress, wtIdParamsOut[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute uniqueId for output %d: %w", i, err)
		}
		cmt, err := GetCommitment(uniqueIdOut, keysOut[i].PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to compute commitment for output %d: %w", i, err)
		}
		stCommitmentsOut[i] = cmt
	}

	wtAssetGroupPathElements, wtAssetGroupPathIndices := resolveAssetGroupProof(
		stAssetGroupMerkleRoot, assetGroupMerkleProof, merkleDepth,
	)

	// Split path elements into chunks of merkleDepth for the payload
	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	// Flatten idParams for payload
	idParamsInFlat := flattenBigIntSlice(wtIdParamsIn)
	idParamsOutFlat := flattenBigIntSlice(wtIdParamsOut)

	payload := map[string]interface{}{
		"StAuctionId":               stAuctionId.String(),
		"StBlindedBid":              stBlindedBid.String(),
		"StVaultId":                 fmt.Sprintf("%d", stVaultId),
		"StTreeNumbers":             bigIntSliceToStrings(stTreeNumbers),
		"StMerkleRoots":             bigIntSliceToStrings(stMerkleRoots),
		"StNullifiers":              bigIntSliceToStrings(stNullifiers),
		"StCommitmentsOuts":         bigIntSliceToStrings(stCommitmentsOut),
		"StAssetGroupMerkleRoot":    stAssetGroupMerkleRoot.String(),
		"WtBidAmount":               wtBidAmount.String(),
		"WtBidRandom":               wtBidRandom.String(),
		"WtPrivateKeys":             bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtValuesIn":                bigIntSliceToStrings(wtValuesIn),
		"WtPathElements":            bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":             bigIntSliceToStrings(wtPathIndices),
		"WtContractAddress":         assetAddress.String(),
		"WtRecipientPK":             bigIntSliceToStrings(extractPublicKeys(keysOut)),
		"WtValuesOut":               bigIntSliceToStrings(wtValuesOut),
		"WtAssetGroupPathElements":  bigIntSliceToStrings(wtAssetGroupPathElements),
		"WtAssetGroupPathIndices":   bigIntToString(wtAssetGroupPathIndices),
		"WtIdParamsIn":              bigIntSliceToStrings(idParamsInFlat),
		"WtIdParamsOut":             bigIntSliceToStrings(idParamsOutFlat),
	}

	_, err = c.PostProof("/proof/auctionBid", payload)
	if err != nil {
		return nil, fmt.Errorf("auctionBid proof request failed: %w", err)
	}

	statement := make([]*big.Int, 0)
	statement = append(statement, stAuctionId, stBlindedBid, big.NewInt(int64(stVaultId)))
	statement = append(statement, stTreeNumbers...)
	statement = append(statement, stMerkleRoots...)
	statement = append(statement, stNullifiers...)
	statement = append(statement, stCommitmentsOut...)
	statement = append(statement, stAssetGroupMerkleRoot)

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  2,
		NumberOfOutputs: 2,
	}, nil
}

// AuctionPrivateOpeningProof generates a proof for AuctionPrivateOpening.circom.
func (c *GnarkClient) AuctionPrivateOpeningProof2(
	stAuctionId *big.Int,
	stBlindedBid *big.Int,
	wtBidAmount *big.Int,
	wtBidRandom *big.Int,
) (*ProofResult, error) {
	payload := map[string]interface{}{
		"StVaultId":    stAuctionId.String(),
		"StBlindedBid": stBlindedBid.String(),
		"WtBidAmount":  wtBidAmount.String(),
		"WtBidRandom":  wtBidRandom.String(),
	}

	_, err := c.PostProof("/proof/auctionPrivateOpening", payload)
	if err != nil {
		return nil, fmt.Errorf("auctionPrivateOpening proof request failed: %w", err)
	}

	statement := []*big.Int{stAuctionId, stBlindedBid}

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
	}, nil
}

// AuctionNotWinningBidProof2 generates a proof that a bid is less than the winning bid.
// Named with "2" suffix to avoid conflict with the map-based version in prover_gnark.go.
func (c *GnarkClient) AuctionNotWinningBidProof2(
	stAuctionId *big.Int,
	stBidBlockNumber *big.Int,
	stWinningBidBlockNumber *big.Int,
	wtBidAmount *big.Int,
	wtBidRandom *big.Int,
	wtWinningBidAmount *big.Int,
	wtWinningBidRandom *big.Int,
) (*ProofResult, error) {
	blindedBid, err := Pedersen(wtBidAmount, wtBidRandom)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blindedBid: %w", err)
	}

	blindedWinningBid, err := Pedersen(wtWinningBidAmount, wtWinningBidRandom)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blindedWinningBid: %w", err)
	}

	// stBlindedBidDifference = (blindedWinningBid - blindedBid + SNARK_SCALAR_FIELD) % SNARK_SCALAR_FIELD
	stBlindedBidDifference := new(big.Int).Sub(blindedWinningBid, blindedBid)
	stBlindedBidDifference.Add(stBlindedBidDifference, SNARK_SCALAR_FIELD)
	stBlindedBidDifference.Mod(stBlindedBidDifference, SNARK_SCALAR_FIELD)

	payload := map[string]interface{}{
		"StAuctionId":              stAuctionId.String(),
		"StBlindedBidDifference":   stBlindedBidDifference.String(),
		"StBidBlockNumber":         stBidBlockNumber.String(),
		"StWinningBidBlockNumber":  stWinningBidBlockNumber.String(),
		"WtBidAmount":              wtBidAmount.String(),
		"WtBidRandom":              wtBidRandom.String(),
		"WtWinningBidAmount":       wtWinningBidAmount.String(),
		"WtWinningBidRandom":       wtWinningBidRandom.String(),
	}

	_, err = c.PostProof("/proof/auctionNotWinning", payload)
	if err != nil {
		return nil, fmt.Errorf("auctionNotWinning proof request failed: %w", err)
	}

	statement := []*big.Int{
		stAuctionId,
		stBlindedBidDifference,
		stBidBlockNumber,
		stWinningBidBlockNumber,
	}

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  1,
		NumberOfOutputs: 1,
	}, nil
}

// BrokerRegistrationProof generates a proof for broker registration.
func (c *GnarkClient) BrokerRegistrationProof(
	stBeacon *big.Int,
	stVaultId int,
	stGroupId *big.Int,
	delegatorKeys []KeyPair,
	merkleDepth int,
	stDelegatorTreeNumbers []*big.Int,
	delegatorMerkleProofs []*MerkleProof,
	wtDelegatorIdParams [][]*big.Int,
	wtBrokerPublicKey *big.Int,
	wtContractAddress *big.Int,
	stAssetGroupTreeNumber *big.Int,
	assetGroupMerkleProof *MerkleProof,
	stBrokerMinCommissionRate *big.Int,
	stBrokerMaxCommissionRate *big.Int,
) (*ProofResult, error) {
	stBrokerBlindedPublicKey, err := BlindedPublicKey(wtBrokerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blinded public key: %w", err)
	}

	stDelegatorMerkleRoots := make([]*big.Int, len(delegatorKeys))
	stDelegatorNullifiers := make([]*big.Int, len(delegatorKeys))
	pathIndices := make([]*big.Int, len(delegatorKeys))
	pathElements := make([][]*big.Int, len(delegatorKeys))

	for i := 0; i < len(delegatorKeys); i++ {
		stDelegatorMerkleRoots[i] = delegatorMerkleProofs[i].Root

		_, err := computeUniqueId(stVaultId, wtContractAddress, wtDelegatorIdParams[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute uniqueId for delegator %d: %w", i, err)
		}

		if wtDelegatorIdParams[i][0].Sign() == 0 {
			pathIndices[i] = big.NewInt(0)
			zeros := make([]*big.Int, merkleDepth)
			for j := range zeros {
				zeros[j] = big.NewInt(0)
			}
			pathElements[i] = zeros
		} else {
			pathIndices[i] = delegatorMerkleProofs[i].Indices
			pathElements[i] = delegatorMerkleProofs[i].Elements
		}

		nullifier, err := GetNullifier(delegatorKeys[i].PrivateKey, pathIndices[i])
		if err != nil {
			return nil, fmt.Errorf("failed to compute nullifier for delegator %d: %w", i, err)
		}
		stDelegatorNullifiers[i] = nullifier
	}

	stAssetGroupMerkleRoot := assetGroupMerkleProof.Root

	payload := map[string]interface{}{
		"StBeacon":                    stBeacon.String(),
		"StVaultId":                   fmt.Sprintf("%d", stVaultId),
		"StGroupId":                   stGroupId.String(),
		"StDelegatorTreeNumbers":      bigIntSliceToStrings(stDelegatorTreeNumbers),
		"StDelegatorMerkleRoots":      bigIntSliceToStrings(stDelegatorMerkleRoots),
		"StDelegatorNullifiers":       bigIntSliceToStrings(stDelegatorNullifiers),
		"StBrokerBlindedPublicKey":    stBrokerBlindedPublicKey.String(),
		"StAssetGroupTreeNumber":      stAssetGroupTreeNumber.String(),
		"StAssetGroupMerkleRoot":      stAssetGroupMerkleRoot.String(),
		"WtDelegatorPrivateKeys":      bigIntSliceToStrings(extractPrivateKeys(delegatorKeys)),
		"WtDelegatorPathElements":     bigIntNestedToStringNested(pathElements),
		"WtDelegatorPathIndices":      bigIntSliceToStrings(pathIndices),
		"WtDelegatorIdParams":         bigIntNestedToStringNested(wtDelegatorIdParams),
		"WtContractAddress":           wtContractAddress.String(),
		"WtBrokerPublicKey":           wtBrokerPublicKey.String(),
		"WtAssetGroupPathIndices":     assetGroupMerkleProof.Indices.String(),
		"WtAssetGroupPathElements":    bigIntSliceToStrings(assetGroupMerkleProof.Elements),
		"StBrokerMinCommissionRate":   stBrokerMinCommissionRate.String(),
		"StBrokerMaxCommissionRate":   stBrokerMaxCommissionRate.String(),
	}

	_, err = c.PostProof("/proof/brokerRegistration", payload)
	if err != nil {
		return nil, fmt.Errorf("brokerRegistration proof request failed: %w", err)
	}

	log.Printf("statement: broker registration")

	statement := make([]*big.Int, 0)
	statement = append(statement, stBeacon, big.NewInt(int64(stVaultId)), stGroupId)
	statement = append(statement, stDelegatorTreeNumbers...)
	statement = append(statement, stDelegatorMerkleRoots...)
	statement = append(statement, stDelegatorNullifiers...)
	statement = append(statement,
		stBrokerBlindedPublicKey,
		stBrokerMinCommissionRate,
		stBrokerMaxCommissionRate,
		stAssetGroupTreeNumber,
		stAssetGroupMerkleRoot,
	)

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  len(stDelegatorMerkleRoots),
		NumberOfOutputs: 0,
	}, nil
}

// LegitBrokerProof generates a proof that a broker is legitimate.
func (c *GnarkClient) LegitBrokerProof(
	stBeacon *big.Int,
	wtPrivateKey *big.Int,
) (*ProofResult, error) {
	publicKey, err := GetPublicKey(wtPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive public key: %w", err)
	}

	stBlindedPublicKey, err := BlindedPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blinded public key: %w", err)
	}

	payload := map[string]interface{}{
		"StBeacon":            stBeacon.String(),
		"StBlindedPublicKey":  stBlindedPublicKey.String(),
		"WtPrivateKey":        wtPrivateKey.String(),
	}

	_, err = c.PostProof("/proof/legitBroker", payload)
	if err != nil {
		return nil, fmt.Errorf("legitBroker proof request failed: %w", err)
	}

	statement := []*big.Int{stBeacon, stBlindedPublicKey}

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  0,
		NumberOfOutputs: 0,
	}, nil
}

// Erc20WithBrokerV1Proof is unsupported — the gnark server does not yet have
// an erc20JoinSplitWithBrokerV1 circuit. This will return an error until the
// circuit is added server-side.
func (c *GnarkClient) Erc20WithBrokerV1Proof(
	stMessage *big.Int,
	wtValuesIn []*big.Int,
	keysIn []KeyPair,
	wtValuesOut []*big.Int,
	keysOut []KeyPair,
	merkleDepth int,
	merkleProofs []*MerkleProof,
	stTreeNumbers []*big.Int,
	wtErc20ContractAddress *big.Int,
	stBrokerBlindedPublicKey *big.Int,
) (*ProofResult, error) {
	return nil, fmt.Errorf("erc20WithBrokerV1 proof is not supported: gnark server does not have erc20JoinSplitWithBrokerV1 circuit")
}

// Erc1155FungibleWithBrokerV1Proof generates an ERC1155 fungible JoinSplit proof with broker.
func (c *GnarkClient) Erc1155FungibleWithBrokerV1Proof(
	message *big.Int,
	valuesIn []*big.Int,
	keysIn []KeyPair,
	valuesOut []*big.Int,
	keysOut []KeyPair,
	merkleDepth int,
	treeNumbers []*big.Int,
	merkleProofs []*MerkleProof,
	erc1155ContractAddress *big.Int,
	erc1155TokenId *big.Int,
	stAssetGroupTreeNumber *big.Int,
	assetGroupMerkleProof *MerkleProof,
	brokerPublicKey *big.Int,
	stBrokerCommissionRate *big.Int,
) (*ProofResult, error) {
	stBrokerBlindedPublicKey, err := BlindedPublicKey(brokerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute blinded public key: %w", err)
	}

	// Prepare ERC1155 proof parameters
	stCommitmentsOut, stNullifiers, wtPathIndices, wtPathElements, err := prepareErc1155ProofParams(
		valuesIn, keysIn, valuesOut, keysOut, merkleDepth, merkleProofs,
		erc1155ContractAddress, erc1155TokenId,
	)
	if err != nil {
		return nil, err
	}

	merkleRoots := make([]*big.Int, len(merkleProofs))
	for i, mp := range merkleProofs {
		merkleRoots[i] = mp.Root
	}

	pathElementChunks := chunkBigIntSlice(wtPathElements, merkleDepth)

	payload := map[string]interface{}{
		"StMessage":                message.String(),
		"StTreeNumbers":            bigIntSliceToStrings(treeNumbers),
		"StMerkleRoots":            bigIntSliceToStrings(merkleRoots),
		"StNullifiers":             bigIntSliceToStrings(stNullifiers),
		"StCommitmentsOut":         bigIntSliceToStrings(stCommitmentsOut),
		"StBrokerBlindedPublicKey": stBrokerBlindedPublicKey.String(),
		"StBrokerCommisionRate":    stBrokerCommissionRate.String(),
		"StAssetGroupTreeNumber":   stAssetGroupTreeNumber.String(),
		"StAssetGroupMerkleRoot":   assetGroupMerkleProof.Root.String(),
		"WtValuesIn":               bigIntSliceToStrings(valuesIn),
		"WtPrivateKeys":            bigIntSliceToStrings(extractPrivateKeys(keysIn)),
		"WtPathElements":           bigIntChunksToStringChunks(pathElementChunks),
		"WtPathIndices":            bigIntSliceToStrings(wtPathIndices),
		"WtRecipientPk":            bigIntSliceToStrings(extractPublicKeys(keysOut)),
		"WtValuesOut":              bigIntSliceToStrings(valuesOut),
		"WtErc1155ContractAddress": erc1155ContractAddress.String(),
		"WtErc1155TokenId":         erc1155TokenId.String(),
		"WtAssetGroupPathElements": bigIntSliceToStrings(assetGroupMerkleProof.Elements),
		"WtAssetGroupPathIndices":  assetGroupMerkleProof.Indices.String(),
	}

	_, err = c.PostProof("/proof/erc1155FungibleWithBroker", payload)
	if err != nil {
		return nil, fmt.Errorf("erc1155FungibleWithBrokerV1 proof request failed: %w", err)
	}

	statement := make([]*big.Int, 0)
	statement = append(statement, message)
	statement = append(statement, treeNumbers...)
	statement = append(statement, merkleRoots...)
	statement = append(statement, stNullifiers...)
	statement = append(statement, stCommitmentsOut...)
	statement = append(statement,
		stBrokerBlindedPublicKey,
		stBrokerCommissionRate,
		stAssetGroupTreeNumber,
		assetGroupMerkleProof.Root,
	)

	return &ProofResult{
		Statement:       statement,
		NumberOfInputs:  len(valuesIn),
		NumberOfOutputs: len(valuesOut),
	}, nil
}

// --- internal helpers ---

// computeUniqueId computes a token uniqueId based on vaultId type.
func computeUniqueId(vaultId int, contractAddress *big.Int, idParams []*big.Int) (*big.Int, error) {
	switch vaultId {
	case 0:
		return Erc20UniqueId(contractAddress, idParams[0])
	case 1:
		return Erc721UniqueId(contractAddress, idParams[0])
	case 2:
		return Erc1155UniqueId(contractAddress, idParams[1], idParams[0])
	default:
		return nil, fmt.Errorf("unknown vaultId: %d", vaultId)
	}
}

// resolveAssetGroupProof returns the asset group path elements and indices.
// If the merkle root is zero, it returns zero-filled elements.
func resolveAssetGroupProof(
	assetGroupMerkleRoot *big.Int,
	assetGroupMerkleProof *MerkleProof,
	merkleDepth int,
) ([]*big.Int, *big.Int) {
	if assetGroupMerkleRoot.Sign() != 0 && assetGroupMerkleProof != nil {
		return assetGroupMerkleProof.Elements, assetGroupMerkleProof.Indices
	}
	zeros := make([]*big.Int, merkleDepth)
	for i := range zeros {
		zeros[i] = big.NewInt(0)
	}
	return zeros, big.NewInt(0)
}

// prepareErc1155ProofParams prepares common ERC1155 proof parameters.
func prepareErc1155ProofParams(
	valuesIn []*big.Int,
	keysIn []KeyPair,
	valuesOut []*big.Int,
	keysOut []KeyPair,
	merkleDepth int,
	merkleProofs []*MerkleProof,
	contractAddress *big.Int,
	tokenId *big.Int,
) (commitmentsOut []*big.Int, nullifiers []*big.Int, pathIndices []*big.Int, pathElements []*big.Int, err error) {
	commitmentsOut = make([]*big.Int, len(keysOut))
	nullifiers = make([]*big.Int, len(valuesIn))
	pathIndices = make([]*big.Int, len(valuesIn))
	pathElements = make([]*big.Int, 0)

	for i := 0; i < len(valuesIn); i++ {
		uniqueId, err := Erc1155UniqueId(contractAddress, tokenId, valuesIn[i])
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to compute erc1155UniqueId for input %d: %w", i, err)
		}
		_, err = GetCommitment(uniqueId, keysIn[i].PublicKey)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to compute commitment for input %d: %w", i, err)
		}

		if valuesIn[i].Sign() == 0 {
			pathIndices[i] = big.NewInt(0)
			zeros := make([]*big.Int, merkleDepth)
			for j := range zeros {
				zeros[j] = big.NewInt(0)
			}
			pathElements = append(pathElements, zeros...)
		} else {
			pathIndices[i] = merkleProofs[i].Indices
			pathElements = append(pathElements, merkleProofs[i].Elements...)
		}

		nullifier, err := GetNullifier(keysIn[i].PrivateKey, pathIndices[i])
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to compute nullifier for input %d: %w", i, err)
		}
		nullifiers[i] = nullifier
	}

	for i := 0; i < len(keysOut); i++ {
		uniqueId, err := Erc1155UniqueId(contractAddress, tokenId, valuesOut[i])
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to compute erc1155UniqueId for output %d: %w", i, err)
		}
		cmt, err := GetCommitment(uniqueId, keysOut[i].PublicKey)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to compute commitment for output %d: %w", i, err)
		}
		commitmentsOut[i] = cmt
	}

	return commitmentsOut, nullifiers, pathIndices, pathElements, nil
}

// bigIntSliceToStrings converts []*big.Int to []string.
func bigIntSliceToStrings(vals []*big.Int) []string {
	result := make([]string, len(vals))
	for i, v := range vals {
		result[i] = v.String()
	}
	return result
}

// bigIntToString converts a *big.Int to string, handling nil.
func bigIntToString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

// bigIntNestedToStringNested converts [][]*big.Int to [][]string.
func bigIntNestedToStringNested(vals [][]*big.Int) [][]string {
	result := make([][]string, len(vals))
	for i, inner := range vals {
		result[i] = bigIntSliceToStrings(inner)
	}
	return result
}

// chunkBigIntSlice splits a flat []*big.Int into chunks of chunkSize.
func chunkBigIntSlice(arr []*big.Int, chunkSize int) [][]*big.Int {
	if chunkSize <= 0 {
		return nil
	}
	var result [][]*big.Int
	for i := 0; i < len(arr); i += chunkSize {
		end := i + chunkSize
		if end > len(arr) {
			end = len(arr)
		}
		result = append(result, arr[i:end])
	}
	return result
}

// bigIntChunksToStringChunks converts [][]*big.Int to [][]string.
func bigIntChunksToStringChunks(chunks [][]*big.Int) [][]string {
	return bigIntNestedToStringNested(chunks)
}

// flattenBigIntSlice flattens [][]*big.Int to []*big.Int.
func flattenBigIntSlice(nested [][]*big.Int) []*big.Int {
	var result []*big.Int
	for _, inner := range nested {
		result = append(result, inner...)
	}
	return result
}

// extractPrivateKeys extracts private keys from a slice of KeyPairs.
func extractPrivateKeys(keys []KeyPair) []*big.Int {
	result := make([]*big.Int, len(keys))
	for i, k := range keys {
		result[i] = k.PrivateKey
	}
	return result
}

// extractPublicKeys extracts public keys from a slice of KeyPairs.
func extractPublicKeys(keys []KeyPair) []*big.Int {
	result := make([]*big.Int, len(keys))
	for i, k := range keys {
		result[i] = k.PublicKey
	}
	return result
}

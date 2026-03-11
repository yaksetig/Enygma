package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// GnarkClient is an HTTP client for the gnark proof generation server.
type GnarkClient struct {
	BaseURL string
	Client  *http.Client
}

// ProofResponse is the standard response from most proof endpoints.
type ProofResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// PrivateMintProofResponse is the response from the /proof/privateMint endpoint.
type PrivateMintProofResponse struct {
	Proof        []string `json:"proof"`
	PublicSignal []string `json:"publicSignal"`
}

// NewGnarkClient creates a new GnarkClient with the given base URL.
// If baseURL is empty, it defaults to http://localhost:8081.
func NewGnarkClient(baseURL string) *GnarkClient {
	if baseURL == "" {
		baseURL = "http://localhost:8081"
	}
	return &GnarkClient{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// PostProof sends a POST request with a JSON payload to the given endpoint
// and returns the raw response body.
func (c *GnarkClient) PostProof(endpoint string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := c.BaseURL + endpoint
	resp, err := c.Client.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Response from %s: %s", endpoint, string(body))
	return body, nil
}

// GnarkProver dispatches proof generation based on the zkeyPath string.
func (c *GnarkClient) GnarkProver(circuitInput map[string]interface{}, zkeyPath string) (*ProofResponse, *PrivateMintProofResponse, error) {
	switch {
	case strings.Contains(zkeyPath, "OwnershipErc721"):
		resp, err := c.Erc721Proof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "JoinSplitErc20"):
		resp, err := c.Erc20Proof(circuitInput, zkeyPath)
		return resp, nil, err
	case strings.Contains(zkeyPath, "OwnershipErc1155NonFungibleWithAuditor"):
		resp, err := c.Erc1155NonFungibleWithAuditorProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "OwnershipErc1155NonFungible"):
		resp, err := c.Erc1155NonFungibleProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "JoinSplitErc1155WithAuditor"):
		resp, err := c.Erc1155FungibleAuditorProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "JoinSplitErc1155"):
		resp, err := c.Erc1155FungibleProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionInit_Auditor"):
		resp, err := c.AuctionInitAuditorProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionInit"):
		resp, err := c.AuctionInitMapProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionBid_Auditor"):
		resp, err := c.AuctionBidAuditorProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionBid"):
		resp, err := c.AuctionBidMapProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionPrivateOpening"):
		resp, err := c.AuctionPrivateOpeningProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "AuctionNotWinningBid"):
		log.Println("AuctionNotWinningBidProof")
		resp, err := c.AuctionNotWinningBidProof(circuitInput)
		return resp, nil, err
	case strings.Contains(zkeyPath, "PrivateMint"):
		resp, err := c.PrivateMintProof(circuitInput)
		return nil, resp, err
	default:
		return nil, nil, fmt.Errorf("unknown zkeyPath: %s", zkeyPath)
	}
}

// Erc20Proof generates a JoinSplitERC20 proof.
func (c *GnarkClient) Erc20Proof(inputs map[string]interface{}, zkeyPath string) (*ProofResponse, error) {
	endpoint := "/proof/joinSplitERC20"
	if zkeyPath == "./build/JoinSplitErc20_10_2.zkey" {
		endpoint = "/proof/joinSplitERC20_10_2"
	}

	chunks := SplitPathElements(inputs)

	payload := map[string]interface{}{
		"StMessage":              toString(inputs["st_message"]),
		"StTreeNumber":           toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":          inputs["st_merkleRoots"],
		"StNullifiers":           inputs["st_nullifiers"],
		"StCommitmentOut":        inputs["st_commitmentsOut"],
		"WtPrivateKeysIn":        inputs["wt_privateKeysIn"],
		"WtPublicKeysOut":        inputs["wt_publicKeysOut"],
		"WtPathElements":         chunks,
		"WtPathIndices":          toStringSlice(inputs["wt_pathIndices"]),
		"WtValuesIn":             inputs["wt_valuesIn"],
		"WtValuesOut":            inputs["wt_valuesOut"],
		"WtErc20ContractAddress": toString(inputs["wt_erc20ContractAddress"]),
	}

	_, err := c.PostProof(endpoint, payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// Erc721Proof generates an OwnershipERC721 proof.
func (c *GnarkClient) Erc721Proof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StMessage":      inputs["st_message"],
		"StTreeNumbers":  toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":  toStringSlice(inputs["st_merkleRoots"]),
		"StNullifiers":   inputs["st_nullifiers"],
		"StCommitmentOut": inputs["st_commitmentsOut"],
		"WtPrivateKeysIn": inputs["wt_privateKeysIn"],
		"WtValues":        inputs["wt_values"],
		"WtPathElements":  []interface{}{inputs["wt_pathElements"]},
		"WtPathIndices":   inputs["wt_pathIndices"],
		"WtPublicKeysOut": inputs["wt_publicKeysOut"],
	}

	_, err := c.PostProof("/proof/ownershipERC721", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// Erc1155FungibleProof generates an ERC1155 fungible proof.
func (c *GnarkClient) Erc1155FungibleProof(inputs map[string]interface{}) (*ProofResponse, error) {
	pathElements := toInterfaceSlice(inputs["wt_pathElements"])
	split1, split2 := splitIntoTwo(pathElements, 8)

	payload := map[string]interface{}{
		"StMessage":                inputs["st_message"],
		"StTreeNumbers":            toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":            inputs["st_merkleRoots"],
		"StCommitmentOut":          inputs["st_commitmentsOut"],
		"StNullifiers":             inputs["st_nullifiers"],
		"StAssetGroupMerkleRoot":   toString(inputs["st_assetGroup_merkleRoot"]),
		"StAssetGroupTreeNumber":   toString(inputs["st_assetGroup_treeNumber"]),
		"WtPrivateKeysIn":          inputs["wt_privateKeysIn"],
		"WtValuesIn":               toStringSlice(inputs["wt_valuesIn"]),
		"WtPathElements":           []interface{}{split1, split2},
		"WtPathIndices":            toStringSlice(inputs["wt_pathIndices"]),
		"WtErc1155ContractAddress": inputs["wt_erc1155ContractAddress"],
		"WtErc1155TokenId":         inputs["wt_erc1155TokenId"],
		"WtPublicKeysOut":          inputs["wt_publicKeysOut"],
		"WtValuesOut":              toStringSlice(inputs["wt_valuesOut"]),
		"WtAssetGroupPathElements": toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  inputs["wt_assetGroup_pathIndices"],
	}

	_, err := c.PostProof("/proof/erc155Fungible", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// Erc1155FungibleAuditorProof generates an ERC1155 fungible proof with auditor fields.
func (c *GnarkClient) Erc1155FungibleAuditorProof(inputs map[string]interface{}) (*ProofResponse, error) {
	pathElements := toInterfaceSlice(inputs["wt_pathElements"])
	split1, split2 := splitIntoTwo(pathElements, 8)

	payload := map[string]interface{}{
		"StMessage":                inputs["st_message"],
		"StTreeNumbers":            toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":            inputs["st_merkleRoots"],
		"StCommitmentOut":          inputs["st_commitmentsOut"],
		"StNullifiers":             inputs["st_nullifiers"],
		"StAssetGroupMerkleRoot":   toString(inputs["st_assetGroup_merkleRoot"]),
		"StAssetGroupTreeNumber":   toString(inputs["st_assetGroup_treeNumber"]),
		"WtPrivateKeysIn":          inputs["wt_privateKeysIn"],
		"WtValuesIn":               toStringSlice(inputs["wt_valuesIn"]),
		"WtPathElements":           []interface{}{split1, split2},
		"WtPathIndices":            toStringSlice(inputs["wt_pathIndices"]),
		"WtErc1155ContractAddress": inputs["wt_erc1155ContractAddress"],
		"WtErc1155TokenId":         inputs["wt_erc1155TokenId"],
		"WtPublicKeysOut":          inputs["wt_publicKeysOut"],
		"WtValuesOut":              toStringSlice(inputs["wt_valuesOut"]),
		"WtAssetGroupPathElements": toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  inputs["wt_assetGroup_pathIndices"],
		"StAuditorPublickey":       inputs["st_auditor_publicKey"],
		"StAuditorAuthKey":         inputs["st_auditor_authKey"],
		"StAuditorNonce":           inputs["st_auditor_nonce"],
		"StAuditorEncryptedValues": inputs["st_auditor_encryptedValues"],
		"WtAuditorRandom":          inputs["wt_auditor_random"],
	}

	_, err := c.PostProof("/proof/erc1155FungibleAuditor", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// Erc1155NonFungibleProof generates an ERC1155 non-fungible proof.
func (c *GnarkClient) Erc1155NonFungibleProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StMessage":                toString(inputs["st_message"]),
		"StTreeNumbers":            toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":            inputs["st_merkleRoots"],
		"StNullifiers":             inputs["st_nullifiers"],
		"StCommitmentOut":          inputs["st_commitmentsOut"],
		"StAssetGroupTreeNumber":   toStringSlice(inputs["st_assetGroup_treeNumbers"]),
		"StAssetGroupMerkleRoot":   inputs["st_assetGroup_merkleRoots"],
		"WtPrivateKeysIn":          inputs["wt_privateKeysIn"],
		"WtValues":                 inputs["wt_values"],
		"WtPathElements":           []interface{}{inputs["wt_pathElements"]},
		"WtPathIndices":            inputs["wt_pathIndices"],
		"WtErc1155TokenId":         inputs["wt_erc1155TokenIds"],
		"WtPublicKeysOut":          inputs["wt_publicKeysOut"],
		"WtErc1155ContractAddress": inputs["wt_erc1155ContractAddress"],
		"WtAssetGroupPathElements": toNestedStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  inputs["wt_assetGroup_pathIndices"],
	}

	_, err := c.PostProof("/proof/erc1155NonFungible", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// Erc1155NonFungibleWithAuditorProof generates an ERC1155 non-fungible proof with auditor.
func (c *GnarkClient) Erc1155NonFungibleWithAuditorProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StMessage":                toString(inputs["st_message"]),
		"StTreeNumbers":            toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":            inputs["st_merkleRoots"],
		"StNullifiers":             inputs["st_nullifiers"],
		"StCommitmentOut":          inputs["st_commitmentsOut"],
		"StAssetGroupTreeNumber":   toStringSlice(inputs["st_assetGroup_treeNumbers"]),
		"StAssetGroupMerkleRoot":   inputs["st_assetGroup_merkleRoots"],
		"WtPrivateKeysIn":          inputs["wt_privateKeysIn"],
		"WtValues":                 inputs["wt_values"],
		"WtPathElements":           []interface{}{inputs["wt_pathElements"]},
		"WtPathIndices":            inputs["wt_pathIndices"],
		"WtErc1155TokenIds":        inputs["wt_erc1155TokenIds"],
		"WtPublicKeysOut":          inputs["wt_publicKeysOut"],
		"WtErc1155ContractAddress": inputs["wt_erc1155ContractAddress"],
		"WtAssetGroupPathElements": toNestedStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  inputs["wt_assetGroup_pathIndices"],
		"StAuditorPublickey":       inputs["st_auditor_publicKey"],
		"StAuditorAuthKey":         inputs["st_auditor_authKey"],
		"StAuditorNonce":           inputs["st_auditor_nonce"],
		"StAuditorEncryptedValues": inputs["st_auditor_encryptedValues"],
		"WtAuditorRandom":          inputs["wt_auditor_random"],
	}

	_, err := c.PostProof("/proof/erc1155NonFungibleAuditor", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionInitMapProof generates an auction init proof (map-based pass-through, non-auditor).
func (c *GnarkClient) AuctionInitMapProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StBeacon":                 toString(inputs["st_beacon"]),
		"StVaultId":               toString(inputs["st_vaultId"]),
		"StAuctionId":             toString(inputs["st_auctionId"]),
		"StTreeNumber":            toString(inputs["st_treeNumber"]),
		"StMerkleRoot":            inputs["st_merkleRoot"],
		"StNullifier":             inputs["st_nullifier"],
		"StAssetGroupMerkleRoot":  toString(inputs["st_assetGroup_merkleRoot"]),
		"WtCommitment":            inputs["wt_commitment"],
		"WtPathElements":          inputs["wt_pathElements"],
		"WtPathIndices":           toString(inputs["wt_pathIndices"]),
		"WtPrivateKey":            inputs["wt_privateKey"],
		"WtIdParams":              inputs["wt_idParams"],
		"WtContractAddress":       inputs["wt_contractAddress"],
		"WtAssetGroupPathElements": toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  inputs["wt_assetGroup_pathIndices"],
	}

	_, err := c.PostProof("/proof/auctionInit", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionBidMapProof generates an auction bid proof (map-based pass-through, non-auditor).
func (c *GnarkClient) AuctionBidMapProof(inputs map[string]interface{}) (*ProofResponse, error) {
	pathElements := toInterfaceSlice(inputs["wt_pathElements"])
	split1, split2 := splitIntoTwo(pathElements, 8)

	payload := map[string]interface{}{
		"StAuctionId":             toString(inputs["st_auctionId"]),
		"StBlindedBid":            toString(inputs["st_blindedBid"]),
		"StVaultId":               toString(inputs["st_vaultId"]),
		"StTreeNumbers":           toStringSlice(inputs["st_treeNumbers"]),
		"StMerkleRoots":           toStringSlice(inputs["st_merkleRoots"]),
		"StNullifiers":            toStringSlice(inputs["st_nullifiers"]),
		"StCommitmentsOuts":       toStringSlice(inputs["st_commitmentsOut"]),
		"StAssetGroupMerkleRoot":  toString(inputs["st_assetGroup_merkleRoot"]),
		"WtBidAmount":             toString(inputs["wt_bidAmount"]),
		"WtBidRandom":             toString(inputs["wt_bidRandom"]),
		"WtPrivateKeys":           toStringSlice(inputs["wt_privateKeysIn"]),
		"WtPathElements": []interface{}{
			toStringSlice(split1),
			toStringSlice(split2),
		},
		"WtPathIndices":            toStringSlice(inputs["wt_pathIndices"]),
		"WtContractAddress":        toString(inputs["wt_contractAddress"]),
		"WtRecipientPK":            toStringSlice(inputs["wt_publicKeysOut"]),
		"WtValuesOut":              toStringSlice(inputs["wt_valuesOut"]),
		"WtAssetGroupPathElements": toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":  toString(inputs["wt_assetGroup_pathIndices"]),
		"WtIdParamsIn":             toNestedStringSlice(inputs["wt_idParamsIn"]),
		"WtIdParamsOut":            toNestedStringSlice(inputs["wt_idParamsOut"]),
	}

	_, err := c.PostProof("/proof/auctionBid", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionBidAuditorProof generates an auction bid proof with auditor fields.
func (c *GnarkClient) AuctionBidAuditorProof(inputs map[string]interface{}) (*ProofResponse, error) {
	pathElements := toInterfaceSlice(inputs["wt_pathElements"])
	split1, split2 := splitIntoTwo(pathElements, 8)

	payload := map[string]interface{}{
		"stBeacon":                 toString(inputs["st_beacon"]),
		"stAuctionId":              toString(inputs["st_auctionId"]),
		"stBlindedBid":             toString(inputs["st_blindedBid"]),
		"stVaultId":                toString(inputs["st_vaultId"]),
		"stTreeNumbers":            toStringSlice(inputs["st_treeNumbers"]),
		"stMerkleRoots":            toStringSlice(inputs["st_merkleRoots"]),
		"stNullifiers":             toStringSlice(inputs["st_nullifiers"]),
		"stCommitmentsOuts":        toStringSlice(inputs["st_commitmentsOut"]),
		"stAssetGroupTreeNumber":   toString(inputs["st_assetGroup_treeNumber"]),
		"stAssetGrupoMerkleRoot":   toString(inputs["st_assetGroup_merkleRoot"]),
		"stAuctioneerPublicKey":    toStringSlice(inputs["st_auctioneer_publicKey"]),
		"stAuctioneerAuthKey":      toStringSlice(inputs["st_auctioneer_authKey"]),
		"stAuctioneerNonce":        toString(inputs["st_auctioneer_nonce"]),
		"stAuctioneerEncryptedValues": toStringSlice(inputs["st_auctioneer_encryptedValues"]),
		"wtAuctioneerRandom":       toString(inputs["wt_auctioneer_random"]),
		"stAuditorPublicKey":       toStringSlice(inputs["st_auditor_publicKey"]),
		"stAuditorAuthKey":         toStringSlice(inputs["st_auditor_authKey"]),
		"stAuditorNonce":           toString(inputs["st_auditor_nonce"]),
		"stAuditorEncryptedValues": toStringSlice(inputs["st_auditor_encryptedValues"]),
		"wtAuditoRandom":           toString(inputs["wt_auditor_random"]),
		"wtBidAmount":              toString(inputs["wt_bidAmount"]),
		"wtBidRandom":              toString(inputs["wt_bidRandom"]),
		"wtPrivateKeysIn":          toStringSlice(inputs["wt_privateKeysIn"]),
		"wtPathElements": []interface{}{
			toStringSlice(split1),
			toStringSlice(split2),
		},
		"wtPathIndices":            toStringSlice(inputs["wt_pathIndices"]),
		"wtContractAddress":        toString(inputs["wt_contractAddress"]),
		"wtPublicKeysOut":          toStringSlice(inputs["wt_publicKeysOut"]),
		"wtAssetGroupPathElements": toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"wtAssetGroupPathIndices":  toString(inputs["wt_assetGroup_pathIndices"]),
		"wtIdParamsIn":             toNestedStringSlice(inputs["wt_idParamsIn"]),
		"wtIdParamsOut":            toNestedStringSlice(inputs["wt_idParamsOut"]),
	}

	_, err := c.PostProof("/proof/auctionBidAuditor", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionInitAuditorProof generates an auction init proof with auditor fields.
func (c *GnarkClient) AuctionInitAuditorProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StBeacon":                  toString(inputs["st_beacon"]),
		"StAuctionId":               toString(inputs["st_auctionId"]),
		"StVaultId":                 toString(inputs["st_vaultId"]),
		"StTreeNumber":              toString(inputs["st_treeNumber"]),
		"StMerkleRoot":              inputs["st_merkleRoot"],
		"StNullifier":               inputs["st_nullifier"],
		"StAuditorPublicKey":        toStringSlice(inputs["st_auditor_publicKey"]),
		"StAuditorAuthKey":          toStringSlice(inputs["st_auditor_authKey"]),
		"StAuditorNonce":            inputs["st_auditor_nonce"],
		"StAuditorEncryptedValues":  toStringSlice(inputs["st_auditor_encryptedValues"]),
		"WtAuditorRandom":           inputs["wt_auditor_random"],
		"StAssetGroupTreeNumber":    inputs["st_assetGroup_treeNumber"],
		"StAssetGroupMerkleRoot":    toString(inputs["st_assetGroup_merkleRoot"]),
		"WtCommitment":              inputs["wt_commitment"],
		"WtPathElements":            inputs["wt_pathElements"],
		"WtPathIndices":             toString(inputs["wt_pathIndices"]),
		"WtPrivateKey":              inputs["wt_privateKey"],
		"WtIdParams":                inputs["wt_idParams"],
		"WtContractAddress":         inputs["wt_contractAddress"],
		"WtAssetGroupPathElements":  toStringSlice(inputs["wt_assetGroup_pathElements"]),
		"WtAssetGroupPathIndices":   inputs["wt_assetGroup_pathIndices"],
	}

	_, err := c.PostProof("/proof/auctionInitAuditor", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionPrivateOpeningProof generates an auction private opening proof.
func (c *GnarkClient) AuctionPrivateOpeningProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StVaultId":   toString(inputs["st_auctionId"]),
		"StBlindedBid": toString(inputs["st_blindedBid"]),
		"WtBidAmount":  toString(inputs["wt_bidAmount"]),
		"WtBidRandom":  toString(inputs["wt_bidRandom"]),
	}

	_, err := c.PostProof("/proof/auctionPrivateOpening", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// AuctionNotWinningBidProof generates a proof that a bid is less than the winning bid.
func (c *GnarkClient) AuctionNotWinningBidProof(inputs map[string]interface{}) (*ProofResponse, error) {
	payload := map[string]interface{}{
		"StVaultId":              toString(inputs["st_auctionId"]),
		"StBlindedBidDifference": toString(inputs["st_blindedBidDifference"]),
		"StBidBlockNumber":       toString(inputs["st_bidBlockNumber"]),
		"StWinningBidBlockNumber": toString(inputs["st_winningBidBlockNumber"]),
		"WtBidAmount":            toString(inputs["wt_bidAmount"]),
		"WtBidRandom":            toString(inputs["wt_bidRandom"]),
		"WtWinningBidAmount":     toString(inputs["wt_winningBidAmount"]),
		"WtWinningBidRandom":     toString(inputs["wt_winningBidRandom"]),
	}

	_, err := c.PostProof("/proof/auctionNotWinning", payload)
	if err != nil {
		return nil, err
	}
	return &ProofResponse{Status: 200, Message: "ok"}, nil
}

// PrivateMintProof generates a private mint proof and returns the proof and public signals.
func (c *GnarkClient) PrivateMintProof(inputs map[string]interface{}) (*PrivateMintProofResponse, error) {
	salt := "0"
	if s, ok := inputs["salt"]; ok && s != nil {
		salt = toString(s)
	}

	payload := map[string]interface{}{
		"commitment":      toString(inputs["commitment"]),
		"contractAddress": toString(inputs["contractAddress"]),
		"tokenId":         toString(inputs["tokenId"]),
		"salt":            salt,
		"amount":          toString(inputs["amount"]),
		"publicKey":       toString(inputs["publicKey"]),
		"cipherText":      toString(inputs["cipherText"]),
	}

	body, err := c.PostProof("/proof/privateMint", payload)
	if err != nil {
		return nil, fmt.Errorf("PrivateMint proof generation failed: %w", err)
	}

	var resp PrivateMintProofResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse PrivateMint response: %w", err)
	}

	log.Println("PrivateMint proof generated successfully")
	return &resp, nil
}

// ChunkArray splits a slice into chunks of the given size.
func ChunkArray(arr []interface{}, chunkSize int) [][]interface{} {
	if chunkSize <= 0 {
		chunkSize = 8
	}

	var result [][]interface{}
	for i := 0; i < len(arr); i += chunkSize {
		chunk := make([]interface{}, chunkSize)
		for j := 0; j < chunkSize; j++ {
			if i+j < len(arr) {
				chunk[j] = arr[i+j]
			}
		}
		result = append(result, chunk)
	}
	return result
}

// SplitPathElements splits inputs["wt_pathElements"] into chunks of 8.
func SplitPathElements(inputs map[string]interface{}) [][]interface{} {
	elements := toInterfaceSlice(inputs["wt_pathElements"])
	return ChunkArray(elements, 8)
}

// --- helper functions ---

// toString converts any value to its string representation.
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// toStringSlice converts a slice of any type to a slice of strings.
func toStringSlice(v interface{}) []string {
	items := toInterfaceSlice(v)
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = toString(item)
	}
	return result
}

// toNestedStringSlice converts a [][]interface{} to [][]string.
func toNestedStringSlice(v interface{}) [][]string {
	outer := toInterfaceSlice(v)
	result := make([][]string, len(outer))
	for i, inner := range outer {
		result[i] = toStringSlice(inner)
	}
	return result
}

// toInterfaceSlice converts any slice-like value to []interface{}.
func toInterfaceSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	if s, ok := v.([]interface{}); ok {
		return s
	}
	if s, ok := v.([]string); ok {
		result := make([]interface{}, len(s))
		for i, item := range s {
			result[i] = item
		}
		return result
	}
	return nil
}

// splitIntoTwo splits a slice at the given index.
func splitIntoTwo(arr []interface{}, splitAt int) ([]interface{}, []interface{}) {
	if len(arr) <= splitAt {
		return arr, nil
	}
	return arr[:splitAt], arr[splitAt:]
}

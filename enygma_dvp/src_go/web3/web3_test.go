package web3

import (
	"fmt"
	"testing"

	"golang.org/x/crypto/sha3"
)

// --- ParseCustomError ---

func TestParseCustomErrorKnown(t *testing.T) {
	tests := []struct {
		contractName string
		errorString  string
	}{
		{"EnygmaDvp", "AuctionIdExists()"},
		{"EnygmaDvp", "InvalidMerkleRoot()"},
		{"EnygmaDvp", "InvalidNullifier()"},
		{"RaylsERC1155", "ZeroIdNotAllowed()"},
		{"RaylsERC1155", "NotImplemented()"},
		{"FungibilityMerkle", "InvalidFungibility()"},
		{"AbstractCoinVault", "RottenChallenge()"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.contractName, tt.errorString), func(t *testing.T) {
			selector := getErrorSelector(tt.errorString)
			errorData := selector + "0000000000000000000000000000000000000000000000000000000000000000"

			result := ParseCustomError(errorData, tt.contractName)
			if result != tt.errorString {
				t.Errorf("ParseCustomError(%s, %s) = %s, want %s", errorData[:10], tt.contractName, result, tt.errorString)
			}
		})
	}
}

func TestParseCustomErrorUnknownContract(t *testing.T) {
	result := ParseCustomError("0x12345678", "NonExistentContract")
	if result != "CustomErrorNotFound" {
		t.Errorf("Expected CustomErrorNotFound, got %s", result)
	}
}

func TestParseCustomErrorUnknownSelector(t *testing.T) {
	result := ParseCustomError("0xdeadbeef", "EnygmaDvp")
	if result != "CustomErrorNotFound" {
		t.Errorf("Expected CustomErrorNotFound, got %s", result)
	}
}

func TestParseCustomErrorTooShort(t *testing.T) {
	result := ParseCustomError("0x1234", "EnygmaDvp")
	if result != "CustomErrorNotFound" {
		t.Errorf("Expected CustomErrorNotFound for short input, got %s", result)
	}
}

func TestParseCustomErrorEmptyString(t *testing.T) {
	result := ParseCustomError("", "EnygmaDvp")
	if result != "CustomErrorNotFound" {
		t.Errorf("Expected CustomErrorNotFound for empty input, got %s", result)
	}
}

// --- CustomErrors map ---

func TestCustomErrorsContainsExpectedContracts(t *testing.T) {
	expectedContracts := []string{
		"RaylsERC1155",
		"EnygmaDvp",
		"FungibilityMerkle",
		"AbstractCoinVault",
	}

	for _, contract := range expectedContracts {
		if _, ok := CustomErrors[contract]; !ok {
			t.Errorf("CustomErrors should contain %s", contract)
		}
	}
}

func TestCustomErrorDecoderInitialized(t *testing.T) {
	if customErrorDecoder == nil {
		t.Fatal("customErrorDecoder should be initialized by init()")
	}

	for contractName, errors := range CustomErrors {
		decoderMap, ok := customErrorDecoder[contractName]
		if !ok {
			t.Errorf("customErrorDecoder missing contract %s", contractName)
			continue
		}

		if len(decoderMap) != len(errors) {
			t.Errorf("Contract %s: expected %d errors, got %d", contractName, len(errors), len(decoderMap))
		}
	}
}

func TestCustomErrorSelectorsAreUnique(t *testing.T) {
	for contractName, decoderMap := range customErrorDecoder {
		seen := make(map[string]string)
		for selector, errStr := range decoderMap {
			if existing, ok := seen[selector]; ok {
				t.Errorf("Contract %s: selector collision between %s and %s", contractName, existing, errStr)
			}
			seen[selector] = errStr
		}
	}
}

// helper: compute keccak256 selector for an error string
func getErrorSelector(errString string) string {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(errString))
	hash := hasher.Sum(nil)
	return fmt.Sprintf("0x%x", hash[:4])
}

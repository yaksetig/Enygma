package utils

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
)

func LoadProvingKey(curve ecc.ID, filename string) (groth16.ProvingKey, error) {
	safeFilename, err := safeProjectPath(filename, ".key")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(safeFilename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pk := groth16.NewProvingKey(curve)
	_, err = pk.ReadFrom(file)
	return pk, err
}

func LoadVerifyingKey(curve ecc.ID, filename string) (groth16.VerifyingKey, error) {
	safeFilename, err := safeProjectPath(filename, ".key")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(safeFilename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	vk := groth16.NewVerifyingKey(curve)
	_, err = vk.ReadFrom(file)
	return vk, err
}

func ParseBigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

func safeProjectPath(candidate string, allowedExts ...string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("file path must not be empty")
	}
	if strings.Contains(candidate, "\x00") {
		return "", fmt.Errorf("file path contains an invalid character")
	}
	if len(allowedExts) > 0 {
		ext := strings.ToLower(filepath.Ext(candidate))
		allowed := false
		for _, allowedExt := range allowedExts {
			if ext == strings.ToLower(allowedExt) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("file path %q has unsupported extension %q", candidate, ext)
		}
	}

	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	absCandidate = filepath.Clean(absCandidate)

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	allowedRoots := []string{cwd}
	for _, root := range strings.Split(os.Getenv("ENYGMA_ALLOWED_FILE_ROOTS"), ",") {
		root = strings.TrimSpace(root)
		if root != "" {
			allowedRoots = append(allowedRoots, root)
		}
	}

	for _, root := range allowedRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if isPathWithin(absCandidate, filepath.Clean(absRoot)) {
			return absCandidate, nil
		}
	}
	return "", fmt.Errorf("file path %q is outside the allowed project roots", candidate)
}

func isPathWithin(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

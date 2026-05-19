package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var contractNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

func safeABIFile(projectRoot, contractName string) (string, error) {
	if !contractNamePattern.MatchString(contractName) {
		return "", fmt.Errorf("contract name %q must contain only letters, numbers, underscore, or hyphen", contractName)
	}
	abiRoot := filepath.Join(projectRoot, "contracts", "abis")
	return safePathWithin(abiRoot, filepath.Join(abiRoot, contractName+".json"))
}

func safePathWithin(root, candidate string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	absRoot = filepath.Clean(absRoot)
	absCandidate = filepath.Clean(absCandidate)

	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return "", err
	}
	if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
		return absCandidate, nil
	}
	return "", fmt.Errorf("path %q is outside %q", candidate, root)
}

package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var circuitFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

func safeArtifactFile(projectRoot, contractPath string) (string, error) {
	if strings.TrimSpace(contractPath) == "" {
		return "", fmt.Errorf("contract path must not be empty")
	}
	if strings.Contains(contractPath, "\x00") || filepath.IsAbs(contractPath) {
		return "", fmt.Errorf("contract path %q is invalid", contractPath)
	}

	artifactRoot := filepath.Join(projectRoot, "artifacts", "contracts")
	candidate := filepath.Join(artifactRoot, contractPath+".json")
	return safePathWithin(artifactRoot, candidate)
}

func safeCircuitBuildJSON(projectRoot, filename string) (string, error) {
	stem, err := safeCircuitBuildStem(projectRoot, filename)
	if err != nil {
		return "", err
	}
	return stem + ".json", nil
}

func safeCircuitBuildStem(projectRoot, filename string) (string, error) {
	if err := validateCircuitFilename(filename); err != nil {
		return "", err
	}
	buildRoot := filepath.Join(projectRoot, "build")
	return safePathWithin(buildRoot, filepath.Join(buildRoot, filename))
}

func validateCircuitFilename(filename string) error {
	if !circuitFilenamePattern.MatchString(filename) {
		return fmt.Errorf("circuit filename %q must contain only letters, numbers, underscore, or hyphen", filename)
	}
	return nil
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

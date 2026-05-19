package script

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/consensys/gnark/backend/groth16"
)

func SavingFiles(pkFile string, vkFile string, pk groth16.ProvingKey, vk groth16.VerifyingKey) error {

	safePKFile, err := safeProjectPath(pkFile, ".key")
	if err != nil {
		return err
	}
	safeVKFile, err := safeProjectPath(vkFile, ".key")
	if err != nil {
		return err
	}

	fpk, err := os.Create(safePKFile)
	if err != nil {
		log.Fatalf("could not create proving.key: %v", err)
	}
	defer fpk.Close()
	if _, err := pk.WriteTo(fpk); err != nil {
		log.Fatalf("failed to write proving key: %v", err)
	}

	// 4) save the verifying key
	fvk, err := os.Create(safeVKFile)
	if err != nil {
		log.Fatalf("could not create verifying.key: %v", err)
	}
	defer fvk.Close()
	if _, err := vk.WriteTo(fvk); err != nil {
		log.Fatalf("failed to write verifying key: %v", err)
	}

	return nil
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

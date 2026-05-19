package utils

import (
	"fmt"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

var (
	A        = big.NewInt(168700)
	D        = big.NewInt(168696)
	gx, _    = new(big.Int).SetString("5299619240641551281634865583518297030282874472190772894086521144482721001553", 10)
	gy, _    = new(big.Int).SetString("16950150798460657717958625567821834550301663161624707787222815936182638968203", 10)
	GBabyJub = &babyjub.Point{X: gx, Y: gy}

	hx, _    = new(big.Int).SetString("10100005861917718053548237064487763771145251762383025193119768015180892676690", 10)
	hy, _    = new(big.Int).SetString("7512830269827713629724023825249861327768672768516116945507944076335453576011", 10)
	HBabyJub = &babyjub.Point{X: hx, Y: hy}

	P, _ = new(big.Int).SetString("2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)
)

func GetPkHash(sk *big.Int) *big.Int {
	inputs := []*big.Int{sk, sk}
	hash, _ := poseidon.Hash(inputs)
	return hash
}

func GetPK(v *big.Int) *babyjub.Point {
	rG := babyjub.NewPoint().Mul(v, GBabyJub)
	return rG
}

func GetH(v *big.Int) *babyjub.Point {
	rG := babyjub.NewPoint().Mul(v, HBabyJub)
	return rG
}

func PedersenCommitmentBabyJub(v *big.Int, r *big.Int) *babyjub.Point {

	vG := GetPK(v)
	vH := GetH(r)

	return AddPks(vG, vH)
}

func AddPks(pk1 *babyjub.Point, pk2 *babyjub.Point) *babyjub.Point {
	return babyjub.NewPoint().Projective().Add(pk1.Projective(), pk2.Projective()).Affine()
}

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

	pk := groth16.NewProvingKey(curve) // e.g., ecc.BN254
	_, err = pk.ReadFrom(file)
	return pk, err
}

// Load verifying key from file
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

	vk := groth16.NewVerifyingKey(curve) // e.g., ecc.BN254
	_, err = vk.ReadFrom(file)
	return vk, err
}

func ModHint(mod *big.Int, inputs []*big.Int, res []*big.Int) error {
	p := new(big.Int)
	p.SetString("2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)

	value := inputs[0]
	q := new(big.Int)
	r := new(big.Int)

	q.DivMod(value, p, r) // q = value / p, r = value % p

	res[0] = r // remainder
	res[1] = q // quotient
	return nil

	return nil
}

func ParseBigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

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

package joinSplitERC20

import(
	"math/big"
)
type JoinSplitERC20Request struct {
	StMessage            string      `json:"stMessage" binding:"required"`
	StTreeNumber         [2]string   `json:"stTreeNumber" binding:"required,len=2"`
	StMerkleRoots        [2]string   `json:"stMerkleRoots" binding:"required,len=2"`
	StNullifiers         [2]string   `json:"stNullifiers" binding:"required,len=2"`
	StCommitmentOut      [2]string   `json:"stCommitmentOut" binding:"required,len=2"`
	WtPrivateKeysIn      [2]string   `json:"wtPrivateKeysIn" binding:"required,len=2"`
	WtValuesIn           [2]string   `json:"wtValuesIn" binding:"required,len=2"`
	WtSaltsIn            [2]string   `json:"wtSaltsIn" binding:"required,len=2"`
	WtPathElements       [2][8]string `json:"wtPathElements" binding:"required,len=2,dive,len=8"`
	WtPathIndices        [2]string   `json:"wtPathIndices" binding:"required,len=2"`
	WtTokenId            string      `json:"wtTokenId" binding:"required"`
	WtSpendPublicKeysOut [2]string   `json:"wtSpendPublicKeysOut" binding:"required,len=2"`
	WtValuesOut          [2]string   `json:"wtValuesOut" binding:"required,len=2"`
	WtSaltsOut           [2]string   `json:"wtSaltsOut" binding:"required,len=2"`
}
type JoinSplitERC20Output struct{
	Proof 			[]*big.Int `json:"proof"`
	PublicSignal    []*big.Int `json:"publicSignal"`
}


//Definition already in templates folder
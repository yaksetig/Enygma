package proof

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"

	enygma "enygma/contracts"
	"enygma/config"
	"enygma/internal/types"
)

func GenerateProof(args *types.TransactionArgs, nullifier *big.Int,
	blockHash *big.Int, publicKey []*big.Int,
	previousCommit []enygma.IEnygmaPoint, txCommit []enygma.IEnygmaPoint,
	txValue []*big.Int, txRandom []*big.Int, secrets []*big.Int,
	k_index []*big.Int, hashArray []*big.Int, tagMessage []*big.Int, cfg *config.Config) *types.Response {

	var pkFinal []string
	var prevCommitFinal [][]string
	var txCommitFinal [][]string

	for _, pkVal := range publicKey {
		pkFinal = append(pkFinal, pkVal.String())
	}

	for _, value := range previousCommit {
		prevCommitFinal = append(prevCommitFinal, []string{value.C1.String(), value.C2.String()})
	}

	for _, commVal := range txCommit {
		txCommitFinal = append(txCommitFinal, []string{commVal.C1.String(), commVal.C2.String()})
	}

	hashedSharedSecrets := convertBigIntsToStrings(hashArray)
	sharedSecrets := convertBigIntsToStrings(secrets)

	txValuesString := convertBigIntsToStrings(txValue)
	txRandomString := convertBigIntsToStrings(txRandom)
	kIndexString := convertBigIntsToStrings(k_index)
	tagMessageString := convertBigIntsToStrings(tagMessage)

	jsonInfo := types.Proof{
		HashedSharedSecrets:       hashedSharedSecrets,
		PublicKey:                 pkFinal,
		PreviousCommit:            prevCommitFinal,
		TxCommit:                  txCommitFinal,
		BlockNumber:               blockHash.String(),
		AnonymitySet:              kIndexString,
		MessageTags:               tagMessageString,
		Nullifier:                 nullifier.String(),
		SenderID:                  strconv.FormatInt(int64(args.SenderId), 10),
		SharedSecrets:             sharedSecrets,
		SecretKey:                 args.Sk.String(),
		PreviousSenderBalance:     args.PreviousV.String(),
		PreviousSenderRandomValue: args.PreviousR.String(),
		TxValues:                  txValuesString,
		TxRandomValues:            txRandomString,
		SenderTxValue:             args.Value.String(),
	}

	jsonMar, _ := json.Marshal(jsonInfo)
	var jsonData = []byte(jsonMar)

	request, err := http.NewRequest("POST", cfg.ProofServerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	clientPost := &http.Client{}
	response, err := clientPost.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	body, _ := ioutil.ReadAll(response.Body)

	var result types.Response
	if e := json.Unmarshal(body, &result); e != nil {
		panic(e)
	}

	return &result
}

func convertBigIntsToStrings(bigInts []*big.Int) []string {
	strings := make([]string, len(bigInts))
	for i, bigInt := range bigInts {
		strings[i] = bigInt.String()
	}
	return strings
}

pragma circom 2.0.0;
include "../circomlib/circuits/comparators.circom";
include "../circomlib/circuits/babyjub.circom";
include "../circomlib/circuits/poseidon.circom";
include "../circomlib/circuits/gates.circom";
include "../circomlib/circuits/mux1.circom";
include "../primitives/MerkleProof.circom";
include "../primitives/Nullifier.circom";
include "../primitives/PublicKey.circom";
include "../primitives/Erc1155UniqueId.circom";

// tm_numOfTokens: max number of tokens in the batch commitment mode.
// st_membershipMerkleRoot
template Erc1155BatchTemplate(tm_numOfTokens, tm_merkleTreeDepth) {

    // Statement
    signal input st_message; // message that has been used for swap/mix functionality
    signal input st_treeNumbers[tm_numOfTokens];
    signal input st_merkleRoots[tm_numOfTokens]; // merkleRoot of the input erc1155 token
    signal input st_nullifiers[tm_numOfTokens];
    signal input st_commitmentsOut[tm_numOfTokens];
    signal input st_membershipTreeNumbers[tm_numOfTokens];
    signal input st_membershipMerkleRoots[tm_numOfTokens];


    // Witness
    signal input wt_privateKeys[tm_numOfTokens];
    signal input wt_values[tm_numOfTokens];
    signal input wt_pathElements[tm_numOfTokens][tm_merkleTreeDepth];
    signal input wt_pathIndices[tm_numOfTokens];
    signal input wt_erc1155TokenIds[tm_numOfTokens];
    signal input wt_outPublicKeys[tm_numOfTokens]; 
    signal input wt_erc1155ContractAddress;
    signal input wt_membershipPathElements[tm_numOfTokens][tm_merkleTreeDepth];
    signal input wt_membershipPathIndices[tm_numOfTokens];

    component cp_outPublicKeys[tm_numOfTokens];
    component cp_inPublicKeys[tm_numOfTokens];
    component cp_nullifiers[tm_numOfTokens];
    component cp_inCommitments[tm_numOfTokens];
    component cp_outCommitments[tm_numOfTokens];
    component cp_merkle[tm_numOfTokens];
    component cp_isDummyInputs[tm_numOfTokens];
    component cp_checkEqualIfIsNotDummys[tm_numOfTokens];
    component cp_checkEqualIfIsNotDummys2[tm_numOfTokens];
    component cp_checkEqualIfIsNotDummys3[tm_numOfTokens];

    for(var i =0; i<tm_numOfTokens; i++){
        cp_isDummyInputs[i] = IsZero();
        cp_isDummyInputs[i].in <== wt_values[i];
        
        //derive pubkey from the spending secret key
        cp_inPublicKeys[i] = PublicKey();
        cp_inPublicKeys[i].privateKey <== wt_privateKeys[i];

        //verify nullifier
        cp_nullifiers[i] = Nullifier();
        cp_nullifiers[i].privateKey <== wt_privateKeys[i];
        cp_nullifiers[i].pathIndex <== wt_pathIndices[i];

        cp_checkEqualIfIsNotDummys[i] = ForceEqualIfEnabled();
        cp_checkEqualIfIsNotDummys[i].enabled <== 1 - cp_isDummyInputs[i].out;
        cp_checkEqualIfIsNotDummys[i].in[0] <== cp_nullifiers[i].out;
        cp_checkEqualIfIsNotDummys[i].in[1] <== st_nullifiers[i];

        // compute and verify input commitment as single 4-input Poseidon hash
        cp_inCommitments[i] = Poseidon(4);
        cp_inCommitments[i].inputs[0] <== wt_erc1155ContractAddress;
        cp_inCommitments[i].inputs[1] <== wt_erc1155TokenIds[i];
        cp_inCommitments[i].inputs[2] <== wt_values[i];
        cp_inCommitments[i].inputs[3] <== cp_inPublicKeys[i].out;

        // verify merkleComp proof on the note commitment
        cp_merkle[i] = MerkleProof(tm_merkleTreeDepth);
        cp_merkle[i].leaf <== cp_inCommitments[i].out;
        cp_merkle[i].pathIndices <== wt_pathIndices[i];
        for(var j=0; j< tm_merkleTreeDepth; j++) {
            cp_merkle[i].pathElements[j] <== wt_pathElements[i][j];
        }

        cp_checkEqualIfIsNotDummys2[i] = ForceEqualIfEnabled();
        cp_checkEqualIfIsNotDummys2[i].enabled <== 1 - cp_isDummyInputs[i].out;
        cp_checkEqualIfIsNotDummys2[i].in[0] <== cp_merkle[i].root;
        cp_checkEqualIfIsNotDummys2[i].in[1] <== st_merkleRoots[i];

        // compute output commitment as single 4-input Poseidon hash
        cp_outCommitments[i] = Poseidon(4);
        cp_outCommitments[i].inputs[0] <== wt_erc1155ContractAddress;
        cp_outCommitments[i].inputs[1] <== wt_erc1155TokenIds[i];
        cp_outCommitments[i].inputs[2] <== wt_values[i];
        cp_outCommitments[i].inputs[3] <== wt_outPublicKeys[i];

        cp_checkEqualIfIsNotDummys3[i] = ForceEqualIfEnabled();
        cp_checkEqualIfIsNotDummys3[i].enabled <== 1 - cp_isDummyInputs[i].out;
        cp_checkEqualIfIsNotDummys3[i].in[0] <== st_commitmentsOut[i];
        cp_checkEqualIfIsNotDummys3[i].in[1] <== cp_outCommitments[i].out;

    }


}

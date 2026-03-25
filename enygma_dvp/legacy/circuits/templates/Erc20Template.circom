pragma circom 2.0.0;
include "../circomlib/circuits/comparators.circom";
include "../circomlib/circuits/babyjub.circom";
include "../circomlib/circuits/poseidon.circom";
include "../circomlib/circuits/gates.circom";
include "../circomlib/circuits/mux1.circom";
include "../primitives/MerkleProof.circom";
include "../primitives/Nullifier.circom";
include "../primitives/PublicKey.circom";

// From paper:
// "It allows users to prove the correctness of
// transferring NFT ownership from an input coin to an output
// one. It checks (i) knowledge of the sender’s seed and randomness 
// for the input commitment, (ii) validity of Merkle
// proof of membership, (iii) correctness of the serial number,
// (iv) and correctness of the output coin’s commitment on the
// same NFT."

// First Erc20 JoinSplit implementation that supports the mix of
// tokens from the same ERC20 contract

template Erc20Template(tm_nInputs, tm_mOutputs, tm_merkleTreeDepth, tm_range) {

    // --- Statement (public inputs) ---
    signal input st_message;
    signal input st_treeNumbers[tm_nInputs];
    signal input st_merkleRoots[tm_nInputs];
    signal input st_nullifiers[tm_nInputs];
    signal input st_commitmentsOut[tm_mOutputs];

    // --- Witness: inputs (coins being spent) ---
    signal input wt_privateKeysIn[tm_nInputs];     // sk_spend — proves ownership
    signal input wt_valuesIn[tm_nInputs];
    signal input wt_saltsIn[tm_nInputs];            // saltB received with each input note
    signal input wt_pathElements[tm_nInputs][tm_merkleTreeDepth];
    signal input wt_pathIndices[tm_nInputs];

    // Single token identifier shared across all inputs and outputs
    signal input wt_tokenId;

    // --- Witness: outputs (new notes being created) ---
    signal input wt_spendPublicKeysOut[tm_mOutputs]; // pk_spend of each recipient
    signal input wt_valuesOut[tm_mOutputs];
    signal input wt_saltsOut[tm_mOutputs];            // saltB from KEM for each output

    var inputsTotal = 0;
    var outputsTotal = 0;

    component cp_publicKeys[tm_nInputs];
    component cp_notesIn[tm_nInputs];
    component cp_nullfiers[tm_nInputs];
    component cp_merkles[tm_nInputs];
    component cp_isDummyInputs[tm_nInputs];
    component cp_checkEqualIfIsNotDummys[tm_nInputs];

    component cp_notesOut[tm_mOutputs];

    // --- verify input notes ---
    for(var i = 0; i < tm_nInputs; i++) {

        assert(wt_valuesIn[i] < tm_range);
        assert(0 <= wt_valuesIn[i]);

        // derive pk_spend from sk_spend inside the circuit
        cp_publicKeys[i] = PublicKey();
        cp_publicKeys[i].privateKey <== wt_privateKeysIn[i];

        // verify nullifier
        cp_nullfiers[i] = Nullifier();
        cp_nullfiers[i].privateKey <== wt_privateKeysIn[i];
        cp_nullfiers[i].pathIndex <== wt_pathIndices[i];
        cp_nullfiers[i].out === st_nullifiers[i];

        // V2 commitment: Poseidon(pk_spend, saltB, amount, tokenId)
        cp_notesIn[i] = Poseidon(4);
        cp_notesIn[i].inputs[0] <== cp_publicKeys[i].out;
        cp_notesIn[i].inputs[1] <== wt_saltsIn[i];
        cp_notesIn[i].inputs[2] <== wt_valuesIn[i];
        cp_notesIn[i].inputs[3] <== wt_tokenId;

        // verify Merkle proof on the input commitment
        cp_merkles[i] = MerkleProof(tm_merkleTreeDepth);
        cp_merkles[i].leaf <== cp_notesIn[i].out;
        cp_merkles[i].pathIndices <== wt_pathIndices[i];
        for(var j = 0; j < tm_merkleTreeDepth; j++) {
            cp_merkles[i].pathElements[j] <== wt_pathElements[i][j];
        }

        // skip Merkle check for dummy (zero-value) inputs
        cp_isDummyInputs[i] = IsZero();
        cp_isDummyInputs[i].in <== wt_valuesIn[i];

        cp_checkEqualIfIsNotDummys[i] = ForceEqualIfEnabled();
        cp_checkEqualIfIsNotDummys[i].enabled <== 1 - cp_isDummyInputs[i].out;
        cp_checkEqualIfIsNotDummys[i].in[0] <== st_merkleRoots[i];
        cp_checkEqualIfIsNotDummys[i].in[1] <== cp_merkles[i].root;

        inputsTotal += wt_valuesIn[i];
    }

    // --- verify output notes ---
    for(var i = 0; i < tm_mOutputs; i++) {
        assert(wt_valuesOut[i] < tm_range);
        assert(0 <= wt_valuesOut[i]);

        // V2 commitment: Poseidon(pk_spendRecipient, saltB, amount, tokenId)
        // pk_spendRecipient is Bob's spend public key, provided by Alice
        // saltB is the KEM-derived salt — matches what is in ciphertextII on-chain
        cp_notesOut[i] = Poseidon(4);
        cp_notesOut[i].inputs[0] <== wt_spendPublicKeysOut[i];
        cp_notesOut[i].inputs[1] <== wt_saltsOut[i];
        cp_notesOut[i].inputs[2] <== wt_valuesOut[i];
        cp_notesOut[i].inputs[3] <== wt_tokenId;
        cp_notesOut[i].out === st_commitmentsOut[i];

        outputsTotal += wt_valuesOut[i];
    }

    // check conservation: total inputs == total outputs
    inputsTotal === outputsTotal;
}

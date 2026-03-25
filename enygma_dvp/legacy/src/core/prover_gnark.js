/*
Low-level proof generation.
These functions post proof requests to the gnark HTTP server and return
the proof in {a, b, c} format suitable for on-chain verification.
*/
const axios = require("axios");

// Converts a flat 8-element proof array from the gnark server into
// the {a, b, c} struct expected by the Solidity GenericGroth16Verifier.
// proofArray = [a.X, a.Y, b.X.A1, b.X.A0, b.Y.A1, b.Y.A0, c.X, c.Y]
function formatGnarkProof(proofArray) {
  return {
    a: [proofArray[0], proofArray[1]],
    b: [
      [proofArray[2], proofArray[3]],
      [proofArray[4], proofArray[5]],
    ],
    c: [proofArray[6], proofArray[7]],
  };
}

async function GnarkProver(circuitInput, zkeyPath) {
  if (zkeyPath.includes("OwnershipErc721")) {
    return Erc721Proof(circuitInput);
  } else if (zkeyPath.includes("JoinSplitErc20")) {
    return Erc20Proof(circuitInput, zkeyPath);
  } else if (zkeyPath.includes("OwnershipErc1155NonFungibleWithAuditor")) {
    return Erc1155NonFungibleWithAuditorProof(circuitInput);
  } else if (zkeyPath.includes("OwnershipErc1155NonFungible")) {
    return Erc1155NonFungibleProof(circuitInput);
  } else if (zkeyPath.includes("JoinSplitErc1155WithAuditor")) {
    return Erc1155FungibleAuditorProof(circuitInput);
  } else if (zkeyPath.includes("JoinSplitErc1155")) {
    return Erc1155FungibleProof(circuitInput);
  } else if (zkeyPath.includes("AuctionInit_Auditor")) {
    return AuctionInitAuditorProof(circuitInput);
  } else if (zkeyPath.includes("AuctionBid_Auditor")) {
    return AuctionBidAuditorProof(circuitInput);
  } else if (zkeyPath.includes("AuctionPrivateOpening")) {
    return AuctionPrivateOpeningProof(circuitInput);
  } else if (zkeyPath.includes("AuctionNotWinningBid")) {
    console.log("AuctionNotWinningBidProof");
    console.log(circuitInput);
    return AuctionNotWinningBidProof(circuitInput);
  }
  return;
}

async function postRequestGnarkCircuit(url, data) {
  try {
    const res = await axios.post(url, data, {
      headers: {
        "Content-Type": "application/json",
      },
    });
    console.log("Response:", res.data);
    return res.data;
  } catch (err) {
    if (err.response) {
      console.error("Error:", err.response.status, err.response.data);
      throw new Error(`Gnark proof request failed: ${err.response.data}`);
    } else {
      console.error("Request failed:", err.message);
      throw new Error(`Gnark proof request failed: ${err.message}`);
    }
  }
}

async function Erc20Proof(inputs, zkeyPath) {
  if (zkeyPath === "./build/JoinSplitErc20_10_2.zkey") {
    console.log("inputs 20_2", inputs);
  }
  let stringURL =
    zkeyPath == "./build/JoinSplitErc20_10_2.zkey"
      ? "http://localhost:8081/proof/joinSplitERC20_10_2"
      : "http://localhost:8081/proof/joinSplitERC20";

  const chunks = splitPathElements(inputs);

  const proofData = await postRequestGnarkCircuit(stringURL, {
    StMessage: inputs.st_message.toString(),
    StTreeNumber: inputs.st_treeNumbers.map((k) => k.toString()),
    StMerkleRoots: inputs.st_merkleRoots,
    StNullifiers: inputs.st_nullifiers,
    StCommitmentOut: inputs.st_commitmentsOut,
    WtPrivateKeysIn: inputs.wt_privateKeysIn,
    WtPublicKeysOut: inputs.wt_publicKeysOut,
    WtPathElements: chunks,
    WtPathIndices: inputs.wt_pathIndices.map((k) => k.toString()),
    WtValuesIn: inputs.wt_valuesIn,
    WtValuesOut: inputs.wt_valuesOut,
    WtErc20ContractAddress: BigInt(inputs.wt_erc20ContractAddress).toString(),
    WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
    WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
  });

  return { proof: formatGnarkProof(proofData.proof) };
}

async function Erc721Proof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/ownershipERC721",
    {
      StMessage: inputs.st_message,
      StTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      StMerkleRoots: inputs.st_merkleRoots.map((k) => k.toString()),
      StNullifiers: inputs.st_nullifiers,
      StCommitmentOut: inputs.st_commitmentsOut,
      WtPrivateKeysIn: inputs.wt_privateKeysIn,
      WtValues: inputs.wt_values,
      WtPathElements: [inputs.wt_pathElements],
      WtPathIndices: inputs.wt_pathIndices,
      WtPublicKeysOut: inputs.wt_publicKeysOut,
      WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
      WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function Erc1155FungibleProof(inputs) {
  let split1 = [];
  let split2 = [];
  for (let i = 0; i < 16; i++) {
    if (i < 8) {
      split1.push(inputs.wt_pathElements[i]);
    } else {
      split2.push(inputs.wt_pathElements[i]);
    }
  }

  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/erc155Fungible",
    {
      StMessage: inputs.st_message,
      StTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      StMerkleRoots: inputs.st_merkleRoots,
      StCommitmentOut: inputs.st_commitmentsOut,
      StNullifiers: inputs.st_nullifiers,
      StAssetGroupMerkleRoot: inputs.st_assetGroup_merkleRoot.toString(),
      StAssetGroupTreeNumber: inputs.st_assetGroup_treeNumber.toString(),
      WtPrivateKeysIn: inputs.wt_privateKeysIn,
      WtValuesIn: inputs.wt_valuesIn.map((k) => k.toString()),
      WtPathElements: [split1, split2],
      WtPathIndices: inputs.wt_pathIndices.map((k) => k.toString()),
      WtErc1155ContractAddress: inputs.wt_erc1155ContractAddress,
      WtErc1155TokenId: inputs.wt_erc1155TokenId,
      WtPublicKeysOut: inputs.wt_publicKeysOut,
      WtValuesOut: inputs.wt_valuesOut.map((k) => k.toString()),
      WtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map((k) =>
        k.toString()
      ),
      WtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices,
      WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
      WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function Erc1155FungibleAuditorProof(inputs) {
  let split1 = [];
  let split2 = [];
  for (let i = 0; i < 16; i++) {
    if (i < 8) {
      split1.push(inputs.wt_pathElements[i]);
    } else {
      split2.push(inputs.wt_pathElements[i]);
    }
  }

  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/erc1155FungibleAuditor",
    {
      StMessage: inputs.st_message,
      StTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      StMerkleRoots: inputs.st_merkleRoots,
      StCommitmentOut: inputs.st_commitmentsOut,
      StNullifiers: inputs.st_nullifiers,
      StAssetGroupMerkleRoot: inputs.st_assetGroup_merkleRoot.toString(),
      StAssetGroupTreeNumber: inputs.st_assetGroup_treeNumber.toString(),
      WtPrivateKeysIn: inputs.wt_privateKeysIn,
      WtValuesIn: inputs.wt_valuesIn.map((k) => k.toString()),
      WtPathElements: [split1, split2],
      WtPathIndices: inputs.wt_pathIndices.map((k) => k.toString()),
      WtErc1155ContractAddress: inputs.wt_erc1155ContractAddress,
      WtErc1155TokenId: inputs.wt_erc1155TokenId,
      WtPublicKeysOut: inputs.wt_publicKeysOut,
      WtValuesOut: inputs.wt_valuesOut.map((k) => k.toString()),
      WtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map((k) =>
        k.toString()
      ),
      WtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices,
      StAuditorPublickey: inputs.st_auditor_publicKey,
      StAuditorAuthKey: inputs.st_auditor_authKey,
      StAuditorNonce: inputs.st_auditor_nonce,
      StAuditorEncryptedValues: inputs.st_auditor_encryptedValues,
      WtAuditorRandom: inputs.wt_auditor_random,
      WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
      WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

// Generates proof for transfer of the ownership of an Erc1155 coin
// Erc1155NonFungibleTemplate with size 1
async function Erc1155NonFungibleProof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/erc1155NonFungible",
    {
      StMessage: inputs.st_message.toString(),
      StTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      StMerkleRoots: inputs.st_merkleRoots,
      StNullifiers: inputs.st_nullifiers,
      StCommitmentOut: inputs.st_commitmentsOut,
      StAssetGroupTreeNumber: inputs.st_assetGroup_treeNumbers.map((k) =>
        k.toString()
      ),
      StAssetGroupMerkleRoot: inputs.st_assetGroup_merkleRoots,
      WtPrivateKeysIn: inputs.wt_privateKeysIn,
      WtValues: inputs.wt_values,
      WtPathElements: [inputs.wt_pathElements],
      WtPathIndices: inputs.wt_pathIndices,
      WtErc1155TokenId: inputs.wt_erc1155TokenIds,
      WtPublicKeysOut: inputs.wt_publicKeysOut,
      WtErc1155ContractAddress: inputs.wt_erc1155ContractAddress,
      WtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map(
        (arrayOfBigInts) =>
          arrayOfBigInts.map((bigIntValue) => bigIntValue.toString())
      ),
      WtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices,
      WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
      WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function Erc1155NonFungibleWithAuditorProof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/erc1155NonFungibleAuditor",
    {
      StMessage: inputs.st_message.toString(),
      StTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      StMerkleRoots: inputs.st_merkleRoots,
      StNullifiers: inputs.st_nullifiers,
      StCommitmentOut: inputs.st_commitmentsOut,
      StAssetGroupTreeNumber: inputs.st_assetGroup_treeNumbers.map((k) =>
        k.toString()
      ),
      StAssetGroupMerkleRoot: inputs.st_assetGroup_merkleRoots,
      WtPrivateKeysIn: inputs.wt_privateKeysIn,
      WtValues: inputs.wt_values,
      WtPathElements: [inputs.wt_pathElements],
      WtPathIndices: inputs.wt_pathIndices,
      WtErc1155TokenIds: inputs.wt_erc1155TokenIds,
      WtPublicKeysOut: inputs.wt_publicKeysOut,
      WtErc1155ContractAddress: inputs.wt_erc1155ContractAddress,
      WtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map(
        (arrayOfBigInts) =>
          arrayOfBigInts.map((bigIntValue) => bigIntValue.toString())
      ),
      WtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices,
      StAuditorPublickey: inputs.st_auditor_publicKey,
      StAuditorAuthKey: inputs.st_auditor_authKey,
      StAuditorNonce: inputs.st_auditor_nonce,
      StAuditorEncryptedValues: inputs.st_auditor_encryptedValues,
      WtAuditorRandom: inputs.wt_auditor_random,
      WtSaltsIn: inputs.wt_saltsIn.map((k) => k.toString()),
      WtSaltsOut: inputs.wt_saltsOut.map((k) => k.toString()),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function AuctionBidAuditorProof(inputs) {
  let split1 = [];
  let split2 = [];
  for (let i = 0; i < 16; i++) {
    if (i < 8) {
      split1.push(inputs.wt_pathElements[i]);
    } else {
      split2.push(inputs.wt_pathElements[i]);
    }
  }

  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/auctionBidAuditor",
    {
      stBeacon: inputs.st_beacon.toString(),
      stAuctionId: inputs.st_auctionId.toString(),
      stBlindedBid: inputs.st_blindedBid.toString(),
      stVaultId: inputs.st_vaultId.toString(),
      stTreeNumbers: inputs.st_treeNumbers.map((k) => k.toString()),
      stMerkleRoots: inputs.st_merkleRoots.map((k) => k.toString()),
      stNullifiers: inputs.st_nullifiers.map((k) => k.toString()),
      stCommitmentsOuts: inputs.st_commitmentsOut.map((k) => k.toString()),
      stAssetGroupTreeNumber: inputs.st_assetGroup_treeNumber.toString(),
      stAssetGrupoMerkleRoot: inputs.st_assetGroup_merkleRoot.toString(),
      // Auctioneer fields
      stAuctioneerPublicKey: inputs.st_auctioneer_publicKey.map((k) =>
        k.toString()
      ),
      stAuctioneerAuthKey: inputs.st_auctioneer_authKey.map((k) =>
        k.toString()
      ),
      stAuctioneerNonce: inputs.st_auctioneer_nonce.toString(),
      stAuctioneerEncryptedValues: inputs.st_auctioneer_encryptedValues.map(
        (k) => k.toString()
      ),
      wtAuctioneerRandom: inputs.wt_auctioneer_random.toString(),
      // Auditor fields
      stAuditorPublicKey: inputs.st_auditor_publicKey.map((k) => k.toString()),
      stAuditorAuthKey: inputs.st_auditor_authKey.map((k) => k.toString()),
      stAuditorNonce: inputs.st_auditor_nonce.toString(),
      stAuditorEncryptedValues: inputs.st_auditor_encryptedValues.map((k) =>
        k.toString()
      ),
      wtAuditoRandom: inputs.wt_auditor_random.toString(),
      wtBidAmount: inputs.wt_bidAmount.toString(),
      wtBidRandom: inputs.wt_bidRandom.toString(),
      wtPrivateKeysIn: inputs.wt_privateKeysIn.map((k) => k.toString()),
      wtPathElements: [
        split1.map((k) => k.toString()),
        split2.map((k) => k.toString()),
      ],
      wtPathIndices: inputs.wt_pathIndices.map((k) => k.toString()),
      wtContractAddress: inputs.wt_contractAddress.toString(),
      wtPublicKeysOut: inputs.wt_publicKeysOut.map((k) => k.toString()),
      wtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map((k) =>
        k.toString()
      ),
      wtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices.toString(),
      wtIdParamsIn: inputs.wt_idParamsIn.map((k) => k.map((x) => x.toString())),
      wtIdParamsOut: inputs.wt_idParamsOut.map((k) =>
        k.map((x) => x.toString())
      ),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function AuctionInitAuditorProof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/auctionInitAuditor",
    {
      StBeacon: inputs.st_beacon.toString(),
      StAuctionId: inputs.st_auctionId.toString(),
      StVaultId: inputs.st_vaultId.toString(),
      StTreeNumber: inputs.st_treeNumber.toString(),
      StMerkleRoot: inputs.st_merkleRoot,
      StNullifier: inputs.st_nullifier,

      StAuditorPublicKey: inputs.st_auditor_publicKey.map((k) => k.toString()),
      StAuditorAuthKey: inputs.st_auditor_authKey.map((k) => k.toString()),
      StAuditorNonce: inputs.st_auditor_nonce,
      StAuditorEncryptedValues: inputs.st_auditor_encryptedValues.map((k) =>
        k.toString()
      ),
      WtAuditorRandom: inputs.wt_auditor_random,

      StAssetGroupTreeNumber: inputs.st_assetGroup_treeNumber,
      StAssetGroupMerkleRoot: inputs.st_assetGroup_merkleRoot.toString(),

      WtCommitment: inputs.wt_commitment,
      WtPathElements: inputs.wt_pathElements,
      WtPathIndices: inputs.wt_pathIndices.toString(),
      WtPrivateKey: inputs.wt_privateKey,
      WtIdParams: inputs.wt_idParams,
      WtContractAddress: inputs.wt_contractAddress,

      WtAssetGroupPathElements: inputs.wt_assetGroup_pathElements.map((k) =>
        k.toString()
      ),
      WtAssetGroupPathIndices: inputs.wt_assetGroup_pathIndices,
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

async function AuctionPrivateOpeningProof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/auctionPrivateOpening",
    {
      StVaultId: inputs.st_auctionId.toString(),
      StBlindedBid: inputs.st_blindedBid.toString(),
      WtBidAmount: inputs.wt_bidAmount.toString(),
      WtBidRandom: inputs.wt_bidRandom.toString(),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

// Generates proof for AuctionNotWinningBid
async function AuctionNotWinningBidProof(inputs) {
  const proofData = await postRequestGnarkCircuit(
    "http://localhost:8081/proof/auctionNotWinning",
    {
      StVaultId: inputs.st_auctionId.toString(),
      StBlindedBidDifference: inputs.st_blindedBidDifference.toString(),
      StBidBlockNumber: inputs.st_bidBlockNumber.toString(),
      StWinningBidBlockNumber: inputs.st_winningBidBlockNumber.toString(),

      WtBidAmount: inputs.wt_bidAmount.toString(),
      WtBidRandom: inputs.wt_bidRandom.toString(),
      WtWinningBidAmount: inputs.wt_winningBidAmount.toString(),
      WtWinningBidRandom: inputs.wt_winningBidRandom.toString(),
    }
  );

  return { proof: formatGnarkProof(proofData.proof) };
}

function chunkArray(arr, chunkSize = 8) {
  console.log(typeof arr);
  console.log(arr);

  const result = [];

  for (let i = 0; i < arr.length; i += chunkSize) {
    const chunk = [];
    for (let j = 0; j < chunkSize; j++) {
      chunk.push(arr[i + j]);
    }
    result.push(chunk);
  }

  return result;
}

function splitPathElements(inputs) {
  return chunkArray(inputs.wt_pathElements, 8);
}

/**
 * Generate a ZK proof for private minting
 * @param {Object} inputs - The inputs for the proof
 * @param {string} inputs.commitment - The commitment to mint
 * @param {string} inputs.contractAddress - The contract address
 * @param {string} inputs.tokenId - The token ID
 * @param {string} inputs.salt - Random salt for commitment privacy
 * @param {string} inputs.amount - The amount to mint
 * @param {string} inputs.publicKey - The receiver's public key
 * @returns {Promise<{proof: string[], publicSignal: string[]}>}
 */
async function PrivateMintProof(inputs) {
  const url = "http://localhost:8081/proof/privateMint";

  try {
    const res = await axios.post(
      url,
      {
        commitment: inputs.commitment.toString(),
        contractAddress: inputs.contractAddress.toString(),
        tokenId: inputs.tokenId.toString(),
        salt: inputs.salt ? inputs.salt.toString() : "0",
        amount: inputs.amount.toString(),
        publicKey: inputs.publicKey.toString(),
        cipherText: inputs.cipherText.toString(),
      },
      {
        headers: {
          "Content-Type": "application/json",
        },
      }
    );

    console.log("PrivateMint proof generated successfully");
    return {
      proof: res.data.proof.map((p) => p.toString()),
      publicSignal: res.data.publicSignal.map((p) => p.toString()),
    };
  } catch (err) {
    if (err.response) {
      console.error("Error:", err.response.status, err.response.data);
      throw new Error(
        `PrivateMint proof generation failed: ${err.response.data}`
      );
    } else {
      console.error("Request failed:", err.message);
      throw new Error(`PrivateMint proof generation failed: ${err.message}`);
    }
  }
}

module.exports = {
  GnarkProver,
  PrivateMintProof,
};

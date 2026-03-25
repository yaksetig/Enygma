const hre = require("hardhat");
const web3 = require("web3");
const ethers = require("ethers");

// ABI fragment used to decode deposit(uint256[] params) calldata
const DEPOSIT_IFACE = new ethers.utils.Interface([
    'function deposit(uint256[] params)',
]);

const VAULT_TYPE_ERC20   = 'ERC20';
const VAULT_TYPE_ERC721  = 'ERC721';
const VAULT_TYPE_ERC1155 = 'ERC1155';

async function addAssetToGroup(admin, vaultId, uniqueIdParams, groupId, zkDvpContract) {
    const zkDvpAdmin = zkDvpContract.connect(admin);
    var tx = await zkDvpAdmin.addAssetToGroup(vaultId, uniqueIdParams, groupId);
    console.log(`Asset has been added to group ${groupId}`);

    return tx;
}

async function mintErc20(admin, account, depositAmount, erc20Contract) {
    const erc20Admin = erc20Contract.connect(admin);
    const accountAddress = await account.getAddress();
    var tx = await erc20Admin.mint(accountAddress, depositAmount);
    console.log("Minted ERC20 token for " + accountAddress);
    return tx;
}

async function mintErc721(admin, account, nft_id, erc721Contract) {
    const erc721Admin = erc721Contract.connect(admin);
    const accountAddress = await account.getAddress();
    var tx = await erc721Admin.mint(accountAddress, nft_id);
    console.log("Minted NFt for " + accountAddress);
    return tx;
}


async function mintErc1155(admin, account, token_id, amount, data, erc1155Contract) {
    // console.log("erc1155Address: "+ erc1155Contract.address);
    // console.log("tokenId: "+ BigInt(token_id));
    // console.log("amount: "+ amount);
    const erc1155Admin = erc1155Contract.connect(admin);
    const accountAddress = await account.getAddress();
    var tx =  await erc1155Admin.mint(accountAddress, token_id, amount, 0);
    console.log("Minted Erc1155 for " + accountAddress);

    return tx;
}


async function mintErc1155Batch(admin, account, tokenIds, amounts, data, erc1155Contract) {
    console.log("Admin.js: minting batch of Erc1155 ...");

    const erc1155Admin = erc1155Contract.connect(admin);
    const accountAddress = await account.getAddress();
    // const data = web3.eth.abi.encodeParameter('uint256[]', fungibilities);
    // console.log("mintBatch data: ", data);
    var tx =  await erc1155Admin.mintBatch(accountAddress, tokenIds, amounts, data);
    console.log("Minted Batch Erc1155 for " + accountAddress);

    return tx;
}

async function mintErc1155Fungible(admin, account, token_id, amount, erc1155Contract) {
    console.log("Admin.js: minting fungible Erc1155 ...");
    
    // data -> one fungible token
    const data = web3.eth.abi.encodeParameter('uint256[]', [0n]);
    // console.log("mintErc1155Fungible data = ", data);
    const erc1155Admin = erc1155Contract.connect(admin);
    const accountAddress = await account.getAddress();
    var tx =  await erc1155Admin.mint(accountAddress, token_id, amount, data);
    console.log(`Minted fungible Erc1155 with id=${token_id}, amount=${amount} for ${accountAddress}`);

    return tx;
}


async function mintErc1155NonFungible(admin, account, token_id, erc1155Contract) {
    console.log("Admin.js: minting non-fungible Erc1155 ...");
    const data = web3.eth.abi.encodeParameter('uint256[]', [1n]);
    const erc1155Admin = erc1155Contract.connect(admin);
    const accountAddress = await account.getAddress();
    var tx =  await erc1155Admin.mint(accountAddress, token_id, 1n, data);
    console.log(`Minted non-fungible Erc1155 with id=${token_id} for ${accountAddress}`);

    return tx;
}


/**
 * Scans all on-chain Commitment events emitted by a vault contract and builds
 * a registry that maps each commitment (as a decimal string) to information
 * about its origin.
 *
 * For commitments created by a plain deposit() call the registry entry
 * includes the depositor address, amount, public key and (for ERC-721/1155)
 * token id.  Commitments that originate from ZK-proof outputs (JoinSplit,
 * swap, settlement …) are still recorded, but the deposit fields are null and
 * isDeposit is false.
 *
 * @param {object} vaultInfo
 *   { contract: ethers.Contract, type: 'ERC20'|'ERC721'|'ERC1155', assetAddress: string }
 * @param {number} [fromBlock=0]  Block to start scanning from (inclusive).
 * @returns {Promise<object>}     coinRegistry  commitment string → entry
 */
async function buildCoinRegistry(vaultInfo, fromBlock = 0) {
    const coinRegistry = {};
    const { contract: vaultContract, type: vaultType, assetAddress } = vaultInfo;

    const filter  = vaultContract.filters.Commitment();
    const events  = await vaultContract.queryFilter(filter, fromBlock);

    for (const event of events) {
        const commitmentKey = event.args.commitment.toBigInt().toString();

        let depositor = null;
        let amount    = null;
        let publicKey = null;
        let tokenId   = null;
        let isDeposit = false;
        let txHash    = null;

        try {
            const tx  = await event.getTransaction();
            txHash    = tx.hash;
            depositor = tx.from;

            // Try to decode calldata as deposit(uint256[])
            try {
                const decoded = DEPOSIT_IFACE.decodeFunctionData('deposit', tx.data);
                const params  = decoded.params.map(p => BigInt(p.toString()));
                isDeposit     = true;

                if (vaultType === VAULT_TYPE_ERC20) {
                    // params: [amount, publicKey]
                    amount    = params[0].toString();
                    publicKey = params[1].toString();
                } else if (vaultType === VAULT_TYPE_ERC721) {
                    // params: [nft_id, publicKey]
                    tokenId   = params[0].toString();
                    amount    = '1';
                    publicKey = params[1].toString();
                } else if (vaultType === VAULT_TYPE_ERC1155) {
                    // params: [amount, tokenId, publicKey]
                    amount    = params[0].toString();
                    tokenId   = params[1].toString();
                    publicKey = params[2].toString();
                }
            } catch (_) {
                // Not a deposit tx — commitment comes from a ZK-proof output
                isDeposit = false;
            }
        } catch (e) {
            console.warn(`buildCoinRegistry: could not fetch tx for commitment ${commitmentKey}: ${e.message}`);
        }

        coinRegistry[commitmentKey] = {
            commitment:   commitmentKey,
            depositor,
            amount,
            publicKey,
            tokenId,
            isDeposit,
            txHash,
            blockNumber:  event.blockNumber,
            vaultType,
            assetAddress,
        };
    }

    return coinRegistry;
}

/**
 * Walks every leaf of the given off-chain MerkleTree (all previous trees plus
 * the current one) and cross-references each commitment against coinRegistry.
 *
 * Returns a structured report that includes:
 *  - recognizedCoins   : leaves matched in the registry (with full coin info)
 *  - unrecognizedCommitments : leaves not in the registry (opaque ZK outputs)
 *  - ownerSummary      : { [depositorAddress]: { owner, coins[], coinCount } }
 *
 * Note: the tree is append-only so all historical leaves appear here,
 * including ones whose funds have since been transferred.  To determine
 * which coins are still "live" you need to additionally track nullifiers
 * via the on-chain Nullifier events (not included here because nullifiers
 * are independent hashes and cannot be mapped back to commitments without
 * the owner's private key).
 *
 * @param {MerkleTree} merkleTree   Off-chain MerkleTree instance.
 * @param {object}     coinRegistry Result of buildCoinRegistry().
 * @returns {object}   Scan report.
 */
function scanTree(merkleTree, coinRegistry) {
    const report = {
        totalTrees:              merkleTree.prevTrees.length + 1,
        totalLeaves:             0,
        // isDeposit=true: depositor address and amount decoded from calldata
        depositCoins:            [],
        // isDeposit=false: commitment came from a ZK-proof output; the
        // on-chain tx sender is the relayer, not the true recipient
        opaqueCoins:             [],
        // commitment not found in the registry at all (event missed / anomaly)
        unrecognizedCommitments: [],
        // aggregated only from depositCoins (known owners)
        ownerSummary:            {},
    };

    function processLeaf(commitment, leafIndex, treeNumber) {
        const key      = commitment.toString();
        const coinInfo = coinRegistry[key];
        report.totalLeaves++;

        if (!coinInfo) {
            report.unrecognizedCommitments.push({ commitment: key, leafIndex, treeNumber });
            return;
        }

        const coin = { ...coinInfo, leafIndex, treeNumber };

        if (coin.isDeposit) {
            report.depositCoins.push(coin);

            const owner = coin.depositor || 'unknown';
            if (!report.ownerSummary[owner]) {
                report.ownerSummary[owner] = { owner, coins: [], coinCount: 0 };
            }
            report.ownerSummary[owner].coins.push(coin);
            report.ownerSummary[owner].coinCount++;
        } else {
            report.opaqueCoins.push(coin);
        }
    }

    // Previous trees: prevTrees[i] was tree number i
    for (let i = 0; i < merkleTree.prevTrees.length; i++) {
        const leaves = merkleTree.prevTrees[i][0] || [];
        for (let j = 0; j < leaves.length; j++) {
            processLeaf(leaves[j], j, i);
        }
    }

    // Current tree: tree number === merkleTree.treeNumber
    const currentLeaves = merkleTree.tree[0] || [];
    for (let i = 0; i < currentLeaves.length; i++) {
        processLeaf(currentLeaves[i], i, merkleTree.treeNumber);
    }

    return report;
}

/**
 * Prints a human-readable summary of a scanTree() report to the console.
 *
 * @param {object} report  Result of scanTree().
 */
function printScanReport(report) {
    const LINE = '='.repeat(50);
    console.log(`\n${LINE}`);
    console.log('         ADMIN TREE SCAN REPORT');
    console.log(LINE);
    console.log(`  Trees scanned            : ${report.totalTrees}`);
    console.log(`  Total leaf commitments   : ${report.totalLeaves}`);
    console.log(`  Deposit coins (owner known)     : ${report.depositCoins.length}`);
    console.log(`  Opaque coins  (ZK proof output) : ${report.opaqueCoins.length}`);
    console.log(`  Unrecognized  (not in events)   : ${report.unrecognizedCommitments.length}`);
    console.log(`${LINE}\n`);

    console.log('  FUND DISTRIBUTION BY DEPOSITOR ADDRESS');
    console.log(`  ${'─'.repeat(46)}`);

    const owners = Object.values(report.ownerSummary);
    if (owners.length === 0) {
        console.log('  (no deposit coins with known owner)');
    } else {
        for (const summary of owners) {
            console.log(`\n  Owner : ${summary.owner}`);
            console.log(`  Coins : ${summary.coinCount}`);
            for (const coin of summary.coins) {
                const amtStr    = coin.amount    !== null ? coin.amount    : '?';
                const tokenStr  = coin.tokenId   !== null ? ` tokenId:${coin.tokenId}` : '';
                const pubKeyStr = coin.publicKey  !== null
                    ? coin.publicKey.slice(0, 12) + '…'
                    : 'hidden';
                const txStr     = coin.txHash ? coin.txHash.slice(0, 14) + '…' : 'n/a';
                console.log(
                    `    [deposit] tree#${coin.treeNumber} leaf#${coin.leafIndex}` +
                    ` | amt:${amtStr}${tokenStr}` +
                    ` | pubKey:${pubKeyStr}` +
                    ` | tx:${txStr}`
                );
            }
        }
    }

    if (report.opaqueCoins.length > 0) {
        console.log(`\n  OPAQUE COINS (ZK proof outputs — true recipient hidden by design)`);
        console.log(`  ${'─'.repeat(46)}`);
        for (const coin of report.opaqueCoins) {
            const txStr = coin.txHash ? coin.txHash.slice(0, 14) + '…' : 'n/a';
            console.log(
                `    tree#${coin.treeNumber} leaf#${coin.leafIndex}` +
                ` | tx:${txStr}` +
                ` | commitment:${coin.commitment.slice(0, 16)}…`
            );
        }
    }

    if (report.unrecognizedCommitments.length > 0) {
        console.log(`\n  UNRECOGNIZED (commitment not found in any on-chain event — anomaly)`);
        console.log(`  ${'─'.repeat(46)}`);
        for (const uc of report.unrecognizedCommitments) {
            console.log(`    tree#${uc.treeNumber} leaf#${uc.leafIndex} | ${uc.commitment}`);
        }
    }

    console.log(`\n${LINE}\n`);
}

module.exports = {
    addAssetToGroup,
    mintErc20,
    mintErc721,
    mintErc1155,
    mintErc1155Batch,
    mintErc1155Fungible,
    mintErc1155NonFungible,
    // Admin tree-scan
    VAULT_TYPE_ERC20,
    VAULT_TYPE_ERC721,
    VAULT_TYPE_ERC1155,
    buildCoinRegistry,
    scanTree,
    printScanReport,
};

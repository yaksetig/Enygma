/* global describe it before */
/**
 * test/18_v2_erc20.test.js
 *
 * E2E integration test for the V2 non-interactive ERC-20 flow:
 *   Deposit → Transfer → Withdraw
 *
 * The V2 commitment convention is:
 *   commitment = Poseidon(pk_spend, salt, amount, tokenId)   (4-input Poseidon)
 *
 * For withdrawals the recipient address acts as pk_spend with salt=0:
 *   withdrawal_commitment = Poseidon(uint160(recipient), 0, amount, tokenId)
 *
 * Prerequisites (must be running before test):
 *   - Hardhat node:  npx hardhat node
 *   - Gnark server:  cd gnark_circuits && go run main.go
 *
 * Run:
 *   npx hardhat test test/18_v2_erc20.test.js --network localhost
 */

const { expect }   = require("chai");
const axios        = require("axios");
const { poseidon } = require("circomlibjs");
const jsUtils      = require("../src/core/utils");
const testHelpers  = require("./testHelpers.js");
const MerkleTree   = require("../src/core/merkle");

const dvpConf    = require("../enygmadvp.config.json");
const TREE_DEPTH = dvpConf["circom"]["meta-parameters"]["tree-depth"];

const GNARK_URL = "http://localhost:8081";
const TOKEN_ID  = 0n;   // tokenId shared across all notes in this test

// ─── helpers ─────────────────────────────────────────────────────────────────

/** V2 ERC-20 commitment: Poseidon4(pk_spend, salt, amount, tokenId) */
function erc20CommitmentV2(pkSpend, salt, amount, tokenId) {
    return poseidon([pkSpend, salt, amount, tokenId]);
}

/** Spend-key nullifier: Poseidon(sk, pathIndices) */
function getNullifier(sk, pathIndices) {
    return poseidon([sk, pathIndices]);
}

/**
 * Build the on-chain ProofReceipt statement in the NON-INTERLEAVED layout
 * expected by checkReceiptConditions for 2 inputs, 2 outputs:
 *
 *   [message, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1]
 */
function buildStatement(message, trees, roots, nullifiers, commitments) {
    return [message, ...trees, ...roots, ...nullifiers, ...commitments];
}

/**
 * BigInt-safe JSON parser.
 *
 * The gnark server returns proof coordinates as plain JSON numbers, but they
 * are ~77-digit BN254 field elements that exceed JavaScript's safe integer
 * range (2^53).  Standard JSON.parse silently loses precision, causing ethers
 * to throw an overflow error when it tries to create a BigNumber from the
 * rounded float.
 *
 * Fix: before parsing, wrap every bare number ≥ 16 digits in double-quotes so
 * that JSON.parse treats them as strings.  The values are then handed to
 * ethers as strings, which BigNumber.from() handles correctly.
 */
function parseBigIntJSON(text) {
    // Quote all bare integers ≥16 digits in any JSON position
    // (after : for object values, after [ or , for array elements)
    const safe = text.replace(/([,\[:])\s*(-?\d{16,})/g, (_, delim, num) => `${delim}"${num}"`);
    return JSON.parse(safe);
}

/** Format an 8-element gnark proof string array into the SnarkProof struct layout. */
function formatGnarkProof(p) {
    // p elements are strings (from parseBigIntJSON); ethers BigNumber.from accepts strings
    return {
        a: [p[0], p[1]],
        b: [[p[2], p[3]], [p[4], p[5]]],
        c: [p[6], p[7]],
    };
}

/** Zero-filled path elements for a dummy (zero-value) input. */
function zeroPaddedPath(depth) {
    return new Array(depth).fill("0");
}

/**
 * POST a joinSplitERC20 proof request to the gnark server and return the
 * 8-element proof array (values as strings to preserve BN254 field precision).
 */
async function gnarkJoinSplitErc20(payload) {
    const res = await axios.post(
        `${GNARK_URL}/proof/joinSplitERC20`,
        payload,
        {
            headers:           { "Content-Type": "application/json" },
            transformResponse: [data => data],   // prevent axios auto-JSON-parse
        },
    );
    const parsed = parseBigIntJSON(res.data);
    return parsed.proof;   // [Ax, Ay, BX1, BX0, BY1, BY0, Cx, Cy] as strings
}

// ─── shared state ─────────────────────────────────────────────────────────────

let admin;
let alice      = {};
let bob        = {};
let contracts;
let merkleTree20;

// ─── test suite ───────────────────────────────────────────────────────────────

describe("V2 ERC-20: Deposit → Transfer → Withdraw", () => {

    // Alice's spend key and note data
    let aliceSk, alicePk, aliceSalt, aliceCmt;

    // Bob's spend key and note data (received via transfer)
    let bobSk, bobPk, bobSalt, bobCmt;

    // ── 1. Deploy contracts ────────────────────────────────────────────────────
    it("deploys and initialises the protocol", async () => {
        let users, merkleTrees;
        [admin, users, contracts, merkleTrees] =
            await testHelpers.deployForTest(2 /* Alice=users[0], Bob=users[1] */);

        alice.wallet = users[0].wallet;
        bob.wallet   = users[1].wallet;
        merkleTree20 = merkleTrees["ERC20"].tree;

        console.log("  Alice:", alice.wallet.address);
        console.log("  Bob  :", bob.wallet.address);
    });

    // ── 2. Mint tokens for Alice ───────────────────────────────────────────────
    it("admin mints 200 ERC20 tokens to Alice", async () => {
        await contracts.erc20.connect(admin).mint(alice.wallet.address, 200n);

        const balance = await contracts.erc20.balanceOf(alice.wallet.address);
        expect(balance.toBigInt()).to.equal(200n);
    });

    // ── 3. Deposit V2 ─────────────────────────────────────────────────────────
    it("Alice deposits 100 tokens with a V2 commitment", async () => {
        // Alice generates her spend key pair and a random salt
        aliceSk   = jsUtils.randomInField();
        alicePk   = poseidon([aliceSk]);
        aliceSalt = jsUtils.randomInField();
        aliceCmt  = erc20CommitmentV2(alicePk, aliceSalt, 100n, TOKEN_ID);

        const erc20 = contracts.erc20.connect(alice.wallet);
        const vault = contracts.erc20CoinVault.connect(alice.wallet);

        await erc20.approve(contracts.erc20CoinVault.address, 100n);

        // depositV2(params, ciphertextI, ciphertextII) — empty ciphertexts are fine
        // for this test since we are not testing the scanning flow
        const tx = await vault.depositV2([100n, aliceCmt], "0x", "0x");
        const rc = await tx.wait();

        // Commitment event must contain Alice's computed commitment
        const cmtEvent = rc.events.find(e => e.event === "Commitment");
        expect(cmtEvent, "Commitment event missing").to.not.be.undefined;
        expect(cmtEvent.args.commitment.toBigInt()).to.equal(aliceCmt);

        // Mirror the on-chain leaf insertion in our local tree
        merkleTree20.insertLeaves([aliceCmt]);

        console.log("  aliceCmt  :", aliceCmt.toString());
        console.log("  Merkle root:", merkleTree20.root.toString());
    });

    // ── 4. Transfer V2 (Alice → Bob) ──────────────────────────────────────────
    it("Alice transfers 100 tokens to Bob via V2 JoinSplit proof", async () => {
        // Generate Alice's Merkle proof before inserting any new leaves
        const aliceProof = merkleTree20.generateProof(aliceCmt);

        // Bob generates a spend key pair and a random salt for his new note
        bobSk   = jsUtils.randomInField();
        bobPk   = poseidon([bobSk]);
        bobSalt = jsUtils.randomInField();
        bobCmt  = erc20CommitmentV2(bobPk, bobSalt, 100n, TOKEN_ID);

        // Dummy second output: amount = 0 (Alice keeps no change)
        const dummySk   = jsUtils.randomInField();
        const dummyPk   = poseidon([dummySk]);
        const dummySalt = jsUtils.randomInField();
        const dummyCmt  = erc20CommitmentV2(dummyPk, dummySalt, 0n, TOKEN_ID);

        // Nullifiers
        // Real: Poseidon(alice_sk, alice_path_indices)
        const aliceNullifier = getNullifier(aliceSk, aliceProof.indices);
        // Dummy input (index=1): Poseidon(dummySk, 0) — circuit always asserts this
        const dummyInputSk   = jsUtils.randomInField();
        const dummyNullifier = getNullifier(dummyInputSk, 0n);

        // Circuit public signal values for the 2-input, 2-output proof
        const tree0 = BigInt(merkleTree20.lastTreeNumber);
        const tree1 = 0n;  // dummy input — root=0 signals "skip"
        const root0 = aliceProof.root;
        const root1 = 0n;  // dummy root → skips root/nullifier check on-chain

        // Statement in non-interleaved layout (matches checkReceiptConditions):
        //   [message, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1]
        const statement = buildStatement(
            0n,
            [tree0, tree1],
            [root0, root1],
            [aliceNullifier, dummyNullifier],
            [bobCmt, dummyCmt],
        );

        // Build gnark server payload
        const proofArr = await gnarkJoinSplitErc20({
            StMessage:            "0",
            StTreeNumber:         [tree0.toString(), tree1.toString()],
            StMerkleRoots:        [root0.toString(), root1.toString()],
            StNullifiers:         [aliceNullifier.toString(), dummyNullifier.toString()],
            StCommitmentOut:      [bobCmt.toString(), dummyCmt.toString()],
            WtPrivateKeysIn:      [aliceSk.toString(), dummyInputSk.toString()],
            WtValuesIn:           ["100", "0"],
            WtSaltsIn:            [aliceSalt.toString(), "0"],
            WtPathElements:       [
                aliceProof.elements.map(e => e.toString()),
                zeroPaddedPath(TREE_DEPTH),
            ],
            WtPathIndices:        [aliceProof.indices.toString(), "0"],
            WtTokenId:            TOKEN_ID.toString(),
            WtSpendPublicKeysOut: [bobPk.toString(), dummyPk.toString()],
            WtValuesOut:          ["100", "0"],
            WtSaltsOut:           [bobSalt.toString(), dummySalt.toString()],
        });

        const proofReceipt = {
            proof:           formatGnarkProof(proofArr),
            statement:       statement,
            numberOfInputs:  2,
            numberOfOutputs: 2,
        };

        const vault = contracts.erc20CoinVault.connect(alice.wallet);
        const tx = await vault.transferV2(
            proofReceipt,
            ["0x", "0x"],   // ciphertextI per output
            ["0x", "0x"],   // ciphertextII per output
        );
        const rc = await tx.wait();

        // EncryptedNote events should include Bob's commitment
        const encNoteEvents = rc.events.filter(e => e.event === "EncryptedNote");
        const encCmts = encNoteEvents.map(e => e.args.commitment.toBigInt());
        expect(encCmts).to.include(bobCmt, "EncryptedNote for Bob not found");

        // Mirror on-chain leaf insertion in local tree
        merkleTree20.insertLeaves([bobCmt, dummyCmt]);

        console.log("  bobCmt    :", bobCmt.toString());
        console.log("  Merkle root:", merkleTree20.root.toString());
    });

    // ── 5. Withdraw V2 (Bob → external) ───────────────────────────────────────
    it("Bob withdraws 100 tokens via withdrawV2", async () => {
        // Bob generates his Merkle proof (tree now contains 3 leaves)
        const bobProof = merkleTree20.generateProof(bobCmt);

        // Withdrawal output commitment: Poseidon(uint160(bob.address), 0, 100, tokenId)
        // salt=0 is fixed — makes the commitment publicly recomputatable on-chain
        const bobAddrInt  = BigInt(bob.wallet.address);
        const withdrawCmt = erc20CommitmentV2(bobAddrInt, 0n, 100n, TOKEN_ID);

        // Dummy second output: amount=0, salt=0
        const dummySk2   = jsUtils.randomInField();
        const dummyPk2   = poseidon([dummySk2]);
        const dummyCmt2  = erc20CommitmentV2(dummyPk2, 0n, 0n, TOKEN_ID);

        // Nullifiers
        const bobNullifier = getNullifier(bobSk, bobProof.indices);
        const dummyInputSk = jsUtils.randomInField();
        const dummyNull    = getNullifier(dummyInputSk, 0n);

        const tree0 = BigInt(merkleTree20.lastTreeNumber);
        const tree1 = 0n;
        const root0 = bobProof.root;
        const root1 = 0n;

        const statement = buildStatement(
            0n,
            [tree0, tree1],
            [root0, root1],
            [bobNullifier, dummyNull],
            [withdrawCmt, dummyCmt2],
        );

        const proofArr = await gnarkJoinSplitErc20({
            StMessage:            "0",
            StTreeNumber:         [tree0.toString(), tree1.toString()],
            StMerkleRoots:        [root0.toString(), root1.toString()],
            StNullifiers:         [bobNullifier.toString(), dummyNull.toString()],
            StCommitmentOut:      [withdrawCmt.toString(), dummyCmt2.toString()],
            WtPrivateKeysIn:      [bobSk.toString(), dummyInputSk.toString()],
            WtValuesIn:           ["100", "0"],
            WtSaltsIn:            [bobSalt.toString(), "0"],
            WtPathElements:       [
                bobProof.elements.map(e => e.toString()),
                zeroPaddedPath(TREE_DEPTH),
            ],
            WtPathIndices:        [bobProof.indices.toString(), "0"],
            WtTokenId:            TOKEN_ID.toString(),
            WtSpendPublicKeysOut: [bobAddrInt.toString(), dummyPk2.toString()],
            WtValuesOut:          ["100", "0"],
            WtSaltsOut:           ["0", "0"],   // withdrawal uses fixed salt=0 for both outputs
        });

        const proofReceipt = {
            proof:           formatGnarkProof(proofArr),
            statement:       statement,
            numberOfInputs:  2,
            numberOfOutputs: 2,
        };

        const bobBalanceBefore = (await contracts.erc20.balanceOf(bob.wallet.address)).toBigInt();

        // vault.withdrawV2([amount, tokenId], recipient, proofReceipt)
        const vault = contracts.erc20CoinVault.connect(bob.wallet);
        const tx = await vault.withdrawV2(
            [100n, TOKEN_ID],
            bob.wallet.address,
            proofReceipt,
        );
        await tx.wait();

        const bobBalanceAfter = (await contracts.erc20.balanceOf(bob.wallet.address)).toBigInt();
        expect(bobBalanceAfter - bobBalanceBefore).to.equal(
            100n,
            "Bob's balance should increase by 100",
        );

        console.log("  withdrawCmt:", withdrawCmt.toString());
        console.log("  Bob balance before:", bobBalanceBefore.toString(),
                    "→ after:", bobBalanceAfter.toString());
    });

    // ── 6. Final balance assertions ────────────────────────────────────────────
    it("verifies final ERC-20 balances are correct", async () => {
        const aliceBal = (await contracts.erc20.balanceOf(alice.wallet.address)).toBigInt();
        const bobBal   = (await contracts.erc20.balanceOf(bob.wallet.address)).toBigInt();
        const vaultBal = (await contracts.erc20.balanceOf(contracts.erc20CoinVault.address)).toBigInt();

        // Alice minted 200, deposited 100 → keeps 100
        expect(aliceBal).to.equal(100n, "Alice should have 100 remaining");
        // Bob started with 0, received 100 via withdrawal
        expect(bobBal).to.equal(100n,   "Bob should have 100");
        // Vault held 100 (Alice's deposit) and paid it to Bob
        expect(vaultBal).to.equal(0n,   "Vault should be empty");

        console.log("  Alice:", aliceBal.toString(),
                    " Bob:", bobBal.toString(),
                    " Vault:", vaultBal.toString());
    });
});

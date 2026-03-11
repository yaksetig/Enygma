/* global describe it ethers before */
/**
 * ============================================================
 *  ADMIN SCAN WALKTHROUGH — A Step-by-Step Learning Test
 * ============================================================
 *
 *  PURPOSE
 *  -------
 *  This test is intentionally verbose and educational.  Every `it` block
 *  contains a plain-English comment that explains WHAT is happening and
 *  WHY it matters before the code runs.  Read each comment first, then
 *  follow the code.
 *
 *  WHAT WE ARE TESTING
 *  -------------------
 *  The admin scan is a surveillance tool the protocol operator uses to
 *  get a bird's-eye view of the Merkle tree without knowing the private
 *  keys of any user.  It answers three questions:
 *
 *    1. Which commitments came from explicit deposits?
 *       → The depositor address and amount are visible in calldata.
 *
 *    2. Which commitments came from ZK-proof operations (JoinSplit etc.)?
 *       → These are "opaque": the true recipient is hidden; only the
 *         relayer (tx sender) is visible, which reveals nothing.
 *
 *    3. Are there any commitments the event log does not recognise?
 *       → Should be zero in a healthy protocol; non-zero would be an anomaly.
 *
 *  SCENARIO USED IN THIS FILE
 *  --------------------------
 *  Three users: Alice, Bob, Carol.
 *
 *    Step A – Alice deposits 1 000 tokens (leaf #0)
 *    Step B – Bob   deposits   600 tokens (leaf #1)
 *    Step C – Carol deposits   200 tokens (leaf #2)
 *    Step D – buildCoinRegistry: snapshot what is on-chain so far
 *             → 3 deposit coins, 0 opaque coins
 *    Step E – Bob JoinSplits his 600-token coin into two 300-token outputs
 *             (leaf #3 and leaf #4).  Bob's original leaf #1 is now spent
 *             (its nullifier is published on-chain, but the tree is append-only
 *             so leaf #1 still appears as a leaf).
 *    Step F – buildCoinRegistry again: now includes the 2 JoinSplit outputs
 *             → 3 deposit coins, 2 opaque coins
 *    Step G – scanTree: walk every leaf and build the full admin report
 *    Step H – printScanReport: render the human-readable summary
 *    Step I – edge case: scanTree with an empty registry
 */

const { expect }   = require("chai");
const prover       = require("../src/core/prover");
const jsUtils      = require("../src/core/utils");
const testHelpers  = require("./testHelpers.js");
const adminActions = require("../src/core/endpoints/admin.js");
const MerkleTree   = require("../src/core/merkle");

const dvpConf    = require("../enygmadvp.config.json");
const TREE_DEPTH = dvpConf["circom"]["meta-parameters"]["tree-depth"];

// ─── shared state ────────────────────────────────────────────────────────────
let admin;
let alice = {};
let bob   = {};
let carol = {};
let contracts;
let merkleTree20; // the off-chain ERC20 Merkle tree

// Deposit amounts chosen so they are easy to reason about
const ALICE_AMOUNT = 1000n;
const BOB_AMOUNT   =  600n;
const CAROL_AMOUNT =  200n;

// We will capture the on-chain commitments as we go
let aliceCoin; // { commitment, proof, root, treeNumber, amount, key }
let bobCoin;
let carolCoin;

// Registry snapshots
let registryBeforeJoinSplit;
let registryAfterJoinSplit;

// Final scan report
let finalReport;

// ─── helpers ─────────────────────────────────────────────────────────────────

/**
 * depositErc20
 * Mints `amount` of the test ERC-20 to `userWallet`, approves the vault,
 * calls vault.deposit(), inserts the resulting commitment into the local
 * Merkle tree, and returns a coin object.
 *
 * WHY A HELPER?
 * The deposit pattern (mint → approve → deposit → capture commitment →
 * insertLeaf) repeats for every depositor.  Extracting it keeps each `it`
 * block focused on the concept being explained rather than the ceremony.
 */
async function depositErc20(userWallet, amount) {
    const erc20Owner = contracts.erc20.connect(admin);
    const erc20User  = contracts.erc20.connect(userWallet);
    const vault      = contracts.erc20CoinVault.connect(userWallet);

    // Generate a fresh Baby-JubJub key pair for this coin.
    // The PUBLIC key goes into the commitment (visible on-chain).
    // The PRIVATE key is only ever known to the depositor.
    const key = jsUtils.newKeyPair();

    await erc20Owner.mint(userWallet.address, amount);
    await erc20User.approve(contracts.erc20CoinVault.address, amount);

    // vault.deposit([amount, publicKey])
    // This emits a Commitment event with:
    //   - commitment = Poseidon(amount, publicKey, nonce)
    // The calldata (amount + publicKey) is publicly readable, which is
    // exactly what buildCoinRegistry decodes later.
    const tx  = await vault.deposit([amount, key.publicKey]);
    const cmt = await testHelpers.getCommitmentFromTx(tx);

    // Mirror the on-chain state in our local off-chain Merkle tree.
    // The tree is append-only: once a leaf is inserted it is never removed.
    merkleTree20.insertLeaves([cmt]);

    return {
        commitment: cmt,
        proof:      merkleTree20.generateProof(cmt),
        root:       merkleTree20.root,
        treeNumber: merkleTree20.lastTreeNumber,
        amount,
        key,
    };
}

// ─── test suite ──────────────────────────────────────────────────────────────
describe("Admin Scan Walkthrough (Educational)", () => {

    // =========================================================================
    //  STEP 0 — Deploy the protocol
    // =========================================================================
    it("STEP 0 — Deploy and initialise the full protocol", async () => {
        /*
         * testHelpers.deployForTest(3) spins up:
         *   - All smart contracts (EnygmaDvp, vaults, verifier, …)
         *   - 3 user wallets (Alice = wallets[1], Bob = wallets[2], Carol = wallets[3])
         *   - Empty local Merkle trees (one per vault type)
         *
         * `admin` is wallets[0] — the contract owner.  It doubles as the
         * relayer for ZK-proof submissions in this test.
         */
        let users, merkleTrees;
        [admin, users, contracts, merkleTrees] =
            await testHelpers.deployForTest(3);

        alice.wallet = users[0].wallet;
        bob.wallet   = users[1].wallet;
        carol.wallet = users[2].wallet;

        // The ERC-20 Merkle tree starts empty (only zero-value leaves).
        merkleTree20 = merkleTrees["ERC20"].tree;

        console.log("\n[STEP 0] Protocol deployed. Wallets:");
        console.log("  Admin :", admin.address);
        console.log("  Alice :", alice.wallet.address);
        console.log("  Bob   :", bob.wallet.address);
        console.log("  Carol :", carol.wallet.address);
    });

    // =========================================================================
    //  STEP A — Alice deposits
    // =========================================================================
    it("STEP A — Alice deposits 1 000 tokens → leaf #0", async () => {
        /*
         * A deposit creates ONE leaf in the Merkle tree.
         *
         * On-chain, the vault contract:
         *   1. Transfers `amount` ERC-20 tokens from Alice to itself (locks funds).
         *   2. Computes commitment = Poseidon(amount, publicKey, nonce).
         *   3. Inserts the commitment into the on-chain sparse Merkle tree.
         *   4. Emits: Commitment(commitment)
         *
         * Crucially, the calldata of the deposit() transaction is PUBLIC.
         * Anyone reading the blockchain can see:
         *   - tx.from   → Alice's address
         *   - params[0] → amount (1 000)
         *   - params[1] → Alice's public key
         *
         * This is what buildCoinRegistry later exploits to reconstruct
         * the deposit metadata WITHOUT needing Alice's private key.
         */
        aliceCoin = await depositErc20(alice.wallet, ALICE_AMOUNT);

        // Leaf #0 is the first leaf ever inserted.
        const leafIndex = merkleTree20.tree[0].indexOf(aliceCoin.commitment);
        expect(leafIndex).to.equal(0);

        console.log(`\n[STEP A] Alice deposited ${ALICE_AMOUNT} tokens.`);
        console.log(`  commitment : ${aliceCoin.commitment.toString().slice(0, 20)}…`);
        console.log(`  leaf index : ${leafIndex} (tree #${merkleTree20.treeNumber})`);
    });

    // =========================================================================
    //  STEP B — Bob deposits
    // =========================================================================
    it("STEP B — Bob deposits 600 tokens → leaf #1", async () => {
        /*
         * Same flow as Alice.  Bob gets his own fresh key pair.
         * The commitment encodes Bob's public key so only Bob can spend it
         * (his private key is needed to build the ZK-proof for JoinSplit).
         *
         * Notice: although Alice and Bob deposited different amounts, from
         * the Merkle tree's perspective both leaves look identical —
         * they are just 256-bit integers.  The amount information only
         * exists inside the commitment (hidden unless you know the preimage)
         * OR in the deposit calldata (which is public for deposit txs).
         */
        bobCoin = await depositErc20(bob.wallet, BOB_AMOUNT);

        const leafIndex = merkleTree20.tree[0].indexOf(bobCoin.commitment);
        expect(leafIndex).to.equal(1);

        console.log(`\n[STEP B] Bob deposited ${BOB_AMOUNT} tokens.`);
        console.log(`  commitment : ${bobCoin.commitment.toString().slice(0, 20)}…`);
        console.log(`  leaf index : ${leafIndex} (tree #${merkleTree20.treeNumber})`);
    });

    // =========================================================================
    //  STEP C — Carol deposits
    // =========================================================================
    it("STEP C — Carol deposits 200 tokens → leaf #2", async () => {
        /*
         * A third depositor.  Adding Carol lets us later verify that
         * ownerSummary correctly groups coins per depositor address —
         * Alice: 1 coin, Bob: 1 coin (pre-JoinSplit), Carol: 1 coin.
         */
        carolCoin = await depositErc20(carol.wallet, CAROL_AMOUNT);

        const leafIndex = merkleTree20.tree[0].indexOf(carolCoin.commitment);
        expect(leafIndex).to.equal(2);

        console.log(`\n[STEP C] Carol deposited ${CAROL_AMOUNT} tokens.`);
        console.log(`  commitment : ${carolCoin.commitment.toString().slice(0, 20)}…`);
        console.log(`  leaf index : ${leafIndex} (tree #${merkleTree20.treeNumber})`);
        console.log(`\n  Tree now has 3 leaves (one per depositor).`);
    });

    // =========================================================================
    //  STEP D — buildCoinRegistry (first snapshot, BEFORE any ZK operation)
    // =========================================================================
    it("STEP D — buildCoinRegistry: 3 deposit events, 0 opaque", async () => {
        /*
         * buildCoinRegistry() does the following:
         *
         *   1. Calls vaultContract.queryFilter(Commitment()) to fetch ALL
         *      Commitment events emitted since block 0.
         *
         *   2. For each event it fetches the full transaction that produced it.
         *
         *   3. It then tries to ABI-decode the calldata as deposit(uint256[]):
         *        - SUCCESS  → isDeposit=true, extracts (amount, publicKey, depositor)
         *        - FAILURE  → isDeposit=false (came from a ZK proof, not a deposit)
         *
         *   4. It builds a plain object (the "registry") keyed by the
         *      commitment's decimal string representation.
         *
         * At this point we have only deposit txs, so every event decodes
         * successfully: isDeposit=true for all three.
         */
        registryBeforeJoinSplit = await adminActions.buildCoinRegistry({
            contract:     contracts.erc20CoinVault,
            type:         adminActions.VAULT_TYPE_ERC20,
            assetAddress: contracts.erc20.address,
        });

        const entries    = Object.values(registryBeforeJoinSplit);
        const deposits   = entries.filter(e => e.isDeposit);
        const opaqueOnes = entries.filter(e => !e.isDeposit);

        expect(entries.length).to.equal(3);
        expect(deposits.length).to.equal(3);
        expect(opaqueOnes.length).to.equal(0);

        // Verify Alice's entry
        const aliceEntry = registryBeforeJoinSplit[aliceCoin.commitment.toString()];
        expect(aliceEntry).to.exist;
        expect(aliceEntry.isDeposit).to.be.true;
        expect(aliceEntry.depositor.toLowerCase()).to.equal(alice.wallet.address.toLowerCase());
        expect(BigInt(aliceEntry.amount)).to.equal(ALICE_AMOUNT);

        // Verify Bob's entry
        const bobEntry = registryBeforeJoinSplit[bobCoin.commitment.toString()];
        expect(bobEntry).to.exist;
        expect(BigInt(bobEntry.amount)).to.equal(BOB_AMOUNT);

        console.log("\n[STEP D] Registry BEFORE JoinSplit:");
        for (const [cmt, e] of Object.entries(registryBeforeJoinSplit)) {
            console.log(
                `  cmt:${cmt.slice(0, 16)}…` +
                ` | isDeposit:${e.isDeposit}` +
                ` | amount:${e.amount}` +
                ` | depositor:${e.depositor?.slice(0, 10)}…`
            );
        }
    });

    // =========================================================================
    //  STEP E — Bob JoinSplits his coin (ZK proof operation)
    // =========================================================================
    it("STEP E — Bob JoinSplits 600 → two outputs of 300 each (leaves #3, #4)", async () => {
        /*
         * JoinSplit is the core ZK operation.  Bob wants to split his 600-
         * token coin into two 300-token outputs — one to a new key of his
         * choosing and one as "change" to himself.
         *
         * HOW IT WORKS (circuit level, simplified):
         *   Inputs (private witness):
         *     - privateKeyIn  : Bob's private key (only he knows this)
         *     - valuesIn      : [600, 0]   (0 is a dummy second input)
         *     - valuesOut     : [300, 300]
         *     - publicKeysOut : [recipientKey.publicKey, changeKey.publicKey]
         *     - merkleProof   : proof that Bob's commitment is in the tree
         *
         *   Outputs (public statement):
         *     - nullifier     : hash that marks Bob's old coin as spent
         *     - commitment0   : Poseidon(300, recipientKey.publicKey, nonce0)
         *     - commitment1   : Poseidon(300, changeKey.publicKey,    nonce1)
         *
         * The RELAYER submits the proof on-chain (here the admin wallet
         * plays that role).  The vault contract:
         *   1. Verifies the ZK proof on-chain.
         *   2. Records the nullifier (Bob's old coin is now unspendable).
         *   3. Emits TWO new Commitment events — one per output coin.
         *
         * WHAT THE ADMIN SEES:
         *   - Two new Commitment events whose parent tx was submitted by the
         *     RELAYER, not by Bob or the recipient.
         *   - The calldata is proof bytes — it does NOT decode as deposit().
         *   - Therefore buildCoinRegistry will mark them as isDeposit=false
         *     and the true recipient stays hidden.
         */
        const splitAmount  = 300n;
        const changeAmount = BOB_AMOUNT - splitAmount; // 300

        // These keys are known only to whoever will receive the outputs.
        // The admin cannot derive them.
        const recipientKey = jsUtils.newKeyPair();
        const changeKey    = jsUtils.newKeyPair();
        const dummyKey     = jsUtils.newKeyPair(); // second input slot (dummy)

        const proof = await prover.prove("JoinSplitErc20", {
            message:              0n,
            valuesIn:             [bobCoin.amount, 0n],
            keysIn:               [bobCoin.key, dummyKey],
            valuesOut:            [splitAmount, changeAmount],
            keysOut:              [recipientKey, changeKey],
            merkleProofs:         [bobCoin.proof, { root: 0n }],
            treeNumbers:          [bobCoin.treeNumber, 0n],
            erc20ContractAddress: contracts.erc20.address,
        });

        // Relayer submits the proof; vault emits 2 new Commitment events
        const vault = contracts.erc20CoinVault.connect(admin);
        const tx    = await vault.transfer(proof);

        const [eventCmts] = await testHelpers.getCommitmentFromTxIndirect(
            tx,
            [contracts.erc20CoinVault.address],
        );

        // Mirror on the local off-chain tree
        merkleTree20.insertLeaves([eventCmts[0], eventCmts[1]]);

        // Tree should now have 5 leaves: 3 deposits + 2 JoinSplit outputs
        expect(merkleTree20.tree[0].length).to.equal(5);

        console.log("\n[STEP E] Bob's JoinSplit produced 2 new commitments:");
        console.log(`  leaf #3 : ${eventCmts[0].toString().slice(0, 20)}…  (split output)`);
        console.log(`  leaf #4 : ${eventCmts[1].toString().slice(0, 20)}…  (change output)`);
        console.log("  The tx sender visible on-chain is the RELAYER, not the true recipient.");
        console.log("  Bob's original leaf #1 commitment still exists in the tree.");
        console.log("  It is now a SPENT coin (nullifier recorded on-chain), but the");
        console.log("  tree is append-only: spent leaves are never removed.");
    });

    // =========================================================================
    //  STEP F — buildCoinRegistry (second snapshot, AFTER JoinSplit)
    // =========================================================================
    it("STEP F — buildCoinRegistry now sees 3 deposits + 2 opaque outputs", async () => {
        /*
         * We call buildCoinRegistry again to capture the two new events.
         * The registry now covers ALL five commitments.
         *
         * The key difference from STEP D:
         *   - The two JoinSplit events' parent tx contains proof calldata,
         *     NOT deposit calldata.  ABI-decoding as deposit() throws an
         *     error, which the try/catch silently catches → isDeposit=false.
         *   - We still record the txHash and blockNumber so the admin knows
         *     WHEN the commitment appeared, just not WHO the true owner is.
         */
        registryAfterJoinSplit = await adminActions.buildCoinRegistry({
            contract:     contracts.erc20CoinVault,
            type:         adminActions.VAULT_TYPE_ERC20,
            assetAddress: contracts.erc20.address,
        });

        const entries    = Object.values(registryAfterJoinSplit);
        const deposits   = entries.filter(e => e.isDeposit);
        const opaqueOnes = entries.filter(e => !e.isDeposit);

        expect(entries.length).to.equal(5);
        expect(deposits.length).to.equal(3);
        expect(opaqueOnes.length).to.equal(2);

        // Opaque entries still have a txHash (the relayer's tx) and blockNumber
        for (const opaque of opaqueOnes) {
            expect(opaque.txHash).to.not.be.null;
            expect(opaque.blockNumber).to.be.at.least(0);
            // But amount and depositor are null — unknown to the admin
            expect(opaque.amount).to.be.null;
            expect(opaque.depositor).to.not.equal(alice.wallet.address);
            expect(opaque.depositor).to.not.equal(bob.wallet.address);
            expect(opaque.depositor).to.not.equal(carol.wallet.address);
        }

        console.log("\n[STEP F] Registry AFTER JoinSplit (5 entries total):");
        for (const [cmt, e] of Object.entries(registryAfterJoinSplit)) {
            console.log(
                `  cmt:${cmt.slice(0, 16)}…` +
                ` | isDeposit:${String(e.isDeposit).padEnd(5)}` +
                ` | amount:${String(e.amount).padEnd(6)}` +
                ` | depositor:${e.depositor?.slice(0, 10)}…`
            );
        }
        console.log("\n  → The 2 opaque entries have amount=null and depositor=relayer.");
        console.log("    The admin cannot tell who received the JoinSplit outputs.");
    });

    // =========================================================================
    //  STEP G — scanTree: cross-reference tree leaves against the registry
    // =========================================================================
    it("STEP G — scanTree: walk the tree and classify all 5 leaves", async () => {
        /*
         * scanTree(merkleTree, coinRegistry) does three things:
         *
         *   1. ITERATES over every leaf of the off-chain Merkle tree
         *      (including all previous trees if the tree overflowed).
         *
         *      tree.prevTrees[]  → completed trees (index = treeNumber)
         *      tree.tree[0]      → current tree leaves
         *
         *   2. For each leaf commitment, looks it up in coinRegistry.
         *
         *      FOUND + isDeposit=true  → push to report.depositCoins
         *                                 accumulate in report.ownerSummary
         *      FOUND + isDeposit=false → push to report.opaqueCoins
         *      NOT FOUND               → push to report.unrecognizedCommitments
         *                                 (anomaly: event was missed somehow)
         *
         *   3. Returns the structured report object.
         *
         * WHY "unrecognizedCommitments" exists:
         *   In a healthy deployment, every on-chain Commitment event is
         *   captured by buildCoinRegistry.  If a commitment appears in the
         *   local tree but not in the registry, something went wrong
         *   (missed event, registry built from a different node, etc.).
         *   This bucket lets the admin detect the anomaly.
         */
        finalReport = adminActions.scanTree(merkleTree20, registryAfterJoinSplit);

        // ── high-level counts ─────────────────────────────────────────
        expect(finalReport.totalTrees).to.equal(1);
        expect(finalReport.totalLeaves).to.equal(5);
        expect(finalReport.depositCoins.length).to.equal(3);
        expect(finalReport.opaqueCoins.length).to.equal(2);
        expect(finalReport.unrecognizedCommitments.length).to.equal(0);

        console.log("\n[STEP G] scanTree report summary:");
        console.log(`  totalTrees             : ${finalReport.totalTrees}`);
        console.log(`  totalLeaves            : ${finalReport.totalLeaves}`);
        console.log(`  depositCoins           : ${finalReport.depositCoins.length}`);
        console.log(`  opaqueCoins            : ${finalReport.opaqueCoins.length}`);
        console.log(`  unrecognizedCommitments: ${finalReport.unrecognizedCommitments.length}`);
    });

    it("STEP G (cont.) — depositCoins have correct leafIndex, amount, and depositor", async () => {
        /*
         * Each entry in depositCoins is enriched with leafIndex and treeNumber
         * at scan time (these come from the tree walk, not from the registry).
         *
         * Because commitments were inserted in order (Alice, Bob, Carol), the
         * leafIndex matches the deposit order:
         *   Alice  → leaf 0
         *   Bob    → leaf 1
         *   Carol  → leaf 2
         */
        const byCommitment = {};
        for (const c of finalReport.depositCoins) {
            byCommitment[c.commitment] = c;
        }

        const aliceLeaf = byCommitment[aliceCoin.commitment.toString()];
        expect(aliceLeaf).to.exist;
        expect(aliceLeaf.leafIndex).to.equal(0);
        expect(aliceLeaf.treeNumber).to.equal(0);
        expect(BigInt(aliceLeaf.amount)).to.equal(ALICE_AMOUNT);
        expect(aliceLeaf.depositor.toLowerCase())
            .to.equal(alice.wallet.address.toLowerCase());

        const bobLeaf = byCommitment[bobCoin.commitment.toString()];
        expect(bobLeaf).to.exist;
        expect(bobLeaf.leafIndex).to.equal(1);
        expect(BigInt(bobLeaf.amount)).to.equal(BOB_AMOUNT);

        const carolLeaf = byCommitment[carolCoin.commitment.toString()];
        expect(carolLeaf).to.exist;
        expect(carolLeaf.leafIndex).to.equal(2);
        expect(BigInt(carolLeaf.amount)).to.equal(CAROL_AMOUNT);

        console.log("\n[STEP G cont.] depositCoins:");
        for (const c of finalReport.depositCoins) {
            console.log(
                `  leaf#${c.leafIndex} tree#${c.treeNumber}` +
                ` | amt:${c.amount}` +
                ` | ${c.depositor.slice(0, 10)}…`
            );
        }
    });

    it("STEP G (cont.) — opaqueCoins are at leaves #3 and #4 with null amount", async () => {
        /*
         * The two JoinSplit output coins land at leaf positions 3 and 4
         * (they were inserted right after the three deposits).
         *
         * Their amount is null in the registry → the admin CANNOT tell how
         * much value is locked in these outputs without the owner's private
         * key.  This is the core privacy guarantee of the ZK scheme.
         *
         * Note also that their depositor field is the RELAYER address
         * (admin wallet in this test), NOT the true recipient — confirming
         * that the on-chain tx sender leaks nothing about the true owner.
         */
        expect(finalReport.opaqueCoins.length).to.equal(2);

        const leafIndices = finalReport.opaqueCoins.map(c => c.leafIndex).sort();
        expect(leafIndices).to.deep.equal([3, 4]);

        for (const c of finalReport.opaqueCoins) {
            expect(c.amount).to.be.null;
            expect(c.treeNumber).to.equal(0);
        }

        console.log("\n[STEP G cont.] opaqueCoins:");
        for (const c of finalReport.opaqueCoins) {
            console.log(
                `  leaf#${c.leafIndex} tree#${c.treeNumber}` +
                ` | amount: HIDDEN` +
                ` | cmt:${c.commitment.slice(0, 16)}…`
            );
        }
        console.log("  → Admin sees THAT these outputs exist, but not WHO owns them");
        console.log("    or HOW MUCH value they hold.");
    });

    it("STEP G (cont.) — ownerSummary groups deposit coins per depositor", async () => {
        /*
         * ownerSummary is built only from depositCoins (where the depositor
         * address is known).  opaqueCoins are excluded because their "owner"
         * (the relayer) is meaningless.
         *
         * Expected structure:
         *   {
         *     "0xAlice…": { owner: "0xAlice…", coinCount: 1, coins: [aliceCoin] },
         *     "0xBob…"  : { owner: "0xBob…",   coinCount: 1, coins: [bobCoin]   },
         *     "0xCarol…": { owner: "0xCarol…", coinCount: 1, coins: [carolCoin] },
         *   }
         *
         * Important: Bob's leaf #1 is SPENT (nullifier on-chain), but scanTree
         * does not track nullifiers.  From the tree-scan perspective Bob still
         * "has" 1 deposit coin in the tree.  To determine liveness you would
         * additionally need to query Nullifier events and cross-reference.
         */
        // Normalise keys to lowercase for consistent lookup
        const summary = {};
        for (const [k, v] of Object.entries(finalReport.ownerSummary)) {
            summary[k.toLowerCase()] = v;
        }

        expect(summary[alice.wallet.address.toLowerCase()]).to.exist;
        expect(summary[alice.wallet.address.toLowerCase()].coinCount).to.equal(1);

        expect(summary[bob.wallet.address.toLowerCase()]).to.exist;
        expect(summary[bob.wallet.address.toLowerCase()].coinCount).to.equal(1);

        expect(summary[carol.wallet.address.toLowerCase()]).to.exist;
        expect(summary[carol.wallet.address.toLowerCase()].coinCount).to.equal(1);

        const totalKnownCoins = Object.values(summary)
            .reduce((acc, s) => acc + s.coinCount, 0);
        expect(totalKnownCoins).to.equal(3); // only deposit coins counted

        console.log("\n[STEP G cont.] ownerSummary (deposit coins only):");
        for (const [addr, s] of Object.entries(finalReport.ownerSummary)) {
            console.log(`  ${addr.slice(0, 10)}… : ${s.coinCount} coin(s)`);
        }
        console.log("  Note: Bob's coin shows coinCount=1 even though it is spent.");
        console.log("  scanTree does not know about nullifiers — it is a tree view only.");
    });

    // =========================================================================
    //  STEP H — printScanReport: human-readable output
    // =========================================================================
    it("STEP H — printScanReport renders the full report to the console", async () => {
        /*
         * printScanReport() is a pure display function — it does not throw,
         * does not perform any async work, and does not modify the report.
         *
         * It prints three sections:
         *   1. Header: counts of trees, leaves, deposits, opaque, unrecognized.
         *   2. FUND DISTRIBUTION: one block per depositor with coin details.
         *   3. OPAQUE COINS: one line per ZK-output (no amount, no owner).
         *
         * The "unrecognized" section only appears if that list is non-empty.
         */
        console.log("\n[STEP H] printScanReport output:");
        expect(() => adminActions.printScanReport(finalReport)).to.not.throw();
        adminActions.printScanReport(finalReport);
    });

    // =========================================================================
    //  STEP I — Edge case: scanTree with an EMPTY registry
    // =========================================================================
    it("STEP I — Edge case: scanTree with empty registry → all leaves unrecognized", async () => {
        /*
         * If buildCoinRegistry is called but returns nothing (e.g., you
         * accidentally queried the wrong block range, or the local indexer
         * is behind), every leaf in the tree will land in
         * report.unrecognizedCommitments.
         *
         * This is the "anomaly" bucket.  It tells the admin: "I see N
         * commitments in the tree but have NO event data to explain them."
         *
         * In a real deployment this would be a red flag: re-run
         * buildCoinRegistry with the correct fromBlock, or investigate
         * whether a commitment was inserted by bypassing the vault.
         */
        const emptyRegistry = {};
        const emptyReport   = adminActions.scanTree(merkleTree20, emptyRegistry);

        expect(emptyReport.totalLeaves).to.equal(5);
        expect(emptyReport.depositCoins.length).to.equal(0);
        expect(emptyReport.opaqueCoins.length).to.equal(0);
        expect(emptyReport.unrecognizedCommitments.length).to.equal(5);
        expect(Object.keys(emptyReport.ownerSummary).length).to.equal(0);

        console.log("\n[STEP I] scanTree with empty registry:");
        console.log(`  All ${emptyReport.unrecognizedCommitments.length} leaves are UNRECOGNIZED.`);
        console.log("  In production this means the admin's event index is stale or missing.");
        adminActions.printScanReport(emptyReport);
    });

    // =========================================================================
    //  STEP J — Edge case: scanTree on a BRAND NEW empty tree
    // =========================================================================
    it("STEP J — Edge case: fresh empty tree → zero leaves, zero coins", async () => {
        /*
         * When the protocol first deploys, or if a new Merkle tree has just
         * been created (tree overflow), the tree has no leaves yet.
         *
         * scanTree must handle this gracefully: all counts are zero and
         * all arrays are empty — no iteration happens at all.
         */
        const freshTree   = new MerkleTree(TREE_DEPTH);
        const freshReport = adminActions.scanTree(freshTree, {});

        expect(freshReport.totalLeaves).to.equal(0);
        expect(freshReport.depositCoins.length).to.equal(0);
        expect(freshReport.opaqueCoins.length).to.equal(0);
        expect(freshReport.unrecognizedCommitments.length).to.equal(0);
        expect(Object.keys(freshReport.ownerSummary).length).to.equal(0);

        console.log("\n[STEP J] Fresh empty tree report:");
        console.log("  totalLeaves=0, no coins of any kind. All arrays empty.");
    });
});

/* global describe it ethers before */
const { expect }    = require("chai");
const prover        = require("../src/core/prover");
const jsUtils       = require("../src/core/utils");
const testHelpers   = require("./testHelpers.js");
const adminActions  = require("../src/core/endpoints/admin.js");

const dvpConf    = require("../enygmadvp.config.json");
const TREE_DEPTH = dvpConf["circom"]["meta-parameters"]["tree-depth"];

// ─── shared state ────────────────────────────────────────────────────────────
let admin;
let alice  = {};
let bob    = {};
let contracts;
let merkleTree20;

// Deposits tracked for later assertions
const aliceDepositAmounts = [500n, 300n];
const bobDepositAmount    = 800n;

// Keys created at deposit time
let aliceKeys  = [];
let bobKey;
let aliceCoins = [];
let bobCoins   = [];

// ─── test suite ──────────────────────────────────────────────────────────────
describe("Admin Tree Scan", () => {

    // ── 1. deploy ─────────────────────────────────────────────────────────────
    it("should deploy and initialise the protocol", async () => {
        let users;
        let merkleTrees;

        [admin, users, contracts, merkleTrees] =
            await testHelpers.deployForTest(2 /* userCount */);

        alice.wallet = users[0].wallet;
        bob.wallet   = users[1].wallet;

        merkleTree20 = merkleTrees["ERC20"].tree;

        console.log("Admin scan test: deployment done.");
    });

    // ── 2. Alice deposits two ERC20 coins ─────────────────────────────────────
    it("Alice deposits two ERC20 coins with known amounts", async () => {
        const erc20Owner = contracts.erc20.connect(admin);
        const erc20Alice = contracts.erc20.connect(alice.wallet);
        const vault      = contracts.erc20CoinVault.connect(alice.wallet);

        const total = aliceDepositAmounts[0] + aliceDepositAmounts[1];

        await erc20Owner.mint(alice.wallet.address, total);
        await erc20Alice.approve(contracts.erc20CoinVault.address, total);

        for (const amount of aliceDepositAmounts) {
            const key = jsUtils.newKeyPair();
            aliceKeys.push(key);

            const tx  = await vault.deposit([amount, key.publicKey]);
            const cmt = await testHelpers.getCommitmentFromTx(tx);

            merkleTree20.insertLeaves([cmt]);
            aliceCoins.push({
                commitment: cmt,
                proof:      merkleTree20.generateProof(cmt),
                root:       merkleTree20.root,
                treeNumber: merkleTree20.lastTreeNumber,
                amount,
                key,
            });
        }

        console.log(`Alice deposited coins: ${aliceCoins.map(c => c.amount)}`);
    });

    // ── 3. Bob deposits one ERC20 coin ────────────────────────────────────────
    it("Bob deposits one ERC20 coin with a known amount", async () => {
        const erc20Owner = contracts.erc20.connect(admin);
        const erc20Bob   = contracts.erc20.connect(bob.wallet);
        const vault      = contracts.erc20CoinVault.connect(bob.wallet);

        await erc20Owner.mint(bob.wallet.address, bobDepositAmount);
        await erc20Bob.approve(contracts.erc20CoinVault.address, bobDepositAmount);

        bobKey = jsUtils.newKeyPair();

        const tx  = await vault.deposit([bobDepositAmount, bobKey.publicKey]);
        const cmt = await testHelpers.getCommitmentFromTx(tx);

        merkleTree20.insertLeaves([cmt]);
        bobCoins.push({
            commitment: cmt,
            proof:      merkleTree20.generateProof(cmt),
            root:       merkleTree20.root,
            treeNumber: merkleTree20.lastTreeNumber,
            amount:     bobDepositAmount,
            key:        bobKey,
        });

        console.log(`Bob deposited coin: ${bobDepositAmount}`);
    });

    // ── 4. Bob splits his coin via JoinSplit (produces ZK-proof outputs) ──────
    it("Bob JoinSplits his coin — outputs are opaque to the admin", async () => {
        const splitAmount = 400n;
        const changeAmount = bobDepositAmount - splitAmount;

        const recipientKey = jsUtils.newKeyPair(); // "Alice's" receive key
        const changeKey    = jsUtils.newKeyPair(); // Bob's change

        const dummyKey = jsUtils.newKeyPair();

        const proof = await prover.prove("JoinSplitErc20", {
            message:           0n,
            valuesIn:          [bobCoins[0].amount, 0n],
            keysIn:            [bobCoins[0].key, dummyKey],
            valuesOut:         [splitAmount, changeAmount],
            keysOut:           [recipientKey, changeKey],
            merkleProofs:      [bobCoins[0].proof, { root: 0n }],
            treeNumbers:       [bobCoins[0].treeNumber, 0n],
            erc20ContractAddress: contracts.erc20.address,
        });

        // Submitted by the relayer (admin wallet doubles as relayer here)
        const vault = contracts.erc20CoinVault.connect(admin);
        const tx    = await vault.transfer(proof);

        const [eventCmts] = await testHelpers.getCommitmentFromTxIndirect(
            tx,
            [contracts.erc20CoinVault.address],
        );

        // Update off-chain tree with the two new (unrecognised) commitments
        merkleTree20.insertLeaves([eventCmts[0], eventCmts[1]]);

        console.log("JoinSplit produced 2 new opaque commitments.");
    });

    // ── 5. buildCoinRegistry ──────────────────────────────────────────────────
    let coinRegistry;

    it("buildCoinRegistry indexes all Commitment events on the vault", async () => {
        coinRegistry = await adminActions.buildCoinRegistry({
            contract:     contracts.erc20CoinVault,
            type:         adminActions.VAULT_TYPE_ERC20,
            assetAddress: contracts.erc20.address,
        });

        // 2 Alice deposits + 1 Bob deposit + 2 JoinSplit outputs = 5 commitments
        const totalCommitments = Object.keys(coinRegistry).length;
        expect(totalCommitments).to.equal(5);

        // Every entry must have a commitment field
        for (const entry of Object.values(coinRegistry)) {
            expect(entry).to.have.property("commitment");
            expect(entry).to.have.property("isDeposit");
            expect(entry).to.have.property("vaultType", adminActions.VAULT_TYPE_ERC20);
        }

        // Direct deposits must be decoded
        const depositEntries = Object.values(coinRegistry).filter(e => e.isDeposit);
        expect(depositEntries.length).to.equal(3);

        // ZK-proof outputs must NOT be decoded as deposits
        const unknownEntries = Object.values(coinRegistry).filter(e => !e.isDeposit);
        expect(unknownEntries.length).to.equal(2);

        console.log("buildCoinRegistry: registry has correct shape.");
    });

    it("buildCoinRegistry records the correct depositor address for each deposit", async () => {
        // Alice's deposits
        for (const coin of aliceCoins) {
            const entry = coinRegistry[coin.commitment.toString()];
            expect(entry).to.exist;
            expect(entry.isDeposit).to.be.true;
            expect(entry.depositor.toLowerCase())
                .to.equal(alice.wallet.address.toLowerCase());
        }

        // Bob's deposit
        const bobEntry = coinRegistry[bobCoins[0].commitment.toString()];
        expect(bobEntry).to.exist;
        expect(bobEntry.isDeposit).to.be.true;
        expect(bobEntry.depositor.toLowerCase())
            .to.equal(bob.wallet.address.toLowerCase());

        console.log("buildCoinRegistry: depositor addresses are correct.");
    });

    it("buildCoinRegistry records the correct deposit amounts", async () => {
        for (let i = 0; i < aliceCoins.length; i++) {
            const entry = coinRegistry[aliceCoins[i].commitment.toString()];
            expect(BigInt(entry.amount)).to.equal(aliceDepositAmounts[i]);
        }

        const bobEntry = coinRegistry[bobCoins[0].commitment.toString()];
        expect(BigInt(bobEntry.amount)).to.equal(bobDepositAmount);

        console.log("buildCoinRegistry: deposit amounts are correct.");
    });

    // ── 6. scanTree ───────────────────────────────────────────────────────────
    let report;

    it("scanTree walks all leaves and produces a structured report", async () => {
        report = adminActions.scanTree(merkleTree20, coinRegistry);

        // Should scan exactly 1 tree (no overflow yet)
        expect(report.totalTrees).to.equal(1);

        // 5 leaves total: 3 deposits + 2 JoinSplit outputs
        expect(report.totalLeaves).to.equal(5);

        // All 5 commitments appear in the registry (built from on-chain events),
        // so nothing should be truly unrecognized
        expect(report.unrecognizedCommitments.length).to.equal(0);
    });

    it("scanTree.depositCoins contains exactly the 3 deposit coins (owner known)", async () => {
        expect(report.depositCoins.length).to.equal(3);

        const depositCommitments = new Set(
            report.depositCoins.map(c => c.commitment),
        );

        for (const coin of aliceCoins) {
            expect(depositCommitments.has(coin.commitment.toString())).to.be.true;
        }
        expect(depositCommitments.has(bobCoins[0].commitment.toString())).to.be.true;

        console.log("scanTree: all 3 deposit coins are in depositCoins.");
    });

    it("scanTree.opaqueCoins contains exactly the 2 JoinSplit outputs (owner hidden)", async () => {
        expect(report.opaqueCoins.length).to.equal(2);

        // Every opaque coin must have isDeposit=false
        for (const coin of report.opaqueCoins) {
            expect(coin.isDeposit).to.be.false;
        }

        // None of Alice's or Bob's deposit commitments should appear here
        const depositCmts = new Set([
            ...aliceCoins.map(c => c.commitment.toString()),
            bobCoins[0].commitment.toString(),
        ]);
        for (const coin of report.opaqueCoins) {
            expect(depositCmts.has(coin.commitment)).to.be.false;
        }

        console.log("scanTree: JoinSplit outputs correctly land in opaqueCoins.");
    });

    it("scanTree.ownerSummary groups deposit coins by depositor address", async () => {
        const aliceAddr = alice.wallet.address.toLowerCase();
        const bobAddr   = bob.wallet.address.toLowerCase();

        // Normalise keys to lowercase for lookup
        const summary = {};
        for (const [k, v] of Object.entries(report.ownerSummary)) {
            summary[k.toLowerCase()] = v;
        }

        expect(summary[aliceAddr]).to.exist;
        expect(summary[aliceAddr].coinCount).to.equal(2);

        expect(summary[bobAddr]).to.exist;
        expect(summary[bobAddr].coinCount).to.equal(1);

        // ownerSummary covers only depositCoins (3 total)
        const totalCount = Object.values(summary)
            .reduce((acc, s) => acc + s.coinCount, 0);
        expect(totalCount).to.equal(3);

        console.log("scanTree: ownerSummary correctly groups deposit coins by depositor.");
    });

    it("scanTree each deposit coin carries the correct amount and depositor", async () => {
        for (const coin of report.depositCoins) {
            const expected = coinRegistry[coin.commitment];
            expect(coin.amount).to.equal(expected.amount);
            expect(coin.depositor).to.equal(expected.depositor);
        }

        console.log("scanTree: deposit coin metadata is intact.");
    });

    it("scanTree each leaf has correct treeNumber and leafIndex set", async () => {
        const allCoins = [...report.depositCoins, ...report.opaqueCoins];
        for (const coin of allCoins) {
            // Only one tree exists so treeNumber must be 0
            expect(coin.treeNumber).to.equal(0);
            expect(coin.leafIndex).to.be.at.least(0);
        }

        console.log("scanTree: treeNumber and leafIndex are correct.");
    });

    // ── 7. printScanReport ────────────────────────────────────────────────────
    it("printScanReport prints the report without throwing", async () => {
        expect(() => adminActions.printScanReport(report)).to.not.throw();

        console.log("\n--- printScanReport output below ---");
        adminActions.printScanReport(report);
    });

    // ── 8. empty-tree edge case ───────────────────────────────────────────────
    it("scanTree on an empty tree returns a zero-count report", async () => {
        const MerkleTree  = require("../src/core/merkle");
        const emptyTree   = new MerkleTree(TREE_DEPTH);
        const emptyReport = adminActions.scanTree(emptyTree, {});

        expect(emptyReport.totalLeaves).to.equal(0);
        expect(emptyReport.depositCoins.length).to.equal(0);
        expect(emptyReport.opaqueCoins.length).to.equal(0);
        expect(emptyReport.unrecognizedCommitments.length).to.equal(0);
        expect(Object.keys(emptyReport.ownerSummary).length).to.equal(0);

        console.log("scanTree: empty tree handled correctly.");
    });

    // ── 9. empty registry edge case ───────────────────────────────────────────
    it("scanTree with an empty registry marks every leaf as unrecognised", async () => {
        const emptyReport = adminActions.scanTree(merkleTree20, {});

        expect(emptyReport.totalLeaves).to.equal(5);
        expect(emptyReport.depositCoins.length).to.equal(0);
        expect(emptyReport.opaqueCoins.length).to.equal(0);
        expect(emptyReport.unrecognizedCommitments.length).to.equal(5);
        expect(Object.keys(emptyReport.ownerSummary).length).to.equal(0);

        console.log("scanTree: empty registry results in all leaves unrecognised.");
    });
});

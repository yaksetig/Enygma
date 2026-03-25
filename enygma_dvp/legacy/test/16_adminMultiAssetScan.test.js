/* global describe it ethers */
const { expect }   = require("chai");
const jsUtils      = require("../src/core/utils");
const testHelpers  = require("./testHelpers.js");
const adminActions = require("../src/core/endpoints/admin.js");

const dvpConf    = require("../enygmadvp.config.json");
const TREE_DEPTH = dvpConf["circom"]["meta-parameters"]["tree-depth"];

// ─── NFT token ids ───────────────────────────────────────────────────────────
const BOB_NFT_ID    = 1001n;
const CLAIRE_NFT_ID = 2002n;

// ─── ERC20 deposit amounts ───────────────────────────────────────────────────
const ALICE_ERC20_AMOUNT = 800n;
const BOB_ERC20_AMOUNT   = 350n;

// ─── shared state ────────────────────────────────────────────────────────────
let admin;
let alice  = {};
let bob    = {};
let claire = {};
let contracts;
let merkleTree20;
let merkleTree721;

// Coins recorded after each deposit step
let aliceCoin20   = null;
let bobCoin721    = null;
let bobCoin20     = null;
let claireCoin721 = null;

// Key pairs created at deposit time
let aliceKey20;
let bobKey721;
let bobKey20;
let claireKey721;

// ─── test suite ──────────────────────────────────────────────────────────────
describe("Admin Scan — Multi-asset Deposits (Alice ERC20 | Bob NFT + ERC20 | Claire NFT)", () => {

    // ── 1. deploy ─────────────────────────────────────────────────────────────
    it("should deploy and initialise the protocol", async () => {
        let users;
        let merkleTrees;

        // Three users: Alice (index 0), Bob (index 1), Claire (index 2)
        [admin, users, contracts, merkleTrees] =
            await testHelpers.deployForTest(3);

        alice.wallet  = users[0].wallet;
        bob.wallet    = users[1].wallet;
        claire.wallet = users[2].wallet;

        merkleTree20  = merkleTrees["ERC20"].tree;
        merkleTree721 = merkleTrees["ERC721"].tree;

        console.log("Deployment done. Users: Alice, Bob, Claire.");
        console.log(`  Alice  : ${alice.wallet.address}`);
        console.log(`  Bob    : ${bob.wallet.address}`);
        console.log(`  Claire : ${claire.wallet.address}`);
    });

    // ── 2. Alice deposits 800 ERC20 ───────────────────────────────────────────
    it("Alice deposits 800 ERC20", async () => {
        const erc20Owner = contracts.erc20.connect(admin);
        const erc20Alice = contracts.erc20.connect(alice.wallet);
        const vault      = contracts.erc20CoinVault.connect(alice.wallet);

        await erc20Owner.mint(alice.wallet.address, ALICE_ERC20_AMOUNT);
        await erc20Alice.approve(contracts.erc20CoinVault.address, ALICE_ERC20_AMOUNT);

        aliceKey20    = jsUtils.newKeyPair();
        const tx      = await vault.deposit([ALICE_ERC20_AMOUNT, aliceKey20.publicKey]);
        const cmt     = await testHelpers.getCommitmentFromTx(tx);

        merkleTree20.insertLeaves([cmt]);
        aliceCoin20 = {
            commitment: cmt,
            proof:      merkleTree20.generateProof(cmt),
            root:       merkleTree20.root,
            treeNumber: merkleTree20.lastTreeNumber,
            amount:     ALICE_ERC20_AMOUNT,
            key:        aliceKey20,
        };

        console.log(`Alice deposited ${ALICE_ERC20_AMOUNT} ERC20.`);
        console.log(`  commitment : ${cmt}`);
    });

    // ── 3. Bob deposits 1 ERC721 (NFT id 1001) ───────────────────────────────
    it("Bob deposits 1 ERC721 (NFT id 1001)", async () => {
        const erc721Owner = contracts.erc721.connect(admin);
        const erc721Bob   = contracts.erc721.connect(bob.wallet);
        const vault       = contracts.erc721CoinVault.connect(bob.wallet);

        await erc721Owner.mint(bob.wallet.address, BOB_NFT_ID);
        await erc721Bob.approve(contracts.erc721CoinVault.address, BOB_NFT_ID);

        bobKey721     = jsUtils.newKeyPair();
        const tx      = await vault.deposit([BOB_NFT_ID, bobKey721.publicKey]);
        const cmt     = await testHelpers.getCommitmentFromTx(tx);

        merkleTree721.insertLeaves([cmt]);
        bobCoin721 = {
            commitment: cmt,
            proof:      merkleTree721.generateProof(cmt),
            root:       merkleTree721.root,
            treeNumber: merkleTree721.lastTreeNumber,
            tokenId:    BOB_NFT_ID,
            key:        bobKey721,
        };

        console.log(`Bob deposited NFT id=${BOB_NFT_ID}.`);
        console.log(`  commitment : ${cmt}`);
    });

    // ── 4. Bob deposits 350 ERC20 ─────────────────────────────────────────────
    it("Bob deposits 350 ERC20", async () => {
        const erc20Owner = contracts.erc20.connect(admin);
        const erc20Bob   = contracts.erc20.connect(bob.wallet);
        const vault      = contracts.erc20CoinVault.connect(bob.wallet);

        await erc20Owner.mint(bob.wallet.address, BOB_ERC20_AMOUNT);
        await erc20Bob.approve(contracts.erc20CoinVault.address, BOB_ERC20_AMOUNT);

        bobKey20      = jsUtils.newKeyPair();
        const tx      = await vault.deposit([BOB_ERC20_AMOUNT, bobKey20.publicKey]);
        const cmt     = await testHelpers.getCommitmentFromTx(tx);

        merkleTree20.insertLeaves([cmt]);
        bobCoin20 = {
            commitment: cmt,
            proof:      merkleTree20.generateProof(cmt),
            root:       merkleTree20.root,
            treeNumber: merkleTree20.lastTreeNumber,
            amount:     BOB_ERC20_AMOUNT,
            key:        bobKey20,
        };

        console.log(`Bob deposited ${BOB_ERC20_AMOUNT} ERC20.`);
        console.log(`  commitment : ${cmt}`);
    });

    // ── 5. Claire deposits 1 ERC721 (NFT id 2002) ────────────────────────────
    it("Claire deposits 1 ERC721 (NFT id 2002)", async () => {
        const erc721Owner  = contracts.erc721.connect(admin);
        const erc721Claire = contracts.erc721.connect(claire.wallet);
        const vault        = contracts.erc721CoinVault.connect(claire.wallet);

        await erc721Owner.mint(claire.wallet.address, CLAIRE_NFT_ID);
        await erc721Claire.approve(contracts.erc721CoinVault.address, CLAIRE_NFT_ID);

        claireKey721  = jsUtils.newKeyPair();
        const tx      = await vault.deposit([CLAIRE_NFT_ID, claireKey721.publicKey]);
        const cmt     = await testHelpers.getCommitmentFromTx(tx);

        merkleTree721.insertLeaves([cmt]);
        claireCoin721 = {
            commitment: cmt,
            proof:      merkleTree721.generateProof(cmt),
            root:       merkleTree721.root,
            treeNumber: merkleTree721.lastTreeNumber,
            tokenId:    CLAIRE_NFT_ID,
            key:        claireKey721,
        };

        console.log(`Claire deposited NFT id=${CLAIRE_NFT_ID}.`);
        console.log(`  commitment : ${cmt}`);
    });

    // ─────────────────────────────────────────────────────────────────────────
    // buildCoinRegistry — ERC20 vault
    // Expected: 2 deposit entries (Alice 800 + Bob 350), 0 opaque
    // ─────────────────────────────────────────────────────────────────────────
    let coinRegistry20;

    it("buildCoinRegistry ERC20: finds exactly 2 deposit entries", async () => {
        coinRegistry20 = await adminActions.buildCoinRegistry({
            contract:     contracts.erc20CoinVault,
            type:         adminActions.VAULT_TYPE_ERC20,
            assetAddress: contracts.erc20.address,
        });

        expect(Object.keys(coinRegistry20).length).to.equal(2);

        for (const entry of Object.values(coinRegistry20)) {
            expect(entry.isDeposit).to.be.true;
            expect(entry.vaultType).to.equal(adminActions.VAULT_TYPE_ERC20);
        }

        console.log("ERC20 registry: 2 entries, all isDeposit=true.");
    });

    it("buildCoinRegistry ERC20: Alice's entry has correct depositor and amount", async () => {
        const entry = coinRegistry20[aliceCoin20.commitment.toString()];
        expect(entry).to.exist;
        expect(entry.depositor.toLowerCase()).to.equal(alice.wallet.address.toLowerCase());
        expect(BigInt(entry.amount)).to.equal(ALICE_ERC20_AMOUNT);

        console.log(`Alice ERC20 — depositor: ${entry.depositor}  amount: ${entry.amount}`);
    });

    it("buildCoinRegistry ERC20: Bob's entry has correct depositor and amount", async () => {
        const entry = coinRegistry20[bobCoin20.commitment.toString()];
        expect(entry).to.exist;
        expect(entry.depositor.toLowerCase()).to.equal(bob.wallet.address.toLowerCase());
        expect(BigInt(entry.amount)).to.equal(BOB_ERC20_AMOUNT);

        console.log(`Bob ERC20 — depositor: ${entry.depositor}  amount: ${entry.amount}`);
    });

    // ─────────────────────────────────────────────────────────────────────────
    // buildCoinRegistry — ERC721 vault
    // Expected: 2 deposit entries (Bob NFT 1001 + Claire NFT 2002), 0 opaque
    // ─────────────────────────────────────────────────────────────────────────
    let coinRegistry721;

    it("buildCoinRegistry ERC721: finds exactly 2 deposit entries", async () => {
        coinRegistry721 = await adminActions.buildCoinRegistry({
            contract:     contracts.erc721CoinVault,
            type:         adminActions.VAULT_TYPE_ERC721,
            assetAddress: contracts.erc721.address,
        });

        expect(Object.keys(coinRegistry721).length).to.equal(2);

        for (const entry of Object.values(coinRegistry721)) {
            expect(entry.isDeposit).to.be.true;
            expect(entry.vaultType).to.equal(adminActions.VAULT_TYPE_ERC721);
            // ERC721 deposits always carry amount = '1'
            expect(entry.amount).to.equal('1');
        }

        console.log("ERC721 registry: 2 entries, all isDeposit=true, amount='1'.");
    });

    it("buildCoinRegistry ERC721: Bob's entry has correct depositor and tokenId", async () => {
        const entry = coinRegistry721[bobCoin721.commitment.toString()];
        expect(entry).to.exist;
        expect(entry.depositor.toLowerCase()).to.equal(bob.wallet.address.toLowerCase());
        expect(BigInt(entry.tokenId)).to.equal(BOB_NFT_ID);

        console.log(`Bob NFT — depositor: ${entry.depositor}  tokenId: ${entry.tokenId}`);
    });

    it("buildCoinRegistry ERC721: Claire's entry has correct depositor and tokenId", async () => {
        const entry = coinRegistry721[claireCoin721.commitment.toString()];
        expect(entry).to.exist;
        expect(entry.depositor.toLowerCase()).to.equal(claire.wallet.address.toLowerCase());
        expect(BigInt(entry.tokenId)).to.equal(CLAIRE_NFT_ID);

        console.log(`Claire NFT — depositor: ${entry.depositor}  tokenId: ${entry.tokenId}`);
    });

    // ─────────────────────────────────────────────────────────────────────────
    // scanTree — ERC20 tree
    // Expected: 1 tree, 2 leaves, 2 depositCoins, 0 opaque, 0 unrecognized
    // ─────────────────────────────────────────────────────────────────────────
    let report20;

    it("scanTree ERC20: correct leaf count and bucket sizes", async () => {
        report20 = adminActions.scanTree(merkleTree20, coinRegistry20);

        expect(report20.totalTrees).to.equal(1);
        expect(report20.totalLeaves).to.equal(2);
        expect(report20.depositCoins.length).to.equal(2);
        expect(report20.opaqueCoins.length).to.equal(0);
        expect(report20.unrecognizedCommitments.length).to.equal(0);

        console.log("ERC20 scanTree: 1 tree, 2 leaves, 2 depositCoins.");
    });

    it("scanTree ERC20: ownerSummary has Alice (1 coin) and Bob (1 coin)", async () => {
        const summary = {};
        for (const [k, v] of Object.entries(report20.ownerSummary)) {
            summary[k.toLowerCase()] = v;
        }

        expect(summary[alice.wallet.address.toLowerCase()]).to.exist;
        expect(summary[alice.wallet.address.toLowerCase()].coinCount).to.equal(1);

        expect(summary[bob.wallet.address.toLowerCase()]).to.exist;
        expect(summary[bob.wallet.address.toLowerCase()].coinCount).to.equal(1);

        const total = Object.values(summary).reduce((acc, s) => acc + s.coinCount, 0);
        expect(total).to.equal(2);

        console.log("ERC20 ownerSummary: Alice 1 coin, Bob 1 coin.");
    });

    it("scanTree ERC20: each depositCoin carries the correct amount and depositor", async () => {
        for (const coin of report20.depositCoins) {
            const expected = coinRegistry20[coin.commitment];
            expect(coin.amount).to.equal(expected.amount);
            expect(coin.depositor).to.equal(expected.depositor);
            expect(coin.treeNumber).to.equal(0);
            expect(coin.leafIndex).to.be.at.least(0);
        }

        console.log("ERC20 depositCoins: metadata intact.");
    });

    // ─────────────────────────────────────────────────────────────────────────
    // scanTree — ERC721 tree
    // Expected: 1 tree, 2 leaves, 2 depositCoins, 0 opaque, 0 unrecognized
    // ─────────────────────────────────────────────────────────────────────────
    let report721;

    it("scanTree ERC721: correct leaf count and bucket sizes", async () => {
        report721 = adminActions.scanTree(merkleTree721, coinRegistry721);

        expect(report721.totalTrees).to.equal(1);
        expect(report721.totalLeaves).to.equal(2);
        expect(report721.depositCoins.length).to.equal(2);
        expect(report721.opaqueCoins.length).to.equal(0);
        expect(report721.unrecognizedCommitments.length).to.equal(0);

        console.log("ERC721 scanTree: 1 tree, 2 leaves, 2 depositCoins.");
    });

    it("scanTree ERC721: ownerSummary has Bob (1 NFT) and Claire (1 NFT)", async () => {
        const summary = {};
        for (const [k, v] of Object.entries(report721.ownerSummary)) {
            summary[k.toLowerCase()] = v;
        }

        expect(summary[bob.wallet.address.toLowerCase()]).to.exist;
        expect(summary[bob.wallet.address.toLowerCase()].coinCount).to.equal(1);

        expect(summary[claire.wallet.address.toLowerCase()]).to.exist;
        expect(summary[claire.wallet.address.toLowerCase()].coinCount).to.equal(1);

        const total = Object.values(summary).reduce((acc, s) => acc + s.coinCount, 0);
        expect(total).to.equal(2);

        console.log("ERC721 ownerSummary: Bob 1 NFT, Claire 1 NFT.");
    });

    it("scanTree ERC721: each depositCoin carries correct tokenId and depositor", async () => {
        for (const coin of report721.depositCoins) {
            const expected = coinRegistry721[coin.commitment];
            expect(coin.tokenId).to.equal(expected.tokenId);
            expect(coin.depositor).to.equal(expected.depositor);
            expect(coin.amount).to.equal('1');
            expect(coin.treeNumber).to.equal(0);
            expect(coin.leafIndex).to.be.at.least(0);
        }

        console.log("ERC721 depositCoins: metadata intact.");
    });

    // ─────────────────────────────────────────────────────────────────────────
    // printScanReport — human-readable output for both trees
    // ─────────────────────────────────────────────────────────────────────────
    it("printScanReport prints the ERC20 tree report without throwing", async () => {
        expect(() => adminActions.printScanReport(report20)).to.not.throw();
        console.log("\n--- ERC20 tree report ---");
        adminActions.printScanReport(report20);
    });

    it("printScanReport prints the ERC721 tree report without throwing", async () => {
        expect(() => adminActions.printScanReport(report721)).to.not.throw();
        console.log("\n--- ERC721 tree report ---");
        adminActions.printScanReport(report721);
    });
});

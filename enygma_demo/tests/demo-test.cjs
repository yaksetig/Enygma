const assert = require("assert");
const { chromium, launchOpts, freshPage, clickAndWait, setupProtocol, BASE_URL } = require("./_env.cjs");

const ids = ["institutional", "retail", "dvp", "auctions"];
let passed = 0;
function pass(name) { passed += 1; console.log(`  PASS  ${name}`); }

(async () => {
  const browser = await chromium.launch({ ...launchOpts, headless: true });
  try {
    const { context, page } = await freshPage(browser, "institutional");

    assert.equal(await page.locator('[data-command="deploy"]').isVisible(), true);
    assert.equal(await page.locator("[data-action]").count(), 0);
    assert.equal(await page.locator('[data-executing-entity="System operator"]').isVisible(), true);
    pass("strict setup gating starts at operator deployment");

    await clickAndWait(page, '[data-command="deploy"]');
    assert.equal(await page.locator('[data-command="auditor"]').isVisible(), true);
    assert.equal(await page.locator('[data-executing-entity="Auditor"]').isVisible(), true);
    await clickAndWait(page, '[data-command="auditor"]');
    assert.equal(await page.locator('[data-executing-entity="System operator"]').isVisible(), true);
    await clickAndWait(page, '[data-command="configure"]');
    assert.equal(await page.locator('[data-executing-entity="Protocol participant"]').isVisible(), true);
    await clickAndWait(page, '[data-command="identity-spend-secret"]');
    let state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.identityCeremony.phase, "spend_secret");
    assert(state.identityCeremony.spendPrivateKey.startsWith("spend_sk_"));
    assert.equal(state.identityCeremony.viewPrivateKey, null);
    assert.equal(state.identityCeremony.spendPublicKey, null);
    assert.equal(state.identities.length, 0);
    assert.equal(await page.locator(".secret-output.is-ready").count(), 1);
    pass("spend secret key is generated first");

    await clickAndWait(page, '[data-command="identity-spend-public"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.identityCeremony.phase, "spend_public");
    assert(state.identityCeremony.spendPublicKey.startsWith("spend_pk_"));
    assert.equal(state.identityCeremony.viewPrivateKey, null);
    assert.equal(await page.locator(".public-output.is-ready").count(), 1);
    assert.match(await page.locator(".spend-card").innerText(), /obtained by hashing the spend secret key/i);
    pass("spend public key follows its hash operation");

    await clickAndWait(page, '[data-command="identity-view-secret"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.identityCeremony.phase, "view_secret");
    assert(state.identityCeremony.viewPrivateKey.startsWith("mlkem_sk_"));
    assert.equal(state.identityCeremony.viewPublicKey, null);
    assert.equal(await page.locator(".secret-output.is-ready").count(), 2);
    pass("view secret key follows the completed spend keypair");

    await clickAndWait(page, '[data-command="identity-view-public"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.identityCeremony.phase, "complete");
    assert(state.identityCeremony.viewPublicKey.startsWith("mlkem_pk_"));
    assert.equal(await page.locator(".public-output.is-ready").count(), 2);
    assert.match(await page.locator(".view-card").innerText(), /ML-KEM key generation/i);
    await clickAndWait(page, '[data-command="identity-confirm"]');
    assert.equal(await page.locator('[data-executing-entity="Registering participant"]').isVisible(), true);
    pass("view public key completes the sequential ceremony");
    pass("ordered deploy, auditor key, configuration, and identity stages");

    assert.equal(await page.locator(".registry-table tbody tr").count(), 1);
    assert.equal(await page.locator('[data-party-row="1"]').count(), 0);
    assert.deepEqual(await page.locator(".registry-table th").allTextContents(), ["Participant", "Spend public key", "View public key", "1 · Register keys", "2 · Share with auditor"]);
    assert.equal(await page.locator('[data-party-row="0"] [data-key="spend"] code').textContent(), state.identities[0].spendPublicKey);
    assert.equal(await page.locator('[data-party-row="0"] [data-key="view"] code').textContent(), state.identities[0].viewPublicKey);
    await clickAndWait(page, '[data-command="register-participant-keys"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.institutional.registrations.length, 1);
    assert.equal(state.protocols.institutional.registrations[0].policy, "long_term");
    assert.equal(state.protocols.institutional.registrations[0].auditEnvelope, null);
    assert.equal(await page.locator(".registry-table tbody tr").count(), 1);
    assert.equal(await page.locator('[data-command="share-participant-key"]').isVisible(), true);
    pass("participant public keys register before auditor sharing");
    await clickAndWait(page, '[data-command="share-participant-key"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert(state.protocols.institutional.registrations[0].auditEnvelope);
    assert.equal(await page.locator('[data-command="register-others"]').isVisible(), true);
    await clickAndWait(page, '[data-command="register-others"]');
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.deepEqual(state.protocols.institutional.registrations.map(r => r.partyId), Array.from({ length: 10 }, (_, i) => `party-${i}`));
    assert(state.protocols.institutional.registrations.every(r => r.policy === "long_term" && r.auditEnvelope));
    assert.equal(await page.locator(".scenario-page .registry-table tbody tr").count(), 10);
    assert.equal(await page.locator("#setupWorkspace").isHidden(), true);
    assert.equal(await page.locator("#progress").isHidden(), true);
    assert.equal(await page.locator(".scenario-page > article").count(), 1);
    assert.equal(await page.locator(".scenario-page [data-action]").count(), 0);
    pass("participant shares with auditor before other parties register");
    pass("completed registration becomes its own walkthrough screen");

    for (const id of ids.slice(1)) await setupProtocol(page, id);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    for (const id of ids) {
      const registrations = state.protocols[id].registrations;
      assert.equal(registrations.length, 10);
      registrations.forEach((registration, index) => {
        assert.equal(registration.spendPublicKey, state.identities[index].spendPublicKey);
        assert.equal(registration.viewPublicKey, state.identities[index].viewPublicKey);
      });
      assert(registrations.every(r => r.policy === "long_term" && r.auditEnvelope));
      assert.equal(state.protocols[id].selectiveRegulator.name, "Additional regulator");
    }
    pass("exact global spend and ML-KEM view keys are reused in all registries");
    pass("all participants use long-term auditing in every protocol");
    assert(ids.every(id => state.protocols[id].leaves.length === 0));
    pass("every commitment tree starts empty with no seeded leaves");

    const auditors = ids.map(id => state.protocols[id].auditor.publicKey);
    assert.equal(new Set(auditors).size, 4);
    assert.equal(state.protocols.retail.transactions.length, 0);
    await page.goto(`${BASE_URL}/#/institutional/channels`);
    assert.equal(await page.locator(".scenario-page > article").count(), 1);
    assert.equal(await page.locator('.scenario-progress [aria-current="step"] strong').innerText(), "Pairwise channels");
    assert.equal(await page.locator('.scenario-page [data-action="payment"]').count(), 0);
    await page.click('[data-action="channels"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.institutional.flow.channels, true);
    assert.equal(state.protocols.institutional.flow.channelPairs.length, 45);
    assert.equal(new Set(state.protocols.institutional.flow.channelPairs.map(pair => `${pair.leftPartyId}:${pair.rightPartyId}`)).size, 45);
    assert.equal(await page.locator(".channel-cell-pair.established").count(), 45);
    assert.match(await page.locator(".channel-network-card").innerText(), /45 \/ 45 ESTABLISHED/);
    assert.equal(state.protocols.retail.transactions.length, 0);
    pass("institutional matrix exposes all 45 unique pairwise channels");
    pass("protocol setup and ledger state remain independent");

    await page.goto(`${BASE_URL}/#/institutional/policy`);
    await page.click('[data-action="freeze"]'); await page.waitForTimeout(40);
    assert.equal((await page.evaluate(() => window.__ENYGMA_DEMO__.state())).protocols.institutional.flow.frozen, true);
    await page.click('[data-action="resume"]'); await page.waitForTimeout(40);
    await page.goto(`${BASE_URL}/#/institutional/payment`);
    await page.click('[data-action="payment"]'); await page.waitForTimeout(40);
    pass("institutional payment, freeze, and recovery paths");

    await page.goto(`${BASE_URL}/#/retail/payment`);
    assert.equal(await page.locator('[data-action="payment"]').isDisabled(), true);
    assert.equal(await page.getByText(/direct tagged channel|rotating recipient tag/i).count(), 0);
    await page.goto(`${BASE_URL}/#/retail/recipient`);
    await page.click('[data-action="prepare-recipient"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.deepEqual(state.protocols.retail.flow.retailRecipient, {
      partyId: state.protocols.retail.registrations[1].partyId,
      name: state.protocols.retail.registrations[1].name,
      spendPublicKey: state.protocols.retail.registrations[1].spendPublicKey,
      viewPublicKey: state.protocols.retail.registrations[1].viewPublicKey
    });
    assert.equal(await page.locator(".recipient-key-grid .key-value").count(), 2);
    await page.goto(`${BASE_URL}/#/retail/payment`);
    assert.match(await page.locator(".payment-construction").innerText(), /ML-KEM encapsulation[\s\S]*Commitments[\s\S]*ZK proof[\s\S]*Submit/);
    await page.click('[data-action="payment"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    const retailPayment = state.protocols.retail.transactions.find(tx => tx.type === "payment");
    assert.equal(retailPayment.from, "party-0");
    assert.equal(retailPayment.to, "party-1");
    assert.equal(state.protocols.retail.leaves.filter(leaf => leaf.sourceTxId === retailPayment.id).length, 2);
    pass("retail payment uses registered recipient keys and the standard per-payment ML-KEM flow");

    await page.goto(`${BASE_URL}/#/dvp/terms-proposal`);
    assert.match(await page.locator(".dvp-parties").innerText(), /Boreal Markets[\s\S]*Atlas Bank/);
    assert.equal(await page.locator('[data-action="propose-terms"]').isDisabled(), true);
    await page.goto(`${BASE_URL}/#/dvp/seller-holdings`);
    await page.fill("#dvpMintSecurityAmount", "1800");
    await page.click('[data-action="mint-security"]'); await page.waitForTimeout(40);
    await page.goto(`${BASE_URL}/#/dvp/seller-asset`);
    await page.fill("#dvpShieldSecurityAmount", "250");
    await page.click('[data-action="shield-security"]'); await page.waitForTimeout(40);
    await page.fill("#dvpShieldSecurityAmount", "950");
    await page.click('[data-action="shield-security"]'); await page.waitForTimeout(40);
    assert.equal(await page.locator('[data-tree-asset="RAYLS-BOND-2030"] .leaf-node.owned-leaf').count(), 2);
    assert.equal(await page.locator('[data-tree-asset="RAYLS-BOND-2030"] .leaf-node[data-owned="true"]').count(), 2);
    await page.goto(`${BASE_URL}/#/dvp/buyer-holdings`);
    await page.fill("#dvpMintCashAmount", "12000000");
    await page.click('[data-action="mint-cash"]'); await page.waitForTimeout(40);
    await page.goto(`${BASE_URL}/#/dvp/terms-proposal`);
    assert.equal(await page.locator('[data-action="propose-terms"]').isDisabled(), true);
    await page.goto(`${BASE_URL}/#/dvp/buyer-cash`);
    await page.fill("#dvpShieldCashAmount", "2462500");
    await page.click('[data-action="shield-cash"]'); await page.waitForTimeout(40);
    await page.fill("#dvpShieldCashAmount", "5537500");
    await page.click('[data-action="shield-cash"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.dvp.flow.sellerPublicSecurity, 600);
    assert.equal(state.protocols.dvp.flow.sellerShieldedSecurity, 1200);
    assert.equal(state.protocols.dvp.flow.buyerPublicCash, 4000000);
    assert.equal(state.protocols.dvp.flow.buyerShieldedCash, 8000000);
    assert.equal(state.protocols.dvp.flow.dvpTerms.status, "draft");
    assert.equal(state.protocols.dvp.leaves.length, 4);
    assert.equal(state.protocols.dvp.trees["RAYLS-BOND-2030"].leafIds.length, 2);
    assert.equal(state.protocols.dvp.trees.USD.leafIds.length, 2);
    assert.notEqual(state.protocols.dvp.trees["RAYLS-BOND-2030"].root, state.protocols.dvp.trees.USD.root);
    assert(state.protocols.dvp.leaves.every(leaf => state.protocols.dvp.transactions.some(tx => tx.id === leaf.sourceTxId && ["shield-security", "shield-cash"].includes(tx.type))));
    assert.equal(state.protocols.dvp.notes.length, 4);
    assert.equal(new Set(state.protocols.dvp.notes.map(note => note.leafId)).size, 4);
    assert.equal(new Set(state.protocols.dvp.notes.map(note => note.salt)).size, 4);
    assert(state.protocols.dvp.notes.every(note => state.protocols.dvp.leaves.some(leaf => leaf.id === note.leafId && leaf.commitment === note.commitment)));
    assert.equal(await page.locator(".owned-note").count(), 2);
    assert.equal(await page.locator('[data-tree-asset="USD"] .leaf-node.owned-leaf').count(), 2);
    assert.match(await page.locator(".note-construction-card").innerText(), /One shielding operation → one private note → one leaf[\s\S]*Leaf 0[\s\S]*Leaf 1/i);
    pass("repeated partial shielding creates a new leaf for every submission");
    assert.equal(await page.locator(".shielding-network [data-traffic]").count(), 1);
    assert.equal(await page.locator('[data-tree-asset="USD"] .leaf-node').count(), 2);
    const cashRootBeforeTraffic = await page.locator('[data-tree-asset="USD"]').getAttribute("data-merkle-root");
    const bondRootBeforeTraffic = state.protocols.dvp.trees["RAYLS-BOND-2030"].root;
    assert.equal(cashRootBeforeTraffic, state.protocols.dvp.trees.USD.root);
    await page.check(".shielding-network [data-traffic]");
    await page.waitForTimeout(3550);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert(state.protocols.dvp.leaves.length > 4);
    assert(state.protocols.dvp.transactions.some(tx => tx.type === "background-shield"));
    assert(state.protocols.dvp.leaves.every(leaf => state.protocols.dvp.transactions.some(tx => tx.id === leaf.sourceTxId)));
    assert.notEqual(state.protocols.dvp.trees.USD.root, cashRootBeforeTraffic);
    assert.equal(state.protocols.dvp.trees["RAYLS-BOND-2030"].root, bondRootBeforeTraffic);
    assert.equal(await page.locator('[data-tree-asset="USD"] .leaf-node.new-path').count(), 1);
    assert.equal(await page.locator('[data-tree-asset="USD"] .leaf-node.owned-leaf').count(), 2);
    assert.equal(await page.locator('[data-tree-asset="USD"] .leaf-node.new-path.owned-leaf').count(), 0);
    await page.uncheck(".shielding-network [data-traffic]");
    pass("cash traffic updates only the cash tree while the bond root remains unchanged");

    await page.goto(`${BASE_URL}/#/dvp/terms-proposal`);
    assert.equal(await page.locator('.terms-form [data-term-leg="security"] #dvpSecurityNote').count(), 1);
    assert.equal(await page.locator('.terms-form [data-term-leg="cash"] #dvpCashNote').count(), 1);
    assert.equal(await page.locator("#dvpSecurityNote option").count(), 2);
    assert.equal(await page.locator("#dvpCashNote option").count(), 2);
    const selectedSecurityNoteId = await page.locator("#dvpSecurityNote").inputValue();
    const selectedCashNoteId = await page.locator("#dvpCashNote").inputValue();
    await page.fill("#dvpExpiryBlocks", "40");
    await page.click('[data-action="propose-terms"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.deepEqual({ status: state.protocols.dvp.flow.dvpTerms.status, securityId: state.protocols.dvp.flow.dvpTerms.securityId, quantity: state.protocols.dvp.flow.dvpTerms.quantity, cashAmount: state.protocols.dvp.flow.dvpTerms.cashAmount, expiryBlocks: state.protocols.dvp.flow.dvpTerms.expiryBlocks, securityInputNoteId: state.protocols.dvp.flow.dvpTerms.securityInputNoteId, cashInputNoteId: state.protocols.dvp.flow.dvpTerms.cashInputNoteId }, { status: "proposed", securityId: "RAYLS-BOND-2030", quantity: 250, cashAmount: 2462500, expiryBlocks: 40, securityInputNoteId: selectedSecurityNoteId, cashInputNoteId: selectedCashNoteId });
    await page.goto(`${BASE_URL}/#/dvp/terms-acceptance`);
    assert.equal(await page.locator(".terms-form").count(), 0);
    assert.equal(await page.locator(".acceptance-sheet").count(), 1);
    assert.match(await page.locator(".acceptance-sheet").innerText(), /Buyer commits[\s\S]*2,462,500 USD[\s\S]*Buyer receives[\s\S]*250 RAYLS-BOND-2030/i);
    await page.click('[data-action="accept-terms"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.dvp.flow.dvpTerms.status, "agreed");
    assert.equal(state.protocols.dvp.flow.dvpTransfer, null);
    assert.equal(state.protocols.dvp.flow.cashLocked, false);
    assert.equal(await page.locator('[data-action="instantiate-transfer"]').isEnabled(), true);
    await page.click('[data-action="instantiate-transfer"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    const firstDvpTransferId = state.protocols.dvp.flow.dvpTransfer.id;
    assert(firstDvpTransferId.startsWith("dvp_"));
    assert(state.protocols.dvp.flow.dvpTransfer.termsCommitment.startsWith("terms_"));
    assert.equal(state.protocols.dvp.flow.dvpTransfer.status, "awaiting_counterparty");
    assert.equal(state.protocols.dvp.flow.cashLocked, true);
    assert.equal(state.protocols.dvp.flow.securityLocked, false);
    assert.equal(state.protocols.dvp.notes.find(note => note.id === selectedCashNoteId).status, "locked");
    assert.equal(await page.locator(".transfer-identity").count(), 1);
    pass("DvP term sheet groups securities and cash under their respective legs");
    pass("buyer acceptance and transfer initiation are distinct actions on one approval screen");
    await page.goto(`${BASE_URL}/#/dvp/timeout`);
    assert.equal(await page.locator('[data-action="timeout"]').isEnabled(), true);
    await page.click('[data-action="timeout"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.dvp.flow.settlement, "reverted");
    assert.equal(state.protocols.dvp.flow.dvpTransfer.status, "reverted");
    assert.equal(state.protocols.dvp.flow.securityLocked, false);
    assert.equal(state.protocols.dvp.flow.cashLocked, false);
    assert.equal(state.protocols.dvp.notes.find(note => note.id === selectedCashNoteId).status, "unspent");
    pass("initiating the DvP immediately exposes the one-sided timeout path");

    await page.click('[data-action="instantiate-transfer"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.notEqual(state.protocols.dvp.flow.dvpTransfer.id, firstDvpTransferId);
    assert.equal(state.protocols.dvp.flow.cashLocked, true);
    assert.equal(state.protocols.dvp.flow.securityLocked, false);
    await page.goto(`${BASE_URL}/#/dvp/settlement`);
    await page.click('[data-action="lock-security"]'); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.dvp.flow.settlement, "settled");
    assert.equal(state.protocols.dvp.flow.securityLocked, true);
    assert.equal(state.protocols.dvp.flow.cashLocked, true);
    assert.equal(state.protocols.dvp.flow.dvpTransfer.status, "settled");
    assert.equal(state.protocols.dvp.transactions.some(tx => tx.type === "atomic-settlement"), true);
    assert.equal(state.protocols.dvp.flow.buyerPrivateSecurity, 250);
    assert.equal(state.protocols.dvp.flow.sellerPrivateCash, 2462500);
    assert.equal(state.protocols.dvp.flow.sellerShieldedSecurity, 950);
    assert.equal(state.protocols.dvp.flow.buyerShieldedCash, 5537500);
    assert.equal(state.protocols.dvp.notes.find(note => note.id === selectedSecurityNoteId).status, "spent");
    assert.equal(state.protocols.dvp.notes.find(note => note.id === selectedCashNoteId).status, "spent");
    assert.equal(state.protocols.dvp.notes.filter(note => note.origin === "dvp_output" && note.transferId === state.protocols.dvp.flow.dvpTransfer.id).length, 2);
    const settledLegCommitments = state.protocols.dvp.transactions.filter(tx => tx.encryptedPayload?.scope === "dvp_leg" && tx.encryptedPayload.transferId === state.protocols.dvp.flow.dvpTransfer.id).map(tx => tx.encryptedPayload.commitment).sort();
    const settlementTx = state.protocols.dvp.transactions.find(tx => tx.type === "atomic-settlement");
    assert.deepEqual(state.protocols.dvp.leaves.filter(leaf => leaf.sourceTxId === settlementTx.id).map(leaf => leaf.commitment).sort(), settledLegCommitments);
    await page.goto(`${BASE_URL}/#/dvp/timeout`);
    assert.equal(await page.locator('[data-action="timeout"]').count(), 0);
    assert.equal(await page.locator('[data-action="instantiate-transfer"]').count(), 0);
    assert.match(await page.locator(".scenario-page").innerText(), /This transfer settled[\s\S]*exactly one leg is locked/i);
    assert.equal(await page.locator('[data-action="settle"]').count(), 0);
    pass("second DvP leg lock settles atomically with no timeout or manual settlement");

    await page.goto(`${BASE_URL}/#/dvp/audit`);
    assert.equal(await page.locator(".audit-perspective").count(), 3);
    assert.equal(await page.locator('[data-share]').count(), 0);
    assert.match(await page.locator(".audit-registration-status").innerText(), /Auditor access established during registration[\s\S]*10 encrypted participant view keys/i);
    assert.doesNotMatch(await page.locator(".audit-perspective.outsider").innerText(), /Atlas Bank|Boreal Markets|2,462,500|250 RAYLS/i);
    assert.match(await page.locator(".audit-perspective.participant").innerText(), /250 RAYLS-BOND-2030[\s\S]*cannot open/i);
    assert.doesNotMatch(await page.locator(".audit-perspective.participant").innerText(), /2,462,500 USD/i);
    assert.match(await page.locator(".audit-perspective.auditor").innerText(), /250 RAYLS-BOND-2030[\s\S]*2,462,500 USD/i);
    assert.equal(state.protocols.dvp.disclosures.length, 0);
    assert.equal(await page.locator('.scenario-progress a[href="#/dvp/traffic"], .scenario-progress a[href="#/dvp/chain"]').count(), 0);
    assert.equal(await page.locator('.scenario-progress a[href="#/dvp/unshield"]').getByText(/Optional unshield/i).count(), 1);
    pass("DvP audit view uses registration-time access and keeps the outsider view opaque");

    const auctionActions = [
      ["auctioneer", "auctioneer"], ["mint", "mint"], ["open", "open"],
      ["bids", "bid"], ["bids", "bid"], ["batch", "batch"], ["settle", "settle"],
      ["challenge", "challenge"], ["challenge", "recover"]
    ];
    for (const [screen, action] of auctionActions) {
      await page.goto(`${BASE_URL}/#/auctions/${screen}`);
      await page.click(`[data-action="${action}"]`); await page.waitForTimeout(40);
    }
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.auctions.flow.challengeOpen, false);
    assert(state.protocols.auctions.flow.auctioneer.publicKey.startsWith("mlkem_pub_"));
    assert.notEqual(state.protocols.auctions.flow.auctioneer.publicKey, state.protocols.auctions.auditor.publicKey);
    pass("auction setup, bids, batch, settlement, challenge, and recovery paths");

    await page.goto(`${BASE_URL}/#/retail/traffic`);
    const before = await page.evaluate(() => ({ tx: window.__ENYGMA_DEMO__.state().protocols.retail.transactions.length, leaves: window.__ENYGMA_DEMO__.state().protocols.retail.leaves.length }));
    await page.waitForTimeout(3550);
    const off = await page.evaluate(() => ({ tx: window.__ENYGMA_DEMO__.state().protocols.retail.transactions.length, leaves: window.__ENYGMA_DEMO__.state().protocols.retail.leaves.length }));
    assert.deepEqual(off, before);
    await page.check("[data-traffic]");
    await page.waitForTimeout(3550);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert(state.protocols.retail.transactions.length > before.tx);
    const txIds = new Set(state.protocols.retail.transactions.map(tx => tx.id));
    assert(state.protocols.retail.leaves.every(leaf => txIds.has(leaf.sourceTxId)));
    await page.uncheck("[data-traffic]");
    pass("traffic off is stable and traffic on creates only transaction-backed leaves");

    const firstTx = state.protocols.retail.transactions[0].id;
    await page.goto(`${BASE_URL}/#/retail/audit`);
    await page.click(`[data-share="${firstTx}"]`); await page.waitForTimeout(40);
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert(state.protocols.retail.disclosures.some(item => item.txId === firstTx && item.scope === "single transaction" && item.recipient === "Additional regulator"));
    pass("selective disclosure grants one transaction key to the additional regulator");

    await page.reload();
    await page.waitForFunction(() => window.__ENYGMA_DEMO__);
    assert.equal((await page.evaluate(() => window.__ENYGMA_DEMO__.state())).protocols.retail.registrations.length, 10);
    assert.equal(new URL(page.url()).hash, "#/retail/audit");
    pass("hash routes and session restoration retain progress");

    await page.evaluate(() => window.__ENYGMA_DEMO__.engine.resetProtocol("retail"));
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.protocols.retail.registrations.length, 0);
    assert.equal(state.identities.length, 10);
    await page.evaluate(() => window.__ENYGMA_DEMO__.engine.resetAll());
    state = await page.evaluate(() => window.__ENYGMA_DEMO__.state());
    assert.equal(state.identities.length, 0);
    assert(ids.every(id => state.protocols[id].contracts.length === 0));
    pass("protocol and entire-demo resets have distinct scope");

    await page.goto(`${BASE_URL}/#/choose`);
    assert.equal(await page.locator("#chooser h1").innerText(), "Choose a protocol to bring online.");
    assert.equal(await page.locator(".skip-link").count(), 1);
    assert.equal(await page.locator("nav[aria-label=Protocols]").count(), 1);
    await context.close();
    const mobile = await freshPage(browser, "choose", { width: 390, height: 844 });
    const overflow = await mobile.page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    assert.equal(overflow, false);
    assert.equal(await mobile.page.locator(".protocol-card").count(), 4);
    await mobile.context.close();
    pass("accessible landmarks and responsive mobile layout");

    console.log(`\n${passed} browser behavior checks passed.`);
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });

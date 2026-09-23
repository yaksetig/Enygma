const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const { chromium, launchOpts, freshPage, clickAndWait, setupProtocol, BASE_URL, root } = require("./_env.cjs");
let passed = 0;
const pass = label => { passed++; console.log(`  PASS  ${label}`); };

(async () => {
  execFileSync(process.execPath, [path.join(root, "scripts/sync-institutional-constants.cjs"), "--check"]);
  const browser = await chromium.launch({ ...launchOpts, headless: true });
  try {
    const { context, page } = await freshPage(browser, "institutional");
    const crypto = await page.evaluate(async () => {
      const c = await import("/js/institutional-crypto.js");
      return { G: c.G.map(String), H: c.H.map(String), field: String(c.FIELD), order: String(c.ORDER), hash: String(c.poseidon([1, 2])), commitment: c.pedersen(100, 1234).map(String), conserved: c.samePoint(c.pointAdd(c.pedersen(-100, 1234), c.pedersen(100, -1234)), [0, 1]) };
    });
    const curveSource = fs.readFileSync(path.join(root, "../enygma_payments/contracts/enygma/contracts/CurveBabyJubJub.sol"), "utf8");
    [...crypto.G, ...crypto.H, crypto.field, crypto.order].forEach(n => assert(curveSource.includes(n)));
    assert.equal(crypto.hash, "7853200120776062878684798364095072458815029376092732009249414926327459813530");
    // Independently calculated with circomlibjs BabyJubJub using the protocol's G/H.
    assert.deepEqual(crypto.commitment, ["13118723633202160428297237932582455498321078918117382354378846477878281743829", "5158729041309331809907455067260836859315088765819363007687726841726284429695"]);
    assert(crypto.conserved);
    pass("Poseidon and Pedersen calculations match protocol constants and independent vectors");

    await setupProtocol(page, "institutional");
    const snapshot = () => page.evaluate(() => window.__ENYGMA_DEMO__.state().protocols.institutional);
    const act = (action, payload = {}) => page.evaluate(async ({ action, payload }) => {
      try { await window.__ENYGMA_DEMO__.engine.executeProtocolAction("institutional", action, payload); return null; }
      catch (error) { return error.message; }
    }, { action, payload });
    const initial = await snapshot();
    assert(initial.flow.institutional.accounts.every(a => a.balance === 0));
    const nav = await page.locator(".scenario-progress").innerText();
    assert(!/bridge|perspectives|channel frozen/i.test(nav));
    await page.goto(`${BASE_URL}/#/institutional/channels`);
    await clickAndWait(page, '[data-action="channels"]');
    await page.goto(`${BASE_URL}/#/institutional/funding`);
    await clickAndWait(page, '[data-action="fund"]');
    assert((await snapshot()).flow.institutional.accounts.every(a => a.balance === 1000));
    pass("institutional setup explicitly funds accounts and removes bridge and Perspectives steps");

    await page.goto(`${BASE_URL}/#/institutional/payment`);
    assert.equal(await page.locator("[data-institutional-users] tbody tr").count(), 10);
    await page.selectOption("#institutionalPayer", "3");
    await page.selectOption("#institutionalRecipient", "5");
    await page.selectOption("#institutionalK", "6");
    await page.fill("#institutionalAmount", "125");
    await page.locator("#institutionalAmount").press("Tab");
    await page.uncheck('[data-institutional-member="1"]');
    await page.check('[data-institutional-member="8"]');
    await clickAndWait(page, '[data-action="calculate"]');
    let current = await snapshot(), draft = current.flow.institutional.draft;
    assert.equal(draft.k, 6);
    assert.equal(draft.payerId, 3);
    assert.equal(draft.recipientId, 5);
    assert.deepEqual(draft.accountIds, [2, 3, 4, 5, 6, 8]);
    assert.equal(draft.rows.filter(row => row.value === 0).length, 4);
    assert.equal(draft.publicSignals.length, 81);
    assert.equal(draft.rows.reduce((sum, row) => sum + row.value, 0), 0);
    assert.equal(current.transactions.filter(tx => tx.batch).length, 0);
    assert(current.flow.institutional.accounts.every(a => a.balance === 1000));
    assert.match(await act("post"), /Generate the ZK proof/);
    await clickAndWait(page, '[data-action="prove"]');
    assert.equal((await snapshot()).flow.institutional.draft.status, "proved");
    pass("selectable payer and k=6 create six balanced commitments before proof generation or posting");

    await page.goto(`${BASE_URL}/#/institutional/policy`);
    assert.equal(await page.locator('[data-action="pause-contract"]').isDisabled(), true);
    assert.match(await act("pause-contract", { actor: "auditor" }), /Only the contract owner/);
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="freeze-user"]');
    assert.deepEqual((await snapshot()).flow.institutional.frozen, [8]);
    assert.match(await act("post"), /frozen from trading/);
    await page.selectOption("#institutionalControlRole", "owner");
    assert.equal(await page.locator('[data-controlled-account="8"] [data-action="unfreeze-user"]').isDisabled(), true);
    await clickAndWait(page, '[data-action="pause-contract"]');
    assert.match(await act("post"), /contract is paused/);
    await page.selectOption("#institutionalControlRole", "auditor");
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="unfreeze-user"]');
    assert.match(await act("post"), /contract is paused/);
    await page.selectOption("#institutionalControlRole", "owner");
    await clickAndWait(page, '[data-action="resume-contract"]');
    pass("contract pause is owner-only and independent of auditor user freezes");

    const before = (await snapshot()).flow.institutional.accounts;
    await page.goto(`${BASE_URL}/#/institutional/payment`);
    await clickAndWait(page, '[data-action="post"]');
    current = await snapshot();
    const posted = current.transactions.find(tx => tx.batch);
    assert.equal(posted.batch.status, "confirmed");
    assert.equal(posted.batch.verification, 6);
    assert.equal(posted.from, "relayer");
    assert.equal(current.flow.institutional.accounts.reduce((sum, a) => sum + a.balance, 0), 10000);
    assert.equal(current.flow.institutional.accounts[2].balance, 875);
    assert.equal(current.flow.institutional.accounts[4].balance, 1125);
    assert.notDeepEqual(current.flow.institutional.accounts[7].commitment, before[7].commitment);
    assert.equal(current.flow.institutional.accounts[7].balance, 1000);
    assert.deepEqual(current.flow.institutional.accounts[0], before[0]);
    assert.match(await act("post"), /balances changed/);
    pass("verified posting atomically changes selected commitments, conserves supply, and prevents replay");

    await page.goto(`${BASE_URL}/#/institutional/chain`);
    assert.equal(await page.locator("[data-institutional-balances] tbody tr").count(), 10);
    assert.equal(await page.locator("[data-institutional-payments] tbody tr").count(), 6);
    assert.equal(await page.locator('[data-open="true"]').count(), 0);
    assert.equal(await page.locator(".institutional-opening").count(), 0);
    await page.selectOption("#institutionalViewer", "3");
    assert.equal(await page.locator('[data-balance-account][data-open="true"]').count(), 1);
    assert.match(await page.locator('[data-balance-account="3"] .institutional-opening').innerText(), /875 EN/);
    assert.equal(await page.locator('[data-payment-account][data-open="true"]').count(), 1);
    await page.selectOption("#institutionalViewer", "8");
    assert.match(await page.locator('[data-balance-account="8"] .institutional-opening').innerText(), /1,000 EN/);
    assert.equal(await page.locator('[data-payment-account="3"]').getAttribute("data-open"), "false");
    await page.goto(`${BASE_URL}/#/institutional/audit`);
    assert.equal(await page.locator('[data-balance-account][data-open="true"]').count(), 10);
    assert.equal(await page.locator('[data-payment-account][data-open="true"]').count(), 6);
    pass("public ledger seals all openings; participant and auditor selections open exactly their scope");

    await page.goto(`${BASE_URL}/#/institutional/payment`);
    await clickAndWait(page, '[data-action="edit-payment"]');
    await page.selectOption("#institutionalK", "2");
    await page.selectOption("#institutionalPayer", "5");
    await page.selectOption("#institutionalRecipient", "3");
    await page.fill("#institutionalAmount", "40");
    for (const action of ["calculate", "prove", "post"]) await clickAndWait(page, `[data-action="${action}"]`);
    current = await snapshot();
    assert.equal(current.transactions.filter(tx => tx.batch).length, 2);
    assert.equal(current.transactions.find(tx => tx.batch).batch.k, 2);
    assert.equal(current.flow.institutional.accounts[2].balance, 915);
    assert.equal(current.flow.institutional.accounts[4].balance, 1085);
    const saved = current.flow.institutional;
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, saved);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.deepEqual(await page.evaluate(() => ({ page: document.documentElement.scrollWidth, viewport: innerWidth })), { page: 390, viewport: 390 });
    pass("k=2 payments, subsequent balance openings, session restoration, and mobile layout remain consistent");

    await page.setViewportSize({ width: 1280, height: 900 });
    await clickAndWait(page, '[data-action="edit-payment"]');
    const valid = { payerId: 3, recipientId: 5, k: 2, amount: 100, accountIds: [3, 5] };
    assert.match(await act("calculate", { ...valid, amount: 999999 }), /within the payer/);
    assert.match(await act("calculate", { ...valid, accountIds: [3, 3] }), /distinct registered/);
    assert.equal(await act("calculate", valid), null);
    await page.evaluate(() => { window.__ENYGMA_DEMO__.engine.protocol("institutional").flow.institutional.draft.rows[0].delta[0] = "1"; });
    assert.match(await act("prove"), /Commitment opening/);
    assert.equal(await act("calculate", valid), null);
    await page.evaluate(() => { window.__ENYGMA_DEMO__.engine.protocol("institutional").flow.institutional.draft.publicSignals[0] = "1"; });
    assert.match(await act("prove"), /public inputs changed/);
    assert.equal(await act("calculate", valid), null);
    assert.equal(await act("prove"), null);
    await act("fund", { amount: 1 });
    assert.match(await act("post"), /Calculate the commitment batch first/);
    pass("insufficient balances, malformed sets, altered commitments/public inputs, and invalidated proofs cannot settle");
    await context.close();
  } finally { await browser.close(); }
  console.log(`\n${passed} institutional checks passed.`);
})().catch(error => { console.error(error); process.exit(1); });

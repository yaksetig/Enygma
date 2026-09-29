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
    initial.flow.institutional.accounts.forEach(a => {
      assert.equal(a.randomness, "0");
      assert.deepEqual(a.commitment, ["0", "1"]);
    });
    const equalMints = await page.evaluate(async () => {
      const { mintInstitutional } = await import("/js/institutional.js");
      const p = structuredClone(window.__ENYGMA_DEMO__.state().protocols.institutional);
      for (const [recipientId, amount] of [[1, 1000], [2, 1000], [3, 600], [3, 400]]) mintInstitutional(p, { actor: "owner", recipientId, amount });
      return p.flow.institutional;
    });
    assert.deepEqual(equalMints.accounts[0].commitment, equalMints.accounts[1].commitment);
    assert.deepEqual(equalMints.accounts[0].commitment, equalMints.accounts[2].commitment);
    assert(equalMints.accounts.every(a => a.randomness === "0"));
    assert.equal(equalMints.totalSupplyAmount, 3000);
    pass("accounts start at the identity and equal mint totals produce identical unblinded commitments");
    const nav = await page.locator(".scenario-progress").innerText();
    assert(!/bridge|perspectives|channel frozen/i.test(nav));
    await page.goto(`${BASE_URL}/#/institutional/channels`);
    assert.equal(await page.locator("#institutionalChannelViewer option").count(), 12);
    for (const viewer of ["public", "party-0", "auditor"]) {
      await page.selectOption("#institutionalChannelViewer", viewer);
      assert.equal(await page.locator("[data-channel-secret]").count(), 0);
    }
    await page.selectOption("#institutionalChannelViewer", "public");
    await clickAndWait(page, '[data-action="channels"]');
    assert.equal(await page.locator("[data-channel-secret]").count(), 0);
    assert.equal(await page.locator(".channel-cell-sealed").count(), 45);
    await page.selectOption("#institutionalChannelViewer", "auditor");
    const displayedKeys = () => page.locator("[data-channel-key-row]").evaluateAll(rows => rows.map(row => ({
      id: row.dataset.channelKeyRow, left: row.dataset.channelLeft, right: row.dataset.channelRight,
      key: row.querySelector("[data-channel-secret]").textContent
    })));
    const allChannelKeys = await displayedKeys();
    assert.equal(allChannelKeys.length, 45);
    assert.equal(new Set(allChannelKeys.map(pair => pair.key)).size, 45);
    assert.equal(await page.locator(".channel-cell-readable").count(), 45);
    assert.equal(await page.locator(".channel-key-heading h3").innerText(), "Auditor · all authorized channels");
    const channelState = await snapshot();
    for (const participant of channelState.registrations) {
      await page.selectOption("#institutionalChannelViewer", participant.partyId);
      const expected = allChannelKeys.filter(pair => [pair.left, pair.right].includes(participant.partyId));
      assert.equal(expected.length, 9);
      assert.deepEqual(await displayedKeys(), expected, `${participant.name} sees only its own keys, identical to the auditor and counterparty views`);
      assert.equal(await page.locator(".channel-cell-readable").count(), 9);
      assert.equal(await page.locator(".channel-cell-sealed").count(), 36);
      assert.equal(await page.locator(".channel-key-heading h3").innerText(), `${participant.name} · own channels`);
      const html = await page.locator(".channel-network-card").innerHTML();
      for (const pair of allChannelKeys.filter(pair => ![pair.left, pair.right].includes(participant.partyId))) assert(!html.includes(pair.key), "unrelated keys must not enter participant markup");
    }
    await page.locator(".channel-shared-key summary").first().click();
    assert(await page.locator("[data-channel-secret]").first().isVisible());
    await page.selectOption("#institutionalChannelViewer", "public");
    const publicHtml = await page.locator(".channel-network-card").innerHTML();
    allChannelKeys.forEach(pair => assert(!publicHtml.includes(pair.key)));
    assert.equal(await page.locator("[data-channel-secret]").count(), 0);
    assert.deepEqual(await snapshot(), channelState, "changing perspectives must not mutate channels or payment state");
    pass("public, all ten participant, and auditor views expose exactly their authorized pairwise keys");

    await page.reload();
    await page.selectOption("#institutionalChannelViewer", "auditor");
    assert.deepEqual(await displayedKeys(), allChannelKeys, "existing channel records retain the same keys on reload");
    const desktopViewport = page.viewportSize();
    await page.setViewportSize({ width: 390, height: 844 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.selectOption("#institutionalChannelViewer", "party-1");
    assert.equal((await displayedKeys()).length, 9);
    await page.setViewportSize(desktopViewport);
    pass("channel keys survive reload and the perspective selector works on mobile");
    await page.goto(`${BASE_URL}/#/institutional/funding`);
    const beforeFunding = await snapshot();
    const mintInput = { actor: "owner", recipientId: 3, amount: 800 };
    for (const invalid of [{ actor: "auditor" }, { recipientId: 0 }, { recipientId: 11 }, { recipientId: 1.5 }, { amount: 0 }, { amount: -1 }, { amount: 1.5 }, { amount: "" }, { amount: "invalid" }, { amount: 1_000_000_001 }]) {
      assert(await act("fund", { ...mintInput, ...invalid }));
      assert.deepEqual(await snapshot(), beforeFunding, "rejected mints must leave accounts, supply and receipts untouched");
    }
    const checkSupply = () => page.evaluate(async () => {
      const { pointAdd, samePoint, pedersen } = await import("/js/institutional-crypto.js");
      const s = window.__ENYGMA_DEMO__.state().protocols.institutional.flow.institutional;
      return s.accounts.reduce((sum, a) => sum + a.balance, 0) === s.totalSupplyAmount
        && samePoint(s.accounts.reduce((sum, a) => pointAdd(sum, a.commitment), [0n, 1n]), s.totalSupplyCommitment)
        && s.accounts.every(a => samePoint(a.commitment, pedersen(a.balance, a.randomness)));
    });
    await page.fill("#institutionalFundingAmount", "800");
    await page.selectOption("#institutionalFundingRecipient", "3");
    assert.equal(await page.inputValue("#institutionalFundingAmount"), "800", "changing recipient retains the entered amount");
    assert.match(await page.locator("#institutionalMintExplanation").innerText(), /Minting adds 800G \+ 0H.*Boreal Markets/);
    assert.equal(await page.locator('[data-mint-account="3"] .mint-coordinate-details code').isVisible(), false);
    await page.locator('[data-mint-account="3"] .mint-coordinate-details summary').click();
    assert.match(await page.locator('[data-mint-account="3"] .mint-coordinate-details code').innerText(), /x = 0\s+y = 1/);
    await clickAndWait(page, '[data-action="fund"]');
    let funded = await snapshot();
    assert.equal(funded.flow.institutional.accounts[2].balance, 800);
    funded.flow.institutional.accounts.forEach((a, i) => { if (i !== 2) assert.deepEqual(a, beforeFunding.flow.institutional.accounts[i]); });
    assert.equal(funded.transactions[0].to, "party-2");
    assert.equal(funded.transactions[0].from, "owner");
    assert.equal(funded.transactions[0].mint.recipientId, 3);
    assert.equal(funded.transactions[0].mint.blinding, "none");
    assert.deepEqual(funded.transactions[0].mint.before, beforeFunding.flow.institutional.accounts[2].commitment);
    assert.deepEqual(funded.transactions[0].mint.after, funded.flow.institutional.accounts[2].commitment);
    assert(funded.transactions[0].block);
    assert.match(await page.locator("[data-mint-receipt]").first().innerText(), /800 EN to Boreal Markets/);
    await page.locator("[data-mint-receipt] summary").first().click();
    assert.match(await page.locator("[data-mint-receipt]").first().innerText(), /B_before[\s\S]*C_mint[\s\S]*B_after/);
    assert(await checkSupply());
    await page.selectOption("#institutionalFundingRecipient", "5");
    await page.fill("#institutionalFundingAmount", "2000");
    await clickAndWait(page, '[data-action="fund"]');
    await page.selectOption("#institutionalFundingRecipient", "3");
    await page.fill("#institutionalFundingAmount", "200");
    await clickAndWait(page, '[data-action="fund"]');
    funded = await snapshot();
    const allocations = funded.flow.institutional.accounts.map(a => a.balance);
    assert.deepEqual(allocations, [0, 0, 1000, 0, 2000, 0, 0, 0, 0, 0]);
    assert.equal(funded.flow.institutional.totalSupplyAmount, 3000);
    assert(funded.flow.institutional.accounts.every(a => a.randomness === "0"));
    assert(await checkSupply());
    assert.equal(await page.locator("[data-mint-receipt]").count(), 3);
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, funded.flow.institutional);
    await page.setViewportSize({ width: 390, height: 844 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.setViewportSize(desktopViewport);
    pass("owner mints different amounts to selected recipients, supports top-ups, and preserves other accounts and total supply");

    // Older sessions have balances and mint receipts but no aggregate supply fields.
    await page.evaluate(() => {
      const key = Object.keys(sessionStorage).find(key => key.startsWith("enygma-demo-state-"));
      const saved = JSON.parse(sessionStorage.getItem(key));
      delete saved.protocols.institutional.flow.institutional.totalSupplyAmount;
      delete saved.protocols.institutional.flow.institutional.totalSupplyCommitment;
      sessionStorage.setItem(key, JSON.stringify(saved));
    });
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, funded.flow.institutional);
    pass("recipient-specific minting survives reload and reconstructs supply for existing sessions");

    // A saved funding screen from the old model should retain its amounts and
    // recipients while replacing registration/mint masks with zero randomness.
    await page.evaluate(async () => {
      const { scalar, poseidon, pedersen, encodePoint, pointAdd } = await import("/js/institutional-crypto.js");
      const key = Object.keys(sessionStorage).find(key => key.startsWith("enygma-demo-state-"));
      const saved = JSON.parse(sessionStorage.getItem(key)), p = saved.protocols.institutional, s = p.flow.institutional;
      const random = s.accounts.map(a => scalar(poseidon([31, a.accountId])));
      random[9] = scalar(-random.slice(0, 9).reduce((a, b) => a + b, 0n));
      s.accounts.forEach((a, i) => Object.assign(a, { balance: 0, randomness: String(random[i]), commitment: encodePoint(pedersen(0, random[i])) }));
      p.transactions.filter(tx => tx.mint).reverse().forEach((tx, i) => {
        const mint = tx.mint, account = s.accounts[mint.recipientId - 1], r = scalar(poseidon([41, mint.recipientId, i + 1]));
        mint.before = [...account.commitment];
        mint.commitment = encodePoint(pedersen(mint.amount, r));
        account.balance += mint.amount;
        account.randomness = String(scalar(BigInt(account.randomness) + r));
        account.commitment = encodePoint(pointAdd(account.commitment, mint.commitment));
        mint.after = [...account.commitment];
        mint.blinding = "secret";
      });
      s.totalSupplyCommitment = encodePoint(s.accounts.reduce((sum, a) => pointAdd(sum, a.commitment), [0n, 1n]));
      s.draft = { status: "proved" };
      sessionStorage.setItem(key, JSON.stringify(saved));
    });
    await page.reload();
    const migrated = await snapshot();
    assert.deepEqual(migrated.flow.institutional.accounts, funded.flow.institutional.accounts);
    assert.deepEqual(migrated.flow.institutional.totalSupplyCommitment, funded.flow.institutional.totalSupplyCommitment);
    assert.deepEqual(migrated.transactions, funded.transactions);
    assert.equal(migrated.flow.institutional.draft, null);
    assert(await checkSupply());
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, migrated.flow.institutional);
    pass("saved funding sessions migrate to zero randomness without losing amounts or mint receipts");

    await page.goto(`${BASE_URL}/#/institutional/payment`);
    assert.equal(await page.locator("[data-institutional-users] tbody tr").count(), 10);
    assert.equal(await page.locator("[data-institutional-recipient-row]").count(), 2, "the default example showcases multiple recipients");
    assert.equal(await page.inputValue("#institutionalK"), "6");
    await page.click('[data-institutional-remove-recipient="1"]');
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
    assert.deepEqual(draft.recipients, [{ accountId: 5, amount: 125 }]);
    assert.deepEqual(draft.accountIds, [2, 3, 4, 5, 6, 8]);
    assert.equal(draft.rows.filter(row => row.value === 0).length, 4);
    assert.equal(draft.publicSignals.length, 81);
    const payerRecipientKey = allChannelKeys.find(pair => pair.left === "party-2" && pair.right === "party-4").key;
    const displayedFingerprint = await page.evaluate(async key => {
      const { scalar, poseidon } = await import("/js/institutional-crypto.js");
      return String(scalar(poseidon([BigInt(key)])));
    }, payerRecipientKey);
    assert.equal(draft.publicSignals[draft.accountIds.indexOf(3) * draft.k + draft.accountIds.indexOf(5)], displayedFingerprint, "displayed pairwise keys must match payment proof fingerprints");
    assert.equal(draft.rows.reduce((sum, row) => sum + row.value, 0), 0);
    assert.equal(current.transactions.filter(tx => tx.batch).length, 0);
    assert.deepEqual(current.flow.institutional.accounts.map(a => a.balance), allocations);
    assert.match(await act("post"), /Generate the ZK proof/);
    await clickAndWait(page, '[data-action="prove"]');
    assert.equal((await snapshot()).flow.institutional.draft.status, "proved");
    pass("selectable payer and k=6 create six balanced commitments before proof generation or posting");

    await page.goto(`${BASE_URL}/#/institutional/policy`);
    assert.equal(await page.locator('[data-action="pause-contract"]').isDisabled(), true);
    assert.match(await act("pause-contract", { actor: "auditor" }), /Only the contract owner/);
    const beforeUnauthorizedControls = await snapshot();
    for (const action of ["freeze-user", "unfreeze-user"]) {
      for (const actor of ["public", "participant", undefined]) {
        assert.match(await act(action, { actor, accountId: 8 }), /Only the contract owner or auditor/);
      }
      assert.match(await act(action, { actor: "owner", accountId: 999 }), /Choose a registered user/);
    }
    assert.deepEqual(await snapshot(), beforeUnauthorizedControls);
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="freeze-user"]');
    assert.deepEqual((await snapshot()).flow.institutional.frozen, [8]);
    assert.match((await snapshot()).transactions[0].label, /^Auditor froze/);
    assert.match(await act("post"), /frozen from trading/);
    await page.selectOption("#institutionalControlRole", "owner");
    assert.equal(await page.locator('[data-controlled-account="8"] [data-action="unfreeze-user"]').isDisabled(), false);
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="unfreeze-user"]');
    assert.deepEqual((await snapshot()).flow.institutional.frozen, []);
    assert.match((await snapshot()).transactions[0].label, /^Owner unfroze/);
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="freeze-user"]');
    assert.deepEqual((await snapshot()).flow.institutional.frozen, [8]);
    assert.match((await snapshot()).transactions[0].label, /^Owner froze/);
    assert.match(await act("post"), /frozen from trading/);
    await clickAndWait(page, '[data-action="pause-contract"]');
    assert.match(await act("post"), /contract is paused/);
    const pausedState = await snapshot();
    assert.match(await act("fund", mintInput), /contract is paused/);
    assert.deepEqual(await snapshot(), pausedState);
    await page.selectOption("#institutionalControlRole", "auditor");
    assert.equal(await page.locator('[data-action="resume-contract"]').isDisabled(), true);
    assert.match(await act("resume-contract", { actor: "auditor" }), /Only the contract owner/);
    await clickAndWait(page, '[data-controlled-account="8"] [data-action="unfreeze-user"]');
    assert.deepEqual((await snapshot()).flow.institutional.frozen, []);
    assert.match((await snapshot()).transactions[0].label, /^Auditor unfroze/);
    assert.match(await act("post"), /contract is paused/);
    await page.selectOption("#institutionalControlRole", "owner");
    await clickAndWait(page, '[data-action="resume-contract"]');
    pass("owner and auditor can freeze/unfreeze users, receipts identify the actor, and contract pause remains owner-only");

    const before = (await snapshot()).flow.institutional.accounts;
    await page.goto(`${BASE_URL}/#/institutional/payment`);
    await clickAndWait(page, '[data-action="post"]');
    current = await snapshot();
    const posted = current.transactions.find(tx => tx.batch);
    assert.equal(posted.batch.status, "confirmed");
    assert.equal(posted.batch.verification, 6);
    assert.equal(posted.from, "relayer");
    assert.equal(current.flow.institutional.accounts.reduce((sum, a) => sum + a.balance, 0), 3000);
    assert(await checkSupply());
    assert.equal(current.flow.institutional.accounts[2].balance, 875);
    assert.equal(current.flow.institutional.accounts[4].balance, 2125);
    assert.notDeepEqual(current.flow.institutional.accounts[7].commitment, before[7].commitment);
    assert.equal(current.flow.institutional.accounts[7].balance, 0);
    assert.deepEqual(current.flow.institutional.accounts[0], before[0]);
    assert.match(await act("post"), /balances changed/);
    pass("verified posting atomically changes selected commitments, conserves supply, and prevents replay");

    await page.goto(`${BASE_URL}/#/institutional/chain`);
    assert.equal(await page.locator("[data-institutional-balances] tbody tr").count(), 10);
    assert.equal(await page.locator("[data-institutional-payments] [data-payment-account]").count(), 6);
    assert.equal(await page.locator("#institutionalViewer option").count(), 12);
    const publicBalances = current.flow.institutional.accounts.filter(a => a.randomness === "0").map(a => a.accountId);
    assert.equal(await page.locator('[data-open="true"]').count(), publicBalances.length);
    assert.equal(await page.locator(".institutional-opening").count(), publicBalances.length);
    assert.equal(await page.locator(".institutional-slot-opening").count(), 0);
    const visibilitySnapshot = await snapshot();
    for (let viewerId = 1; viewerId <= 10; viewerId++) {
      const expectedOpen = viewerId === 3 ? posted.batch.accountIds : posted.batch.accountIds.includes(viewerId) ? [viewerId] : [];
      await page.selectOption("#institutionalViewer", String(viewerId));
      assert.equal(await page.locator("#institutionalViewer").evaluate(el => el === document.activeElement), true);
      assert.deepEqual(await page.locator('[data-payment-account][data-open="true"]').evaluateAll(els => els.map(el => Number(el.dataset.paymentAccount))), expectedOpen);
      assert.equal(await page.locator(".institutional-slot-opening").count(), expectedOpen.length);
      assert.equal(await page.locator('[data-balance-account][data-open="true"]').count(), new Set([...publicBalances, viewerId]).size);
      assert.equal(await page.locator(".institutional-slot-balance").count(), posted.batch.accountIds.includes(viewerId) ? 1 : 0, "sending a payment must not reveal other accounts' total balances");
      const projected = await page.evaluate(async viewer => {
        const { institutionalPaymentView } = await import("/js/institutional.js");
        const batch = window.__ENYGMA_DEMO__.state().protocols.institutional.transactions.find(tx => tx.batch).batch;
        return institutionalPaymentView(batch, viewer);
      }, String(viewerId));
      for (const row of posted.batch.rows) {
        const slot = page.locator(`[data-payment-account="${row.accountId}"]`), projectedRow = projected.rows.find(r => r.accountId === row.accountId);
        if (expectedOpen.includes(row.accountId)) {
          assert.deepEqual(projectedRow.opening, { value: row.value, randomness: row.r });
          assert((await slot.locator(".institutional-slot-opening").textContent()).includes(row.r));
        } else {
          assert.equal(projectedRow.opening, null);
          assert(!(await slot.innerHTML()).includes(row.r), "sealed slots must omit their secret masks from the DOM");
          assert.doesNotMatch(await slot.textContent(), /Received|Sent|No money moved|Verify this opening/);
          assert.match(await slot.innerText(), /Amount hidden/);
        }
        assert.equal(Boolean(projectedRow.balanceOpening), row.accountId === viewerId);
        assert.equal(projectedRow.previousRandomness, undefined);
      }
    }
    await page.selectOption("#institutionalViewer", "5");
    assert.match(await page.locator(".institutional-payment-perspective").innerText(), /You received 125 EN/);
    assert.match(await page.locator(".institutional-payment-perspective").innerText(), /cannot tell whether other participants received money or received nothing/);
    await page.selectOption("#institutionalViewer", "8");
    assert.match(await page.locator('[data-payment-account="8"] .institutional-slot-amount').innerText(), /No money moved\s+0 EN/);
    assert.equal(await page.locator('[data-payment-account="3"]').getAttribute("data-open"), "false");
    await page.selectOption("#institutionalViewer", "auditor");
    assert.equal(await page.locator('[data-balance-account][data-open="true"]').count(), 10);
    assert.equal(await page.locator('[data-payment-account][data-open="true"]').count(), 6);
    assert.equal(await page.locator(".institutional-slot-balance").count(), 6);
    await page.selectOption("#institutionalViewer", "public");
    assert.equal(await page.locator(".institutional-slot-opening, .institutional-slot-balance").count(), 0, "switching back to public must remove all private openings");
    assert.deepEqual(await snapshot(), visibilitySnapshot, "switching perspectives must not alter transactions or balances");
    await page.goto(`${BASE_URL}/#/institutional/audit`);
    assert.equal(await page.locator('[data-balance-account][data-open="true"]').count(), 10);
    assert.equal(await page.locator('[data-payment-account][data-open="true"]').count(), 6);
    pass("sender opens every payment slot, other participants open only their own, and balance access stays separate");

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
    assert.equal(current.flow.institutional.accounts[4].balance, 2085);
    assert(await checkSupply());
    const saved = current.flow.institutional;
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, saved);
    await page.goto(`${BASE_URL}/#/institutional/chain`);
    const secondPayment = page.locator(`[data-payment-batch="${current.transactions.find(tx => tx.batch).id}"]`);
    const firstPayment = page.locator(`[data-payment-batch="${posted.id}"]`);
    await page.selectOption("#institutionalViewer", "3");
    assert.equal(await firstPayment.locator('[data-open="true"]').count(), 6);
    assert.equal(await secondPayment.locator('[data-open="true"]').count(), 1, "sending one payment does not grant all openings in another payment");
    assert.match(await secondPayment.locator(".institutional-payment-perspective").innerText(), /other amount can be inferred/);
    await page.selectOption("#institutionalViewer", "5");
    assert.equal(await firstPayment.locator('[data-open="true"]').count(), 1);
    assert.equal(await secondPayment.locator('[data-open="true"]').count(), 2);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.deepEqual(await page.evaluate(() => ({ page: document.documentElement.scrollWidth, viewport: innerWidth })), { page: 390, viewport: 390 });
    await page.selectOption("#institutionalViewer", "public");
    assert.equal(await page.locator('[data-payment-account][data-open="true"]').count(), 0);
    pass("per-transaction access follows changing sender/recipient roles for k=2 and k=6, including reload and mobile");

    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`${BASE_URL}/#/institutional/payment`);
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
    const beforeTopUp = (await snapshot()).flow.institutional.accounts[2];
    assert.notEqual(beforeTopUp.randomness, "0", "private payments introduce randomness");
    assert.equal(await act("fund", { actor: "owner", recipientId: 3, amount: 1 }), null);
    const afterTopUp = (await snapshot()).flow.institutional.accounts[2];
    assert.equal(afterTopUp.randomness, beforeTopUp.randomness, "minting preserves existing transfer randomness");
    assert.equal(afterTopUp.balance, beforeTopUp.balance + 1);
    assert(await checkSupply());
    assert.match(await act("post"), /Calculate the commitment batch first/);
    pass("insufficient balances, malformed sets, altered commitments/public inputs, and invalidated proofs cannot settle");

    await page.selectOption("#institutionalK", "6");
    await page.selectOption("#institutionalPayer", "5");
    await page.selectOption("#institutionalRecipient", "3");
    const recipients = [{ accountId: 3, amount: 75 }, { accountId: 1, amount: 125 }, { accountId: 2, amount: 200 }, { accountId: 4, amount: 250 }, { accountId: 6, amount: 350 }];
    for (let i = 0; i < recipients.length; i++) {
      if (i) await page.click("#institutionalAddRecipient");
      await page.selectOption(`[data-institutional-recipient="${i}"]`, String(recipients[i].accountId));
      await page.fill(`[data-institutional-recipient-amount="${i}"]`, String(recipients[i].amount));
    }
    assert.equal(await page.locator("#institutionalAddRecipient").isDisabled(), true);
    assert.equal(await page.locator('#institutionalK option[value="2"]').evaluate(el => el.disabled), true, "reducing batch size must not silently discard recipients");
    assert.equal(await page.locator('[data-institutional-recipient="1"] option[value="3"]').evaluate(el => el.disabled), true, "a bank cannot receive twice in the same batch");
    assert.equal(await page.locator('[data-institutional-recipient="1"] option[value="5"]').evaluate(el => el.disabled), true, "the payer cannot also be a recipient");
    assert.match(await page.locator("#institutionalPaymentDistribution").innerText(), /1,000 EN[\s\S]*5 recipients/);
    for (const amount of ["", "0", "-1", "1.5", "3000"]) {
      await page.fill("#institutionalAmount", amount);
      assert.equal(await page.locator('[data-action="calculate"]').isDisabled(), true);
    }
    await page.fill("#institutionalAmount", "75");
    assert.equal(await page.locator('[data-action="calculate"]').isDisabled(), false);
    assert.equal(await page.locator("#institutionalAmount").evaluate(el => el === document.activeElement), true);
    const multiInput = { payerId: 5, k: 6, accountIds: [1, 2, 3, 4, 5, 6], recipients };
    const beforeInvalidMulti = await snapshot();
    for (const invalid of [
      [], null, {}, [...recipients, { accountId: 7, amount: 1 }],
      [{ accountId: 5, amount: 1 }], [{ accountId: 7, amount: 1 }],
      [{ accountId: 1, amount: 100 }, { accountId: 1, amount: 50 }],
      [{ accountId: 1, amount: 0 }], [{ accountId: 1, amount: -10 }], [{ accountId: 1, amount: 1.5 }],
      [{ accountId: 1, amount: 2000 }, { accountId: 2, amount: 2000 }],
      [{ accountId: 1, amount: Number.MAX_SAFE_INTEGER }, { accountId: 2, amount: Number.MAX_SAFE_INTEGER }]
    ]) {
      assert(await act("calculate", { ...multiInput, recipients: invalid }));
      assert.deepEqual(await snapshot(), beforeInvalidMulti);
    }
    assert(await act("calculate", { ...multiInput, k: 2, accountIds: [3, 5] }));
    assert.equal(await act("freeze-user", { actor: "owner", accountId: 6 }), null);
    assert.match(await act("calculate", multiInput), /frozen/);
    assert.equal(await act("unfreeze-user", { actor: "owner", accountId: 6 }), null);
    pass("multi-recipient entry enforces capacity, unique banks, positive amounts, aggregate funds and trading restrictions");

    const beforeMulti = (await snapshot()).flow.institutional;
    await clickAndWait(page, '[data-action="calculate"]');
    let multi = (await snapshot()).flow.institutional.draft;
    assert.deepEqual(multi.recipients, recipients);
    assert.equal(multi.amount, 1000);
    assert.equal(multi.rows.find(row => row.accountId === 5).value, -1000);
    recipients.forEach(recipient => assert.equal(multi.rows.find(row => row.accountId === recipient.accountId).value, recipient.amount));
    assert.equal(multi.rows.filter(row => row.value < 0).length, 1);
    assert.equal(multi.rows.filter(row => row.value > 0).length, 5);
    assert.equal(multi.rows.reduce((sum, row) => sum + row.value, 0), 0);
    assert.deepEqual(await page.locator(".institutional-proof-roles strong").allTextContents(), ["1", "5"]);
    assert.match(await page.locator(".institutional-proof-context").innerText(), /1,000 EN to 5 recipients/);
    assert.doesNotMatch(await page.locator(".institutional-proof-roles").innerText(), /same amounts|no balance change/);
    assert.deepEqual((await snapshot()).flow.institutional.accounts, beforeMulti.accounts);
    await clickAndWait(page, '[data-action="prove"]');
    await page.evaluate(() => {
      const draft = window.__ENYGMA_DEMO__.engine.protocol("institutional").flow.institutional.draft;
      draft.recipients[0].amount += 1;
      draft.recipients[1].amount -= 1;
    });
    assert.match(await act("post"), /Payment inputs changed/, "redistributing the same total after proving must invalidate the proof");
    assert.equal(await act("calculate", multiInput), null);
    assert.equal(await act("prove"), null);
    await clickAndWait(page, '[data-action="post"]');
    const afterMulti = await snapshot(), multiTx = afterMulti.transactions.find(tx => tx.batch);
    afterMulti.flow.institutional.accounts.forEach((account, i) => {
      const credit = recipients.find(recipient => recipient.accountId === account.accountId)?.amount || 0;
      assert.equal(account.balance, beforeMulti.accounts[i].balance + (account.accountId === 5 ? -1000 : credit));
    });
    assert.equal(afterMulti.flow.institutional.totalSupplyAmount, beforeMulti.totalSupplyAmount);
    assert.deepEqual(afterMulti.flow.institutional.totalSupplyCommitment, beforeMulti.totalSupplyCommitment);
    assert(await checkSupply());
    pass("one proof atomically pays five different amounts, binds every allocation, and conserves the total supply");

    await page.goto(`${BASE_URL}/#/institutional/chain`);
    const multiCard = page.locator(`[data-payment-batch="${multiTx.id}"]`);
    for (const viewer of ["public", "5", "auditor", ...recipients.map(recipient => String(recipient.accountId)), "10"]) {
      await page.selectOption("#institutionalViewer", viewer);
      const all = viewer === "5" || viewer === "auditor", own = recipients.find(recipient => String(recipient.accountId) === viewer);
      assert.equal(await multiCard.locator('[data-payment-account][data-open="true"]').count(), all ? 6 : own ? 1 : 0);
      if (all) assert.match(await multiCard.locator(".institutional-payment-perspective").innerText(), /1,000 EN to 5 recipients/);
      else {
        assert.doesNotMatch(await multiCard.locator(".institutional-payment-perspective").innerText(), /5 recipients|1,000 EN/);
        if (own) assert.match(await multiCard.locator(".institutional-payment-perspective").innerText(), new RegExp(`You received ${own.amount} EN`));
      }
    }
    await page.reload();
    assert.deepEqual((await snapshot()).flow.institutional, afterMulti.flow.institutional);
    await page.goto(`${BASE_URL}/#/institutional/payment`);
    await clickAndWait(page, '[data-action="edit-payment"]');
    for (let i = 0; i < recipients.length; i++) {
      assert.equal(await page.inputValue(`[data-institutional-recipient="${i}"]`), String(recipients[i].accountId));
      assert.equal(await page.inputValue(`[data-institutional-recipient-amount="${i}"]`), String(recipients[i].amount));
    }
    await page.setViewportSize({ width: 390, height: 844 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.click('[data-institutional-remove-recipient="4"]');
    await page.click('[data-institutional-remove-recipient="3"]');
    for (let i = 0; i < 3; i++) await page.fill(`[data-institutional-recipient-amount="${i}"]`, String((i + 1) * 10));
    await clickAndWait(page, '[data-action="calculate"]');
    assert.deepEqual(await page.locator(".institutional-proof-roles strong").allTextContents(), ["1", "3", "2"]);
    for (const action of ["prove", "post"]) await clickAndWait(page, `[data-action="${action}"]`);
    multi = (await snapshot()).flow.institutional.draft;
    assert.equal(multi.amount, 60);
    assert.equal(multi.rows.filter(row => row.value === 0).length, 2);
    assert(await checkSupply());
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    pass("multi-recipient privacy views, reload/edit, mobile entry, and partially filled batches retain correct recipient counts");
    await context.close();
  } finally { await browser.close(); }
  console.log(`\n${passed} institutional checks passed.`);
})().catch(error => { console.error(error); process.exit(1); });

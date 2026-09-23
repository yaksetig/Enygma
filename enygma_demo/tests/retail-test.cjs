const assert = require("assert");
const { chromium, launchOpts, freshPage, clickAndWait, setupProtocol, BASE_URL } = require("./_env.cjs");
let passed = 0;
const pass = label => { passed++; console.log(`  PASS  ${label}`); };
(async () => {
  const browser = await chromium.launch({ ...launchOpts, headless: true });
  try {
    const { context, page } = await freshPage(browser, "retail");
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    const crypto = await page.evaluate(async () => {
      const { poseidon } = await import("/js/institutional-crypto.js");
      const { noteCommitment, retailTree } = await import("/js/retail.js");
      return { hash4: String(poseidon([1, 2, 3, 4])), note: BigInt(noteCommitment("12345", "67890", 100)).toString(), root: BigInt(retailTree({ leaves: [] }).root).toString() };
    });
    // Independent circomlibjs vectors; zero leaf is keccak256("ZkDvp") mod BN254 Fr.
    assert.equal(crypto.hash4, "18821383157269793795438455681495246036402687001665670618754263018637548127333");
    assert.equal(crypto.note, "15241344517819359587650128474647003468021671710377477845515685924075604051790");
    assert.equal(crypto.root, "10201237349411555912925030596934589346435566232504971402396421291692387493081");
    pass("four-input note commitments and depth-8 Poseidon tree match independent protocol vectors");
    await setupProtocol(page, "retail");
    const state = () => page.evaluate(() => window.__ENYGMA_DEMO__.state().protocols.retail);
    const act = (action, payload = {}) => page.evaluate(async ({ action, payload }) => {
      try { await window.__ENYGMA_DEMO__.engine.executeProtocolAction("retail", action, payload); return null; } catch(e) { return e.message; }
    }, { action, payload });
    await page.goto(`${BASE_URL}/#/retail/shielding`);
    assert.equal(await page.locator(".retail-tree-svg").count(), 1);
    assert.equal(await page.locator('[data-traffic]').count(), 1);
    assert(!/Network traffic/.test(await page.locator(".scenario-progress").innerText()));
    assert.match(await act("shield", { amount: 100, count: 2 }), /exceed your public/);
    await clickAndWait(page, '[data-action="mint-cash"]');
    await page.selectOption("#retailShieldCount", "3");
    await page.fill("#retailShieldAmount", "120");
    await clickAndWait(page, '[data-action="shield"]');
    let p = await state();
    assert.equal(p.flow.retail.publicBalance, 640);
    assert.equal(p.notes.length, 3);
    assert.equal(p.leaves.length, 3);
    assert.equal(new Set(p.notes.map(n => n.salt)).size, 3);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 3);
    assert.equal(await page.locator('.retail-note-select').count(), 3);
    assert.equal(p.trees.USD.depth, 8);
    await page.selectOption("#retailShieldCount", "1");
    await page.fill("#retailShieldAmount", "65");
    await clickAndWait(page, '[data-action="shield"]');
    p = await state();
    assert.deepEqual(p.notes.map(n => n.amount), [120, 120, 120, 65]);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 4);
    const recomputed = await page.evaluate(async () => {
      const { noteCommitment } = await import('/js/retail.js');
      return window.__ENYGMA_DEMO__.state().protocols.retail.notes.every(n => noteCommitment(n.spendPublicKey, n.salt, n.amount) === n.commitment);
    });
    assert(recomputed);
    pass("batch shielding and repeated deposits retain every distinct note, opening and leaf");

    await page.goto(`${BASE_URL}/#/retail/private-tags`);
    await clickAndWait(page, '[data-action="configure-tags"]');
    assert.equal(await page.locator('[data-retail-recipient="1"]').isDisabled(), true);
    assert.match(await page.locator('[data-retail-recipient="1"]').innerText(), /Channel established/);
    assert.equal(await page.locator('[data-action="configure-tags"]').isDisabled(), true);
    const established = await state();
    for (const mode of ['full', 'subset', 'rift', 'none']) {
      assert.match(await act('configure-tags', { recipientIndex: 1, mode }), /already established/);
    }
    const afterDuplicates = await state();
    assert.deepEqual(afterDuplicates.flow.retail.channels, established.flow.retail.channels);
    assert.deepEqual(afterDuplicates.transactions, established.transactions);
    assert.deepEqual(afterDuplicates.ledger, established.ledger);
    pass("established peers are disabled and duplicate channel requests cannot publish in any privacy mode");
    await page.click('[data-retail-recipient="2"]');
    await page.click('[data-retail-tag-mode="subset"]');
    await clickAndWait(page, '[data-action="configure-tags"]');
    p = await state();
    assert.equal(p.flow.retail.channels.length, 2);
    assert.deepEqual(p.flow.retail.channels.map(c => c.recipientPartyId), ["party-1", "party-2"]);
    assert.equal(await page.locator('[data-channel-record]').count(), 2);
    assert.equal(await page.locator('.retail-channel-map path.established').count(), 2);
    assert.equal(await page.locator('[data-retail-tag-mode="full"]').isEnabled(), true);
    await page.reload();
    assert.equal(await page.locator('[data-retail-recipient="1"]').isDisabled(), true);
    assert.equal(await page.locator('[data-retail-recipient="2"]').isDisabled(), true);
    assert.equal(await page.locator('[data-retail-recipient="3"]').isEnabled(), true);
    assert.equal(await page.locator('[data-action="configure-tags"]').isDisabled(), true);
    pass("multiple independent private channels remain selectable after publishing each request");

    await page.goto(`${BASE_URL}/#/retail/payment`);
    assert.equal(await page.locator('#retailPaymentRecipient option').count(), 2);
    const channel1 = p.flow.retail.channels[0], channel2 = p.flow.retail.channels[1];
    await page.selectOption('#retailPaymentRecipient', channel1.id);
    await page.selectOption('#retailPaymentNote', p.notes[3].id);
    await page.fill('#retailPaymentAmount', '40');
    await clickAndWait(page, '[data-action="payment"]');
    p = await state();
    const first = p.transactions.find(t => t.type === 'payment');
    assert.equal(first.to, 'party-1');
    assert.equal(first.tagChannelId, channel1.id);
    assert.deepEqual(first.outputs.map(o => o.amount), [40, 25]);
    assert.equal(p.notes[3].status, 'spent');
    assert.equal(p.leaves.length, 6);
    assert.equal(first.verified, true);
    assert.equal(first.tagWindow.length, 3);
    assert.equal(first.privateTag, first.tagWindow[0].tag);
    assert.deepEqual(first.channelEncryptedFields, ['amount', 'token_id', 'salt']);
    const membership = await page.evaluate(async tx => {
      const { poseidon } = await import('/js/institutional-crypto.js');
      const p = window.__ENYGMA_DEMO__.state().protocols.retail;
      const input = p.notes.find(n => n.id === tx.inputNoteId);
      let hash = BigInt(input.commitment), index = input.leafIndex;
      for (const sibling of tx.siblings) { hash = index & 1 ? poseidon([sibling, hash]) : poseidon([hash, sibling]); index >>= 1; }
      return hash === BigInt(tx.root);
    }, first);
    assert(membership);
    assert.match(await act('payment', { amount: 5, channelId: channel1.id, noteId: first.inputNoteId }), /unspent note/);
    assert.match(await act('payment', { amount: 1000, channelId: channel1.id, noteId: p.notes[0].id }), /unspent note/);
    assert.equal((await state()).leaves.length, 6);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 5);
    pass("a selected input produces exact recipient and change outputs, with membership and double-spend guards");

    await page.selectOption('#retailPaymentRecipient', channel2.id);
    await page.selectOption('#retailPaymentNote', p.notes[0].id);
    await page.fill('#retailPaymentAmount', '120');
    await clickAndWait(page, '[data-action="payment"]');
    p = await state();
    const second = p.transactions.find(t => t.type === 'payment');
    assert.equal(second.to, 'party-2');
    assert.deepEqual(second.outputs.map(o => o.amount), [120, 0]);
    assert.equal(p.leaves.length, 8);
    assert.notEqual(first.nullifier, second.nullifier);
    assert.equal(await page.locator('#retailPaymentNote option').count(), 3); // two untouched 120 notes and 25 change; no zero note.
    const outstanding = p.notes.filter(n => n.status === 'unspent').reduce((sum,n) => sum+n.amount,0);
    assert.equal(outstanding + p.flow.retail.publicBalance, 1000);
    pass("subsequent payments can choose another channel and conserve funds, including exact-spend zero change");

    await page.goto(`${BASE_URL}/#/retail/chain`);
    await page.selectOption('#retailViewer', 'public');
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 0);
    assert.match(await page.locator('[data-leaf-inspector]').innerText(), /Owner and amount hidden/);
    await page.selectOption('#retailViewer', 'party-1');
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 0);
    await page.goto(`${BASE_URL}/#/retail/scan`);
    await page.selectOption('#retailScanPayment', first.id);
    await clickAndWait(page, '[data-action="scan"]');
    assert.equal((await state()).leaves.length, 8);
    assert.match(await page.locator('.retail-recovered').innerText(), /40 USD added to Atlas Bank/);
    assert.equal(await page.locator('.scan-match').count(), 1);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 1);
    await page.selectOption('#retailScanPayment', second.id);
    assert.equal(await page.locator('.retail-recovered').count(), 0);
    await clickAndWait(page, '[data-action="scan"]');
    assert.match(await page.locator('.retail-recovered').innerText(), /120 USD added to Boreal Markets/);
    await page.goto(`${BASE_URL}/#/retail/chain`);
    await page.selectOption('#retailViewer', 'party-1');
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 1);
    assert.match(await page.locator('.retail-viewer').innerText(), /40 USD/);
    await page.reload();
    p = await state();
    assert.equal(p.flow.retail.channels.length, 2);
    assert.equal(p.notes.length, 8);
    assert.equal(p.transactions.filter(t => t.scanned).length, 2);
    pass("scanning selects the correct payment and opens only that wallet’s leaves without growing the tree");

    await page.goto(`${BASE_URL}/#/retail/shielding`);
    await page.check('[data-traffic]');
    await page.waitForTimeout(3550);
    await page.uncheck('[data-traffic]');
    p = await state();
    assert.equal(p.leaves.length, 10);
    assert.equal(p.transactions[0].type, 'background-payment');
    assert(p.transactions[0].inputNoteId);
    const stable = p.leaves.length;
    await page.waitForTimeout(3550);
    assert.equal((await state()).leaves.length, stable);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 6); // 4 funding + 2 change (including spent).
    pass("network traffic starts before payments, funds background wallets and never hides your own notes");

    await page.setViewportSize({ width: 390, height: 844 });
    for (const screen of ['shielding', 'private-tags', 'payment', 'scan', 'chain']) {
      await page.goto(`${BASE_URL}/#/retail/${screen}`);
      const sizes = await page.evaluate(() => ({ width: innerWidth, page: document.documentElement.scrollWidth }));
      assert(sizes.page <= sizes.width + 1, `${screen} overflows at ${sizes.page}px`);
      if (screen !== 'private-tags') assert.equal(await page.locator('.retail-tree-svg').count(), 1);
    }
    assert.deepEqual(errors, []);
    pass("all Retail screens retain their visuals on mobile without page overflow or console errors");
    await page.goto(`${BASE_URL}/#/retail/private-tags`);
    for (let peer = 3; peer < 10; peer++) {
      await page.click(`[data-retail-recipient="${peer}"]`);
      await clickAndWait(page, '[data-action="configure-tags"]');
    }
    assert.equal((await state()).flow.retail.channels.length, 9);
    assert.equal(await page.locator('[data-retail-recipient]:disabled').count(), 9);
    assert.equal(await page.locator('[data-action="configure-tags"]').isDisabled(), true);
    assert.match(await page.locator('[data-action="configure-tags"]').innerText(), /All channels established/);
    await page.goto(`${BASE_URL}/#/retail/payment`);
    assert.equal(await page.locator('#retailPaymentRecipient option').count(), 9);
    assert.equal(await page.locator('[data-action="payment"]').isEnabled(), true);
    pass("connecting every peer closes channel creation while all existing channels remain available for payments");
    await context.close();
  } finally { await browser.close(); }
  console.log(`\n${passed} Retail checks passed.`);
})().catch(error => { console.error(error); process.exit(1); });

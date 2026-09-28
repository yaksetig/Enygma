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
    assert.equal(await page.locator('[data-action="mint-cash"]').innerText(), "Mint USD");
    assert(await page.evaluate(() => document.querySelector('[data-action="mint-cash"]').getBoundingClientRect().bottom < document.querySelector('.retail-shield-scene').getBoundingClientRect().top));
    const totalInput = page.locator("#retailShieldAmount"), originalInput = await totalInput.elementHandle();
    await totalInput.fill("");
    await totalInput.pressSequentially("2000");
    assert.equal(await totalInput.inputValue(), "2000");
    assert(await originalInput.evaluate(el => el.isConnected && el === document.activeElement), "typing must keep the same input node focused");
    await totalInput.evaluate(el => el.setSelectionRange(1, 3));
    await totalInput.pressSequentially("5");
    assert.equal(await totalInput.inputValue(), "250");
    assert.equal(await totalInput.evaluate(el => el.selectionStart), 2);
    await totalInput.press("Backspace");
    assert.equal(await totalInput.inputValue(), "20");
    await totalInput.fill("200");
    assert.deepEqual(await page.locator('[data-retail-shield-note]').evaluateAll(els => els.map(el => el.value)), ["100", "100"], "changing the total must not rewrite custom note inputs");
    await page.fill("#retailMintAmount", "001000");
    assert.equal(await page.locator("#retailMintAmount").inputValue(), "001000");
    assert.match(await act("shield", { totalAmount: 200, amounts: [100, 100] }), /exceed your public/);
    await clickAndWait(page, '[data-action="mint-cash"]');
    assert.equal(await page.locator("#retailMintAmount").inputValue(), "001000");
    assert.match((await state()).transactions[0].label, /minted 1000 USD/);
    await page.selectOption("#retailShieldCount", "3");
    assert.deepEqual(await page.locator('[data-retail-shield-note]').evaluateAll(els => els.map(el => el.value)), ["100", "100", ""]);
    await page.click('[data-retail-split-evenly]');
    assert.deepEqual(await page.locator('[data-retail-shield-note]').evaluateAll(els => els.map(el => el.value)), ["67", "67", "66"]);
    await page.selectOption("#retailShieldCount", "4");
    await page.click('[data-retail-split-evenly]');
    assert.deepEqual(await page.locator('[data-retail-shield-note]').evaluateAll(els => els.map(el => el.value)), ["50", "50", "50", "50"]);
    await page.selectOption("#retailShieldCount", "3");
    for (const [i, amount] of ["120", "50", "30"].entries()) await page.fill(`#retailShieldNote${i}`, amount);
    for (const bad of ["", "0", "-1", "1.5", "1e2", "1,000", "29", "31"]) {
      await page.fill("#retailShieldNote2", bad);
      assert.equal(await page.locator("#retailShieldNote2").inputValue(), bad, "validation must not reformat the input");
      assert.equal(await page.locator('[data-action="shield"]').isDisabled(), true);
    }
    await page.fill("#retailShieldNote2", "30");
    assert.match(await page.locator("#retailShieldSummary").innerText(), /120 \+ 50 \+ 30 = 200 USD/);
    assert.deepEqual(await page.locator('.retail-mini-note strong').allTextContents(), ["120 USD", "50 USD", "30 USD"]);
    const beforeInvalidPlans = await state();
    for (const payload of [
      { totalAmount: 200, amounts: [120, 50, 31] },
      { totalAmount: 200, amounts: [120, 80, ""] },
      { totalAmount: 200, amounts: [120, 80, 0] },
      { totalAmount: 200, amounts: [120, 81, -1] },
      { totalAmount: 200, amounts: [120, 50, "30.5"] },
      { totalAmount: 200, amounts: [120, 50, "3e1"] },
      { totalAmount: 200, amounts: [] },
      { totalAmount: 200, amounts: [40, 40, 40, 40, 40] },
      { totalAmount: 1001, amounts: [1001] },
      { totalAmount: Number.MAX_SAFE_INTEGER, amounts: [Number.MAX_SAFE_INTEGER, 1] }
    ]) assert(await act("shield", payload));
    assert.deepEqual(await state(), beforeInvalidPlans, "invalid splits must not partially debit funds or insert notes");
    pass("minting precedes shielding, numeric editing preserves the caret, and custom splits validate before any deposit");
    await clickAndWait(page, '[data-action="shield"]');
    let p = await state();
    assert.equal(p.flow.retail.publicBalance, 800);
    assert.equal(p.notes.length, 3);
    assert.deepEqual(p.notes.map(n => n.amount), [120, 50, 30]);
    assert.equal(p.leaves.length, 3);
    assert.equal(new Set(p.notes.map(n => n.salt)).size, 3);
    assert.equal(await page.locator('.retail-tree-node[data-owned="true"]').count(), 3);
    assert.equal(await page.locator('.retail-note-select').count(), 3);
    assert.equal(p.trees.USD.depth, 8);
    await page.selectOption("#retailShieldCount", "1");
    await page.fill("#retailShieldAmount", "65");
    await page.fill("#retailShieldNote0", "65");
    await clickAndWait(page, '[data-action="shield"]');
    p = await state();
    assert.deepEqual(p.notes.map(n => n.amount), [120, 50, 30, 65]);
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
    assert.equal(await page.locator('#retailPaymentNote option').count(), 3); // untouched 50 and 30 notes, plus 25 change; no zero note.
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
    await page.fill('#retailShieldAmount', '00200');
    await page.locator('#retailShieldAmount').evaluate(el => el.setSelectionRange(1, 3));
    await page.waitForTimeout(3550);
    assert.deepEqual(await page.locator('#retailShieldAmount').evaluate(el => ({ value: el.value, start: el.selectionStart, end: el.selectionEnd, focused: el === document.activeElement })), { value: '00200', start: 1, end: 3, focused: true }, "background updates must preserve the typed text and selection");
    await page.uncheck('[data-traffic]');
    p = await state();
    assert.equal(p.leaves.length, 10);
    assert.equal(p.transactions[0].type, 'background-payment');
    assert(p.transactions[0].inputNoteId);
    const stable = p.leaves.length;
    await page.waitForTimeout(3550);
    assert.equal((await state()).leaves.length, stable);
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '8');
    await page.click('[data-retail-tree-page="0"]');
    assert.equal(await page.locator('#retailTreeFollow').isChecked(), false);
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

    const growTree = target => page.evaluate(async target => {
      const engine = window.__ENYGMA_DEMO__.engine, p = engine.protocol('retail');
      const pause = engine.pause, persist = engine.persist;
      // Execute real deposits, omitting animation delays and intermediate renders.
      engine.pause = async () => {};
      engine.persist = () => {};
      try {
        while (p.leaves.length < target) {
          const count = Math.min(4, target - p.leaves.length);
          await engine.executeProtocolAction('retail', 'shield', { totalAmount: count, amounts: Array(count).fill(1) });
        }
      } finally { engine.pause = pause; engine.persist = persist; engine.persist(); }
    }, target);
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.goto(`${BASE_URL}/#/retail/shielding`);
    await growTree(32);
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '0', 'new deposits must not change a manually selected group');
    await page.check('#retailTreeFollow');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '24');
    assert.equal(await page.locator('.retail-tree-node[data-retail-leaf]').count(), 8);
    assert(await page.locator('.retail-tree-panel').evaluate(el => el.clientWidth > 1100), 'the explorer must use the full desktop width');
    assert(await page.locator('.retail-tree-scroll').evaluate(el => el.scrollWidth <= el.clientWidth), 'eight readable leaves should fit the desktop explorer');
    const checkTreeHashes = () => page.evaluate(() => {
      const p = window.__ENYGMA_DEMO__.state().protocols.retail;
      return document.querySelector('.retail-tree-panel').dataset.merkleRoot === p.trees.USD.root && [...document.querySelectorAll('[data-node-hash]')].every(el => el.dataset.nodeHash === p.trees.USD.levels[Number(el.dataset.treeLevel)][Number(el.dataset.treeIndex)]);
    });
    assert(await checkTreeHashes(), 'a later group must use global subtree indexes and the actual shared root');
    await page.click('[data-retail-tree-page="8"]');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '8');
    assert(await checkTreeHashes());
    await page.click('[data-retail-tree-move="next"]');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '16');
    await page.click('[data-retail-tree-move="previous"]');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '8');
    const oldNote = (await state()).notes[0];
    await page.click(`.retail-note-select[data-retail-leaf="${oldNote.leafId}"]`);
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '0');
    await page.locator('#retailTreeLeaf0').focus();
    await page.locator('#retailTreeLeaf0').press('Enter');
    assert.match(await page.locator('[data-leaf-inspector]').innerText(), /120 USD/);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'retailTreeLeaf0');
    await growTree(256);
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '0');
    assert.equal(await page.locator('[data-retail-tree-page]').count(), 32);
    await page.check('#retailTreeFollow');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '248');
    assert.equal(await page.locator('.retail-tree-node[data-retail-leaf]').count(), 8);
    assert.equal(await page.locator('#retailTreeLeaf255').count(), 1);
    assert.equal(await page.locator('#retailTreeNext').isDisabled(), true);
    assert(await checkTreeHashes());
    assert(await page.locator('.retail-tree-svg').evaluate(el => el.getBoundingClientRect().width <= 1100), 'the drawing must stay bounded at full capacity');
    await page.goto(`${BASE_URL}/#/retail/chain`);
    await page.selectOption('#retailViewer', 'public');
    assert.equal(await page.locator('.owned-group, .retail-tree-node[data-owned="true"]').count(), 0);
    assert.match(await page.locator('[data-leaf-inspector]').innerText(), /Owner and amount hidden/);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.click('[data-retail-tree-page="0"]');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    assert(await page.locator('.retail-tree-map').evaluate(el => el.clientHeight <= 128));
    await page.check('#retailTreeFollow');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '254');
    assert.equal(await page.locator('.retail-tree-node[data-retail-leaf]').count(), 2);
    assert(await checkTreeHashes());
    assert(await page.locator('.retail-tree-scroll').evaluate(el => el.scrollWidth <= el.clientWidth));
    assert(await page.locator('.retail-tree-map').evaluate(el => { const selected = el.querySelector('[aria-current="true"]').getBoundingClientRect(), bounds = el.getBoundingClientRect(); return selected.top >= bounds.top && selected.bottom <= bounds.bottom; }), 'the current group must remain visible in the compact mobile map');
    await page.setViewportSize({ width: 900, height: 1000 });
    await page.waitForFunction(() => document.querySelector('.retail-tree-panel').dataset.treePageSize === '4');
    assert.equal(await page.locator('.retail-tree-panel').getAttribute('data-tree-start'), '252');
    assert.equal(await page.locator('.retail-tree-node[data-retail-leaf]').count(), 4);
    assert(await checkTreeHashes());
    assert.deepEqual(errors, []);
    pass("full-width explorer browses all 256 real leaves with correct subtree hashes, stable inspection, keyboard access and private views");
    await context.close();
  } finally { await browser.close(); }
  console.log(`\n${passed} Retail checks passed.`);
})().catch(error => { console.error(error); process.exit(1); });

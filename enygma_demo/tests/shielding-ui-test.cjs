const assert = require("assert");
const { chromium, launchOpts, freshPage, setupProtocol, clickAndWait, BASE_URL } = require("./_env.cjs");

let passed = 0;
const pass = name => { passed++; console.log(`  PASS  ${name}`); };
const go = (page, route) => page.goto(`${BASE_URL}/#/${route}`);
const snapshot = (page, id) => page.evaluate(id => window.__ENYGMA_DEMO__.state().protocols[id], id);

async function checkTree(page, id, asset) {
  const p = await snapshot(page, id), tree = p.trees[asset];
  const panel = page.locator(`[data-tree-asset="${asset}"]`);
  assert.equal(await panel.getAttribute("data-merkle-root"), tree.root);
  const nodes = await panel.locator("[data-node-hash]").evaluateAll(nodes => nodes.map(node => ({
    level: Number(node.dataset.treeLevel), index: Number(node.dataset.treeIndex), hash: node.dataset.nodeHash
  })));
  for (const node of nodes) assert.equal(node.hash, tree.levels[node.level][node.index]);
  return panel;
}

(async () => {
  const browser = await chromium.launch({ ...launchOpts, headless: true });
  try {
    const { context, page } = await freshPage(browser, "dvp", { width: 1440, height: 1100 });
    await setupProtocol(page, "dvp");
    await go(page, "dvp/seller-holdings");
    await page.fill("#dvpMintSecurityAmount", "20000");
    await clickAndWait(page, '[data-action="mint-security"]');
    await go(page, "dvp/seller-asset");
    const amount = page.locator("#dvpShieldSecurityAmount"), submit = page.locator('[data-action="shield-security"]');
    for (const value of ["", "-1", "2.5", "20001", "1e3"]) {
      await amount.fill(value);
      assert.equal(await amount.inputValue(), value);
      assert(await submit.isDisabled());
    }
    await amount.fill("0013");
    assert(await submit.isEnabled());
    await clickAndWait(page, '[data-action="shield-security"]');
    let p = await snapshot(page, "dvp"), asset = p.flow.securityAssetId;
    assert.equal(p.notes[0].amount, 13);
    assert.equal(await amount.inputValue(), "0013");
    assert.equal(await page.locator(".network-registry").count(), 0);
    assert.equal(await page.locator(".scenario-aside").count(), 0);
    await checkTree(page, "dvp", asset);
    pass("shielding validates raw input without changing typed text and creates the exact chosen note");

    await page.evaluate(async () => {
      const engine = window.__ENYGMA_DEMO__.engine;
      for (let i = 0; i < 16; i++) await engine.executeProtocolAction("dvp", "shield-security", { amount: 20 + i });
    });
    let tree = await checkTree(page, "dvp", asset);
    assert.equal(await tree.getAttribute("data-tree-start"), "16");
    await page.click('[data-note-tree-move="previous"]');
    assert.equal(await tree.getAttribute("data-tree-start"), "8");
    await page.click('[data-note-tree-page="0"]');
    await page.locator('[data-note-leaf][data-tree-index="0"]').focus();
    await page.keyboard.press("Enter");
    assert.match(await page.locator("[data-leaf-inspector]").innerText(), /13 RAYLS-BOND-2030/);
    await page.locator('[data-disclosure="shield-calculation"] summary').click();
    await page.locator(".note-inventory-item summary").first().click();
    const ownNotes = (await snapshot(page, "dvp")).notes;
    await page.check("[data-traffic]");
    await amount.fill("250");
    await amount.evaluate(el => el.setSelectionRange(1, 2));
    await page.waitForFunction(() => window.__ENYGMA_DEMO__.engine.protocol("dvp").leaves.length > 17);
    await page.evaluate(() => window.__ENYGMA_DEMO__.engine.setTraffic("dvp", false));
    assert.equal(await amount.inputValue(), "250");
    assert.deepEqual(await amount.evaluate(el => [el === document.activeElement, el.selectionStart, el.selectionEnd]), [true, 1, 2]);
    assert.equal(await tree.getAttribute("data-tree-start"), "0");
    assert(await page.locator('[data-disclosure="shield-calculation"]').evaluate(el => el.open));
    assert(await page.locator(".note-inventory-item").first().evaluate(el => el.open));
    assert.deepEqual((await snapshot(page, "dvp")).notes, ownNotes);
    await page.check("[data-note-tree-follow]");
    await page.locator('[data-note-leaf][data-owned="false"]').last().click();
    assert.match(await page.locator("[data-leaf-inspector]").innerText(), /Sealed note commitment/);
    assert.doesNotMatch(await page.locator("[data-leaf-inspector]").innerText(), /PRIVATE WALLET OPENING/);
    await checkTree(page, "dvp", asset);
    pass("tree navigation and keyboard inspection preserve private openings, focus, and pinned branches during live traffic");

    for (const [width, size] of [[1440, 8], [820, 4], [390, 2]]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.waitForFunction(size => document.querySelector(".commitment-explorer").dataset.treePageSize === String(size), size);
      await page.click('[data-note-tree-page="0"]');
      tree = await checkTree(page, "dvp", asset);
      assert.equal(await tree.locator(".leaf-node").count(), size);
      await page.click('[data-note-tree-move="next"]');
      assert.equal(await tree.getAttribute("data-tree-start"), String(size));
      await checkTree(page, "dvp", asset);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    }
    pass("desktop, tablet, and phone tree groups use actual subtree hashes and stay inside the page");

    await go(page, "dvp/buyer-holdings");
    await page.fill("#dvpMintCashAmount", "12000000");
    await clickAndWait(page, '[data-action="mint-cash"]');
    await go(page, "dvp/buyer-cash");
    await page.fill("#dvpShieldCashAmount", "2462500");
    await clickAndWait(page, '[data-action="shield-cash"]');
    p = await snapshot(page, "dvp");
    assert.equal(p.trees.USD.leafIds.length, 1);
    assert.equal(p.trees[asset].leafIds.length, 18);
    await checkTree(page, "dvp", "USD");
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 1100 });
      await page.waitForFunction(width => innerWidth === width, width);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      assert(await page.locator(".retail-mini-note strong").evaluate(el => el.scrollWidth <= el.clientWidth && el.getBoundingClientRect().right < el.parentElement.getBoundingClientRect().right));
    }
    await go(page, "dvp/seller-asset");
    await page.click('[data-note-tree-page="0"]');
    assert.equal(await page.locator('[data-tree-asset="USD"]').count(), 0);
    await go(page, "dvp/terms-proposal");
    assert.equal(await page.locator(".network-registry").count(), 0);
    pass("cash and securities retain separate roots and readable large amounts without a repeated registry");

    await setupProtocol(page, "auctions");
    await go(page, "auctions/auctioneer");
    await clickAndWait(page, '[data-action="auctioneer"]');
    await go(page, "auctions/auctioneer-registration");
    await clickAndWait(page, '[data-action="register-auctioneer"]');
    await go(page, "auctions/mint-asset");
    await clickAndWait(page, '[data-action="mint-nft"]');
    await go(page, "auctions/list-asset");
    await clickAndWait(page, '[data-action="list-asset"]');
    await go(page, "auctions/fund-bidder");
    await page.fill("#auctionMintCashAmount", "20000");
    await clickAndWait(page, '[data-action="mint-cash"]');
    await go(page, "auctions/shield-bidder");
    for (const value of ["70", "930"]) {
      await page.fill("#auctionShieldCashAmount", value);
      await clickAndWait(page, '[data-action="shield-cash"]');
    }
    p = await snapshot(page, "auctions");
    const fundingNotes = p.notes.filter(note => note.assetId === "USD");
    assert.deepEqual(fundingNotes.map(note => note.amount), [70, 930]);
    assert.equal(await page.locator(".network-registry").count(), 0);
    assert.equal(await page.locator("[data-traffic]").count(), 0);
    await checkTree(page, "auctions", "USD");
    await page.setViewportSize({ width: 390, height: 844 });
    await page.locator(".note-inventory-item summary").first().click();
    assert.match(await page.locator(".note-inventory-item").first().innerText(), new RegExp(fundingNotes[0].commitment));
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.reload();
    assert.deepEqual((await snapshot(page, "auctions")).notes, p.notes);
    await checkTree(page, "auctions", "USD");
    pass("auction shielding shares the explorer and expandable note cards while preserving each deposited note across reload");
    await context.close();
  } finally { await browser.close(); }
  console.log(`\n${passed} shared shielding checks passed.`);
})().catch(error => { console.error(error); process.exitCode = 1; });

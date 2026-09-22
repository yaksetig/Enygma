const path = require('path');
const { pathToFileURL } = require('url');
const { chromium } = require('../../enygma_demo/node_modules/playwright');

let failures = 0;
const pass = (condition, message) => {
  console.log(`${condition ? '  PASS' : '  FAIL'}  ${message}`);
  if (!condition) failures++;
};
const state = page => page.evaluate(() => window.__ENYGMA_PRESENTATION.state());
const waitIdle = page => page.waitForFunction(() => {
  const api = window.__ENYGMA_PRESENTATION;
  return api && !api.state().busy;
}, null, { timeout: 20000 });

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

  const url = pathToFileURL(path.join(__dirname, 'index.html')).href;
  await page.goto(url);
  await page.evaluate(() => sessionStorage.clear());
  await page.reload();
  await page.waitForFunction(() => window.__ENYGMA_PRESENTATION);

  console.log('\n--- registry and identity ---');
  pass(await page.locator('#registry .trow[data-row]').count() === 8,
       'eight aligned registry/bitmap rows render before registration');
  await page.click('#register');
  await page.waitForFunction(() => window.__ENYGMA_PRESENTATION.state().registered,
                             null, { timeout: 10000 });
  let s = await state(page);
  pass(s.users.length === 8 && s.users.every((u, i) => u.registered && u.index === i),
       'all eight parties register in deterministic row order');
  pass(s.users.every(u => u.pkSpend.length === 64 && u.pkView.length === 128),
       'each public registry row contains spend and view public-key material');
  pass(s.notes.length === 3 && s.notes.every(n => n.amount === 100) && s.leaves.length === 3,
       'Alice, Bob and Charlie receive one private 100-token note each');
  pass(await page.locator('#identity [data-reveal]').count() === 2,
       'only the active party exposes two secret reveal controls');

  console.log('\n--- channel privacy modes ---');
  const expected = { none: 1, subset: 3, rift: 7, full: 8 };
  for (const [mode, count] of Object.entries(expected)) {
    await page.click(`[data-mode="${mode}"]`);
    await page.waitForFunction(m => window.__ENYGMA_PRESENTATION.state().mode === m &&
                                    window.__ENYGMA_PRESENTATION.state().preview,
                               mode);
    s = await state(page);
    pass(s.preview.bits.length === count && s.preview.bits.includes(1),
         `${mode} produces ${count} candidate row(s) and includes Bob`);
  }
  await page.click('[data-mode="subset"]');
  await page.waitForFunction(() => window.__ENYGMA_PRESENTATION.state().preview?.mode === 'subset');
  s = await state(page);
  pass(s.preview.bits.includes(2), 'Charlie is a decoy candidate in the Alice → Bob subset');
  const previewBitmap = s.preview.bitmap;
  await page.click('#open-channel');
  await waitIdle(page);
  s = await state(page);
  pass(s.channels.length === 1 && s.channels[0].bitmap === previewBitmap && s.channels[0].published,
       'the established directional channel retains the exact preview bitmap');

  console.log('\n--- payment, tag publication and scanning ---');
  await page.fill('#amount', '30');
  await page.click('#pay');
  await waitIdle(page);
  s = await state(page);
  const first = s.payments[0];
  pass(s.payments.length === 1 && first.amount === 30 && first.newLeaves.length === 2,
       'Alice → Bob payment inserts recipient and change commitments');
  pass(first.tx && first.tagTx && first.tag && first.nullifier && s.root,
       'payment receipt, separate tag transaction, nullifier and new root are present');

  await page.click('[data-actor="2"]');
  await page.click('#scan');
  await waitIdle(page);
  s = await state(page);
  pass(s.scanResults[0]?.kind === 'decoy',
       'Charlie attempts decapsulation and fails as the subset decoy');

  await page.click('[data-actor="1"]');
  await page.click('#scan');
  await waitIdle(page);
  s = await state(page);
  pass(s.scanResults[0]?.kind === 'matched' && /recomputed leaf/.test(s.scanResults[0].detail),
       'Bob decrypts the note and recomputes the destination commitment');

  await page.click('[data-actor="0"]');
  await page.selectOption('#pay-to', '1');
  await page.fill('#amount', '20');
  await page.click('#pay');
  await waitIdle(page);
  s = await state(page);
  pass(s.payments.length === 2 && s.payments[1].input === first.newLeaves[1],
       'a second payment reuses the channel and spends Alice’s evolved change note');

  console.log('\n--- actionable failures and responsive layout ---');
  await page.fill('#amount', '0');
  pass(/greater than zero/.test(await page.locator('#err-pay').innerText()),
       'invalid amounts produce an actionable error without mutation');
  const beforeFailure = (await state(page)).payments.length;
  await page.evaluate(() => window.__ENYGMA_PRESENTATION.setService('prover', false));
  await page.fill('#amount', '5');
  await page.evaluate(() => document.querySelector('#pay').click());
  s = await state(page);
  pass(/Prover unavailable/.test(await page.locator('#err-pay').innerText()) &&
       s.payments.length === beforeFailure,
       'dependency failure preserves session state');
  await page.evaluate(() => window.__ENYGMA_PRESENTATION.setService('prover', true));

  for (const width of [1440, 1080, 760]) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(100);
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth - document.documentElement.clientWidth);
    pass(overflow <= 1, `no horizontal overflow at ${width}px (${overflow}px)`);
  }
  pass(errors.length === 0, errors.length ? `page errors: ${errors.join(' | ')}` : 'no page or console errors');

  await browser.close();
  process.exit(failures ? 1 : 0);
})().catch(err => { console.error(err); process.exit(1); });

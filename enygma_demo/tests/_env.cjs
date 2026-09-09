const path = require("path");

function loadPlaywright() {
  const candidates = [process.env.PLAYWRIGHT_PATH, "playwright", "/opt/node22/lib/node_modules/playwright"].filter(Boolean);
  for (const candidate of candidates) {
    try { return require(candidate); } catch { /* Try the next supported location. */ }
  }
  throw new Error("Playwright not found. Run npm install in enygma_demo/.");
}

const { chromium } = loadPlaywright();
const launchOpts = process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {};
const BASE_URL = process.env.DEMO_URL || "http://127.0.0.1:4193";

async function freshPage(browser, route = "choose", viewport = { width: 1280, height: 900 }) {
  const context = await browser.newContext({ viewport, reducedMotion: "reduce" });
  const page = await context.newPage();
  await page.goto(`${BASE_URL}/#/${route}`);
  await page.evaluate(() => sessionStorage.clear());
  await page.reload();
  return { context, page };
}

async function clickAndWait(page, selector) {
  await page.click(selector);
  await page.waitForFunction(() => document.body.dataset.busy !== "true", null, { timeout: 15000 });
}

async function setupProtocol(page, id, all = true) {
  await page.goto(`${BASE_URL}/#/${id}`);
  for (const command of ["deploy", "auditor", "configure"]) await clickAndWait(page, `[data-command="${command}"]`);
  if (await page.locator('[data-command="identity-spend-secret"]').count()) {
    for (const command of ["identity-spend-secret", "identity-spend-public", "identity-view-secret", "identity-view-public"]) {
      await clickAndWait(page, `[data-command="${command}"]`);
    }
  }
  await clickAndWait(page, '[data-command="identity-confirm"]');
  if (all) {
    await clickAndWait(page, '[data-command="register-participant-keys"]');
    await clickAndWait(page, '[data-command="share-participant-key"]');
    await clickAndWait(page, '[data-command="register-others"]');
  }
}

module.exports = { chromium, launchOpts, BASE_URL, freshPage, clickAndWait, setupProtocol, root: path.join(__dirname, "..") };

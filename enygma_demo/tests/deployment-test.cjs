const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { chromium, launchOpts, freshPage, clickAndWait, root } = require("./_env.cjs");

const readRepo = file => fs.readFileSync(path.join(root, "..", file), "utf8");

// Read the actual deployment calls, including asset contracts and repeated instances.
function scriptContracts(source, id) {
  if (id === "institutional") {
    return [...source.matchAll(/path\.join\(CONTRACTS_DIR, "[^"]+", "([^"]+)\.json"\)/g)].map(match => match[1]);
  }
  const calls = /deployContract(?:WithArgs|WithLibraries|FromPath)?\(\s*client,\s*owner,\s*(?:"([^"]+)"|filepath\.Join\([^)]*"([^"/]+)\.json"\s*\))/g;
  return [...source.matchAll(calls)].map(match => (match[1] || match[2]).split("/").at(-1));
}

(async () => {
  const config = fs.readFileSync(path.join(root, "js/config.js"), "utf8");
  const { PROTOCOLS, initializationSteps } = await import(`data:text/javascript;base64,${Buffer.from(config).toString("base64")}`);
  const browser = await chromium.launch({ ...launchOpts, headless: true });
  try {
    for (const [id, suite] of Object.entries(PROTOCOLS)) {
      assert.deepEqual(suite.contracts.map(item => item.name), scriptContracts(readRepo(suite.deploymentSource), id), `${id}: deployment names and order match the script`);
      const instanceIds = suite.contracts.map(item => item.instance || item.name);
      assert.equal(new Set(instanceIds).size, instanceIds.length, `${id}: distinct contract instances`);
      suite.contracts.forEach((item, index) => {
        for (const dependency of [...(item.libraries || []), ...(item.constructorArgs || []).filter(arg => instanceIds.includes(arg))]) {
          assert(instanceIds.indexOf(dependency) < index, `${id}: ${dependency} is deployed before ${item.name}`);
        }
      });
      const initSource = readRepo(suite.initializationSource);
      if (id !== "institutional") {
        const actualMethods = [...initSource.matchAll(/(?:callContractMethod|callMethod)\(client,\s*auth,\s*\w+,\s*\w+,\s*"([^"]+)"/g)].map(match => match[1]);
        const methodRuns = methods => methods.filter((method, index) => method !== methods[index - 1]);
        assert.deepEqual(methodRuns(suite.initialization.map(step => step.method)), methodRuns(actualMethods), `${id}: initialization call order matches the script`);
      } else {
        assert.match(initSource, /inst\.Initialize\(/);
        assert.match(initSource, /inst\.AddVerifier\(/);
      }
      if (id === "retail" || id === "dvp") {
        const configPath = id === "retail" ? "enygma_retail_payments/enygmapayment.config.json" : "enygma_dvp/enygmadvp.config.json";
        const circuits = JSON.parse(readRepo(configPath)).circom.circuits.map(circuit => circuit.filename);
        assert.deepEqual(suite.initialization.filter(step => /VerificationKey$/.test(step.method)).map(step => step.args[0]), circuits, `${id}: verification keys follow protocol configuration`);
      }

      const { context, page } = await freshPage(browser, id);
      assert.deepEqual(await page.locator("[data-deployment-contract]").evaluateAll(cards => cards.map(card => card.dataset.deploymentContract)), instanceIds);
      await clickAndWait(page, '[data-command="deploy"]');
      const protocol = await page.evaluate(id => window.__ENYGMA_DEMO__.state().protocols[id], id);
      assert.equal(protocol.stage, 1);
      assert.deepEqual(protocol.contracts.map(item => item.id), instanceIds);
      assert.equal(protocol.deploymentActions.length, initializationSteps(id).length);
      assert.equal(new Set(protocol.contracts.map(item => item.address)).size, instanceIds.length);
      for (const deployed of protocol.contracts) {
        const spec = suite.contracts.find(item => (item.instance || item.name) === deployed.id);
        const resolve = value => protocol.contracts.find(item => item.id === value)?.address ?? value;
        assert.deepEqual(deployed.constructorArgs, (spec.constructorArgs || []).map(resolve));
        assert.deepEqual(deployed.libraries, Object.fromEntries((spec.libraries || []).map(name => [name, resolve(name)])));
      }
      assert.equal(new Set(protocol.ledger.map(item => item.id)).size, protocol.ledger.length);
      assert(protocol.ledger.every((item, index) => index === 0 || item.block < protocol.ledger[index - 1].block));
      await page.locator(".deployment-receipts > summary").click();
      assert.equal(await page.locator(".deployment-receipts tbody tr").count(), suite.contracts.length);
      await page.reload();
      assert.deepEqual(await page.evaluate(id => window.__ENYGMA_DEMO__.state().protocols[id].contracts, id), protocol.contracts);
      await context.close();
      console.log(`  PASS  ${id}: ${suite.contracts.length} deployments and ${protocol.deploymentActions.length} initialization calls match protocol sources`);
    }
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });

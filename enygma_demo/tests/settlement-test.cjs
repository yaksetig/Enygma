/* settlement.html — shield → lock → swap → audit → unshield.
 *
 * Drives the five steps and checks chain state after each one, re-deriving every commitment and
 * nullifier from the openings the page exposes rather than trusting what was rendered.
 *
 * The checks that matter most:
 *   - the chain pane exposes NO per-leaf spend state. A nullifier cannot be mapped back to a leaf,
 *     so "which of these was spent" must stay unanswerable from the ledger view. (Same property
 *     nf-test.cjs enforces for index.html.)
 *   - Alice and Bob shield into a populated tree, not an empty one, so the anonymity set is real.
 *   - after the swap the vault's public ERC-20 balances are unchanged.
 *   - the auditor's decryption is a genuine AEAD open, per-party.
 */
const path = require("path");

/* This suite has its own page, so it overrides DEMO_PAGE unconditionally. Use SETTLEMENT_PAGE to
 * point it at a different build. Must happen before _env.cjs resolves PAGE. */
process.env.DEMO_PAGE = process.env.SETTLEMENT_PAGE ||
                        path.join(__dirname, "..", "settlement.html");

const { chromium, launchOpts, PAGE } = require("./_env.cjs");
const w = (p, ms) => p.waitForTimeout(ms);
let fails = 0;
const pass = (c, m) => { console.log((c ? "  PASS  " : "  FAIL  ") + m); if(!c) fails++; };

async function step(pg, n){
  await pg.click(`.sbtn[data-step="${n}"]`);
  await pg.waitForFunction(() => !window.__ENYGMA_DVP.state().busy, null, { timeout: 60000 });
  await w(pg, 150);
}
const snap = (pg) => pg.evaluate(() => {
  const S = window.__ENYGMA_DVP.state();
  return { block: S.block, bal: S.bal, root: S.root, reveal: S.reveal,
           auditorRegistered: S.auditorRegistered, auditStarted: S.auditStarted, dvp: { ...S.dvp },
           leaves: S.leaves.map(l => ({ i:l.i, C:l.C, owner:l.owner, tok:l.tok, amt:l.amt,
                                        salt:l.salt, state:l.state, ct:l.note.ct })),
           nfs: S.nfs.slice(), txs: S.txs.length };
});
const totals = (bal, t) => bal[t].Alice + bal[t].Bob + bal[t].Others + bal[t].Vault;

(async () => {
  const b = await chromium.launch(launchOpts);
  const pg = await b.newPage({ viewport: { width: 1440, height: 1100 } });
  const errs = [];
  pg.on("pageerror", e => errs.push("pageerror: " + e.message));
  pg.on("console", m => { if(m.type() === "error") errs.push(m.text()); });

  await pg.goto(PAGE);
  await pg.waitForFunction(() => window.__ENYGMA_DVP && window.__ENYGMA_DVP.ready,
                           null, { timeout: 20000 });
  // the vault keeps taking unrelated deposits on a timer; freeze it so counts are deterministic
  await pg.evaluate(() => window.__ENYGMA_DVP.setTraffic(false));

  /* ── genesis ───────────────────────────────────────────────────────────── */
  console.log("--- genesis ---");
  const g = await snap(pg);
  pass(g.bal.ACME.Alice === 100 && g.bal.USDC.Bob === 5000,
       "Alice holds 100 ACME and Bob 5,000 USDC, both public");
  pass(g.bal.ACME.Others > 0 && g.bal.USDC.Others > 0, "other accounts hold public balances too");
  pass(g.bal.ACME.Vault > 0 && g.bal.USDC.Vault > 0, "and it already custodies those deposits");
  pass(g.leaves.length === 8 && g.root !== null,
       `the vault already has a history when the page loads (${g.leaves.length} leaves)`);
  pass(g.nfs.length === 0, "but nothing has been spent yet");
  pass(g.leaves.every(l => l.owner !== "Alice" && l.owner !== "Bob"),
       "none of the pre-existing leaves is Alice's or Bob's");
  pass(g.reveal === false, "the reveal toggle is off by default — the page opens on what the chain sees");

  const ACME0 = totals(g.bal, "ACME"), USDC0 = totals(g.bal, "USDC");

  /* ── 0 · register auditor ──────────────────────────────────────────────── */
  console.log("\n--- 0 · register: governance setup before Alice or Bob transacts ---");
  const beforeReg = await pg.evaluate(() => ({
    audit: document.getElementById("audRows").innerText,
    registerDisabled: document.getElementById("btnRegisterAuditor").disabled,
    shieldDisabled: document.querySelector('.sbtn[data-step="1"]').disabled,
  }));
  pass(/not registered/.test(beforeReg.audit) && !beforeReg.registerDisabled,
       "registration is the first available action and the auditor initially reads nothing");
  pass(beforeReg.shieldDisabled, "shielding is locked until the auditor is registered");

  await step(pg, 0);
  const s0 = await snap(pg);
  const afterReg = await pg.evaluate(() => ({
    audit: document.getElementById("audRows").innerText,
    shareDisabled: document.getElementById("btnShareA").disabled &&
                   document.getElementById("btnShareB").disabled,
    log: document.getElementById("log").innerText,
  }));
  pass(s0.auditorRegistered && /registerAuditor/.test(afterReg.log),
       "registerAuditor is recorded before the shield, lock or swap transactions");
  pass(/AEAD auth failure/.test(afterReg.audit) && afterReg.shareDisabled,
       "the registered auditor has no view key and disclosure remains locked until the audit step");

  /* ── 1 · shield ────────────────────────────────────────────────────────── */
  console.log("\n--- 1 · shield: ERC-20 in, commitment out, into a populated tree ---");
  await step(pg, 1);
  const s1 = await snap(pg);
  pass(s1.bal.ACME.Alice === 0 && s1.bal.USDC.Bob === 0, "Alice's and Bob's public balances went to zero");
  pass(totals(s1.bal,"ACME") === ACME0 && totals(s1.bal,"USDC") === USDC0, "no supply was created");

  pass(s1.leaves.length === 13, `the tree holds ${s1.leaves.length} leaves, not two`);
  const aliceLeaf = s1.leaves.find(l => l.owner === "Alice");
  const bobLeaf   = s1.leaves.find(l => l.owner === "Bob");
  pass(aliceLeaf.i === 8, `Alice lands at leaf ${aliceLeaf.i} — after eight deposits that predate her`);
  pass(bobLeaf.i - aliceLeaf.i === 4,
       `three unrelated deposits land between Alice's leg and Bob's (leaf ${aliceLeaf.i} → ${bobLeaf.i})`);
  pass(s1.leaves.filter(l => l.owner !== "Alice" && l.owner !== "Bob").length === 11,
       "eleven leaves belong to nobody in this story — that is the anonymity set");

  const derived = await pg.evaluate(async () => {
    const D = window.__ENYGMA_DVP, S = D.state();
    const out = [];
    for(const l of S.leaves){
      const p = D.partyOf(l.owner);
      out.push({ ok: (await D.commit(p.pkSpend, l.salt, l.amt, D.TOK[l.tok].id)) === l.C });
    }
    return { all: out.every(o => o.ok), rootRe: await D.root(S.leaves.map(x => x.C)), root: S.root };
  });
  pass(derived.all, "every commitment re-derives as Poseidon4(pk_spend, salt, amount, tokenId)");
  pass(derived.rootRe === derived.root, "the Merkle root re-derives from the leaf list");

  /* the commitment is built on screen, value by value — and those values are the real ones */
  console.log("\n    the commitment construction panel");
  const mint = await pg.evaluate(() => ({
    shown: document.getElementById("mintCard").style.display !== "none",
    title: document.getElementById("mintTitle").innerText,
    body:  document.getElementById("mintGrid").innerText,
    cells: document.querySelectorAll("#mintGrid .bytes")[0].children.length,
    byteGroups: document.querySelectorAll("#mintGrid .bytes").length,
    stagesDone: document.querySelectorAll("#mintStages .mst.ok").length,
  }));
  pass(mint.shown && /Bob shields 5,000 USDC/.test(mint.title),
       "the panel shows the last commitment built, value by value");
  pass(["pk_spend","salt","amount","tokenId"].every(k => mint.body.includes(k)),
       "all four inputs to the commitment are named");
  pass(/Poseidon4\(pk_spend, salt, amount, tokenId\)/.test(mint.body),
       "the hash it feeds is written out in full");
  pass(mint.cells === 32, `the commitment renders as ${mint.cells} bytes — a fixed shape whatever went in`);
  pass(mint.byteGroups === 1 && /Merkle tree/.test(mint.body),
       "the panel shows one actual commitment and identifies it as the Merkle-tree leaf value");
  pass(mint.stagesDone === 6, `all six construction stages completed (${mint.stagesDone})`);
  pass(!/undefined|NaN/.test(mint.body), "no placeholder leaked into the panel");

  // the panel is not decoration: the salt on screen is the salt in the leaf
  const shownSalt = await pg.evaluate(() => {
    const rows = [...document.querySelectorAll("#mintGrid .mrow")];
    const r = rows.find(x => x.innerText.includes("salt"));
    return r.querySelector(".v").innerText.trim().split(/\s+/)[0];
  });
  pass(shownSalt === bobLeaf.salt,
       "the salt rendered on screen is the one actually committed to, not a decorative value");
  const reCommit = await pg.evaluate(async (s) => {
    const D = window.__ENYGMA_DVP;
    return D.commit(D.partyOf("Bob").pkSpend, s.salt, s.amt, D.TOK[s.tok].id);
  }, bobLeaf);
  pass(reCommit === bobLeaf.C, "and that opening re-derives the commitment the panel displayed");

  // the chain pane renders hashes only
  const chainText = await pg.evaluate(() => document.getElementById("leaves").innerText);
  pass(!/Alice|Bob|acct|ACME|USDC/.test(chainText),
       "the on-chain tree shows hashes only — no owner, no asset, no amount");

  /* ── 2 · lock ──────────────────────────────────────────────────────────── */
  console.log("\n--- 2 · lock: both legs into the DvP contract ---");
  await step(pg, 2);
  const s2 = await snap(pg);
  pass(s2.leaves.filter(l => l.state === "locked").length === 2, "both notes are locked");
  pass(s2.dvp.status === "matched" && s2.dvp.legs.length === 2,
       `the contract holds both legs (${s2.dvp.status})`);
  pass(s2.nfs.length === 0, "nothing is spent yet — locking is not spending");

  // the ZK statement pane is the answer to "does the proof say which leaf?"
  const zk = await pg.evaluate(() => document.getElementById("zk").innerText);
  pass(/merkleRoot/.test(zk) && /nullifier/.test(zk), "the public inputs list the root and the nullifier");
  pass(/leafIndex/.test(zk) && /merklePath/.test(zk) && /sk_spend/.test(zk),
       "leafIndex, merklePath and sk_spend are listed as private witness");
  pass(/never says which leaf/i.test(zk),
       "the pane states plainly that the proof does not reveal which leaf is spent");
  const zkPrivate = await pg.evaluate(() =>
    document.querySelector(".zkcol.prv").innerText);
  pass(!new RegExp(String(s2.leaves.find(l => l.owner === "Alice").i) + "\\b").test(
         zkPrivate.replace(/leafIndex/g, "")) || /▓/.test(zkPrivate),
       "the private witness column publishes no actual values, only redactions");

  /* ── 3 · swap ──────────────────────────────────────────────────────────── */
  console.log("\n--- 3 · swap: two nullifiers, two new commitments, one transaction ---");
  await step(pg, 3);
  const s3 = await snap(pg);
  pass(s3.leaves.filter(l => l.state === "spent").length === 2, "both input notes are spent");
  pass(s3.leaves.length === 15, `two new commitments were inserted (${s3.leaves.length} leaves)`);
  pass(s3.dvp.status === "settled", "the swap is settled");

  const aliceNow = s3.leaves.filter(l => l.owner === "Alice" && l.state === "unspent");
  const bobNow   = s3.leaves.filter(l => l.owner === "Bob"   && l.state === "unspent");
  pass(aliceNow.length === 1 && aliceNow[0].tok === "USDC" && aliceNow[0].amt === 5000,
       "Alice now holds a 5,000 USDC note — Bob's asset");
  pass(bobNow.length === 1 && bobNow[0].tok === "ACME" && bobNow[0].amt === 100,
       "Bob now holds a 100 ACME note — Alice's asset");
  pass(totals(s3.bal,"ACME") === ACME0 && totals(s3.bal,"USDC") === USDC0,
       "the swap created and destroyed nothing");

  /* the headline invariant */
  pass(s3.bal.ACME.Vault === s2.bal.ACME.Vault && s3.bal.USDC.Vault === s2.bal.USDC.Vault,
       "the vault's public ERC-20 balances are UNCHANGED — ownership moved inside the shielded set");

  /* the privacy invariant the ZK proof exists to provide */
  console.log("\n--- and the ledger cannot say which leaf was retired ---");
  const leakage = await pg.evaluate(() => {
    const tree = document.getElementById("leaves").innerText;
    const nfs  = document.getElementById("nfs").innerText;
    const tiles = [...document.querySelectorAll("#leaves .leaf")];
    return { tree, nfs,
             spentMarkers: (tree.match(/spent/gi) || []).length,
             classes: [...new Set(tiles.map(t => t.className.trim()))],
             anon: document.getElementById("anonBox").innerText };
  });
  pass(leakage.spentMarkers === 0,
       "no leaf tile carries a spent marker — the chain has no per-leaf spend state to show");
  pass(leakage.classes.length === 1,
       `every leaf tile renders identically (${leakage.classes.length} distinct style)`);
  pass(!/leaf \d/.test(leakage.nfs) && !/Alice|Bob/.test(leakage.nfs),
       "the published nullifier set names no leaf and no owner");
  pass(/anonymity set/.test(leakage.anon) && /15/.test(leakage.anon),
       "the anonymity set is stated as the full tree");
  pass(/unknown/i.test(leakage.anon),
       "and the page says outright that which leaves were retired is unknown");

  const nf = await pg.evaluate(async () => {
    const D = window.__ENYGMA_DVP, S = D.state();
    const spent = S.leaves.filter(l => l.state === "spent");
    const re = [];
    for(const l of spent) re.push(await D.nullif(D.partyOf(l.owner).skSpend, l.i));
    return { re, on: S.nfs.map(x => x.nf), wrongKey: await D.nullif("00".repeat(32), spent[0].i) };
  });
  pass(nf.re.every(x => nf.on.includes(x)),
       "both published nullifiers re-derive from their own spend keys");
  pass(!nf.on.includes(nf.wrongKey),
       "a different spend key on the same leaf yields a different nullifier");

  /* the reveal toggle is explicitly off-chain */
  await pg.click("#reveal");
  await w(pg, 250);
  const revealed = await pg.evaluate(() => ({
    tree: document.getElementById("leaves").innerText,
    warn: document.getElementById("anonBox").innerText,
  }));
  pass(/Alice · 100 ACME/.test(revealed.tree) && /Bob · 5,000 USDC/.test(revealed.tree),
       "reveal shows the openings the chain cannot compute");
  pass(/NOT on chain/i.test(revealed.warn), "and labels them as not on chain");
  await pg.click("#reveal");
  await w(pg, 200);

  /* ── 4 · audit ─────────────────────────────────────────────────────────── */
  console.log("\n--- 4 · audit: a view key opens its owner's notes and nothing else ---");
  const beforeAudit = await pg.evaluate(() => ({
    audit: document.getElementById("audRows").innerText,
    shareDisabled: document.getElementById("btnShareA").disabled &&
                   document.getElementById("btnShareB").disabled,
  }));
  pass(/AEAD auth failure/.test(beforeAudit.audit) && beforeAudit.shareDisabled,
       "the auditor is already registered, but disclosure waits for the audit step");

  await step(pg, 4);
  const auditStarted = await pg.evaluate(() => ({
    state: window.__ENYGMA_DVP.state().auditStarted,
    shareEnabled: !document.getElementById("btnShareA").disabled &&
                  !document.getElementById("btnShareB").disabled,
  }));
  pass(auditStarted.state && auditStarted.shareEnabled,
       "the audit step enables the two explicit view-key sharing actions");

  await pg.click("#btnShareA");
  await pg.waitForFunction(() => window.__ENYGMA_DVP.state().alice.sharedView, null, { timeout: 10000 });
  await w(pg, 400);
  const shared = await pg.evaluate(() => document.getElementById("audRows").innerText);
  pass(/Alice · 100 ACME/.test(shared) && /Alice · 5,000 USDC/.test(shared),
       "with Alice's view key the auditor reads both of her notes — before and after the swap");
  pass((shared.match(/AEAD auth failure/g) || []).length === 13,
       "every other note stays sealed — a view key is per-party, not a master key");

  const aead = await pg.evaluate(async () => {
    const D = window.__ENYGMA_DVP, S = D.state();
    const l = S.leaves[0], p = D.partyOf(l.owner);
    const good = await D.hkdf(p.skView, "enygma/note/" + l.salt, 32);
    let threw = false;
    try { await D.open_("11".repeat(32), l.note.ct); } catch(e){ threw = true; }
    return { threw, opened: await D.open_(good, l.note.ct), expect: D.TOK[l.tok].id + "|" + l.amt };
  });
  pass(aead.threw, "a key the auditor does not hold throws, rather than rendering a 'no'");
  pass(aead.opened === aead.expect, "the right view key opens the note to tokenId ‖ amount");
  pass(/view key only/.test(await pg.evaluate(() => document.getElementById("log").innerText)),
       "the log records that disclosure carries no spend authority");

  /* ── 5 · unshield ──────────────────────────────────────────────────────── */
  console.log("\n--- 5 · unshield: back out to public ERC-20, under the new owners ---");
  await step(pg, 5);
  const s5 = await snap(pg);
  pass(s5.bal.USDC.Alice === 5000 && s5.bal.ACME.Bob === 100,
       "Alice withdrew 5,000 USDC and Bob 100 ACME — the swap was real");
  pass(s5.bal.ACME.Alice === 0 && s5.bal.USDC.Bob === 0, "neither kept what they started with");
  pass(totals(s5.bal,"ACME") === ACME0 && totals(s5.bal,"USDC") === USDC0,
       `supply is conserved end to end (${totals(s5.bal,"ACME")} ACME, ${totals(s5.bal,"USDC")} USDC)`);
  pass(s5.bal.ACME.Vault > 0 && s5.bal.USDC.Vault > 0,
       "the vault still holds the other ten accounts' deposits — they never withdrew");
  pass(s5.nfs.length === 4, "four nullifiers: two notes each, spent once in the swap and once on exit");

  /* ── page health ───────────────────────────────────────────────────────── */
  console.log("\n--- the page ---");
  const methods = await pg.evaluate(() => document.getElementById("log").innerText);
  for(const m of ["depositV2", "submitPartialSettlement", "exchangeOnGroupPair",
                  "registerAuditor", "withdraw"])
    pass(methods.includes(m), `the log names the real contract call ${m}`);

  await pg.click("#btnReset");
  await w(pg, 400);
  await pg.evaluate(() => window.__ENYGMA_DVP.setTraffic(false));
  const reset = await snap(pg);
  pass(reset.leaves.length === 0 && reset.root === null && reset.nfs.length === 0 &&
       reset.bal.ACME.Alice === 100 &&
       !reset.reveal && !reset.auditorRegistered,
       "reset returns the chain to a blank, pre-registration genesis with reveal off");

  /* the vault does not stand still — and that is the point, since the anonymity set is the tree */
  console.log("\n--- ambient traffic keeps the anonymity set growing ---");
  const before = (await snap(pg)).leaves.length;
  const period = await pg.evaluate(() => window.__ENYGMA_DVP.TRAFFIC_MS);
  await pg.evaluate(() => window.__ENYGMA_DVP.setTraffic(true));
  await pg.waitForFunction((n) => window.__ENYGMA_DVP.state().leaves.length > n,
                           before, { timeout: period * 3 });
  const after = (await snap(pg)).leaves.length;
  await pg.evaluate(() => window.__ENYGMA_DVP.setTraffic(false));
  pass(after > before, `unrelated deposits keep arriving on a timer (${before} → ${after} leaves)`);
  const grown = await pg.evaluate(() => document.getElementById("anonBox").innerText);
  pass(grown.includes(String(after)), "and the stated anonymity set tracks the tree as it grows");
  const paused = await pg.evaluate(() => document.getElementById("btnTraffic").dataset.on);
  pass(paused === "false", "traffic can be paused from the chain header");

  await pg.setViewportSize({ width: 430, height: 900 });
  await w(pg, 300);
  const overflow = await pg.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth);
  pass(overflow <= 1, `no horizontal overflow at 430px (${overflow}px)`);

  pass(errs.length === 0, errs.length ? "page errors: " + errs.join(" | ") : "no page or console errors");

  await b.close();
  process.exit(fails ? 1 : 0);
})();

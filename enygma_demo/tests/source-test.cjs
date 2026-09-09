const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { root } = require("./_env.cjs");

const deployable = ["index.html", "styles.css", "js/app.js", "js/config.js", "js/demo-engine.js"];
const source = deployable.map(file => fs.readFileSync(path.join(root, file), "utf8")).join("\n");
const retiredCurve = new RegExp(["baby", "jub"].join("\\s*"), "i");
const environmentFraming = new RegExp(["simu" + "lat", "fix" + "ture", "mock" + "ed", "presentation" + "-only"].join("|"), "i");
const dvpSource = source.slice(source.indexOf("function dvpSteps"), source.indexOf("function auctionSteps"));
const checks = [
  [!retiredCurve.test(source), "no retired auditor-curve references"],
  [!/crypto\.subtle|WebCrypto|Pedersen|Poseidon|Merkle proof/i.test(source), "no real cryptographic implementation"],
  [!/\bfetch\s*\(|EventSource|WebSocket|XMLHttpRequest/i.test(source), "no backend or streaming integration"],
  [!/prefers-color-scheme\s*:\s*dark|data-theme|theme-toggle/i.test(source), "no dark-mode implementation"],
  [!/anonymity filler|anonymity user/i.test(source), "no invented anonymity leaves or users"],
  [!/direct tagged channel|rotating recipient tag/i.test(source), "no invented retail recipient-tag modes"],
  [!fs.existsSync(path.join(root, "settlement.html")), "duplicate settlement page removed"],
  [!source.includes("settlement.html"), "no stale settlement links"],
  [/sourceTxId/.test(source), "leaves retain transaction provenance"],
  [/function rebuildCommitmentTrees/.test(source) && /data-tree-asset/.test(source) && /background-shield/.test(source), "DvP shielding traffic updates independent per-asset Merkle trees"],
  [/}, 3400\);/.test(source), "network traffic interval is slowed to 3.4 seconds"],
  [!/case "dvp:(?:issue|lock|settle)"/.test(source) && /case "dvp:mint-security"/.test(source) && /case "dvp:shield-security"/.test(source) && /case "dvp:instantiate-transfer"[\s\S]*p\.flow\.cashLocked = true/.test(source) && /case "dvp:lock-security"/.test(source) && !/case "dvp:lock-cash"/.test(source), "DvP uses flexible holdings, initiates with the cash leg, and settles on the seller response"],
  [/encryptedFields: \["salt", "token_id", "amount"\]/.test(source) && /H\(pk_spend, salt, token_id, amount\)/.test(source), "auditable payloads match the commitment opening fields"],
  [/leafCommitments: \[securityLeg\.encryptedPayload\.commitment, cashLeg\.encryptedPayload\.commitment\]/.test(source), "settlement leaves use the commitments carried by the two encrypted legs"],
  [/transactions\.filter\(tx => tx\.encryptedPayload/.test(source) && /Disclose symmetric key/.test(source), "audit disclosure controls appear only for encrypted payloads"],
  [/function recordPrivateNote/.test(source) && /securityInputNoteId/.test(source) && /cashInputNoteId/.test(source) && /One shielding operation → one private note → one leaf/.test(source), "DvP shielding retains distinct notes and terms reference exact input leaves"],
  [/ownedLeafIds/.test(source) && /owned-leaf/.test(source) && /data-owned/.test(source), "all participant-controlled shielding leaves remain highlighted"],
  [/function dvpAuditCard/.test(source) && /Auditor access established during registration/.test(source) && /The same settlement, three different views/.test(source), "DvP auditing reuses registration-time access and compares privacy perspectives"],
  [!/id: "traffic"|id: "chain"/.test(dvpSource), "DvP has no trailing traffic or public-chain stages"],
  [/label: "Optional unshield"/.test(source), "DvP unshielding is explicitly optional"],
  [!environmentFraming.test(source), "professional environment language throughout"],
  [source.includes("#5B4BE0") && source.includes("#B87708") && source.includes("#171922"), "Rayls light palette is centralized"]
];

for (const [condition, name] of checks) {
  assert(condition, name);
  console.log(`  PASS  ${name}`);
}
console.log(`\n${checks.length} source checks passed.`);

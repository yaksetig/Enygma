const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { root } = require("./_env.cjs");

const deployable = ["index.html", "styles.css", "js/app.js", "js/config.js", "js/demo-engine.js"];
const source = deployable.map(file => fs.readFileSync(path.join(root, file), "utf8")).join("\n");
const unrelatedCurve = /secp256k1|ECDH|ECDSA/i;
const environmentFraming = new RegExp(["simu" + "lat", "fix" + "ture", "mock" + "ed", "presentation" + "-only"].join("|"), "i");
const dvpSource = source.slice(source.indexOf("function dvpSteps"), source.indexOf("function auctionSteps"));
const auctionSource = source.slice(source.indexOf("function auctionSteps"), source.indexOf("function scenarioSteps"));
const retailSource = source.slice(source.indexOf("function retailSteps"), source.indexOf("function formatAmount"));
const checks = [
  [!unrelatedCurve.test(source), "no host-chain wallet primitives in protocol explanations"],
  [/Pedersen on BabyJubJub/.test(source) && /Groth16 over BN254/.test(source), "institutional commitments and proof curves match the protocol"],
  [!/\bfetch\s*\(|EventSource|WebSocket|XMLHttpRequest/i.test(source), "no backend or streaming integration"],
  [!/prefers-color-scheme\s*:\s*dark|data-theme|theme-toggle/i.test(source), "no dark-mode implementation"],
  [!/anonymity filler|anonymity user/i.test(source), "no invented anonymity leaves or users"],
  [!/direct tagged channel|rotating recipient tag/i.test(source), "no invented retail recipient-tag modes"],
  [/function publicNetworkRegistry/.test(source) && /Public blockchain state/.test(source) && /publicNetworkRegistry\(p\)/.test(source), "public participant registry persists beside every protocol action"],
  [/retail:configure-tags/.test(source) && /\["none", "subset", "rift", "full"\]/.test(source) && /retailTagCandidateIndices/.test(source) && /Published bit/.test(retailSource), "retail implements the four protocol-defined private-tag bitmap modes"],
  [!/prepare-recipient|Retrieve Atlas Bank keys|Retrieve the recipient’s public keys/.test(source) && /Registry binding/.test(retailSource), "retail key lookup is a visible registry binding rather than a retrieval action"],
  [!fs.existsSync(path.join(root, "settlement.html")), "duplicate settlement page removed"],
  [!source.includes("settlement.html"), "no stale settlement links"],
  [/sourceTxId/.test(source), "leaves retain transaction provenance"],
  [/function rebuildCommitmentTrees/.test(source) && /data-tree-asset/.test(source) && /background-shield/.test(source), "DvP shielding traffic updates independent per-asset Merkle trees"],
  [/}, 3400\);/.test(source), "network traffic interval is slowed to 3.4 seconds"],
  [!/case "dvp:(?:issue|lock|settle)"/.test(source) && /case "dvp:mint-security"/.test(source) && /case "dvp:shield-security"/.test(source) && /case "dvp:instantiate-transfer"[\s\S]*p\.flow\.cashLocked = true/.test(source) && /case "dvp:lock-security"/.test(source) && !/case "dvp:lock-cash"/.test(source), "DvP uses flexible holdings, initiates with the cash leg, and settles on the seller response"],
  [/\["token_id", "amount", "salt_star"\]/.test(source) && /\["token_id", "amount"\]/.test(source) && /Poseidon\(pk_spend, salt, amount, token_id\)/.test(source), "note and swap payload fields and commitment input order match the protocol"],
  [/leafCommitments: \[securityLeg\.encryptedPayload\.commitment, cashLeg\.encryptedPayload\.commitment\]/.test(source), "settlement leaves use the commitments carried by the two encrypted legs"],
  [/transactions\.filter\(tx => tx\.encryptedPayload/.test(source) && /Disclose symmetric key/.test(source), "audit disclosure controls appear only for encrypted payloads"],
  [/function recordPrivateNote/.test(source) && /securityInputNoteId/.test(source) && /cashInputNoteId/.test(source) && /One shielding operation → one private note → one leaf/.test(source), "DvP shielding retains distinct notes and terms reference exact input leaves"],
  [/ownedLeafIds/.test(source) && /owned-leaf/.test(source) && /data-owned/.test(source), "all participant-controlled shielding leaves remain highlighted"],
  [/function dvpAuditCard/.test(source) && /Auditor access established during registration/.test(source) && /The same settlement, three different views/.test(source), "DvP auditing reuses registration-time access and compares privacy perspectives"],
  [!/id: "traffic"|id: "chain"/.test(dvpSource), "DvP has no trailing traffic or public-chain stages"],
  [/label: "Optional unshield"/.test(source), "DvP unshielding is explicitly optional"],
  [/case "auctions:register-auctioneer"/.test(source) && /auction\.auctioneerRegistered/.test(source) && /Register auctioneer for this auction/.test(auctionSource), "auctioneer key is bound to one specific auction"],
  [/case "auctions:mint-nft"/.test(source) && /assetQuantity/.test(source) && /auctionAssetQuantity/.test(auctionSource) && /case "auctions:list-asset"/.test(source) && /Bidders are shown/.test(auctionSource), "auction asset has a selectable certificate quantity before listing"],
  [/case "auctions:mint-cash"/.test(source) && /case "auctions:shield-cash"/.test(source) && /case "auctions:submit-bid"/.test(source) && /auctionBidAmount/.test(auctionSource) && /changeAmount/.test(source) && /auction_change/.test(source), "auction bid splits a funded USD note into the chosen bid and private change"],
  [/case "auctions:close-bidding"/.test(source) && /case "auctions:prove-winner"/.test(source) && /highest-valid-bid proof/.test(source), "bidding closes at timeout before the auctioneer proves the winner"],
  [/privateWinningAmount/.test(source) && /Winning amount remains private/.test(auctionSource) && /Public winning amount/.test(auctionSource), "winner announcement and settlement do not publish the winning amount"],
  [/winning-bid/.test(source) && /zk-circuit/.test(source) && /amount\* ≥ amountᵢ/.test(auctionSource) && /No amount is a public output/.test(auctionSource), "winning bid and highest-valid-bid circuit are shown visually"],
  [/data-auction-perspective/.test(auctionSource) && /PUBLIC CHAIN VIEW/.test(auctionSource) && /YOUR WALLET VIEW/.test(auctionSource) && /AUTHORIZED AUDITOR VIEW/.test(auctionSource) && !/controls this leaf/.test(source), "auction settlement separates public, wallet, and auditor perspectives"],
  [/atomic-auction-settlement/.test(source) && /auction_recovery/.test(source) && /asset-tree-stack/.test(auctionSource), "auction settlement atomically creates asset, payout, and loser-recovery notes"],
  [!/publicChainCard\(p\)|auditCard\(p\)/.test(auctionSource), "auction uses protocol-specific privacy views instead of generic audit and chain cards"],
  [!environmentFraming.test(source + fs.readFileSync(path.join(root, "README.md"), "utf8")), "professional environment language throughout"],
  [source.includes("#5B4BE0") && source.includes("#B87708") && source.includes("#171922"), "Rayls light palette is centralized"]
];

for (const [condition, name] of checks) {
  assert(condition, name);
  console.log(`  PASS  ${name}`);
}
console.log(`\n${checks.length} source checks passed.`);

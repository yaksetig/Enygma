import { commitmentTreeExplorer, treePageSize } from "./commitment-tree-ui.js";
import { walletSummary, shieldingScene, shieldNotePreview as sharedShieldNotePreview } from "./shielding-ui.js";
import { retailState, hasRetailChannel, availableRetailNotes, validateRetailShieldPlan, RETAIL_DEPTH } from "./retail.js";

export const retailUI = {
  channelDraft: { recipientIndex: 1, mode: "full", excludedIndices: new Set([8, 9]) },
  channelId: "", noteId: "", txId: "", amount: "30", shieldAmount: "200", shieldCount: "2", shieldAmounts: ["100", "100"], mintAmount: "1000", viewer: "public", leafId: "", treeStart: 0, treeFollow: true
};
const short = (value, n = 12, end = 6) => value ? String(value).length > n + end + 1 ? `${String(value).slice(0, n)}…${String(value).slice(-end)}` : String(value) : "—";
const fmt = value => Number(value).toLocaleString("en-US");
const inputValue = value => String(value).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const validUSD = value => /^\d+$/.test(String(value)) && Number.isSafeInteger(Number(value)) && Number(value) > 0;
const shieldAmounts = () => retailUI.shieldAmounts.slice(0, Number(retailUI.shieldCount));
const nameOf = (p, id) => p.registrations.find(r => r.partyId === id)?.name || "You";
const initials = name => name === "You" ? "YOU" : name.split(" ").map(n => n[0]).join("");
const code = value => `<code title="${value || ""}">${short(value, 16, 8)}</code>`;
const head = (eyebrow, title, status = "") => `<div class="panel-heading"><div><p class="eyebrow">${eyebrow}</p><h2>${title}</h2></div>${status ? `<span class="context-badge">${status}</span>` : ""}</div>`;
const steps = (items, active) => `<ol class="retail-process">${items.map(([id, title, sub], i) => `<li class="${active === id ? "running" : active && items.findIndex(x => x[0] === active) > i ? "done" : ""}"><b>${i + 1}</b><span><strong>${title}</strong><small>${sub}</small></span></li>`).join("")}</ol>`;
const activeChannel = p => retailState(p).channels.find(c => c.id === retailUI.channelId) || retailState(p).channels.at(-1);
const selectedPayment = p => p.transactions.find(t => t.id === retailUI.txId && t.type === "payment") || p.transactions.find(t => t.type === "payment");

function noteCard(note, label, extra = "") {
  return `<div class="retail-note-card ${extra}"><header><span>${label}</span><b>${note ? `Leaf ${note.leafIndex ?? "new"}` : "NEW NOTE"}</b></header><strong>${note ? fmt(note.amount) : "—"}<small> USD</small></strong><code>${short(note?.commitment, 16, 7)}</code><footer>${note?.status === "spent" ? "Spent · commitment stays in the tree" : "Poseidon commitment"}</footer></div>`;
}

function treePanel(p, viewer = "party-0", showTraffic = true) {
  return commitmentTreeExplorer(p, { viewer, showTraffic, state: retailUI, control: "retail", depth: RETAIL_DEPTH, capacity: 256, vault: "Erc20CoinVault" });
}

function walletStrip(p) {
  const s = retailState(p), notes = availableRetailNotes(p);
  return `${walletSummary({ asset: "USD", publicBalance: s.publicBalance, privateBalance: notes.reduce((sum, n) => sum + n.amount, 0), noteCount: notes.length, leafCount: p.leaves.length })}<div class="retail-tree-jump"><button type="button" class="button button-ghost" data-retail-tree-show>Explore live commitment tree ↓</button></div>`;
}
function noteInventory(p) {
  const notes = (p.notes || []).filter(n => n.ownerPartyId === "party-0");
  return `<section class="retail-inventory"><div class="retail-section-heading"><h3>Your notes</h3><span>Every note remains visible after later deposits and payments.</span></div><div class="retail-note-inventory">${notes.map(n => `<button type="button" data-retail-leaf="${n.leafId}" class="retail-note-select ${n.status === "spent" ? "spent" : ""}">${noteCard(n, n.origin === "change" ? "Payment change" : "Shielded note")}</button>`).join("") || noteCard(null, "Shield your first note", "placeholder")}</div></section>`;
}

function shieldPlanStatus(p) {
  try { return { ...validateRetailShieldPlan({ amounts: shieldAmounts(), totalAmount: retailUI.shieldAmount }, retailState(p).publicBalance), valid: true }; }
  catch (error) { return { valid: false, message: error.message }; }
}

function shieldSummary(p) {
  const status = shieldPlanStatus(p), amounts = shieldAmounts();
  const sum = amounts.reduce((total, amount) => total + (validUSD(amount) ? Number(amount) : 0), 0);
  return `<div class="retail-shield-summary ${status.valid ? "ready" : "incomplete"}"><strong>${amounts.map(amount => validUSD(amount) ? fmt(amount) : "—").join(" + ")} = ${Number.isSafeInteger(sum) ? fmt(sum) : "—"} USD</strong><span>${status.valid ? `Ready to shield. ${fmt(retailState(p).publicBalance - status.total)} USD will remain public.` : status.message}</span></div>`;
}

function shieldNotePreview() {
  return sharedShieldNotePreview(shieldAmounts());
}

function refreshShieldInputs(p) {
  const summary = document.querySelector("#retailShieldSummary");
  if (!summary) return;
  summary.innerHTML = shieldSummary(p);
  document.querySelector(".retail-output-stack").innerHTML = shieldNotePreview();
  document.querySelector('[data-action="shield"]').disabled = !shieldPlanStatus(p).valid;
  document.querySelector('[data-action="mint-cash"]').disabled = !validUSD(retailUI.mintAmount);
  document.querySelector('[data-retail-split-evenly]').disabled = !validUSD(retailUI.shieldAmount) || Number(retailUI.shieldAmount) < Number(retailUI.shieldCount);
  const hashAmount = document.querySelector("[data-retail-shield-hash-amount]");
  if (hashAmount) hashAmount.textContent = validUSD(shieldAmounts()[0]) ? shieldAmounts()[0] : "—";
}

export function retailShieldCard(p) {
  const s = retailState(p), active = s.activity?.kind === "shield" ? s.activity : null;
  const latest = active?.output || [...p.notes].reverse().find(n => n.origin === "shielding" && n.ownerPartyId === "party-0");
  const amounts = shieldAmounts(), count = Number(retailUI.shieldCount);
  return `<article class="panel flow-card retail-workspace">${head("Fund your wallet · shield into private notes", "Watch your money become commitments", active ? `NOTE ${active.index} / ${active.count}` : "PAYER · YOU")}<div class="retail-live-layout"><div class="retail-action-pane retail-shield-action">${walletStrip(p)}
    <section class="retail-mint-stage"><h3>1. Mint public USD</h3><p>Add funds to your public wallet before choosing how to shield them.</p><div class="retail-funding-controls"><label>USD to mint<input id="retailMintAmount" type="text" inputmode="numeric" pattern="[0-9]*" value="${inputValue(retailUI.mintAmount)}"></label><button class="button button-primary" data-action="mint-cash" ${validUSD(retailUI.mintAmount) ? "" : "disabled"}>Mint USD</button><small>The issuer mints tokens to your public wallet.</small></div></section>
    <section class="retail-shield-plan"><h3>2. Choose your private notes</h3><p>Set the total, then choose how much goes into each note. Use whole USD amounts; the notes do not need to be equal.</p><div class="retail-form"><label>Total USD to shield<input id="retailShieldAmount" type="text" inputmode="numeric" pattern="[0-9]*" value="${inputValue(retailUI.shieldAmount)}"></label><label>Number of notes<select id="retailShieldCount">${[1, 2, 3, 4].map(n => `<option value="${n}" ${n === count ? "selected" : ""}>${n} ${n === 1 ? "note" : "notes"}</option>`).join("")}</select></label></div><div class="retail-split-heading"><strong>Choose each note’s amount</strong><button type="button" class="button button-ghost" data-retail-split-evenly ${!validUSD(retailUI.shieldAmount) || Number(retailUI.shieldAmount) < count ? "disabled" : ""}>Split evenly</button></div><div class="retail-note-amounts">${amounts.map((amount, i) => `<label><span>Note ${i + 1} · USD</span><input id="retailShieldNote${i}" data-retail-shield-note="${i}" type="text" inputmode="numeric" pattern="[0-9]*" value="${inputValue(amount)}"></label>`).join("")}</div><div id="retailShieldSummary" aria-live="polite">${shieldSummary(p)}</div><button class="button button-primary retail-shield-submit" data-action="shield" ${shieldPlanStatus(p).valid ? "" : "disabled"}>Shield into ${count} private ${count === 1 ? "note" : "notes"}</button></section>
    ${shieldingScene({ amounts, phase: active?.step })}
    ${steps([["derive", "Derive salt & key", "ML-KEM-768 → HKDF-SHA256"], ["commit", "Commit each note", "Poseidon(pk, salt, amount, token)"], ["insert", "Append to tree", "depositV2() → Commitment"]], active?.step)}
    <details class="retail-math" data-disclosure="retail-shield-calculation"><summary>Commitment calculation · token_id = 0 (USD)</summary><div class="retail-hash-equation"><span><small>pk_spend</small>${code(p.registrations[0].spendPublicKey)}</span><b>+</b><span><small>salt</small>${code(latest?.salt)}</span><b>+</b><span><small>amount</small><strong ${latest ? "" : "data-retail-shield-hash-amount"}>${latest?.amount ?? (validUSD(amounts[0]) ? amounts[0] : "—")}</strong></span><b>+</b><span><small>token_id</small><strong>0</strong></span><i>→ Poseidon →</i><span class="hash-result"><small>C</small>${code(latest?.commitment)}</span></div><p>ML-KEM encapsulates to your view public key. HKDF-SHA256 derives the salt and encryption key; AES-256-GCM encrypts the note data. Each deposit adds a separate commitment.</p></details>
    </div><div class="retail-live-tree">${treePanel(p)}</div></div>${noteInventory(p)}</article>`;
}

function channelMap(p, preview) {
  const s = retailState(p), channels = s.channels;
  const active = s.activity?.kind === "channel";
  const recipients = [...new Set([...channels.map(c => c.recipientPartyId), `party-${preview.recipientIndex}`])];
  const height = Math.max(220, recipients.length * 78), gap = height / recipients.length;
  return `<div class="retail-channel-map ${active ? "animating" : ""}" aria-label="Directional private channels from you to channel peers"><svg viewBox="0 0 680 ${height}" role="img"><title>${channels.length} established private channels and the selected channel peer</title>${recipients.map((id, i) => `<path class="${channels.some(c => c.recipientPartyId === id) ? "established" : "pending"}" d="M 115 ${height / 2} C 295 ${height / 2}, 325 ${(i + .5) * gap}, 483 ${(i + .5) * gap}"/><circle class="channel-packet" r="5"><animateMotion dur="2.5s" repeatCount="indefinite" path="M 115 ${height / 2} C 295 ${height / 2}, 325 ${(i + .5) * gap}, 483 ${(i + .5) * gap}"/></circle><g transform="translate(483,${(i + .5) * gap})"><rect width="185" height="60" x="0" y="-30" rx="12"/><text x="16" y="-4">${nameOf(p, id)}</text><text class="channel-sub" x="16" y="15">${channels.filter(c => c.recipientPartyId === id).length || "New"} ${channels.some(c => c.recipientPartyId === id) ? `established channel${channels.filter(c => c.recipientPartyId === id).length === 1 ? "" : "s"}` : "channel peer"}</text></g>`).join("")}<g transform="translate(15,${height / 2 - 45})" class="channel-sender"><rect width="125" height="90" rx="16"/><text x="62" y="36" text-anchor="middle">YOU</text><text x="62" y="58" class="channel-sub" text-anchor="middle">Your wallet</text></g></svg><div class="retail-map-caption"><span>PRIVATE WALLET MAP</span><strong>${channels.length} channel${channels.length === 1 ? "" : "s"} established</strong><small>Channel associations stay in your wallet.</small></div></div>`;
}

export function retailChannelCard(p, modes, registry) {
  const s = retailState(p), draft = retailUI.channelDraft, recipient = p.registrations[draft.recipientIndex];
  const connected = hasRetailChannel(p, recipient.partyId);
  const allConnected = p.registrations.slice(1).every(peer => hasRetailChannel(p, peer.partyId));
  const ch = s.activity?.channel || s.channels.at(-1);
  return `<article class="panel flow-card retail-workspace">${head("Private tags · establish channels", "Connect with multiple channel peers.", `${s.channels.length} ESTABLISHED`)}${channelMap(p, draft)}<div class="retail-section-heading"><h3>Create another channel</h3><span>Select a participant to establish a channel with, then choose the public candidate bitmap.</span></div><div class="tag-mode-grid">${modes}</div>${registry}
    <div class="retail-channel-request ${s.activity?.kind === "channel" ? "animating" : ""}"><div><span class="avatar">${initials(recipient.name)}</span><strong>${recipient.name}</strong><small>Registry binding · pk_view</small></div><i>→</i><div><b>ML-KEM-768</b><small>Encapsulate → c1 + shared secret</small><b>AES-256-GCM</b><small>HKDF channel key → encrypted c2</small></div><i>→</i><div class="retail-envelope"><b>Relayer → openChannel()</b><span><code>c1</code><code>c2</code><code>bitmap</code></span><small>TagChannelRegistry</small></div></div>
    ${steps([["encapsulate", "Encapsulate", "Channel peer’s pk_view"], ["encrypt", "Seal channel data", "HKDF + AES-256-GCM"], ["relay", "Relay request", "c1 · c2 · bitmap"], ["published", "Store channel", "ChannelOpened event"]], s.activity?.kind === "channel" ? s.activity.step : null)}
    ${connected ? `<div class="callout success-callout">${allConnected ? "You have established a channel with every other participant. Choose an existing channel below to make a payment." : `Your channel with ${recipient.name} is established. Select another participant above to create a new channel.`}</div>` : ""}
    <button class="button button-primary" data-action="configure-tags" ${connected ? "disabled" : ""}>${allConnected ? "All channels established" : connected ? "Select another channel peer" : "Establish private-tag channel"}</button>
    <div class="retail-channel-list">${s.channels.map(c => `<div class="channel-record" data-channel-record="${c.id}"><span class="avatar">${initials(nameOf(p, c.recipientPartyId))}</span><span><small>YOUR CHANNEL ${c.index}</small><strong>You → ${nameOf(p, c.recipientPartyId)}</strong><code>${short(c.id, 15, 6)}</code></span><span><small>${c.mode === "full" ? "Full privacy" : c.mode}</small><strong class="mono">${c.bitmap}</strong><small>Published bitmap · ${c.candidateIndices.length} candidates</small></span><button class="button button-small button-ghost" data-retail-use-channel="${c.id}">Use channel for a payment →</button></div>`).join("")}</div>
    ${ch ? `<details class="retail-math"><summary>Latest on-chain channel record · openChannel(c1, c2, bitmap)</summary><dl><dt>ML-KEM capsule · c1</dt><dd>${code(ch.c1)}</dd><dt>Encrypted channel data · c2</dt><dd>${code(ch.c2)}</dd><dt>Public bitmap</dt><dd><code>${ch.bitmap}</code></dd></dl></details>` : ""}</article>`;
}

function paymentDiagram(p, channel, input, amount, draft, tx) {
  const output = draft?.outputs || tx?.outputs;
  const recipient = nameOf(p, channel?.recipientPartyId);
  const phase = retailState(p).activity?.kind === "payment" ? retailState(p).activity.step : "";
  const inputNote = draft ? p.notes.find(n => n.id === draft.inputNoteId) : input;
  return `<div class="retail-payment-scene ${phase ? `animating phase-${phase}` : ""}" aria-label="One input note splits into recipient and change outputs"><div>${noteCard(inputNote, "Your input note", "input")}<div class="retail-nullifier"><span>Nullifier</span>${code(draft?.nullifier || tx?.nullifier)}<small>NF = Poseidon(sk_spend, leafIndex)</small></div></div><div class="retail-split-path"><svg viewBox="0 0 100 200" aria-hidden="true"><path d="M0 100 H35 Q50 100 50 85 V50 Q50 35 65 35 H100 M50 100 V150 Q50 165 65 165 H100"/><circle cx="50" cy="100" r="13"/><text x="50" y="104" text-anchor="middle">π</text></svg><span>Groth16<br>BN254</span></div><div class="retail-payment-outputs">${noteCard(output?.[0] || { amount, commitment: null }, `${recipient} receives`, "recipient")}${noteCard(output?.[1] || { amount: Math.max(0, (input?.amount || 0) - amount), commitment: null }, "Your private change", "change")}</div></div>`;
}

export function retailPaymentCard(p) {
  const s = retailState(p), channel = activeChannel(p), notes = availableRetailNotes(p);
  const input = notes.find(n => n.id === retailUI.noteId) || notes[0];
  const amount = Number(retailUI.amount) || 0, last = p.transactions.find(t => t.type === "payment");
  const draft = s.draft;
  const recipient = p.registrations.find(r => r.partyId === channel?.recipientPartyId);
  const active = s.activity?.kind === "payment" ? s.activity.step : null;
  return `<article class="panel flow-card retail-workspace">${head("Private payment · payer’s wallet", "Spend a note. Create two new ones.", channel ? `CHANNEL ${channel.index}` : "CHOOSE A CHANNEL")}<div class="retail-live-layout"><div class="retail-action-pane">${walletStrip(p)}
    <div class="retail-form"><label>Pay recipient<select id="retailPaymentRecipient" ${!s.channels.length ? "disabled" : ""}>${s.channels.length ? s.channels.map(c => `<option value="${c.id}" ${c.id === channel?.id ? "selected" : ""}>${nameOf(p, c.recipientPartyId)} · channel ${c.index} · ${c.mode}</option>`).join("") : `<option>Establish a private channel first</option>`}</select></label><label>Spend this note<select id="retailPaymentNote" ${!notes.length ? "disabled" : ""}>${notes.length ? notes.map(n => `<option value="${n.id}" ${n.id === input?.id ? "selected" : ""}>Leaf ${n.leafIndex} · ${fmt(n.amount)} USD</option>`).join("") : `<option>Shield a note first</option>`}</select></label><label>Amount · USD<input id="retailPaymentAmount" type="number" min="1" step="1" max="${input?.amount || 0}" value="${retailUI.amount}"></label></div>
    ${!channel || !input ? `<div class="retail-prerequisites">${!input ? `<a href="#/retail/shielding">Shield notes →</a>` : ""}${!channel ? `<a href="#/retail/private-tags">Establish channels →</a>` : ""}</div>` : ""}
    <div class="registry-binding-visual retail-binding"><div class="binding-recipient"><small>Payment recipient</small><strong>${recipient?.name || "Choose a channel"}</strong><span>Registry binding</span></div><div class="binding-path"><span><small>pk_spend → commitment</small>${code(recipient?.spendPublicKey)}</span></div><div class="binding-path"><span><small>pk_view → encrypted note</small>${code(recipient?.viewPublicKey)}</span></div></div>
    ${paymentDiagram(p, channel, input, amount, draft, null)}
    <div class="retail-conservation"><span>Input <b>${fmt(draft ? p.notes.find(n => n.id === draft.inputNoteId)?.amount : input?.amount || 0)}</b></span><i>=</i><span>Recipient <b>${fmt(draft?.amount ?? amount)}</b></span><i>+</i><span>Change <b>${fmt(draft?.change ?? Math.max(0, (input?.amount || 0) - amount))}</b></span><small>Amounts stay in the private witness.</small></div>
    <button class="button button-primary" data-action="payment" ${!channel || !input || amount <= 0 || amount > input.amount ? "disabled" : ""}>${last ? "Build and publish another payment" : "Build proof and publish payment"}</button>
    ${steps([["membership", "Prove membership", "Input → Merkle root"], ["outputs", "Create outputs", "Recipient + change"], ["prove", "Generate proof", "Ownership + conservation"], ["relay", "Relay payment", "EnygmaDvp.payment()"], ["verify", "Verify on-chain", "Groth16 + nullifier"], ["append", "Append leaves", "Two new commitments"], ["tag", "Publish private tag", "TagRegistry.publishTag()"]], active)}
    ${draft || last ? paymentReceipt(p, draft, draft ? null : last) : `<div class="retail-proof-preview"><span>π</span><div><strong>The chain verifies the proof before inserting either output.</strong><small>Membership · spend authority · value conservation · unused nullifier</small></div></div>`}
    </div><div class="retail-live-tree">${treePanel(p)}</div></div>${noteInventory(p)}</article>`;
}

function paymentReceipt(p, draft, tx) {
  const outputs = draft?.outputs || tx?.outputs || [];
  return `<section class="retail-publication"><header><span class="retail-proof-seal">${tx?.verified ? "✓" : "π"}</span><div><small>${tx ? `LATEST CONFIRMED PAYMENT · ${nameOf(p, tx.to).toUpperCase()} · PROOF VERIFIED` : "BUILDING PAYMENT"}</small><h3>${tx ? "Two commitments entered the vault tree" : "Private witness → public statement"}</h3></div></header><div class="retail-public-fields"><span><small>Input root</small>${code(draft?.root || tx?.root)}</span><span><small>Nullifier</small>${code(draft?.nullifier || tx?.nullifier)}</span>${outputs.map((o, i) => `<span><small>${i === 0 ? "Recipient" : "Change"} commitment</small>${code(o.commitment)}</span>`).join("")}</div><details class="retail-math"><summary>Inspect output commitment calculations</summary>${outputs.map((o, i) => `<div class="retail-output-formula"><strong>${i === 0 ? "Recipient output" : "Payer change"}</strong><code>C = Poseidon(pk_spend, salt, ${o.amount}, 0)</code><dl><dt>pk_spend</dt><dd>${o.spendPublicKey}</dd><dt>salt</dt><dd>${o.salt}</dd><dt>C</dt><dd>${o.commitment}</dd></dl></div>`).join("")}</details>${tx?.privateTag ? `<div class="retail-tag-window"><header><strong>Then: publish the private tag</strong><span>Poseidon(block_number, pk_spend, ss_field)</span></header><div>${tx.tagWindow?.map((t, i) => `<span class="${i === 0 ? "landed" : ""}"><small>Block ${t.block}${i === 0 ? " · PUBLISHED" : " · window candidate"}</small>${code(t.tag)}</span>`).join("") || code(tx.privateTag)}</div><p>The relayer publishes the tag for the landing block and an AES-256-GCM channel payload containing <code>amount · token_id · salt</code>.</p><a class="button button-ghost button-small" href="#/retail/scan" data-retail-scan-payment="${tx.id}">Scan for this payment →</a></div>` : ""}</section>`;
}

export function retailScanCard(p) {
  const s = retailState(p), tx = selectedPayment(p), channel = s.channels.find(c => c.id === tx?.tagChannelId);
  const scanned = tx?.scanned, active = s.activity?.kind === "scan" ? s.activity.step : null;
  const recipient = nameOf(p, tx?.to), note = p.notes.find(n => n.id === tx?.outputs?.[0]?.noteId);
  return `<article class="panel flow-card retail-workspace">${head("Recipient discovery · private wallet", "Find the tag. Open your note.", scanned ? "NOTE RECOVERED" : "SCAN INCOMING PAYMENTS")}<label class="retail-payment-select">Payment to scan<select id="retailScanPayment">${p.transactions.filter(t => t.type === "payment").map(t => `<option value="${t.id}" ${t.id === tx?.id ? "selected" : ""}>${nameOf(p, t.to)} · ${short(t.id)} · ${t.scanned ? "recovered" : "unread"}</option>`).join("") || `<option>No payments yet</option>`}</select></label>
    <div class="retail-scan-scene ${active ? "animating" : ""}"><div class="retail-radar"><i></i><i></i><i></i><b>${scanned ? "✓" : "⌕"}</b></div><div><small>${recipient.toUpperCase()}’S WALLET</small><h3>${scanned ? "Commitment matched" : "Looking for a matching private tag"}</h3><code>${short(tx?.privateTag, 22, 8)}</code><p>Discover the channel using the candidate bitmap, then derive its block tags locally. A match opens the encrypted note and identifies its commitment in the tree.</p></div></div>
    ${steps([["channel", "Discover channel", "Bitmap → ML-KEM → shared secret"], ["match", "Match block tag", "Poseidon(block, pk, ss)"], ["decrypt", "Open note data", "AES-GCM → amount, token, salt"], ["recover", "Recognize leaf", "Recompute C and match"]], active)}
    <div class="table-wrap scan-table"><table><thead><tr><th>#</th><th>Registered wallet</th><th>Bitmap</th><th>Channel discovery</th></tr></thead><tbody>${channel ? p.registrations.map((r, i) => `<tr class="${scanned && r.partyId === channel.recipientPartyId ? "scan-match" : ""}"><td>${i}</td><td>${r.name}</td><td><span class="compact-bitmap ${channel.candidateIndices.includes(i) ? "included" : "outside"}">${channel.candidateIndices.includes(i) ? 1 : 0}</span></td><td>${!channel.candidateIndices.includes(i) ? "Skip · bitmap bit 0" : r.partyId === channel.recipientPartyId ? scanned ? "Channel opened · tag matched · note recovered" : "Can open with sk_view" : scanned ? "Channel authentication rejected" : "Candidate decapsulation"}</td></tr>`).join("") : ""}</tbody></table></div>
    <button class="button button-primary" data-action="scan" ${!tx || scanned ? "disabled" : ""}>${scanned ? "Note recovered" : "Run recipient scan"}</button>
    ${scanned && note ? `<div class="retail-recovered">${noteCard(note, `${recipient}’s recovered note`, "recipient")}<div><strong>${fmt(note.amount)} USD added to ${recipient}’s private wallet</strong><code>C = Poseidon(pk_spend, salt, amount, token_id)</code><span>Recomputed commitment equals leaf ${note.leafIndex}.</span><small>The scan recognizes an existing leaf; it does not insert another one.</small></div></div>` : ""}${treePanel(p, tx?.to || "party-1")}</article>`;
}

export function retailChainCard(p) {
  const viewer = retailUI.viewer, notes = viewer === "public" ? [] : availableRetailNotes(p, viewer);
  return `<article class="panel flow-card retail-workspace">${head("Public chain & participant wallets", "Same tree. Different private openings.")}<div class="retail-viewer"><label>Open wallet<select id="retailViewer"><option value="public" ${viewer === "public" ? "selected" : ""}>Public chain</option>${p.registrations.map(r => `<option value="${r.partyId}" ${viewer === r.partyId ? "selected" : ""}>${r.name} · private wallet</option>`).join("")}</select></label><div><small>${viewer === "public" ? "PUBLIC DATA" : "SPENDABLE PRIVATE BALANCE"}</small><strong>${viewer === "public" ? `${p.leaves.length} commitments` : `${fmt(notes.reduce((a, n) => a + n.amount, 0))} USD`}</strong></div></div>${treePanel(p, viewer)}<div class="table-wrap"><table><thead><tr><th>Operation</th><th>Nullifier</th><th>Output commitments</th><th>Verification</th></tr></thead><tbody>${p.transactions.filter(t => t.outputs).map(t => `<tr><td>${t.type.includes("shield") ? "depositV2()" : "payment()"}<small>${short(t.id)}</small></td><td>${code(t.nullifier)}</td><td>${t.outputs.map(o => code(o.commitment)).join("<br>")}</td><td>${t.verified ? "Groth16 verified" : "Deposit confirmed"}</td></tr>`).join("")}</tbody></table></div></article>`;
}

export function retailPayload(action) {
  const read = id => document.getElementById(id)?.value;
  if (action === "mint-cash") return { amount: read("retailMintAmount") };
  if (action === "shield") return { totalAmount: read("retailShieldAmount"), amounts: Array.from(document.querySelectorAll("[data-retail-shield-note]"), input => input.value) };
  if (action === "payment") return { amount: read("retailPaymentAmount"), channelId: read("retailPaymentRecipient"), noteId: read("retailPaymentNote") };
  if (action === "scan") return { txId: read("retailScanPayment") };
  return {};
}
const inputFields = { retailMintAmount: "mintAmount", retailShieldAmount: "shieldAmount", retailPaymentAmount: "amount" };
export function retailInput(target, p) {
  if (target.matches("[data-retail-shield-note]")) retailUI.shieldAmounts[Number(target.dataset.retailShieldNote)] = target.value;
  else if (inputFields[target.id]) retailUI[inputFields[target.id]] = target.value;
  else return false;
  if (target.id === "retailPaymentAmount") return true;
  // Keep the input node and caret intact while updating only the preview.
  refreshShieldInputs(p);
  return false;
}
export function retailChange(target) {
  if (target.id === "retailTreeFollow") {
    retailUI.treeStart = Number(document.querySelector(".retail-tree-panel")?.dataset.treeStart || 0);
    retailUI.treeFollow = target.checked;
    retailUI.leafId = "";
    return true;
  }
  const fields = { retailPaymentRecipient: "channelId", retailPaymentNote: "noteId", retailShieldCount: "shieldCount", retailScanPayment: "txId", retailViewer: "viewer" };
  if (!fields[target.id]) return false;
  retailUI[fields[target.id]] = target.value;
  if (target.id === "retailShieldCount") {
    while (retailUI.shieldAmounts.length < Number(target.value)) retailUI.shieldAmounts.push("");
  }
  if (target.id === "retailViewer") retailUI.leafId = "";
  return true;
}
export function retailClick(target, p) {
  const pageSize = treePageSize();
  if (target.closest("[data-retail-tree-show]")) {
    const panel = document.querySelector(".retail-tree-panel");
    panel?.scrollIntoView({ block: "start", behavior: "instant" });
    panel?.querySelector(".retail-tree-scroll")?.focus({ preventScroll: true });
    return false;
  }
  const group = target.closest("[data-retail-tree-page]"), move = target.closest("[data-retail-tree-move]")?.dataset.retailTreeMove;
  if (group || move) {
    const start = Number(document.querySelector(".retail-tree-panel")?.dataset.treeStart || 0);
    const lastStart = Math.floor(Math.max(0, p.leaves.length - 1) / pageSize) * pageSize;
    retailUI.treeStart = Math.max(0, Math.min(lastStart, group ? Number(group.dataset.retailTreePage) : start + (move === "next" ? pageSize : -pageSize)));
    retailUI.treeFollow = false;
    retailUI.leafId = "";
    return true;
  }
  if (target.closest("[data-retail-split-evenly]")) {
    const total = Number(retailUI.shieldAmount), count = Number(retailUI.shieldCount);
    if (!validUSD(retailUI.shieldAmount) || total < count) return false;
    retailUI.shieldAmounts = Array.from({ length: count }, (_, i) => String(Math.floor(total / count) + (i < total % count ? 1 : 0)));
    return true;
  }
  const leaf = target.closest("[data-retail-leaf]")?.dataset.retailLeaf;
  if (leaf) {
    retailUI.leafId = leaf;
    const index = p.leaves.findIndex(item => item.id === leaf);
    if (index >= 0) { retailUI.treeStart = Math.floor(index / pageSize) * pageSize; retailUI.treeFollow = false; }
    return true;
  }
  const channel = target.closest("[data-retail-use-channel]")?.dataset.retailUseChannel;
  if (channel) { retailUI.channelId = channel; location.hash = "#/retail/payment"; return true; }
  const tx = target.closest("[data-retail-scan-payment]")?.dataset.retailScanPayment;
  if (tx) retailUI.txId = tx;
  return false;
}

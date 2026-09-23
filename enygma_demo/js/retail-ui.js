import { retailState, hasRetailChannel, availableRetailNotes, RETAIL_DEPTH } from "./retail.js";

export const retailUI = {
  channelDraft: { recipientIndex: 1, mode: "full", excludedIndices: new Set([8, 9]) },
  channelId: "", noteId: "", txId: "", amount: "30", shieldAmount: "100", shieldCount: "2", mintAmount: "1000", viewer: "public", leafId: ""
};
const short = (value, n = 12, end = 6) => value ? String(value).length > n + end + 1 ? `${String(value).slice(0, n)}…${String(value).slice(-end)}` : String(value) : "—";
const fmt = value => Number(value).toLocaleString("en-US");
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
  const tree = p.trees?.USD, leaves = p.leaves, count = leaves.length;
  const depth = Math.max(1, Math.ceil(Math.log2(Math.max(2, count))));
  const slots = 2 ** depth, width = Math.max(440, slots * 114), height = 134 + depth * 90;
  const newest = leaves.at(-1)?.sourceTxId;
  const known = (p.notes || []).filter(n => n.ownerPartyId === viewer && n.discovered !== false);
  const owned = new Set(known.map(n => n.leafId));
  const selection = leaves.find(l => l.id === retailUI.leafId) || leaves.find(l => l.id === known.at(-1)?.leafId) || leaves.at(-1);
  const opening = known.find(n => n.leafId === selection?.id);
  const levelY = level => 112 + (depth - level) * 90;
  const nodeX = (level, index) => width * ((index + .5) * 2 ** level / slots);
  let paths = "", nodes = "";
  for (let level = depth; level >= 0; level--) {
    const nodeCount = Math.ceil(Math.max(count, 2) / 2 ** level);
    for (let i = 0; i < nodeCount; i++) {
      const leaf = level === 0 ? leaves[i] : null;
      const value = level === 0 ? leaf?.commitment : tree?.levels[level]?.[i];
      const x = nodeX(level, i), y = levelY(level);
      const latest = level === 0 ? leaf?.sourceTxId === newest : count > 0 && i === ((count - 1) >> level);
      const note = known.find(n => n.leafId === leaf?.id);
      if (level < depth) paths += `<path class="${latest ? "new-path" : ""}" d="M ${nodeX(level + 1, Math.floor(i / 2))} ${levelY(level + 1) + 23} V ${y - 34} H ${x} V ${y - 23}"/>`;
      const title = level === 0 ? leaf ? `Leaf ${i}${note ? note.status === "spent" ? " · spent" : " · yours" : ""}` : "Empty leaf" : `Poseidon · L${level}`;
      nodes += `<g class="retail-tree-node ${leaf && owned.has(leaf.id) ? "owned-leaf" : ""} ${note?.status === "spent" ? "spent-leaf" : ""} ${latest ? "inserted" : ""} ${!value ? "empty-leaf" : ""} ${selection?.id === leaf?.id && leaf ? "selected-leaf" : ""}" transform="translate(${x},${y})" ${leaf ? `data-retail-leaf="${leaf.id}" data-owned="${owned.has(leaf.id)}" data-source-tx="${leaf.sourceTxId}" role="button" tabindex="0" aria-label="Inspect leaf ${i}${owned.has(leaf.id) ? ", recognized by your wallet" : ", commitment"}"` : ""}><title>${value || "Empty subtree"}</title><rect x="-51" y="-24" width="102" height="48" rx="9"/><text y="-5">${title}</text><text y="12" class="hash">${short(value, 6, 4)}</text></g>`;
    }
  }
  const empty = count === 0;
  return `<section class="retail-tree-panel" data-tree-asset="USD" data-merkle-root="${tree?.root || ""}"><div class="retail-network-head"><div><p class="eyebrow">Live commitment tree · Erc20CoinVault</p><h3>${empty ? "Your first note starts here" : `${count} commitments, one shared tree`}</h3></div>${showTraffic ? `<label class="retail-traffic-control"><span class="traffic-dot ${p.traffic ? "on" : ""}"></span><span>Network traffic</span><span class="switch"><input type="checkbox" data-traffic ${p.traffic ? "checked" : ""} aria-label="Network traffic"><span></span></span></label>` : ""}</div><div class="retail-tree-legend"><span><i class="purple"></i>${viewer === "public" ? "Public chain · ownership hidden" : `${viewer === "party-0" ? "Your" : `${nameOf(p, viewer)}’s`} notes (${known.length})`}</span><span><i class="gold"></i>New commitments</span><span><i class="grey"></i>Other / empty leaves</span><b>Depth ${RETAIL_DEPTH} · ${count} / 256 leaves</b></div>
    <div class="retail-tree-scroll"><svg class="retail-tree-svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" aria-label="USD commitment tree. ${count} leaves. Select a leaf to inspect it."><g class="retail-tree-edges"><path class="${count ? "new-path" : ""}" d="M ${width / 2} 50 V 75 H ${nodeX(depth, 0)} V 88"/>${paths}</g><g class="retail-tree-root" transform="translate(${width / 2},26)"><rect x="-136" y="-24" width="272" height="48" rx="12"/><text y="-5">MERKLE ROOT · DEPTH 8</text><text y="12" class="hash">${tree ? short(tree.root, 18, 8) : "Waiting for the first deposit"}</text></g>${nodes}${depth < RETAIL_DEPTH ? `<text class="retail-collapsed-path" x="${nodeX(depth, 0) + 65}" y="77">${RETAIL_DEPTH - depth} upper levels folded</text>` : ""}</svg></div>
    <div class="retail-leaf-inspector" data-leaf-inspector>${selection ? `<div><small>SELECTED · LEAF ${leaves.indexOf(selection)}</small><strong>${opening ? `${opening.ownerName} · ${fmt(opening.amount)} USD` : "Sealed note commitment"}</strong><code>${selection.commitment}</code></div><div><small>${opening ? "PRIVATE WALLET OPENING" : "PUBLIC DATA"}</small>${opening ? `<span>Salt ${short(opening.salt, 13, 6)}</span><span>${opening.status === "spent" ? "Spent · nullifier published" : "Unspent note"}</span>` : `<span>Owner and amount hidden</span>`}<span>Source ${short(selection.sourceTxId, 13, 6)}</span></div>` : `<div><small>EMPTY TREE</small><strong>Shield a note to watch its commitment arrive.</strong><span>Each additional note gets a separate leaf.</span></div>`}</div>
    ${count > 5 ? `<div class="retail-tree-hint">↔ Scroll the tree to inspect every commitment. Your notes stay purple as the tree grows.</div>` : ""}
    <div class="retail-chain-ticker" aria-label="Recent tree insertions">${p.transactions.filter(t => p.leaves.some(l => l.sourceTxId === t.id)).slice(0, 3).map(t => `<span><b>${t.background ? "NETWORK" : "YOU"}</b>${t.type.includes("shield") ? "Deposit" : "Payment"}<code>${short(t.id, 9, 4)}</code><strong>+${p.leaves.filter(l => l.sourceTxId === t.id).length} leaves</strong></span>`).join("") || `<span>Deposits and payments will appear here as they enter the tree.</span>`}</div></section>`;
}

function walletStrip(p) {
  const s = retailState(p), notes = availableRetailNotes(p);
  return `<div class="retail-wallet-strip"><div><span>Public USD</span><strong>${fmt(s.publicBalance)}</strong><small>Available to shield</small></div><b>→</b><div class="private"><span>Your private USD</span><strong>${fmt(notes.reduce((sum, n) => sum + n.amount, 0))}</strong><small>${notes.length} spendable notes</small></div><div><span>Network tree</span><strong>${p.leaves.length}</strong><small>Commitment leaves</small></div></div>`;
}
function noteInventory(p) {
  const notes = (p.notes || []).filter(n => n.ownerPartyId === "party-0");
  return `<section class="retail-inventory"><div class="retail-section-heading"><h3>Your notes</h3><span>Every note remains visible after later deposits and payments.</span></div><div class="retail-note-inventory">${notes.map(n => `<button type="button" data-retail-leaf="${n.leafId}" class="retail-note-select ${n.status === "spent" ? "spent" : ""}">${noteCard(n, n.origin === "change" ? "Payment change" : "Shielded note")}</button>`).join("") || noteCard(null, "Shield your first note", "placeholder")}</div></section>`;
}

export function retailShieldCard(p) {
  const s = retailState(p), active = s.activity?.kind === "shield" ? s.activity : null;
  const latest = active?.output || [...p.notes].reverse().find(n => n.origin === "shielding" && n.ownerPartyId === "party-0");
  const amount = Number(retailUI.shieldAmount) || 0, count = Number(retailUI.shieldCount);
  return `<article class="panel flow-card retail-workspace">${head("Fund your wallet · shield into private notes", "Watch your money become commitments", active ? `NOTE ${active.index} / ${active.count}` : "PAYER · YOU")}<div class="retail-live-layout"><div class="retail-action-pane">${walletStrip(p)}
    <div class="retail-shield-scene ${active ? `animating phase-${active.step}` : ""}" aria-label="Shielding: public tokens enter the vault and produce private note commitments"><div class="retail-token-source"><div class="retail-coins"><i>$</i><i>$</i><i>$</i></div><strong>Public USD</strong><small>Approve vault spending</small></div><div class="retail-flow-track"><i></i><i></i><i></i><span>depositV2()</span></div><div class="retail-vault"><span>▥</span><strong>Erc20CoinVault</strong><small>Tokens held in custody</small></div><div class="retail-flow-track"><i></i><i></i><i></i><span>Poseidon</span></div><div class="retail-output-stack">${Array.from({ length: count }, (_, i) => `<div class="retail-mini-note" style="--i:${i}"><span>PRIVATE NOTE ${i + 1}</span><strong>${fmt(amount)} USD</strong><code>C${i + 1} → leaf</code></div>`).join("")}</div></div>
    <div class="retail-funding-controls"><label>Public funds to allocate<input id="retailMintAmount" type="number" min="1" step="1" value="${retailUI.mintAmount}"></label><button class="button button-ghost" data-action="mint-cash">Allocate USD</button><small>Issuer → RaylsERC20.mint()</small></div>
    <div class="retail-form"><label>USD per note<input id="retailShieldAmount" type="number" min="1" step="1" value="${retailUI.shieldAmount}"></label><label>Number of notes<select id="retailShieldCount">${[1, 2, 3, 4].map(n => `<option value="${n}" ${n === count ? "selected" : ""}>${n} ${n === 1 ? "note" : "notes"}</option>`).join("")}</select></label><button class="button button-primary" data-action="shield" ${s.publicBalance < amount * count || amount <= 0 ? "disabled" : ""}>Shield ${count} ${count === 1 ? "note" : "notes"}</button></div>
    ${steps([["derive", "Derive salt & key", "ML-KEM-768 → HKDF-SHA256"], ["commit", "Commit each note", "Poseidon(pk, salt, amount, token)"], ["insert", "Append to tree", "depositV2() → Commitment"]], active?.step)}
    <details class="retail-math" ${latest ? "open" : ""}><summary>Commitment calculation · token_id = 0 (USD)</summary><div class="retail-hash-equation"><span><small>pk_spend</small>${code(p.registrations[0].spendPublicKey)}</span><b>+</b><span><small>salt</small>${code(latest?.salt)}</span><b>+</b><span><small>amount</small><strong>${latest?.amount ?? amount}</strong></span><b>+</b><span><small>token_id</small><strong>0</strong></span><i>→ Poseidon →</i><span class="hash-result"><small>C</small>${code(latest?.commitment)}</span></div><p>ML-KEM encapsulates to your view public key. HKDF-SHA256 derives the salt and encryption key; AES-256-GCM encrypts the note data. Each deposit adds a separate commitment.</p></details>
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
  if (action === "shield") return { amount: read("retailShieldAmount"), count: read("retailShieldCount") };
  if (action === "payment") return { amount: read("retailPaymentAmount"), channelId: read("retailPaymentRecipient"), noteId: read("retailPaymentNote") };
  if (action === "scan") return { txId: read("retailScanPayment") };
  return {};
}
const inputFields = { retailMintAmount: "mintAmount", retailShieldAmount: "shieldAmount", retailPaymentAmount: "amount" };
export function retailInput(target) {
  if (!inputFields[target.id]) return false;
  retailUI[inputFields[target.id]] = target.value;
  return true;
}
export function retailChange(target) {
  const fields = { retailPaymentRecipient: "channelId", retailPaymentNote: "noteId", retailShieldCount: "shieldCount", retailScanPayment: "txId", retailViewer: "viewer" };
  if (!fields[target.id]) return false;
  retailUI[fields[target.id]] = target.value;
  if (target.id === "retailViewer") retailUI.leafId = "";
  return true;
}
export function retailClick(target) {
  const leaf = target.closest("[data-retail-leaf]")?.dataset.retailLeaf;
  if (leaf) { retailUI.leafId = leaf; return true; }
  const channel = target.closest("[data-retail-use-channel]")?.dataset.retailUseChannel;
  if (channel) { retailUI.channelId = channel; location.hash = "#/retail/payment"; return true; }
  const tx = target.closest("[data-retail-scan-payment]")?.dataset.retailScanPayment;
  if (tx) retailUI.txId = tx;
  return false;
}

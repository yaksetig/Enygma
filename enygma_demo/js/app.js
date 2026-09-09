import { PROTOCOLS, PROTOCOL_IDS, SETUP_STEPS, PARTY_NAMES } from "./config.js";
import { engineAdapter } from "./demo-engine.js";

const $ = selector => document.querySelector(selector);
const els = {
  chooser: $("#chooser"), workspace: $("#workspace"), grid: $("#protocolGrid"), nav: $("#protocolNav"),
  identity: $("#identityChip"), title: $("#protocolTitle"), kicker: $("#protocolKicker"), description: $("#protocolDescription"),
  progress: $("#progress"), setupWorkspace: $("#setupWorkspace"), stage: $("#stageContent"), stagePanel: $("#stagePanel"), activity: $("#activityList"),
  experience: $("#protocolExperience"), toasts: $("#toastRegion")
};

let route = "choose";
let busy = false;

function short(value, lead = 15, tail = 8) {
  if (!value) return "—";
  return value.length <= lead + tail + 1 ? value : `${value.slice(0, lead)}…${value.slice(-tail)}`;
}

function toast(message) {
  const item = document.createElement("div");
  item.className = "toast";
  item.textContent = message;
  els.toasts.append(item);
  setTimeout(() => item.remove(), 3000);
}

function routeFromHash() {
  const candidate = location.hash.replace(/^#\//, "").split("/")[0];
  return PROTOCOL_IDS.includes(candidate) ? candidate : "choose";
}

function screenFromHash() {
  return location.hash.replace(/^#\//, "").split("/")[1] || "";
}

function setupIndex(p) {
  return engineAdapter.isReady(p.id) ? 5 : p.stage;
}

function renderNav() {
  els.nav.innerHTML = PROTOCOL_IDS.map(id => `<a href="#/${id}" class="${route === id ? "active" : ""}">${PROTOCOLS[id].short}</a>`).join("");
  const identity = engineAdapter.state.identities[0];
  els.identity.classList.toggle("ready", Boolean(identity));
  els.identity.innerHTML = `<span class="status-dot"></span>${identity ? short(identity.spendPublicKey, 12, 6) : "No identity yet"}`;
}

function renderChooser() {
  els.grid.innerHTML = PROTOCOL_IDS.map(id => {
    const item = PROTOCOLS[id];
    const p = engineAdapter.protocol(id);
    const ready = engineAdapter.isReady(id);
    const status = ready ? "Ready" : p.stage ? `Setup ${Math.min(p.stage + 1, 5)} of 5` : "Not started";
    return `<a class="protocol-card" href="#/${id}">
      <span class="protocol-icon">${item.icon}</span>
      <h2>${item.name}</h2><p>${item.description}</p>
      <footer><span class="${ready ? "completion" : ""}">${status}</span><span aria-hidden="true">→</span></footer>
    </a>`;
  }).join("");
}

function renderProgress(p) {
  const index = setupIndex(p);
  els.progress.innerHTML = SETUP_STEPS.map(([name, detail], i) => {
    const state = i < index ? "complete" : i === index ? "active" : "";
    return `<li class="${state}" ${i === index ? 'aria-current="step"' : ""}><span class="progress-number">${i < index ? "✓" : i + 1}</span>${name}<span>${detail}</span></li>`;
  }).join("");
}

function stageHero(number, actor, title, copy) {
  const actors = {
    operator: ["OP", "Executing entity", "System operator"],
    auditor: ["AU", "Executing entity", "Auditor"],
    participant: ["ID", "Executing entity", "Protocol participant"],
    registrants: ["P", "Executing entity", "Registering participant"],
    system: ["✓", "Current state", "Protocol ready"]
  };
  const [symbol, label, name] = actors[actor];
  const stepLabel = number === "✓" ? "Setup complete" : `Setup step ${number}`;
  return `<div class="stage-hero"><div class="stage-copy"><p class="eyebrow">${stepLabel}</p><h2>${title}</h2><p class="panel-copy">${copy}</p></div><div class="stage-context"><div class="actor-box actor-${actor}" data-executing-entity="${name}"><span class="actor-symbol" aria-hidden="true">${symbol}</span><span><small>${label}</small><strong>${name}</strong></span></div><span class="stage-number">${number}</span></div></div>`;
}

function renderDeploy(p) {
  const contracts = PROTOCOLS[p.id].contracts.map(name => `<div class="detail-card"><label>Protocol contract</label><strong>${name}</strong></div>`).join("");
  return `${stageHero(1, "operator", "Deploy the protocol suite", "Bring this protocol’s independent environment online and establish its core system contracts.")}
    <div class="stage-body"><div class="detail-grid">${contracts}</div><button class="button button-primary" data-command="deploy">Deploy contract suite</button></div>`;
}

function renderAuditor(p) {
  return `${stageHero(2, "auditor", "Generate the auditor key", "Create a protocol-specific ML-KEM keypair. This key can reveal only the audit scope participants grant; it never grants spending, freezing, or operator authority.")}
    <div class="stage-body"><div class="callout">The auditor generates and retains this keypair. The system operator receives only the public key for configuration.</div><div class="button-row" style="margin-top:18px"><button class="button button-auditor" data-command="auditor">Generate ML-KEM keypair</button></div></div>`;
}

function renderConfigure(p) {
  return `${stageHero(3, "operator", "Register the auditor public key", "The operator writes the auditor’s public key into this protocol’s independent system configuration.")}
    <div class="stage-body"><div class="detail-card"><label>Auditor public key</label><span class="key-value">${p.auditor.publicKey}</span></div><div class="button-row" style="margin-top:18px"><button class="button button-primary" data-command="configure">Set auditor key in system</button></div></div>`;
}

function renderIdentity(p) {
  const identity = engineAdapter.state.identities[0];
  const ceremony = engineAdapter.state.identityCeremony;
  const phase = identity ? "complete" : ceremony.phase;
  const rank = { empty: 0, spend_secret: 1, spend_public: 2, view_secret: 3, complete: 4 }[phase];
  const spendSecret = ceremony.spendPrivateKey || "Waiting for participant key generation";
  const viewSecret = ceremony.viewPrivateKey || "Waiting for participant key generation";
  const spendPublic = ceremony.spendPublicKey || "Waiting for H(sk_spend)";
  const viewPublic = ceremony.viewPublicKey || "Waiting for ML-KEM key generation";
  const button = phase === "empty"
    ? `<button class="button button-primary" data-command="identity-spend-secret">Generate spend secret key</button>`
    : phase === "spend_secret"
      ? `<button class="button button-primary" data-command="identity-spend-public">Hash spend secret key</button>`
      : phase === "spend_public"
        ? `<button class="button button-primary" data-command="identity-view-secret">Generate view secret key</button>`
        : phase === "view_secret"
          ? `<button class="button button-primary" data-command="identity-view-public">Generate view public key</button>`
          : `<button class="button button-primary" data-command="identity-confirm">Continue to registration</button>`;
  const footerCopy = phase === "empty" ? "Begin with the private spend key that authorizes transactions."
    : phase === "spend_secret" ? "Spend secret ready. Hash it to obtain the corresponding public key."
      : phase === "spend_public" ? "Spend keypair complete. Now create the private viewing key."
        : phase === "view_secret" ? "View secret ready. Complete the ML-KEM keypair with its public key."
          : "Both keypairs are complete. Private keys never enter the public registry.";
  const progress = ["Spend secret", "Spend public", "View secret", "View public"].map((label, index) => `<span class="${rank > index ? "done" : rank === index ? "active" : ""}">${index + 1} · ${label}</span>${index < 3 ? "<i></i>" : ""}`).join("");
  return `${stageHero(4, "participant", identity ? "Confirm your existing Enygma identity" : "Generate your Enygma identity", identity ? "Review the same participant-generated keys before registering them unchanged in this protocol." : "Build the identity one key at a time. Each secret appears before the operation that produces its corresponding public key.")}
    <div class="stage-body key-ceremony" data-ceremony-phase="${phase}">
      <div class="ceremony-progress" aria-label="Identity generation progress">${progress}</div>
      <div class="ceremony-grid">
        <article class="ceremony-card spend-card">
          <header><span class="ceremony-icon">S</span><div><p class="eyebrow">Spend authorization</p><h3>Hash-based spend keypair</h3></div></header>
          <div class="key-output secret-output ${rank >= 1 ? "is-ready" : ""}"><label>Private · sk_spend</label><span class="key-value">${spendSecret}</span><small>Generated and retained by the participant.</small></div>
          <div class="operation-lane ${rank >= 2 ? "is-complete" : ""}" aria-label="Hash the spend secret key to obtain the public key"><code>sk_spend</code><span class="operation-arrow"><b>H(·)</b>→</span><code>pk_spend</code></div>
          <div class="key-output public-output ${rank >= 2 ? "is-ready" : ""}"><label>Public · pk_spend</label><span class="key-value">${spendPublic}</span><small>The spend public key is obtained by hashing the spend secret key.</small></div>
        </article>
        <article class="ceremony-card view-card">
          <header><span class="ceremony-icon">V</span><div><p class="eyebrow">Transaction viewing</p><h3>ML-KEM view keypair</h3></div></header>
          <div class="key-output secret-output ${rank >= 3 ? "is-ready" : ""}"><label>Private · sk_view</label><span class="key-value">${viewSecret}</span><small>Kept private unless the selected audit policy grants access.</small></div>
          <div class="operation-lane ${rank >= 4 ? "is-complete" : ""}" aria-label="ML-KEM key generation binds the view keypair"><code>sk_view</code><span class="operation-arrow"><b>ML-KEM</b>→</span><code>pk_view</code></div>
          <div class="key-output public-output ${rank >= 4 ? "is-ready" : ""}"><label>Public · pk_view</label><span class="key-value">${viewPublic}</span><small>ML-KEM key generation binds the private decapsulation key to its public encapsulation key.</small></div>
        </article>
      </div>
      <div class="ceremony-footer"><p>${footerCopy}</p>${button}</div>
    </div>`;
}

function registryKey(label, value) {
  return `<details class="registry-key"><summary title="Show full ${label}">${short(value, 18, 10)}</summary><code>${value}</code></details>`;
}

function registryTable(p, indices) {
  const rows = indices.map(index => {
    const identity = engineAdapter.state.identities[index];
    const registration = p.registrations[index];
    const name = PARTY_NAMES[index];
    return `<tr data-party-row="${index}">
      <td><div class="registry-party"><span class="avatar">${name.split(" ").map(v => v[0]).slice(0,2).join("")}</span><span><strong>${name}</strong>${index === 0 ? "<small>You</small>" : ""}</span></div></td>
      <td data-key="spend">${registryKey("spend public key", identity.spendPublicKey)}</td>
      <td data-key="view">${registryKey("view public key", identity.viewPublicKey)}</td>
      <td><span class="registry-status ${registration ? "registered" : "pending"}">${registration ? "✓ Keys registered" : index === p.registrations.length ? "Registering" : "Queued"}</span></td>
      <td><span class="registry-status ${registration?.auditEnvelope ? "registered" : "pending"}">${registration?.auditEnvelope ? "✓ Shared" : registration ? "Ready to share" : "Waiting"}</span></td>
    </tr>`;
  }).join("");
  return `<div class="registry-table-wrap"><table class="registry-table"><thead><tr><th>Participant</th><th>Spend public key</th><th>View public key</th><th>1 · Register keys</th><th>2 · Share with auditor</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

function registrationProcess(p) {
  const keysRegistered = p.registrations.length;
  const keysShared = p.registrations.filter(item => item.auditEnvelope).length;
  return `<div class="registration-process" aria-label="Two-step registration process">
    <div class="process-step ${keysRegistered ? "complete" : "active"}"><span>1</span><div><strong>Register public keys</strong><small>Publish <code>pk_spend</code> and <code>pk_view</code> to the participant registry.</small></div><b>${keysRegistered} / ${PARTY_NAMES.length}</b></div>
    <span class="process-connector" aria-hidden="true">→</span>
    <div class="process-step auditor-step ${keysShared === PARTY_NAMES.length ? "complete" : keysRegistered > keysShared ? "active" : ""}"><span>2</span><div><strong>Share with auditor</strong><small>Encrypt <code>sk_view</code> to the configured auditor public key.</small></div><b>${keysShared} / ${PARTY_NAMES.length}</b></div>
  </div>`;
}

function renderRegistration(p) {
  const next = p.registrations.length;
  const completed = p.registrations.filter(item => item.auditEnvelope).length;
  const visibleIndices = p.registrationBatchStarted ? PARTY_NAMES.map((_, index) => index) : [0];
  const tableLabel = p.registrationBatchStarted ? "Protocol registry" : "Your registration";
  const tableStatus = p.registrationBatchStarted ? `${completed} / ${PARTY_NAMES.length} complete` : p.registrations[0]?.auditEnvelope ? "Complete" : next ? "Step 2 of 2" : "Step 1 of 2";
  const participant = `<div class="registration-group"><div class="registration-label"><span>${tableLabel}</span><strong>${tableStatus}</strong></div>${registryTable(p, visibleIndices)}</div>`;
  let remainder;
  if (next === 0) {
    remainder = `<div class="registration-gate"><div><p class="eyebrow">Step 1 · Participant registry</p><h3>Register your public keys</h3><p>Publish the spend and view public keys shown in the table. No private key is included in this operation.</p></div><button class="button button-primary" data-command="register-participant-keys">Register public keys</button></div>`;
  } else if (!p.registrations[0].auditEnvelope) {
    remainder = `<div class="registration-gate"><div><p class="eyebrow">Step 2 · Long-term auditing</p><h3>Share your view key with the auditor</h3><p>Encrypt <code>sk_view</code> to the configured auditor public key. The spend secret key is never shared.</p></div><button class="button button-auditor" data-command="share-participant-key">Share view key with auditor</button></div>`;
  } else if (!p.registrationBatchStarted) {
    remainder = `<div class="registration-gate ready"><div><p class="eyebrow">Participant registered</p><h3>Bring the other parties online</h3><p>Nine additional parties will register together, each with the same long-term audit policy.</p></div><button class="button button-primary" data-command="register-others">Register others</button></div>`;
  } else {
    remainder = `<div class="callout success-callout">The remaining parties are registering in order. Their spend and view public keys remain visible in the registry above.</div>`;
  }
  return `${stageHero(5, "registrants", "Register protocol participants", "Each participant first registers both public keys, then separately encrypts and shares the view key with the auditor.")}
    <div class="stage-body registration-flow">${registrationProcess(p)}${participant}${remainder}</div>`;
}

function renderStage(p) {
  if (p.stage === 0) els.stage.innerHTML = renderDeploy(p);
  else if (p.stage === 1) els.stage.innerHTML = renderAuditor(p);
  else if (p.stage === 2) els.stage.innerHTML = renderConfigure(p);
  else if (p.stage === 3) els.stage.innerHTML = renderIdentity(p);
  else els.stage.innerHTML = renderRegistration(p);
}

function renderActivity(p) {
  if (!p.ledger.length) {
    els.activity.innerHTML = `<div class="activity-empty">No protocol activity yet.<br>Deployment will create the first receipt.</div>`;
    return;
  }
  els.activity.innerHTML = p.ledger.map(item => `<article class="activity-item ${item.audit ? "audit" : ""}"><strong>${item.label}</strong><p>block ${item.block} · ${short(item.hash, 13, 7)}</p></article>`).join("");
}

function auditCard(p) {
  const longTerm = p.registrations.filter(r => r.policy === "long_term").length;
  const transactions = p.transactions.filter(tx => tx.encryptedPayload && (p.id !== "dvp" || tx.encryptedPayload.scope === "dvp_leg")).slice(0, 6);
  return `<article class="panel flow-card wide"><div class="panel-heading"><div><p class="eyebrow">Audit access</p><h2>Auditor and regulator access</h2></div><span class="context-badge">AUDIT SCOPE</span></div>
    <p>During registration, each participant encrypted its <code>sk_view</code> to the auditor. That long-term path lets the auditor recover the key for encrypted note data involving that participant. The additional regulator instead receives only a selected transaction’s symmetric note-data key.</p><div class="callout audit-model"><strong>One key per encrypted payload.</strong> Each row corresponds to note data <code>salt · token_id · amount</code> appended in encrypted form alongside a commitment <code>H(pk_spend, salt, token_id, amount)</code>. Proposal, acceptance, transfer identity, and settlement receipts do not create extra disclosure keys.</div>
    <div class="metric-row"><div class="metric"><span>Encrypted view keys</span><strong>${longTerm}</strong></div><div class="metric"><span>Encrypted ${p.id === "dvp" ? "trade legs" : "payloads"}</span><strong>${transactions.length}</strong></div><div class="metric"><span>Selective keys shared</span><strong>${p.disclosures.length}</strong></div></div>
    <div class="regulator-row"><div><span class="avatar">AR</span><div><strong>${p.selectiveRegulator.name}</strong><span class="mono">${short(p.selectiveRegulator.publicKey, 18, 8)}</span></div></div><span class="policy-badge selective">Selective only</span></div>
    <div class="transaction-list">${transactions.length ? transactions.map(tx => `<div class="transaction auditable-transaction"><div><strong>${tx.encryptedPayload.leg === "cash" ? "Cash leg" : tx.encryptedPayload.leg === "security" ? "Security leg" : tx.label}</strong><small>${formatAmount(tx.encryptedPayload.amount)} ${tx.encryptedPayload.tokenId} · ${tx.id}</small></div><span><b>${short(tx.encryptedPayload.commitment, 18, 8)}</b><small>encrypted: salt · token_id · amount</small></span>${tx.selectiveShared ? `<span class="policy-badge selective">Symmetric key shared</span>` : `<button class="button button-auditor button-small" data-share="${tx.id}">Disclose symmetric key</button>`}</div>`).join("") : `<div class="empty-state">No transaction carrying encrypted note data is available for selective disclosure yet.</div>`}</div>
  </article>`;
}

function publicChainCard(p) {
  const treeIds = Object.keys(p.trees || {});
  return `<article class="panel flow-card full"><div class="panel-heading"><div><p class="eyebrow">Public chain view</p><h2>Commitment leaves</h2></div><span>${p.leaves.length} leaves</span></div>
    <p class="panel-copy">Each asset has its own commitment tree and root. Every leaf below was produced by a source transaction. The public view cannot attribute a leaf to a participant; scenario provenance remains available only as internal test state. Privacy comes from proving knowledge of an opening and membership of some leaf without revealing which leaf.</p>
    <div class="asset-tree-stack">${treeIds.length ? treeIds.map(assetId => commitmentTree(p, assetId)).join("") : commitmentTree(p)}</div>
  </article>`;
}

function trafficSwitch(p, assetId = null) {
  const on = p.traffic && (p.id !== "dvp" || !assetId || p.trafficAsset === assetId);
  const activeCopy = p.id === "retail" ? "Registered background participants are making private payments; each payment appends recipient and change commitments." : `Registered background participants are shielding ${assetId || p.flow.securityAssetId}; each transaction appends one commitment to that asset’s tree.`;
  return `<div class="switch-row"><div><strong>${on ? "Network traffic on" : "Network traffic off"}</strong><div class="panel-copy">${on ? activeCopy : "No background transaction or tree insertion occurs."}</div></div><label class="switch"><input type="checkbox" data-traffic ${assetId ? `data-traffic-asset="${assetId}"` : ""} ${on ? "checked" : ""} aria-label="Network traffic${assetId ? ` for ${assetId}` : ""}"><span></span></label></div>`;
}

function commitmentTree(p, assetId = null, ownerPartyId = null) {
  const tree = assetId ? p.trees?.[assetId] : Object.values(p.trees || {})[0];
  const resolvedAsset = assetId || tree?.assetId || "Asset";
  const levels = tree?.levels || [];
  const leafIds = new Set(tree?.leafIds || []);
  const ownedLeafIds = new Set((p.notes || []).filter(note => note.ownerPartyId === ownerPartyId && note.assetId === resolvedAsset && note.status !== "spent").map(note => note.leafId));
  const ownerName = p.registrations.find(item => item.partyId === ownerPartyId)?.name || "Participant";
  const leaves = p.leaves.filter(leaf => leafIds.has(leaf.id));
  if (!leaves.length || !levels.length) return `<div class="commitment-tree empty" data-tree-asset="${resolvedAsset}"><div class="tree-heading"><div><p class="eyebrow">${resolvedAsset} Merkle tree</p><strong>Independent asset root</strong></div></div><div class="empty-state">This asset tree is empty. A shielding transaction creates its first leaf.</div></div>`;
  const topDown = [...levels].reverse();
  const newestSource = leaves.at(-1)?.sourceTxId;
  const minWidth = Math.max(620, leaves.length * 94);
  const renderedLevels = topDown.map((level, levelIndex) => {
    const isRoot = levelIndex === 0;
    const isLeaves = levelIndex === topDown.length - 1;
    const nodes = level.map((value, index) => {
      const leaf = isLeaves ? leaves[index] : null;
      const onNewestPath = index === level.length - 1;
      const isOwned = Boolean(leaf && ownedLeafIds.has(leaf.id));
      const label = isRoot ? "Current root" : isLeaves ? `Leaf ${index}` : `Level ${topDown.length - levelIndex - 1}`;
      return `<div class="tree-node ${isRoot ? "root" : isLeaves ? "leaf-node" : "branch"} ${onNewestPath ? "new-path" : ""} ${isOwned ? "owned-leaf" : ""}" ${leaf ? `data-source-tx="${leaf.sourceTxId}" data-owned="${isOwned}"` : ""}><small>${label}</small><strong>${short(value, isRoot ? 20 : 12, 7)}</strong>${leaf ? `<span>source ${short(leaf.sourceTxId, 10, 5)}</span>${isOwned ? `<em>${ownerName} controls this leaf</em>` : ""}` : ""}</div>`;
    }).join("");
    return `<div class="tree-level ${isLeaves ? "leaf-level" : ""}" style="--node-count:${level.length}">${nodes}</div>`;
  }).join("");
  return `<div class="commitment-tree" data-tree-asset="${resolvedAsset}" data-merkle-root="${tree.root}"><div class="tree-heading"><div><p class="eyebrow">${resolvedAsset} Merkle tree</p><strong>${leaves.length} transaction-backed ${leaves.length === 1 ? "leaf" : "leaves"} · ${ownedLeafIds.size} controlled by ${ownerName}</strong></div><div class="tree-legend"><span class="tree-owner-key"><i></i>${ownerName}’s leaves</span><span class="tree-insertion-key"><i></i>Newest insertion path</span></div></div><div class="tree-scroll"><div class="tree-canvas" style="min-width:${minWidth}px">${renderedLevels}</div></div><div class="tree-insertion-receipt"><span>Latest insertion</span><strong>${short(newestSource, 18, 8)}</strong><small>Commitment appended → ${resolvedAsset} root updated</small></div></div>`;
}

function shieldingNetwork(p, assetId, ownerPartyId) {
  const leafCount = p.trees?.[assetId]?.leafIds.length || 0;
  return `<section class="shielding-network"><div class="shielding-network-head"><div><p class="eyebrow">Live ${assetId} network context</p><h3>Watch commitments enter this asset’s tree</h3></div><span class="context-badge">${leafCount} ${assetId} LEAVES</span></div><p>Every purple leaf is controlled by the participant executing this shielding step and remains highlighted after later insertions. Background commitments stay neutral; cash and bond commitments never share a root.</p>${trafficSwitch(p, assetId)}${commitmentTree(p, assetId, ownerPartyId)}</section>`;
}

function privateNoteConstruction(p, ownerPartyId, assetId) {
  const notes = (p.notes || []).filter(note => note.ownerPartyId === ownerPartyId && note.assetId === assetId && note.origin === "shielding");
  const latest = notes.at(-1);
  const owner = p.registrations.find(item => item.partyId === ownerPartyId);
  const construction = latest ? `<div class="commitment-construction" data-note-id="${latest.id}">
      <div><span>1</span><small>Fresh salt</small><strong class="mono">${short(latest.salt, 18, 8)}</strong></div>
      <div><span>2</span><small>Note fields</small><strong>${formatAmount(latest.amount)} ${latest.assetId}</strong></div>
      <div><span>3</span><small>Commitment</small><strong class="mono">${short(latest.commitment, 18, 8)}</strong></div>
      <div><span>4</span><small>Tree insertion</small><strong>Leaf ${latest.leafIndex}</strong></div>
    </div>` : `<div class="commitment-construction pending"><div><span>1</span><small>Generate</small><strong>fresh salt</strong></div><div><span>2</span><small>Bind</small><strong>token_id + amount</strong></div><div><span>3</span><small>Hash</small><strong>create C</strong></div><div><span>4</span><small>Append</small><strong>new leaf</strong></div></div>`;
  const rows = notes.map(note => `<tr class="owned-note" data-note-id="${note.id}"><td>Leaf ${note.leafIndex}</td><td>${formatAmount(note.amount)} ${note.assetId}</td><td class="mono">${short(note.salt, 12, 6)}</td><td class="mono">${short(note.commitment, 16, 7)}</td><td><span class="policy-badge ${note.status === "unspent" ? "" : "selective"}">${note.status}</span></td></tr>`).join("");
  return `<section class="note-construction-card"><div class="panel-heading"><div><p class="eyebrow">Commitment construction</p><h3>One shielding operation → one private note → one leaf</h3></div><span class="context-badge">${notes.length} OWNED ${notes.length === 1 ? "NOTE" : "NOTES"}</span></div>
    <p>The participant generates a fresh salt, then commits to the note as <code>C = H(pk_spend, salt, token_id, amount)</code>. Repeating shielding never overwrites an earlier note.</p>
    <div class="commitment-formula"><span>pk_spend</span><b>${short(owner?.spendPublicKey || "pending", 16, 7)}</b><i>+</i><span>salt</span><b>${latest ? short(latest.salt, 16, 7) : "generated on shield"}</b><i>+</i><span>token_id</span><b>${assetId}</b><i>+</i><span>amount</span><b>${latest ? formatAmount(latest.amount) : "chosen above"}</b></div>
    ${construction}
    <div class="owned-notes"><div class="owned-notes-heading"><strong>Participant’s private notes</strong><span>Every shielding leaf remains independently tracked</span></div>${rows ? `<div class="table-wrap"><table><thead><tr><th>Tree position</th><th>Value</th><th>Salt</th><th>Commitment</th><th>Status</th></tr></thead><tbody>${rows}</tbody></table></div>` : `<div class="empty-state">No private notes yet. The first shielding operation will create leaf 0.</div>`}</div>
  </section>`;
}

function dvpAuditCard(p) {
  const transferId = p.flow.dvpTransfer?.id;
  const outputs = (p.notes || []).filter(note => note.origin === "dvp_output" && note.transferId === transferId);
  const participantOutput = outputs.find(note => note.ownerPartyId === "party-1");
  const envelopes = p.registrations.filter(item => item.auditEnvelope).length;
  const roots = Object.values(p.trees || {}).map(tree => `<span><small>${tree.assetId} root</small><strong class="mono">${short(tree.root, 18, 8)}</strong></span>`).join("");
  const opaqueOutputs = outputs.map(note => `<div class="opaque-commitment"><span>Commitment</span><strong class="mono">${short(note.commitment, 18, 8)}</strong><small>owner · token · amount hidden</small></div>`).join("");
  const participantRows = outputs.map(note => note.id === participantOutput?.id
    ? `<div class="opened-note"><span>Opened with Atlas Bank’s sk_view</span><strong>${formatAmount(note.amount)} ${note.assetId}</strong><small>Recipient: ${note.ownerName} · salt ${short(note.salt, 12, 6)}</small></div>`
    : `<div class="opaque-commitment"><span>Other participant’s commitment</span><strong class="mono">${short(note.commitment, 18, 8)}</strong><small>cannot open</small></div>`).join("");
  const auditorRows = outputs.map(note => `<div class="opened-note"><span>${note.assetId} tree · leaf ${note.leafIndex}</span><strong>${formatAmount(note.amount)} ${note.assetId}</strong><small>Recipient: ${note.ownerName} · salt ${short(note.salt, 12, 6)}</small><b class="mono">${short(note.commitment, 18, 8)}</b></div>`).join("");
  return `<article class="panel flow-card wide dvp-audit"><div class="panel-heading"><div><p class="eyebrow">Privacy perspectives</p><h2>The same settlement, three different views</h2></div><span class="context-badge ${outputs.length === 2 ? "complete" : ""}">${outputs.length} NEW COMMITMENTS</span></div>
    <p>The auditor already received each participant’s encrypted <code>sk_view</code> during registration. There is no second disclosure step here: that established access lets the auditor derive the note-data keys and open both settlement outputs, without exposing or controlling any spend key.</p>
    <div class="audit-registration-status"><span>✓</span><div><strong>Auditor access established during registration</strong><small>${envelopes} encrypted participant view keys registered with the configured auditor key</small></div></div>
    ${outputs.length ? `<div class="audit-root-strip">${roots}</div><div class="audit-perspectives">
      <section class="audit-perspective outsider"><header><span>01</span><div><small>Public network</small><strong>Outsider</strong></div></header><p>Sees new commitments and updated asset roots, but cannot attribute or open either note.</p>${opaqueOutputs}</section>
      <section class="audit-perspective participant"><header><span>02</span><div><small>Registered party</small><strong>Atlas Bank</strong></div></header><p>Can open the security note it received. The counterparty’s cash output remains opaque.</p>${participantRows}</section>
      <section class="audit-perspective auditor"><header><span>03</span><div><small>Authorized oversight</small><strong>Auditor</strong></div></header><p>Uses the registration-time view-key envelopes to open both encrypted settlement outputs.</p>${auditorRows}</section>
    </div>` : `<div class="empty-state">Complete the atomic DvP settlement to create its two output commitments. The public and auditor perspectives will then appear here.</div>`}
  </article>`;
}

function trafficCard(p) {
  const trees = Object.keys(p.trees || {}).map(assetId => commitmentTree(p, assetId)).join("");
  return `<article class="panel flow-card"><p class="eyebrow">Network traffic</p><h2>Background activity</h2><p>Registered background parties perform protocol-valid ${p.id === "retail" ? "payments" : "shielding transactions"}. Pausing traffic stops all background state changes.</p>${trafficSwitch(p)}${p.id === "dvp" ? `<div class="asset-tree-stack">${trees || commitmentTree(p)}</div>` : ""}</article>`;
}

function participantInitials(name) {
  if (name === "You") return "YOU";
  return name.split(" ").map(part => part[0]).join("").slice(0, 3).toUpperCase();
}

function pairwiseChannelMatrix(p) {
  const parties = p.registrations;
  const established = new Set(p.flow.channelPairs?.map(pair => `${pair.leftPartyId}:${pair.rightPartyId}`) || []);
  let pairOrder = 0;
  const headers = parties.map(party => `<th scope="col"><abbr title="${party.name}">${participantInitials(party.name)}</abbr></th>`).join("");
  const rows = parties.map((rowParty, row) => {
    const cells = parties.map((columnParty, column) => {
      if (column > row) return `<td class="channel-cell channel-cell-empty" aria-hidden="true"></td>`;
      if (column === row) return `<td class="channel-cell channel-cell-self"><span aria-label="${rowParty.name}; same participant">—</span></td>`;
      const key = `${columnParty.partyId}:${rowParty.partyId}`;
      const isEstablished = established.has(key);
      const label = `${columnParty.name} ↔ ${rowParty.name}`;
      const currentOrder = pairOrder;
      pairOrder += 1;
      return `<td class="channel-cell channel-cell-pair ${isEstablished ? "established" : "pending"}" style="--pair-order:${currentOrder}" title="${label}: ${isEstablished ? "established" : "pending"}"><span aria-label="Channel ${label}; ${isEstablished ? "established" : "pending"}">${isEstablished ? "✓" : ""}</span></td>`;
    }).join("");
    return `<tr><th scope="row"><span class="channel-row-index">${String(row + 1).padStart(2, "0")}</span><span>${rowParty.name}</span></th>${cells}</tr>`;
  }).join("");
  return `<div class="channel-matrix-wrap">
    <table class="channel-matrix">
      <caption class="sr-only">Lower-triangle matrix of bilateral channels between registered participants</caption>
      <thead><tr><th scope="col"><span class="sr-only">Participant</span></th>${headers}</tr></thead>
      <tbody>${rows}</tbody>
    </table>
  </div>`;
}

function registrationReviewCard(p) {
  return `<article class="panel flow-card registration-review-card"><div class="panel-heading"><div><p class="eyebrow">Registration complete</p><h2>Participant registry</h2></div><span class="context-badge complete">10 / 10 REGISTERED</span></div><p>Every participant has registered both public keys and separately shared its view key with the auditor.</p>${registrationProcess(p)}<div class="registration-review-table">${registryTable(p, PARTY_NAMES.map((_, index) => index))}</div></article>`;
}

function institutionalChannelCard(p) {
  const channelCount = p.flow.channelPairs?.length || 0;
  return `<article class="panel flow-card channel-network-card"><div class="panel-heading"><div><p class="eyebrow">Pairwise channels</p><h2>Institution network</h2></div><span class="context-badge ${p.flow.channels ? "complete" : ""}">${channelCount} / 45 ESTABLISHED</span></div><p>Each cell in the triangle is one bilateral private channel between the participant on its row and the participant at the top of its column.</p>${pairwiseChannelMatrix(p)}<div class="channel-matrix-footer"><div class="channel-legend"><span><i class="legend-cell pending"></i>Pending</span><span><i class="legend-cell established">✓</i>Established</span></div><div class="button-row"><button class="button button-primary" data-action="channels" ${p.flow.channels ? "disabled" : ""}>${p.flow.channels ? "All channels established" : "Establish 45 channels"}</button></div></div></article>`;
}

function institutionalSteps(p) {
  return [
    { id: "registration", label: "Registration", actor: "Registered participants", complete: true, content: registrationReviewCard(p) },
    { id: "channels", label: "Pairwise channels", actor: "Registered institutions", complete: p.flow.channels, content: institutionalChannelCard(p) },
    { id: "payment", label: "Private payment", actor: "Paying institution", complete: p.transactions.some(tx => tx.type === "payment"), content: `<article class="panel flow-card"><p class="eyebrow">Private payment</p><h2>Post a payment envelope</h2><p>Send a private payment through the established bilateral channel without revealing its contents to the public network.</p><div class="button-row"><button class="button button-primary" data-action="payment" ${!p.flow.channels || p.flow.frozen ? "disabled" : ""}>Post payment</button></div></article>` },
    { id: "policy", label: "Policy control", actor: "System operator", complete: p.transactions.some(tx => tx.type === "freeze"), content: `<article class="panel flow-card"><p class="eyebrow">Operator control</p><h2>Freeze and resume a channel</h2><p>Exercise protocol policy without exposing any participant spend key.</p><div class="button-row"><button class="button button-auditor" data-action="${p.flow.frozen ? "resume" : "freeze"}" ${!p.flow.channels ? "disabled" : ""}>${p.flow.frozen ? "Resume channel" : "Freeze channel"}</button></div></article>` },
    { id: "bridge", label: "Private bridge", actor: "Originating institution", complete: p.flow.bridgeReady, content: `<article class="panel flow-card"><p class="eyebrow">Interoperability</p><h2>Bridge private assets</h2><p>Create linked source and destination commitments with a compact receipt.</p><div class="button-row"><button class="button button-primary" data-action="bridge">Bridge assets</button></div></article>` },
    { id: "perspectives", label: "Perspectives", actor: "Demo viewer", complete: false, content: `<article class="panel flow-card"><p class="eyebrow">Perspectives</p><h2>Inspect each protocol view</h2><p>Change perspective without changing the underlying transaction.</p><select aria-label="Institutional perspective"><option>Participant: own envelope details</option><option>Network: commitments only</option><option>Auditor: consented scope</option><option>Operator: policy controls</option></select></article>` },
    { id: "audit", label: "Audit access", actor: "Auditor and regulator", complete: p.disclosures.length > 0, content: auditCard(p) },
    { id: "chain", label: "Public chain", actor: "Public network", complete: p.leaves.length > 0, content: publicChainCard(p) }
  ];
}

function retailSteps(p) {
  const recipient = p.flow.retailRecipient;
  const paymentComplete = p.transactions.some(tx => tx.type === "payment");
  const recipientKeys = recipient ? `<div class="recipient-key-grid">
    <div class="detail-card"><label>Recipient · ${recipient.name}</label><strong>Registered public keys</strong></div>
    <div class="detail-card"><label>Spend public key · pk_spend</label><span class="key-value">${recipient.spendPublicKey}</span></div>
    <div class="detail-card"><label>View public key · pk_view</label><span class="key-value">${recipient.viewPublicKey}</span></div>
  </div>` : `<div class="callout">The payer selects Atlas Bank and retrieves its two registered public keys. No recipient secret is exposed.</div>`;
  return [
    { id: "registration", label: "Registration", actor: "Registered participants", complete: true, content: registrationReviewCard(p) },
    { id: "recipient", label: "Recipient keys", actor: "Payer", complete: Boolean(recipient), content: `<article class="panel flow-card"><p class="eyebrow">Payment preparation</p><h2>Retrieve the recipient’s public keys</h2><p>The payer looks up the recipient in the participant registry and obtains <code>pk_spend</code> for the new note commitment and <code>pk_view</code> for payment-data encryption.</p>${recipientKeys}<div class="button-row"><button class="button button-primary" data-action="prepare-recipient" ${recipient ? "disabled" : ""}>${recipient ? "Recipient keys retrieved" : "Retrieve Atlas Bank keys"}</button></div></article>` },
    { id: "payment", label: "Private payment", actor: "Payer", complete: paymentComplete, content: `<article class="panel flow-card"><p class="eyebrow">Private payment</p><h2>Create and submit the payment</h2><p>The payer encapsulates a fresh shared secret to the recipient’s <code>pk_view</code>, encrypts the note data, creates recipient and change commitments, and proves value conservation and ownership of an existing leaf.</p><div class="payment-construction" aria-label="Private payment construction"><span><b>1</b><strong>ML-KEM encapsulation</strong><small>Fresh secret against recipient pk_view</small></span><span><b>2</b><strong>Commitments</strong><small>Recipient output and payer change</small></span><span><b>3</b><strong>ZK proof</strong><small>Ownership, membership, and conservation</small></span><span><b>4</b><strong>Submit</strong><small>Ciphertext, proof, and nullifier</small></span></div><div class="button-row"><button class="button button-primary" data-action="payment" ${!recipient || paymentComplete ? "disabled" : ""}>${paymentComplete ? "Payment submitted" : "Send private payment"}</button></div></article>` },
    { id: "scan", label: "Recipient scan", actor: "Recipient", complete: p.transactions.some(tx => tx.type === "scan"), content: `<article class="panel flow-card"><p class="eyebrow">Recipient view</p><h2>Discover the incoming note</h2><p>The recipient processes published payment envelopes with <code>sk_view</code>. A successful decapsulation reveals the encrypted note data; the recipient recomputes the commitment and accepts only an exact match.</p><div class="button-row"><button class="button button-primary" data-action="scan" ${!paymentComplete ? "disabled" : ""}>Scan payment envelopes</button></div></article>` },
    { id: "traffic", label: "Network traffic", actor: "Registered participants", complete: p.traffic, content: trafficCard(p) },
    { id: "audit", label: "Audit access", actor: "Auditor and regulator", complete: p.disclosures.length > 0, content: auditCard(p) },
    { id: "chain", label: "Public chain", actor: "Public network", complete: p.leaves.length > 0, content: publicChainCard(p) }
  ];
}

function formatAmount(value) {
  return new Intl.NumberFormat("en-US").format(value);
}

function dvpParties(terms) {
  const seller = engineAdapter.state.identities.find(identity => identity.id === terms.sellerPartyId);
  const buyer = engineAdapter.state.identities.find(identity => identity.id === terms.buyerPartyId);
  return `<div class="dvp-parties" aria-label="DvP counterparties">
    <div class="dvp-party seller"><span>S</span><div><small>Seller · security leg</small><strong>${seller?.name || "Boreal Markets"}</strong></div></div>
    <span class="dvp-party-link" aria-hidden="true">↔</span>
    <div class="dvp-party buyer"><span>B</span><div><small>Buyer · cash leg</small><strong>${buyer?.name || "Atlas Bank"}</strong></div></div>
  </div>`;
}

function dvpTermSheet(terms) {
  return `<div class="term-sheet">
    <section class="term-leg-card security" data-term-leg="security"><header><span>S</span><div><small>Seller delivers</small><strong>Security leg</strong></div></header><dl><div><dt>Security</dt><dd>${terms.securityId}</dd></div><div><dt>Quantity</dt><dd>${formatAmount(terms.quantity)} units</dd></div></dl></section>
    <span class="term-swap" aria-hidden="true">⇄</span>
    <section class="term-leg-card cash" data-term-leg="cash"><header><span>$</span><div><small>Buyer delivers</small><strong>Cash leg</strong></div></header><dl><div><dt>Currency</dt><dd>${terms.cashToken}</dd></div><div><dt>Amount</dt><dd>${formatAmount(terms.cashAmount)} ${terms.cashToken}</dd></div></dl></section>
    <div class="term-condition"><span>Settlement condition</span><strong>Either both legs lock and settle atomically, or one locked leg returns after ${terms.expiryBlocks} blocks.</strong></div>
  </div>`;
}

function dvpTransferIdentity(transfer) {
  if (!transfer) return `<div class="empty-state">The transfer ID and terms commitment are created when the buyer initiates the accepted trade.</div>`;
  return `<div class="transfer-identity"><div><span>DvP transfer ID</span><strong>${transfer.id}</strong></div><div><span>Terms commitment</span><strong>${transfer.termsCommitment}</strong></div><b class="policy-badge ${transfer.status === "settled" ? "" : "selective"}">${transfer.status.replaceAll("_", " ")}</b></div>`;
}

function dvpAcceptanceSummary(terms) {
  return `<div class="acceptance-sheet" aria-label="Buyer approval summary">
    <div class="acceptance-party"><span>AB</span><div><small>Reviewing party</small><strong>Atlas Bank · Buyer</strong><p>Confirms the consideration and settlement window.</p></div></div>
    <div class="acceptance-summary"><p class="eyebrow">Terms received from Boreal Markets</p><dl><div><dt>Buyer commits</dt><dd>${formatAmount(terms.cashAmount)} ${terms.cashToken}</dd></div><div><dt>Buyer receives</dt><dd>${formatAmount(terms.quantity)} ${terms.securityId}</dd></div><div><dt>One-leg expiry</dt><dd>${terms.expiryBlocks} blocks</dd></div></dl></div>
  </div>`;
}

function dvpSteps(p) {
  const terms = p.flow.dvpTerms;
  const securityNotes = (p.notes || []).filter(note => note.ownerPartyId === "party-2" && note.assetId === p.flow.securityAssetId && note.origin === "shielding" && (note.status === "unspent" || note.id === terms.securityInputNoteId));
  const cashNotes = (p.notes || []).filter(note => note.ownerPartyId === "party-1" && note.assetId === terms.cashToken && note.origin === "shielding" && (note.status === "unspent" || note.id === terms.cashInputNoteId));
  const proposed = terms.status === "proposed" || terms.status === "agreed";
  const agreed = terms.status === "agreed";
  const sellerHasSecurity = p.flow.sellerPublicSecurity + p.flow.sellerShieldedSecurity + p.flow.buyerPrivateSecurity + p.flow.buyerPublicSecurity > 0;
  const buyerHasCash = p.flow.buyerPublicCash + p.flow.buyerShieldedCash + p.flow.sellerPrivateCash > 0;
  const securityShielded = p.flow.sellerShieldedSecurity > 0 || p.flow.settlement === "settled";
  const cashShielded = p.flow.buyerShieldedCash > 0 || p.flow.settlement === "settled";
  const bothShielded = securityShielded && cashShielded;
  const settled = p.flow.settlement === "settled";
  const oneLegLocked = p.flow.securityLocked !== p.flow.cashLocked;
  const transferActive = Boolean(p.flow.dvpTransfer && ["open", "awaiting_counterparty"].includes(p.flow.dvpTransfer.status));
  const securityInputAvailable = (p.notes || []).some(note => note.id === terms.securityInputNoteId && note.status === "unspent");
  const cashInputAvailable = (p.notes || []).some(note => note.id === terms.cashInputNoteId && note.status === "unspent");
  const canInitiate = agreed && !transferActive && securityInputAvailable && cashInputAvailable;
  const securityHoldingsContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Pre-trade holdings · security issuer</p><h2>Allocate securities to the seller</h2></div><span class="context-badge ${sellerHasSecurity ? "complete" : ""}">${formatAmount(p.flow.sellerPublicSecurity)} PUBLIC UNITS</span></div><p>Choose how many ${p.flow.securityAssetId} units the issuer allocates to ${engineAdapter.state.identities[2]?.name || "Boreal Markets"}. This holding exists before, and independently of, the later DvP agreement.</p><div class="dvp-leg security-leg"><span>S</span><div><small>Seller’s public balance</small><strong>${formatAmount(p.flow.sellerPublicSecurity)} ${p.flow.securityAssetId}</strong></div><b>${sellerHasSecurity ? "Available" : "Empty"}</b></div><div class="amount-action"><label>Amount to issue<input id="dvpMintSecurityAmount" type="number" min="1" step="1" value="1000" ${proposed ? "disabled" : ""}></label><button class="button button-primary" data-action="mint-security" ${proposed ? "disabled" : ""}>Issue securities</button></div></article>`;
  const cashHoldingsContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Pre-trade holdings · cash issuer</p><h2>Allocate cash to the buyer</h2></div><span class="context-badge ${buyerHasCash ? "complete" : ""}">${formatAmount(p.flow.buyerPublicCash)} USD</span></div><p>Choose the cash allocation for ${engineAdapter.state.identities[1]?.name || "Atlas Bank"}. This public balance is established before the counterparties negotiate a trade.</p><div class="dvp-leg cash-leg"><span>B</span><div><small>Buyer’s public balance</small><strong>${formatAmount(p.flow.buyerPublicCash)} USD</strong></div><b>${buyerHasCash ? "Available" : "Empty"}</b></div><div class="amount-action"><label>Amount to issue<input id="dvpMintCashAmount" type="number" min="1" step="1" value="10000000" ${proposed ? "disabled" : ""}></label><button class="button button-primary" data-action="mint-cash" ${proposed ? "disabled" : ""}>Issue cash</button></div></article>`;
  const noteOptions = (notes, selectedId) => notes.map(note => `<option value="${note.id}" ${note.id === selectedId ? "selected" : ""}>Leaf ${note.leafIndex} · ${formatAmount(note.amount)} ${note.assetId} · ${short(note.commitment, 10, 5)}</option>`).join("");
  const proposalContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Bilateral terms · seller</p><h2>Propose the DvP parameters</h2></div><span class="context-badge ${proposed ? "complete" : ""}">${terms.status.toUpperCase()}</span></div><p>Each leg identifies one existing private note. The note’s committed token and amount become the exact trade parameters, so the contract can consume that leaf without treating an aggregate balance as a spendable object.</p>${dvpParties(terms)}<div class="holdings-strip"><span>Seller notes available: <strong>${securityNotes.length}</strong></span><span>Buyer notes available: <strong>${cashNotes.length}</strong></span></div><div class="terms-form">
      <fieldset class="term-leg-fields security" data-term-leg="security"><legend><span>S</span><strong>Security leg</strong><small>Seller delivers</small></legend><div><label>Seller’s private note<select id="dvpSecurityNote" ${proposed ? "disabled" : ""}>${noteOptions(securityNotes, terms.securityInputNoteId)}</select><small>The selected commitment fixes the security identifier and quantity.</small></label></div></fieldset>
      <fieldset class="term-leg-fields cash" data-term-leg="cash"><legend><span>$</span><strong>Cash leg</strong><small>Buyer delivers</small></legend><div><label>Buyer’s private note<select id="dvpCashNote" ${proposed ? "disabled" : ""}>${noteOptions(cashNotes, terms.cashInputNoteId)}</select><small>The selected commitment fixes the currency and consideration.</small></label></div></fieldset>
      <label class="term-expiry">Settlement condition · one-leg lock expiry<input id="dvpExpiryBlocks" type="number" min="1" value="${terms.expiryBlocks}" ${proposed ? "disabled" : ""}><small>Blocks after the first leg locks. If the second leg locks in time, settlement is immediate.</small></label>
    </div><div class="button-row"><button class="button button-primary" data-action="propose-terms" ${proposed || !securityShielded || !cashShielded ? "disabled" : ""}>${proposed ? "Terms proposed to buyer" : "Propose terms to buyer"}</button></div></article>`;
  const acceptanceContent = `<article class="panel flow-card acceptance-card"><div class="panel-heading"><div><p class="eyebrow">Buyer approval</p><h2>Accept and initiate the DvP</h2></div><span class="context-badge ${transferActive ? "complete" : ""}">${transferActive ? "TRADE IN FLIGHT" : agreed ? "TERMS AGREED" : proposed ? "AWAITING BUYER" : "NO PROPOSAL"}</span></div><p>This approval view is intentionally separate from the seller’s proposal form. The buyer first accepts the received term sheet, then initiates its identified transfer by submitting the encrypted cash leg.</p>${dvpAcceptanceSummary(terms)}<div class="dvp-initiation-process"><div class="process-step ${agreed ? "complete" : proposed ? "active" : ""}"><span>${agreed ? "✓" : "1"}</span><div><strong>Accept exact terms</strong><small>Buyer approves security, cash, and expiry parameters.</small></div></div><span class="process-connector">→</span><div class="process-step ${transferActive || p.flow.dvpTransfer ? "complete" : agreed ? "active" : ""}"><span>${p.flow.dvpTransfer ? "✓" : "2"}</span><div><strong>Instantiate and initiate</strong><small>Create the transfer ID and submit the buyer’s encrypted cash leg.</small></div></div></div>${p.flow.dvpTransfer ? `${dvpTransferIdentity(p.flow.dvpTransfer)}<div class="leg-status-strip"><span class="complete"><b>Cash leg</b><strong>Submitted by buyer</strong></span><span class="${settled ? "complete" : "waiting"}"><b>Security leg</b><strong>${settled ? "Submitted · settled" : "Awaiting seller"}</strong></span></div>` : ""}<div class="button-row">${!agreed ? `<button class="button button-primary" data-action="accept-terms" ${!proposed ? "disabled" : ""}>Accept DvP terms</button>` : `<button class="button button-primary" data-action="instantiate-transfer" ${!canInitiate ? "disabled" : ""}>${p.flow.dvpTransfer ? "Initiate another transfer" : "Instantiate transfer and submit cash leg"}</button>`}</div></article>`;
  const securityContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Seller response · security leg</p><h2>Complete the DvP</h2></div><span class="context-badge ${settled ? "complete" : ""}">${settled ? "ATOMICALLY SETTLED" : p.flow.cashLocked ? "CASH LOCKED · WAITING" : "NOT INITIATED"}</span></div><p>The buyer initiated this transfer with its cash leg. The seller can now submit the matching encrypted security leg; because both legs are then present, the contract settles immediately.</p>${dvpTransferIdentity(p.flow.dvpTransfer)}${dvpTermSheet(terms)}${settled ? `<div class="callout success-callout dvp-result"><strong>Atomic DvP complete.</strong> The buyer received ${formatAmount(terms.quantity)} ${terms.securityId}; the seller received ${formatAmount(terms.cashAmount)} ${terms.cashToken}.</div>` : ""}<div class="button-row"><button class="button button-primary" data-action="lock-security" ${!transferActive || !p.flow.cashLocked || !bothShielded || p.flow.securityLocked || settled ? "disabled" : ""}>${settled ? "Both legs settled" : "Submit security leg and settle"}</button></div></article>`;
  const securityDefaultShield = Math.min(p.flow.sellerPublicSecurity || 1, 500);
  const cashDefaultShield = Math.min(p.flow.buyerPublicCash || 1, 2500000);
  const securityContentBefore = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Pre-trade privacy · seller</p><h2>Shield securities</h2></div><span class="context-badge ${securityShielded ? "complete" : ""}">${formatAmount(p.flow.sellerShieldedSecurity)} PRIVATE UNITS</span></div><p>Choose how much of the seller’s public inventory becomes a private note. You can shield repeatedly; every submission creates and retains another independently spendable leaf in the ${p.flow.securityAssetId} tree.</p><div class="balance-movement"><span>Public balance<strong>${formatAmount(p.flow.sellerPublicSecurity)} ${p.flow.securityAssetId}</strong></span><b aria-hidden="true">→</b><span>Private balance<strong>${formatAmount(p.flow.sellerShieldedSecurity)} ${p.flow.securityAssetId}</strong></span></div><div class="amount-action"><label>Amount to shield<input id="dvpShieldSecurityAmount" type="number" min="1" max="${p.flow.sellerPublicSecurity}" step="1" value="${securityDefaultShield}" ${!p.flow.sellerPublicSecurity || proposed ? "disabled" : ""}></label><button class="button button-primary" data-action="shield-security" ${!p.flow.sellerPublicSecurity || proposed ? "disabled" : ""}>${securityShielded ? "Shield again · create another leaf" : "Shield securities"}</button></div>${privateNoteConstruction(p, "party-2", p.flow.securityAssetId)}${shieldingNetwork(p, p.flow.securityAssetId, "party-2")}</article>`;
  const cashContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Pre-trade privacy · buyer</p><h2>Shield cash</h2></div><span class="context-badge ${cashShielded ? "complete" : ""}">${formatAmount(p.flow.buyerShieldedCash)} PRIVATE USD</span></div><p>Choose how much of the buyer’s public cash balance becomes a private note. Each submission creates and retains a separate leaf in the ${terms.cashToken} tree.</p><div class="balance-movement"><span>Public balance<strong>${formatAmount(p.flow.buyerPublicCash)} ${terms.cashToken}</strong></span><b aria-hidden="true">→</b><span>Private balance<strong>${formatAmount(p.flow.buyerShieldedCash)} ${terms.cashToken}</strong></span></div><div class="amount-action"><label>Amount to shield<input id="dvpShieldCashAmount" type="number" min="1" max="${p.flow.buyerPublicCash}" step="1" value="${cashDefaultShield}" ${!p.flow.buyerPublicCash || proposed ? "disabled" : ""}></label><button class="button button-primary" data-action="shield-cash" ${!p.flow.buyerPublicCash || proposed ? "disabled" : ""}>${cashShielded ? "Shield again · create another leaf" : "Shield cash"}</button></div>${privateNoteConstruction(p, "party-1", terms.cashToken)}${shieldingNetwork(p, terms.cashToken, "party-1")}</article>`;
  let timeoutState;
  if (settled) timeoutState = `<div class="timeout-state"><div class="callout success-callout"><strong>This transfer settled.</strong> Both identified input notes were consumed, so its timeout is no longer available. The timeout branch applies only while exactly one leg is locked.</div></div>`;
  else if (oneLegLocked) timeoutState = `<div class="timeout-state"><div class="dvp-leg"><span>1</span><div><small>Current state</small><strong>${p.flow.securityLocked ? "Security locked; waiting for buyer cash" : "Cash locked; waiting for seller security"}</strong></div><b>${terms.expiryBlocks} block window</b></div><button class="button button-auditor" data-action="timeout">Expire and return locked leg</button></div>`;
  else if (p.flow.settlement === "reverted") timeoutState = `<div class="timeout-state"><div class="callout"><strong>Cash leg returned.</strong> The seller did not submit the security leg before expiry, so no exchange occurred.</div><button class="button button-primary" data-action="instantiate-transfer" ${canInitiate ? "" : "disabled"}>Initiate another transfer</button></div>`;
  else timeoutState = `<div class="empty-state">Timeout becomes available only after exactly one party locks its leg and the counterparty fails to lock before the agreed expiry.</div>`;
  const timeoutContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Alternate transfer outcome</p><h2>One-sided transfer timeout</h2></div><span class="context-badge">${settled ? "NOT APPLICABLE" : oneLegLocked ? "ONE LEG LOCKED" : p.flow.settlement.toUpperCase()}</span></div><p>A timeout never competes with a fully submitted transfer. It returns the authorized amount only when one participant submitted its leg and the other did not.</p>${dvpTransferIdentity(p.flow.dvpTransfer)}${dvpTermSheet(terms)}${timeoutState}</article>`;
  return [
    { id: "registration", label: "Registration", actor: "Registered participants", complete: true, content: registrationReviewCard(p) },
    { id: "seller-holdings", label: "Issue securities", actor: "Security issuer", complete: sellerHasSecurity, content: securityHoldingsContent },
    { id: "seller-asset", label: "Shield securities", actor: "Boreal Markets · Seller", complete: securityShielded, content: securityContentBefore },
    { id: "buyer-holdings", label: "Issue cash", actor: "Cash issuer", complete: buyerHasCash, content: cashHoldingsContent },
    { id: "buyer-cash", label: "Shield cash", actor: "Atlas Bank · Buyer", complete: cashShielded, content: cashContent },
    { id: "terms-proposal", label: "Propose terms", actor: "Boreal Markets · Seller", complete: proposed, content: proposalContent },
    { id: "terms-acceptance", label: "Accept & initiate", actor: "Atlas Bank · Buyer", complete: Boolean(p.flow.dvpTransfer), content: acceptanceContent },
    { id: "settlement", label: "Seller response", actor: "Boreal Markets · Seller", complete: settled, content: securityContent },
    { id: "timeout", label: "Timeout branch", actor: "Protocol timer", complete: p.flow.settlement === "reverted", content: timeoutContent },
    { id: "audit", label: "Audit perspectives", actor: "Auditor", complete: p.transactions.some(tx => tx.type === "atomic-settlement"), content: dvpAuditCard(p) },
    { id: "unshield", label: "Optional unshield", actor: "Atlas Bank · Buyer", complete: p.transactions.some(tx => tx.type === "unshield"), content: `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Optional exit to a public balance</p><h2>Unshield acquired securities</h2></div><span class="context-badge">OPTIONAL</span></div><p>The private DvP is already complete. If the buyer later needs public-form assets, it may unshield a chosen quantity; this is not part of settlement and publicly reveals that exited quantity.</p><div class="balance-movement"><span>Private balance<strong>${formatAmount(p.flow.buyerPrivateSecurity)} ${terms.securityId}</strong></span><b aria-hidden="true">→</b><span>Public balance<strong>${formatAmount(p.flow.buyerPublicSecurity)} ${terms.securityId}</strong></span></div><div class="button-row"><button class="button button-primary" data-action="unshield" ${!settled || p.flow.buyerPrivateSecurity < 25 ? "disabled" : ""}>Optionally unshield 25 units</button></div></article>` }
  ];
}

function auctionSteps(p) {
  const auction = p.flow.auction;
  const userBid = auction.bids.find(bid => bid.bidderPartyId === "party-0");
  const availableNotes = (p.notes || []).filter(note => note.ownerPartyId === "party-0" && note.assetId === auction.cashToken && note.origin === "auction_funding" && note.status === "unspent");
  const winningBid = auction.winnerProof ? auction.bids.find(bid => bid.id === auction.winnerProof.winnerBidId) : null;
  const auctionIdentity = `<div class="auction-identity"><div><small>Auction</small><strong>${auction.title}</strong></div><div><small>Auction reference</small><strong class="mono">${auction.reference}</strong></div><div><small>Bidder-visible asset</small><strong>${auction.assetType}</strong></div><span class="policy-badge ${auction.status === "settled" ? "" : "selective"}">${auction.status.replaceAll("_", " ")}</span></div>`;
  const keyContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Auction-specific role key</p><h2>Auctioneer setup</h2></div><span class="context-badge ${auction.auctioneerKey ? "complete" : ""}">${auction.auctioneerKey ? "KEY READY" : "NOT GENERATED"}</span></div><p>The auctioneer—not the operator—generates a dedicated ML-KEM keypair for this auction. Bidders seal bid data to its public key; the secret key remains with the auctioneer.</p>${auctionIdentity}${auction.auctioneerKey ? `<div class="auction-key-pair"><div><small>sk_auction · retained by auctioneer</small><strong class="mono secret-value">${short(auction.auctioneerKey.secretKey, 20, 9)}</strong></div><span>ML-KEM derives</span><div><small>pk_auction · publish for this auction</small><strong class="mono">${short(auction.auctioneerKey.publicKey, 20, 9)}</strong></div></div>` : `<div class="empty-state">No auctioneer key exists for this auction yet.</div>`}<div class="button-row"><button class="button button-auditor" data-action="auctioneer" ${auction.auctioneerKey ? "disabled" : ""}>${auction.auctioneerKey ? "Auctioneer key generated" : "Generate auctioneer keypair"}</button></div></article>`;
  const registerAuctioneerContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Auction configuration</p><h2>Register the auctioneer</h2></div><span class="context-badge ${auction.auctioneerRegistered ? "complete" : ""}">${auction.auctioneerRegistered ? "REGISTERED" : "PENDING"}</span></div><p>The operator binds this auctioneer public key to this auction reference. It cannot be silently reused for a different auction.</p>${auctionIdentity}<div class="registration-binding"><span><small>Auction reference</small><strong class="mono">${auction.reference}</strong></span><b>+</b><span><small>Registered bid-decryption key</small><strong class="mono">${short(auction.auctioneerKey?.publicKey, 18, 8)}</strong></span></div><div class="button-row"><button class="button button-primary" data-action="register-auctioneer" ${!auction.auctioneerKey || auction.auctioneerRegistered ? "disabled" : ""}>${auction.auctioneerRegistered ? "Auctioneer registered" : "Register auctioneer for this auction"}</button></div></article>`;
  const mintAssetContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Asset creation · issuer view</p><h2>Mint the non-fungible asset</h2></div><span class="context-badge ${auction.nftMinted ? "complete" : ""}">${auction.nftMinted ? "OWNED BY SELLER" : "NOT MINTED"}</span></div><p>The issuer creates one specific Class A share certificate and allocates it to the seller before any auction terms exist.</p><div class="nft-certificate"><span>CLASS A</span><div><small>Non-fungible share certificate</small><strong>${auction.assetTokenId}</strong><dl><div><dt>Asset type</dt><dd>${auction.assetType}</dd></div><div><dt>Quantity</dt><dd>1</dd></div><div><dt>Owner</dt><dd>${p.registrations[2]?.name || "Boreal Markets"}</dd></div></dl></div></div><div class="button-row"><button class="button button-primary" data-action="mint-nft" ${!auction.auctioneerRegistered || auction.nftMinted ? "disabled" : ""}>${auction.nftMinted ? "Asset minted to seller" : "Mint Class A share"}</button></div></article>`;
  const listContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Seller listing</p><h2>Lock the asset and open the auction</h2></div><span class="context-badge ${auction.listed ? "complete" : ""}">${auction.listed ? `${auction.biddingDuration} BLOCK WINDOW` : "DRAFT"}</span></div><p>The seller converts its specific certificate into a locked auction commitment and defines when bidding closes. Bidders see the asset type, auction reference, and remaining time—not the certificate’s private details.</p><div class="listing-visibility"><section><small>Seller and auditor know</small><strong>${auction.assetTokenId}</strong><span>Specific Class A certificate · quantity 1</span></section><section><small>Bidders are shown</small><strong>${auction.assetType}</strong><span>Asset type only · details remain private</span></section></div><div class="amount-action"><label>Bidding timeout · blocks<input id="auctionDuration" type="number" min="2" value="${auction.biddingDuration}" ${auction.listed ? "disabled" : ""}></label><button class="button button-primary" data-action="list-asset" ${!auction.nftMinted || auction.listed ? "disabled" : ""}>${auction.listed ? "Asset locked · bidding open" : "List asset for auction"}</button></div>${auction.listed ? commitmentTree(p, auction.assetType, "party-2") : ""}</article>`;
  const mintCashContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Bidder funding · operator</p><h2>Mint USD to the bidder</h2></div><span class="context-badge ${auction.publicCash > 0 || auction.privateCash > 0 ? "complete" : ""}">${formatAmount(auction.publicCash)} PUBLIC USD</span></div><p>The bidder needs funds before constructing a bid. The operator allocates a public USD balance; this operation reveals no bid and creates no auction commitment.</p><div class="dvp-leg cash-leg"><span>$</span><div><small>Your public balance</small><strong>${formatAmount(auction.publicCash)} USD</strong></div><b>${auction.publicCash > 0 ? "Available" : "Empty"}</b></div><div class="amount-action"><label>Amount to mint<input id="auctionMintCashAmount" type="number" min="1" value="1000"></label><button class="button button-primary" data-action="mint-cash">Mint USD to user</button></div></article>`;
  const shieldDefault = Math.min(auction.publicCash || 1, 650);
  const shieldCashContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Bidder funding · you</p><h2>Shield USD for bidding</h2></div><span class="context-badge ${auction.privateCash > 0 ? "complete" : ""}">${formatAmount(auction.privateCash)} PRIVATE USD</span></div><p>Choose the value of a private bidding note. Each shielding action creates its own salt, commitment, and USD-tree leaf; that exact note can later become one sealed bid.</p><div class="balance-movement"><span>Public balance<strong>${formatAmount(auction.publicCash)} USD</strong></span><b aria-hidden="true">→</b><span>Private notes<strong>${formatAmount(auction.privateCash)} USD</strong></span></div><div class="amount-action"><label>Amount to shield<input id="auctionShieldCashAmount" type="number" min="1" max="${auction.publicCash}" value="${shieldDefault}" ${auction.publicCash <= 0 || userBid ? "disabled" : ""}></label><button class="button button-primary" data-action="shield-cash" ${auction.publicCash <= 0 || userBid ? "disabled" : ""}>Shield into a new USD note</button></div>${privateNoteConstruction(p, "party-0", auction.cashToken)}${commitmentTree(p, auction.cashToken, "party-0")}</article>`;
  const noteOptions = availableNotes.map(note => `<option value="${note.id}">Leaf ${note.leafIndex} · ${formatAmount(note.amount)} USD · ${short(note.commitment, 11, 5)}</option>`).join("");
  const bidContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Sealed bid · you</p><h2>Bid on the asset type</h2></div><span class="context-badge ${userBid ? "complete" : ""}">${userBid ? "BID LOCKED" : `${auction.blocksRemaining} BLOCKS LEFT`}</span></div><p>You know this auction offers one <strong>${auction.assetType}</strong>. You do not receive the certificate ID or private asset details. Your selected USD note becomes the bid amount and is locked—not publicly revealed.</p><div class="bid-target"><span>AUCTION TARGET</span><strong>${auction.assetType}</strong><small>${auction.reference} · certificate details withheld</small></div>${userBid ? `<div class="sealed-bid-receipt"><span><small>Your private amount</small><strong>${formatAmount(userBid.amount)} USD</strong></span><span><small>Published bid commitment</small><strong class="mono">${short(userBid.commitA, 18, 8)}</strong></span><span><small>Bid envelope</small><strong class="mono">${short(userBid.ciphertext, 18, 8)}</strong></span></div><div class="callout success-callout">The public contract sees an opaque commitment, ciphertext, nullifier, and validity proof. The auctioneer can decrypt the bid with <code>sk_auction</code>; the auditor can open it through the bidder’s registration-time audit access.</div>` : `<label class="bid-note-select">Private USD note to lock<select id="auctionBidNote">${noteOptions}</select><small>The note amount is visible here only because you control it.</small></label><div class="payment-construction"><span><b>1</b><strong>Select note</strong><small>Choose one funded USD leaf</small></span><span><b>2</b><strong>Seal bid</strong><small>Encapsulate to pk_auction</small></span><span><b>3</b><strong>Prove validity</strong><small>Ownership and note membership</small></span><span><b>4</b><strong>Lock</strong><small>Submit opaque bid commitment</small></span></div><div class="button-row"><button class="button button-primary" data-action="submit-bid" ${!availableNotes.length || auction.status !== "bidding" ? "disabled" : ""}>Submit sealed bid</button></div>`}</article>`;
  const biddingContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Bidding window</p><h2>Collect bids until timeout</h2></div><span class="context-badge ${auction.status === "closed" || auction.status === "winner_announced" || auction.status === "settled" ? "complete" : ""}">${auction.blocksRemaining} BLOCKS REMAINING</span></div><p>Bid envelopes enter against the same auction reference. The public network can count them but cannot read their amounts or associate commitments with participant identities.</p><div class="auction-clock"><span>${auction.blocksRemaining}</span><div><small>Bidding timeout</small><strong>${auction.status === "bidding" ? "Submissions remain open" : "Deadline reached · submissions closed"}</strong></div><b>${auction.bids.length} sealed bids</b></div><div class="opaque-bid-stream">${auction.bids.map((bid, index) => `<div><span>Bid ${String(index + 1).padStart(2, "0")}</span><strong class="mono">${short(bid.commitA, 16, 7)}</strong><small>amount hidden</small></div>`).join("") || `<div class="empty-state">No bids submitted.</div>`}</div><div class="button-row">${!auction.otherBidsCollected ? `<button class="button button-primary" data-action="collect-bids" ${!userBid ? "disabled" : ""}>Receive remaining sealed bids</button>` : `<button class="button button-auditor" data-action="close-bidding" ${auction.status !== "bidding" ? "disabled" : ""}>Advance to bidding timeout</button>`}</div></article>`;
  const privateBidRows = auction.bids.map((bid, index) => `<tr><td>${String(index + 1).padStart(2, "0")}</td><td>${bid.bidderName}</td><td class="mono">${short(bid.commitA, 13, 6)}</td><td><strong>${formatAmount(bid.amount)} USD</strong></td><td><span class="policy-badge">Valid</span></td></tr>`).join("");
  const reviewContent = `<article class="panel flow-card wide"><div class="panel-heading"><div><p class="eyebrow">After the bidding timeout</p><h2>Open and validate the sealed bids</h2></div><span class="context-badge ${auction.status !== "bidding" && auction.bids.length ? "complete" : ""}">${auction.bids.length} BID ENVELOPES</span></div><p>Only after submissions close does the auctioneer use <code>sk_auction</code> to open every bid. The auditor independently reaches the same bid data through the view-key access granted during participant registration.</p><div class="auction-view-grid"><section class="auction-view public"><small>Public network</small><h3>Opaque commitments only</h3><p>Amounts and bidder identities remain hidden.</p><strong>${auction.bids.length} commitments</strong></section><section class="auction-view private"><small>Auctioneer</small><h3>All valid bids opened</h3><p>Decrypts with the auction-specific key.</p><strong>${auction.status === "bidding" ? "Available after timeout" : `${auction.bids.length} amounts visible`}</strong></section><section class="auction-view auditor"><small>Auditor</small><h3>All valid bids auditable</h3><p>Uses registration-time view-key envelopes.</p><strong>${auction.status === "bidding" ? "Waiting for close" : `${auction.bids.length} amounts visible`}</strong></section></div>${auction.status === "bidding" ? `<div class="empty-state">The auctioneer does not open bids while submissions are still accepted.</div>` : `<div class="table-wrap private-bid-table"><table><thead><tr><th>Bid</th><th>Opened identity</th><th>Commitment</th><th>Private amount</th><th>Validity</th></tr></thead><tbody>${privateBidRows}</tbody></table></div><div class="privacy-boundary"><span>Visible in this private view</span><strong>Auctioneer and auditor only</strong><small>None of these amounts are added to the public auction state.</small></div>`}</article>`;
  const proofContent = `<article class="panel flow-card"><div class="panel-heading"><div><p class="eyebrow">Winner selection · auctioneer</p><h2>Prove the highest valid bid</h2></div><span class="context-badge ${auction.winnerProof ? "complete" : ""}">${auction.winnerProof ? "WINNER ANNOUNCED" : "PROOF PENDING"}</span></div><p>The auctioneer evaluates the opened bids privately. It announces one winning commitment and proves the comparison without publishing the winning amount or any losing amount.</p><div class="proof-statement"><span>PROOF STATEMENT</span><strong>“Out of all valid bids, this specific bid has the highest amount.”</strong><p>The proof establishes that the selected commitment is active and valid, and that its hidden amount is greater than or equal to every other active valid bid.</p></div>${auction.winnerProof ? `<div class="winner-announcement"><span><small>Public winner announcement</small><strong class="mono">${short(auction.winnerProof.winnerCommitment, 22, 10)}</strong></span><span><small>Proof</small><strong class="mono">${short(auction.winnerProof.id, 22, 10)}</strong></span><b>Winning amount remains private</b></div>` : `<div class="button-row"><button class="button button-primary" data-action="prove-winner" ${auction.status !== "closed" ? "disabled" : ""}>Prove and announce winning commitment</button></div>`}</article>`;
  let settlementBody = `<div class="empty-state">The auctioneer must first announce a winning commitment with the highest-valid-bid proof.</div>`;
  if (auction.winnerProof && !auction.settlement) settlementBody = `<div class="settlement-contract"><div><span>1</span><strong>Verify winner proof</strong><small>Highest valid bid; amount remains hidden</small></div><div><span>2</span><strong>Consume locked notes</strong><small>Winning USD bid + listed NFT</small></div><div><span>3</span><strong>Atomic outputs</strong><small>NFT to winner + USD to seller</small></div><div><span>4</span><strong>Recover losing bids</strong><small>Fresh unlinkable USD notes</small></div></div>${auction.challengeOpen ? `<div class="callout">A challenger requested explicit verification. The contract now verifies the same proof before executing the atomic DvP.</div><div class="button-row"><button class="button button-primary" data-action="settle">Verify challenged proof and execute DvP</button></div>` : `<div class="button-row"><button class="button button-primary" data-action="settle">Execute atomic DvP</button><button class="button button-auditor" data-action="challenge">Challenge winner proof</button></div>`}`;
  if (auction.settlement) settlementBody = `<div class="callout success-callout"><strong>Atomic auction settlement complete.</strong> The winning commitment received the Class A share, the seller received a private USD note, and every losing bid received a fresh recovery note. No bid amount was published.</div><div class="settlement-outputs"><span><small>Winning bid</small><strong class="mono">${short(auction.settlement.winnerCommitment, 18, 8)}</strong></span><span><small>Valid bids compared</small><strong>${auction.settlement.validBidCount}</strong></span><span><small>Public winning amount</small><strong>Not disclosed</strong></span></div><div class="asset-tree-stack">${commitmentTree(p, auction.assetType, auction.settlement.winnerPartyId)}${commitmentTree(p, auction.cashToken)}</div>`;
  const settlementContent = `<article class="panel flow-card full"><div class="panel-heading"><div><p class="eyebrow">Contract execution</p><h2>Atomic auction DvP</h2></div><span class="context-badge ${auction.settlement ? "complete" : ""}">${auction.settlement ? "SETTLED" : auction.challengeOpen ? "CHALLENGED" : "AWAITING PROOF"}</span></div><p>The contract acts only on the proven winning commitment. Asset delivery and seller payment occur atomically; a partial settlement cannot persist.</p>${settlementBody}</article>`;
  const auditContent = `<article class="panel flow-card wide"><div class="panel-heading"><div><p class="eyebrow">Auction privacy boundary</p><h2>Who can see what</h2></div><span class="context-badge">NO PUBLIC BID AMOUNTS</span></div><div class="auction-visibility-matrix"><div><strong>Public network</strong><span>Auction reference and asset type</span><span>Bid commitments and proof</span><span>Updated asset roots</span><b>Cannot open any bid amount</b></div><div><strong>Individual bidder</strong><span>Public auction information</span><span>Its own note and bid amount</span><span>Whether its commitment won</span><b>Cannot open competing bids</b></div><div><strong>Auctioneer</strong><span>All valid bids after timeout</span><span>Winner-comparison witness</span><span>Settlement output construction</span><b>Cannot spend participant notes</b></div><div><strong>Auditor</strong><span>All bids through registered view access</span><span>Asset lock and settlement outputs</span><span>Winner-proof result</span><b>Cannot bid, choose, or spend</b></div></div>${auction.settlement ? `<div class="callout success-callout">The auditor can open the winning USD payout, NFT delivery, and losing-bid recovery notes using the access established during registration. The public sees only opaque commitments.</div>` : `<div class="empty-state">Complete settlement to inspect its final audit scope.</div>`}</article>`;
  return [
    { id: "registration", label: "Registration", actor: "Registered participants", complete: true, content: registrationReviewCard(p) },
    { id: "auctioneer", label: "Auctioneer setup", actor: "Auctioneer", complete: Boolean(auction.auctioneerKey), content: keyContent },
    { id: "auctioneer-registration", label: "Register auctioneer", actor: "System operator", complete: auction.auctioneerRegistered, content: registerAuctioneerContent },
    { id: "mint-asset", label: "Mint asset", actor: "Class A share issuer", complete: auction.nftMinted, content: mintAssetContent },
    { id: "list-asset", label: "List asset", actor: "Boreal Markets · Seller", complete: auction.listed, content: listContent },
    { id: "fund-bidder", label: "Mint USD", actor: "System operator", complete: auction.publicCash > 0 || auction.privateCash > 0, content: mintCashContent },
    { id: "shield-bidder", label: "Shield USD", actor: "You · Bidder", complete: auction.privateCash > 0, content: shieldCashContent },
    { id: "submit-bid", label: "Submit bid", actor: "You · Bidder", complete: Boolean(userBid), content: bidContent },
    { id: "bidding-window", label: "Bidding timeout", actor: "Registered bidders and protocol timer", complete: auction.status !== "bidding" && auction.bids.length > 1, content: biddingContent },
    { id: "bid-review", label: "Open bids", actor: "Auctioneer and Auditor", complete: auction.status !== "bidding" && auction.bids.length > 1, content: reviewContent },
    { id: "winner-proof", label: "Prove winner", actor: "Auctioneer", complete: Boolean(auction.winnerProof), content: proofContent },
    { id: "settlement", label: "Atomic DvP", actor: auction.challengeOpen ? "Smart contract verifier" : "Smart contract", complete: Boolean(auction.settlement), content: settlementContent },
    { id: "audit", label: "Privacy views", actor: "Auditor", complete: Boolean(auction.settlement), content: auditContent }
  ];
}

function scenarioSteps(p) {
  if (p.id === "institutional") return institutionalSteps(p);
  if (p.id === "retail") return retailSteps(p);
  if (p.id === "dvp") return dvpSteps(p);
  return auctionSteps(p);
}

function scenarioActivity(p) {
  const items = p.ledger.slice(0, 5);
  return `<aside class="panel scenario-aside"><div><p class="eyebrow">Executing entity</p><div class="scenario-actor"><span aria-hidden="true">${screenFromHash() === "audit" ? "AU" : "→"}</span><strong data-scenario-actor></strong></div></div><div class="scenario-receipts"><p class="eyebrow">Recent receipts</p>${items.map(item => `<div class="scenario-receipt"><strong>${item.label}</strong><span>block ${item.block} · ${short(item.hash, 11, 6)}</span></div>`).join("") || `<div class="activity-empty">No action receipts yet.</div>`}</div></aside>`;
}

function renderExperience(p) {
  const ready = engineAdapter.isReady(p.id);
  els.experience.hidden = !ready;
  if (!ready) return;
  const steps = scenarioSteps(p);
  const requested = screenFromHash();
  const activeIndex = Math.max(0, steps.findIndex(step => step.id === requested));
  const active = steps[activeIndex];
  const stepLinks = steps.map((step, index) => `<a href="#/${p.id}/${step.id}" class="${index === activeIndex ? "active" : step.complete ? "complete" : ""}" ${index === activeIndex ? 'aria-current="step"' : ""}><span>${step.complete ? "✓" : index + 1}</span><strong>${step.label}</strong></a>`).join("");
  const previous = activeIndex > 0 ? `<a class="button button-ghost" href="#/${p.id}/${steps[activeIndex - 1].id}">← Previous</a>` : `<span></span>`;
  const next = activeIndex < steps.length - 1 ? `<a class="button button-primary" href="#/${p.id}/${steps[activeIndex + 1].id}">Next: ${steps[activeIndex + 1].label} →</a>` : `<a class="button button-primary" href="#/choose">Finish walkthrough</a>`;
  els.experience.innerHTML = `<div class="experience-header"><div><p class="eyebrow">Protocol walkthrough · ${activeIndex + 1} of ${steps.length}</p><h2>${active.label}</h2></div><span class="context-badge">ONE STEP AT A TIME</span></div>
    <nav class="scenario-progress" aria-label="${PROTOCOLS[p.id].name} walkthrough">${stepLinks}</nav>
    <div class="scenario-layout"><section class="scenario-page" aria-live="polite">${active.content}</section>${scenarioActivity(p)}</div>
    <nav class="scenario-footer" aria-label="Walkthrough navigation">${previous}${next}</nav>`;
  els.experience.querySelector("[data-scenario-actor]").textContent = active.actor;
}

function renderWorkspace() {
  const meta = PROTOCOLS[route];
  const p = engineAdapter.protocol(route);
  els.kicker.textContent = `${meta.short} protocol`;
  els.title.textContent = meta.name;
  els.description.textContent = meta.description;
  const ready = engineAdapter.isReady(p.id);
  els.progress.hidden = ready;
  els.setupWorkspace.hidden = ready;
  if (!ready) {
    renderProgress(p);
    renderStage(p);
    renderActivity(p);
  } else {
    els.stage.innerHTML = "";
    els.activity.innerHTML = "";
  }
  renderExperience(p);
}

function render() {
  route = routeFromHash();
  const choosing = route === "choose";
  els.chooser.hidden = !choosing;
  els.workspace.hidden = choosing;
  renderNav();
  renderChooser();
  if (!choosing) renderWorkspace();
}

async function run(label, operation) {
  if (busy) return;
  busy = true;
  document.body.dataset.busy = "true";
  const origin = document.activeElement;
  if (origin instanceof HTMLButtonElement) { origin.disabled = true; origin.classList.add("working"); }
  try { await operation(); toast(label); }
  catch (error) { toast(error.message || "The protocol action could not be completed."); }
  finally { busy = false; delete document.body.dataset.busy; render(); }
}

document.addEventListener("click", async event => {
  const command = event.target.closest("[data-command]")?.dataset.command;
  const action = event.target.closest("[data-action]")?.dataset.action;
  const share = event.target.closest("[data-share]")?.dataset.share;
  if (command) {
    const operations = {
      deploy: ["Contract suite deployed", () => engineAdapter.deploy(route)],
      auditor: ["Auditor key generated", () => engineAdapter.generateAuditorKey(route)],
      configure: ["Auditor key configured", () => engineAdapter.configureAuditor(route)],
      "identity-spend-secret": ["Spend secret key generated", () => engineAdapter.generateSpendSecret(route)],
      "identity-spend-public": ["Spend public key generated", () => engineAdapter.generateSpendPublic(route)],
      "identity-view-secret": ["View secret key generated", () => engineAdapter.generateViewSecret(route)],
      "identity-view-public": ["View public key generated", () => engineAdapter.generateIdentity(route)],
      "identity-confirm": [engineAdapter.state.identities.length ? "Identity confirmed for registration" : "Identity generated", () => engineAdapter.confirmIdentity(route)],
      "register-participant-keys": ["Participant public keys registered", () => engineAdapter.registerParty(route, 0)],
      "share-participant-key": ["View key shared with auditor", () => engineAdapter.sharePartyViewKey(route, 0)],
      "register-others": ["All other parties registered", () => engineAdapter.registerAll(route)]
    };
    if (operations[command]) await run(...operations[command]);
  }
  if (action) {
    let payload = {};
    if (route === "dvp" && action === "mint-security") payload = { amount: $("#dvpMintSecurityAmount")?.value };
    if (route === "dvp" && action === "mint-cash") payload = { amount: $("#dvpMintCashAmount")?.value };
    if (route === "dvp" && action === "shield-security") payload = { amount: $("#dvpShieldSecurityAmount")?.value };
    if (route === "dvp" && action === "shield-cash") payload = { amount: $("#dvpShieldCashAmount")?.value };
    if (route === "dvp" && action === "propose-terms") {
      payload = {
        securityInputNoteId: $("#dvpSecurityNote")?.value,
        cashInputNoteId: $("#dvpCashNote")?.value,
        expiryBlocks: $("#dvpExpiryBlocks")?.value
      };
    }
    if (route === "auctions" && action === "list-asset") payload = { duration: $("#auctionDuration")?.value };
    if (route === "auctions" && action === "mint-cash") payload = { amount: $("#auctionMintCashAmount")?.value };
    if (route === "auctions" && action === "shield-cash") payload = { amount: $("#auctionShieldCashAmount")?.value };
    if (route === "auctions" && action === "submit-bid") payload = { noteId: $("#auctionBidNote")?.value };
    await run("Protocol action completed", () => engineAdapter.executeProtocolAction(route, action, payload));
  }
  if (share) await run("Symmetric note-data key disclosed to additional regulator", () => engineAdapter.shareTransactionKey(route, share));
});

document.addEventListener("change", event => {
  if (event.target.matches("[data-traffic]")) engineAdapter.setTraffic(route, event.target.checked, event.target.dataset.trafficAsset || null);
});

$("#resetProtocol").addEventListener("click", () => {
  if (confirm(`Reset ${PROTOCOLS[route].name}? The global identity will be preserved.`)) { engineAdapter.resetProtocol(route); toast("Protocol reset; shared identity preserved"); }
});

$("#resetAll").addEventListener("click", () => {
  if (confirm("Reset every protocol and remove the shared identity from this session?")) { engineAdapter.resetAll(); toast("Entire demo reset"); }
});

window.addEventListener("hashchange", render);
engineAdapter.addEventListener("change", render);
engineAdapter.restoreTimers();
window.__ENYGMA_DEMO__ = { engine: engineAdapter, state: () => engineAdapter.snapshot(), protocols: PROTOCOLS };
render();

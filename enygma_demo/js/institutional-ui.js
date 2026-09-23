import { institutionalState, G, H } from "./institutional.js";

export const institutionalUI = { payerId: 1, recipientId: 2, k: 2, amount: 100, accountIds: [1, 2], viewer: "public", controlsRole: "auditor" };
const format = n => Number(n).toLocaleString("en-US");
const short = s => String(s).length > 20 ? `${String(s).slice(0, 10)}…${String(s).slice(-7)}` : String(s);
const signed = n => n > 0 ? `+${format(n)}` : n < 0 ? `−${format(-n)}` : "0";
const point = (p, label = "Coordinates") => `<details class="institutional-point"><summary title="Show ${label}"><span class="point-coordinates"><span>x ${short(p[0])}</span><span>y ${short(p[1])}</span></span></summary><code>x = ${p[0]}<br>y = ${p[1]}</code></details>`;
const button = (action, label, disabled = false, extra = "") => `<button class="button button-primary" data-action="${action}" ${disabled ? "disabled" : ""} ${extra}>${label}</button>`;
const panel = (eyebrow, title, content, badge = "") => `<article class="panel flow-card institutional-card"><div class="panel-heading"><div><p class="eyebrow">${eyebrow}</p><h2>${title}</h2></div>${badge ? `<span class="context-badge">${badge}</span>` : ""}</div>${content}</article>`;
const canOpen = (viewer, id) => viewer === "auditor" || viewer === String(id);

export function selectInstitutionalSet(p) {
  const s = institutionalState(p), ui = institutionalUI;
  const required = [ui.payerId, ui.recipientId];
  ui.accountIds = [...new Set([...required, ...ui.accountIds.filter(id => !s.frozen.includes(id))])].slice(0, ui.k);
  for (const a of s.accounts) if (ui.accountIds.length < ui.k && !ui.accountIds.includes(a.accountId) && !s.frozen.includes(a.accountId)) ui.accountIds.push(a.accountId);
}

function accountOptions(p, selected) {
  return p.registrations.map((r, i) => `<option value="${i + 1}" ${Number(selected) === i + 1 ? "selected" : ""}>${i + 1} · ${r.name}</option>`).join("");
}

export function institutionalAccounts(p, viewer = "public") {
  const s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-balances><thead><tr><th>Account</th><th>Registered user</th><th>Current balance commitment Bᵢ</th><th>Trading</th><th>Balance opening</th></tr></thead><tbody>${s.accounts.map(a => {
    const open = canOpen(viewer, a.accountId);
    return `<tr data-balance-account="${a.accountId}" data-open="${open}"><td>${a.accountId}</td><td><strong>${a.name}</strong></td><td>${point(a.commitment, "balance commitment")}</td><td><span class="policy-badge ${s.frozen.includes(a.accountId) ? "selective" : ""}">${s.frozen.includes(a.accountId) ? "Frozen" : "Eligible"}</span></td><td>${open ? `<div class="institutional-opening"><strong>${format(a.balance)} EN</strong><details><summary>Verify opening</summary><code>v = ${a.balance}<br>r = ${a.randomness}<br>Bᵢ = v·G + r·H ✓</code></details></div>` : `<span class="institutional-sealed">Sealed balance</span>`}</td></tr>`;
  }).join("")}</tbody></table></div>`;
}

export function institutionalFundingCard(p) {
  const s = institutionalState(p);
  return panel("Owner · issuance", "Fund the registered accounts", `<p>The owner calls <code>mintSupply</code> to issue EN to the registered accounts. Each account’s balance remains a Pedersen commitment.</p><div class="institutional-controls"><label>EN per account<input id="institutionalFundingAmount" type="number" min="1" max="1000000000" step="1" value="1000"></label>${button("fund", "Mint to all registered users", s.paused)}</div>${s.paused ? '<div class="callout">The owner must resume the contract before minting.</div>' : ""}${institutionalAccounts(p)}`, s.funded ? "ACCOUNTS FUNDED" : "BALANCES START AT ZERO");
}

function constructionTable(p, draft) {
  const ui = draft || institutionalUI, s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-users><thead><tr><th>In set</th><th>Registered user</th><th>Spend public key</th><th>Balance commitment Bᵢ</th><th>Payer’s instructions</th></tr></thead><tbody>${s.accounts.map(a => {
    const selected = ui.accountIds.includes(a.accountId), payer = a.accountId === ui.payerId, recipient = a.accountId === ui.recipientId, frozen = s.frozen.includes(a.accountId);
    return `<tr data-institutional-account="${a.accountId}" class="${selected ? "institutional-selected" : ""}"><td><input type="checkbox" aria-label="Include ${a.name}" data-institutional-member="${a.accountId}" ${selected ? "checked" : ""} ${draft || payer || recipient || frozen || (!selected && ui.accountIds.length >= ui.k) ? "disabled" : ""}></td><td><strong>${a.name}</strong><small>Account ${a.accountId}${frozen ? " · Frozen" : ""}</small></td><td class="mono">${short(p.registrations[a.accountId - 1].spendPublicKey)}</td><td>${point(a.commitment)}</td><td>${payer ? `<b>Payer · ${signed(-ui.amount)} EN</b>` : recipient ? `<b>Recipient · ${signed(ui.amount)} EN</b>` : selected ? "Privacy participant · 0 EN" : "Outside this batch"}</td></tr>`;
  }).join("")}</tbody></table></div>`;
}

function commitmentCalculation(draft) {
  return `<section class="institutional-calculation"><div class="institutional-section-heading"><div><p class="eyebrow">Payer’s private calculation</p><h3>${draft.k} commitment deltas</h3></div><code>ΔCᵢ = Δvᵢ·G + rᵢ·H</code></div><p>Amounts sum to zero. The payer’s blinding factor balances the other slots, so <code>Σ ΔCᵢ = (0, 1)</code>. A zero-value slot still changes its commitment through <code>rᵢ·H</code>.</p><div class="table-wrap institutional-table"><table data-institutional-calculation><thead><tr><th>Account</th><th>Private Δvᵢ</th><th>Private rᵢ</th><th>Calculated ΔCᵢ</th><th>Calculation</th></tr></thead><tbody>${draft.rows.map(row => `<tr><td>${row.accountId} · ${row.name}</td><td>${signed(row.value)} EN</td><td class="mono">${short(row.r)}</td><td>${point(row.delta)}</td><td><details class="institutional-math"><summary>Show calculation</summary><code>Δvᵢ = ${row.value}<br>rᵢ = ${row.r}<br>Δvᵢ·G = (${row.valuePoint.join(", ")})<br>rᵢ·H = (${row.randomPoint.join(", ")})<br>ΔCᵢ = (${row.delta.join(", ")})<br>Bᵢ′ = Bᵢ + ΔCᵢ<br>Bᵢ′ = (${row.after.join(", ")})</code></details></td></tr>`).join("")}</tbody></table></div><details class="institutional-math institutional-derivation"><summary>Generators and blinding-factor derivation</summary><code>BabyJubJub: 168700x² + y² = 1 + 168696x²y²<br>G = (${G.join(", ")})<br>H = (${H.join(", ")})<br>s_sender = Poseidon(previous_r, sk_spend) mod ℓ<br>η = Poseidon(s_sender, epoch_hash)<br>nonceᵢ = Poseidon(η, Poseidon(sender_id, account_idᵢ))<br>hᵢ = Poseidon(Poseidon(21), sᵢ, nonceᵢ) mod ℓ<br>r_sender = Σ h_other mod ℓ; r_other = −h_other mod ℓ<br>tagᵢ = Poseidon(Poseidon(12), sᵢ, nonceᵢ) mod ℓ</code></details></section>`;
}

function publicPosting(p, draft) {
  const call = { commitmentDeltas: draft.rows.map(row => ({ c1: row.delta[0], c2: row.delta[1] })), proof: { proof: draft.proof?.id || "π · generate next", public_signal: draft.publicSignals }, participantIds: draft.accountIds, bankTag: "" };
  return `<section class="institutional-posting"><p class="eyebrow">Public contract call</p><h3>Enygma.transfer</h3><p><code>transfer(commitmentDeltas, proof, participantIds, bankTag)</code></p><div class="institutional-public-summary"><span><small>Recipient contract</small><b class="mono">${short(p.contracts.find(c => c.name === "Enygma").address)}</b></span><span><small>Published set</small><b>[${draft.accountIds.join(", ")}]</b></span><span><small>Commitment deltas</small><b>${draft.k} curve points</b></span><span><small>Proof</small><b>Groth16 over BN254</b></span></div><p class="institutional-public-note">The call carries commitments, public inputs and the proof. Payment amounts, blinding factors and the payer’s identity within the set stay private.</p><details class="institutional-calldata"><summary>Inspect posted fields · ${draft.publicSignals.length} public signals</summary><pre>${JSON.stringify(call, null, 2)}</pre></details></section>`;
}

const verificationChecks = ["Groth16 proof accepted by EnygmaVerifier", "Public keys, current balances and commitment deltas match", "Pairwise fingerprints match confirmed registry entries", "Epoch anchor and chain/contract domain match", "Nullifier is unused, then consumed", "Apply every Bᵢ′ = Bᵢ + ΔCᵢ atomically"];

export function institutionalPaymentCard(p) {
  const s = institutionalState(p), draft = s.draft, ui = draft || institutionalUI;
  const validSelection = ui.accountIds.length === ui.k && !ui.accountIds.some(id => s.frozen.includes(id));
  const controls = `<div class="institutional-controls"><label>Payer<select id="institutionalPayer" ${draft ? "disabled" : ""}>${accountOptions(p, ui.payerId)}</select></label><label>Recipient<select id="institutionalRecipient" ${draft ? "disabled" : ""}>${accountOptions(p, ui.recipientId)}</select></label><label>Commitments · k<select id="institutionalK" ${draft ? "disabled" : ""}><option value="2" ${ui.k === 2 ? "selected" : ""}>2 · Confidential</option><option value="6" ${ui.k === 6 ? "selected" : ""}>6 · With privacy participants</option></select></label><label>Amount · EN<input id="institutionalAmount" type="number" min="1" step="1" value="${ui.amount}" ${draft ? "disabled" : ""}></label></div>`;
  const rank = !draft ? 0 : draft.status === "calculated" ? 1 : draft.status === "proved" ? 2 : draft.status === "confirmed" ? 4 : 3;
  const progress = `<ol class="institutional-payment-progress" aria-label="Payment progress">${["Select accounts", "Calculate commitments", "Generate ZK proof", "Post and verify"].map((name, i) => `<li class="${i < rank ? "complete" : i === rank ? "active" : ""}"><b>${i < rank ? "✓" : i + 1}</b>${name}</li>`).join("")}</ol>`;
  const gates = s.paused ? '<div class="callout">The owner has paused the contract. Payments are stopped until the owner resumes it.</div>' : !p.flow.channels ? '<div class="callout">Establish the pairwise channels before building a payment.</div>' : !s.funded ? '<div class="callout">Fund the accounts in the preceding step before making a payment.</div>' : !validSelection ? `<div class="callout">Select exactly ${ui.k} eligible users, including the payer and recipient.</div>` : "";
  const proof = draft ? `<section class="institutional-proof"><p class="eyebrow">Payer generates the proof</p><h3>Prove the batch without revealing its openings</h3><div class="institutional-proof-boundary"><div><small>Private witness</small><strong>Spend key · balance opening · Δvᵢ · rᵢ</strong></div><span>→</span><div><small>Public statement</small><strong>Registered set · Bᵢ · ΔCᵢ · tags · η</strong></div><span>→</span><div><small>Groth16 over BN254</small><strong>Proof π</strong></div></div>${draft.proof ? `<ul class="institutional-checks">${draft.checks.map(check => `<li>✓ ${check}</li>`).join("")}</ul><div class="institutional-proof-id"><small>Generated proof</small><code>${draft.proof.id}</code></div>` : button("prove", "Generate ZK proof", s.paused || !validSelection)}</section>` : "";
  const verification = draft?.proof ? `<section class="institutional-verification"><p class="eyebrow">On-chain verification</p><h3>${draft.status === "confirmed" ? "Verified · balances updated" : draft.status === "verifying" ? "Enygma is verifying the batch…" : "Verify before updating balances"}</h3><ol>${verificationChecks.map((check, i) => `<li class="${(draft.verification || 0) > i ? "complete" : ""}"><b>${(draft.verification || 0) > i ? "✓" : i + 1}</b>${check}</li>`).join("")}</ol>${draft.status === "proved" ? button("post", `Post ${draft.k} commitments and proof`, s.paused || !validSelection) : draft.status === "confirmed" ? '<a class="button button-primary" href="#/institutional/chain">Inspect the public ledger →</a>' : '<span class="policy-badge">Transaction pending</span>'}</section>` : "";
  return panel("Payer’s private workspace", "Build a commitment batch", `${progress}<p>Choose any registered payer and recipient. With <strong>k=2</strong>, the two accounts keep their amounts confidential. With <strong>k=6</strong>, four additional users receive zero-value commitment updates. Choose their rows below.</p>${controls}${gates}${constructionTable(p, draft)}<div class="institutional-selection-summary"><span>${ui.accountIds.length} / ${ui.k} users selected</span><strong>Payer: ${p.registrations[ui.payerId - 1]?.name}</strong></div>${!draft ? button("calculate", `Calculate ${ui.k} commitments`, !p.flow.channels || !s.funded || s.paused || !validSelection) : `${commitmentCalculation(draft)}${proof}${publicPosting(p, draft)}${verification}<div class="button-row">${button("edit-payment", draft.status === "confirmed" ? "Build another payment" : "Edit payment", ["submitted", "verifying"].includes(draft.status))}</div>`}`, `${ui.k} COMMITMENTS`);
}

function paymentHistory(p, viewer) {
  const payments = p.transactions.filter(tx => tx.batch);
  if (!payments.length) return '<div class="empty-state">No payment batch has been posted yet. Fund the accounts, then build and post a batch.</div>';
  return payments.map((tx, index) => `<section class="institutional-batch" data-payment-batch="${tx.id}"><div class="institutional-section-heading"><div><h3>Payment ${payments.length - index} · ${tx.batch.k} commitments</h3><small class="mono">Block ${tx.block} · ${short(tx.hash)}</small></div><span class="policy-badge">ZK proof verified</span></div><div class="table-wrap institutional-table"><table data-institutional-payments><thead><tr><th>Published account</th><th>Posted ΔCᵢ</th><th>Updated Bᵢ′</th><th>Opened change</th></tr></thead><tbody>${tx.batch.rows.map(row => `<tr data-payment-account="${row.accountId}" data-open="${canOpen(viewer, row.accountId)}"><td>${row.accountId} · ${row.name}</td><td>${point(row.delta)}</td><td>${point(row.after)}</td><td>${canOpen(viewer, row.accountId) ? `<strong>${signed(row.value)} EN</strong><small>${format(row.previousBalance)} → ${format(row.balance)} EN</small>` : '<span class="institutional-sealed">Sealed</span>'}</td></tr>`).join("")}</tbody></table></div><details class="institutional-batch-details"><summary>Inspect contract call and verification</summary>${publicPosting(p, tx.batch)}<ol class="institutional-checks">${verificationChecks.map(check => `<li>✓ ${check}</li>`).join("")}</ol></details></section>`).join("");
}

export function institutionalChainCard(p) {
  const viewer = institutionalUI.viewer;
  return panel("Public chain · account ledger", "Registered balances and posted payments", `<p>The ledger stores Pedersen commitments on BabyJubJub. Select a registered participant to open its own balance and payment deltas with its private information.</p><div class="institutional-view-selector"><label>Open balances as<select id="institutionalViewer"><option value="public" ${viewer === "public" ? "selected" : ""}>Public network · commitments only</option>${accountOptions(p, viewer)}</select></label><span>${viewer === "public" ? "All balances and payment amounts are sealed." : `Only ${p.registrations[Number(viewer) - 1]?.name}’s balance and updates are opened.`}</span></div>${institutionalAccounts(p, viewer)}<div class="institutional-section-heading"><h3>Posted commitment batches</h3><span>${p.transactions.filter(tx => tx.batch).length} posted</span></div>${paymentHistory(p, viewer)}`);
}

export function institutionalControlCard(p) {
  const s = institutionalState(p), role = institutionalUI.controlsRole;
  return panel("Contract owner and auditor", "Manage trading controls", `<div class="institutional-view-selector"><label>Act as<select id="institutionalControlRole"><option value="auditor" ${role === "auditor" ? "selected" : ""}>Auditor · user trading controls</option><option value="owner" ${role === "owner" ? "selected" : ""}>Owner · contract pause / resume</option></select></label></div><section class="institutional-control-scope"><div><p class="eyebrow">Owner only</p><h3>${s.paused ? "Contract paused" : "Contract active"}</h3><p>Only the contract owner can call <code>Enygma.pause()</code> or <code>Enygma.unpause()</code>. Pausing stops payments for every user. Auditor view-key access does not grant this authority.</p></div>${button(s.paused ? "resume-contract" : "pause-contract", s.paused ? "Resume contract" : "Pause contract", role !== "owner")}</section><section class="institutional-control-scope"><div><p class="eyebrow">Auditor · individual users</p><h3>Freeze or restore a user’s trading</h3><p>A frozen user cannot be included in a payment batch as payer, recipient, or privacy participant. Its commitments and audit history remain available.</p></div><span class="context-badge">${s.frozen.length} FROZEN</span></section><div class="table-wrap institutional-table"><table data-institutional-controls><thead><tr><th>Registered user</th><th>Trading status</th><th>Auditor action</th></tr></thead><tbody>${s.accounts.map(a => `<tr data-controlled-account="${a.accountId}"><td>${a.name}</td><td>${s.frozen.includes(a.accountId) ? "Frozen from trading" : "Eligible to trade"}</td><td>${button(s.frozen.includes(a.accountId) ? "unfreeze-user" : "freeze-user", s.frozen.includes(a.accountId) ? "Unfreeze user" : "Freeze user", role !== "auditor", `data-account-id="${a.accountId}"`)}</td></tr>`).join("")}</tbody></table></div><p class="institutional-public-note">Unfreezing a user does not resume a paused contract. Both controls must permit the payment.</p>`);
}

export function institutionalAuditCard(p) {
  const grants = p.registrations.filter(r => r.auditEnvelope).length;
  return panel("Auditor workspace", "Open the authorized audit records", `<div class="audit-registration-status"><span>✓</span><div><strong>View-key access established during registration</strong><small>${grants} registered users shared their view keys with this auditor.</small></div></div><p>Use the granted audit access to inspect balance openings and each posted commitment delta. The secret spend keys remain with the participants. Contract pause remains an owner-only action.</p>${institutionalAccounts(p, "auditor")}<div class="institutional-section-heading"><h3>Audited payment history</h3><a href="#/institutional/policy">Open trading controls →</a></div>${paymentHistory(p, "auditor")}`, "AUDITOR");
}

export function institutionalPayload(action, target) {
  if (action === "fund") return { amount: document.querySelector("#institutionalFundingAmount")?.value };
  if (action === "calculate") return { ...institutionalUI, accountIds: [...institutionalUI.accountIds], amount: document.querySelector("#institutionalAmount")?.value };
  if (["pause-contract", "resume-contract", "freeze-user", "unfreeze-user"].includes(action)) return { actor: institutionalUI.controlsRole, accountId: target.closest("[data-action]")?.dataset.accountId };
  return {};
}

export function changeInstitutionalControl(target, p) {
  const ui = institutionalUI;
  if (target.id === "institutionalAmount") { ui.amount = Number(target.value); return false; }
  if (target.id === "institutionalViewer") ui.viewer = target.value;
  else if (target.id === "institutionalControlRole") ui.controlsRole = target.value;
  else if (target.matches("[data-institutional-member]")) {
    const id = Number(target.dataset.institutionalMember);
    ui.accountIds = target.checked ? [...new Set([...ui.accountIds, id])] : ui.accountIds.filter(value => value !== id);
  } else if (["institutionalPayer", "institutionalRecipient", "institutionalK", "institutionalAmount"].includes(target.id)) {
    const field = { institutionalPayer: "payerId", institutionalRecipient: "recipientId", institutionalK: "k", institutionalAmount: "amount" }[target.id];
    const previous = ui[field];
    ui[field] = Number(target.value);
    if (ui.payerId === ui.recipientId) ui[field === "payerId" ? "recipientId" : "payerId"] = previous;
    if (field !== "amount") selectInstitutionalSet(p);
  } else return false;
  return true;
}

export function inputInstitutionalAmount(target) {
  if (target.id !== "institutionalAmount") return;
  institutionalUI.amount = Number(target.value);
  const payerCell = document.querySelector(`[data-institutional-account="${institutionalUI.payerId}"] td:last-child b`);
  const recipientCell = document.querySelector(`[data-institutional-account="${institutionalUI.recipientId}"] td:last-child b`);
  if (payerCell) payerCell.textContent = `Payer · ${signed(-institutionalUI.amount)} EN`;
  if (recipientCell) recipientCell.textContent = `Recipient · ${signed(institutionalUI.amount)} EN`;
}

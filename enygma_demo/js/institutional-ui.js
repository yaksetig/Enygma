import { institutionalState, institutionalPaymentView, G, H } from "./institutional.js";
import { pedersen, encodePoint } from "./institutional-crypto.js";

export const institutionalUI = { payerId: 1, recipientId: 2, k: 2, amount: 100, accountIds: [1, 2], fundingRecipientId: 1, fundingAmount: 1000, viewer: "public", channelViewer: "public", controlsRole: "auditor" };
const format = n => Number(n).toLocaleString("en-US");
const short = s => String(s).length > 20 ? `${String(s).slice(0, 10)}…${String(s).slice(-7)}` : String(s);
const signed = n => n > 0 ? `+${format(n)}` : n < 0 ? `−${format(-n)}` : "0";
const point = (p, label = "Coordinates") => `<details class="institutional-point"><summary title="Show ${label}"><span class="point-coordinates"><span>x ${short(p[0])}</span><span>y ${short(p[1])}</span></span></summary><code>x = ${p[0]}<br>y = ${p[1]}</code></details>`;
const button = (action, label, disabled = false, extra = "") => `<button class="button button-primary" data-action="${action}" ${disabled ? "disabled" : ""} ${extra}>${label}</button>`;
const panel = (eyebrow, title, content, badge = "") => `<article class="panel flow-card institutional-card"><div class="panel-heading"><div><p class="eyebrow">${eyebrow}</p><h2>${title}</h2></div>${badge ? `<span class="context-badge">${badge}</span>` : ""}</div>${content}</article>`;
const canOpen = (viewer, id) => viewer === "auditor" || viewer === String(id);
const coordinates = (value, label = "Inspect the stored point") => `<details class="mint-coordinate-details"><summary>${label}</summary><p>The commitment is one point on the curve. These two coordinates identify that point; they are not two separate balances.</p><code>x = ${value[0]}<br>y = ${value[1]}</code></details>`;

function mintCommitmentExplanation(p) {
  const s = institutionalState(p), ui = institutionalUI;
  const recipient = s.accounts.find(a => a.accountId === ui.fundingRecipientId);
  const amount = Number(ui.fundingAmount);
  if (!recipient || !Number.isSafeInteger(amount) || amount < 1 || amount > 1_000_000_000) return `<section id="institutionalMintExplanation" class="mint-commitment-guide"><h3>Money recorded as a commitment</h3><p>Enter a whole-token amount to see how the recipient’s commitment will be updated.</p></section>`;
  const amountLabel = format(amount);
  return `<section id="institutionalMintExplanation" class="mint-commitment-guide"><p class="eyebrow">From EN to a balance commitment</p><h3>How ${amountLabel} EN reaches ${recipient.name}</h3><p>The ledger credits the user by adding a <strong>commitment representing ${amountLabel} EN</strong> to their existing balance commitment.</p><div class="mint-commitment-terms"><div class="mint-commitment-term"><small>Amount part</small><strong>${amountLabel} · G</strong><span>G is a fixed curve point shared by everyone. Multiplying it by the amount represents that amount on the curve.</span></div><b aria-hidden="true">+</b><div class="mint-commitment-term"><small>Blinding part</small><strong>r_mint · H</strong><span>H is another fixed curve point. A secret random number r_mint adds a mask to the commitment.</span></div></div><div class="mint-commitment-equation"><div><small>${recipient.name} · before</small><strong>B_before</strong></div><b>+</b><div><small>Minted commitment</small><strong>${amountLabel}G + r_mint H</strong></div><b>=</b><div><small>${recipient.name} · after</small><strong>B_before + C_mint</strong></div></div><p>The mint amount and recipient are public: adding a mask does not make that announced amount secret. The current mint adds a fresh secret blinding factor; registration also starts with a blinded zero balance, <code>0G + r_start H</code>.</p>${coordinates(recipient.commitment, "Inspect the recipient’s current point")}<div class="mint-commitment-note"><strong>What if there is no randomness?</strong><p>Starting from <code>0G + 0H = (0, 1)</code>, minting ${amountLabel} EN with <code>r = 0</code> gives <code>${amountLabel}G + 0H</code>. Two accounts funded this way have identical coordinates. A commitment without a secret mask does not hide the amount.</p>${coordinates(encodePoint(pedersen(amount, 0)), `Compare: the point for ${amountLabel}G + 0H`)}</div></section>`;
}

function mintCommitmentAccounts(p) {
  const s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-mint-balances><thead><tr><th>Recipient</th><th>What the ledger stores</th><th>Meaning</th></tr></thead><tbody>${s.accounts.map(a => `<tr data-mint-account="${a.accountId}"><td><strong>${a.name}</strong><small>Account ${a.accountId}</small></td><td><strong class="mint-balance-formula">v · G + r · H</strong>${coordinates(a.commitment, "Inspect x and y")}</td><td>The account’s balance plus its secret mask.</td></tr>`).join("")}</tbody></table></div>`;
}

function mintReceiptCalculation(mint) {
  if (!mint?.before || !mint?.after) return "";
  return `<details class="mint-coordinate-details"><summary>How this mint changed the commitment</summary><p>Existing commitment + ${format(mint.amount)} EN commitment = updated balance commitment. Addition uses the curve’s point-addition rule.</p><code>B_before = (${mint.before.join(", ")})<br>+ C_mint = (${mint.commitment.join(", ")})<br>= B_after = (${mint.after.join(", ")})</code></details>`;
}

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
  const s = institutionalState(p), ui = institutionalUI;
  const recipient = s.accounts.find(a => a.accountId === ui.fundingRecipientId);
  const mints = p.transactions.filter(tx => tx.type === "fund");
  const history = mints.length ? `<section class="institutional-mint-history"><div class="institutional-section-heading"><h3>Recent mints</h3><span>${mints.length} confirmed</span></div>${mints.slice(0, 5).map(tx => `<div class="institutional-mint-receipt" data-mint-receipt><span aria-hidden="true">✓</span><div><strong>${tx.label}</strong><small>${tx.mint ? `Account ${tx.mint.recipientId} · ` : ""}Confirmed${tx.block ? ` · block ${tx.block}` : ""}</small>${mintReceiptCalculation(tx.mint)}</div></div>`).join("")}</section>` : "";
  return panel("Owner · issuance", "Mint funds to a participant", `<p>Choose a registered recipient and the amount of EN to issue. Repeat to fund another participant or add to an existing allocation. Each mint updates only the chosen account’s balance commitment.</p><div class="institutional-mint-summary"><span>Total EN issued<strong>${format(s.totalSupplyAmount)} EN</strong></span><span>Minting authority<strong>Contract owner</strong></span></div><div class="institutional-controls institutional-mint-controls"><label>Recipient<select id="institutionalFundingRecipient" ${s.paused ? "disabled" : ""}>${accountOptions(p, ui.fundingRecipientId)}</select></label><label>Amount to mint · EN<input id="institutionalFundingAmount" type="number" min="1" max="1000000000" step="1" value="${ui.fundingAmount}" ${s.paused ? "disabled" : ""}></label></div>${mintCommitmentExplanation(p)}${button("fund", `Mint EN to ${recipient?.name || "recipient"}`, s.paused || !recipient)}${s.paused ? '<div class="callout">The owner must resume the contract before minting.</div>' : ""}${history}<div class="institutional-section-heading"><h3>Account commitments</h3><span>${s.accounts.length} registered accounts</span></div>${mintCommitmentAccounts(p)}`, "OWNER ONLY");
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

function paymentProof(draft, disabled) {
  const claims = [
    ["I own the account being debited.", "I know its secret spending key and the balance behind its current commitment."],
    ["I have enough money to pay.", "The payment fits within my balance. It cannot leave me with a negative balance."],
    ["The amount spent equals the amount received.", "Every token taken from the payer is credited to the recipient. No money is created or lost."],
    [`All ${draft.k} commitments are correctly built.`, "Each commitment represents its intended balance change, combined with the correct secret mask."],
    ["I used the shared keys established with these participants.", "Those keys produce the masks and recognition tags, so participants can identify and read their own updates."]
  ];
  const result = !draft.proof ? `${button("prove", "Generate proof of these claims", disabled)}<p class="institutional-proof-next">Generating a proof does not move funds. The contract must verify it before changing any balances.</p>`
    : draft.status === "confirmed" ? `<div class="institutional-proof-result accepted"><strong>Proof accepted · payment complete</strong><p>The contract verified the proof and applied all ${draft.k} commitment updates together.</p></div>`
    : ["submitted", "verifying"].includes(draft.status) ? `<div class="institutional-proof-result"><strong>Proof submitted · verification in progress</strong><p>The contract is checking the payment before applying the balance updates.</p></div>`
    : `<div class="institutional-proof-result"><strong>Proof generated · ready to submit</strong><p>The proof is tied to these ${draft.k} commitments. No funds have moved yet; the contract still needs to verify the payment.</p></div>`;
  return `<section class="institutional-proof" aria-labelledby="institutionalProofTitle">
    <p class="eyebrow">Payer generates the proof</p><h3 id="institutionalProofTitle">What this proof says</h3>
    <blockquote class="institutional-proof-statement">“Here are ${draft.k} commitments for one private transfer. I own the account being debited, and I can prove this payment follows the rules below.”</blockquote>
    <p>Each commitment records one account’s balance change while hiding the amount.</p>
    <div class="institutional-proof-roles" aria-label="Accounts in this payment"><div><strong>1</strong><span>account pays</span></div><div><strong>1</strong><span>account receives</span></div>${draft.k > 2 ? `<div><strong>${draft.k - 2}</strong><span>extra accounts keep the same amounts</span></div>` : ""}</div>
    ${draft.k > 2 ? '<p class="institutional-proof-context">The extra accounts get new commitments, but no money moves in or out of them. They help hide which accounts are paying and receiving.</p>' : ""}
    <ol class="institutional-proof-claims">${claims.map(([title, description], i) => `<li><span aria-hidden="true">${i + 1}</span><div><strong>${title}</strong><p>${description}</p></div></li>`).join("")}</ol>
    <p class="institutional-proof-privacy"><strong>What stays private?</strong> The proof lets the contract check these rules without revealing the payment amount, account balances, or secret keys.</p>
    ${result}
    <details class="institutional-proof-technical"><summary>Technical proof details</summary><dl><div><dt>Proof system</dt><dd>Groth16 over BN254</dd></div><div><dt>Private inputs</dt><dd>Spend key, previous balance opening, amount changes, shared secrets and blinding factors.</dd></div><div><dt>Public inputs</dt><dd>Registered account set, public keys, balance commitments, commitment deltas, pairwise fingerprints, tags, epoch anchor, nullifier and domain.</dd></div></dl>${draft.proof ? `<p>Checks performed during proof generation:</p><ul class="institutional-checks">${draft.checks.map(check => `<li>✓ ${check}</li>`).join("")}</ul><div class="institutional-proof-id"><small>Generated proof reference</small><code>${draft.proof.id}</code></div>` : ""}</details>
  </section>`;
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
  const proof = draft ? paymentProof(draft, s.paused || !validSelection) : "";
  const verification = draft?.proof ? `<section class="institutional-verification"><p class="eyebrow">On-chain verification</p><h3>${draft.status === "confirmed" ? "Verified · balances updated" : draft.status === "verifying" ? "Enygma is verifying the batch…" : "Verify before updating balances"}</h3><ol>${verificationChecks.map((check, i) => `<li class="${(draft.verification || 0) > i ? "complete" : ""}"><b>${(draft.verification || 0) > i ? "✓" : i + 1}</b>${check}</li>`).join("")}</ol>${draft.status === "proved" ? button("post", `Post ${draft.k} commitments and proof`, s.paused || !validSelection) : draft.status === "confirmed" ? '<a class="button button-primary" href="#/institutional/chain">Inspect the transaction →</a>' : '<span class="policy-badge">Transaction pending</span>'}</section>` : "";
  return panel("Payer’s private workspace", "Build a commitment batch", `${progress}<p>Choose any registered payer and recipient. With <strong>k=2</strong>, the two accounts keep their amounts confidential. With <strong>k=6</strong>, four additional users receive zero-value commitment updates. Choose their rows below.</p>${controls}${gates}${constructionTable(p, draft)}<div class="institutional-selection-summary"><span>${ui.accountIds.length} / ${ui.k} users selected</span><strong>Payer: ${p.registrations[ui.payerId - 1]?.name}</strong></div>${!draft ? button("calculate", `Calculate ${ui.k} commitments`, !p.flow.channels || !s.funded || s.paused || !validSelection) : `${commitmentCalculation(draft)}${proof}${publicPosting(p, draft)}${verification}<div class="button-row">${button("edit-payment", draft.status === "confirmed" ? "Build another payment" : "Edit payment", ["submitted", "verifying"].includes(draft.status))}</div>`}`, `${ui.k} COMMITMENTS`);
}

function paymentViewExplanation(view) {
  const own = view.rows.find(row => row.own), count = view.rows.length;
  if (view.scope === "sender") return ["You created this payment.", `You know the amount and secret mask for all ${count} slots, including any that move no money. Opening these changes does not reveal other participants’ total balances.`];
  if (view.scope === "auditor") return ["Audit access opens every slot.", "The view-key access granted at registration lets you inspect all payment changes and the corresponding account balances."];
  if (view.scope === "participant") return [own.opening.value > 0 ? `You received ${format(own.opening.value)} EN.` : "Your balance did not change.", count > 2
    ? `Your shared key opens your slot. The other ${count - 1} slots remain hidden: you cannot tell whether other participants received money or received nothing.`
    : "Your shared key opens only your slot directly. With just two accounts, the other amount can be inferred because money out must equal money in."];
  if (view.scope === "outside") return ["You are outside this payment.", "None of these slots belongs to you, so no payment amounts are opened in your view."];
  return ["The transaction is verified. Its amounts stay hidden.", "You can see the participating accounts and their commitments. The proof confirms a valid payment without identifying which slots sent money, received money, or moved no money."];
}

function paymentSlot(row, index) {
  const open = Boolean(row.opening), value = row.opening?.value;
  const role = !open ? "Not opened" : value < 0 ? "Sent" : value > 0 ? "Received" : "No money moved";
  const balance = row.balanceOpening ? `<p class="institutional-slot-balance">${row.own ? "Your balance" : "Account balance"}: <strong>${format(row.balanceOpening.before)} → ${format(row.balanceOpening.after)} EN</strong></p>` : "";
  const opening = open ? `<details class="institutional-slot-opening"><summary>Verify this opening</summary><p>This amount and secret mask reproduce the posted commitment.</p><code>Amount change = ${signed(value)} EN<br>Secret mask r = ${row.opening.randomness}<br>ΔC = (${value})G + rH<br>ΔC = (${row.delta.join(", ")})</code></details>` : "";
  return `<article class="institutional-payment-slot ${open ? "opened" : "sealed"}" data-payment-account="${row.accountId}" data-open="${open}"><header><span class="institutional-slot-number">${index + 1}</span><div><h4>${row.name}</h4><small>Account ${row.accountId}${row.own ? " · Your account" : ""}</small></div><span class="institutional-slot-status">${open ? "Opened" : "Sealed"}</span></header><div class="institutional-slot-amount"><small>${role}</small><strong>${open ? `${signed(value)} EN` : "Amount hidden"}</strong></div>${balance}${!open ? '<p class="institutional-slot-unknown">No opening is available for this slot in your view.</p>' : value === 0 ? '<p class="institutional-slot-unknown">The commitment changed; the amount stayed the same.</p>' : ""}${opening}<details class="institutional-slot-points"><summary>Public commitment points</summary><p>Posted balance change</p><code>(${row.delta.join(", ")})</code><p>Updated balance commitment</p><code>(${row.after.join(", ")})</code></details></article>`;
}

function paymentHistory(p, viewer) {
  const payments = p.transactions.filter(tx => tx.batch);
  if (!payments.length) return '<div class="empty-state">No payment has been posted yet. Build and verify a payment, then return to inspect it from each participant’s view.</div>';
  return payments.map((tx, index) => {
    const view = institutionalPaymentView(tx.batch, viewer);
    const [title, explanation] = paymentViewExplanation(view);
    const opened = view.rows.filter(row => row.opening).length;
    return `<section class="institutional-batch" data-payment-batch="${tx.id}"><div class="institutional-section-heading"><div><h3>Payment ${payments.length - index} · ${tx.batch.k} commitments</h3><small class="mono">Block ${tx.block} · ${short(tx.hash)}</small></div><span class="policy-badge">ZK proof verified</span></div><div class="institutional-payment-perspective"><div><strong>${title}</strong><p>${explanation}</p></div><span><b>${opened} / ${tx.batch.k}</b>slots opened</span></div><div class="institutional-payment-slots" data-institutional-payments>${view.rows.map(paymentSlot).join("")}</div><details class="institutional-batch-details"><summary>Inspect contract call and verification</summary>${publicPosting(p, tx.batch)}<ol class="institutional-checks">${verificationChecks.map(check => `<li>✓ ${check}</li>`).join("")}</ol></details></section>`;
  }).join("");
}

export function institutionalChainCard(p) {
  const viewer = institutionalUI.viewer;
  return panel("Transaction inspection", "What can each participant see?", `<p>Choose a view to open the parts of each payment that participant knows. The sender can open every slot in a payment they created. Each other participant can open only their own slot.</p><div class="institutional-view-selector"><label>Inspect transactions as<select id="institutionalViewer"><option value="public" ${viewer === "public" ? "selected" : ""}>Public network · all amounts hidden</option>${accountOptions(p, viewer)}<option value="auditor" ${viewer === "auditor" ? "selected" : ""}>Auditor · authorized audit access</option></select></label><span>${viewer === "public" ? "No private openings are available to the public." : viewer === "auditor" ? "View-key access opens all registered accounts and their payment slots." : "Your access is evaluated for each payment: all slots if you sent it, only your slot otherwise."}</span></div>${paymentHistory(p, viewer)}<details class="institutional-account-ledger"><summary>Current account balances and commitments</summary><p>The ledger stores Pedersen commitments on BabyJubJub. Participants can open only their own total balance, even when they know every amount in a payment they sent. The auditor has the registered users’ view-key access.</p>${institutionalAccounts(p, viewer)}</details>`);
}

export function institutionalControlCard(p) {
  const s = institutionalState(p), role = institutionalUI.controlsRole;
  const canManageUsers = ["owner", "auditor"].includes(role);
  return panel("Contract owner and auditor", "Manage trading controls", `<div class="institutional-view-selector"><label>Act as<select id="institutionalControlRole"><option value="auditor" ${role === "auditor" ? "selected" : ""}>Auditor · user trading controls</option><option value="owner" ${role === "owner" ? "selected" : ""}>Owner · user controls and contract pause</option></select></label></div><section class="institutional-control-scope"><div><p class="eyebrow">Owner only</p><h3>${s.paused ? "Contract paused" : "Contract active"}</h3><p>Only the contract owner can call <code>Enygma.pause()</code> or <code>Enygma.unpause()</code>. Pausing stops payments for every user. Auditor view-key access does not grant this authority.</p></div>${button(s.paused ? "resume-contract" : "pause-contract", s.paused ? "Resume contract" : "Pause contract", role !== "owner")}</section><section class="institutional-control-scope"><div><p class="eyebrow">Owner and auditor · individual users</p><h3>Freeze or restore a user’s trading</h3><p>Both the owner and auditor can freeze or unfreeze a user. A frozen user cannot be included in a payment batch as payer, recipient, or privacy participant. Its commitments and audit history remain available.</p></div><span class="context-badge">${s.frozen.length} FROZEN</span></section><div class="table-wrap institutional-table"><table data-institutional-controls><thead><tr><th>Registered user</th><th>Trading status</th><th>Trading action</th></tr></thead><tbody>${s.accounts.map(a => `<tr data-controlled-account="${a.accountId}"><td>${a.name}</td><td>${s.frozen.includes(a.accountId) ? "Frozen from trading" : "Eligible to trade"}</td><td>${button(s.frozen.includes(a.accountId) ? "unfreeze-user" : "freeze-user", s.frozen.includes(a.accountId) ? "Unfreeze user" : "Freeze user", !canManageUsers, `data-account-id="${a.accountId}"`)}</td></tr>`).join("")}</tbody></table></div><p class="institutional-public-note">Unfreezing a user does not resume a paused contract. Both controls must permit the payment.</p>`);
}

export function institutionalAuditCard(p) {
  const grants = p.registrations.filter(r => r.auditEnvelope).length;
  return panel("Auditor workspace", "Open the authorized audit records", `<div class="audit-registration-status"><span>✓</span><div><strong>View-key access established during registration</strong><small>${grants} registered users shared their view keys with this auditor.</small></div></div><p>Use the granted audit access to inspect balance openings and each posted commitment delta. The secret spend keys remain with the participants. Contract pause remains an owner-only action.</p>${institutionalAccounts(p, "auditor")}<div class="institutional-section-heading"><h3>Audited payment history</h3><a href="#/institutional/policy">Open trading controls →</a></div>${paymentHistory(p, "auditor")}`, "AUDITOR");
}

export function institutionalPayload(action, target) {
  if (action === "fund") return { actor: "owner", recipientId: document.querySelector("#institutionalFundingRecipient")?.value, amount: document.querySelector("#institutionalFundingAmount")?.value };
  if (action === "calculate") return { ...institutionalUI, accountIds: [...institutionalUI.accountIds], amount: document.querySelector("#institutionalAmount")?.value };
  if (["pause-contract", "resume-contract", "freeze-user", "unfreeze-user"].includes(action)) return { actor: institutionalUI.controlsRole, accountId: target.closest("[data-action]")?.dataset.accountId };
  return {};
}

export function changeInstitutionalControl(target, p) {
  const ui = institutionalUI;
  if (target.id === "institutionalFundingAmount") { ui.fundingAmount = target.value; return false; }
  if (target.id === "institutionalAmount") { ui.amount = Number(target.value); return false; }
  if (target.id === "institutionalViewer") ui.viewer = target.value;
  else if (target.id === "institutionalFundingRecipient") ui.fundingRecipientId = Number(target.value);
  else if (target.id === "institutionalChannelViewer") ui.channelViewer = target.value;
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

export function inputInstitutionalAmount(target, p) {
  if (target.id === "institutionalFundingAmount") {
    institutionalUI.fundingAmount = target.value;
    const explanation = document.querySelector("#institutionalMintExplanation");
    if (explanation) explanation.outerHTML = mintCommitmentExplanation(p);
    return;
  }
  if (target.id !== "institutionalAmount") return;
  institutionalUI.amount = Number(target.value);
  const payerCell = document.querySelector(`[data-institutional-account="${institutionalUI.payerId}"] td:last-child b`);
  const recipientCell = document.querySelector(`[data-institutional-account="${institutionalUI.recipientId}"] td:last-child b`);
  if (payerCell) payerCell.textContent = `Payer · ${signed(-institutionalUI.amount)} EN`;
  if (recipientCell) recipientCell.textContent = `Recipient · ${signed(institutionalUI.amount)} EN`;
}

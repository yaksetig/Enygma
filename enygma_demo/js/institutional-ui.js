import { institutionalState, institutionalPaymentView, G, H } from "./institutional.js";

export const institutionalUI = { payerId: 1, recipients: [{ accountId: 2, amount: "100" }, { accountId: 3, amount: "50" }], k: 6, accountIds: [1, 2, 3, 4, 5, 6], fundingRecipientId: 1, fundingAmount: 1000, viewer: "public", channelViewer: "public", controlsRole: "auditor" };
const format = n => Number(n).toLocaleString("en-US");
const short = s => String(s).length > 20 ? `${String(s).slice(0, 10)}…${String(s).slice(-7)}` : String(s);
const signed = n => n > 0 ? `+${format(n)}` : n < 0 ? `−${format(-n)}` : "0";
const point = (p, label = "Coordinates") => `<details class="institutional-point"><summary title="Show ${label}"><span class="point-coordinates"><span>x ${short(p[0])}</span><span>y ${short(p[1])}</span></span></summary><code>x = ${p[0]}<br>y = ${p[1]}</code></details>`;
const button = (action, label, disabled = false, extra = "") => `<button class="button button-primary" data-action="${action}" ${disabled ? "disabled" : ""} ${extra}>${label}</button>`;
const panel = (eyebrow, title, content, badge = "") => `<article class="panel flow-card institutional-card"><div class="panel-heading"><div><p class="eyebrow">${eyebrow}</p><h2>${title}</h2></div>${badge ? `<span class="context-badge">${badge}</span>` : ""}</div>${content}</article>`;
const canOpen = (viewer, id) => viewer === "auditor" || viewer === String(id);
const coordinates = (value, label = "Coordinates") => `<details class="mint-coordinate-details"><summary>${label}</summary><code>x = ${value[0]}<br>y = ${value[1]}</code></details>`;
const recipientsFor = payment => payment.recipients || [{ accountId: payment.recipientId, amount: payment.amount }];
const paymentTotal = payment => recipientsFor(payment).reduce((sum, recipient) => sum + Number(recipient.amount), 0);
const recipientInputId = (field, index) => `institutional${field}${index || ""}`;

function mintCommitmentExplanation(p) {
  const s = institutionalState(p), ui = institutionalUI;
  const recipient = s.accounts.find(a => a.accountId === ui.fundingRecipientId);
  const amount = Number(ui.fundingAmount);
  if (!recipient || !Number.isSafeInteger(amount) || amount < 1 || amount > 1_000_000_000) return `<p id="institutionalMintExplanation" class="mint-commitment-guide">Choose a recipient and a whole-token amount to mint.</p>`;
  const amountLabel = format(amount);
  return `<p id="institutionalMintExplanation" class="mint-commitment-guide">Minting adds <strong>${amountLabel}G + 0H</strong> to the balance commitment for <strong>${recipient.name}</strong>.</p>`;
}

function mintCommitmentAccounts(p) {
  const s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-mint-balances><thead><tr><th>Recipient</th><th>Balance</th><th>Balance commitment</th></tr></thead><tbody>${s.accounts.map(a => {
    const transparent = a.randomness === "0";
    return `<tr data-mint-account="${a.accountId}"><td><strong>${a.name}</strong></td><td>${transparent ? `${format(a.balance)} EN` : "Private"}</td><td><strong class="mint-balance-formula">${transparent ? `${format(a.balance)}G + 0H` : "vG + rH"}</strong>${coordinates(a.commitment)}</td></tr>`;
  }).join("")}</tbody></table></div>`;
}

function mintReceiptCalculation(mint) {
  if (!mint?.before || !mint?.after) return "";
  return `<details class="mint-coordinate-details"><summary>How this mint changed the commitment</summary><p>Existing commitment + ${format(mint.amount)} EN commitment = updated balance commitment. Addition uses the curve’s point-addition rule.</p><code>B_before = (${mint.before.join(", ")})<br>+ C_mint = (${mint.commitment.join(", ")})<br>= B_after = (${mint.after.join(", ")})</code></details>`;
}

export function selectInstitutionalSet(p) {
  const s = institutionalState(p), ui = institutionalUI;
  const required = [ui.payerId, ...ui.recipients.map(recipient => recipient.accountId)];
  ui.accountIds = [...new Set([...required, ...ui.accountIds.filter(id => !s.frozen.includes(id))])].slice(0, ui.k);
  for (const a of s.accounts) if (ui.accountIds.length < ui.k && !ui.accountIds.includes(a.accountId) && !s.frozen.includes(a.accountId)) ui.accountIds.push(a.accountId);
}

function accountOptions(p, selected, excluded = []) {
  return p.registrations.map((r, i) => `<option value="${i + 1}" ${Number(selected) === i + 1 ? "selected" : ""} ${excluded.includes(i + 1) ? "disabled" : ""}>${i + 1} · ${r.name}</option>`).join("");
}

export function institutionalAccounts(p, viewer = "public") {
  const s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-balances><thead><tr><th>Account</th><th>Registered user</th><th>Current balance commitment Bᵢ</th><th>Trading</th><th>Balance opening</th></tr></thead><tbody>${s.accounts.map(a => {
    const open = a.randomness === "0" || canOpen(viewer, a.accountId);
    return `<tr data-balance-account="${a.accountId}" data-open="${open}"><td>${a.accountId}</td><td><strong>${a.name}</strong></td><td>${point(a.commitment, "balance commitment")}</td><td><span class="policy-badge ${s.frozen.includes(a.accountId) ? "selective" : ""}">${s.frozen.includes(a.accountId) ? "Frozen" : "Eligible"}</span></td><td>${open ? `<div class="institutional-opening"><strong>${format(a.balance)} EN</strong><details><summary>Verify opening</summary><code>v = ${a.balance}<br>r = ${a.randomness}<br>Bᵢ = v·G + r·H ✓</code></details></div>` : `<span class="institutional-sealed">Sealed balance</span>`}</td></tr>`;
  }).join("")}</tbody></table></div>`;
}

export function institutionalFundingCard(p) {
  const s = institutionalState(p), ui = institutionalUI;
  const recipient = s.accounts.find(a => a.accountId === ui.fundingRecipientId);
  const mints = p.transactions.filter(tx => tx.type === "fund");
  const history = mints.length ? `<section class="institutional-mint-history"><div class="institutional-section-heading"><h3>Recent mints</h3><span>${mints.length} confirmed</span></div>${mints.slice(0, 5).map(tx => `<div class="institutional-mint-receipt" data-mint-receipt><span aria-hidden="true">✓</span><div><strong>${tx.label}</strong><small>${tx.mint ? `Account ${tx.mint.recipientId} · ` : ""}Confirmed${tx.block ? ` · block ${tx.block}` : ""}</small>${mintReceiptCalculation(tx.mint)}</div></div>`).join("")}</section>` : "";
  return panel("Owner · issuance", "Mint funds to a participant", `<p>Every account starts at <strong>0 EN</strong>, stored as <strong>0G + 0H</strong>. Minting adds the chosen amount with zero randomness. Private transfers introduce random factors later.</p><div class="institutional-controls institutional-mint-controls"><label>Recipient<select id="institutionalFundingRecipient" ${s.paused ? "disabled" : ""}>${accountOptions(p, ui.fundingRecipientId)}</select></label><label>Amount to mint · EN<input id="institutionalFundingAmount" type="number" min="1" max="1000000000" step="1" value="${ui.fundingAmount}" ${s.paused ? "disabled" : ""}></label></div>${button("fund", `Mint EN to ${recipient?.name || "recipient"}`, s.paused || !recipient)}${mintCommitmentExplanation(p)}${s.paused ? '<div class="callout">The owner must resume the contract before minting.</div>' : ""}<div class="institutional-section-heading"><h3>Account balances</h3><span>Total issued · ${format(s.totalSupplyAmount)} EN</span></div>${mintCommitmentAccounts(p)}${history}`, "OWNER ONLY");
}

function constructionTable(p, draft) {
  const ui = draft || institutionalUI, s = institutionalState(p);
  return `<div class="table-wrap institutional-table"><table data-institutional-users><thead><tr><th>In set</th><th>Registered user</th><th>Spend public key</th><th>Balance commitment Bᵢ</th><th>Payer’s instructions</th></tr></thead><tbody>${s.accounts.map(a => {
    const selected = ui.accountIds.includes(a.accountId), payer = a.accountId === ui.payerId, recipient = recipientsFor(ui).find(r => r.accountId === a.accountId), frozen = s.frozen.includes(a.accountId);
    return `<tr data-institutional-account="${a.accountId}" class="${selected ? "institutional-selected" : ""}"><td><input type="checkbox" aria-label="Include ${a.name}" data-institutional-member="${a.accountId}" ${selected ? "checked" : ""} ${draft || payer || recipient || frozen || (!selected && ui.accountIds.length >= ui.k) ? "disabled" : ""}></td><td><strong>${a.name}</strong><small>Account ${a.accountId}${frozen ? " · Frozen" : ""}</small></td><td class="mono">${short(p.registrations[a.accountId - 1].spendPublicKey)}</td><td>${point(a.commitment)}</td><td>${payer ? `<b>Payer · ${signed(-paymentTotal(ui))} EN</b>` : recipient ? `<b>Recipient · ${signed(Number(recipient.amount))} EN</b>` : selected ? "Privacy participant · 0 EN" : "Outside this batch"}</td></tr>`;
  }).join("")}</tbody></table></div>`;
}

function paymentInputError(p, ui) {
  const s = institutionalState(p), recipients = recipientsFor(ui), total = paymentTotal(ui);
  if (s.paused) return "The owner has paused the contract. Resume it before making a payment.";
  if (!p.flow.channels) return "Establish the pairwise channels before building a payment.";
  if (!s.funded) return "Mint funds to the payer before making a payment.";
  if (ui.accountIds.length !== ui.k || ui.accountIds.some(id => s.frozen.includes(id))) return `Select exactly ${ui.k} eligible accounts, including the payer and all recipients.`;
  if (recipients.some(recipient => !Number.isSafeInteger(Number(recipient.amount)) || Number(recipient.amount) <= 0)) return "Enter a positive whole-token amount for each recipient.";
  if (!Number.isSafeInteger(total) || total > s.accounts.find(a => a.accountId === ui.payerId)?.balance) return "The combined payment exceeds the payer’s available balance.";
  return "";
}

function paymentDistribution(ui) {
  const recipients = recipientsFor(ui), total = paymentTotal(ui), zeroCount = ui.k - 1 - recipients.length;
  return `<div id="institutionalPaymentDistribution" class="institutional-payment-distribution"><div><small>Total paid by one account</small><strong>${Number.isSafeInteger(total) ? format(total) : "—"} EN</strong></div><span>${recipients.length} ${recipients.length === 1 ? "recipient" : "recipients"}${zeroCount ? ` · ${zeroCount} zero-value ${zeroCount === 1 ? "slot" : "slots"}` : " · every other account receives funds"}</span></div>`;
}

function recipientControls(p, ui, locked) {
  const recipients = recipientsFor(ui), s = institutionalState(p);
  return `<section class="institutional-recipients"><div class="institutional-section-heading"><h3>Recipients</h3><span>${recipients.length} / ${ui.k - 1} recipient slots</span></div><div class="institutional-recipient-list">${recipients.map((recipient, index) => `<div class="institutional-recipient-row" data-institutional-recipient-row="${index}"><label>Bank ${index + 1}<select id="${recipientInputId("Recipient", index)}" data-institutional-recipient="${index}" ${locked ? "disabled" : ""}>${accountOptions(p, recipient.accountId, [ui.payerId, ...s.frozen, ...recipients.filter((_, i) => i !== index).map(r => r.accountId)])}</select></label><label>Amount · EN<input id="${recipientInputId("Amount", index)}" data-institutional-recipient-amount="${index}" type="number" min="1" step="1" value="${recipient.amount}" ${locked ? "disabled" : ""}></label><button type="button" class="button button-ghost" data-institutional-remove-recipient="${index}" aria-label="Remove recipient ${index + 1}" ${locked || recipients.length === 1 ? "disabled" : ""}>Remove</button></div>`).join("")}</div><button type="button" id="institutionalAddRecipient" class="button button-ghost" data-institutional-add-recipient ${locked || recipients.length >= ui.k - 1 ? "disabled" : ""}>+ Add recipient</button>${recipients.length > 1 ? '<p class="institutional-recipient-hint">A two-account batch needs one recipient. Remove extra recipients to use that size.</p>' : ""}${paymentDistribution(ui)}</section>`;
}

function commitmentCalculation(draft) {
  return `<section class="institutional-calculation"><div class="institutional-section-heading"><div><p class="eyebrow">Payer’s private calculation</p><h3>${draft.k} commitment deltas</h3></div><code>ΔCᵢ = Δvᵢ·G + rᵢ·H</code></div><p>Amounts sum to zero. The payer’s blinding factor balances the other slots, so <code>Σ ΔCᵢ = (0, 1)</code>. A zero-value slot still changes its commitment through <code>rᵢ·H</code>.</p><div class="table-wrap institutional-table"><table data-institutional-calculation><thead><tr><th>Account</th><th>Private Δvᵢ</th><th>Private rᵢ</th><th>Calculated ΔCᵢ</th><th>Calculation</th></tr></thead><tbody>${draft.rows.map(row => `<tr><td>${row.accountId} · ${row.name}</td><td>${signed(row.value)} EN</td><td class="mono">${short(row.r)}</td><td>${point(row.delta)}</td><td><details class="institutional-math"><summary>Show calculation</summary><code>Δvᵢ = ${row.value}<br>rᵢ = ${row.r}<br>Δvᵢ·G = (${row.valuePoint.join(", ")})<br>rᵢ·H = (${row.randomPoint.join(", ")})<br>ΔCᵢ = (${row.delta.join(", ")})<br>Bᵢ′ = Bᵢ + ΔCᵢ<br>Bᵢ′ = (${row.after.join(", ")})</code></details></td></tr>`).join("")}</tbody></table></div><details class="institutional-math institutional-derivation"><summary>Generators and blinding-factor derivation</summary><code>BabyJubJub: 168700x² + y² = 1 + 168696x²y²<br>G = (${G.join(", ")})<br>H = (${H.join(", ")})<br>s_sender = Poseidon(previous_r, sk_spend) mod ℓ<br>η = Poseidon(s_sender, epoch_hash)<br>nonceᵢ = Poseidon(η, Poseidon(sender_id, account_idᵢ))<br>hᵢ = Poseidon(Poseidon(21), sᵢ, nonceᵢ) mod ℓ<br>r_sender = Σ h_other mod ℓ; r_other = −h_other mod ℓ<br>tagᵢ = Poseidon(Poseidon(12), sᵢ, nonceᵢ) mod ℓ</code></details></section>`;
}

function paymentProof(draft, disabled) {
  const receiverCount = draft.rows.filter(row => row.value > 0).length, zeroCount = draft.rows.filter(row => row.value === 0).length;
  const claims = [
    ["I own the account being debited.", "I know its secret spending key and the balance behind its current commitment."],
    ["I have enough money to pay.", "The payment fits within my balance. It cannot leave me with a negative balance."],
    ["The amount spent equals the total received.", "The payer’s debit equals the sum sent to all recipients. No money is created or lost."],
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
    <div class="institutional-proof-roles" aria-label="Accounts in this payment"><div><strong>1</strong><span>account pays</span></div><div><strong>${receiverCount}</strong><span>${receiverCount === 1 ? "account receives" : "accounts receive"}</span></div>${zeroCount ? `<div><strong>${zeroCount}</strong><span>${zeroCount === 1 ? "account has no balance change" : "accounts have no balance change"}</span></div>` : ""}</div>
    <p class="institutional-proof-context">This ${draft.k}-account batch supports up to ${draft.k - 1} ${draft.k === 2 ? "recipient" : "recipients"}. This payment sends a total of ${format(draft.amount)} EN to ${receiverCount} ${receiverCount === 1 ? "recipient" : "recipients"}.${zeroCount ? " The remaining slots get new commitments with no money moving in or out." : " Every non-payer slot receives funds."}</p>
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
  const controls = `<div class="institutional-controls"><label>Payer<select id="institutionalPayer" ${draft ? "disabled" : ""}>${accountOptions(p, ui.payerId, s.frozen)}</select></label><label>Accounts in the batch<select id="institutionalK" ${draft ? "disabled" : ""}><option value="2" ${ui.k === 2 ? "selected" : ""} ${recipientsFor(ui).length > 1 ? "disabled" : ""}>2 accounts · 1 recipient</option><option value="6" ${ui.k === 6 ? "selected" : ""}>6 accounts · up to 5 recipients</option></select></label></div>${recipientControls(p, ui, Boolean(draft))}`;
  const rank = !draft ? 0 : draft.status === "calculated" ? 1 : draft.status === "proved" ? 2 : draft.status === "confirmed" ? 4 : 3;
  const progress = `<ol class="institutional-payment-progress" aria-label="Payment progress">${["Select accounts", "Calculate commitments", "Generate ZK proof", "Post and verify"].map((name, i) => `<li class="${i < rank ? "complete" : i === rank ? "active" : ""}"><b>${i < rank ? "✓" : i + 1}</b>${name}</li>`).join("")}</ol>`;
  const inputError = !draft ? paymentInputError(p, ui) : s.paused ? "The owner has paused the contract." : !validSelection ? "A selected account is frozen from trading." : "";
  const gates = `<div id="institutionalPaymentGate" class="callout" ${inputError ? "" : "hidden"}>${inputError}</div>`;
  const proof = draft ? paymentProof(draft, s.paused || !validSelection) : "";
  const verification = draft?.proof ? `<section class="institutional-verification"><p class="eyebrow">On-chain verification</p><h3>${draft.status === "confirmed" ? "Verified · balances updated" : draft.status === "verifying" ? "Enygma is verifying the batch…" : "Verify before updating balances"}</h3><ol>${verificationChecks.map((check, i) => `<li class="${(draft.verification || 0) > i ? "complete" : ""}"><b>${(draft.verification || 0) > i ? "✓" : i + 1}</b>${check}</li>`).join("")}</ol>${draft.status === "proved" ? button("post", `Post ${draft.k} commitments and proof`, s.paused || !validSelection) : draft.status === "confirmed" ? '<a class="button button-primary" href="#/institutional/chain">Inspect the transaction →</a>' : '<span class="policy-badge">Transaction pending</span>'}</section>` : "";
  return panel("Payer’s private workspace", "Pay multiple banks in one transfer", `${progress}<p>A six-account batch has <strong>one payer and up to five recipients</strong>. Choose each recipient’s amount; the payer spends their combined total. Any remaining slots hold participants with no balance change.</p>${controls}${gates}<div class="institutional-section-heading"><h3>Accounts in this batch</h3><span>${ui.accountIds.length} / ${ui.k} selected</span></div>${constructionTable(p, draft)}${!draft ? button("calculate", `Calculate ${ui.k} commitments`, Boolean(inputError)) : `${commitmentCalculation(draft)}${proof}${publicPosting(p, draft)}${verification}<div class="button-row">${button("edit-payment", draft.status === "confirmed" ? "Build another payment" : "Edit payment", ["submitted", "verifying"].includes(draft.status))}</div>`}`, `${ui.k} COMMITMENTS`);
}

function paymentViewExplanation(view) {
  const own = view.rows.find(row => row.own), count = view.rows.length;
  if (view.scope === "sender" || view.scope === "auditor") {
    const recipients = view.rows.filter(row => row.opening.value > 0), total = recipients.reduce((sum, row) => sum + row.opening.value, 0);
    const distribution = `One account paid ${format(total)} EN to ${recipients.length} ${recipients.length === 1 ? "recipient" : "recipients"} in this transfer.`;
    return view.scope === "sender" ? ["You created this payment.", `${distribution} You can open all ${count} slots, including any that move no money. Opening these changes does not reveal other participants’ total balances.`] : ["Audit access opens every slot.", `${distribution} Your registered view-key access opens all payment changes and account balances.`];
  }
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
  return panel("Transaction inspection", "What can each participant see?", `<p>Choose a view to open the parts of each payment that participant knows. The sender can open every slot in a payment they created. Each other participant can open only their own slot.</p><div class="institutional-view-selector"><label>Inspect transactions as<select id="institutionalViewer"><option value="public" ${viewer === "public" ? "selected" : ""}>Public network · payment amounts hidden</option>${accountOptions(p, viewer)}<option value="auditor" ${viewer === "auditor" ? "selected" : ""}>Auditor · authorized audit access</option></select></label><span>${viewer === "public" ? "No private openings are available to the public." : viewer === "auditor" ? "View-key access opens all registered accounts and their payment slots." : "Your access is evaluated for each payment: all slots if you sent it, only your slot otherwise."}</span></div>${paymentHistory(p, viewer)}<details class="institutional-account-ledger"><summary>Current account balances and commitments</summary><p>Balances with zero randomness are public. After private transfers add random factors, participants can open only their own balance. The auditor can open all registered accounts.</p>${institutionalAccounts(p, viewer)}</details>`);
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

export function institutionalPayload(action, target, p) {
  if (action === "fund") return { actor: "owner", recipientId: document.querySelector("#institutionalFundingRecipient")?.value, amount: document.querySelector("#institutionalFundingAmount")?.value };
  if (action === "calculate") return { payerId: institutionalUI.payerId, k: institutionalUI.k, accountIds: [...institutionalUI.accountIds], recipients: institutionalUI.recipients.map((recipient, index) => ({ accountId: recipient.accountId, amount: document.getElementById(recipientInputId("Amount", index))?.value ?? recipient.amount })) };
  if (action === "edit-payment") {
    const draft = institutionalState(p).draft;
    if (draft) Object.assign(institutionalUI, { payerId: draft.payerId, k: draft.k, accountIds: [...draft.accountIds], recipients: recipientsFor(draft).map(recipient => ({ accountId: recipient.accountId, amount: String(recipient.amount) })) });
  }
  if (["pause-contract", "resume-contract", "freeze-user", "unfreeze-user"].includes(action)) return { actor: institutionalUI.controlsRole, accountId: target.closest("[data-action]")?.dataset.accountId };
  return {};
}

export function changeInstitutionalControl(target, p) {
  const ui = institutionalUI;
  if (target.id === "institutionalFundingAmount") { ui.fundingAmount = target.value; return false; }
  if (target.matches("[data-institutional-recipient-amount]")) return false;
  if (target.id === "institutionalViewer") ui.viewer = target.value;
  else if (target.id === "institutionalFundingRecipient") ui.fundingRecipientId = Number(target.value);
  else if (target.id === "institutionalChannelViewer") ui.channelViewer = target.value;
  else if (target.id === "institutionalControlRole") ui.controlsRole = target.value;
  else if (target.matches("[data-institutional-member]")) {
    const id = Number(target.dataset.institutionalMember);
    ui.accountIds = target.checked ? [...new Set([...ui.accountIds, id])] : ui.accountIds.filter(value => value !== id);
  } else if (target.matches("[data-institutional-recipient]")) {
    const index = Number(target.dataset.institutionalRecipient), accountId = Number(target.value);
    if (accountId === ui.payerId || ui.recipients.some((r, i) => i !== index && r.accountId === accountId)) return false;
    ui.recipients[index].accountId = accountId;
    selectInstitutionalSet(p);
  } else if (target.id === "institutionalPayer") {
    const previous = ui.payerId;
    ui.payerId = Number(target.value);
    const recipient = ui.recipients.find(r => r.accountId === ui.payerId);
    if (recipient) recipient.accountId = previous;
    selectInstitutionalSet(p);
  } else if (target.id === "institutionalK") {
    const k = Number(target.value);
    if (ui.recipients.length > k - 1) return false;
    ui.k = k;
    selectInstitutionalSet(p);
  } else return false;
  return true;
}

export function clickInstitutionalControl(target, p) {
  const ui = institutionalUI, s = institutionalState(p);
  if (s.draft) return false;
  if (target.closest("[data-institutional-add-recipient]") && ui.recipients.length < ui.k - 1) {
    const available = s.accounts.filter(a => a.accountId !== ui.payerId && !s.frozen.includes(a.accountId) && !ui.recipients.some(r => r.accountId === a.accountId));
    const next = available.find(a => ui.accountIds.includes(a.accountId)) || available[0];
    if (!next) return false;
    ui.recipients.push({ accountId: next.accountId, amount: "" });
    selectInstitutionalSet(p);
    return recipientInputId("Amount", ui.recipients.length - 1);
  }
  const remove = target.closest("[data-institutional-remove-recipient]");
  if (remove && ui.recipients.length > 1) {
    ui.recipients.splice(Number(remove.dataset.institutionalRemoveRecipient), 1);
    selectInstitutionalSet(p);
    return "institutionalAddRecipient";
  }
  return false;
}

export function inputInstitutionalAmount(target, p) {
  if (target.id === "institutionalFundingAmount") {
    institutionalUI.fundingAmount = target.value;
    const explanation = document.querySelector("#institutionalMintExplanation");
    if (explanation) explanation.outerHTML = mintCommitmentExplanation(p);
    return;
  }
  if (!target.matches("[data-institutional-recipient-amount]") || institutionalState(p).draft) return;
  const index = Number(target.dataset.institutionalRecipientAmount);
  institutionalUI.recipients[index].amount = target.value;
  const total = paymentTotal(institutionalUI);
  const payerCell = document.querySelector(`[data-institutional-account="${institutionalUI.payerId}"] td:last-child b`);
  const recipientCell = document.querySelector(`[data-institutional-account="${institutionalUI.recipients[index].accountId}"] td:last-child b`);
  if (payerCell) payerCell.textContent = `Payer · ${signed(-total)} EN`;
  if (recipientCell) recipientCell.textContent = `Recipient · ${signed(Number(target.value))} EN`;
  const distribution = document.getElementById("institutionalPaymentDistribution");
  if (distribution) distribution.outerHTML = paymentDistribution(institutionalUI);
  const error = paymentInputError(p, institutionalUI), gate = document.getElementById("institutionalPaymentGate");
  if (gate) { gate.textContent = error; gate.hidden = !error; }
  const calculate = document.querySelector('[data-action="calculate"]');
  if (calculate) calculate.disabled = Boolean(error);
}

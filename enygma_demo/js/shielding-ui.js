import { commitmentTreeExplorer, noteTreeState } from "./commitment-tree-ui.js";

const fmt = value => Number(value).toLocaleString("en-US");
const escape = value => String(value).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const validAmount = value => /^\d+$/.test(String(value)) && Number.isSafeInteger(Number(value)) && Number(value) > 0;
const inputs = new Map();

export function walletSummary({ asset, publicBalance, privateBalance, noteCount, leafCount }) {
  return `<div class="retail-wallet-strip"><div><span>Public ${asset}</span><strong>${fmt(publicBalance)}</strong><small>Available to shield</small></div><b aria-hidden="true">→</b><div class="private"><span>Private ${asset}</span><strong>${fmt(privateBalance)}</strong><small>${noteCount} spendable ${noteCount === 1 ? "note" : "notes"}</small></div><div><span>Network tree</span><strong>${leafCount}</strong><small>Commitment leaves</small></div></div>`;
}

export function shieldNotePreview(amounts, unit = "USD") {
  return amounts.map((amount, i) => `<div class="retail-mini-note" style="--i:${i}"><span>PRIVATE NOTE ${i + 1}</span><strong class="${fmt(amount).length > 7 ? "long-amount" : ""}">${validAmount(amount) ? fmt(amount) : "—"}<small> ${unit}</small></strong><code>C${i + 1} → leaf</code></div>`).join("");
}

export function shieldingScene({ amounts, unit = "USD", asset = "USD", symbol = "$", vault = "Erc20CoinVault", deposit = "depositV2()", phase = "" }) {
  return `<div class="retail-shield-scene ${phase ? `animating phase-${phase}` : ""}" aria-label="Shielding: public ${asset} enter the vault and produce private note commitments"><div class="retail-token-source"><div class="retail-coins"><i>${symbol}</i><i>${symbol}</i><i>${symbol}</i></div><strong>Public ${asset}</strong><small>Approve vault spending</small></div><div class="retail-flow-track"><i></i><i></i><i></i><span>${deposit}</span></div><div class="retail-vault"><span>▥</span><strong>${vault}</strong><small>Assets held in custody</small></div><div class="retail-flow-track"><i></i><i></i><i></i><span>Poseidon</span></div><div class="retail-output-stack">${shieldNotePreview(amounts, unit)}</div></div>`;
}

function shieldStatus(value, publicBalance, unit, locked) {
  if (locked) return "This funding step is complete. The selected notes are now used by the next step.";
  if (!publicBalance) return "Mint a public balance in the previous step to begin shielding.";
  if (!validAmount(value)) return "Enter a positive whole amount.";
  if (Number(value) > publicBalance) return "The amount exceeds the available public balance.";
  return `${fmt(Number(value))} ${unit} becomes one private note. ${fmt(publicBalance - Number(value))} ${unit} stays public.`;
}

function noteDetails(p, ownerPartyId, assetId, notes) {
  const latest = notes.at(-1), owner = p.registrations.find(r => r.partyId === ownerPartyId);
  return `<details class="retail-math" data-disclosure="shield-calculation"><summary>How the commitment is calculated${latest ? " · latest note" : ""}</summary><p>One shielding operation → one private note → one leaf. Each note uses a fresh salt and remains independently spendable.</p><code>C = Poseidon(pk_spend, salt, amount, token_id)</code><dl><dt>Spend public key</dt><dd>${owner?.spendPublicKey || "—"}</dd><dt>Fresh salt</dt><dd>${latest?.salt || "Generated when you shield"}</dd><dt>Amount</dt><dd>${latest ? fmt(latest.amount) : "Chosen in the form"}</dd><dt>Asset</dt><dd>${assetId}</dd><dt>Commitment</dt><dd>${latest?.commitment || "Added to the tree after shielding"}</dd></dl></details>`;
}

function noteInventory(notes, unit) {
  return `<section class="note-construction-card unified-note-inventory"><div class="retail-section-heading"><h3>Private notes</h3><span>${notes.length} ${notes.length === 1 ? "note" : "notes"} · select one to inspect</span></div><div class="retail-note-inventory">${notes.map(note => `<details class="owned-note note-inventory-item" data-note-id="${note.id}" data-disclosure="note-${note.id}"><summary><div class="retail-note-card ${note.status === "spent" ? "spent" : ""}"><header><span>${note.origin === "auction_change" ? "Bid change" : "Shielded note"}</span><b>Leaf ${note.leafIndex}</b></header><strong>${fmt(note.amount)}<small> ${unit}</small></strong><footer>${note.status === "unspent" ? "Available to spend" : note.status}</footer></div></summary><dl><dt>Salt</dt><dd>${note.salt}</dd><dt>Commitment</dt><dd>${note.commitment}</dd><dt>Asset</dt><dd>${note.assetId}</dd></dl></details>`).join("") || '<p class="note-inventory-empty">Your first shielded note will appear here.</p>'}</div></section>`;
}

export function singleNoteShieldCard(p, config) {
  const { title, role, ownerPartyId, assetId, unit, assetLabel, publicBalance, privateBalance, inputId, defaultAmount, action, locked = false, origins = ["shielding"], traffic = false, vault, description } = config;
  const amount = inputs.get(inputId) ?? String(defaultAmount);
  const notes = p.notes.filter(note => note.ownerPartyId === ownerPartyId && note.assetId === assetId && origins.includes(note.origin));
  const valid = !locked && validAmount(amount) && Number(amount) <= publicBalance;
  const context = `${p.id}:${assetId}`;
  return `<article class="panel flow-card retail-workspace unified-shield-workspace" data-shield-input="${inputId}" data-shield-balance="${publicBalance}" data-shield-unit="${unit}" data-shield-locked="${locked}"><div class="panel-heading"><div><p class="eyebrow">${role}</p><h2>${title}</h2></div><span class="context-badge">${assetId}</span></div><p>${description}</p>${walletSummary({ asset: assetLabel, publicBalance, privateBalance, noteCount: notes.filter(n => n.status === "unspent").length, leafCount: p.trees?.[assetId]?.leafIds.length || 0 })}<div class="retail-tree-jump"><button type="button" class="button button-ghost" data-note-tree-show>Explore live commitment tree ↓</button></div><div class="shield-workbench"><section class="shield-controls"><h3>Choose your private note</h3><p>Each submission creates a separate note.</p><div class="retail-form"><label>Amount to shield · ${unit}<input id="${inputId}" data-shield-amount type="text" inputmode="numeric" pattern="[0-9]*" value="${escape(amount)}" ${locked || !publicBalance ? "disabled" : ""}></label></div><p class="shield-status" data-shield-status aria-live="polite">${shieldStatus(amount, publicBalance, unit, locked)}</p><button class="button button-primary retail-shield-submit" data-action="${action}" ${valid ? "" : "disabled"}>Shield into a private note</button></section><section class="shield-visual">${shieldingScene({ amounts: [amount], unit, asset: assetLabel, symbol: unit === "USD" ? "$" : "▤", vault, deposit: "Deposit" })}${noteDetails(p, ownerPartyId, assetId, notes)}</section></div><section class="shielding-network" data-shield-context="${context}">${commitmentTreeExplorer(p, { assetId, viewer: ownerPartyId, showTraffic: traffic, state: noteTreeState(p, assetId), vault })}</section>${noteInventory(notes, unit)}</article>`;
}

export function shieldAmountInput(target) {
  if (!target.matches("[data-shield-amount]")) return;
  inputs.set(target.id, target.value);
  const workspace = target.closest(".unified-shield-workspace"), balance = Number(workspace.dataset.shieldBalance), unit = workspace.dataset.shieldUnit, locked = workspace.dataset.shieldLocked === "true";
  workspace.querySelector(".retail-output-stack").innerHTML = shieldNotePreview([target.value], unit);
  workspace.querySelector("[data-shield-status]").textContent = shieldStatus(target.value, balance, unit, locked);
  workspace.querySelector("[data-action]").disabled = locked || !validAmount(target.value) || Number(target.value) > balance;
}

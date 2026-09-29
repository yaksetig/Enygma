import { ORDER, G, H, scalar, poseidon, pedersen, pointAdd, pointMultiply, encodePoint, samePoint, spendScalar, spendPublic } from "./institutional-crypto.js";

const point = (v, r) => encodePoint(pedersen(v, r));
const pairSecret = (a, b) => scalar(poseidon([Math.min(a, b), Math.max(a, b), 768]));
const fingerprint = (a, b) => a === b ? "0" : String(scalar(poseidon([pairSecret(a, b)])));
const clone = value => JSON.parse(JSON.stringify(value));

// A perspective projection for the browser walkthrough. Use the same scalar
// as payment construction, including for channels restored from older sessions.
export function institutionalChannelView(p, viewer = "public") {
  if (!p.flow.channels) return [];
  return (p.flow.channelPairs || []).map(pair => {
    const leftIndex = p.registrations.findIndex(r => r.partyId === pair.leftPartyId);
    const rightIndex = p.registrations.findIndex(r => r.partyId === pair.rightPartyId);
    const left = p.registrations[leftIndex], right = p.registrations[rightIndex];
    const canOpen = Boolean(left && right && (viewer === left.partyId || viewer === right.partyId || (viewer === "auditor" && left.auditEnvelope && right.auditEnvelope)));
    return { ...pair, leftName: left?.name, rightName: right?.name, canOpen, sharedKey: canOpen ? `0x${pairSecret(leftIndex + 1, rightIndex + 1).toString(16).padStart(64, "0")}` : null };
  });
}

// Opening a payment delta does not grant the opening of an account's balance.
export function institutionalPaymentView(batch, viewer = "public") {
  const sender = viewer === String(batch.payerId), auditor = viewer === "auditor";
  const member = batch.rows.some(row => viewer === String(row.accountId));
  return {
    scope: auditor ? "auditor" : sender ? "sender" : member ? "participant" : viewer === "public" ? "public" : "outside",
    rows: batch.rows.map(row => {
      const own = viewer === String(row.accountId);
      return {
        accountId: row.accountId, name: row.name, delta: [...row.delta], after: [...row.after], own,
        opening: sender || auditor || own ? { value: row.value, randomness: row.r } : null,
        balanceOpening: auditor || own ? { before: row.previousBalance, after: row.balance } : null
      };
    })
  };
}

export function institutionalState(p) {
  if (!p.flow.institutional) p.flow.institutional = { accounts: [], funded: false, paused: false, frozen: [], draft: null, epoch: 9000, mintCount: 0 };
  const state = p.flow.institutional;
  if (p.registrations.length === 10 && !state.accounts.length) {
    state.accounts = p.registrations.map((reg, i) => ({ accountId: i + 1, partyId: reg.partyId, name: reg.name, balance: 0, randomness: "0", commitment: point(0, 0) }));
  }
  state.totalSupplyAmount ??= state.accounts.reduce((sum, a) => sum + a.balance, 0);
  state.totalSupplyCommitment ??= encodePoint(state.accounts.reduce((sum, a) => pointAdd(sum, a.commitment), [0n, 1n]));
  return state;
}

// Update saved funding-only walkthroughs without rewriting settled private payments.
export function migrateInstitutionalFunding(p) {
  const s = institutionalState(p);
  if (p.transactions.some(tx => tx.batch) || s.accounts.every(a => a.randomness === "0")) return;
  const balances = new Map(s.accounts.map(a => [a.accountId, a.balance]));
  for (const tx of p.transactions) {
    if (!tx.mint) continue;
    const mint = tx.mint, after = balances.get(mint.recipientId), before = after - mint.amount;
    Object.assign(mint, { before: point(before, 0), commitment: point(mint.amount, 0), after: point(after, 0), blinding: "none" });
    balances.set(mint.recipientId, before);
  }
  for (const a of s.accounts) Object.assign(a, { randomness: "0", commitment: point(a.balance, 0) });
  s.totalSupplyCommitment = point(s.totalSupplyAmount, 0);
  s.draft = null;
  s.epoch++;
}

export function mintInstitutional(p, input) {
  const s = institutionalState(p);
  if (input.actor !== "owner") throw new Error("Only the contract owner can mint funds.");
  if (s.paused) throw new Error("The contract is paused. Only the owner can resume it.");
  const recipientId = Number(input.recipientId), amount = Number(input.amount);
  const recipient = s.accounts.find(a => a.accountId === recipientId);
  if (!Number.isSafeInteger(recipientId) || !recipient) throw new Error("Choose a registered recipient.");
  if (!Number.isSafeInteger(amount) || amount < 1 || amount > 1_000_000_000) throw new Error("Choose a whole-token funding amount from 1 to 1,000,000,000.");
  if (!Number.isSafeInteger(s.totalSupplyAmount + amount) || !Number.isSafeInteger(recipient.balance + amount)) throw new Error("This mint would exceed the supported balance range.");
  const before = [...recipient.commitment];
  const commitment = point(amount, 0);
  const nextBalance = encodePoint(pointAdd(recipient.commitment, commitment));
  const nextSupply = encodePoint(pointAdd(s.totalSupplyCommitment, commitment));
  recipient.balance += amount;
  recipient.commitment = nextBalance;
  s.totalSupplyAmount += amount;
  s.totalSupplyCommitment = nextSupply;
  s.mintCount++;
  s.epoch++;
  s.funded = true;
  s.draft = null;
  return { recipientId, recipientName: recipient.name, amount, commitment, before, after: nextBalance, blinding: "none" };
}

function allowed(s, accountIds) {
  if (s.paused) throw new Error("The contract is paused. Only the owner can resume it.");
  if (accountIds.some(id => s.frozen.includes(id))) throw new Error("A selected user is frozen from trading. Unfreeze the user or choose an eligible set.");
}

export function buildInstitutionalPayment(p, identities, input) {
  const s = institutionalState(p), k = Number(input.k), payerId = Number(input.payerId);
  const ids = [...(input.accountIds || [])].map(Number).sort((a, b) => a - b);
  if (!p.flow.channels) throw new Error("Establish the pairwise channels first.");
  if (![2, 6].includes(k) || ids.length !== k || new Set(ids).size !== k || ids.some(id => !s.accounts.some(a => a.accountId === id))) throw new Error("Select exactly k distinct registered users.");
  // Accept the previous single-recipient shape when restoring saved drafts.
  const requested = input.recipients === undefined ? [{ accountId: input.recipientId, amount: input.amount }] : input.recipients;
  if (!Array.isArray(requested) || !requested.length || requested.length > k - 1) throw new Error(`Choose between 1 and ${k - 1} recipients for this batch.`);
  const recipients = requested.map(recipient => ({ accountId: Number(recipient?.accountId), amount: Number(recipient?.amount) }));
  if (!ids.includes(payerId) || recipients.some(recipient => recipient.accountId === payerId || !ids.includes(recipient.accountId)) || new Set(recipients.map(recipient => recipient.accountId)).size !== recipients.length) throw new Error("Choose distinct recipients within the set, separate from the payer.");
  if (recipients.some(recipient => !Number.isSafeInteger(recipient.amount) || recipient.amount <= 0)) throw new Error("Each recipient must receive a positive whole-token amount.");
  const amount = recipients.reduce((sum, recipient) => sum + recipient.amount, 0);
  allowed(s, ids);
  const payer = s.accounts.find(a => a.accountId === payerId);
  if (!Number.isSafeInteger(amount) || amount > payer.balance) throw new Error("The combined payment must be within the payer’s balance.");
  const secretKey = spendScalar(identities.find(i => i.id === payer.partyId).spendPrivateKey);
  const senderSecret = scalar(poseidon([payer.randomness, secretKey]));
  const epochHash = poseidon([s.epoch]);
  const nullifier = poseidon([senderSecret, epochHash]);
  const hashRandom = poseidon([21]), hashTag = poseidon([12]);
  const slots = ids.map(id => {
    const secret = id === payerId ? senderSecret : pairSecret(payerId, id);
    const nonce = poseidon([nullifier, poseidon([payerId, id])]);
    return { id, nonce, secret, r: scalar(poseidon([hashRandom, secret, nonce])), tag: String(scalar(poseidon([hashTag, secret, nonce]))) };
  });
  const senderR = scalar(slots.filter(slot => slot.id !== payerId).reduce((sum, slot) => sum + slot.r, 0n));
  const rows = slots.map(slot => {
    const account = s.accounts.find(a => a.accountId === slot.id);
    const value = slot.id === payerId ? -amount : recipients.find(recipient => recipient.accountId === slot.id)?.amount || 0;
    const r = slot.id === payerId ? senderR : scalar(-slot.r);
    const delta = point(value, r), after = encodePoint(pointAdd(account.commitment, delta));
    return { accountId: slot.id, name: account.name, value, r: String(r), tag: slot.tag, nonce: String(slot.nonce), valuePoint: encodePoint(pointMultiply(G, value)), randomPoint: encodePoint(pointMultiply(H, r)), delta, before: [...account.commitment], after, previousBalance: account.balance, previousRandomness: account.randomness, balance: account.balance + value, randomness: String(scalar(BigInt(account.randomness) + r)) };
  });
  const domainId = (31337n << 160n) | BigInt(p.contracts.find(c => c.name === "Enygma").address);
  const publicSignals = [
    ...ids.flatMap(a => ids.map(b => fingerprint(a, b))),
    ...ids.map(id => String(spendPublic(identities.find(i => i.id === s.accounts[id - 1].partyId).spendPrivateKey))),
    ...rows.flatMap(row => row.before), ...rows.flatMap(row => row.delta),
    String(epochHash), ...ids.map(String), ...rows.map(row => row.tag), String(nullifier), String(domainId)
  ];
  return { status: "calculated", k, payerId, recipients, amount, accountIds: ids, rows, epoch: s.epoch, nullifier: String(nullifier), publicSignals, proof: null, checks: [], privateSenderSecret: String(senderSecret) };
}

export function validateInstitutionalPayment(p, draft) {
  const s = institutionalState(p);
  if (!draft) throw new Error("Calculate the commitment batch first.");
  allowed(s, draft.accountIds);
  if (draft.epoch !== s.epoch || draft.rows.some(row => !samePoint(row.before, s.accounts[row.accountId - 1].commitment))) throw new Error("Account balances changed. Calculate a fresh batch before proving or posting.");
  if (draft.rows.reduce((sum, row) => sum + row.value, 0) !== 0 || draft.rows.some(row => row.balance < 0)) throw new Error("The payment must conserve value and keep balances nonnegative.");
  const debits = draft.rows.filter(row => row.value < 0);
  if (debits.length !== 1 || debits[0].accountId !== draft.payerId) throw new Error("Exactly one account, the payer, must be debited.");
  for (const row of draft.rows) {
    if (!samePoint(row.delta, pedersen(row.value, row.r)) || !samePoint(row.after, pointAdd(row.before, row.delta)) || !samePoint(row.after, pedersen(row.balance, row.randomness))) throw new Error("Commitment opening does not match the payment.");
  }
  const sum = draft.rows.reduce((acc, row) => pointAdd(acc, row.delta), [0n, 1n]);
  if (!samePoint(sum, [0n, 1n])) throw new Error("Commitment deltas must sum to the identity (0, 1).");
  return ["Payer knows its spend key and previous balance opening", "Payment is within the payer’s balance", "Every delta matches Δvᵢ·G + rᵢ·H", "Σ Δvᵢ = 0 and Σ ΔCᵢ = (0, 1)", "Pairwise fingerprints, tags and blinding factors are bound", "Nullifier and chain/contract domain are bound"];
}

export function institutionalBinding(draft) {
  return JSON.stringify([draft.accountIds, draft.publicSignals, draft.rows, draft.nullifier]);
}

export function settleInstitutionalPayment(p) {
  const s = institutionalState(p), draft = s.draft;
  validateInstitutionalPayment(p, draft);
  if (!draft.proof || draft.proof.binding !== institutionalBinding(draft)) throw new Error("Generate a valid proof for this exact commitment batch first.");
  if (p.transactions.some(tx => tx.batch?.nullifier === draft.nullifier)) throw new Error("This nullifier has already been consumed.");
  for (const row of draft.rows) Object.assign(s.accounts[row.accountId - 1], { balance: row.balance, randomness: row.randomness, commitment: [...row.after] });
  draft.status = "confirmed";
  s.epoch++;
  return clone(draft);
}

export { G, H, ORDER };

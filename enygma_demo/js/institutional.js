import { ORDER, G, H, scalar, poseidon, pedersen, pointAdd, pointMultiply, encodePoint, samePoint, spendScalar, spendPublic } from "./institutional-crypto.js";

const point = (v, r) => encodePoint(pedersen(v, r));
const pairSecret = (a, b) => scalar(poseidon([Math.min(a, b), Math.max(a, b), 768]));
const fingerprint = (a, b) => a === b ? "0" : String(scalar(poseidon([pairSecret(a, b)])));
const clone = value => JSON.parse(JSON.stringify(value));

export function institutionalState(p) {
  if (!p.flow.institutional) p.flow.institutional = { accounts: [], funded: false, paused: false, frozen: [], draft: null, epoch: 9000, mintCount: 0 };
  const state = p.flow.institutional;
  if (p.registrations.length === 10 && !state.accounts.length) {
    const blindings = p.registrations.map((_, i) => scalar(poseidon([31, i + 1])));
    blindings[9] = scalar(-blindings.slice(0, 9).reduce((a, b) => a + b, 0n));
    state.accounts = p.registrations.map((reg, i) => ({ accountId: i + 1, partyId: reg.partyId, name: reg.name, balance: 0, randomness: String(blindings[i]), commitment: point(0, blindings[i]) }));
  }
  return state;
}

export function mintInstitutional(p, amount) {
  const s = institutionalState(p);
  if (s.paused) throw new Error("The contract is paused. Only the owner can resume it.");
  if (!Number.isSafeInteger(amount) || amount < 1 || amount > 1_000_000_000) throw new Error("Choose a whole-token funding amount from 1 to 1,000,000,000.");
  const randoms = s.accounts.map(a => scalar(poseidon([41, a.accountId, s.mintCount + 1])));
  randoms[randoms.length - 1] = scalar(-randoms.slice(0, -1).reduce((a, b) => a + b, 0n));
  s.accounts.forEach((a, i) => {
    a.balance += amount;
    a.randomness = String(scalar(BigInt(a.randomness) + randoms[i]));
    a.commitment = point(a.balance, a.randomness);
  });
  s.mintCount++;
  s.epoch++;
  s.funded = true;
  s.draft = null;
}

function allowed(s, accountIds) {
  if (s.paused) throw new Error("The contract is paused. Only the owner can resume it.");
  if (accountIds.some(id => s.frozen.includes(id))) throw new Error("A selected user is frozen from trading. Unfreeze the user or choose an eligible set.");
}

export function buildInstitutionalPayment(p, identities, input) {
  const s = institutionalState(p), k = Number(input.k), payerId = Number(input.payerId), recipientId = Number(input.recipientId), amount = Number(input.amount);
  const ids = [...(input.accountIds || [])].map(Number).sort((a, b) => a - b);
  if (!p.flow.channels) throw new Error("Establish the pairwise channels first.");
  if (![2, 6].includes(k) || ids.length !== k || new Set(ids).size !== k || ids.some(id => !s.accounts.some(a => a.accountId === id))) throw new Error("Select exactly k distinct registered users.");
  if (payerId === recipientId || !ids.includes(payerId) || !ids.includes(recipientId)) throw new Error("Select a payer and a different recipient within the set.");
  allowed(s, ids);
  const payer = s.accounts.find(a => a.accountId === payerId);
  if (!Number.isSafeInteger(amount) || amount <= 0 || amount > payer.balance) throw new Error("The payment must be a positive whole-token amount within the payer’s balance.");
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
    const value = slot.id === payerId ? -amount : slot.id === recipientId ? amount : 0;
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
  return { status: "calculated", k, payerId, recipientId, amount, accountIds: ids, rows, epoch: s.epoch, nullifier: String(nullifier), publicSignals, proof: null, checks: [], privateSenderSecret: String(senderSecret) };
}

export function validateInstitutionalPayment(p, draft) {
  const s = institutionalState(p);
  if (!draft) throw new Error("Calculate the commitment batch first.");
  allowed(s, draft.accountIds);
  if (draft.epoch !== s.epoch || draft.rows.some(row => !samePoint(row.before, s.accounts[row.accountId - 1].commitment))) throw new Error("Account balances changed. Calculate a fresh batch before proving or posting.");
  if (draft.rows.reduce((sum, row) => sum + row.value, 0) !== 0 || draft.rows.some(row => row.balance < 0)) throw new Error("The payment must conserve value and keep balances nonnegative.");
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

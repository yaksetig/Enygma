import { FIELD, mod, poseidon } from "./institutional-crypto.js";

// Same zero leaf, depth and hash as core/Merkle.sol and the retail launcher.
export const RETAIL_DEPTH = 8;
export const RETAIL_ZERO = 21786163348386038604960770631540184405176086071424084281943739401806009032442n;
export const fieldValue = value => mod(BigInt(String(value).replace(/^(spend_pk_|spend_sk_|salt_|tag_)/, "0x")));
export const fieldHex = value => `0x${mod(value).toString(16).padStart(64, "0")}`;
export const noteCommitment = (key, salt, amount) => fieldHex(poseidon([fieldValue(key), fieldValue(salt), amount, 0]));
export function randomField() {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return fieldHex(BigInt(`0x${Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("")}`) % FIELD);
}
const zeroLevels = [RETAIL_ZERO];
for (let i = 0; i < RETAIL_DEPTH; i++) zeroLevels.push(poseidon([zeroLevels[i], zeroLevels[i]]));

export function retailTree(p) {
  const leaves = p.leaves.filter(leaf => leaf.assetId === "USD");
  const levels = [leaves.map(leaf => leaf.commitment)];
  for (let level = 0; level < RETAIL_DEPTH; level++) {
    const current = levels[level], next = [];
    for (let i = 0; i < Math.max(1, current.length); i += 2) next.push(fieldHex(poseidon([current[i] || zeroLevels[level], current[i + 1] || zeroLevels[level]])));
    levels.push(next);
  }
  p.trees = { USD: { assetId: "USD", depth: RETAIL_DEPTH, leafIds: leaves.map(leaf => leaf.id), levels, root: levels[RETAIL_DEPTH][0], zeros: zeroLevels.map(fieldHex) } };
  return p.trees.USD;
}

export function retailState(p) {
  const s = p.flow.retail ||= { publicBalance: 0, channels: [], draft: null, activity: null };
  s.channels ||= [];
  if (!s.channels.length && p.flow.retailTagChannel) s.channels.push({ ...p.flow.retailTagChannel, index: 0 });
  p.notes ||= [];
  return s;
}

export function migrateRetail(p) {
  const s = retailState(p);
  s.activity = null;
  s.draft = null;
  for (const ch of s.channels) ch.sharedSecret ||= randomField();
  for (const leaf of p.leaves) leaf.assetId = "USD";
  retailTree(p);
}

function amountValue(value, name = "Amount") {
  const number = Number(value);
  if (!Number.isSafeInteger(number) || number <= 0) throw new Error(`${name} must be a positive whole number.`);
  return number;
}
function capacity(p, count) {
  if (p.leaves.length + count > 2 ** RETAIL_DEPTH) throw new Error("This tree is full. Reset the Retail walkthrough to start a new tree.");
}
export const availableRetailNotes = (p, owner = "party-0") => (p.notes || []).filter(n => n.ownerPartyId === owner && n.status === "unspent" && n.discovered !== false && n.amount > 0);

function makePayload(owner, amount, token) {
  const salt = randomField();
  return { scope: "private_note", leg: "note", ownerPartyId: owner.partyId, tokenId: "USD", tokenField: "0", spendPublicKey: owner.spendPublicKey, amount, salt, commitment: noteCommitment(owner.spendPublicKey, salt, amount), ciphertext: token("enc_note_", salt, 64), symmetricKey: token("tx_key_", `${salt}:key`, 64), capsule: token("mlkem_ct_", `${salt}:capsule`, 64), commitmentAlgorithm: "Poseidon(pk_spend, salt, amount, token_id)", keyDerivation: "HKDF-SHA256", encryption: "AES-256-GCM", encryptedFields: ["token_id", "amount"] };
}

function appendNotes(engine, p, type, label, payloads, options = {}) {
  const tx = engine.addTransaction("retail", type, label, { ...options, leaves: payloads.length, assetId: "USD", leafCommitments: payloads.map(item => item.commitment) });
  tx.encryptedPayload = payloads[0];
  tx.outputs = payloads;
  payloads.forEach((payload, index) => {
    const leaf = p.leaves.find(l => l.sourceTxId === tx.id && l.commitment === payload.commitment);
    const owner = p.registrations.find(r => r.partyId === payload.ownerPartyId);
    const note = { id: `${tx.id}:note:${index}`, ownerPartyId: owner.partyId, ownerName: owner.name, assetId: "USD", amount: payload.amount, salt: payload.salt, spendPublicKey: owner.spendPublicKey, commitment: payload.commitment, leafId: leaf.id, leafIndex: p.leaves.indexOf(leaf), sourceTxId: tx.id, origin: type === "shield" || type === "background-shield" ? "shielding" : index === 0 ? "payment" : "change", status: "unspent", discovered: options.background || type === "shield" || index === 1 };
    p.notes.push(note);
    payload.noteId = note.id;
  });
  return tx;
}

async function phase(engine, s, kind, step, extra = {}) {
  s.activity = { kind, step, ...extra };
  engine.persist();
  await engine.pause(620);
}

export async function executeRetail(engine, action, payload, token, candidates) {
  const p = engine.protocol("retail"), s = retailState(p);
  if (s.activity) throw new Error("Wait for the current operation to finish.");
  try {
    if (action === "mint-cash") {
      const amount = amountValue(payload.amount);
      if (!Number.isSafeInteger(s.publicBalance + amount)) throw new Error("Public balance is too large.");
      s.publicBalance += amount;
      return engine.addTransaction("retail", action, `RaylsERC20.mint() allocated ${amount} USD to You`, { from: -1, to: 0 });
    }
    if (action === "shield") {
      const amount = amountValue(payload.amount), count = amountValue(payload.count || 1, "Note count");
      if (count > 4) throw new Error("Shield up to four notes at a time.");
      if (amount * count > s.publicBalance) throw new Error("The notes exceed your public USD balance. Allocate funds first.");
      capacity(p, count);
      let tx;
      for (let i = 0; i < count; i++) {
        const output = makePayload(p.registrations[0], amount, token);
        await phase(engine, s, "shield", "derive", { index: i + 1, count, output });
        await phase(engine, s, "shield", "commit", { index: i + 1, count, output });
        s.publicBalance -= amount;
        tx = appendNotes(engine, p, "shield", `Erc20CoinVault.depositV2() · ${amount} USD · leaf ${p.leaves.length}`, [output], { from: 0, to: 0 });
        tx.calls = ["RaylsERC20.approve(vault, amount)", "Erc20CoinVault.depositV2([amount, pk_spend, salt, tokenId], capsule, ciphertext)"];
        await phase(engine, s, "shield", "insert", { index: i + 1, count, output });
      }
      return tx;
    }
    if (action === "configure-tags") {
      const recipientIndex = Number(payload.recipientIndex), recipient = p.registrations[recipientIndex];
      if (!recipient || recipientIndex === 0) throw new Error("Select a registered recipient other than the payer.");
      const mode = ["none", "subset", "rift", "full"].includes(payload.mode) ? payload.mode : "full";
      const excludedIndices = mode === "rift" ? [...new Set((payload.excludedIndices || []).map(Number).filter(i => Number.isInteger(i) && i >= 0 && i < p.registrations.length && i !== recipientIndex))] : [];
      if (mode === "rift" && excludedIndices.length >= p.registrations.length - 1) throw new Error("Rift must leave more than the recipient. Choose No privacy for a single candidate.");
      const candidateIndices = candidates(mode, p.registrations.length, recipientIndex, excludedIndices);
      const sharedSecret = randomField();
      const ch = { id: token("tag_channel_", `${sharedSecret}:${recipientIndex}`, 22), index: s.channels.length, senderPartyId: "party-0", recipientPartyId: recipient.partyId, mode, excludedIndices, candidateIndices, bitmap: p.registrations.map((_, i) => candidateIndices.includes(i) ? "1" : "0").join(""), sharedSecret, c1: token("mlkem_ct_", sharedSecret, 64), c2: token("channel_ct_", `${sharedSecret}:data`, 64) };
      await phase(engine, s, "channel", "encapsulate", { channel: ch });
      await phase(engine, s, "channel", "encrypt", { channel: ch });
      await phase(engine, s, "channel", "relay", { channel: ch });
      const tx = engine.addTransaction("retail", action, `TagChannelRegistry.openChannel() · channel ${ch.index} · ${candidateIndices.length} candidates`, { from: 0, to: recipientIndex });
      ch.sourceTxId = tx.id;
      s.channels.push(ch);
      p.flow.retailTagChannel = ch;
      p.flow.retailRecipient = { partyId: recipient.partyId, name: recipient.name, spendPublicKey: recipient.spendPublicKey, viewPublicKey: recipient.viewPublicKey };
      await phase(engine, s, "channel", "published", { channel: ch });
      return tx;
    }
    if (action === "payment") {
      const channel = s.channels.find(ch => ch.id === payload.channelId) || (!payload.channelId ? s.channels.at(-1) : null);
      if (!channel) throw new Error("Establish a private-tag channel and select the recipient before creating a payment.");
      const amount = amountValue(payload.amount, "Payment amount");
      const notes = availableRetailNotes(p);
      const input = payload.noteId ? notes.find(n => n.id === payload.noteId) : [...notes].sort((a, b) => a.amount - b.amount).find(n => n.amount >= amount);
      if (!input || input.amount < amount) throw new Error("Select one unspent note large enough for this payment.");
      capacity(p, 2);
      const recipient = p.registrations.find(r => r.partyId === channel.recipientPartyId);
      const outputs = [makePayload(recipient, amount, token), makePayload(p.registrations[0], input.amount - amount, token)];
      const root = p.trees.USD.root;
      const sk = fieldValue(engine.state.identities[0].spendPrivateKey);
      const nullifier = fieldHex(poseidon([sk, input.leafIndex]));
      const siblings = Array.from({ length: RETAIL_DEPTH }, (_, level) => p.trees.USD.levels[level][(input.leafIndex >> level) ^ 1] || p.trees.USD.zeros[level]);
      s.draft = { inputNoteId: input.id, inputCommitment: input.commitment, channelId: channel.id, amount, change: input.amount - amount, root, siblings, nullifier, outputs, phase: "membership" };
      for (const step of ["membership", "outputs", "prove", "relay", "verify"]) {
        s.draft.phase = step;
        await phase(engine, s, "payment", step);
      }
      if (input.status !== "unspent" || p.transactions.some(tx => tx.nullifier === nullifier)) throw new Error("This note has already been spent.");
      const tx = appendNotes(engine, p, "payment", `EnygmaDvp.payment() · two commitments appended`, outputs, { from: 0, to: p.registrations.indexOf(recipient) });
      Object.assign(tx, { inputNoteId: input.id, root, siblings, nullifier, tagChannelId: channel.id, change: input.amount - amount, verified: true });
      input.status = "spent";
      input.spentBy = tx.id;
      s.draft.txId = tx.id;
      // Payment receipt precedes the separate tag publication, as in session.go.
      const block = p.ledger[0].block + 1;
      tx.tagWindow = [0, 1, 2].map(offset => ({ block: block + offset, tag: fieldHex(poseidon([block + offset, fieldValue(recipient.spendPublicKey), fieldValue(channel.sharedSecret)])) }));
      tx.privateTag = tx.tagWindow[0].tag;
      tx.tagBlock = block;
      tx.channelCiphertext = token("channel_note_", `${tx.id}:${channel.id}`, 64);
      tx.channelEncryptedFields = ["amount", "token_id", "salt"];
      engine.receipt("retail", "publish-tag", "TagRegistry.publishTag() · landing-block tag + encrypted note");
      await phase(engine, s, "payment", "append");
      await phase(engine, s, "payment", "tag");
      s.draft = null;
      return tx;
    }
    if (action === "scan") {
      const tx = p.transactions.find(t => t.id === payload.txId && t.type === "payment") || (!payload.txId ? p.transactions.find(t => t.type === "payment") : null);
      if (!tx?.privateTag) throw new Error("Publish a payment and its tag before scanning.");
      if (tx.scanned) throw new Error("This note has already been recovered.");
      for (const step of ["channel", "match", "decrypt", "recover"]) await phase(engine, s, "scan", step, { txId: tx.id });
      const note = p.notes.find(n => n.id === tx.outputs?.[0]?.noteId);
      if (note) note.discovered = true;
      tx.scanned = true;
      return engine.addTransaction("retail", "scan", `${p.registrations.find(r => r.partyId === tx.to)?.name} recovered the matching note`, { from: Number(tx.to.split("-")[1]), to: Number(tx.to.split("-")[1]) });
    }
    throw new Error(`Unsupported Retail action: ${action}`);
  } finally { s.activity = null; engine.persist(); }
}

export function retailTraffic(engine, token) {
  const p = engine.protocol("retail"), s = retailState(p);
  if (s.activity) return;
  if (p.leaves.length > 253) { engine.setTraffic("retail", false); return; }
  const count = p.transactions.filter(t => t.background).length;
  const owner = p.registrations[1 + (count % 9)], recipient = p.registrations[1 + ((count + 2) % 9)];
  const input = availableRetailNotes(p, owner.partyId)[0];
  if (!input) {
    const tx = appendNotes(engine, p, "background-shield", "Background wallet · depositV2() · one new commitment", [makePayload(owner, 100, token)], { from: p.registrations.indexOf(owner), to: p.registrations.indexOf(owner), background: true });
    tx.calls = ["RaylsERC20.mint()", "RaylsERC20.approve()", "Erc20CoinVault.depositV2()"];
  } else {
    const amount = Math.min(input.amount, 10);
    const tx = appendNotes(engine, p, "background-payment", "Background wallet · payment() · two new commitments", [makePayload(recipient, amount, token), makePayload(owner, input.amount - amount, token)], { from: p.registrations.indexOf(owner), to: p.registrations.indexOf(recipient), background: true });
    input.status = "spent";
    input.spentBy = tx.id;
    tx.inputNoteId = input.id;
    tx.nullifier = fieldHex(poseidon([fieldValue(engine.state.identities[p.registrations.indexOf(owner)].spendPrivateKey), input.leafIndex]));
    tx.verified = true;
  }
}

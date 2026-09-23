import { PARTY_NAMES, PROTOCOL_IDS, PROTOCOLS, PROTOCOL_PRIMITIVES, spendPublicKeyFor, initializationSteps } from "./config.js";
import { spendPublic } from "./institutional-crypto.js";
import { institutionalState, mintInstitutional, buildInstitutionalPayment, validateInstitutionalPayment, institutionalBinding, settleInstitutionalPayment } from "./institutional.js";

const STORAGE_KEY = "enygma-demo-state-v12";
const VERSION = 12;

function seedNumber(value) {
  let hash = 2166136261;
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function deriveHex(label, length = 64) {
  let state = seedNumber(label) || 1;
  let output = "";
  while (output.length < length) {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    output += state.toString(16).padStart(8, "0");
  }
  return output.slice(0, length);
}

function token(prefix, label, length = 64) {
  return `${prefix}${deriveHex(label, length)}`;
}

function spendPublicKeys(secret) {
  return {
    spendPublicKey: token("spend_pk_", `Poseidon(${secret})`),
    institutionalSpendPublicKey: `spend_pk_${spendPublic(secret).toString(16).padStart(64, "0")}`
  };
}

function deploymentRecord(protocol, spec, address) {
  const resolve = value => protocol.contracts.find(item => (item.instance || item.name) === value)?.address ?? value;
  return {
    id: spec.instance || spec.name,
    name: spec.name,
    instance: spec.instance,
    address: address || token("0x", `${protocol.id}:contract:${spec.instance || spec.name}`, 40),
    constructorArgs: (spec.constructorArgs || []).map(resolve),
    libraries: Object.fromEntries((spec.libraries || []).map(name => [name, resolve(name)]))
  };
}

function pairwiseChannels(id, registrations) {
  const pairs = [];
  for (let row = 1; row < registrations.length; row += 1) {
    for (let column = 0; column < row; column += 1) {
      const left = registrations[column];
      const right = registrations[row];
      pairs.push({
        id: token("channel_", `${id}:${left.partyId}:${right.partyId}`, 18),
        leftPartyId: left.partyId,
        rightPartyId: right.partyId
      });
    }
  }
  return pairs;
}

function defaultDvpTerms() {
  return {
    status: "draft",
    sellerPartyId: "party-2",
    buyerPartyId: "party-1",
    securityId: "RAYLS-BOND-2030",
    quantity: 100,
    cashToken: "USD",
    cashAmount: 985000,
    expiryBlocks: 30,
    securityInputNoteId: null,
    cashInputNoteId: null
  };
}

function defaultAuction() {
  return {
    reference: "auction_class_a_01",
    title: "Class A Share Auction",
    assetType: "Class A Share",
    assetTokenId: "CLASS-A-0042",
    cashToken: "USD",
    sellerPartyId: "party-2",
    bidderPartyId: "party-0",
    auctioneerKey: null,
    auctioneerRegistered: false,
    nftMinted: false,
    sellerPublicNft: 0,
    assetQuantity: 0,
    nftNoteId: null,
    listed: false,
    biddingDuration: 12,
    settlementDuration: 8,
    blocksRemaining: 12,
    publicCash: 0,
    privateCash: 0,
    bids: [],
    otherBidsCollected: false,
    status: "draft",
    winnerProof: null,
    challengeOpen: false,
    settlement: null
  };
}

function instantiateDvpTransfer(flow, id) {
  flow.dvpTransferCount += 1;
  flow.securityLocked = false;
  flow.cashLocked = false;
  flow.locked = false;
  flow.dvpTransfer = {
    id: token("dvp_", `${id}:transfer:${flow.dvpTransferCount}`, 22),
    termsCommitment: token("terms_", `${id}:terms:${flow.dvpTransferCount}:${JSON.stringify(flow.dvpTerms)}`, 28),
    status: "open"
  };
  flow.settlement = "idle";
  return flow.dvpTransfer;
}

function positiveAmount(value, label) {
  const amount = Math.round(Number(value));
  if (!Number.isFinite(amount) || amount <= 0) throw new Error(`${label} must be greater than zero.`);
  return Math.min(amount, 1_000_000_000_000);
}

function retailTagCandidateIndices(mode, totalUsers, recipientIndex, excludedIndices = []) {
  if (mode === "none") return [recipientIndex];
  if (mode === "subset") {
    const candidates = [recipientIndex];
    const decoys = Math.max(1, Math.floor(Math.sqrt(totalUsers)));
    for (let offset = 1; candidates.length < decoys + 1; offset += 1) {
      const index = (recipientIndex + offset * 2) % totalUsers;
      if (!candidates.includes(index)) candidates.push(index);
    }
    return candidates.sort((a, b) => a - b);
  }
  if (mode === "rift") {
    const excluded = new Set(excludedIndices.filter(index => index !== recipientIndex));
    return Array.from({ length: totalUsers }, (_, index) => index).filter(index => !excluded.has(index));
  }
  return Array.from({ length: totalUsers }, (_, index) => index);
}

function payloadPrimitives(id, scope) {
  return {
    commitmentAlgorithm: PROTOCOL_PRIMITIVES[id].commitment,
    keyDerivation: PROTOCOL_PRIMITIVES[id].derivation,
    encryption: scope === "dvp_leg" ? PROTOCOL_PRIMITIVES.dvp.swapEncryption : "AES-256-GCM",
    encryptedFields: scope === "dvp_leg" ? ["token_id", "amount", "salt_star"] : ["token_id", "amount"]
  };
}

function encryptedNotePayload(id, txId, details) {
  const salt = details.salt || token("salt_", `${id}:${txId}:${details.leg || "note"}:salt`, 40);
  const commitment = details.commitment || (id === "institutional"
    ? token("C_", `${id}:${txId}:Pedersen:${details.amount}:${salt}`)
    : token("0x", `${details.spendPublicKey || details.ownerPartyId}:${salt}:${details.amount}:${details.tokenId}`, 64));
  return {
    scope: details.scope || "private_note",
    leg: details.leg || "note",
    ownerPartyId: details.ownerPartyId,
    transferId: details.transferId || null,
    tokenId: details.tokenId,
    amount: details.amount,
    salt,
    commitment,
    ciphertext: token("enc_note_", `${id}:${txId}:salt-token-amount`, 64),
    symmetricKey: token("tx_key_", `${id}:${txId}:symmetric-key`, 64),
    ...payloadPrimitives(id, details.scope)
  };
}

function recordPrivateNote(protocol, tx, details) {
  const payload = details.payload || tx.encryptedPayload;
  const leaf = protocol.leaves.find(item => item.sourceTxId === tx.id && item.commitment === payload?.commitment);
  if (!leaf) return null;
  const tree = protocol.trees[details.assetId];
  const note = {
    id: token("note_", `${protocol.id}:${tx.id}:${details.ownerPartyId}`, 22),
    ownerPartyId: details.ownerPartyId,
    ownerName: details.ownerName,
    assetId: details.assetId,
    amount: details.amount,
    spendPublicKey: details.spendPublicKey,
    salt: payload.salt,
    commitment: payload.commitment,
    leafId: leaf.id,
    leafIndex: tree?.leafIds.indexOf(leaf.id) ?? -1,
    sourceTxId: tx.id,
    transferId: details.transferId || null,
    origin: details.origin || "shielding",
    status: "unspent"
  };
  protocol.notes.push(note);
  return note;
}

function rebuildCommitmentTrees(protocol) {
  const groups = new Map();
  for (const leaf of protocol.leaves) {
    if (!leaf.assetId) {
      const source = protocol.transactions.find(tx => tx.id === leaf.sourceTxId);
      if (source?.type === "atomic-settlement") {
        const settlementLeaves = protocol.leaves.filter(item => item.sourceTxId === leaf.sourceTxId);
        leaf.assetId = settlementLeaves.indexOf(leaf) === 0 ? protocol.flow.securityAssetId : protocol.flow.dvpTerms.cashToken;
      } else {
        leaf.assetId = source?.encryptedPayload?.tokenId || source?.assetId || "DEFAULT";
      }
    }
    if (!groups.has(leaf.assetId)) groups.set(leaf.assetId, []);
    groups.get(leaf.assetId).push(leaf);
  }
  protocol.trees = {};
  for (const [assetId, leaves] of groups) {
    const levels = [leaves.map(leaf => leaf.commitment)];
    let current = levels[0];
    let level = 0;
    while (current.length > 1) {
      const parents = [];
      for (let index = 0; index < current.length; index += 2) {
        const left = current[index];
        const right = current[index + 1] || token("empty_", `${protocol.id}:${assetId}:${level}:${index + 1}`, 18);
        parents.push(token("node_", `${protocol.id}:${assetId}:${level}:${left}:${right}`, 24));
      }
      levels.push(parents);
      current = parents;
      level += 1;
    }
    protocol.trees[assetId] = { assetId, leafIds: leaves.map(leaf => leaf.id), levels, root: current[0] };
  }
}

function migrateDvpFlow(flow) {
  if (!flow.dvpTerms) {
    const legacyStarted = Number(flow.issued || 0) > 0 || flow.locked || flow.settlement !== "idle";
    flow.dvpTerms = { ...defaultDvpTerms(), status: legacyStarted ? "agreed" : "draft" };
  }
  if (typeof flow.securityIssued !== "number") flow.securityIssued = Number(flow.issued || 0);
  if (typeof flow.cashShielded !== "number") flow.cashShielded = Number(flow.issued || 0) > 0 ? flow.dvpTerms.cashAmount : 0;
  const legacyNegotiated = flow.dvpTerms.status !== "draft";
  if (typeof flow.securityAssetId !== "string") flow.securityAssetId = "RAYLS-BOND-2030";
  if (typeof flow.sellerPublicSecurity !== "number") flow.sellerPublicSecurity = legacyNegotiated ? Math.max(0, 1000 - flow.securityIssued) : 0;
  if (typeof flow.buyerPublicCash !== "number") flow.buyerPublicCash = legacyNegotiated ? Math.max(0, 10000000 - flow.cashShielded) : 0;
  if (typeof flow.sellerShieldedSecurity !== "number") flow.sellerShieldedSecurity = flow.securityIssued;
  if (typeof flow.buyerShieldedCash !== "number") flow.buyerShieldedCash = flow.cashShielded;
  if (typeof flow.buyerPrivateSecurity !== "number") flow.buyerPrivateSecurity = flow.settlement === "settled" ? flow.dvpTerms.quantity : 0;
  if (typeof flow.sellerPrivateCash !== "number") flow.sellerPrivateCash = flow.settlement === "settled" ? flow.dvpTerms.cashAmount : 0;
  if (typeof flow.buyerPublicSecurity !== "number") flow.buyerPublicSecurity = 0;
  if (flow.dvpSchemaVersion !== 2) {
    if (flow.dvpTerms.status !== "draft" && flow.settlement !== "settled") {
      flow.sellerShieldedSecurity += flow.sellerPublicSecurity;
      flow.buyerShieldedCash += flow.buyerPublicCash;
      flow.sellerPublicSecurity = 0;
      flow.buyerPublicCash = 0;
      flow.securityIssued = flow.sellerShieldedSecurity;
      flow.cashShielded = flow.buyerShieldedCash;
    }
    flow.dvpSchemaVersion = 2;
  }
  if (typeof flow.dvpTransferCount !== "number") flow.dvpTransferCount = flow.settlement === "idle" ? 0 : 1;
  if (typeof flow.dvpTransfer === "undefined") {
    flow.dvpTransfer = flow.settlement === "idle" ? null : {
      id: token("dvp_", `dvp:transfer:${Math.max(1, flow.dvpTransferCount)}`, 22),
      termsCommitment: token("terms_", `dvp:terms:${Math.max(1, flow.dvpTransferCount)}`, 28),
      status: flow.settlement === "settled" ? "settled" : flow.settlement === "reverted" ? "reverted" : "awaiting_counterparty"
    };
  }
  if (typeof flow.securityLocked !== "boolean") flow.securityLocked = Boolean(flow.locked);
  if (typeof flow.cashLocked !== "boolean") flow.cashLocked = false;
  if (flow.settlement === "locked") flow.settlement = "awaiting_counterparty";
  return flow;
}

function blankProtocol(id) {
  return {
    id,
    stage: 0,
    contracts: [],
    deploymentActions: [],
    auditor: null,
    auditorConfigured: false,
    registrations: [],
    registrationBatchStarted: false,
    selectiveRegulator: {
      name: "Additional regulator",
      publicKey: token("mlkem_pk_", `${id}:selective-regulator`, 80)
    },
    transactions: [],
    leaves: [],
    notes: [],
    trees: {},
    ledger: [],
    disclosures: [],
    traffic: false,
    trafficAsset: null,
    flow: {
      channels: false,
      channelPairs: [],
      retailRecipient: null,
      retailTagChannel: null,
      issued: 0,
      locked: false,
      settlement: "idle",
      dvpTerms: defaultDvpTerms(),
      securityIssued: 0,
      cashShielded: 0,
      securityAssetId: "RAYLS-BOND-2030",
      sellerPublicSecurity: 0,
      buyerPublicCash: 0,
      sellerShieldedSecurity: 0,
      buyerShieldedCash: 0,
      buyerPrivateSecurity: 0,
      sellerPrivateCash: 0,
      buyerPublicSecurity: 0,
      dvpSchemaVersion: 2,
      dvpTransferCount: 0,
      dvpTransfer: null,
      securityLocked: false,
      cashLocked: false,
      auction: defaultAuction()
    }
  };
}

function freshState() {
  return {
    version: VERSION,
    identities: [],
    identityCeremony: {
      phase: "empty",
      spendPrivateKey: null,
      viewPrivateKey: null,
      spendPublicKey: null,
      viewPublicKey: null
    },
    createdAt: 0,
    protocols: Object.fromEntries(PROTOCOL_IDS.map(id => [id, blankProtocol(id)]))
  };
}

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

export class DemoEngine extends EventTarget {
  constructor() {
    super();
    this.state = this.load();
    this.timers = new Map();
  }

  load() {
    try {
      const parsed = JSON.parse(sessionStorage.getItem(STORAGE_KEY));
      if (parsed?.version === VERSION && parsed.protocols) {
        // Keep existing walkthrough progress while adding the institutional derivation.
        for (const identity of [...(parsed.identities || []), parsed.identityCeremony].filter(Boolean)) {
          if (identity.spendPublicKey) {
            identity.institutionalSpendPublicKey = spendPublicKeys(identity.spendPrivateKey).institutionalSpendPublicKey;
          }
        }
        const institutional = parsed.protocols.institutional;
        // Institutional balances are account commitments, not note-tree leaves.
        if (institutional) {
          institutional.leaves = [];
          institutional.trees = {};
          delete institutional.flow.frozen;
          delete institutional.flow.bridgeReady;
          institutional.transactions = institutional.transactions.filter(tx => !["bridge", "freeze", "resume"].includes(tx.type) && (tx.type !== "payment" || tx.batch));
          institutional.ledger = institutional.ledger.filter(entry => !["bridge", "freeze", "resume"].includes(entry.kind) && (entry.kind !== "payment" || !entry.label.includes("envelope")));
          const accountState = institutionalState(institutional);
          if (["submitted", "verifying"].includes(accountState.draft?.status)) {
            accountState.draft.status = "proved";
            accountState.draft.verification = 0;
          }
        }
        for (const registration of institutional?.registrations || []) {
          const identity = parsed.identities.find(item => item.id === registration.partyId);
          if (identity) registration.spendPublicKey = spendPublicKeyFor(identity, "institutional");
        }
        if (institutional?.flow && !Array.isArray(institutional.flow.channelPairs)) {
          institutional.flow.channelPairs = institutional.flow.channels ? pairwiseChannels("institutional", institutional.registrations) : [];
        }
        if (parsed.protocols.dvp?.flow) {
          const dvpFlow = migrateDvpFlow(parsed.protocols.dvp.flow);
          if (dvpFlow.dvpTerms.status === "agreed" && dvpFlow.dvpTransfer?.status === "open" && !dvpFlow.cashLocked && !dvpFlow.securityLocked) dvpFlow.dvpTransfer = null;
        }
        for (const protocol of Object.values(parsed.protocols)) {
          if (protocol.stage > 0) {
            const previous = protocol.contracts || [];
            protocol.contracts = [];
            for (const spec of PROTOCOLS[protocol.id].contracts) {
              const existing = previous.find(item => item.name === spec.name && (item.instance || "") === spec.instance);
              protocol.contracts.push(deploymentRecord(protocol, spec, existing?.address));
            }
            protocol.deploymentActions ||= initializationSteps(protocol.id);
          } else {
            protocol.deploymentActions ||= [];
          }
          if (protocol.auditor) protocol.auditor.algorithm = "ML-KEM-768";
          for (const tx of protocol.transactions || []) {
            tx.proofSystem = PROTOCOL_PRIMITIVES[protocol.id].proof;
            if (!tx.encryptedPayload && protocol.id === "dvp" && ["lock-security", "lock-cash"].includes(tx.type)) {
              const leg = tx.type === "lock-security" ? "security" : "cash";
              const terms = protocol.flow.dvpTerms;
              tx.encryptedPayload = encryptedNotePayload(protocol.id, tx.id, {
                scope: "dvp_leg",
                leg,
                transferId: tx.transferId || protocol.flow.dvpTransfer?.id,
                ownerPartyId: leg === "security" ? "party-1" : "party-2",
                tokenId: leg === "security" ? terms.securityId : terms.cashToken,
                amount: leg === "security" ? terms.quantity : terms.cashAmount
              });
            }
            if (tx.encryptedPayload) Object.assign(tx.encryptedPayload, payloadPrimitives(protocol.id, tx.encryptedPayload.scope));
          }
          rebuildCommitmentTrees(protocol);
        }
        const retailFlow = parsed.protocols.retail?.flow;
        if (retailFlow && typeof retailFlow.retailRecipient === "undefined") retailFlow.retailRecipient = null;
        if (retailFlow && typeof retailFlow.retailTagChannel === "undefined") retailFlow.retailTagChannel = null;
        return parsed;
      }
    } catch { /* A clean session is a valid starting point. */ }
    return freshState();
  }

  persist() {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(this.state));
    this.dispatchEvent(new CustomEvent("change", { detail: this.snapshot() }));
  }

  snapshot() { return clone(this.state); }
  protocol(id) { return this.state.protocols[id]; }
  isReady(id) {
    const registrations = this.protocol(id).registrations;
    return registrations.length === PARTY_NAMES.length && registrations.every(item => item.auditEnvelope);
  }

  async pause(ms = 280) {
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
    await new Promise(resolve => setTimeout(resolve, reduced ? 15 : ms));
  }

  receipt(id, kind, label, audit = false) {
    const protocol = this.protocol(id);
    const ordinal = (protocol.receiptCount ?? Math.max(0, ...protocol.ledger.map(item => item.block - 9000))) + 1;
    protocol.receiptCount = ordinal;
    const entry = {
      id: token("rcpt_", `${id}:${kind}:${ordinal}`, 18),
      kind,
      label,
      audit,
      block: 9000 + ordinal,
      hash: token("0x", `${id}:block:${ordinal}`, 64),
    };
    protocol.ledger.unshift(entry);
    protocol.ledger = protocol.ledger.slice(0, 30);
    return entry;
  }

  async deploy(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 0) return;
    for (const spec of PROTOCOLS[id].contracts.slice(protocol.contracts.length)) {
      await this.pause(160);
      protocol.contracts.push(deploymentRecord(protocol, spec));
      this.receipt(id, "deploy-contract", `${spec.instance || spec.name} deployed`);
      this.persist();
    }
    for (const step of initializationSteps(id).slice(protocol.deploymentActions.length)) {
      await this.pause(100);
      const resolve = value => protocol.contracts.find(item => item.id === value)?.address ?? value;
      protocol.deploymentActions.push({ ...step, targetAddress: resolve(step.target), resolvedArgs: step.args.map(resolve) });
      this.receipt(id, "initialize-contract", `${step.target}.${step.method}(${step.args.join(", ")})`);
      this.persist();
    }
    protocol.stage = 1;
    this.receipt(id, "deploy", `${protocol.contracts.length} contracts deployed and initialized`);
    this.persist();
  }

  async generateAuditorKey(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 1 || protocol.auditor) return;
    await this.pause(800);
    protocol.auditor = {
      algorithm: "ML-KEM-768",
      publicKey: token("mlkem_pub_", `${id}:auditor:public`, 80),
      privateKey: token("mlkem_sec_", `${id}:auditor:private`, 80),
    };
    this.receipt(id, "auditor", "Auditor ML-KEM-768 keypair generated", true);
    this.persist();
  }

  confirmAuditorKey(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 1 || !protocol.auditor?.privateKey || !protocol.auditor?.publicKey) return;
    protocol.stage = 2;
    this.persist();
  }

  async configureAuditor(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 2 || !protocol.auditor) return;
    await this.pause();
    protocol.auditorConfigured = true;
    protocol.stage = 3;
    this.receipt(id, "configure", "Auditor public key registered by operator", true);
    this.persist();
  }

  async generateSpendSecret(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 3 || this.state.identities.length || this.state.identityCeremony.phase !== "empty") return;
    await this.pause(420);
    this.state.identityCeremony = {
      phase: "spend_secret",
      spendPrivateKey: token("spend_sk_", "global:0:spend:private", 64),
      viewPrivateKey: null,
      spendPublicKey: null,
      viewPublicKey: null
    };
    this.persist();
  }

  async generateSpendPublic(id) {
    const protocol = this.protocol(id);
    const ceremony = this.state.identityCeremony;
    if (protocol.stage !== 3 || this.state.identities.length || ceremony.phase !== "spend_secret") return;
    await this.pause(620);
    Object.assign(ceremony, spendPublicKeys(ceremony.spendPrivateKey));
    ceremony.phase = "spend_public";
    this.persist();
  }

  async generateViewSecret(id) {
    const protocol = this.protocol(id);
    const ceremony = this.state.identityCeremony;
    if (protocol.stage !== 3 || this.state.identities.length || ceremony.phase !== "spend_public") return;
    await this.pause(420);
    ceremony.viewPrivateKey = token("mlkem_sk_", "global:0:view:private", 80);
    ceremony.phase = "view_secret";
    this.persist();
  }

  async generateIdentity(id) {
    const protocol = this.protocol(id);
    const ceremony = this.state.identityCeremony;
    if (protocol.stage !== 3 || this.state.identities.length || ceremony.phase !== "view_secret") return;
    await this.pause(760);
    ceremony.viewPublicKey = token("mlkem_pk_", `${ceremony.viewPrivateKey}:keygen`, 80);
    ceremony.phase = "complete";
    this.state.identities = PARTY_NAMES.map((name, index) => ({
      id: `party-${index}`,
      name,
      ...(index === 0 ? { spendPublicKey: ceremony.spendPublicKey, institutionalSpendPublicKey: ceremony.institutionalSpendPublicKey } : spendPublicKeys(token("spend_sk_", `global:${index}:spend:private`, 64))),
      spendPrivateKey: index === 0 ? ceremony.spendPrivateKey : token("spend_sk_", `global:${index}:spend:private`, 64),
      viewPublicKey: index === 0 ? ceremony.viewPublicKey : token("mlkem_pk_", `global:${index}:view:public`, 80),
      viewPrivateKey: index === 0 ? ceremony.viewPrivateKey : token("mlkem_sk_", `global:${index}:view:private`, 80),
    }));
    this.state.createdAt = Date.now();
    this.persist();
  }

  async confirmIdentity(id) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 3 || !this.state.identities.length || this.state.identityCeremony.phase !== "complete") return;
    await this.pause(220);
    protocol.stage = 4;
    this.receipt(id, "identity", "Participant identity confirmed for registration");
    this.persist();
  }

  async registerParty(id, partyIndex = this.protocol(id).registrations.length) {
    const protocol = this.protocol(id);
    if (protocol.stage !== 4 || !protocol.auditorConfigured || !this.state.identities[partyIndex]) return;
    if (partyIndex !== protocol.registrations.length) throw new Error("Parties must register sequentially.");
    await this.pause(160);
    const identity = this.state.identities[partyIndex];
    const policy = "long_term";
    protocol.registrations.push({
      partyId: identity.id,
      name: identity.name,
      spendPublicKey: spendPublicKeyFor(identity, id),
      viewPublicKey: identity.viewPublicKey,
      policy,
      auditEnvelope: null,
    });
    this.receipt(id, "registration", `${identity.name} public keys registered`);
    this.persist();
  }

  async sharePartyViewKey(id, partyIndex) {
    const protocol = this.protocol(id);
    const registration = protocol.registrations[partyIndex];
    if (!registration || registration.auditEnvelope) return;
    await this.pause(180);
    registration.auditEnvelope = token("kem_envelope_", `${id}:${registration.partyId}:view-key`, 72);
    this.receipt(id, "audit-registration", `${registration.name} view key shared with auditor`, true);
    this.persist();
  }

  async registerAll(id) {
    const protocol = this.protocol(id);
    if (protocol.registrations.length !== 1 || !protocol.registrations[0].auditEnvelope) return;
    protocol.registrationBatchStarted = true;
    this.persist();
    while (this.protocol(id).registrations.length < PARTY_NAMES.length) {
      const partyIndex = this.protocol(id).registrations.length;
      await this.registerParty(id, partyIndex);
      await this.sharePartyViewKey(id, partyIndex);
    }
  }

  addTransaction(id, type, label, options = {}) {
    const protocol = this.protocol(id);
    const ordinal = protocol.transactions.length + 1;
    const from = options.from ?? (ordinal % 8) + 1;
    const to = options.to ?? ((ordinal + 2) % 9) + 1;
    const tx = {
      id: token("tx_", `${id}:${type}:${ordinal}`, 18),
      type,
      label,
      transferId: options.transferId || null,
      from: this.state.identities[from]?.id ?? "system",
      to: this.state.identities[to]?.id ?? "system",
      hash: token("0x", `${id}:transaction:${ordinal}`, 64),
      proof: token("proof_", `${id}:proof:${ordinal}`, 54),
      proofSystem: PROTOCOL_PRIMITIVES[id].proof,
      ciphertext: token("ciphertext_", `${id}:ciphertext:${ordinal}`, 66),
      status: options.status ?? "confirmed",
      background: Boolean(options.background),
      selectiveShared: false,
      longTermVisible: [from, to].some(index => protocol.registrations[index]?.policy === "long_term"),
    };
    if (options.encryptedNote) tx.encryptedPayload = encryptedNotePayload(id, tx.id, options.encryptedNote);
    protocol.transactions.unshift(tx);
    if (options.leaves) {
      for (let index = 0; index < options.leaves; index += 1) {
        protocol.leaves.push({
          id: token("leaf_", `${tx.id}:${index}`, 20),
          commitment: options.leafCommitments?.[index] || (options.leaves === 1 ? tx.encryptedPayload?.commitment : null) || token("0x", `${tx.id}:commitment:${index}`, 64),
          assetId: options.leafAssets?.[index] || options.assetId || options.encryptedNote?.tokenId || "DEFAULT",
          sourceTxId: tx.id,
          provenance: { from: tx.from, to: tx.to, action: type },
        });
      }
      rebuildCommitmentTrees(protocol);
    }
    this.receipt(id, type, label);
    return tx;
  }

  async executeProtocolAction(id, action, payload = {}) {
    if (!this.isReady(id)) throw new Error("Complete setup and registration first.");
    const p = this.protocol(id);
    await this.pause(220);
    let tx;
    switch (`${id}:${action}`) {
      case "institutional:channels": {
        p.flow.channelPairs = pairwiseChannels(id, p.registrations);
        p.flow.channels = true;
        tx = this.addTransaction(id, action, `${p.flow.channelPairs.length} pairwise channels established`);
        break;
      }
      case "institutional:fund": {
        mintInstitutional(p, Number(payload.amount));
        tx = this.addTransaction(id, action, `Owner minted ${Number(payload.amount).toLocaleString("en-US")} EN to each registered account`);
        break;
      }
      case "institutional:calculate": {
        institutionalState(p).draft = buildInstitutionalPayment(p, this.state.identities, payload);
        break;
      }
      case "institutional:edit-payment": institutionalState(p).draft = null; break;
      case "institutional:prove": {
        const draft = institutionalState(p).draft;
        const checks = validateInstitutionalPayment(p, draft);
        const expected = buildInstitutionalPayment(p, this.state.identities, draft);
        if (institutionalBinding(expected) !== institutionalBinding(draft)) throw new Error("Payment witness or public inputs changed. Calculate the batch again.");
        draft.checks = checks;
        draft.proof = { id: token("groth16_", institutionalBinding(draft), 64), binding: institutionalBinding(draft) };
        draft.status = "proved";
        break;
      }
      case "institutional:post": {
        const s = institutionalState(p), draft = s.draft;
        validateInstitutionalPayment(p, draft);
        if (!draft.proof || draft.proof.binding !== institutionalBinding(draft)) throw new Error("Generate the ZK proof before posting.");
        const expected = buildInstitutionalPayment(p, this.state.identities, draft);
        if (institutionalBinding(expected) !== institutionalBinding(draft)) throw new Error("Payment inputs changed. Calculate and prove a fresh batch.");
        draft.status = "submitted";
        draft.verification = 0;
        this.persist();
        try {
          await this.pause(400);
          draft.status = "verifying";
          for (let i = 1; i <= 6; i++) {
            draft.verification = i;
            this.persist();
            await this.pause(220);
          }
          const batch = settleInstitutionalPayment(p);
          tx = this.addTransaction(id, "payment", `${batch.k} commitment deltas verified and applied`, { from: -1, to: -1 });
          Object.assign(tx, { batch, proof: batch.proof.id, from: "relayer", to: "Enygma", block: p.ledger[0].block });
        } catch (error) {
          draft.status = "proved";
          draft.verification = 0;
          this.persist();
          throw error;
        }
        break;
      }
      case "institutional:pause-contract":
      case "institutional:resume-contract": {
        if (payload.actor !== "owner") throw new Error("Only the contract owner can pause or resume the contract.");
        const s = institutionalState(p);
        s.paused = action === "pause-contract";
        tx = this.addTransaction(id, action, s.paused ? "Owner paused the Enygma contract" : "Owner resumed the Enygma contract");
        break;
      }
      case "institutional:freeze-user":
      case "institutional:unfreeze-user": {
        if (payload.actor !== "auditor") throw new Error("Switch to the auditor to manage user trading eligibility.");
        const s = institutionalState(p), accountId = Number(payload.accountId);
        const account = s.accounts.find(a => a.accountId === accountId);
        if (!account) throw new Error("Choose a registered user.");
        s.frozen = action === "freeze-user" ? [...new Set([...s.frozen, accountId])] : s.frozen.filter(id => id !== accountId);
        tx = this.addTransaction(id, action, `Auditor ${action === "freeze-user" ? "froze" : "unfroze"} ${account.name} from trading`);
        break;
      }
      case "retail:configure-tags": {
        const recipientIndex = Number(payload.recipientIndex);
        const recipient = p.registrations[recipientIndex];
        const mode = ["none", "subset", "rift", "full"].includes(payload.mode) ? payload.mode : "full";
        if (!recipient || recipient.partyId === "party-0") throw new Error("Select a registered recipient other than the payer.");
        const excludedIndices = mode === "rift" ? (payload.excludedIndices || []).map(Number).filter(index => Number.isInteger(index) && index >= 0 && index < p.registrations.length && index !== recipientIndex) : [];
        const candidateIndices = retailTagCandidateIndices(mode, p.registrations.length, recipientIndex, excludedIndices);
        p.flow.retailRecipient = {
          partyId: recipient.partyId,
          name: recipient.name,
          spendPublicKey: recipient.spendPublicKey,
          viewPublicKey: recipient.viewPublicKey
        };
        p.flow.retailTagChannel = {
          id: token("tag_channel_", `${id}:party-0:${recipient.partyId}:${mode}`, 22),
          senderPartyId: "party-0",
          recipientPartyId: recipient.partyId,
          mode,
          excludedIndices,
          candidateIndices,
          bitmap: Array.from({ length: p.registrations.length }, (_, index) => candidateIndices.includes(index) ? "1" : "0").join(""),
          c1: token("mlkem_ct_", `${id}:${recipient.viewPublicKey}:tag-channel`, 54),
          c2: token("channel_ct_", `${id}:${recipient.partyId}:${mode}:channel-data`, 54)
        };
        tx = this.addTransaction(id, action, `Payer established a ${mode} private-tag channel with ${candidateIndices.length} bitmap candidates`, { from: 0, to: recipientIndex });
        break;
      }
      case "retail:payment": {
        if (!p.flow.retailRecipient || !p.flow.retailTagChannel) throw new Error("Establish the private-tag channel before creating a payment.");
        const amount = positiveAmount(payload.amount, "Payment amount");
        const recipientIndex = p.registrations.findIndex(item => item.partyId === p.flow.retailRecipient.partyId);
        tx = this.addTransaction(id, action, `Private payment sent to ${p.flow.retailRecipient.name}`, { leaves: 2, from: 0, to: recipientIndex, encryptedNote: { ownerPartyId: p.flow.retailRecipient.partyId, tokenId: "USD", amount } });
        tx.tagChannelId = p.flow.retailTagChannel.id;
        tx.privateTag = token("tag_", `${id}:${tx.id}:${p.flow.retailTagChannel.id}`, 40);
        break;
      }
      case "retail:scan": {
        if (!p.transactions.some(item => item.type === "payment")) throw new Error("Submit the private payment before the recipient scans for it.");
        tx = this.addTransaction(id, action, `${p.flow.retailRecipient?.name || "Recipient"} recovered one matching private note`, { from: 1, to: 1 });
        break;
      }
      case "dvp:propose-terms": {
        if (p.flow.dvpTerms.status !== "draft") throw new Error("The DvP terms have already been proposed.");
        if (p.flow.sellerShieldedSecurity <= 0 || p.flow.buyerShieldedCash <= 0) throw new Error("Both counterparties must shield their holdings before negotiating terms.");
        const securityNote = p.notes.find(note => note.id === payload.securityInputNoteId && note.ownerPartyId === "party-2" && note.status === "unspent");
        const cashNote = p.notes.find(note => note.id === payload.cashInputNoteId && note.ownerPartyId === "party-1" && note.status === "unspent");
        if (!securityNote || !cashNote) throw new Error("Select one available private note for each DvP leg.");
        const securityId = securityNote.assetId;
        const quantity = securityNote.amount;
        const cashAmount = cashNote.amount;
        const expiryBlocks = Math.max(1, Math.round(Number(payload.expiryBlocks) || 30));
        if (securityId !== p.flow.securityAssetId) throw new Error("The seller does not hold the proposed security.");
        p.flow.dvpTerms = { ...p.flow.dvpTerms, securityId, quantity, cashAmount, expiryBlocks, securityInputNoteId: securityNote.id, cashInputNoteId: cashNote.id, status: "proposed" };
        tx = this.addTransaction(id, action, "Seller proposed bilateral DvP terms", { from: 2, to: 1 });
        break;
      }
      case "dvp:accept-terms": {
        if (p.flow.dvpTerms.status !== "proposed") throw new Error("The seller must propose terms before the buyer can accept them.");
        const securityNote = p.notes.find(note => note.id === p.flow.dvpTerms.securityInputNoteId && note.status === "unspent");
        const cashNote = p.notes.find(note => note.id === p.flow.dvpTerms.cashInputNoteId && note.status === "unspent");
        if (!securityNote || !cashNote) throw new Error("The private notes referenced by these terms are no longer available.");
        p.flow.dvpTerms.status = "agreed";
        tx = this.receipt(id, action, "Buyer accepted the bilateral DvP terms");
        break;
      }
      case "dvp:mint-security": {
        if (p.flow.dvpTerms.status !== "draft") throw new Error("Establish holdings before negotiating DvP terms.");
        const amount = positiveAmount(payload.amount, "Security issuance amount");
        p.flow.sellerPublicSecurity += amount;
        tx = this.addTransaction(id, action, `Security issuer allocated ${amount.toLocaleString("en-US")} ${p.flow.securityAssetId} units to seller`, { from: 0, to: 2 });
        break;
      }
      case "dvp:mint-cash": {
        if (p.flow.dvpTerms.status !== "draft") throw new Error("Establish holdings before negotiating DvP terms.");
        const amount = positiveAmount(payload.amount, "Cash issuance amount");
        p.flow.buyerPublicCash += amount;
        tx = this.addTransaction(id, action, `Cash issuer allocated ${amount.toLocaleString("en-US")} USD to buyer`, { from: 0, to: 1 });
        break;
      }
      case "dvp:shield-security": {
        if (p.flow.dvpTerms.status !== "draft") throw new Error("Shield holdings before negotiating DvP terms.");
        if (p.flow.sellerPublicSecurity <= 0) throw new Error("Issue securities to the seller before shielding them.");
        const amount = positiveAmount(payload.amount, "Security shielding amount");
        if (amount > p.flow.sellerPublicSecurity) throw new Error("Security shielding amount exceeds the seller’s public balance.");
        p.flow.sellerShieldedSecurity += amount;
        p.flow.sellerPublicSecurity -= amount;
        p.flow.securityIssued = p.flow.sellerShieldedSecurity;
        p.flow.issued = p.flow.securityIssued;
        const owner = p.registrations[2];
        tx = this.addTransaction(id, action, `${amount.toLocaleString("en-US")} ${p.flow.securityAssetId} units shielded into seller’s private balance`, { leaves: 1, from: 2, to: 2, encryptedNote: { scope: "pretrade_note", leg: "security holding", ownerPartyId: "party-2", spendPublicKey: owner.spendPublicKey, tokenId: p.flow.securityAssetId, amount } });
        recordPrivateNote(p, tx, { ownerPartyId: "party-2", ownerName: owner.name, spendPublicKey: owner.spendPublicKey, assetId: p.flow.securityAssetId, amount });
        break;
      }
      case "dvp:shield-cash": {
        if (p.flow.dvpTerms.status !== "draft") throw new Error("Shield holdings before negotiating DvP terms.");
        if (p.flow.buyerPublicCash <= 0) throw new Error("Issue cash to the buyer before shielding it.");
        const amount = positiveAmount(payload.amount, "Cash shielding amount");
        if (amount > p.flow.buyerPublicCash) throw new Error("Cash shielding amount exceeds the buyer’s public balance.");
        p.flow.buyerShieldedCash += amount;
        p.flow.buyerPublicCash -= amount;
        p.flow.cashShielded = p.flow.buyerShieldedCash;
        const owner = p.registrations[1];
        tx = this.addTransaction(id, action, `${amount.toLocaleString("en-US")} ${p.flow.dvpTerms.cashToken} shielded into buyer’s private balance`, { leaves: 1, from: 1, to: 1, encryptedNote: { scope: "pretrade_note", leg: "cash holding", ownerPartyId: "party-1", spendPublicKey: owner.spendPublicKey, tokenId: p.flow.dvpTerms.cashToken, amount } });
        recordPrivateNote(p, tx, { ownerPartyId: "party-1", ownerName: owner.name, spendPublicKey: owner.spendPublicKey, assetId: p.flow.dvpTerms.cashToken, amount });
        break;
      }
      case "dvp:instantiate-transfer": {
        if (p.flow.dvpTerms.status !== "agreed") throw new Error("Both parties must agree to the DvP terms first.");
        if (p.flow.dvpTransfer && !["reverted", "settled"].includes(p.flow.dvpTransfer.status)) throw new Error("This DvP agreement already has an active transfer.");
        const securityInput = p.notes.find(note => note.id === p.flow.dvpTerms.securityInputNoteId && note.status === "unspent");
        const cashInput = p.notes.find(note => note.id === p.flow.dvpTerms.cashInputNoteId && note.status === "unspent");
        if (!securityInput || !cashInput) throw new Error("The private notes referenced by this DvP are not available.");
        instantiateDvpTransfer(p.flow, id);
        cashInput.status = "locked";
        p.flow.cashLocked = true;
        p.flow.locked = true;
        p.flow.settlement = "awaiting_counterparty";
        p.flow.dvpTransfer.status = "awaiting_counterparty";
        tx = this.addTransaction(id, "lock-cash", `Buyer initiated ${p.flow.dvpTransfer.id} and submitted the cash leg`, { from: 1, to: 2, transferId: p.flow.dvpTransfer.id, encryptedNote: { scope: "dvp_leg", leg: "cash", transferId: p.flow.dvpTransfer.id, ownerPartyId: "party-2", spendPublicKey: p.registrations[2].spendPublicKey, tokenId: p.flow.dvpTerms.cashToken, amount: p.flow.dvpTerms.cashAmount } });
        break;
      }
      case "dvp:lock-security": {
        if (!p.flow.dvpTransfer || !["open", "awaiting_counterparty"].includes(p.flow.dvpTransfer.status)) throw new Error("Instantiate an active DvP transfer before locking.");
        const securityInput = p.notes.find(note => note.id === p.flow.dvpTerms.securityInputNoteId && note.status === "unspent");
        const cashInput = p.notes.find(note => note.id === p.flow.dvpTerms.cashInputNoteId && note.status === "locked");
        if (!securityInput || !cashInput) throw new Error("Both DvP input notes must be available before settlement.");
        if (p.flow.securityLocked) throw new Error("The seller’s security leg is already locked.");
        p.flow.securityLocked = true;
        p.flow.locked = true;
        p.flow.settlement = "awaiting_counterparty";
        p.flow.dvpTransfer.status = "awaiting_counterparty";
        securityInput.status = "locked";
        const securityLeg = this.addTransaction(id, action, "Seller submitted the security leg", { from: 2, to: 1, transferId: p.flow.dvpTransfer.id, encryptedNote: { scope: "dvp_leg", leg: "security", transferId: p.flow.dvpTransfer.id, ownerPartyId: "party-1", spendPublicKey: p.registrations[1].spendPublicKey, tokenId: p.flow.dvpTerms.securityId, amount: p.flow.dvpTerms.quantity } });
        tx = securityLeg;
        if (p.flow.cashLocked) {
          p.flow.settlement = "settled";
          p.flow.locked = false;
          p.flow.dvpTransfer.status = "settled";
          p.flow.sellerShieldedSecurity -= p.flow.dvpTerms.quantity;
          p.flow.buyerShieldedCash -= p.flow.dvpTerms.cashAmount;
          p.flow.buyerPrivateSecurity += p.flow.dvpTerms.quantity;
          p.flow.sellerPrivateCash += p.flow.dvpTerms.cashAmount;
          securityInput.status = "spent";
          cashInput.status = "spent";
          const cashLeg = p.transactions.find(item => item.type === "lock-cash" && item.encryptedPayload?.transferId === p.flow.dvpTransfer.id);
          tx = this.addTransaction(id, "atomic-settlement", "Both DvP legs submitted; atomic settlement completed", { leaves: 2, from: 2, to: 1, leafCommitments: [securityLeg.encryptedPayload.commitment, cashLeg.encryptedPayload.commitment], leafAssets: [p.flow.dvpTerms.securityId, p.flow.dvpTerms.cashToken] });
          recordPrivateNote(p, tx, { payload: securityLeg.encryptedPayload, ownerPartyId: "party-1", ownerName: p.registrations[1].name, spendPublicKey: p.registrations[1].spendPublicKey, assetId: p.flow.dvpTerms.securityId, amount: p.flow.dvpTerms.quantity, transferId: p.flow.dvpTransfer.id, origin: "dvp_output" });
          recordPrivateNote(p, tx, { payload: cashLeg.encryptedPayload, ownerPartyId: "party-2", ownerName: p.registrations[2].name, spendPublicKey: p.registrations[2].spendPublicKey, assetId: p.flow.dvpTerms.cashToken, amount: p.flow.dvpTerms.cashAmount, transferId: p.flow.dvpTransfer.id, origin: "dvp_output" });
        }
        break;
      }
      case "dvp:timeout": {
        const exactlyOneLegLocked = p.flow.securityLocked !== p.flow.cashLocked;
        if (!exactlyOneLegLocked || p.flow.settlement === "settled") throw new Error("Timeout applies only while exactly one DvP leg is locked.");
        const waitingFor = p.flow.securityLocked ? "buyer cash leg" : "seller security leg";
        p.flow.securityLocked = false;
        p.flow.cashLocked = false;
        p.flow.locked = false;
        p.flow.settlement = "reverted";
        p.flow.dvpTransfer.status = "reverted";
        const returnedNote = p.notes.find(note => note.id === p.flow.dvpTerms.cashInputNoteId && note.status === "locked");
        if (returnedNote) returnedNote.status = "unspent";
        tx = this.addTransaction(id, action, `Counterparty ${waitingFor} was not locked; deposited leg returned`, { status: "reverted" });
        break;
      }
      case "dvp:unshield": {
        if (p.flow.settlement !== "settled") throw new Error("Complete the bilateral DvP settlement before unshielding.");
        if (p.flow.buyerPrivateSecurity < 25) throw new Error("The buyer has fewer than 25 private security units.");
        p.flow.buyerPrivateSecurity -= 25;
        p.flow.buyerPublicSecurity += 25;
        tx = this.addTransaction(id, action, "Buyer unshielded 25 acquired security units to the public vault", { from: 1, to: 1 });
        break;
      }
      case "auctions:auctioneer": {
        const auction = p.flow.auction;
        if (auction.auctioneerKey) throw new Error("The auctioneer key has already been generated for this auction.");
        auction.auctioneerKey = {
          secretKey: token("mlkem_sk_", `${id}:${auction.reference}:auctioneer-secret`, 80),
          publicKey: token("mlkem_pk_", `${id}:${auction.reference}:auctioneer-public`, 80)
        };
        tx = this.receipt(id, action, `Auctioneer generated its bid-decryption key for ${auction.title}`);
        break;
      }
      case "auctions:register-auctioneer": {
        const auction = p.flow.auction;
        if (!auction.auctioneerKey) throw new Error("The auctioneer must generate its auction key first.");
        auction.auctioneerRegistered = true;
        tx = this.addTransaction(id, action, `Operator registered the auctioneer public key for ${auction.reference}`);
        break;
      }
      case "auctions:mint-nft": {
        const auction = p.flow.auction;
        if (!auction.auctioneerRegistered) throw new Error("Register the auctioneer for this auction first.");
        const quantity = positiveAmount(payload.quantity, "Class A share quantity");
        auction.nftMinted = true;
        auction.assetQuantity = quantity;
        auction.sellerPublicNft = quantity;
        tx = this.addTransaction(id, action, `Issuer minted a non-fungible certificate representing ${quantity.toLocaleString("en-US")} Class A shares to ${p.registrations[2].name}`, { from: 0, to: 2 });
        break;
      }
      case "auctions:list-asset": {
        const auction = p.flow.auction;
        if (!auction.nftMinted || auction.sellerPublicNft !== auction.assetQuantity) throw new Error("The seller must own the complete certificate before listing it.");
        const duration = Math.max(2, Math.round(Number(payload.duration) || 12));
        const owner = p.registrations[2];
        auction.biddingDuration = duration;
        auction.blocksRemaining = duration;
        auction.sellerPublicNft = 0;
        auction.listed = true;
        auction.status = "bidding";
        tx = this.addTransaction(id, action, `${owner.name} listed a certificate for ${auction.assetQuantity.toLocaleString("en-US")} Class A shares with a ${duration}-block bidding window`, { leaves: 1, assetId: auction.assetType, from: 2, to: 2, encryptedNote: { scope: "auction_asset", leg: "locked auction asset", ownerPartyId: "party-2", spendPublicKey: owner.spendPublicKey, tokenId: auction.assetTokenId, amount: auction.assetQuantity } });
        const note = recordPrivateNote(p, tx, { ownerPartyId: "party-2", ownerName: owner.name, spendPublicKey: owner.spendPublicKey, assetId: auction.assetType, amount: auction.assetQuantity, origin: "auction_asset" });
        if (note) note.status = "locked";
        auction.nftNoteId = note?.id || null;
        break;
      }
      case "auctions:mint-cash": {
        const auction = p.flow.auction;
        if (!auction.listed) throw new Error("The seller must list the auction asset before bidder funding begins.");
        const amount = positiveAmount(payload.amount, "USD mint amount");
        auction.publicCash += amount;
        tx = this.addTransaction(id, action, `Operator minted ${amount.toLocaleString("en-US")} USD to the bidder`, { from: 0, to: 0 });
        break;
      }
      case "auctions:shield-cash": {
        const auction = p.flow.auction;
        const amount = positiveAmount(payload.amount, "USD shielding amount");
        if (amount > auction.publicCash) throw new Error("The shielding amount exceeds the bidder’s public USD balance.");
        const owner = p.registrations[0];
        auction.publicCash -= amount;
        auction.privateCash += amount;
        tx = this.addTransaction(id, action, `${owner.name} shielded ${amount.toLocaleString("en-US")} USD into a private bidding note`, { leaves: 1, from: 0, to: 0, encryptedNote: { scope: "auction_funding", leg: "bidder funding", ownerPartyId: "party-0", spendPublicKey: owner.spendPublicKey, tokenId: auction.cashToken, amount } });
        recordPrivateNote(p, tx, { ownerPartyId: "party-0", ownerName: owner.name, spendPublicKey: owner.spendPublicKey, assetId: auction.cashToken, amount, origin: "auction_funding" });
        break;
      }
      case "auctions:submit-bid": {
        const auction = p.flow.auction;
        if (auction.status !== "bidding") throw new Error("This auction is not accepting bids.");
        const inputNote = p.notes.find(note => note.id === payload.noteId && note.ownerPartyId === "party-0" && note.origin === "auction_funding" && note.status === "unspent");
        if (!inputNote) throw new Error("Select one available private USD note for the bid.");
        const bidAmount = positiveAmount(payload.amount, "Bid amount");
        if (bidAmount > inputNote.amount) throw new Error("The bid cannot exceed the selected private note.");
        const bidder = p.registrations[0];
        const changeAmount = inputNote.amount - bidAmount;
        const changePayload = changeAmount > 0 ? encryptedNotePayload(id, `${auction.reference}:bidder-change:${auction.bids.length}`, { scope: "auction_change", leg: "bidder change", ownerPartyId: "party-0", spendPublicKey: bidder.spendPublicKey, tokenId: auction.cashToken, amount: changeAmount }) : null;
        inputNote.status = "spent";
        auction.privateCash -= bidAmount;
        tx = this.addTransaction(id, action, `${bidder.name} submitted a ${bidAmount.toLocaleString("en-US")} USD sealed bid and retained ${changeAmount.toLocaleString("en-US")} USD as private change`, { leaves: changePayload ? 1 : 0, leafCommitments: changePayload ? [changePayload.commitment] : [], leafAssets: changePayload ? [auction.cashToken] : [], from: 0, to: 2, encryptedNote: { scope: "auction_bid", leg: "sealed bid", ownerPartyId: "party-0", spendPublicKey: bidder.spendPublicKey, tokenId: auction.cashToken, amount: bidAmount } });
        const changeNote = changePayload ? recordPrivateNote(p, tx, { payload: changePayload, ownerPartyId: "party-0", ownerName: bidder.name, spendPublicKey: bidder.spendPublicKey, assetId: auction.cashToken, amount: changeAmount, origin: "auction_change" }) : null;
        const revertSalt = token("salt_", `${id}:${auction.reference}:bid:0:revert`, 40);
        auction.bids.push({
          id: token("bid_", `${id}:${auction.reference}:0`, 18), bidderPartyId: "party-0", bidderName: bidder.name,
          amount: bidAmount, inputNoteId: inputNote.id, changeNoteId: changeNote?.id || null, commitA: tx.encryptedPayload.commitment,
          commitB: token("0x", `${p.registrations[2].spendPublicKey}:${tx.id}:seller-payout:${bidAmount}`, 64),
          revertSalt, revertCommit: token("0x", `${bidder.spendPublicKey}:${revertSalt}:${auction.cashToken}:${bidAmount}`, 64),
          ciphertext: tx.ciphertext, proof: tx.proof, valid: true, status: "active"
        });
        break;
      }
      case "auctions:collect-bids": {
        const auction = p.flow.auction;
        if (!auction.bids.some(bid => bid.bidderPartyId === "party-0")) throw new Error("Submit your bid before receiving the remaining bids.");
        if (auction.otherBidsCollected) throw new Error("The remaining bids have already been received.");
        const bidders = [{ index: 1, amount: 420 }, { index: 3, amount: 575 }, { index: 4, amount: 610 }, { index: 5, amount: 540 }];
        for (const item of bidders) {
          const bidder = p.registrations[item.index];
          const bidTx = this.addTransaction(id, "sealed-bid", `${bidder.name} submitted a sealed bid`, { from: item.index, to: 2, encryptedNote: { scope: "auction_bid", leg: "sealed bid", ownerPartyId: bidder.partyId, spendPublicKey: bidder.spendPublicKey, tokenId: auction.cashToken, amount: item.amount } });
          const revertSalt = token("salt_", `${id}:${auction.reference}:bid:${item.index}:revert`, 40);
          auction.bids.push({
            id: token("bid_", `${id}:${auction.reference}:${item.index}`, 18), bidderPartyId: bidder.partyId, bidderName: bidder.name,
            amount: item.amount, inputNoteId: null, commitA: bidTx.encryptedPayload.commitment,
            commitB: token("0x", `${p.registrations[2].spendPublicKey}:${bidTx.id}:seller-payout:${item.amount}`, 64),
            revertSalt, revertCommit: token("0x", `${bidder.spendPublicKey}:${revertSalt}:${auction.cashToken}:${item.amount}`, 64),
            ciphertext: bidTx.ciphertext, proof: bidTx.proof, valid: true, status: "active"
          });
        }
        auction.otherBidsCollected = true;
        tx = this.receipt(id, action, "Four additional sealed bids entered the auction");
        break;
      }
      case "auctions:close-bidding": {
        const auction = p.flow.auction;
        if (auction.bids.length < 2) throw new Error("At least two valid bids are required for this walkthrough.");
        auction.blocksRemaining = 0;
        auction.status = "closed";
        tx = this.receipt(id, action, `The ${auction.biddingDuration}-block bidding window expired; submissions closed`);
        break;
      }
      case "auctions:prove-winner": {
        const auction = p.flow.auction;
        if (auction.status !== "closed") throw new Error("The bidding timeout must expire before bids can be opened.");
        const validBids = auction.bids.filter(bid => bid.valid && bid.status === "active");
        const winner = validBids.reduce((best, bid) => !best || bid.amount > best.amount ? bid : best, null);
        if (!winner) throw new Error("No valid bids are available.");
        auction.winnerProof = {
          id: token("proof_", `${id}:${auction.reference}:highest-valid-bid`, 54),
          winnerBidId: winner.id,
          winnerPartyId: winner.bidderPartyId,
          winnerCommitment: winner.commitA,
          validBidCount: validBids.length,
          privateWinningAmount: winner.amount,
          statement: "The selected commitment is an active valid bid and its hidden amount is greater than or equal to every other active valid bid."
        };
        auction.status = "winner_announced";
        tx = this.addTransaction(id, action, "Auctioneer announced the winning bid commitment with a highest-valid-bid proof", { from: 2, to: 0 });
        break;
      }
      case "auctions:challenge": {
        const auction = p.flow.auction;
        if (!auction.winnerProof || auction.settlement) throw new Error("A pending winner proof is required before challenge.");
        auction.challengeOpen = true;
        tx = this.addTransaction(id, action, "Winner proof challenged; contract verification required", { status: "challenged" });
        break;
      }
      case "auctions:settle": {
        const auction = p.flow.auction;
        if (!auction.winnerProof || auction.settlement) throw new Error("The auctioneer must announce a proven winner before settlement.");
        const winner = auction.bids.find(bid => bid.id === auction.winnerProof.winnerBidId);
        const winnerRegistration = p.registrations.find(item => item.partyId === winner.bidderPartyId);
        const seller = p.registrations[2];
        const nftPayload = encryptedNotePayload(id, `${auction.reference}:nft-output`, { scope: "auction_settlement", leg: "asset delivery", ownerPartyId: winner.bidderPartyId, spendPublicKey: winnerRegistration.spendPublicKey, tokenId: auction.assetTokenId, amount: auction.assetQuantity });
        const cashPayload = encryptedNotePayload(id, `${auction.reference}:cash-output`, { scope: "auction_settlement", leg: "seller proceeds", ownerPartyId: seller.partyId, spendPublicKey: seller.spendPublicKey, tokenId: auction.cashToken, amount: winner.amount, commitment: winner.commitB });
        const losers = auction.bids.filter(bid => bid.id !== winner.id && bid.status === "active");
        const recoveryPayloads = losers.map(bid => {
          const registration = p.registrations.find(item => item.partyId === bid.bidderPartyId);
          return encryptedNotePayload(id, `${auction.reference}:${bid.id}:recovery`, { scope: "auction_recovery", leg: "losing bid recovery", ownerPartyId: bid.bidderPartyId, spendPublicKey: registration.spendPublicKey, tokenId: auction.cashToken, amount: bid.amount, salt: bid.revertSalt, commitment: bid.revertCommit });
        });
        const payloads = [nftPayload, cashPayload, ...recoveryPayloads];
        tx = this.addTransaction(id, "atomic-auction-settlement", `${auction.challengeOpen ? "Challenged proof verified; " : "Winner proof accepted; "}auction DvP settled atomically`, { leaves: payloads.length, from: 2, to: this.state.identities.findIndex(identity => identity.id === winner.bidderPartyId), leafCommitments: payloads.map(item => item.commitment), leafAssets: [auction.assetType, ...payloads.slice(1).map(() => auction.cashToken)] });
        const nftInput = p.notes.find(note => note.id === auction.nftNoteId);
        if (nftInput) nftInput.status = "spent";
        for (const bid of auction.bids) {
          bid.status = bid.id === winner.id ? "won" : "recovered";
          const input = p.notes.find(note => note.id === bid.inputNoteId);
          if (input) input.status = "spent";
        }
        recordPrivateNote(p, tx, { payload: nftPayload, ownerPartyId: winner.bidderPartyId, ownerName: winnerRegistration.name, spendPublicKey: winnerRegistration.spendPublicKey, assetId: auction.assetType, amount: auction.assetQuantity, origin: "auction_output" });
        recordPrivateNote(p, tx, { payload: cashPayload, ownerPartyId: seller.partyId, ownerName: seller.name, spendPublicKey: seller.spendPublicKey, assetId: auction.cashToken, amount: winner.amount, origin: "auction_output" });
        recoveryPayloads.forEach((payloadItem, index) => {
          const bid = losers[index];
          const registration = p.registrations.find(item => item.partyId === bid.bidderPartyId);
          recordPrivateNote(p, tx, { payload: payloadItem, ownerPartyId: bid.bidderPartyId, ownerName: registration.name, spendPublicKey: registration.spendPublicKey, assetId: auction.cashToken, amount: bid.amount, origin: "auction_recovery" });
        });
        if (winner.bidderPartyId !== "party-0") auction.privateCash += auction.bids.find(bid => bid.bidderPartyId === "party-0")?.amount || 0;
        auction.challengeOpen = false;
        auction.status = "settled";
        auction.settlement = { txId: tx.id, winnerBidId: winner.id, winnerCommitment: winner.commitA, winnerPartyId: winner.bidderPartyId, validBidCount: auction.winnerProof.validBidCount };
        break;
      }
      default: throw new Error(`Unsupported protocol action: ${id}:${action}`);
    }
    this.persist();
    return tx;
  }

  async shareTransactionKey(id, txId) {
    const protocol = this.protocol(id);
    const tx = protocol.transactions.find(item => item.id === txId);
    if (!tx?.encryptedPayload) throw new Error("This transaction does not carry selectively disclosable note data.");
    await this.pause(180);
    tx.selectiveShared = true;
    protocol.disclosures.push({
      txId,
      envelope: token("kem_txkey_", `${id}:${txId}:selective`, 70),
      scope: "single transaction",
      keyType: "symmetric note-data key",
      recipient: protocol.selectiveRegulator.name,
      regulatorPublicKey: protocol.selectiveRegulator.publicKey
    });
    this.receipt(id, "disclosure", `Transaction key disclosed to additional regulator`, true);
    this.persist();
  }

  setTraffic(id, enabled, assetId = null) {
    const protocol = this.protocol(id);
    if (!["retail", "dvp"].includes(id) || !this.isReady(id)) return;
    protocol.traffic = Boolean(enabled);
    protocol.trafficAsset = enabled && id === "dvp" ? (assetId || protocol.flow.securityAssetId) : null;
    this.receipt(id, "traffic", `Network traffic ${enabled ? "enabled" : "paused"}`);
    this.persist();
    this.syncTraffic(id);
  }

  syncTraffic(id) {
    clearInterval(this.timers.get(id));
    this.timers.delete(id);
    if (!this.protocol(id).traffic) return;
    const timer = setInterval(() => {
      const protocol = this.protocol(id);
      if (!protocol.traffic) return;
      const count = protocol.transactions.filter(tx => tx.background).length + 1;
      const type = id === "retail" ? "background-payment" : "background-shield";
      const label = id === "retail" ? `Background payment ${count}` : `Background participant shielded holding ${count}`;
      const from = ((count * 2) % 8) + 1;
      const to = ((count * 2 + 2) % 8) + 1;
      const trafficAsset = protocol.trafficAsset || protocol.flow.securityAssetId;
      const encryptedNote = id === "retail"
        ? { ownerPartyId: `party-${to}`, tokenId: "USD", amount: 10 + count }
        : { scope: "pretrade_note", leg: "background holding", ownerPartyId: `party-${to}`, tokenId: trafficAsset, amount: 25 + count };
      this.addTransaction(id, type, label, { leaves: id === "retail" ? 2 : 1, background: true, from, to, encryptedNote });
      this.persist();
    }, 3400);
    this.timers.set(id, timer);
  }

  restoreTimers() {
    for (const id of ["retail", "dvp"]) this.syncTraffic(id);
  }

  resetProtocol(id) {
    clearInterval(this.timers.get(id));
    this.timers.delete(id);
    this.state.protocols[id] = blankProtocol(id);
    this.persist();
  }

  resetAll() {
    for (const timer of this.timers.values()) clearInterval(timer);
    this.timers.clear();
    this.state = freshState();
    this.persist();
  }
}

export const engineAdapter = new DemoEngine();

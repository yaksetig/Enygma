# Enygma Demo

An interactive, single-file walkthrough of the Enygma protocols. Open
[`index.html`](./index.html) in a browser — there is no build step, no server and no network
access of any kind.

The demo is deliberately **not** a mock-up. Commitments are real curve points, payloads are real
AES-GCM ciphertexts, and the conservation checks are computed rather than asserted. See
[What is real](#what-is-real-and-what-is-substituted) for the exact boundary.

## One identity, four networks

Enygma is four protocols, and each is deployed as its own network with its own `UserRegistry`,
its own participants and its own circuits. They are not one system, and the demo does not pretend
they are. What they genuinely share is your identity: a keypair minted once works on all four.

That is the shape of the walkthrough:

| Step | What happens |
| --- | --- |
| **1 · Keys** | You generate one spend keypair and one ML-KEM view keypair, for real, in the browser. Nothing is registered and no network is loaded until you do. |
| **2 · Choose** | Which network to open. Each card names the keys that network needs, and is ticked once your keys are on its registry. |
| **3 · Flow** | The network itself. |

**The first network you open forms in front of you**: an empty registry, then ten members
registering one at a time — yours first, through every derivation stage, because that is the part
worth reading. Whatever else the network needs to be usable follows (pairwise channels and opening
balances for Institutional; opening notes for DvP and Auctions).

**Every network after that, you simply join**: the other nine were registered before you arrived,
and joining is one `register(pk_spend, pk_view)` call carrying the keys you already hold. About
two seconds, no second key generation, ever. Each network offers *Replay how this network formed*
if you want the long version again.

Each network consumes the subset of your identity its circuits need:

| Protocol | spend keypair | view keypair (ML-KEM-768) | extras |
| --- | --- | --- | --- |
| **Institutional Payments** | ✅ | ✅ | a pairwise channel with every other bank |
| **Retail Payments** | ✅ | ✅ | an auditor escrow of your view key |
| **DvP** | ✅ | ✅ | — |
| **Auctions** | ✅ | — | the auction house's own key, not yours |

Each network is deep-linkable: `#/institutional`, `#/retail`, `#/dvp`, `#/auctions`. Opening one
before generating keys lands you back on step 1 — the identity is the prerequisite, not a formality.
The single-step ▶ control is still there for registering one more party by hand.

## The four protocols

| Product | What it shows |
| --- | --- |
| **Institutional Payments** | Confidential bank balances as Pedersen commitments; composing one payment by hand and stepping through the arithmetic that produces the envelope; the envelope as it lands on chain; and the recipient side, where a bank trials the messaging tag at its own slot. Its **Bridge** tab is where the two commitment schemes meet: `withdraw` turns an account balance into a note and `deposit` brings it back, spending the note and publishing a nullifier. |
| **Retail Payments** | Shielded ERC-20 transfers between people. One note in, two notes out — a payment and your change — with the recipient scanning the chain by trial decapsulation, and an auditor escrow on the side. |
| **DvP** | An atomic two-legged swap: an asset against cash, with both terminal outcomes — settlement and the deadline revert. Swapping lives here and only here; the Institutional Bridge tab hands off to it rather than duplicating it. |
| **Auctions** | Sealed-bid NFT auctions. Every bid is encrypted to a single auctioneer key, and only the winner's is ever opened. |

Institutional Payments is additionally viewed through a **perspective** selector — Bank, Private
Network Hub (the chain), Regulator, or Operator. The same ledger renders differently for each,
which is the point: what a party can see is a function of the keys it holds, not of a UI
permission flag.

## Provenance

The behaviour is taken from the protocol documents in this repository, not invented:

- [`enygma_payments/protocol_description.md`](../enygma_payments/protocol_description.md) —
  key generation and registration (§2–3), key agreement (§4), issuance (§5), the private-transfer
  envelope, nullifier, messaging tags, proof statement and Retrieve procedure (§6), and auditing (§7).
- [`enygma_payments/payload.mmd`](../enygma_payments/payload.mmd) — the wire layout of a transaction.
- [`enygma_retail_payments/protocol_description.md`](../enygma_retail_payments/protocol_description.md) —
  the retail note model, registration with auditor escrow, and the scan-by-decapsulation path.
- [`enygma_dvp/dvp_protocol.md`](../enygma_dvp/dvp_protocol.md) — note commitments, nullifiers,
  Alice's leg, Bob's retrieval and completion, and the revert path.
- [`enygma_dvp_auctions/docs/auction_protocol_v2.md`](../enygma_dvp_auctions/docs/auction_protocol_v2.md) —
  the sealed-bid auction layer built on top of DvP.
- [`enygma_payments/contracts/enygma/contracts/Enygma.sol`](../enygma_payments/contracts/enygma/contracts/Enygma.sol) —
  `transfer`, `withdraw`, `deposit` and the supply `check`.

Constraint counts and gas figures quoted in the pages are measured from the real circuits —
the DvP numbers come from [`enygma_dvp/README.md`](../enygma_dvp/README.md).

## What is real, and what is substituted

**Computed for real**, in the browser, using WebCrypto and a hand-written secp256k1 implementation:

- the identity in step 1: `sk ← crypto.getRandomValues(32)`, and both public halves derived under
  separate domain labels — `pk_spend = H("enygma/spend/v1" ‖ sk_spend)`,
  `pk_view = H("enygma/mlkem768/ek/v1" ‖ sk_view)`
- every Pedersen commitment `C = v·G + r·H`, and the point additions that apply a payment
- the conservation invariants `Σ rᵢ ≡ 0 (mod N)` and `Σ Cᵢ = 𝒪`
- `H` derived nothing-up-my-sleeve by hashing a constant to the curve
- all HKDF-SHA256 derivations — blinding factors, messaging tags, per-block content keys — under
  separate domain labels
- AES-256-GCM payloads. The failed trial decryptions are genuine AEAD authentication failures,
  not a rendered "no"
- the tag trial that discovers a payment, and the `C − r·H = v·G` check that opens it
- DvP note hashes, nullifiers and the Merkle root

**Substituted**, because a static page cannot do otherwise:

| Production | Here | Why |
| --- | --- | --- |
| Poseidon | SHA-256 | WebCrypto has no Poseidon. Structure is unchanged. |
| BN254 | secp256k1 | Same commitment algebra; no pairing is needed for what is demonstrated. |
| ML-KEM-768 encapsulation | a consistent pairwise secret | Running a lattice KEM in JavaScript is out of scope; every value *derived* from the secret is real. |
| Groth16 proof | a placeholder digest | Proof generation is not a browser-page operation. |

No claim the page makes about conservation, discovery or attribution depends on a substituted
part. The test suites re-derive each of them independently from the exposed state.

## A second page: shield → lock → swap → audit

[`settlement.html`](./settlement.html) is a separate self-contained page showing the DvP lifecycle
as chain state you watch change. Alice starts with 100 ACME, Bob with 5,000 USDC, both public.
Five buttons, each one a real contract call:

| Step | Call | What moves on chain |
| --- | --- | --- |
| **1 · Shield** | `Erc20CoinVault.depositV2` | `transferFrom` sends the tokens to the vault; a commitment `Poseidon4(pk_spend, salt, amount, tokenId)` is inserted as a leaf, and an `EncryptedNote` is emitted alongside it. |
| **2 · Lock** | `EnygmaDvp.submitPartialSettlement` ×2 | Each leg locks its nullifier and registers a `swapId` with a deadline. No token moves and nothing is spent — locking is not spending. |
| **3 · Swap** | `EnygmaDvp.exchangeOnGroupPair` | Both nullifiers published, both input leaves spent, two new leaves inserted with ownership swapped — in one transaction. |
| **4 · Audit** | `EnygmaDvp.registerAuditor` | An auditor is registered; then a party discloses `sk_view`. Decryption on the page is a real AEAD open. |
| **5 · Unshield** | `Erc20CoinVault.withdraw` | Nullify and `transfer` back out, so the tokens land in public balances under their new owners. |

Two things the page is careful about, because they are where a demo usually cheats:

**The vault has a history, and it does not stand still.** Eight deposits are already in the tree
when the page loads — the vault did not open this morning. Alice then shields into leaf 8, three
unrelated deposits land while she waits, and only then does Bob shield into leaf 12. On top of
that an ambient ticker adds one more unrelated deposit every four seconds for as long as the page
is open, so the anonymity set keeps growing while you work; the chain header has a
**network traffic: on/off** control to pause it. Nobody chooses their neighbours in the tree, and
a two-leaf tree would make the spend proof's privacy vacuous no matter how sound the circuit is.

**The chain has no per-leaf spend state.** A nullifier is `H(sk_spend, leafIndex)` and cannot be
run backwards, so the ledger genuinely cannot say which leaf a nullifier retired. The tree pane
therefore renders every leaf identically, forever — no spent markers, no greying out — and states
the anonymity set outright: *15 leaves under this root · 2 retired, which 2 is unknown*. A
**reveal openings** toggle shows the owners and amounts with a standing warning that none of it is
on chain; it exists to make the contrast visible, not to describe the ledger.

The **Groth16 statement** pane spells out the same thing as data: `merkleRoot`, the nullifier, the
output commitments and the deadline are public inputs; `leafIndex`, `salt`, `amount`, `tokenId`,
`sk_spend` and the Merkle path are private witness, rendered as redactions. The proof says *"I know
an opening of some leaf under this root"* — never which one.

The other headline is in the balances panel: after step 3 the vault's **public ERC-20 balances are
unchanged**. Ownership moved inside the shielded set and nothing about it reached the token
contracts. Step 5 proves the swap was real by taking the tokens back out under the new owners.

The audit step is worth clicking twice. With only Alice's view key the auditor opens exactly her
two notes — the 100 ACME she shielded and the 5,000 USDC she ended up with — while every other note
stays an AEAD failure. A view key is per-party, and it carries no spend authority.

Same substitution boundary as `index.html`: SHA-256 stands in for Poseidon, a derived pairwise
secret for ML-KEM encapsulation. Commitments, nullifiers, the Merkle root, the HKDF derivations and
every AES-GCM open are computed for real in the browser.

## Tests

Thirteen headless suites drive the pages in Chromium and check the cryptography from the outside —
re-deriving values with their own code and comparing, rather than trusting what is rendered.

```bash
cd enygma_demo
npm install            # playwright
npx playwright install chromium
npm test
```

To run a single suite: `node tests/keys-test.cjs`.

If Playwright or Chromium live outside the project, point at them explicitly:

```bash
PLAYWRIGHT_PATH=/path/to/playwright CHROMIUM_PATH=/path/to/chrome npm test
```

`DEMO_PAGE=/path/to/other.html` runs the suites against a different build of the page.

| Suite | Covers |
| --- | --- |
| `keys-test.cjs` | The shell: the page opens on key generation and nothing is seeded before it; both public keys re-derive independently from the secrets; regeneration draws fresh entropy; every registry starts empty and fills incrementally rather than in one jump; you are the first of the ten registered, carrying the `pk_spend` from step 1, in all four products; deep links are gated on holding keys; each product boots exactly once. |
| `env-test.cjs` | Envelope shape against §6; blinding factors are re-derivable by each recipient; the sender's slot tag is *not* derivable from any channel secret; per-bank Retrieve; a non-recipient cannot decrypt another slot. |
| `pk-test.cjs` | Interbank-settlement and client-payment legs in one transaction; fixed-length payloads, so a decoy, a settlement and a customer batch are byte-identical on the wire. |
| `flow-test.cjs` | Tab and pane wiring, the guided walkthrough end to end, a manual payment with its six derivation steps, and a cold start from an empty registry. |
| `inv-test.cjs` | Fresh accounts are `Com(0,0) = 𝒪` and identical for every bank; total supply is invariant across transfers. |
| `aud-test.cjs` | Decapsulating a disclosed capsule verifies that index; a commitment that disagrees is *not* marked audited; a bank opening its own channel is not an audit. |
| `bridge-test.cjs` | The Institutional Bridge tab: supply is unchanged in both directions, note-hash binding, Merkle root, owner unattributability per persona, that bringing a note back publishes a nullifier and spends the leaf without deleting it, double-spend rejection, escrow reconciliation, and that a freeze blocks both legs. Also that no swap UI survives here. |
| `settlement-test.cjs` | [`settlement.html`](./settlement.html): the five steps end to end — commitments and the Merkle root re-derived from their openings, nullifiers re-derived from their own spend keys, that the chain panel leaks no owner/asset/amount, that the vault's public balances are unchanged across the swap, that one view key opens its owner's notes and genuinely fails on the other's, and that supply is conserved from shield to unshield. Overrides `DEMO_PAGE` for itself — use `SETTLEMENT_PAGE` to point it elsewhere. |
| `swap-test.cjs` | The DvP network: all three output commitments re-derived from their openings, both nullifiers, salts as HKDF of the shared secret under separate labels, a fresh IV per sealing, a UI run end to end, and the revert path returning your own asset. |
| `nf-test.cjs` | The notes table exposes no per-leaf spend state, the published nullifier set never names a leaf, the contract's check order is stated where spending happens, and only a leaf's own holder can decide by recomputing its nullifier. |
| `frz-test.cjs`, `frz-test2.cjs` | The operator's halt blocks `transfer`, `withdraw` and `deposit` — including mid-walkthrough — and grants no visibility. |
| `faq-test.cjs` | The protocol explainer and FAQ: placement, expand/collapse, grounding in the spec's own terms, and layout down to 430px. |

Suites that exercise a network call `enterProduct(pg, 'institutional')` from
[`tests/_env.cjs`](./tests/_env.cjs), which walks step 1 and step 2 and then takes the **join**
path — the same two-second path a returning visitor takes. Pass `{ mode: 'formation' }` to watch a
network form from empty instead (what `keys-test.cjs` does), or `{ mode: 'empty' }` to stop at the
empty registry.

## Notes

- **Light theme only, one palette.** The four networks used to carry four grounds, four accents and
  three different dark variants; they now share a single light palette and one header rhythm, so
  moving between them reads as moving around one product rather than between four.
- Self-contained: no external scripts, styles, fonts or images. The published version runs under a
  content-security policy that blocks every external host, so everything is inline.
- Each network ends with a **provenance strip** linking the actual files it is a picture of —
  protocol descriptions, contracts and circuits in this repository — and every FAQ closes with a
  *This build* section stating plainly what is real cryptography here and what stands in for
  something a browser cannot do.
- The four networks share a document, and some element ids still appear in more than one of them
  (the FAQ ids are now namespaced per network; others are not). Each scopes every lookup to its own
  root element, so this is invisible at runtime — but a query written against `document` may
  resolve into a network you did not mean. Scope to `#app-payment`, `#app-retail`, `#app-dvp`
  or `#app-auctions`.

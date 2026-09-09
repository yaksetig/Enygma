# Enygma browser demo

This directory contains a polished, browser-native walkthrough of four Enygma protocol experiences: institutional payments, retail payments, delivery versus payment (DvP), and sealed-bid auctions.

The walkthrough is self-contained and intentionally isolated from live protocol services. It does not call Go services, relayers, provers, blockchains, RPC endpoints, or repository backends, and it uses no network API at runtime. Addresses, keys, ciphertexts, proofs, blocks, receipts, transaction hashes, and commitments are generated consistently by the local demo engine.

## Unified setup

Every protocol follows the same ordered setup:

1. The system operator deploys that protocol’s contract suite.
2. The auditor generates a protocol-specific ML-KEM keypair.
3. The operator registers the auditor public key in the protocol configuration.
4. The visitor completes the personal key ceremony in strict order: spend secret key, spend public key, view secret key, view public key—then confirms the global Enygma identity or reviews and reuses it unchanged.
5. The visitor first registers both public keys, then separately encrypts and shares the view key with the auditor. A “Register others” action visibly performs the same two operations for each of the nine additional parties. The registry table exposes every participant name, spend public key, view public key, key-registration status, and auditor-sharing status; full keys can be expanded inline.

Each setup screen includes an explicit **Executing entity** panel so the audience can distinguish operator actions, auditor actions, participant-controlled key generation, and per-party registration.

Setup and protocol execution are deliberately separated. Registration occupies its own screen; once it completes, the setup workspace is removed and the protocol walkthrough begins. The walkthrough renders exactly one current operation at a time—with explicit Previous and Next navigation—and never combines the registration table and protocol action controls into one dashboard.

The Retail walkthrough follows the standard per-payment path. The payer retrieves the recipient’s registered spend and view public keys, performs a fresh ML-KEM encapsulation to the view public key, constructs recipient and change commitments, produces the payment proof, and submits the resulting envelope. The recipient then scans published envelopes with its private view key and accepts a note only after recomputing the corresponding commitment. The optional high-frequency channel optimization is intentionally outside this walkthrough; the demo does not introduce recipient-tag modes that the protocol does not define.

The DvP walkthrough is explicitly bilateral. Boreal Markets acts as seller and Atlas Bank as buyer. Issuance and shielding amounts are editable, so each party can shield all or only part of its chosen public allocation before negotiation. Each shielding submission creates a separate private note with its own fresh salt, commitment, and asset-tree leaf; earlier notes remain visible and independently tracked. The seller’s proposal selects one unspent security note and one unspent cash note, making the committed token and amount on each input leaf the exact terms of the exchange. On one dedicated approval screen, the buyer first accepts that term sheet and then initiates it: initiation creates a unique transfer ID and terms commitment and submits the buyer’s encrypted cash leg. If the seller submits the matching encrypted security leg in time, the second leg triggers atomic settlement and creates the two recipient output commitments; otherwise the buyer’s cash note returns to an unspent state after timeout.

The Auctions walkthrough is organized around one named `Class A Share Auction`. The auctioneer first generates a dedicated ML-KEM bid-decryption key and the operator binds its public key to that exact auction reference. A share issuer mints one non-fungible Class A certificate to Boreal Markets; the seller then locks it into an auction commitment and chooses the bidding timeout. Bidders see the auction reference, asset type, and remaining time but not the certificate ID or private metadata. The operator separately funds the visitor with USD, the visitor shields a chosen amount into a distinct private note, and that exact note is locked as a sealed bid. Additional registered bidders contribute opaque commitments before the timeout closes submissions.

After the bidding deadline, the auctioneer opens all active bids with its auction-specific secret key and the auditor can inspect the same bids through the registration-time view-key envelopes. Public observers continue to see only commitments. The auctioneer announces a winning commitment with a proof that it is an active valid bid whose hidden amount is at least every other active valid bid; no winning or losing amount is published. The contract can accept that proof directly or verify it after a challenge, then atomically delivers the NFT to the winner, creates the seller’s private USD payout, and returns every losing bid through a fresh recovery commitment. The Class A asset and USD outputs remain in separate trees.

The identity ceremony is deliberately sequential: `sk_spend` appears first, followed by `H(sk_spend) → pk_spend`; only then does `sk_view` appear, followed by the ML-KEM operation that produces `pk_view`. Private keys never enter the registry. The roster’s exact public-key values are reused across every protocol registry. Contract deployment, auditor configuration, registrations, ledgers, and protocol actions remain independent per protocol.

Progress is stored in `sessionStorage`. Routes such as `#/retail`, `#/dvp`, and `#/auctions` resume the corresponding protocol, while action routes such as `#/dvp/seller-holdings`, `#/dvp/settlement`, and `#/dvp/audit` open one specific walkthrough screen. “Reset protocol” keeps the global identity; “Reset entire demo” clears everything in the current browser session.

## Audit access

Every participant uses long-term auditing by default. After public-key registration, a separate operation creates an ML-KEM envelope for the participant’s view secret and shares it with the configured auditor, allowing that auditor to inspect later transactions involving the participant.

An additional regulator has a separate public key and no standing access. In protocols that demonstrate selective disclosure, a participant can share a symmetric note-data key with that regulator for one chosen encrypted payload only. DvP instead demonstrates the long-term auditor path already established during registration: its audit screen compares what an outsider, one ordinary participant, and the auditor can open after settlement. The outsider sees only commitments and updated roots; a participant opens only its own received note; the auditor uses the registered encrypted `sk_view` envelopes to open both encrypted output notes. No second disclosure action is required. Each leg appends encrypted `salt`, `token_id`, and `amount` data alongside `H(pk_spend, salt, token_id, amount)`.

Neither access path reveals spend keys or grants spending, freezing, or administrative authority. Auctions also generate a separate auctioneer ML-KEM keypair for bid decryption and bind it to one auction reference; it is not a participant, auditor, or regulator key. The auctioneer may inspect and compare bids after the timeout but cannot spend them or choose a different commitment without violating the highest-valid-bid proof.

## Traffic and commitment leaves

Retail and DvP include an optional traffic switch. Traffic is unavailable until setup and registration finish and is off by default. In DvP, the switch is embedded directly in both shielding steps so the audience can watch ordinary background shielding transactions append commitments while the seller or buyer prepares its own note. The interval is 3.4 seconds. The active shielding screen scopes traffic to that asset, so cash traffic changes only the cash tree and bond traffic changes only the bond tree. With traffic off, no background state changes occur.

Every asset has an independent commitment tree and root, and every tree starts empty. Shielding may be submitted repeatedly in user-selected partial amounts; each submission creates a distinct note and leaf in that asset’s tree. The note inventory preserves its salt, token identifier, amount, commitment, tree position, source transaction, and lifecycle status. Every leaf records a `sourceTxId` in scenario state. The shielding screens keep every unspent leaf controlled by the executing participant highlighted in purple, independently of the subtler newest-insertion path. Background leaves remain neutral. The demo never seeds anonymity users, inserts filler deposits, or claims those deposits provide privacy. Fixed-shape envelopes and recipient scanning candidates are protocol presentation details, not tree leaves.

The DvP walkthrough has no trailing network-traffic or public-chain stages. Traffic appears only inside the pre-trade shielding screens where it gives the tree animation context. The post-settlement audit screen contains the deliberately opaque public perspective. Unshielding appears afterward as an explicitly optional lifecycle action, not as a required part of DvP settlement.

The public explanation is: a zero-knowledge proof establishes knowledge of an opening and membership of some leaf without revealing which leaf. Cryptographic verification is outside the scope of this front-end walkthrough.

## Structure

```text
enygma_demo/
├── index.html          Small static shell
├── styles.css          Shared Rayls light-mode design system
├── js/
│   ├── app.js          Router, rendering, and interactions
│   ├── config.js       Protocol manifests and roster
│   └── demo-engine.js  Serializable future-backend adapter
└── tests/              Playwright behavior and source checks
```

`demo-engine.js` exposes asynchronous operations named `deploy`, `generateAuditorKey`, `configureAuditor`, `generateIdentity`, `registerParty`, `sharePartyViewKey`, `executeProtocolAction`, `shareTransactionKey`, and `setTraffic`. A future backend can implement the same boundary without changing the experience layer.

## Design system

The demo is light mode only. Shared tokens use Rayls purple `#5B4BE0`, auditor yellow `#B87708` with `#FBEDD3`, white surfaces, near-black `#171922` text, `#F4F5F9` background, and `#E3E5EF` borders. Success and error colors are limited to semantic feedback. Keyboard focus, reduced motion, mobile layouts, and desktop layouts are covered by the shared CSS and behavior tests.

## Run locally

ES modules must be served over HTTP:

```sh
cd enygma_demo
python3 -m http.server 4173
```

Open `http://127.0.0.1:4173/#/choose`.

Run tests with:

```sh
npm install
npm test
```

The test runner starts its own local static server and launches Playwright Chromium. Set `CHROMIUM_PATH` only when using a system Chromium binary.

## Cloudflare Pages

Create a Pages project for this repository with:

- Repository root: repository root
- Build command: `exit 0`
- Build output directory: `enygma_demo`
- Runtime environment variables: none

This is a static HTML deployment; there is no Functions directory or runtime backend.

# Enygma browser demo

This directory contains a polished, browser-native walkthrough of four Enygma protocol experiences: institutional payments, retail payments, delivery versus payment (DvP), and sealed-bid auctions.

The walkthrough follows the protocol’s key generation, registration, private transfers, recipient discovery, and auditing flows.

## Protocol primitives

The registration screen includes a compact primitive reference, and each operation names the primitives it uses. `js/config.js` holds the shared vocabulary.

| Operation | Primitive | Protocol source |
| --- | --- | --- |
| Institutional spend public key | `Poseidon(sk, sk) mod ℓ` | `enygma_payments/go_client/cmd/register_bank/main.go` |
| Institutional balance commitments | Pedersen on BabyJubJub, `C = v·G + r·H` | `enygma_payments/go_client/internal/curve/curve.go` |
| Note spend public key | `Poseidon(sk)` | `enygma_dvp/src/core/utils.go`, `NewSpendKeyPair` |
| View keys and encapsulation | ML-KEM-768 | `enygma_dvp/src/core/utils.go`, `NewViewKeyPair` and `Encapsulate`; `enygma_payments/go_client/agreement/manager.go` |
| Note commitment | `Poseidon(pk_spend, salt, amount, token_id)` | `enygma_dvp/gnark_circuits/primitives/Erc20Commitment.go` |
| Note salt and key | HKDF-SHA256, labels `note salt` and `encryption key` | `enygma_dvp/src/core/utils.go` |
| Note payload | AES-256-GCM | `enygma_dvp/src/core/utils.go`, `EncryptPayload` |
| DvP swap payload | ChaCha20-Poly1305 | `enygma_dvp/src/core/utils.go`, `EncryptSwapPayload` |
| Retail channel key and payload | HKDF-SHA256 (`enygma-channel-key-v1`) and AES-256-GCM | `enygma_retail_payments/private_tags/src/tag.go` and `channel.go` |
| Retail private tag | `Poseidon(block_number, pk_spend, ss_field)` | `enygma_retail_payments/private_tags/src/tag.go` |
| Note trees | `Poseidon(left, right)` | `enygma_dvp/src/core/merkle.go` |
| Zero-knowledge proofs | Groth16 over BN254 | Protocol gnark circuit handlers |

Copy describes the protocol directly. Avoid environment disclaimers in headings, tooltips, receipts, and explanations.

## Deployment contract mapping

Deployment cards follow every contract creation in the repository deployment scripts, in order. Each card shows its role, instance name, constructor dependencies, and linked libraries. The operation then follows the corresponding initialization sequence, with a receipt for each call. Completed deployment addresses remain available from setup and registration.

| Product | Contracts | Initialization calls | Deployment source | Initialization source |
| --- | ---: | ---: | --- | --- |
| Institutional | 2 | 2 | `enygma_payments/run_scripts/deploy_node.js` | `enygma_payments/demo/main.go` |
| Retail | 12 | 8 | `enygma_retail_payments/scripts/deploy.go` | `enygma_retail_payments/scripts/init.go` |
| DvP | 16 | 41 | `enygma_dvp/scripts/deploy.go` | `enygma_dvp/scripts/init.go` |
| Auctions | 8 | 9 | `enygma_dvp_auctions/scripts/deploy.go` | `enygma_dvp_auctions/scripts/init.go` |

Retail includes `RaylsERC20`. DvP includes `RaylsERC20`, `RaylsERC721`, and `RaylsERC1155`, followed by four vaults and two `AssetGroup` instances. Auctions deploys separate `NftVault` and `UsdcVault` instances of `AuctionCoinVault`. Institutional deploys `Enygma` with the Node script’s default epoch interval of one block and `Verifier` from `EnygmaVerifier.sol`.

Initialization registers the configured circuit verification keys in order, connects the payment coordinator and vaults, configures DvP asset groups and allowed pairs, and grants the auction coordinator its role on both auction vaults. The DvP `addEnygma` call references an existing institutional deployment. Contract creation and initialization are displayed as separate operations.

`tests/deployment-test.cjs` checks the manifest against contract-creation calls in the deployment scripts, initialization method order, and the circuit lists in the protocol configuration files. It also verifies unique instance addresses, constructor/library bindings, receipt ordering, and restoration after reload.

## Unified setup

Every protocol follows the same ordered setup:

1. The system operator deploys each contract in script order, then initializes and connects the suite.
2. The auditor runs ML-KEM-768 KeyGen in its private workspace. Both outputs appear together: the secret key stays with the auditor and the public key goes to the operator. The keypair remains visible until the visitor continues to public-key registration.
3. The operator registers the auditor public key in the protocol configuration.
4. The visitor completes the personal key ceremony in strict order: spend secret key, spend public key, view secret key, view public key—then confirms the global Enygma identity or reviews and reuses its secrets with the selected protocol’s spend-key derivation.
5. The visitor first registers both public keys, then separately encrypts and shares the view key with the auditor. A “Register others” action visibly performs the same two operations for each of the nine additional parties. The registry table exposes every participant name, spend public key, view public key, key-registration status, and auditor-sharing status; full keys can be expanded inline.

Each setup screen includes an explicit **Executing entity** panel so the audience can distinguish operator actions, auditor actions, participant-controlled key generation, and per-party registration.

Setup and protocol execution are deliberately separated. Registration occupies its own screen; once it completes, the setup workspace is removed and the protocol walkthrough begins. The walkthrough renders exactly one current operation at a time—with explicit Previous and Next navigation—and never combines the registration table and protocol action controls into one dashboard.

The public participant registry remains visible beside every protocol action and always shows each registered name, spend public key, and ML-KEM-768 view public key. Protocol screens read directly from that on-chain registry instead of turning a public-key lookup into a separate transaction or a “retrieve” button.

The Retail walkthrough first establishes a directional private-tag channel from the payer to a selected registry row. The setup table separates the payer’s private recipient selection from the bitmap published on-chain and visualizes all four protocol modes: **No privacy** sets only the recipient bit; **Subset** sets the recipient plus `⌊√N⌋` decoys; **Rift** sets every row except explicit exclusions while always retaining the recipient; and **Full privacy** sets every registered row. The payment view then shows how the selected row’s `pk_spend` enters the recipient commitment and `pk_view` protects the encrypted note data. The private tag is published with the channel bitmap. During discovery, rows outside the bitmap skip the channel, included decoys fail authenticated decryption, and the intended recipient opens the note with `sk_view` and accepts it only after recomputing the commitment.

The DvP walkthrough is explicitly bilateral. Boreal Markets acts as seller and Atlas Bank as buyer. Issuance and shielding amounts are editable, so each party can shield all or only part of its chosen public allocation before negotiation. Each shielding submission creates a separate private note with its own fresh salt, commitment, and asset-tree leaf; earlier notes remain visible and independently tracked. The seller’s proposal selects one unspent security note and one unspent cash note, making the committed token and amount on each input leaf the exact terms of the exchange. On one dedicated approval screen, the buyer first accepts that term sheet and then initiates it: initiation creates a unique transfer ID and terms commitment and submits the buyer’s encrypted cash leg. If the seller submits the matching encrypted security leg in time, the second leg triggers atomic settlement and creates the two recipient output commitments; otherwise the buyer’s cash note returns to an unspent state after timeout.

The Auctions walkthrough is organized around one named `Class A Share Auction`. The auctioneer first generates a dedicated ML-KEM-768 bid-decryption key and the operator binds its public key to that exact auction reference. A share issuer mints one non-fungible Class A certificate representing a user-selected number of shares to Boreal Markets; the seller then locks the entire certificate into an auction commitment and chooses the bidding timeout. Bidders see the auction reference, asset type, and remaining time but not the certificate ID, represented quantity, or private metadata. The operator separately funds the visitor with USD, and the visitor shields a chosen amount into a distinct private note. A sealed bid can spend any amount up to that note's value: its input leaf is consumed, the chosen amount is locked as the bid, and any remainder becomes a fresh participant-controlled change leaf. Additional registered bidders contribute opaque commitments before the timeout closes submissions.

After the bidding deadline, the auctioneer opens all active bids with its auction-specific secret key and the auditor can inspect the same bids through the registration-time view-key envelopes. Public observers continue to see only commitments. The private review highlights the highest valid bid, then a compact circuit view shows the private openings, commitment-validity checks, pairwise amount constraints, and the public proof output. The auctioneer announces a winning commitment with a proof that it is an active valid bid whose hidden amount is at least every other active valid bid; no winning or losing amount is published. The contract can accept that proof directly or verify it after a challenge, then atomically delivers the certificate to the winner, creates the seller’s private USD payout, and returns every losing bid through a fresh recovery commitment. The settlement screen defaults to a neutral public-chain perspective with no ownership annotations; explicit wallet and auditor perspectives reveal only the additional information available to that role. The Class A asset and USD outputs remain in separate trees.

The identity ceremony is deliberately sequential: `sk_spend` appears first, followed by the protocol’s Poseidon derivation of `pk_spend`; only then does `sk_view` appear, followed by ML-KEM-768 key generation. Retail, DvP, and auctions use `Poseidon(sk_spend)`. Institutional payments use `Poseidon(sk_spend, sk_spend) mod ℓ`, where ℓ is the BabyJubJub subgroup order. The same participant spend secret and ML-KEM-768 view keypair are reused; the registry receives the spend public key for its protocol. Private keys never enter the registry. Contract deployment, auditor configuration, registrations, ledgers, and protocol actions remain independent per protocol.

Progress is stored in `sessionStorage`. Routes such as `#/retail`, `#/dvp`, and `#/auctions` resume the corresponding protocol, while action routes such as `#/dvp/seller-holdings`, `#/dvp/settlement`, and `#/dvp/audit` open one specific walkthrough screen. “Reset protocol” keeps the global identity; “Reset entire demo” clears everything in the current browser session.

## Audit access

Every participant uses long-term auditing by default. After public-key registration, a separate operation creates an ML-KEM-768 envelope for the participant’s view secret and shares it with the configured auditor, allowing that auditor to inspect later transactions involving the participant.

An additional regulator has a separate public key and no standing access. In protocols that demonstrate selective disclosure, a participant can share a symmetric note-data key with that regulator for one chosen encrypted payload only. DvP instead demonstrates the long-term auditor path already established during registration: its audit screen compares what an outsider, one ordinary participant, and the auditor can open after settlement. The outsider sees only commitments and updated roots; a participant opens only its own received note; the auditor uses the registered encrypted `sk_view` envelopes to open both encrypted output notes. No second disclosure action is required. Notes use `Poseidon(pk_spend, salt, amount, token_id)`. ML-KEM-768 and HKDF-SHA256 derive the note salt and encryption key; AES-256-GCM protects the token identifier and amount. DvP swap payloads use ChaCha20-Poly1305 and also carry the settlement salt.

Neither access path reveals spend keys or grants spending, freezing, or administrative authority. Auctions also generate a separate auctioneer ML-KEM-768 keypair for bid decryption and bind it to one auction reference; it is not a participant, auditor, or regulator key. The auctioneer may inspect and compare bids after the timeout but cannot spend them or choose a different commitment without violating the highest-valid-bid proof.

## Traffic and commitment leaves

Retail and DvP include an optional traffic switch. Traffic is unavailable until setup and registration finish and is off by default. In DvP, the switch is embedded directly in both shielding steps so the audience can watch ordinary background shielding transactions append commitments while the seller or buyer prepares its own note. The interval is 3.4 seconds. The active shielding screen scopes traffic to that asset, so cash traffic changes only the cash tree and bond traffic changes only the bond tree. With traffic off, no background state changes occur.

Every asset has an independent commitment tree and root, and every tree starts empty. Shielding may be submitted repeatedly in user-selected partial amounts; each submission creates a distinct note and leaf in that asset’s tree. The note inventory preserves its salt, token identifier, amount, commitment, tree position, source transaction, and lifecycle status. Every leaf records a `sourceTxId` in scenario state. The shielding screens keep every unspent leaf controlled by the executing participant highlighted in purple, independently of the subtler newest-insertion path. Background leaves remain neutral. The demo never seeds anonymity users, inserts filler deposits, or claims those deposits provide privacy. Fixed-shape envelopes and recipient scanning candidates are protocol presentation details, not tree leaves.

The DvP walkthrough has no trailing network-traffic or public-chain stages. Traffic appears only inside the pre-trade shielding screens where it gives the tree animation context. The post-settlement audit screen contains the deliberately opaque public perspective. Unshielding appears afterward as an explicitly optional lifecycle action, not as a required part of DvP settlement.

The public explanation is: a zero-knowledge proof establishes knowledge of an opening and membership of some leaf without revealing which leaf.

## Structure

```text
enygma_demo/
├── index.html          Small static shell
├── styles.css          Shared Rayls light-mode design system
├── js/
│   ├── app.js          Router, rendering, and interactions
│   ├── config.js       Protocol manifests and roster
│   └── demo-engine.js  Serializable walkthrough state
└── tests/              Playwright behavior and source checks
```

`demo-engine.js` exposes asynchronous operations named `deploy`, `generateAuditorKey`, `configureAuditor`, `generateIdentity`, `registerParty`, `sharePartyViewKey`, `executeProtocolAction`, `shareTransactionKey`, and `setTraffic`.

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

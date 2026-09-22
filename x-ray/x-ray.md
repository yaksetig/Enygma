# X-Ray Report

> Enygma | 14,002 production Solidity nSLOC | `1e522ca` (`main`) | Hardhat/Go monorepo | 20/08/26

---

## 1. Protocol Overview

**What it does:** Enygma combines shielded token vaults, atomic delivery-versus-payment, confidential institutional balances, private retail registries, and sealed-bid auctions.

- **Users**: Holders deposit public assets, transact with private notes, make confidential payments, or bid in auctions.
- **Core flow**: Off-chain gnark provers create Groth16 proofs whose public signals drive on-chain nullifier and commitment updates.
- **Key mechanism**: Poseidon commitments/Merkle trees for notes plus Pedersen commitments for institutional balances.
- **Token model**: External ERC20/ERC721/ERC1155 custody, custom Rayls tokens, and account-based confidential institutional supply.
- **Admin model**: Owners configure verifiers, VK order, vaults, groups, relayers, issuers, and auctioneers without timelocks.

For a visual overview, see the [architecture diagram](architecture.svg).

### Contracts in Scope

| Subsystem | Key Contracts | nSLOC | Role |
|---|---|---:|---|
| DvP core and vaults | `EnygmaDvp`, `SwapRelayer`, legacy `EnygmaAuction`, verifier, four vault types | 3,355 | Private-note routing, proof dispatch, settlement, custody |
| Rayls tokens | `RaylsERC20`, `RaylsERC721`, `RaylsERC1155` | 294 | Issuance and token metadata |
| Standalone auction | `EnygmaAuction`, `AuctionCoinVault`, verifier | 821 | Sealed bidding and optimistic settlement |
| Institutional ledger | `Enygma`, `CurveBabyJubJub`, generated transfer verifiers | 9,317 | Confidential balances, supply, bridge proofs |
| Retail registries | user, tag, and channel registries | 215 | Key and encrypted-message publication |

Generated verifier artifacts are counted because their fixed public-input ABIs are security-critical integration dependencies. Interfaces, mocks, tests, vendored dependencies, `enygma_demo/**`, and nested demo directories are excluded.

### How It Fits Together

The core trick: public contracts update opaque commitments and irreversible nullifiers only after a zero-knowledge proof establishes ownership and value conservation.

### Shielded deposit and withdrawal

```text
User → ERC20/721/1155 vault
     ├─ transfer public asset into custody
     ├─ Poseidon commitment calculation
     └─ append commitment to Merkle tree

User → withdraw receipt → verifier → nullifier update → public asset transfer
```

### Atomic DvP

```text
Initiator → EnygmaDvp.submitPartialSettlement()
          ├─ validate receipt
          └─ store pending leg + lock input
Counterparty → matching leg → validate both → nullify both → insert both outputs
```

### Institutional payment

```text
Institution → Enygma.transfer()
            ├─ generated Groth16 verifier delegatecall
            ├─ bind active balance commitments
            ├─ consume nullifier
            └─ apply Pedersen commitment deltas at epoch slot
```

### Private auction

```text
Seller → lock NFT note → bidders lock payment notes
Auctioneer → proven batch winners → optimistic final claim
           ├─ challenge verifies final proof immediately
           └─ expiry finalizes stored claim
Settlement → nullify winner inputs → insert NFT and seller payout notes
```

---

## 2. Threat & Trust Model

### Protocol Threat Profile

> Protocol classified as: **Bridge/settlement** with **auction and confidential-ledger** characteristics

Proof systems and relayer/vault boundaries dominate the asset-custody risk; lifecycle and accounting transitions dominate the auction and institutional subsystems.

### Actors & Adversary Model

| Actor | Trust Level | Capabilities |
|---|---|---|
| DvP owner | Trusted | Instantly configures verifier, VK order, vaults, groups, relayers, auditors, and private minting. |
| Institutional owner | Trusted | Instantly registers accounts/verifiers, mints and burns supply, and changes the bridge dependency. |
| Auction owner | Trusted | Instantly grants auctioneers and changes the challenge window. |
| Authorized relayer | Bounded (settlement route) | Locks/unlocks nullifiers and submits atomic swap/exchange operations; no pause boundary. |
| Auctioneer | Bounded (winner claims) | Submits proven batch results and optimistic final claims. |
| Registered institution | Bounded (valid proof expected) | Submits balance deltas and DvP bridge operations. |

**Adversary Ranking**

1. **Malicious proof submitter** — controls proof encodings, public inputs, receipt counts, statement layouts, and withdrawal recipients.
2. **Relayer/front-running adversary** — observes receipts and competes to reserve nullifiers or control pending-swap metadata.
3. **Compromised operator** — can replace critical proof/bridge configuration instantly without a timelock.
4. **Malicious auction participant or auctioneer** — targets incomplete state/count checks and the honest-challenger assumption.

See [entry-points.md](entry-points.md) for the full permissionless entry-point map.

### Trust Boundaries

- **Proof-to-state boundary** — vault and ledger state assumes verifier integration fails closed; proof return semantics and ABI ordering are decisive (`Verifier.sol:100-116`, `Enygma.sol:447-502`).

- **Relayer-to-vault boundary** — registered relayers can request locks using receipt-controlled tree/nullifier indices (`EnygmaDvp.sol:531-544`).

- **Admin-to-verifier boundary** — owner changes execute immediately; no timelock separates configuration from proof acceptance.

- **Auctioneer-to-settlement boundary** — final standalone claims are trusted after a challenge window without mandatory verification (`EnygmaAuction.sol:434-559`).

### Key Attack Surfaces

- **Verifier boolean semantics** &nbsp;&#91;[X-1](invariants.md#x-1), [E-1](invariants.md#e-1)&#93; — `GenericGroth16Verifier.sol:120-131` returns false on a failed pairing while multiple DvP callers discard the result; trace every state-changing consumer.

- **Receipt-driven nullifier locking** &nbsp;&#91;[X-4](invariants.md#x-4), [E-3](invariants.md#e-3)&#93; — `SwapRelayer.sol:74-112` and `EnygmaDvp.sol:531-544` separate lock acquisition from receipt validation.

- **Institutional epoch/identity bookkeeping** &nbsp;&#91;[I-2](invariants.md#i-2), [X-2](invariants.md#x-2), [E-2](invariants.md#e-2)&#93; — `Enygma.sol:737-870` uses caller account IDs for proof binding and snapshot writes across two rollover paths.

- **ERC1155 metadata and cap writers** &nbsp;&#91;[I-5](invariants.md#i-5), [I-6](invariants.md#i-6)&#93; — `RaylsERC1155.sol:147-276` has registration, component retyping, and supply writers with asymmetric guards.

- **Legacy auction lifecycle authority** — `enygma_dvp/contracts/core/contracts/EnygmaAuction.sol:442-719` retains commented authorization, state, time, winner-state, and proof-count checks.

- **Optimistic settlement availability assumption** — `enygma_dvp_auctions/contracts/core/contracts/EnygmaAuction.sol:514-559` has divergent verified-challenge and unverified-expiry paths worth tracing against deployment monitoring.

### Protocol-Type Concerns

**As a bridge/settlement protocol:**

- Generated verifier input widths and registration order are runtime protocol state (`Enygma.sol:447-502`, `Verifier.sol:66-97`).
- Tree rollover changes the namespace used for roots and nullifiers (`Merkle.sol:151-220`).

**As an auction protocol:**

- Seller, bidder, auctioneer, challenger, and timeout paths each release or transform locked notes through different lifecycle checks.

### Temporal Risk Profile

**Deployment & Initialization:**

- Verifier, vault, role, VK-order, and circuit-artifact setup spans multiple transactions; only some dependencies use one-shot latches.

**Deprecation:**

- Legacy and standalone auction implementations coexist, so deployments must identify which lifecycle and verifier semantics remain live.

### Composability & Dependency Risks

> **External ERC tokens** — via vault deposit/withdraw functions
> - Assumes: exact transfers, conventional return behavior, no balance rebasing
> - Validates: ownership/approval indirectly; no before/after balance check
> - Mutability: token-specific
> - On failure: revert or unchecked false return depending on token

> **Generated Groth16 verifiers** — via DvP verifier and institutional delegatecalls
> - Assumes: VK/public-input layout exactly matches clients and circuits
> - Validates: pairing/public-field checks in generated code
> - Mutability: owner-selected addresses or ordered VK registry
> - On failure: mixed false-return and revert semantics

> **DvP bridge** — via institutional deposit/withdraw
> - Assumes: configured DvP contract implements the expected vault flow and boolean semantics
> - Validates: selected boolean results; fixed ABI dispatch
> - Mutability: replaceable by institutional owner
> - On failure: transaction reverts

---

## 3. Invariants

> ### 📋 Full invariant map: **[invariants.md](invariants.md)**
>
> - **16 Enforced Guards** (`G-1` … `G-16`)
> - **7 Single-Contract Invariants** (`I-1` … `I-7`)
> - **4 Cross-Contract Invariants** (`X-1` … `X-4`)
> - **3 Economic Invariants** (`E-1` … `E-3`)
>
> Seven inferred/cross-contract/economic blocks are not fully enforced on-chain.

---

## 4. Documentation Quality

| Aspect | Status | Notes |
|---|---|---|
| README | Present | Root and per-subsystem READMEs exist. |
| NatSpec | Present | 1,712 annotations detected, concentrated in generated verifier and newer auction code. |
| Spec/Whitepaper | Present | Protocol descriptions and formal models cover each major subsystem. |
| Inline Comments | Adequate but contradictory | Multiple TODOs explicitly mark security checks as incomplete; legacy/new flows diverge. |

---

## 5. Test Analysis

| Metric | Value | Source |
|---|---:|---|
| Test files | 45 | 42 Go + 3 JavaScript files, non-demo scan |
| Test functions | 233 | 201 Go + 32 JavaScript tests |
| Line coverage | Unavailable | Hardhat coverage did not terminate cleanly in the DvP package |
| Branch coverage | Unavailable | Same tooling limitation |

### Test Depth

| Category | Count | Contracts Covered |
|---|---:|---|
| Unit/integration | 233 | DvP, relayer, gnark circuits, payments, auctions |
| Stateless fuzz | 0 | none detected |
| Stateful fuzz | 0 | no Foundry/Echidna/Medusa harness detected |
| Formal verification executable harness | 0 | prose/Tamarin/Verifpal/Lean artifacts exist, but no Solidity formal runner detected |

### Gaps

- No stateful invariant campaign covers vault conservation, nullifier lifecycles, epoch rollover, or auction transitions.
- No negative test was identified that uses a well-formed on-curve but pairing-invalid proof against every state-changing verifier consumer.
- Cross-layer ABI/key-order compatibility depends on integration tests rather than a generated manifest assertion.

---

## 6. Developer & Git History

> Repo shape: normal development — 592 commits on `main`; DvP source was touched by 16 commits over a 285-day history.

### Contributors

| Author | Commits |
|---|---:|
| Mario Yaksetig | 417 |
| Stephen Yang | 169 |
| omega_searcher | 6 |

### Review & Process Signals

| Signal | Value | Assessment |
|---|---:|---|
| Unique contributors | 3 | Small team |
| Merge commits | 5 of 592 | Most history is direct commits |
| DvP source test co-change | 75% | File co-modification signal, not coverage |

### Security-Relevant Commits

| SHA | Date | Subject | Score |
|---|---|---|---:|
| `327a6ff` | 2026-07-01 | vulnerability fix and clean up | 18 |
| `bb29be0` | 2026-06-08 | fix vulnerability, update keys and verification files | 17 |
| `bd4e520` | 2026-08-03 | formal verification, protocol fee support, demos | 17 |
| `7dd403f` | 2026-04-09 | new update | 13 |
| `4391d1b` | 2026-05-14 | fix circuit input | 11 |

### Technical Debt Markers

The git analyzer found 27 TODO/HACK-style markers in DvP Solidity, including explicit notes for auction access control/state checks, statement sizing, proof conditions, tree-number connectivity, duplicate commitments, receipt counts, and unaudited dispatch (`EnygmaAuction.sol:119-758`, `EnygmaDvp.sol:263-837`, vault files).

### Security Observations

- **DVP source concentration** — Stephen Yang authored 98.5% of additions in the DvP git-analysis scope.
- **High-churn trust boundaries** — access control changed in 12 source-touching commits and fund flows in 10.
- **Late activity** — two DvP source commits landed within the final 30-day history window, both with tests changed.
- **Security TODOs remain live** — several comments describe missing checks in production entry points rather than future features.

### Cross-Reference Synthesis

- **Verifier/vault code combines high churn with X-1** — proof semantics and custody paths deserve the first adversarial review pass.
- **Legacy auction TODOs align with its unrestricted entry map** — commented lifecycle guards are current-branch code, not historical notes.
- **Institutional epoch invariants are newer than the DvP git scope** — repository-wide history should be reviewed separately before release.

---

## X-Ray Verdict

**FRAGILE** — documentation and unit/integration tests exist, but access-control operations are instantaneous and no stateful fuzz or executable invariant suite covers the cross-layer accounting and proof boundaries.

**Structural facts:**

1. 14,002 production Solidity nSLOC across five subsystems, including 6,796 nSLOC of generated bridge verifier artifacts.
2. 45 non-demo test files with 233 detected test functions; no stateful fuzz harness was found.
3. Three contributors and five merge commits across 592 commits on the analyzed branch.
4. Legacy and standalone auction implementations coexist with different authorization and settlement models.

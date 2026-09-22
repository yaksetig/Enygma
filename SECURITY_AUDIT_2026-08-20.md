# Enygma security audit

Date: 2026-08-20<br>
Commit reviewed: `1e522ca0eab4382e3518abd88e405729c453e337` (`main`)<br>
Scope: the repository root, excluding `enygma_demo/**` and every directory named `demo`; generated `artifacts/`, `cache/`, and `node_modules/` were treated as build output rather than reviewed source.

## Executive summary

The current DvP vault stack is **not safe to deploy with assets**. Its shared Groth16 verifier returns a boolean for invalid proofs, but every production vault call site discards that result. A public caller can therefore withdraw escrowed ERC-20, ERC-721, or ERC-1155 assets with a well-formed but pairing-invalid proof. In ERC-20 and ERC-1155 paths, a zero root also skips root/nullifier validation.

This review found:

- 1 Critical issue: system-wide proof bypass and direct vault drain.
- 9 High issues: long-lived nullifier-lock DoS, public token-metadata corruption, legacy auction takeover, three institutional accounting/integration failures, a post-rollover prover/circuit mismatch, insecure proof-service defaults, and an optimistic-auction trust gap.
- 4 Medium issues and several low-severity specification/deployment discrepancies.

If any reviewed DvP vault is deployed with value, the immediate action is to remove exposure and migrate or upgrade before accepting more deposits. Fixing client code does not protect an already-deployed vulnerable vault because the exploit is entirely on-chain.

## Method

The review used the Trail of Bits `audit-context-building`, `entry-point-analyzer`, `guidelines-advisor`, `sharp-edges`, `spec-to-code-compliance`, and `semgrep` skills. Work included:

- architecture, trust-boundary, state-invariant, and external-call mapping;
- a complete source-defined Solidity mutator/access inventory;
- specification-to-code comparison across the root and module protocol documents;
- manual Solidity and Go review, with cross-layer tracing into gnark circuits and generated verifier ABIs;
- OSV dependency scanning and selected Go test execution.

Semgrep was not run because the Trail of Bits workflow requires separate approval of the exact target, mode, and rulesets. Slither was attempted but could not compile the contracts because `solc` is not installed. These limitations are recorded below.

## Findings overview

| ID | Severity | Finding |
|---|---:|---|
| C-01 | Critical | Ignored Groth16 boolean permits forged-proof withdrawals from every DvP vault type |
| H-01 | High | Public `SwapRelayer` can lock attacker-selected nullifiers without proof verification |
| H-02 | High | Public ERC-1155 registration can overwrite existing token metadata and supply accounting |
| H-03 | High | Legacy auction settlement lacks authorization, phase, timing, and loser-count checks |
| H-04 | High | Institutional epoch rollover erases untouched account balances |
| H-05 | High | Institutional `burn` leaves both total-supply representations unchanged |
| H-06 | High | Bundled bridge verifier ABIs do not match the contract call ABI |
| H-07 | High | DvP Go clients and circuits derive different nullifiers after tree rollover |
| H-08 | High, deployment-dependent | Proof services accept private witnesses over unauthenticated HTTP and expose expensive proving work |
| H-09 | High, assumption-dependent | Optimistic auction can transfer the NFT under an unverified recipient commitment if no honest challenger acts |
| M-01 | Medium | Public challenge registration enables ownership-proof front-running/DoS |
| M-02 | Medium | Retail fee proof handlers share mutable witness state across concurrent requests |
| M-03 | Medium | Retail two-input proof response uses an order different from the public witness |
| M-04 | Medium | Institutional “one transaction per bank per block” rule is not enforced |

## Detailed findings

### C-01 — Ignored Groth16 boolean permits forged-proof withdrawals

`IVerifier.verifyProof` returns `bool`. `GenericGroth16Verifier.verify` reverts only when the pairing precompile call fails; for a correctly encoded, on-curve proof that does not satisfy the pairing equation it returns `false`. `Verifier.verifyProof` forwards that result unchanged.

All 19 production calls in the DvP vault/legacy-auction stack discard the return value:

- ERC-20: `enygma_dvp/contracts/core/contracts/vaults/Erc20CoinVault.sol:364-406`
- ERC-721: `enygma_dvp/contracts/core/contracts/vaults/Erc721CoinVault.sol:225-235`
- ERC-1155: `enygma_dvp/contracts/core/contracts/vaults/Erc1155CoinVault.sol:429-468`
- institutional bridge vault: `enygma_dvp/contracts/core/contracts/vaults/EnygmaErc20CoinVault.sol:196-211`
- legacy auction: `enygma_dvp/contracts/core/contracts/EnygmaAuction.sol:225-229`, `323-327`, `465-469`, and `713-718`

The public withdrawal paths then transfer assets after the ignored result. ERC-20 and ERC-1155 receipt checks skip root and nullifier checks when the supplied root is zero, so a forged receipt does not even need to reference a historical root. ERC-721 requires a historical root but never establishes ownership of a leaf under it.

Impact: a caller can drain escrowed fungible tokens and select NFTs/ERC-1155 balances without knowing a valid note opening. An all-zero proof may revert at the precompile, but that does not prevent exploitation: the caller can use valid curve points whose pairing equation evaluates false.

Remediation:

1. Make invalid verification revert in the shared verifier, or require the returned boolean at every call site.
2. Reject zero roots for real inputs and validate receipt shape before indexing.
3. Add negative tests using syntactically valid, on-curve, pairing-invalid proofs. All-zero proof tests are insufficient.
4. Treat existing immutable deployments as compromised and migrate assets/state.

### H-01 — Public relayer can lock arbitrary nullifiers

`SwapRelayer.submitReceipt` is public and forwards its unverified receipt to the authorized `EnygmaDvp.lockReceiptNullifiers` path (`enygma_dvp/contracts/core/contracts/SwapRelayer.sol:74-112`). The DvP function copies tree numbers and nullifiers directly from caller-controlled statement fields without verifying the receipt (`enygma_dvp/contracts/core/contracts/EnygmaDvp.sol:531-544`).

An attacker can pre-lock an observed or predicted victim nullifier under a fresh swap ID and choose a very distant expiry. Duplicate locks are rejected, while only the attacker can cancel their stored leg after expiry. This produces a long-lived spend denial of service.

Remediation: verify the receipt before locking; cap expiries; bind a submission to an authenticated party or signed swap intent; and include the vault in the lock identity.

### H-02 — Public ERC-1155 token metadata overwrite

`RaylsERC1155.registerNewToken` is public (`enygma_dvp/contracts/erc1155/contracts/RaylsERC1155.sol:147-159`). It writes `_metadatas[newTokenId]` before checking whether that ID already exists (`204-213`), so the post-write existence check cannot detect an existing registration. It can also rewrite supplied sub-token types (`187-196`).

For normal tokens, an arbitrary caller can select an existing ID and replace fungibility, cap, decimals, and recorded `totalSupply`, corrupting the constraints used by later owner mints.

Remediation: restrict registration to an issuer/admin role, check existence before any write, validate sub-token transitions, and add overwrite regression tests.

### H-03 — Legacy auction settlement is unrestricted and under-validated

`declareWinner` is public although its comment identifies the auctioneer as caller (`enygma_dvp/contracts/core/contracts/EnygmaAuction.sol:483-489`). Auction state, deadline, winning-bid state, and expected comparison-proof count checks are commented out (`492-507`, `671-685`). An empty comparison array therefore verifies nothing before the function spends locked bid/item notes and creates winner commitments.

Once a bid opening is public, any caller can settle early or omit competing bids. The ignored proof booleans in C-01 make the legacy auction even less constrained.

Remediation: retire or disable the legacy auction in favor of the newer role-gated implementation, or add explicit auctioneer access control plus complete state/time/winner/loser checks.

### H-04 — Institutional epoch rollover erases balances

Accounts are explicitly 1-based. At an epoch transition, `_updateBalancesForTransfer` copies IDs `0..N-1`, omitting account `N` if it is not a participant (`enygma_payments/contracts/enygma/contracts/Enygma.sol:794-838`). `_updateBalances`, used by bridge deposit/withdraw paths, writes only participant accounts before advancing the global `lastBlockNum` (`844-870`).

`getBalance` reads only the active snapshot and maps an absent slot to the neutral commitment (`535-545`). A legitimate transaction crossing an epoch can therefore make untouched balances disappear and break the sum-to-total-supply invariant.

Remediation: centralize epoch advancement in one routine that copies every 1-based account exactly once, then applies participant deltas. Add rollover tests where the last account and multiple non-participants hold non-zero balances.

### H-05 — `burn` leaves total supply unchanged

`mintSupply` updates the Pedersen total-supply commitment and `totalSupplyAmount`; `burn` subtracts from an account but updates neither (`enygma_payments/contracts/enygma/contracts/Enygma.sol:230-247`, `276-307`). A successful non-zero burn leaves both getters stale and causes `check()` to fail when snapshots are otherwise correct.

Remediation: subtract the burned amount from both representations, reject burns exceeding the account balance according to the intended proof/accounting model, and assert the invariant after mint/burn/epoch operations in tests.

### H-06 — Bridge verifier ABI mismatch

The institutional contract types deposit and withdrawal public signals as `[50]` and delegatecalls `verifyProof(uint256[8],uint256[50])`. The bundled generated deposit verifier exposes `[51]`, while `WithdrawVerifier1` exposes `[1]` (`enygma_payments/contracts/enygmaverifier/zkdvp/DepositVerifier.sol:1410-1413`; `WithdrawVerifier1.sol:560-563`). The deposit handler itself serializes 51 public values.

Registering these bundled verifiers yields no matching selector, so deposit and one-split withdrawal revert before state changes. The Go client/relayer layer is also stale: clients post `/relay/deposit` and `/relay/withdraw`, while the current relayer registers neither route.

Remediation: generate ABI types from one circuit manifest, make signal counts compile-time constants shared across Go/Solidity generation, and add deployment tests that call every registered verifier through the exact production selector.

### H-07 — Nullifier mismatch after DvP tree rollover

The active DvP gnark circuits constrain `Poseidon(sk, pathIndex)` (`enygma_dvp/gnark_circuits/primitives/Nullifier.go:8-10`; `templates/DvpInitiator.go:90-92`). Many active Go prover paths instead compute `Poseidon(sk, treeNumber * 2^depth + pathIndex)` (`enygma_dvp/src/core/utils.go:148-164`), including DvP initiator/destination and generic payment paths.

The formulas coincide only in tree 0. After rollover, affected clients construct a public nullifier that the checked-in circuit cannot satisfy, making later-tree notes unspendable through those paths.

Remediation: choose one formula, update every circuit/client/formal model, regenerate PK/VK/verifier artifacts, and test the first and last leaves of tree 0 and tree 1 end to end.

### H-08 — Proof services expose private witnesses and proving capacity

The institutional and retail services use `gin.Default()`, register expensive proof routes without authentication/rate/body-size limits, and bind all interfaces with `Run(":" + port)` (`enygma_payments/gnark-server/pkg/api/server.go:15-27`; `cmd/server/main.go:8-13`; `enygma_retail_payments/gnark_circuits/server/api/server.go:14-23`; `main.go:11-16`). Requests contain spend secret keys, note salts/openings, and shared secrets, and `groth16.Prove` is executed synchronously per request.

If reachable beyond a trusted local host, this enables anonymous CPU/memory exhaustion and exposes spend material over plaintext HTTP to an active network observer. DvP binds loopback but logs full initiator/destination request structs containing spend keys and salts. Handler constructors across modules also ignore PK/VK load errors.

Remediation: bind loopback or a private Unix socket by default; require authenticated TLS for any remote use; apply request-size, concurrency, rate, and server-timeout limits; never log witnesses; and fail startup when keys cannot be loaded. Document whether proving is local-wallet, privacy-node, or shared-service infrastructure.

### H-09 — Optimistic auction trusts an unverified NFT recipient commitment

In the newer standalone auction, `settleOptimistic` stores an unverified final proof/statement. After the challenge window, `finalizeSettlement` applies it without verification (`enygma_dvp_auctions/contracts/core/contracts/EnygmaAuction.sol:434-499`, `547-558`). The USDC payout commitment is bound to the winner's stored `commitB`, but the NFT output commitment at statement index 34 is protected only by the unverified proof (`752-797`).

A malicious auctioneer can direct the NFT to an arbitrary commitment if no honest watcher challenges in time. The interface/docs mention slashing invalid auctioneer claims and invalid challengers, but no bond or slashing mechanism exists in the implementation. This is a high-impact trust assumption rather than an unconditional exploit: safety depends on at least one available, uncensored challenger.

Remediation: verify the final proof on-chain, or implement a complete bonded optimistic protocol with explicit watcher assumptions, slashing, monitoring, bounded windows, and recovery from pending settlement.

### M-01 — Public challenge poisoning

`EnygmaDvp.checkAndRegisterChallenge` lets anyone permanently consume any challenge (`enygma_dvp/contracts/core/contracts/EnygmaDvp.sol:900-912`). Ownership proof paths call it before proof verification. A front-runner can register a disclosed challenge first and cause the legitimate proof transaction to revert.

Remediation: allow only vaults to consume challenges and make consumption atomic with a successful proof, or bind challenges to caller/vault/nonce with a commitment scheme.

### M-02 — Shared witness race in retail fee handlers

The retail `paymentFee` and `paymentRelayerFeePublic` handlers allocate `witness` in the outer handler closure and reassign/mutate it for every request (`enygma_retail_payments/gnark_circuits/server/circuits/paymentFee/handler.go:66-126`; `paymentRelayerFeePublic/handler.go:68-122`). Gin invokes handlers concurrently.

Concurrent requests can interleave witness population, proving, and public-witness creation, producing intermittent failures or a response derived from another/mixed request.

Remediation: allocate a request-local witness inside the returned handler function and add concurrent/race tests.

### M-03 — Two-input response ordering differs from proof witness

The two-input circuit's public order is non-interleaved: all trees, roots, nullifiers, commitments, then address. The response builder instead appends `tree[i], root[i], nullifier[i]` inside one loop (`enygma_retail_payments/gnark_circuits/server/circuits/payment2in/handler.go:145-155`). Its returned array therefore does not match the proof's public witness.

Remediation: serialize from the actual public witness or one shared canonical statement builder; add an integration test that submits the exact HTTP response to the on-chain verifier.

### M-04 — Per-bank-per-block rule is not enforced

The institutional specification limits each bank to one transaction per block. The circuit nullifier is derived from a shared secret that changes with the previous balance randomness plus the block/epoch value, while the contract records only the nullifier. A bank can chain a second valid state transition in the same epoch using updated randomness and obtain a new nullifier.

Remediation: enforce a direct `(accountId, block/epoch)` spend marker or derive the nullifier from an immutable account secret and the intended unique time value.

## Dependency review

OSV Scanner found several affected dependency versions. Call analysis highlighted:

- `gnark-crypto` 0.17.0/0.18.0, `GO-2025-4087` (unchecked vector-deserialization allocation), fixed in 0.18.1;
- `go-ethereum` 1.16.8, `GO-2026-4507` and `GO-2026-4511`, fixed in 1.16.9;
- `golang.org/x/net` 0.25.0 findings in the institutional proof server/relayer.

The reviewed application does not directly expose every affected symbol to untrusted input—for example, the visible gnark vector reads load local key artifacts—so these were not promoted above the confirmed code-level issues. Upgrade the dependency families together, regenerate/retest proof artifacts where required, and rerun OSV after the upgrade. `gnark` 0.12.0 also has signature-circuit advisories, but no use of gnark's ECDSA/EdDSA verification packages was found in the reviewed circuits.

## Lower-severity and specification issues

- DvP user registration uses `pkSpend != 0` as its duplicate sentinel, allowing repeated registrations with a zero spend key.
- DvP formal deadline equality, swap-ID derivation, and first-leg output timing disagree with newer code/flow documentation.
- The documented DvP circuit suite is broader than the three circuits in the active generation/server toolchain.
- Retail change-output ownership is stronger than its protocol document; the implementation should be treated as authoritative or the spec updated.
- Tracked development configs contain known local-chain mnemonics/private keys and are labeled demo-only. Keep them mechanically blocked from production configuration.
- DvP initiator/destination proofs have no explicit vault/deployment public input. Replay safety currently depends on root/VK/deployment uniqueness and should be made explicit.
- The retail scanner cursor does not include chain ID, registry address, confirmation depth, or key-set version; reorgs or newly learned historical channels can cause missed records.

## Verification performed

Passing:

- `go test ./...` in `enygma_payments/relayer`
- `go test ./...` in `enygma_payments/gnark-server`
- `go test ./...` in `enygma_retail_payments/relayer`
- `go test ./...` in `enygma_retail_payments/src`
- `go test ./...` in `enygma_retail_payments/private_tags/src`
- `go test ./server/...` in `enygma_retail_payments/gnark_circuits`
- `go test ./...` in `enygma_dvp_auctions/src`

Not clean:

- `go test ./...` in `enygma_dvp/src` runs outside the listener sandbox but fails six current tests: three proof helpers now require recipient encapsulation keys, and three ERC-20 scan tests find zero notes.
- Slither 0.11.5 cannot run because `solc` is absent.
- No node dependency tree is installed, so Hardhat contract compilation/tests were not executed.
- Semgrep OSS was detected, but the binary's local certificate/telemetry initialization also needs correction before an approved scan can run.

## Remediation order

1. Stop asset exposure; fix C-01 everywhere and add pairing-invalid negative tests.
2. Fix H-01 through H-03 and either retire or strictly gate the legacy auction/token surfaces.
3. Repair institutional snapshots, burn accounting, and bridge verifier ABI generation.
4. Reconcile the DvP nullifier formula and regenerate every coupled PK/VK/verifier artifact.
5. Harden proof-service boundaries and make witnesses request-local.
6. Decide whether optimistic auction safety truly assumes an honest watcher; implement the missing economic/operational controls or verify proofs synchronously.
7. Resolve existing DvP test regressions, add epoch/tree-rollover and exact-ABI deployment tests, then run Slither, Semgrep, OSV, and full integration tests in CI.

## Semgrep plan awaiting approval

Recommended mode: **Run all**, because important-only mode drops third-party rules shipped at `INFO` before post-filtering.

Proposed scan scope: an ephemeral copy of this commit containing the repository minus `enygma_demo/**`, every `demo/`, and generated artifact/cache directories. This preserves the requested exclusion without adding a `.semgrepignore` to the working tree.

Engine: Semgrep OSS. Detected source: Go (220 files), Solidity (77), JavaScript (8), Python (1), and shell (4).

Rulesets:

- baseline: `p/security-audit`, `p/secrets`
- Go: `p/golang`
- JavaScript: `p/javascript`
- Python: `p/python`
- third party: Trail of Bits `semgrep-rules`, Decurity `semgrep-smart-contracts`, dgryski `semgrep-go`, elttam `semgrep-rules`, Kondukto `semgrep-rules`, and Apiiro `malicious-code-ruleset`

The scan must not be started until the user explicitly approves this target, mode, and complete ruleset list.

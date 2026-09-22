# Security Audit of `enygma_payments`

**A privacy-preserving inter-bank / CBDC settlement system**

| | |
|---|---|
| **Target** | `enygma_payments/` in the `Enygma` repository |
| **Commit audited** | `1e522ca` ("Rename suite switcher's Payment tab to Institutional Payments") |
| **Audit dates** | 21–22 August 2026 |
| **Auditor** | AI Auditor (Auditician automated audit pipeline) |
| **Methodology** | Auditician: survey → brainstorm → threat model → local (file-by-file) audit → global (focus-area) audit → adversarial validation → report |
| **Report version** | 1 |

> **This audit was performed by AI, and this report was written by AI.** Every finding below was
> re-checked by an independent adversarial validation pass whose explicit instruction was to *refute*
> the finding first, and many were proven by execution. Nonetheless, bugs may have been missed, and
> the absence of a finding in this report is not evidence that a defect does not exist.

---

## 1. Executive summary

This section is written for a risk officer, not a cryptographer.

**The most urgent item is not a cryptographic flaw. It is an operational one.** A private key that
signs settlement transactions on a live public blockchain — Rayls chain id 72957 — is committed in
plaintext in this repository, in a file named `contracts/test`, together with the shared password
("bearer token") that the settlement service uses to authenticate every bank. At the time of
measurement that key's account held roughly **0.717** of the chain's native token and had already
sent **216 transactions**. The same repository also contains, in plaintext, the spending keys of all
six member banks and the cryptographic proving keys needed to produce valid payment instructions. An
attacker whose only capability is *having read this repository* therefore holds everything required
to sign as the settlement relayer, authenticate to it, and — demonstrated by execution during this
audit — construct a completely honest, valid payment instruction that empties any of the six banks'
balances. This requires an incident response today, not a code change (finding **C-06**).

**Beyond that, the one live value-moving path in the system has multiple, independently demonstrated
ways to create money from nothing or take it from another institution.** We confirmed five separate
defects on that path, each sufficient on its own and each with a different root cause, so fixing any
one of them does not fix the others:

- A prover can choose the result of the arithmetic reduction the payment proof depends on, and mint
  an arbitrary amount from a zero balance (**C-01**). Demonstrated end-to-end: a proof produced with
  the repository's own committed proving key was accepted by the repository's own verifier contract
  on a local chain, moving a zero balance to 10²⁴ units while the system's own solvency check still
  reported "OK".
- A prover can present a balance that is numerically larger than reality but opens the *same*
  on-chain record, defeating the "do you have enough money?" test entirely (**C-02**). Demonstrated
  against the deployed verifier bytecode, with three negative controls each failing at a different,
  identified constraint.
- The amounts credited to the *other* participants in a payment are never checked to be positive, so
  a "credit" of a negative amount is a silent debit of another institution's account (**C-03**).
  Demonstrated on a local chain against the real contracts: the victim went from 1000 to 600 and the
  attacker from 0 to 400, with no accomplice, no cost, and the solvency check still returning true.
- Naming one's own account twice in a payment discards one of the two entries whenever the payment
  is the first of a new settlement window — which, at the settings the repository ships, is the
  common case rather than the rare one. The result is that a balance doubles (**C-05**). Demonstrated
  with a real proof, a real verifier and a matched control run that isolates the cause.
- Any member can, in a single ordinary-looking payment that costs it nothing, permanently render
  **every other member's entire balance unspendable**, with no recovery path in the contract
  (**C-04**).

**Is the system safe to deploy with value? No.** Not in its present form and not with these
credentials. Four of the five money-integrity defects above are in the zero-knowledge circuits and
the trusted-setup artefacts; fixing them invalidates the proving and verifying keys and requires
redeploying the verifier contract. One of them (**C-04**) is a design gap in what the payment proof
binds. Separately, the confidentiality the product is built to provide does not hold: who paid whom
can be recovered from public blockchain data alone (**H-01**), and so can the amounts, for any bank
that pays more than once in a settlement window (**H-02**) — both demonstrated by execution against
the real proving server.

**What this audit is not saying.** This is a research prototype implementing a published design, and
it should be judged as one. "Unfinished software exists" is not a finding, and we have not reported
it as one. The finding is narrower and harder: *specific, demonstrated defects exist on the code
path that runs today*, and the project's documentation and publications assert security properties —
multi-party trusted setup, end-to-end payload encryption, scoped regulatory disclosure, formal
verification — that the implementation does not provide. We have also been deliberate about the
difference between a demonstrated compromise and an unfalsifiable trust gap: **H-11** (the origin of
one cryptographic constant cannot be established) and **H-12** (the one-time setup ceremony was
performed by a single unnamed party) are trust gaps, not demonstrated attacks, and we say so in
their write-ups. The audit's fifteen validation downgrades and one outright invalidation are part of
its result, not caveats to it.

**Final counts after validation:** 6 Critical, 9 High, 19 Medium, 20 Low, 8 Info — 62 reported
findings, of which 37 sit on code that executes today.

---

## 2. Scope and method

### 2.1 What was audited

Everything under `audit-target/enygma_payments/` at commit `1e522ca`, with emphasis on
runtime-reachable code:

| Component | Contents |
|---|---|
| On-chain | `contracts/enygma/contracts/Enygma.sol` (1088 lines), `CurveBabyJubJub.sol`, `contracts/enygma/interfaces/**` |
| Vendored contracts | `contracts/utils/**` (OpenZeppelin-derived ERC20 / AccessControl / Ownable), diffed against the upstream tags their headers claim |
| Zero-knowledge circuits | `gnark-server/pkg/circuits/{enygma,enygma_fee,deposit,withdraw}/circuit.go` and their HTTP handlers |
| Trusted setup & keys | `gnark-server/cmd/setup/`, `gnark-server/keygen/`, `gnark-server/utils/`, `gnark-server/poseidon/` |
| Client | `go_client/**` (non-test): curve operations, randomness, proof generation, transaction construction, ML-KEM key agreement, DvP deposit/withdraw |
| Relayer | `relayer/**` (non-test): the settlement service and its HTTP surface |
| Deployment | `run_scripts/deploy_direct.py`, `run_scripts/deploy_node.js`, `hardhat.config.js`, `demo/main.go` |
| Documentation | `README.md`, `protocol_description.md`, `demo_instructions.md`, `formal_methods/` |

Local review was organised into **13 module batches** rather than strictly one file per agent, so
that cross-file reasoning within a module was preserved; these were executed as **10 local-auditor
passes**, three adjacent batch pairs having been reviewed together. **Six global focus areas** (G1–G6)
then looked for emergent defects across module boundaries.

### 2.2 What was not audited line by line

Four classes of **generated artefact** were excluded from line-by-line review, per the audit
instructions. For each, we verified the *interface contract* against every call site — return types,
public-input ordering and count, argument encoding — rather than the generated arithmetic:

1. `contracts/enygma/contracts/{EnygmaVerifier,EnygmaFeeVerifier}.sol` — gnark-generated Groth16 verifiers.
2. `contracts/enygmaverifier/zkdvp/*.sol` and `gnark-server/keys/**/*.sol` — the DvP verifier sets.
3. `go_client/contracts/enygma.go` and `relayer/contracts/enygma.go` — abigen bindings.
4. `gnark-server/poseidon/constants.go` — Poseidon round constants (spot-checked, and
   differentially tested against the `iden3/go-iden3-crypto` reference for widths t = 2, 3, 4).

That interface-contract check is where **M-14** (deposit public-input arity 51 vs 50), **H-16**
(verifier files matching no proving key) and **I-02** (stale binding struct arities) came from, so
the exclusion did not cost coverage of the thing that matters about generated code.

Also out of scope: `node_modules/`, build `artifacts/` and `cache/`, and `*_test.go` files as audit
*targets* — although tests were read freely as evidence of what the developers believe the
invariants to be, and several findings rest on what the test suite does.

The sibling project `enygma_dvp` is **out of scope**. It was read read-only in exactly two places to
settle two questions that would otherwise have left a severity undetermined (**C-09** and **L-12**).

### 2.3 Two limits on what this report can claim

**We cannot say which program is running on chain 72957.** `contracts/enygma/.gitignore` excludes
`artifacts/`, `git ls-files` returns zero tracked build artefacts, and the artefacts present in the
working tree compile an ABI-incompatible *predecessor* of the audited contract (a 50-signal transfer
proof, `registerAccount` with no `viewKey`, no `transferWithFee`). There is no recorded bytecode
hash, no chain id alongside the recorded address, and no verification step anywhere (**L-03**).
Every statement of the form "the deployed X matches Y" in this report is a statement about
*repository artefacts*, not about chain state.

**No network was used for validation.** Chain facts (the relayer account's balance and nonce, the
chain id, the absence of code at the address in `go_client/config/address.json`) are second-hand from
an earlier local-audit run and are flagged as such wherever used. The two key addresses were
re-derived offline and match.

### 2.4 Method, and what "validated" means here

97 raw plausible-issue files were produced by local and global auditing. Consolidation deduplicated
them into **63 findings** (IDs C-01…I-07) covering all 97 inputs. Each finding then received a
**dedicated adversarial validation pass** whose instruction was to *refute the finding first* and to
record the refutation attempts that failed. Where the register and a validation record disagree,
**the validation record governs this report**.

Validation was not a formality. It produced:

- **one merge** — C-07 (`transferWithFee` minting) is not an independent Critical; it is C-02 applied
  to the fee path, and closing C-02 closes it. Reported once, remediated once;
- **fifteen severity changes, every one of them downwards.** The consequential ones: **H-16 High →
  Low** (its forgery half withdrawn entirely, after git history showed the mismatched verifiers are
  the developers' own superseded output), **H-03 High → Low** (it fires on no configuration the
  repository produces), **C-08 Critical → High** and **C-09 Critical → Medium** (four gates each),
  **H-09 High → Medium** (its central claim refuted by the shipped client), **H-10 High → Medium**
  (magnitude wrong by ~125×), **M-02, M-03, M-04, M-10 Medium → Low**, **L-14 Low → Info**, plus
  H-04, H-06, H-07 and H-08 High → Medium. The full list, with reasons, is in §10.3;
- **three reachability re-classifications**, which are separate from severity: **H-03 LIVE → LATENT**
  and **L-02 LIVE → LATENT** downwards, and one upwards — **H-13 LATENT → LIVE** (`burn` is a normal
  issuer operation, not a test-only path). H-13's *severity* did not move; it stayed High;
- **one outright invalidation** — `L9-nonce-gap-stalls-settlement.md`, whose "permanent nonce gap
  halts settlement forever" claim was refuted by reading the pinned `go-ethereum` source: the
  relayer leaves `auth.Nonce` nil, so every submission re-queries the *pending* nonce, and the
  eviction event the finding depended on is precisely what repairs the condition. That file is the
  only one of 97 that moved to `issues/invalid/`;
- **and several strengthenings** — C-01, C-02, C-03, C-05, H-14 and H-15 were each carried further
  than the original filing, from "argued" to "executed against the repository's own committed proving
  keys and verifier contracts on a local chain".

Where a validator corrected the original claim, the correction is stated in the finding. Those
corrections are part of this report's evidence, not footnotes to it.

**Distribution after validation:** 37 findings are on **LIVE** paths, 6 are **LATENT** and 19 are
**BROKEN**. The arithmetic, stated explicitly because the register is internally inconsistent here:
the register's 63 per-entry labels are **38 LIVE / 6 LATENT / 19 BROKEN** (its own summary table says
39 / 6 / 18, which disagrees with its entries — the Info block is the source of the discrepancy, and
the per-entry labels are the ones that were checked). Validation then moved **H-03** and **L-02** from
LIVE to LATENT (−2), moved **H-13** from LATENT to LIVE (+1), and merged **C-07** (LATENT) into C-02,
which removes an entry but no LIVE label. 38 − 2 + 1 = **37 LIVE**, 6 LATENT, 19 BROKEN, over 62
reported findings.

---

## 3. The reachability model — why one path matters more than the rest

Severity in this report is calibrated against a reachability map (`audit-state/globals/G1.md`) built
by reconstructing what an operator following the repository's own runbooks actually stands up. Three
classes are used throughout:

| Class | Meaning |
|---|---|
| **LIVE** | Demonstrably executes today, in the configuration the repository's own tooling and documentation produce. A defect here is exploitable now. |
| **LATENT** | The code is present and reachable in principle, but nothing in the repository wires it up. A defect here becomes exploitable the moment an operator performs a normal, expected setup step — typically one owner transaction. |
| **BROKEN** | Cannot execute at all in its current form: it always reverts, always returns HTTP 400, or always 404s. A defect here is *masked*. **Masked is not the same as absent** — see §6. |

### 3.1 Exactly one value-moving path is live

```
demo/main.go  →  POST /proof/enygma (gnark server, :8080)
              →  POST /relay/transfer (relayer, :8082, bearer token)
              →  Enygma.transfer()  →  EnygmaVerifier.verifyProof(uint256[8],uint256[80])
```

Everything else that could move value is dark:

| Entry point | Class | Why |
|---|---|---|
| `Enygma.transfer` | **LIVE** | The only path the demo and the integration tests drive. `contracts/enygma/contracts/EnygmaVerifier.sol` is byte-identical to `gnark-server/keys/EnygmaVerifier.sol` and reproduces from `EnygmaVk.key`; the whole chain is coherent and was exercised end to end during validation. |
| `Enygma.transferWithFee` | **LATENT** | Every component ships — the function is `external`, the verifier is committed inside the hardhat sources root, the proving and verifying keys are in git, the prover route `POST /proof/enygma_fee` is registered, the relayer route `POST /relay/transfer_fee` is registered, and there is an end-to-end test. Only `addFeeVerifier` is never called outside that test. Enabling fees *is* that one owner transaction. |
| `Enygma.withdraw` | **BROKEN** | `_withdrawVerifiers[*] == address(0)` (`Enygma.sol:455`); `addWithdrawVerifier` has **zero callers anywhere** — not even a test; the six withdraw verifiers live outside the only hardhat sources root and are never compiled; the prover route returns HTTP 400 on every request; and `addZkDvp` likewise has zero callers. |
| `Enygma.deposit` | **BROKEN twice over** | No deposit verifier is ever registered, *and* `Enygma.sol:500` emits the selector for `verifyProof(uint256[8],uint256[50])` (`0x18e2c03f`) while both committed `DepositVerifier.sol` files declare `uint256[51]` (`0xcdae3e76`). No fallback function exists, so the call reverts unconditionally. |
| `burn` | **LIVE (owner)** | Owner-only; its only caller in the tree is a test, but it is a normal issuer operation on any real deployment. |
| The whole DvP bridge | **BROKEN** | Four independent reasons, listed in §6.2. |

### 3.2 Consequences for how this report should be read

- The **six Criticals are all on the LIVE transfer path or in committed credentials.** They are not
  hypothetical and not conditional on future wiring.
- **C-08, C-09, H-14, H-15, H-16, M-14, M-15, M-16 are on BROKEN paths.** They harm nobody today.
  They are reported because the actions that arm them are routine and because several of them are
  *masked by defects that look trivial* — see §6.
- **Two findings turn on a single unread on-chain value.** If `_feeVerifier` is non-zero on the live
  contract, the fee path is live and C-02's fee instance becomes immediately exploitable. If
  `_totalRegisteredParties` equals the highest registered bank id, H-03 moves from Low back toward
  High. Both are one storage read for an operator; neither could be answered offline.
- **Epoch rollover is the common case, not an edge case.** Every state-changing function ends with
  `lastBlockNum = _currentEpochStart()`. `deploy_direct.py` defaults `EPOCH_INTERVAL = 30`, which at
  the developers' own estimate of ~0.5–1 s Rayls block times is a 15–30 second window; at the demo's
  advertised `epochInterval = 1` every transaction crosses a boundary. Several findings (C-05, H-03,
  H-15) are conditioned on rollover, and none of them should be discounted on that basis.

---

## 4. Findings overview

Ordered by severity, then by exploitability (LIVE before LATENT before BROKEN), then by how easily
the defect can be reached. **Severities are the post-validation ones**, which differ from the
pre-validation register in fifteen places (plus three reachability re-classifications and one merge);
where they differ, the change is stated in the finding.

| ID | Severity | Class | Title |
|---|---|---|---|
| **C-06** | Critical | LIVE | A live, funded relayer private key, the shared bearer token, and all six bank spending keys are committed in plaintext |
| **C-01** | Critical | LIVE | Unconstrained modular-reduction quotient lets a prover choose the result of every `mod P` reduction — unlimited minting |
| **C-02** | Critical | LIVE | Balance scalars are bounded at 252 bits but consumed modulo a 251-bit order, so `b + 2P` opens the same commitment and defeats the solvency proof *(absorbs C-07, the fee-path instance)* |
| **C-03** | Critical | LIVE | Non-sender transfer amounts have no range proof, so a "credit" of a negative amount debits any other institution |
| **C-05** | Critical | LIVE | Duplicate account ids in one payment silently discard a debit at epoch rollover, doubling a balance |
| **C-04** | Critical | LIVE | Recipients' shared secrets are free witnesses: any member can permanently freeze every other member's entire balance, for free |
| **H-13** | High | LIVE | `burn()` is an unproven plaintext subtraction on a hidden balance; over-burning wraps a debt into a ~2²⁵⁰ spendable balance |
| **H-01** | High | LIVE | Sender anonymity collapses from k=6 to k=1 for a passive chain observer, by two independent protocol-level mechanisms |
| **H-02** | High | LIVE | Epoch-constant, direction-symmetric blinding factors open an entire settlement window's payment graph in plaintext |
| **H-05** | High | LIVE | The demo server — the only working end-to-end client — exposes unauthenticated owner-privileged routes on all interfaces |
| **H-11** | High | LIVE | The Pedersen value generator `G` has no verifiable provenance *(trust gap, not a demonstrated compromise)* |
| **H-12** | High | LIVE | The Groth16 trusted setup is a single `groth16.Setup` call on one machine; the specification mandates an MPC ceremony *(trust gap)* |
| **C-08** | High | BROKEN | The withdraw circuit has no solvency comparator at all — the only one of four circuits without one |
| **H-14** | High | BROKEN | `withdraw()` keys its verifier on `depositParams.length`, so empty state arrays void every binding |
| **H-15** | High | BROKEN | `_updateBalances` performs no epoch propagation, destroying every non-participant's balance |
| **H-04** | Medium | LIVE | Spend keys and every blinding factor are POSTed in cleartext to an unauthenticated proving service bound to all interfaces |
| **H-06** | Medium | LIVE | One static shared bearer token authenticates every bank, over plaintext HTTP, with the value published |
| **H-07** | Medium | LIVE | Account id 0 and in-range unregistered ids are accepted as payment participants — an ownerless value sink |
| **H-08** | Medium | LIVE | Contract ownership is `private immutable` with no transfer, renounce, pause, upgrade — or even a getter |
| **H-09** | Medium | LIVE | The relayer is an undocumented single trusted intermediary that can censor and totally order every payment |
| **H-10** | Medium | LIVE | Any token holder can drain the relayer's funded account, or starve its global mutex, and halt all settlement |
| **M-01** | Medium | LIVE | Proof verification uses `delegatecall` with no code check; a codeless verifier address makes every proof "valid" |
| **M-05** | Medium | LIVE | Exact `lastBlockNum` equality lets any participant invalidate every outstanding proof for one transaction's cost |
| **M-06** | Medium | LIVE | `registerAccount` has no guards of any kind; a repeat call destroys a bank's balance and double-counts supply |
| **M-07** | Medium | LIVE | `deploy_direct.py` defaults to Rayls **mainnet** while the documented procedure says local Hardhat |
| **M-08** | Medium | LIVE | Proving server: no auth, no limits, a permanent goroutine leak on every malformed request, remote panics |
| **M-09** | Medium | LIVE | Relayer: no HTTP timeouts, no body limit, and a global mutex held across `WaitMined` |
| **M-12** | Medium | LIVE | The documented encryption, key-rotation, retrieval and auditing layers do not exist — and could not deliver the promise if built |
| **M-11** | Medium | LATENT | ML-KEM layer: no leader, implicitly-rejected ciphertexts accepted, peer view keys silently manufactured |
| **M-13** | Medium | LATENT | `transferWithFee` never reads signals 50–53, so the fee is destroyed with no recipient |
| **C-09** | Medium | BROKEN | `withdraw()` forwards caller-chosen `depositParams` to the DvP contract with no binding to the proof |
| **M-14** | Medium | BROKEN | Deposit public-signal arity 51 vs 50 — and slot 50 stays unread even after the obvious fix |
| **M-15** | Medium | BROKEN | The bridge has no supply accounting, and both circuits move value in the wrong direction |
| **M-16** | Medium | BROKEN | `WithdrawVerifier1..6` all prove the identical statement: six trapdoors for one constraint system |
| **M-02** | Low | LIVE | The demo's "ML-KEM key agreement" performs no key exchange; its fabricated blobs are registered on-chain as view keys |
| **M-03** | Low | LIVE | The withdraw handler leaves four witness fields unassigned, so all six prover routes return 400 unconditionally |
| **M-04** | Low | LIVE | The nullifier implements neither the specified double-spend control nor any other |
| **L-01** | Low | LIVE | Nothing binds a proof to a chain id or a contract address, and every fresh deployment has an identical genesis state |
| **L-03** | Low | LIVE | No reproducible path from source to deployed bytecode; artefacts untracked, stale and ABI-incompatible |
| **L-04** | Low | LIVE | Proving-key load errors silently discarded; verifying keys never used; nothing binds a deployed verifier to a key |
| **L-05** | Low | LIVE | Relayer performs no numeric range validation and zero-pads short signal arrays on one route |
| **L-06** | Low | LIVE | The README's post-quantum migration claim does not achieve what it states; published figures understate the circuit |
| **L-07** | Low | LIVE | `formal_methods/` claims three tools prove the system; one artefact is missing, one is axioms, one models classical DH |
| **H-03** | Low | LATENT | Off-by-one carry-forward loop against 1-based ids, plus `(0,0)` as an absorbing element |
| **L-02** | Low | LATENT | `initialize()` after `registerAccount` breaks the supply invariant; `registerAccount` lacks `whenInitialized` |
| **M-10** | Low | LATENT | The spend key and the balance opening are passed as command-line arguments |
| **H-16** | Low | BROKEN | Eight zkdvp verifiers in `contracts/` match no proving key — stale copies, fail-closed *(forgery half withdrawn)* |
| **L-08** | Low | BROKEN | The documented `go_client` CLI can never produce a valid proof — and it is the mask on four other findings |
| **L-09** | Low | BROKEN | Proof-generation failure is never detected; an all-zero proof is forwarded as if it had succeeded |
| **L-10** | Low | BROKEN | The CLI submits 0-based circuit indices as on-chain account ids; the contract would accept them |
| **L-11** | Low | BROKEN | One nullifier set is shared by four circuits; fee, withdraw and deposit collide with each other |
| **L-12** | Low | BROKEN | External calls precede state updates in `withdraw()` and `deposit()`; no reentrancy guard anywhere |
| **L-13** | Low | BROKEN | zkdvp clients derive every blinding factor from constants checked into the repository |
| **L-15** | Low | BROKEN | Two different points named "G", and dead commitment helpers that use the wrong one |
| **I-01** | Info | LIVE | Generator `H` **is** correctly NUMS-derived; only the source comment ("randomnly generated") is wrong |
| **I-04** | Info | LIVE | `curve.GetNegative(0)` returns `P` instead of `0`; a second copy is wrong in the opposite direction |
| **I-05** | Info | LIVE | Base-0 vs base-10 parse divergence between the witness and the returned public signal |
| **I-03** | Info | LATENT | Poseidon constant tables disagree on supported widths; `t = 5` panics in one of four |
| **L-14** | Info | BROKEN | Vendored OpenZeppelin `ERC20.sol` has an added uncapped `mint()` — but nothing anywhere inherits it |
| **I-02** | Info | BROKEN | abigen bindings declare struct arities contradicting their own embedded ABI |
| **I-06** | Info | BROKEN | The CLI's transfer deltas are a hardcoded array; the `<value>` argument is ignored |
| **I-07** | Info | BROKEN | Client config accepts an empty or garbage contract address and never binds it to a network |

**Totals:** Critical 6 · High 9 · Medium 19 · Low 20 · Info 8 = **62**.
LIVE 37 · LATENT 6 · BROKEN 19.

---

## 5. Detailed findings

### 5.1 Critical

---

#### C-06 — A live, funded relayer private key, the shared bearer token, and all six bank spending keys are committed in plaintext

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**Description.** `enygma_payments/contracts/test` is not a test. It is a thirteen-line operator
runbook containing two copy-pasteable relayer invocations. The first is sanitised; the second is not.

```
contracts/test:1   RELAYER_RPC_URL=https://mainnet-rpc.rayls.com \
contracts/test:2   RELAYER_CHAIN_ID=72957 \
contracts/test:3   RELAYER_PRIVATE_KEY=$MY_KEY \          <-- sanitised
contracts/test:4   RELAYER_API_KEY=$MY_API_KEY \          <-- sanitised
contracts/test:5   RELAYER_GAS_LIMIT=10000000 \
contracts/test:6   ./run.sh
contracts/test:7
contracts/test:8   cd enygma_payments/relayer
contracts/test:9   RELAYER_PRIVATE_KEY=b30e25be…36f58 \   <-- NOT sanitised
contracts/test:10  RELAYER_API_KEY=enygma-test-secret     \
contracts/test:11  RELAYER_RPC_URL=https://mainnet-rpc.rayls.com \
contracts/test:12  RELAYER_CHAIN_ID=72957                 \
contracts/test:13  go run main.go
```

Somebody sanitised one invocation and forgot the other.

**Affected code.**
`contracts/test:1-13` (relayer signing key and bearer token);
`demo/main.go:62` (`defaultRelayKey = "enygma-test-secret"`), `demo/main.go:64, 73-75`
(`bankSks = {424242, 1, 2, 3, 4, 5}`);
`demo/index.html:1024-1029` and `demo/bank.html:730-735` (the same six spend keys served to the
browser);
`go_client/enygma_test/transaction_test.go:225, 232-236, 312-315`;
`demo/run.sh:5`;
and — not in the original filing — **`enygma_payments/demo/demo`, a git-tracked 14 MB compiled Go
binary that embeds the owner key, the bearer token and the bank spend keys.**

**Evidence.** The address was re-derived offline, with no network:

```
$ cast wallet address --private-key 0xb30e25be...36f58
0xEa8D34E0aAC0308F58b740768C82e2411A38C2b7
```

Chain state (measured in an earlier local-audit run, second-hand here): balance
`717354592000000000` wei ≈ **0.717**, **nonce 216**, on chain 72957 (`eth_chainId = 0x11cfd`, block
≈ 7.44 M). The relayer binary has exactly two transaction-producing code paths, so the overwhelmingly
likely content of those 216 transactions is Enygma transfers or reverted attempts.

The validator then established, by execution, that the committed bank spend key plus the committed
commitment opening is a **complete spending witness** for the production circuit — no soundness bug
required:

```
$ GOPROXY=off go run .
publicKeys[1] (bank 0, sk=424242) = 1002076657430037762004737508147136417897403580796686909438903593217026752784
previousCommit[0] = Com(500,67890) = (15286012236459773897748348014726510653103139388622289233367313104159152986767,
                                      16530541161111203901128109623065495234393326788215620801132297721723000067146)
compiling EnygmaCircuit (k=6) …
20:16:38 INF parsed circuit inputs  nbPublic=80  nbSecret=23
compiled: 94206 constraints
RESULT: SOLVED — committed sk/prevR/prevV is a complete spending witness
        drained 500 of 500 from bank 0 (accountId 1) to bank 1 (accountId 2)
```

`nbPublic=80` matches the contract's `uint256[80] public_signal`, i.e. this is the same circuit
instance `Enygma.transfer` consumes.

The opening `(prevBalance, prevR)` is public on this design by construction: `registerAccount`
(`Enygma.sol:191-217`) takes `randomness` as plaintext calldata, and `mintSupply` (`:230-268`)
commits with blinding **zero** and emits `SupplyMinted(lastBlockNum, amount, recipientId)` in the
clear. The repository even writes the resulting opening down: `Com(500, 67890)` at
`transaction_test.go:232-236`.

**Git history — and a documented failed scrub.**

```
$ git log --all --oneline -- enygma_payments/contracts/test
bd4e520 Formal verification, protocol fee support, and new demo apps … (#3)   2026-08-03
1533c07 adding relayer with fee                                               2026-07-16
3ab07d3 adding relayer and also n block transaction feature                   2026-07-10
```

`3ab07d3` created the file empty; `1533c07` added all thirteen lines including the literal key. **The
key has been in git for about five weeks and is present at HEAD.** The repository has already
demonstrated that working-tree deletion is not remediation:

```
$ git show --stat 3fd294d
2025-11-13 Stephen Yang  "removing demo private key"
-  PrivateKeyString = "[REDACTED_PRIVATE_KEY]"
+  PrivateKeyString = "" // Please insert private key of your walllet
```

Two deliberate removals of the owner key in November 2025 — and it is still at HEAD today in five
other places plus the tracked binary. There is no secret scanner: no `.gitleaks.toml`, no
`.pre-commit-config.yaml`, no `.github/workflows`, and no `.gitignore` rule covering `contracts/test`.
The repository has two GitHub remotes and a merge-PR workflow, so the exposure population is not one
laptop.

**Exploitation path.** Attacker's starting capability: **read access to the repository. Nothing
else.** The two credential classes compose — the spend keys supply the witness, the relayer key or
the bearer token supplies the `onlyRegistered` `msg.sender`:

| Credential | Capability |
|---|---|
| `b30e25be…` | Sweep the ~0.717 native balance; halt settlement by draining gas. Submit `transfer`/`transferWithFee` **directly**, bypassing the relayer's serialisation, dedup and any rate limiting — `relayer/cmd/register/main.go` exists precisely to make the relayer address satisfy `onlyRegistered`, and `.env.example:3` states the address "must be funded with gas **and registered as a participant in Enygma**". Grief every honest bank by re-stamping `lastBlockNum`. |
| `enygma-test-secret` | Have the relayer sign and submit an arbitrary payload. The relayer performs no proof verification and no identity check. |
| `424242, 1..5` | Spend the entire balance of any of the six banks, honestly, with a proof the deployed verifier accepts — demonstrated above. |

**Validator corrections that must be carried forward.**

1. **The Groth16 proving keys are NOT a credential leak, and were removed from this finding.**
   Proving keys are public parameters by design; every honest prover needs them and the protocol
   requires distributing them to every Privacy Node. `keygen/generate_keys.go:31` calls
   `groth16.Setup(ccs)`, which draws from `crypto/rand`; no seed is committed and the trapdoor is not
   recoverable from a `pk` file. The setup concern is **H-12**, not this. Leaving this claim in would
   have damaged the report.
2. **The client default `change-me` is not a weak server default.** `relayer/config/config.go:67`
   defaults to the empty string and `:94-96` hard-fails on it. The relayer has no default token.
   Downgraded to a Low note.
3. **The owner/deploy key `34d091c6…` is a separate, lower-severity item** — see H-08. Every consumer
   of it defaults to a local chain, and the address is at nonce 0 with zero balance on 72957.
4. **What is deliberately NOT claimed:** that the committed spend keys are the ones currently
   registered on the live contract (strong circumstantial evidence — the mainnet-default Go test
   suite registers exactly these — but not proof), and that large economic value is at stake (all
   Enygma value originates from `onlyOwner mintSupply`; the 0.717 native balance is gas money). The
   plausible reading is a pilot on a public production chain. That tempers the monetary figure, not
   the grade.

**Real-world impact.** Total loss of the authorisation model for the deployment concerned. The holder
can sign as the settlement relayer for the entire payment rail, authenticate to the relayer API, and
produce verifier-accepted proofs that spend any of the six banks' balances. For a system whose users
are regulated financial institutions, this is the one finding in the audit that needs an incident
response today rather than a code change.

**Remediation.**

1. Treat `0xEa8D34E0aAC0308F58b740768C82e2411A38C2b7` as compromised **now**: move its balance, stop
   using it, and clear its registration if it has one.
2. **Rewrite git history** — the key is in commit `bd4e520` and its ancestors, so deleting the file
   at HEAD accomplishes nothing. Force-push and invalidate existing clones. The scrub must cover
   `enygma_payments/demo/demo` (a binary), which a `git filter-repo` path list chosen from a grep of
   *text* files will silently miss.
3. Rotate the bearer token on every deployment and replace the single shared token with per-caller
   credentials (H-06).
4. For the bank spend keys, **rotation is not clean and redeployment is the honest answer**:
   re-registering an account zeroes its balance commitment, double-adds to `totalSupply` and inflates
   `_totalRegisteredParties` (`Enygma.sol:207-215`). On any deployment carrying real balances,
   redeploy rather than rotate. This is what makes C-06 a redeployment-grade incident.
5. Install a secret scanner in CI and a pre-commit hook. The repository has already failed a manual
   scrub twice.

---

#### C-01 — Unconstrained `ModHint` quotient lets a prover choose the result of every `mod P` reduction

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED (two scope corrections, both
narrowing the description, neither narrowing the impact)**

**Description.** Every modular reduction in all four circuits is implemented by the same
two-constraint gadget: `q·P + r == value` together with `IsLess(r, P) == 1`. Because `P` (the Baby
Jubjub prime subgroup order) is invertible in the BN254 scalar field, a satisfying
`q = (value − r)·P⁻¹` exists for **any** chosen `r ∈ [0, P)`, and `q` is bounded nowhere. The
reduction is therefore vacuous. The missing third constraint is a three-bit bound, `q ≤ 7`.

**Affected code.** `gnark-server/utils/utils.go:93-108` (the hint definition);
`gnark-server/pkg/circuits/enygma/circuit.go:85-91, 109-115, 126-130, 155-161, 186-192, 270-276,
288-294`; `enygma_fee/circuit.go:100-105, 123-128, 134-139, 151-156, 177-182, 195-200, 275-280,
288-293`; `deposit/circuit.go:81-87, 104-110, 116-121, 133-139, 209-215, 229-235, 245-251`;
`withdraw/circuit.go:90-96, 102-108, 121-127, 201-207, 221-235, 237-243`; consumed by
`contracts/enygma/contracts/Enygma.sol:396-417` and `:794-834`.

**Census.** 28 source-level call sites, **88 loop-expanded invocations** (22 / 23 / 22 / 21 across the
four circuits). Every quotient wire is referenced **exactly once**, in its own `AssertIsEqual` line,
and nowhere else. There is no `ToBinary(q, …)`, no `AssertIsLessOrEqual(q, …)`, and no shared
reduction helper — the gadget is copy-pasted inline at all 28 sites.

**Why the prover needs nothing special.** gnark v0.12.0's `frontend/cs/r1cs/builder.go:349-360`
documents `NewHint` explicitly: *"No new constraints are added to the newly created wire and must be
added manually in the circuit."* The compiled R1CS stores only the hint **ID**; the implementation is
supplied at proving time via the public option `solver.OverrideHint`. The attacker does not patch the
circuit, patch gnark, or hand-assemble a witness — they call ordinary `groth16.Prove` with one extra
option.

**Evidence — executed end to end against the committed keys and the real contracts.**

```
$ cd C01-repro && GOFLAGS=-mod=mod GOPROXY=off go run .

compiled UNMODIFIED enygma.EnygmaCircuit (k=6): 94206 constraints, 84175 internal, 81 public, 23 secret
loaded committed keys/EnygmaPk.key and keys/EnygmaVk.key

does the committed VK reproduce the DEPLOYED verifier contract?
  sha256(ExportSolidity(EnygmaVk.key)) = d9f5a9925763539c71ffdfde19eb0ee0cd18ba30b58020fe2cef3a528f73c09f
  sha256(.../contracts/enygma/contracts/EnygmaVerifier.sol) = d9f5a9925763539c71ffdfde19eb0ee0cd18ba30b58020fe2cef3a528f73c09f
  byte-identical: true

=== CONTROL A: honest witness, honest hint (transfer 7 of 100) ===
  groth16.Prove OK (239ms)
  ModHint invoked 22 times while solving this circuit
  groth16.Verify OK  <-- proof accepted by the COMMITTED EnygmaVk.key
  public: SenderTxValue(range-proved, private) = 7
  public: TxValues[sender] (on-chain delta)    = 2736...373034   (== P - 7, a debit)

=== CONTROL B: attack witness, HONEST hint (must fail) ===
  groth16.Prove FAILED after 1ms:
    constraint #2070 is not satisfied: 1 ⋅ 1000000000000000000000000 != 0

=== ATTACK 1: mint 10^24 from a zero balance (evil hint) ===
  groth16.Prove OK (238ms)
    hint override fired for input 27360303589799094027... (1 time)
  groth16.Verify OK  <-- proof accepted by the COMMITTED EnygmaVk.key
  public: SenderTxValue(range-proved, private) = 0
  public: TxValues[sender] (on-chain delta)    = 1000000000000000000000000   (a CREDIT)
```

**CONTROL B is the decisive control**: the *identical* assignment, solved with a byte-for-byte
reimplementation of the honest `utils.ModHint`, is rejected. The attack is created solely by the
freedom in the hint output — exactly one of the 22 invocations is overridden; the other 21 reduce
honestly.

Then the on-chain leg, against the repository's own `Enygma.sol` + `CurveBabyJubJub.sol` +
`EnygmaVerifier.sol`, deployed and driven through the repository's own setup sequence:

```
$ cd C01-onchain && FOUNDRY_OFFLINE=true forge test -vv --use ~/.svm/0.8.24/solc-0.8.24

[PASS] test_A_honest_transfer_endtoend()   (gas: 10871953)
[PASS] test_B_attack_mint_endtoend()       (gas: 11288813)
  ATTACK: sender is a freshly registered account holding Com(0, r) -- zero funds
  ATTACK: transfer() ACCEPTED. sender's new balance opens to 1000000000000000000000000
          totalSupplyAmount still: 0
          Enygma.check() (the only public solvency invariant) passes: true
[PASS] test_C_attack_forged_pubkey_endtoend() (gas: 8305394)
[PASS] test_D_attack_theft_endtoend()      (gas: 12588000)
  THEFT: victim account 1 holds Com(500, r) -- r is public calldata from registerAccount
  THEFT: victim now opens to 0, attacker account 2 now opens to 500
         attacker never knew the victim's spend key
         Enygma.check() still passes: true
[PASS] test_E_verifier_rejects_tampered_signal() (gas: 4808266)
  VERIFIER CONTROL: untouched attack proof accepted
  VERIFIER CONTROL: same proof with one public signal changed is REJECTED

Suite result: ok. 5 passed; 0 failed; 0 skipped.
```

`test_E` is the verifier-side control: the same verifier that accepts the attack proof rejects it the
moment a public signal is perturbed. The verifier is behaving normally; the circuit is unsound.

**Exploitation path.** Attacker capabilities, all satisfied in a normal deployment: (1) be a
registered institution — `transfer` is `onlyRegistered`, which in an inter-bank setting every
participant is; (2) hold the proving key — `gnark-server/keys/EnygmaPk.key` is tracked in git, and
each participant runs the prover locally anyway; (3) run `groth16.Prove` with one extra option,
~240 ms per proof. **Mint:** register (balance `Com(0, r)`), prove with `PreviousSenderBalance = 0`
and `SenderTxValue = 0` so the range proof is honestly satisfied, override the `expectedTxValue` hint
to return `X`, park `P − X` on any other slot to satisfy `Σ TxValues ≡ 0`, submit. **Theft:** override
only the public-key hint so `Poseidon(sk,sk) mod P` "equals" the victim's registered key; the
remaining gate is opening the victim's commitment, which this design publishes.

**Validator corrections.**

- Forging `PublicKey[sender]` **alone is not theft** — `_verifyPublicInputsFP` pins the public key
  and the previous commitment to the *same* account id, so the attacker still needs the victim's
  opening. It is theft only because the opening is public in this design (registration randomness in
  calldata, mint with zero blinding). `test_C` shows the authority check falling; `test_D` shows the
  actual theft.
- The register's suggested reproduction (a hint returning `q+1, r−P`) is not the cleanest
  formulation: `r − P` is negative and fails `IsLess(r, P)`. The correct construction is "choose `r`,
  derive `q = (value − r)·P⁻¹ mod p`".

**Real-world impact.** Unlimited creation of shielded value in an inter-bank settlement / CBDC
system, by any single participant, invisible to the protocol's only public solvency check. Every
honest institution's holdings are diluted against the real fiat backing. Plus complete theft of any
funded-but-not-yet-spent participant's balance.

**Remediation.** Replace all 28 inline reductions with one helper emitting **three** constraints:

```go
func ReduceModP(api frontend.API, value frontend.Variable) frontend.Variable {
    out, _ := api.NewHint(utils.ModHint, 2, value)
    r, q := out[0], out[1]
    api.AssertIsEqual(api.Add(api.Mul(q, P), r), value)
    api.AssertIsEqual(cmp.IsLess(api, r, P), 1)   // r < P
    api.AssertIsLessOrEqual(q, 7)                 // <-- MISSING TODAY; floor(Fr/P) == 7
    return r
}
```

`q ≤ 7` compiles to a three-bit decomposition and is essentially free. This requires a **new trusted
setup and redeployment of every verifier**. Add a regression test asserting that `ccs.Solve` with
`solver.OverrideHint(solver.GetHintID(utils.ModHint), tamperedHint)` **fails** — that test fails
today. Independently, and as defence in depth: stop publishing balance openings — `registerAccount`
should not take `randomness` in plaintext calldata, and `mintSupply` should not commit with `r = 0`
nor emit the amount.

---

#### C-02 — Balance scalars are bounded at 252 bits but consumed modulo a 251-bit order, so `b + 2P` opens the same commitment and defeats the solvency proof

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED**
***Absorbs C-07*** *(the `transferWithFee` arbitrary-mint finding), which the validator established is
not independent — see the sub-section below. Reported once, remediated once.*

**Description.** `utils.ScalarMul` / `utils.PedersenCommitment` multiply generators whose order is
`P` (251 bits, `log2(P) ≈ 250.60`), while the circuit's only bound on `PreviousSenderBalance` is
`api.ToBinary(·, 252)`. Since `2^252 / P = 2.6451`, a 252-bit witness may exceed `P` by up to `2P`,
and `Com(b, r) == Com(b + 2P, r)` **exactly**. A prover therefore opens the *same* on-chain
commitment — so the contract's previous-commitment binding passes honestly — while the in-circuit
solvency comparator `api.Cmp(previousV, v) != −1` sees a balance of roughly `2P ≈ 5.5 × 10^75`.

**Affected code.** `gnark-server/utils/circuits.go:63-82` (`ScalarMul`), `:105-115`
(`PedersenCommitment`); `gnark-server/pkg/circuits/enygma/circuit.go:75-81, 176-179, 227-237`; the
same gadget/range-check pairing in `enygma_fee/circuit.go:90-96, 227-244` (which uses an even wider
`ToBinary(totalDebit, 253)` ≈ 5.29·P) and `deposit/circuit.go:175`; `withdraw/circuit.go` has no
comparator at all (C-08).

**Evidence — the arithmetic, then the deployed verifier.**

```
$ python3 arith.py
P bitlen 251
Fr bitlen 254
2^252/P = 2.645075027614269
2P   = 5472060717959818805561601436314318772153627944317134518400431321896894746082
2P < 2^252 ? True
3P < 2^252 ? False
G on curve: True  H on curve: True
P*G == identity: True
P*H == identity: True
 b     : (7433071517369258790898288631616683476567116341576506817529361376893749892433, ...)
 b+2P  : (7433071517369258790898288631616683476567116341576506817529361376893749892433, ...)
 EQUAL : True
```

Both generators have order exactly `P`, so there is no 8-torsion component; and since the token has
`DECIMALS = 2`, `b + 2P < 2^252` for every conceivable balance.

Satisfiability, with three negative controls that each fail at a *different, identified* constraint:

```
$ GOFLAGS=-mod=mod GOPROXY=off go run . prove
compiling the UNMODIFIED enygma-server/pkg/circuits/enygma.EnygmaCircuit ...
  constraints=94206  public=81  secret=23

CONTROL A: honest claimed balance = realBalance (0)              -> rejected: constraint #40589
CONTROL B: claimed = realBalance + 1  (naive lie)                -> rejected: constraint #40589
CONTROL D: claimed = 1e18 (solvent, but commitment differs)      -> rejected: constraint #20943
ATTACK  1: claimed = realBalance + P                             -> SATISFIED
ATTACK  2: claimed = realBalance + 2P                            -> SATISFIED
CONTROL C: claimed = realBalance + 3P (must fail: 3P > 2^252)    -> rejected: constraint #37033
```

Constraint #40589 is the solvency comparator (`circuit.go:232-233`); #20943 is the
previous-commitment binding (`:178-179`); #37033 is the 252-bit decomposition (`:227`). **All three
protections genuinely work.** The *only* reason `P` and `2P` get through is the gap between "bounded
by `2^252`" and "reduced mod `P`".

Answering the register's own question directly:

```
--- register's validation question: real balance = 100, claim 100+2P, spend 1e18 ---
IsSolved(realBalance=100, claimed=100+2P, spend=1e18) -> true
Com(100,12345)                    = (16921674065580869673406387932145254081465895333724033169844103052606943166731, 3583679023436375626166366756741816544600867867372265961071091475515556299066)
PreviousCommit[sender] in witness = (16921674065580869673406387932145254081465895333724033169844103052606943166731, 3583679023436375626166366756741816544600867867372265961071091475515556299066)
identical: true
```

Then a real proof under the committed keys, and finally the **deployed verifier bytecode** on a local
anvil node:

```
$ cast call --rpc-url http://127.0.0.1:8599 $ADDR "verifyProof(uint256[8],uint256[80])" "$P8" "$S80"
0x
exit=0                                  <-- ACCEPTED (verifyProof reverts on a bad proof)

$ # CONTROL: same proof, input[42] (= PreviousCommit[0].x) incremented by 1
$ cast call ... "$P8" "$S80T"
Error: execution reverted: custom error 0x7fcdd1f4      <-- ProofInvalid
exit=1
```

`input[42]`/`input[43]` are `FP_PREVIOUS_COMMIT_OFFSET + 0` and carry exactly `Com(0, 12345)` — the
sender's honest on-chain balance point, which is what `_verifyPublicInputsFP` (`Enygma.sol:747-752`)
compares against storage. It matches.

**Two things the register understated:**

```
$ go test -run TestMintCeiling -v
realBalance=0 claimed=2P amount=2736030358979909402780800718157159386076813972158567259200215660948447373040 -> satisfied=true   (P-1)
realBalance=0 claimed=2P amount=1606938044258990275541962092341162602522202993782792835301376             -> satisfied=true   (2^200)
realBalance=0 claimed=2P amount=5472060717959818805561601436314318772153627944317134518400431321896894746082 -> satisfied=false  (2P > P)

$ go test -run TestWrapped -v
HONEST witness, balance = P-1e18, spend 1e18 -> satisfied=true
```

1. **One transaction can credit an accomplice with up to `P − 1 ≈ 2.7 × 10^75` units.** The ceiling is
   the whole scalar range, not "some large amount".
2. **The attack does not need to be repeated.** After the aliased transfer the attacker's own
   commitment opens to `(0 − v) mod P = P − v`, an ordinary 251-bit balance that passes the honest
   solvency check with an entirely honest witness thereafter. One aliased transfer converts a zero
   balance into a permanently spendable balance of ≈ `P`.

**A harness defect worth recording.** The validator could **not** reproduce the original local
auditor's demonstration, because `audit-state/local-notes/L6-repro/` does not contain it — its only
`func main()` is a verifying-key comparison, and the C-02 program was never saved. The register's
"L6 demonstrated this" was, until validation, unverifiable. It was rebuilt from scratch and holds;
L6's quoted commitment for `Com(0, 12345)` is byte-identical to the independent recomputation, so L6
did run something real.

**Independence from C-01, verified specifically.** The harness registers the target's own unmodified
`utils.ModHint`; nothing named `Evil*` is imported; every hint output in the malicious witness is the
true reduction. The two defects have disjoint fixes: bounding the quotient does not narrow
`ToBinary(·, 252)`, and narrowing the range check does not bound the quotient. **They must not be
merged.**

**Corrections to the source text.** `P ≈ 2^251.13` (register) and `P ≈ 2^250.9` (issue file) are both
wrong: `P.bit_length() = 251`, `log2(P) ≈ 250.60`. And the original Info-graded contributing issue
concluded *"I could not construct value creation from this alone"* — that conclusion is wrong and must
not be carried forward; value **is** created, the author simply did not push the construction through.

##### C-02 (fee path) — formerly C-07: `transferWithFee` mints arbitrary supply

The `enygma_fee` circuit asserts conservation as `Σ TxCommit + Fee·G == (0,1)`. `Fee` is bounded only
by `ToBinary(·, 252)`, so `Fee = P − X` flips the identity from a burn of `Fee` to a **mint of `X`**,
and the circuit pins the attacker's own slot as the credited one — no accomplice needed.
`Enygma.transferWithFee` reads signals 6-11, 12-23, 24-35, 36 and 49 and **never reads 50 (`Fee`),
51-52 or 53**. Demonstrated:

```
$ GOFLAGS=-mod=mod GOPROXY=off go run .
claimed balance   = 5472060717959818805561601436314318772153627944317134518400431321896894746182   (= real + 2P)
Fee public signal = 2736030358979909402780800718157159386076813972158567259199215660948447373041   (= P - 1e18)
sum(TxCommit) == mint*G ? true   <-- NET SUPPLY CHANGE = +1e18
Com(real,r) == Com(real+2P,r) ? true   <-- on-chain check passes

=== R1CS SATISFIED WITH ATTACKER WITNESS: true ===
control (honest balance, same Fee): satisfied = false (expected false)
      error: constraint #39051 is not satisfied: 1 ⋅ 1 != 0

Prove(committed EnygmaFeePk.key) err = <nil>
=== Verify(committed EnygmaFeeVk.key) err = <nil> => SHIPPED KEYS ACCEPT: true ===
ExportSolidity(vk) == committed EnygmaFeeVerifier.sol : true (gen 56375 bytes, file 56375 bytes)
```

**Why this is C-02 and not a separate Critical.** To mint `X` the attacker needs its own slot to
decode as a small positive `X`; the circuit pins it to `(P − v − Fee) mod P`, which forces
`Fee ≈ P` for *every* mint. The solvency comparator then demands `PreviousSenderBalance ≥ v + Fee ≈ P`
— which no honest balance approaches, and which is exactly why the control run fails at constraint
#39051. **The only way through is C-02's aliasing.** Fixing C-02 closes the fee mint completely,
degrading the fee path to M-13 (fee silently destroyed). Bounding `Fee` alone does **not** fix it
(`Fee = P − X < P` still mints `X`). C-02 is already LIVE on the transfer path and already yields
unlimited unbacked issuance there; the fee instance's marginal contribution is only "no second
account needed", on a path that is currently switched off. Reporting it separately would
double-count one root cause and inflate the Critical total.

**Also found while refuting it — a documented control that does not exist.**
`go_client/enygma_test/fee_transfer_test.go:10` and `:438` both state that *"the relayer verifies
`signal[50] == PROTOCOL_FEE`"*, and `relayer/server/handler.go:218-219` repeats that the fee "is
visible to the relayer". `RelayTransferFee` (`handler.go:220-294`) was read line by line: it parses
the proof, checks `len(req.PublicSignal) == 54`, parses commitments, dedups, and calls
`TransferWithFee`. **There is no comparison of `pubSig54[50]` against anything.** The fee amount is
unconstrained even in honest operation.

**Reachability of the fee instance: LATENT, not BROKEN.** Every component of the fee feature is
finished and shipped — `external` function on a live contract, verifier committed inside the hardhat
sources root, both keys in git, prover route registered, relayer route registered, abigen bindings
generated in both client packages, end-to-end test present. Only `addFeeVerifier` is uncalled. The
guard at `Enygma.sol:971` is a feature flag, not a defect, and flags get flipped by operations
without code review. **`_feeVerifier` is `private` with no getter; its live value could not be read
offline. If it is non-zero, this is exploitable today and is the single most urgent item in the
report.**

**Remediation.** One rule closes C-01's sibling family, C-02, the fee mint and half of H-13:
**every witness that is both bit-decomposed and used as a Baby Jubjub scalar must additionally be
asserted `< P`.** `grep -n 'ToBinary(.*252)'` finds every candidate in nine lines. Concretely:
replace `api.ToBinary(v, 252)` with a 251-bit decomposition plus `AssertIsLessOrEqual(v, P-1)`, or
bound monetary quantities to a realistic width (64 bits is ample). Requires a new trusted setup and
verifier redeployment. Additionally, have `transferWithFee` read signal 50 and compare it to a
contract-side `PROTOCOL_FEE`, and credit the fee to a real recipient (M-13).

---

#### C-03 — Non-sender transfer amounts have no range proof, so a "credit" of a negative amount debits any other institution

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED, with two corrections that make it
worse than filed**

**Description.** The circuits range-check only the **sender's** slot. For every other slot the sole
constraint is the aggregate `Σ TxValues ≡ 0 (mod P)`. A "credit" of `P − w` is therefore a debit of
`w`, and the contract applies it verbatim. There is no sign concept in the contract at all: the
deltas are hiding Pedersen commitments and `_updateBalancesForTransfer` performs a blind
`CurveBabyJubJub.pointAdd`.

**Affected code.** `gnark-server/pkg/circuits/enygma/circuit.go` — `TxValues` appears at exactly three
sites: `:72` (selects the sender's slot only), `:201` (field-element sum), `:252` (Pedersen
commitment). The only decompositions in the whole circuit are `:75`, `:76`, `:77` and `:227`, none of
which is inside a loop over `i`. Same shape in `enygma_fee/circuit.go:87, 192, 260`,
`deposit/circuit.go:69, 164, 199`, `withdraw/circuit.go:71, 160, 191`. Consumed by
`contracts/enygma/contracts/Enygma.sol:734-770` and `:794-834`.

Every candidate indirect binding was checked and none touches `TxValues[i]` for `i ≠ sender`:
Poseidon preimages take secrets and block numbers, never a value; commitment equality is satisfied
for any field element because Pedersen is perfectly hiding; the random-factor derivation constrains
`TxRandomValues[i]`, never `TxValues[i]`; and `Σ TxCommit == (0,1)` is implied by the sum check and
adds nothing.

**Evidence — the stock, unauthenticated prover accepts it.**

```
$ ./bin/stockserver &
[GIN-debug] POST   /proof/enygma  --> enygma-server/pkg/circuits/enygma.NewHandler.func1 (3 handlers)

$ grep -A9 '"tx_values"' attack.json
 "tx_values": [
  "0",
  "400",
  "2736030358979909402780800718157159386076813972158567259200215660948447372641",   # = P - 400
  "0", "0", "0"
 ]

$ curl -s -w "\nHTTP %{http_code}\n" -X POST http://127.0.0.1:8099/proof/enygma \
      -H 'Content-Type: application/json' --data-binary @attack.json -o resp.json
HTTP 200
proof len: 8
publicSignal len: 80
```

**No `Authorization` header was sent. The stock handler accepted `P − 400` verbatim and returned a
valid Groth16 proof.** The only validation on that path is a gin binding tag on the *slice*
(`binding:"required,min=1,max=6"`) — a length check, not a value check — and `utils.ParseBigInt`
discards its ok flag.

Circuit satisfaction with a negative control:

```
$ go run .
Case A  control / honest transfer                          -> SATISFIED
Case B  ATTACK: SenderTxValue = 0, TxValues = [0, +400, P-400, 0, 0, 0]
        slot 2 = VICTIM (balance 1000, never consented)    -> SATISFIED
Case C  negative control: TxValues[2] = P-400-1            -> NOT SATISFIED: [assertIsEqual] 5347602748...682 == 0
                                                              enygma.(*EnygmaCircuit).Define circuit.go:206
Case E  ATTACK, X = 10^18 >> victim's balance of 1000      -> SATISFIED
```

Case C is what makes this evidence rather than an assertion: the solver *does* reject a witness that
fails conservation, and it fails at the conservation assertion itself. The attack passes because
`P − 400` is a *correct* solution to the only constraint that exists.

End to end, against the real contracts on a local chain, after the repository's own setup sequence
(`initialize` → `addVerifier` → `registerAccount ×6` → `mintSupply(1000, 3)`):

```
--- submitting Enygma.transfer from attacker EOA (accountId 1) ---
tx status 0x1 gasUsed 0x1290bc
check() = true

accountId 2 : before value 0    -> claimed after value 400
accountId 3 : before value 1000 -> claimed after value 600
```

Byte-for-byte match on the independently recomputed commitments. **The victim lost 400. The attacker's
account gained 400 with an opening it knows. The attacker's own balance is unchanged — it paid
nothing. `check()` returns `true`,** and cannot detect this by construction, because the deltas sum
to the curve identity.

**Two validator corrections, both strengthening the finding.**

1. **No colluding second institution is needed.** The contributing issue said a colluder was required
   to keep the money. The circuit constrains `PublicKey[i]` and `PreviousCommit[i]` for the sender's
   slot *only*, and `_verifyPublicInputsFP` iterates `participantIds` independently per slot with no
   duplicate check — so the attacker lists **its own account id twice** and takes the credit itself:

   ```
   participantIds = [1,1,3,4,5,6]   (attacker id 1 appears twice, NO colluder)
   tx status 0x1
   check() = true
   victim  id3 expected Com(600, ...)   = (587144088372849817279026783975803788108207026199845072450315169543829767097, ...)
   attacker id1 CHAIN Com(400, ...)     = (15852763130019363533154158274581384897744733268683849534449578240498215953264, ...)
   ```

   Single attacker, single account, zero balance, no accomplice: victim 1000 → 600, attacker 0 → 400,
   `check()` true. (Note the interaction with C-05: at `epochInterval = 1` the duplicate-id variant
   still lands the theft but `check()` then reverts, because one delta is dropped. With a second
   controlled account it is silent unconditionally.)
2. **The attacker needs no balance at all.** `SenderTxValue = 0` satisfies the solvency proof for any
   previous balance, including zero. The attack is free.

**Exploitation path.** Everything the attacker needs about the victim is public — `publicKeys[victimId]`
and `balanceCommitments[lastBlockNum][victimId]`, both returned by the unauthenticated view
`getPublicValues()`. The circuit only *opens* the sender's previous commitment; for every other slot
it merely asserts the point is on the curve. So no shared secret, no prior transaction, no key
agreement and no consent from the victim is required. The sender chooses `participantIds`
unilaterally.

The victim's blinding factor is simultaneously overwritten with one derived from an attacker-chosen
secret — which is **C-04**'s freeze. So by default the victim is *both* robbed and frozen. If the
attacker and victim have run key agreement, the attacker can use the genuine pairwise secret and the
theft is completely silent.

**Independence.** The proof used the unmodified `utils.ModHint` (registered by the stock server) and
only canonical scalars strictly below `P`, so neither C-01 nor C-02 is engaged. Conversely, fixing
C-01 and C-02 leaves C-03 completely untouched, because the non-sender slots are not decomposed *at
all*. **Three distinct fixes are required.**

**Real-world impact.** Any participating institution can, for free and in a single transaction, move
an arbitrary amount out of any other institution's balance into an account it controls, with no
consent, no counterparty signature and no on-chain trace distinguishing it from a normal payment. The
amount is not bounded by the victim's balance — over-debiting wraps the victim's committed value to a
near-`P` quantity, which then passes the 252-bit range check and is itself spendable, inflating
supply. This is exactly the property `protocol_description.md:269` claims ("The total amount of funds
I am spending is being credited to the other participants") and which is not enforced.

**Remediation.** Range-check **every** `TxValues[i]` to a width `w` with `k · 2^w ≪ P` (64 bits is
ample); encode the single negative slot explicitly rather than as a field element that happens to sum
to zero; and assert `Σ_{i≠sender} TxValues[i] == SenderTxValue` **over the integers** instead of
relying on a mod-`P` point identity. Apply identically to all four circuits. Add the negative test
(`TxValues[j] = P − 1` for `j ≠ sender` must fail) — it currently passes. Independently, reject
duplicate entries in `participantIds` on-chain, and require every participant id to be registered
(H-07).

---

#### C-05 — Duplicate account ids silently discard a debit at epoch rollover, minting value from nothing

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED (demonstrated end to end)**

**Description.** `_updateBalancesForTransfer` reads each account's old balance from one epoch slot and
writes the new balance to a different one:

| line | code | slot |
|---|---|---|
| `:798` | `uint256 epochStart = _currentEpochStart();` | `(block.number / epochInterval) * epochInterval` |
| `:819` | `uint256 accountId = participantIds[i];` | **no distinctness check** |
| **`:820-822`** | `Point storage oldBalance = balanceCommitments[lastBlockNum][accountId];` | **READ @ `lastBlockNum`** |
| **`:831`** | `balanceCommitments[epochStart][accountId] = Point(newX, newY);` | **WRITE @ `epochStart`** |
| `:838` | `lastBlockNum = epochStart;` | advanced only *after* the loop |

When `epochStart == lastBlockNum` the storage pointer aliases the written slot, so repeated ids
accumulate correctly. When they differ — i.e. on the first transaction of a new epoch — every
iteration reads the pre-rollover value, so for a duplicated id the later write **overwrites** the
earlier one and that delta is silently discarded. Choosing which slot carries the debit chooses mint
versus burn: the last write wins.

**Affected code.** `contracts/enygma/contracts/Enygma.sol:794-839` (esp. `:820`, `:831`), `:844-871`
(the identical pattern at `:852-854` / `:863`), `:734-771`, `:985-1022`, `:675-720`;
`run_scripts/deploy_direct.py:23-24`; `run_scripts/deploy_node.js:14-16`.

**No distinctness control exists anywhere.** Every use of `participantIds` in `Enygma.sol` was
enumerated (lines 399, 405, 414, 429, 432, 451, 463, 477, 493, 505, 520, 677/684-686, 736/743-745,
796/805/819, 846/851, 987/994/996); none compares two entries of the array to each other.
`_isParticipant` is a linear membership test, not a uniqueness test. A repo-wide grep for
`distinct|duplicate|strictly increasing|sorted|unique` over the contract and interface yields one
hit: a doc comment.

**And the circuit's one anti-duplicate constraint is bypassed for free.** The circuit does constrain
its own `AnonymitySet` (`circuit.go:59-64`: `Σ_i IsZero(AnonymitySet[i] − SenderId) == 1`), but
**the contract never reads `AnonymitySet`** — `FP_K_INDEX_OFFSET` (`Enygma.sol:43`) and
`K_INDEX_OFFSET` (`:27`) are declared and never dereferenced. So the attacker keeps a perfectly
honest, fully distinct `AnonymitySet = [1,2,3,4,5,6]` inside the proof and supplies a *different*,
duplicated `participantIds = [1,1,3,4,5,6]` to the contract.

**Evidence.** Environment: `solc 0.8.24`, `anvil 1.5.1`, Go 1.25.5 with gnark v0.12.0, offline.
Contracts compiled unmodified from the target; `EnygmaVerifier.sol` verified byte-identical to
`gnark-server/keys/EnygmaVerifier.sol`. Deployment mirrors `deploy_direct.py`'s default
(`epochInterval = 30`), with the repository's own committed demo spend keys `{424242,1,2,3,4,5}`, then
`mintSupply(1000, 1)`.

```
$ attackbin -mode prove -sk 424242 -prevbal 1000 -prevr 12345 -block 0 -v 1000 -creditslot 1 \
    -pubkeys "PK1,PK1,PK3,PK4,PK5,PK6" -prevcommits "B1,B1,B3,B4,B5,B6" \
    -pk gnark-server/keys/EnygmaPk.key
DBG running circuit in test engine
circuit constraints satisfied
INF parsed circuit inputs nbPublic=80 nbSecret=23
compiled: 94206 constraints
DBG prover done backend=groth16 curve=bn254 nbConstraints=94206 took=165.865541
```

Submitted from bank 1's own address — **no relayer involved**:

```
>>> block.number=36 lastBlockNum=0 epochStart=30
tx status 0x1 gasUsed 1236928
>>> after: lastBlockNum=30
bal[1]=20936131367659454039710403683913032054681462117968302843789937999236629617278,
       6993076546814354242371815269948387152022145667880315877887434671980378458072
check(): Error: execution reverted: custom error 0xca3e0a68
```

`cast sig "BalanceMismatch()"` → `0xca3e0a68`. The prover had printed the predicted post-state
**before** the transaction was sent:

```
EXPECTED_IF_DEBIT_DROPPED 20936131367659454039710403683913032054681462117968302843789937999236629617278
                          6993076546814354242371815269948387152022145667880315877887434671980378458072
NEWV 2000  NEWR 2463156057206256334374071222406449411357581607633333537200200076896736547769
```

Exact match. Account 1's committed value went 1000 → 2000 with no counterparty debited.

**The matched control run isolates the cause.** Fresh chain, identical proof, identical duplicated
ids, submitted so that `epochStart == lastBlockNum`:

```
>>> block.number=11 lastBlockNum=0 epochStart=0
tx status 0x1 gasUsed 956928
check(): true
```

No inflation. The *only* difference is whether `epochStart == lastBlockNum`, which pins the defect
precisely to the `:820` / `:831` read/write asymmetry.

**The minted value is spendable.** Immediately afterwards, a fully honest transfer of the full 2000
from account 1 to account 2:

```
$ attackbin -mode prove -sk 424242 -prevbal 2000 -prevr 2463156057206256334374071222406449411357581607633333537200200076896736547769 ...
circuit constraints satisfied          # no prev-commitment mismatch warning

=== HONEST follow-up: account 1 spends the full 2000 to account 2 ===
tx status 0x1 gasUsed 941374
```

An institution that was ever minted 1000 spent 2000 to a different institution. The fabricated value
has left the attacker's account and is, on chain, indistinguishable from honest funds.

**When does it trigger?** `lastBlockNum` is assigned `epochStart` by every mutator, so the rollover
branch is taken exactly when no Enygma-mutating call has yet occurred in the current epoch. This is
not a timing coincidence the attacker must hit: `lastBlockNum` and `epochInterval` are public state,
so the attacker computes the condition and submits only when it holds. A lost race costs only gas. At
`EPOCH_INTERVAL = 30` on a ~0.5–1 s chain that is a 15–30 second window; at `deploy_node.js`'s
`EPOCH_INTERVAL = 1` it is unconditional.

**Magnitude.** A pure mint is capped by the attacker's own current balance (the solvency check forces
the credit to equal the debit), so the **balance doubles per transaction**: 1000 → 2000 → 4000 → …
Roughly 40 transactions from any non-zero starting balance exceeds any realistic supply. Repeatable
once per epoch (both `prevR` and `lastBlockNum` change after each success, so a fresh nullifier is
always available). Combined with C-03's missing receiver range check, the per-transaction gain is not
even bounded by the attacker's balance.

**Validator corrections.**

1. The `AnonymitySet` need not contain the duplicate — the contract never reads it. This makes the
   attack *strictly easier* than described and removes the only circuit-side constraint that looked
   like it might block it.
2. `participantIds` and `commitmentDeltas` are iterated with **different bounds**
   (`participantIds.length` at `:743` vs `commitmentDeltas.length` at `:817`). Not needed here (both
   are 6) but it is the same missing-length-check family and belongs in the fix.
3. The `_updateBalances` variant (withdraw/deposit) carries the identical defect but is on a BROKEN
   path. Note it in the fix; do not count it as a second live Critical.

**Real-world impact.** `check()` *does* detect the resulting inconsistency — it reverted
`BalanceMismatch` in every attack run — but `check()` is an `external view` with **no on-chain caller
and no caller anywhere in the repository outside tests**. Nothing monitors it. And because balances
are Pedersen commitments, no participant can see the inflation from chain state.

**Remediation**, in order of importance:

1. Make the update a read-modify-write on a **single** slot: copy every account forward to
   `epochStart` first, then read *and* write participants at `epochStart`.
2. Reject duplicates explicitly: require `participantIds` strictly increasing, and require
   `participantIds.length == commitmentDeltas.length`.
3. Bind `participantIds` to the proof: assert `public_signal[FP_K_INDEX_OFFSET + i] == participantIds[i]`.
   The constants already exist; they are simply never read.
4. In the circuits, constrain `PublicKey[i]` and `PreviousCommit[i]` for **all** slots, not only the
   sender's.

Fixes 1 and 2 are each individually sufficient to stop this attack; 3 and 4 close the class.

---

#### C-04 — Recipients' shared secrets are free witnesses: any member can permanently freeze every other member's entire balance, for free

**Severity: Critical · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**Description.** Each participant's new blinding factor is
`r_i = Poseidon(Poseidon(21), s_i, blockNumber) mod P`, where `s_i` is a **private witness the prover
chooses**. Only the *sender's own* slot is pinned to anything (`circuit.go:102-115`, which ties it to
`Poseidon(PreviousSenderRandomValue, SecretKey)`). Every other `SharedSecrets[i]` is a free field
element, and it fully determines the shift applied to participant `i`'s blinding factor. The victim's
commitment moves from `Com(v, R)` to `Com(v, R − r_j)`: its **value is unchanged**, and it can never
again satisfy the circuit's previous-commitment constraint. Recovering `r_j` is a Poseidon preimage
or a discrete log.

The artefact that was supposed to bind this — the 36-signal `FingerPrintofSharedSecrets` matrix — does
not:

```go
// enygma/circuit.go:124-143
isRowSender := api.IsZero(api.Sub(circuit.AnonymitySet[i], circuit.SenderId))
for j := 0; j < k; j++ {
    isColSender  := api.IsZero(api.Sub(circuit.AnonymitySet[j], circuit.SenderId))
    isNotDiagonal := api.Sub(1, api.Mul(isRowSender, isColSender))
    shouldCheck  := api.Mul(isColSender, isNotDiagonal)
    diff := api.Sub(circuit.FingerPrintofSharedSecrets[i][j], hashMod)
    api.AssertIsEqual(api.Mul(shouldCheck, diff), 0)
}
```

`shouldCheck == 1` for exactly `k − 1 = 5` cells. **31 of the 36 cells are unconstrained** (the
original filing said 30; the diagonal cell is free too). The 5 that *are* checked compare against
`Poseidon(SharedSecrets[i]) mod P` — the prover's own fabricated secret. That is self-consistency,
not authenticity. There is no on-chain fingerprint registry to compare against; key agreement is
entirely off-chain over a shared filesystem.

**And the contract never reads them:**

```
$ grep -n "FP_FINGERPRINT_OFFSET\|FP_FINGERPRINT_SIZE\|FP_K_INDEX_OFFSET\|FP_K_INDEX_SIZE\|FP_MESSAGE_TAGS_OFFSET" contracts/enygma/contracts/Enygma.sol
34:    uint256 private constant FP_FINGERPRINT_OFFSET = 0;
35:    uint256 private constant FP_FINGERPRINT_SIZE = 36;
43:    uint256 private constant FP_K_INDEX_OFFSET = 67;
44:    uint256 private constant FP_K_INDEX_SIZE = 6;
45:    uint256 private constant FP_MESSAGE_TAGS_OFFSET = 73;
```

Each appears **exactly once — at its own declaration**. `_verifyPublicInputsFP` reads only the public
keys, previous commitments and tx commitments; `_verifyBlockNumberFP` reads signal 66;
`_consumeNullifierFP` reads signal 79. **Signals 0-35, 67-72 and 73-78 — 48 of 80 — are never
compared against anything.**

**Affected code.** `gnark-server/pkg/circuits/enygma/circuit.go:36-44, 100-143, 181-193, 258-307`;
`enygma_fee/circuit.go:59, 114-140, 172-183, 265-302` (the fee circuit constrains all six
`HashedSharedSecrets` but still only against the prover's own secrets, so it has the identical
weakness); `contracts/enygma/contracts/Enygma.sol:34-35` (declared, never read), `:396-418`,
`:734-771`, `:794-839`. Nothing off-chain compensates: the gnark handler copies JSON straight into the
witness and the relayer parses and forwards, both with no validation, and `MessageTags` is generated
by senders in five places and **verified by nobody**.

**Evidence — five stages against the unmodified circuit, with the stock `ModHint` and canonical
witness values.**

```
$ cd audit-state/validation/C-04-repro && GOPROXY=off go run .

== STAGE 1: does the UNMODIFIED EnygmaCircuit accept the malicious witness? ==
  SenderTxValue = 0, TxValues = [0,0,0,0,0,0]  (zero-value transfer)
  SharedSecrets[1..5] = fresh random field elements, never agreed with anybody
  30 of the 36 FingerPrintofSharedSecrets cells set to the junk value 424242
  CIRCUIT SATISFIED: true   <-- honest ModHint, unmodified circuit

== STAGE 2: apply the deltas exactly as Enygma.sol:815-831 does ==
  account 1: value still 1000  ; commitment moved: true ; opens with r-r_i: true
  ... (accounts 2-6, all five peers hit in ONE transaction)

== STAGE 3: Enygma.check() -- does the global supply invariant notice? ==
  sum(balances) before == after : true  -> check() still returns true

== STAGE 3b: detection via the public MessageTags signal ==
  MessageTags[victim slot] published : 181982036880974906108362...
  tag victim expects for its real s  : 515153889028239200004696...
  match: false  -> victim CAN tell it was targeted (but not undo it)

== STAGE 4: victim (account 2) tries to spend, using the r it legitimately holds ==
  CIRCUIT REJECTS the victim's spend: [assertIsEqual] 5401437101855360166950005030634449284366542347068145733024402457875649312339 == 5925809147869302042380217412531756102550586762175978879866117522387120809547
  enygma.(*EnygmaCircuit).Define
	circuit.go:178

== STAGE 5: identical spend, but with r' = r - r_victim (only the attacker knows r_victim) ==
  CIRCUIT SATISFIED
```

Stage 4 rejects at exactly `circuit.go:178`, the previous-commitment assertion; stage 5 shows the same
spend succeeding once the attacker-only blinding factor is supplied. That pair isolates the cause
precisely: the money is still there, but its opening is now attacker-only knowledge.

**Permanence — worked through, not assumed.** Brute force is a 254-bit Poseidon preimage; recovering
`r_j` from `r_j·H` is a ~2¹²⁵ discrete log; the genuine pairwise secret does not help because the
attacker used a different one and only one-way images reach the chain; C-01's free hint gives no
relief because there is **no hint anywhere on the previous-commitment path**; C-02's aliasing changes
which integer represents a scalar, not which subgroup element it is; epoch rollover only *copies* the
frozen point forward (`:800-812`, `:906-923`) and never re-derives it; and an attacker-initiated undo
would need a Poseidon preimage of `P − r_j`. Every write to `balanceCommitments` in the file was
enumerated (lines 204, 264, 303, 806, 831, 863, 915): there is no setter, no upgrade proxy, no pause
and no rescue function, and `_owner` is `immutable`.

**Validator corrections.**

1. **"30 of 36 unconstrained" → 31 of 36.**
2. **"Undetectable" is overstated → "unattributable, unpreventable, and never actually checked".**
   Unlike the fingerprint cells, `MessageTags[i]` **is** constrained in-circuit for every slot
   (`circuit.go:183-193`) and published in calldata, so a victim holding the genuine secrets can
   detect that its slot's tag matches none of them (stage 3b). However: **no code anywhere performs
   this check**; detection yields no attribution (the sender is private and `msg.sender` is the shared
   relayer); detection is after the fact with no revert, challenge or dispute mechanism; and `check()`
   still passes. An honest sender whose key agreement has merely de-synced produces an identical
   on-chain record, so the attacker has full deniability — and that same equivalence means this is not
   only an attack but a **fragility**: a stale ciphertext file or a re-run key agreement destroys a
   balance by accident.
3. **An out-of-band issuer bailout exists — this is the one factual overstatement in the register.**
   `registerAccount` is `onlyOwner` with no already-registered guard, so the owner can register the
   victim at a **fresh** id and then `mintSupply` to it; both operations keep `check()` balanced. But
   this is trusted-issuer re-issuance, not protocol recovery: it requires the owner to be available,
   willing and told the victim's true balance; it permanently orphans the frozen commitment inside
   `totalSupply`; it inflates `totalSupplyAmount`; and the owner key is itself a committed credential.
   Re-registering at the **same** id — the remedy the original filing described — additionally breaks
   `check()` permanently. **The funds are irretrievably destroyed at the protocol level; the only
   remedy is the issuer absorbing the loss.** The Critical grade stands.
4. **The attack does not require a zero-value transaction** — it rides on ordinary payments, which is
   strictly stealthier.
5. **It is independent of C-01 and C-02**, demonstrated with the stock hint and canonical values.

**Exploitation path.** Attacker capability: one registered account. No owner privileges, no relayer
compromise, no race, no modified prover, no pre-existing shared secret with the victim, and no funds.
Read `getPublicValues(n)` and `GetBlckHash()` (both unauthenticated views); build a witness with
itself in one slot and all five peers in the others, real public keys and commitments copied from
chain, `TxValues = [0,…,0]`, and a fresh random `SharedSecrets[i]` for each peer; fill the five
constrained fingerprint cells and leave the other 31 arbitrary; POST to the unauthenticated prover or
prove locally; submit.

**Real-world impact.** Every other institution's entire balance becomes permanently unspendable in
one transaction, for the price of gas. Their money is still counted in `totalSupply`, `check()` still
returns true, and there is no in-protocol way to move it again — no transfer, no withdraw, no burn.
`AUDIT-PROCESS.md`'s canonical Critical example is "it's easy to steal or destroy users' funds"; this
is exactly that, on a LIVE path, with a working demonstration. It is also the exact property
`protocol_description.md:284` argues cannot happen — the specification's argument that a misbehaving
payer can only harm itself runs backwards in the implementation.

**Remediation.** Bind each recipient's shared secret to something the recipient controls. The data
needed is already published and simply never read: have `Enygma.sol` compare the 36 fingerprint
signals against an on-chain registry of `Poseidon(s_ij)` values established at key-agreement time, or
require the sender to prove `SharedSecrets[i]` matches the recipient's registered view key. As an
interim mitigation with no circuit change, have every institution run a monitor that recomputes its
expected `MessageTags` value for each epoch and alarms on a mismatch — that at least converts silent
destruction into a detected incident. Independently, add a proof-carrying recovery path so a frozen
balance can be re-established without zeroing it.

---

### 5.2 High

---

#### H-13 — `burn()` is an unproven plaintext subtraction on a hidden balance

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**
*(The register classified this LATENT on the grounds that `burn`'s only caller in the tree is a test.
The validator classified it LIVE: it is a normal issuer operation on any real deployment.)*

**Description.**

```solidity
// Enygma.sol:276-308
function burn(uint256 accountId, uint256 amount) external onlyOwner returns (bool) {
    if (amount > CurveBabyJubJub.P) revert BurnExceedsModulus();
    (uint256 negCommitX, uint256 negCommitY) = pedCom(CurveBabyJubJub.P - amount, 0);
    _propagateBalancesExcept(accountId);
    Point storage accountBalance = balanceCommitments[lastBlockNum][accountId];
    (uint256 newX, uint256 newY) = CurveBabyJubJub.pointAdd(
        accountBalance.c1, accountBalance.c2, negCommitX, negCommitY);
    ...
    emit BurnSuccessful(accountId, amount);
}
```

The only guard is `amount > P`. **There is no check that `amount` does not exceed the account's actual
balance — and there fundamentally cannot be one, because the balance is a Pedersen commitment the
contract cannot open.** Burning more than the balance yields `Com(balance − amount + P, r)`, a value
just below `P ≈ 2^250.6`.

**That wrapped balance is spendable.** The transfer circuit range-checks the sender's balance with
`ToBinary(v, 252)`, and `2^252 > P`, so a value near `P` is representable and passes; the solvency
comparator `previousV ≥ v` is then satisfied trivially. Note the interaction with C-02: there, a
*prover* exploits the 252-vs-251-bit gap. Here **the contract itself manufactures an aliased value
through ordinary administrative use** — no attacker and no malformed proof required.

**Second half — supply accounting.** `totalSupplyAmount` is written in exactly one place, `mintSupply`
at `:246`. `burn` never decrements it and never updates the homomorphic `totalSupplyX/Y` pair, so
after any burn `Σ balances` no longer equals recorded supply in **either** representation and
`check()` reverts `BalanceMismatch` permanently. Nothing calls `check()` outside tests, so this is
silent.

Also note `:280` uses `>` where `>=` is meant, so `amount == P` is a silent no-op that still emits
`BurnSuccessful`.

**Why this stays High despite being owner-only.** The validator considered grading it Medium for
consistency with M-01 (also owner-error-triggered) and rejected that, on **preventability**: with
M-01, an owner can check that a verifier address has code before setting it, so care suffices. With
`burn`, the owner **cannot** determine whether they are over-burning, because the balance is hidden by
design. Care does not help; the operation is unverifiable by construction. The failure is silent —
no revert, no distinguishable event — and the consequence is a ~2²⁵⁰ spendable balance at a member
institution.

**A remediation-ordering warning that must survive into any fix.** Subtracting `amount·G` from
`totalSupply` inside `burn` — the obvious fix for the second half — makes `check()` pass again *after
an over-burn*, making the first half **more** invisible. Do not fix the accounting without fixing the
proof.

**Remediation.** Require a zero-knowledge proof for `burn` exactly as `transfer` does: the account
proves `balance ≥ amount` and the correct new commitment, rather than the contract performing
plaintext arithmetic on a hidden value. Then decrement `totalSupplyAmount` and update
`totalSupplyX/Y`. Fix `>` to `>=` at `:280`. Add a caller for `check()` so invariant breaks surface.

---

#### H-01 — Sender anonymity collapses from k = 6 to k = 1 for a passive chain observer

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**Description.** The product's headline privacy property is that a payment is indistinguishable among
the six members of its anonymity set. It is not. The validator broke it by execution against the
**real, unmodified gnark proving server** (`gnark-server/` copied byte-for-byte, started with
`go run ./cmd/server/main.go`, real `keys/EnygmaPk.key`, real Groth16 proofs), with the observer given
**only the 80 public signals that `Enygma.transfer()` puts in calldata**.

Four mechanisms were filed. The validator's per-mechanism verdict, which corrects the register:

| # | Mechanism | Verdict | Adversary | Transactions |
|---|---|---|---|---|
| 1 | Message-tag symmetry + epoch-constant anchor | **CONFIRMED — protocol-level, asserted in-circuit** | any passive chain observer | **2** in one epoch |
| 2 | Fingerprint matrix: a single non-zero column *is* the sender | **CONFIRMED with correction** — holds for the documented client and one of the demo's two branches | any passive chain observer | **1** |
| 3 | Fingerprint as a permanent pairwise relationship graph | **CONFIRMED but over-graded** — real, but contributes nothing to k = 1; **Low** on its own | any passive chain observer | 1 |
| 4 | The relayer sees the submitting bank | **CONFIRMED — different adversary class** | relayer operator and on-path network observer only, **not** a chain observer | 1 |

The merged claim "k = 1 for every adversary" survives — mechanisms 1+2 cover the chain observer and 4
covers the relayer — but **it is not true that all four mechanisms give k = 1**, and the register's
sentence *"Both shipped client constructions leave everything but the sender's column zero"* is wrong
as written.

**Evidence.** Bank 0 sends two transfers in the same epoch (identical `block_number = 3000`):

```
$ go run . sparse
T1: proof OK, 80 public signals
T2: proof OK, 80 public signals

[H-01 #2] FingerPrint matrix (public signals 0..35), non-zero map for T1:
   column 0: 5 non-zero entries
   column 1: 0 non-zero entries
   ... columns 2-5: 0 non-zero entries
   => EXACTLY ONE non-zero column: slot 0. AnonymitySet[0] = 0  => SENDER (k=1 from ONE tx)

[H-01 #1] Message tags (public signals 73..78): T1 vs T2, same epoch anchor
   identical tags at identical slots: 5/6 ; differing slots: [0]
   => the only slot that changed is 0 => AnonymitySet[0] = 0 is the SENDER of BOTH
   epoch anchor signal[66]: T1=3000  T2=3000  (equal: true)
   nullifier signal[79]:    T1=191224004792...  T2=964956250563...  (equal: false)
                            <- distinct, so BOTH transactions are accepted on chain
```

With the demo's *full-matrix* branch, mechanism 2 is defeated but mechanism 1 is not:

```
$ go run . full
[H-01 #2] ... column 0..5: 5 non-zero entries each
   => 6 non-zero columns: this matrix does NOT single out the sender
[H-01 #1] identical tags at identical slots: 5/6 ; differing slots: [0]
   => the only slot that changed is 0 => AnonymitySet[0] = 0 is the SENDER of BOTH
```

And direction symmetry:

```
$ go run . cross
  tag at T1 slot 1 (bank0->bank1) == tag at T3 slot 0 (bank1->bank0) : true
```

**Affected code.** `gnark-server/pkg/circuits/enygma/circuit.go:27, 118-143, 183-193`;
`enygma/handler.go:155-160`; `contracts/enygma/contracts/Enygma.sol:32-35, 44, 641-645, 734-771`;
`demo/main.go:211-213, 952-960, 978-1008, 1533-1534, 1569-1608`; `demo/fingerprint_matrix.json`;
`go_client/internal/randomness/operation.go:31-63`; `go_client/agreement/manager.go:174-180`;
`relayer/server/handler.go:133-295`; `relayer/main.go:28`.

**Why mechanism 1 cannot be fixed client-side.** The circuit **asserts**
`MessageTags[i] == Poseidon(Poseidon(12), SharedSecrets[i], BlockNumber) mod P`, and `SharedSecrets[i]`
must be the genuine pairwise secret or the recipient cannot derive its own blinding factor and open
its new balance. The secret is direction-agnostic (`pairKey` canonicalises to `min_max`;
`demo/main.go:1533-1534` writes the matrix symmetrically), and `blockNumber` is the epoch anchor, not
the current block. Every refutation was tried and failed: the nullifier does *not* limit a bank to one
transaction per epoch (the two same-epoch transfers produced different nullifiers, shown above);
`blockNumber` does not change per transaction; a careful client cannot randomise the tags.

**Mechanism 2 is a client bug, not a protocol break** — and that matters for the fix. 31 of the 36
fingerprint entries are unconstrained public inputs, so a client can fill them with anything at zero
cost; one line in each client closes it. There are **three** shipped constructions, not two:
`go_client/enygma_test/transaction_test.go:104-124` (sparse — and this is the client
`demo_instructions.md` Step 4 documents as the end-to-end flow), `demo/main.go:983-1007` fallback
branch (sparse), and `demo/main.go:983-986` primary branch (full symmetric matrix, does *not* leak).
A wrinkle worth reporting: `demo/fingerprint_matrix.json` is git-tracked and contains a matrix in
which only the pair {0,1} is non-zero, so on a fresh clone proving would fail for the other four
banks — the demo's realistic states are "run key agreement first" (no leak) or "delete the file"
(leak).

**Structural aggravators, verified.** The anonymity set is never *selected* — every shipped client
hardcodes all six banks — so the "5 of 6 tags match" test always has the same slots to compare.
Participation is never hidden: `participantIds` is plain calldata under real-world identities. And
calldata is permanent, so **the exposure is retroactive**: fixing this tomorrow does not
un-deanonymise what is already on chain.

**Exploitation path.** Attacker capability: **read access to the chain. No keys, no registration, no
network position.** Fetch every `transfer()`; if signals 0–35 have exactly one non-zero column `c`,
the sender is `AnonymitySet[c]` — one transaction, no computation. Otherwise group by signal 66 and
find two transactions in the group differing in exactly one tag slot. Feed the recovered labels into
H-02 to attach amounts.

**Real-world impact.** For an inter-bank settlement system, the directed counterparty graph — who paid
whom, how often, in what sequence — is itself commercially sensitive. The system's headline privacy
property does not hold against the weakest attacker in its own threat model. Combined with H-02, the
observer obtains a fully labelled plaintext payment matrix. **High, not Critical:** no funds move and
no soundness is broken; `AUDIT-PROCESS.md` caps "reads sensitive user data" at High.

**Remediation.** Mechanism 1 requires a protocol change: mix a per-transaction value (not the epoch
anchor) into the tag derivation, and make the pairwise secret direction-asymmetric so that
`tag_{A→B} ≠ tag_{B→A}`. Mechanism 2 is one line per client: fill the whole fingerprint matrix, or
better, remove the 31 unconstrained public signals from the circuit's public interface entirely —
they are read by nothing on chain. Mechanism 3: mix a rotating value into `Poseidon(s_ij)` so the
pairwise identifier is not permanent. Mechanism 4 is closable by deployment (TLS plus a shared
submission proxy) without touching the protocol.

---

#### H-02 — Epoch-constant, direction-symmetric blinding factors open an entire settlement window's payment graph

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED**

**Description.** `r_i = Poseidon(Poseidon(21), s_i, blockNumber) mod P`, and `blockNumber` is the
**epoch anchor** (`lastBlockNum`), not the current block — so `r` is constant for a whole epoch. And
because `s` is one value per *unordered* pair, `r_{A,B}(E) = r_{B,A}(E)`: an anchor learned while A
was paying B opens transactions in which **B is paying A**. Subtracting two commitment deltas at a
common slot cancels the `H` component exactly, leaving `(v₁ − v₂)·G`, and the protocol *requires*
amounts to be small enough to solve that discrete log by table lookup — the legitimate recipient
recovers `v` from `v·G` the same way.

**Evidence — real prover, real proofs, observer sees only calldata.**

```
[H-02] Subtract the two commitment deltas slot-by-slot (signals 54..65):
   slot 0: C1-C2 = (100)*G  -> H CANCELLED EXACTLY; v1-v2 = 100 (ground truth 100) true
   slot 1: C1-C2 = (60)*G   -> H CANCELLED EXACTLY; v1-v2 = 60  (ground truth 60)  true
   slot 2: C1-C2 = (40)*G   -> H CANCELLED EXACTLY; v1-v2 = 40  (ground truth 40)  true
   slot 3: C1-C2 = (-120)*G -> H CANCELLED EXACTLY; v1-v2 = -120 (ground truth -120) true
   slot 4: C1-C2 = (-80)*G  -> H CANCELLED EXACTLY; v1-v2 = -80 (ground truth -80) true
   slot 5: C1-C2 = (0)*G    -> H CANCELLED EXACTLY; v1-v2 = 0   (ground truth 0)   true
```

Cross-direction (T1 = bank0 pays bank1 60; T3 = bank1 pays bank0 75, same epoch):

```
  tag at T1 slot 1 (bank0->bank1) == tag at T3 slot 0 (bank1->bank0) : true
  T1.delta[bank1] - T3.delta[bank0] = (-15)*G, H cancelled: true  (ground truth 60-75 = -15)
```

An earlier global-audit harness, re-run during validation, performed the full anchored version end to
end: from **one all-zero dummy transaction** it recovered **all four senders and every amount**, and
correctly reported "no anchor for this pair" where none existed — i.e. the attack does not over-claim.

**The two load-bearing premises, settled.**

*(a) Can one bank land two transfers inside one epoch?* **Yes.** `GetBlckHash()` returns
`lastBlockNum`; every mutator sets it to `epochStart`; `_verifyBlockNumberFP` compares signal 66
against the **stored** value, not against `_currentEpochStart()`. Nothing rate-limits a bank per
epoch — the nullifier includes `prevR`, which advances after every transfer *by design*
(`Enygma.sol:640-643` says so in a comment), and the two same-epoch transfers above produced different
nullifiers. The developers' own tests assume this: `epoch_test.go:3-14` asserts epoch-slot behaviour,
and `sequential_transfer_test.go` performs exactly two back-to-back bank-0 transfers against
`epochInterval = 30`.

*(b) Does `EPOCH_INTERVAL = 1` defeat it?* **Yes — but it is not the shipped default.** With interval
1 no anchor is ever used twice, and both this finding and H-01's tag mechanism collapse. But
`deploy_direct.py:24` defaults to **30** and that is the documented deploy (`run_scripts/README.md`
and `demo_instructions.md` §3 both instruct exactly that script and nothing else); every Go
integration test deploys with 30; and `deploy_node.js`, which defaults to 1, is **referenced by
nothing in the repository** — a grep for `deploy_node` matches only its own header comment. Interval 1
is also not a free mitigation: it caps the whole system at one accepted transaction per anchor, which
is M-05. **There is no setting that is simultaneously private and live.**

**Independently confirmed: balances are public until the first shielded transfer.** `registerAccount`
sets the initial balance to `Com(0, randomness)` with `randomness` a public calldata argument
(`Enygma.sol:191-204`); `mintSupply` adds `Com(amount, 0)` — blinding **zero** — and emits the amount
and recipient in the clear (`:222-268`); `burn` likewise. So an account's exact balance is fully open
to any chain observer until it makes its first shielded payment, and permanently open if it only ever
receives issuance. The specification's own answer to this — private issuance — is unimplemented
(M-12).

**Other refutations that failed.** *"`r` is per-transaction"* — only the sender's own slot changes;
receiver slots are epoch-constant by circuit assertion. *"Only differences leak"* — every shipped
client pads unused slots with `v = 0`, so most pair-slots have a known-plaintext occurrence in any
epoch with two or more transfers; in the run above slot 1 was `+60` in T1 and `0` in T2, so the
difference *is* the amount. Worse, `protocol_description.md:402` **recommends** an all-zero dummy
transaction as a privacy measure — which publishes `−r_{S,X}(E)·H` in the clear for all five pairs at
once, handing the observer the exact key it needs.

**Affected code.** `go_client/internal/randomness/operation.go:14-29, 65-82`;
`gnark-server/pkg/circuits/enygma/circuit.go:258-307`;
`contracts/enygma/contracts/Enygma.sol:629-630, 643-645, 191-205, 267`;
`go_client/agreement/manager.go:174-180`; `demo/main.go:206-210, 884-925`;
`run_scripts/deploy_direct.py:24`.

**Real-world impact.** Amount confidentiality — the property the Pedersen commitments exist to provide
— fails against a passive chain observer with no keys and no privileged position, and the exposure is
permanent and retroactive because calldata is permanent.

**Remediation.** Derive the blinding factor from a **per-transaction** value rather than the epoch
anchor, and make it direction-asymmetric. Do not commit issuance with `r = 0`, and do not publish
registration randomness in calldata. Remove the dummy-transaction advice from
`protocol_description.md` until the derivation is fixed — as written it is an attack, not a defence.
Note the sequencing constraint from **I-04**: the zero-value negation bug currently *prevents* dummy
transactions, so fixing that bug in isolation would make privacy worse, not better.

---

#### H-05 — The demo server exposes unauthenticated owner-privileged routes on all interfaces

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**Description.** `demo/main.go` is not a throwaway: the reachability analysis establishes it is the
**only program in the repository that drives a complete working payment**, `demo_instructions.md` is
its runbook, and a 14 MB compiled `demo/demo` binary is checked in. It binds all interfaces
(`main.go:53`, `listenAddr = ":9090"`, passed to `http.ListenAndServe` at `:1765`) and registers
**14** handlers on a plain `http.ServeMux` (`:1745-1758`) with no middleware, no authentication, no
CSRF token and no Content-Type check; `json.Decode` errors are discarded.

Among those 14 routes, `/run/setup`, `/run/register-bank` and `/run/mint` drive **owner-only**
contract functions using the hardcoded `defaultOwnerKey` (`main.go:61`) whenever `MY_KEY` is unset.
`handleRunMint` (`:398`) is reachable by any unauthenticated caller.

Separately, `main.go:743` emits the **full decimal spend key, twice**, into the SSE log stream:

```go
fc.log(fmt.Sprintf("Bank %d: pk = Poseidon(%s,%s) = %s", bankIdx, sk.String(), sk.String(), ...))
```

The adjacent `view_ek` line at `:742` is correctly truncated to 8 bytes — so this is an
inconsistency, not a uniform policy. And `main.go:328` sets `Access-Control-Allow-Origin: *` on the
`/events` stream, so any web page the operator visits can `new EventSource("http://<host>:9090/events")`
and read it.

**Validator corrections.**

1. **The CORS wildcard is on one route, not all.** It appears exactly once, at `:328`, inside the SSE
   handler. The other 13 routes set no CORS header, so browsers apply same-origin policy to their
   *responses*. The register implied blanket cross-origin read access; that is wrong. The substance
   survives because `/events` is precisely the stream carrying the spend keys.
2. **The route count is 14, not 13.**
3. **Cross-origin *writes* do not need CORS.** The `/run/*` routes are simple POSTs, so a malicious
   page can trigger them regardless of correction 1 — only the response is unreadable. `/run/mint` is
   CSRF-able from any page the operator visits, independently.

**Marginal-risk analysis, to avoid double-counting.** C-06 already books the fact that the six bank
spend keys are committed constants; anyone with the repository has them. **So the cross-origin
spend-key leak adds little marginal risk and must not be presented as the primary harm.** What is
genuinely new in H-05 is **unauthenticated, owner-privileged mutation reachable over the network and
by CSRF**: `/run/mint` mints an attacker-chosen amount using the owner key with no credential at all,
so it is not subsumed by C-06. `/run/setup` and `/run/register-bank` likewise — and per M-06, a repeat
`register-bank` destroys a bank's balance while the demo logs *"already registered (OK —
idempotent)"*.

**Note the deliberate tension with H-04**, which was downgraded to Medium on a similar
deployment-topology argument. The distinction is intentional: H-04's harm (spend-key disclosure) is
largely already realised by C-06, whereas H-05's mint route requires no key material and is therefore
additive.

**Remediation.** Bind `127.0.0.1:9090`; add authentication to all `/run/*` routes; truncate `sk` in
the log at `:743` exactly as `view_ek` already is at `:742`; drop or restrict the `*` origin.

---

#### H-11 — The Pedersen value generator `G` has no verifiable provenance

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED**
***This is an unfalsifiable trust gap, not a demonstrated compromise. The report does not claim
anyone holds the relationship.***

**Description.** Pedersen binding requires that nobody knows `e` with `G = e·H`. The project fixes `H`
by a public nothing-up-my-sleeve derivation — correctly, verifiably, and reproducibly (see I-01 and
§9). It does **not** do so for `G`. `G` is a non-standard point, deliberately distinct from the iden3
base point (which the codebase keeps separately as `GBabyJub`), and there is no script, comment, test
or document anywhere in the repository that derives it.

**Affected code.** `go_client/internal/curve/curve.go:8-21`; `gnark-server/utils/circuits.go:10-19`;
`gnark-server/utils/utils.go:18-33`; `go_client/utils/utils.go:31-32`;
`contracts/enygma/contracts/CurveBabyJubJub.sol:18-26`;
`gnark-server/pkg/circuits/enygma/circuit.go:176-179, 227-237`.

**What was done to refute it.** The instruction was to try hard to *find* a legitimate derivation.

1. **Self-check first.** The curve implementation was verified to reproduce the standard Baby Jubjub
   cofactor relation `8·GEN == B8` exactly, so the subsequent negative results are meaningful rather
   than artefacts of a broken script.
2. **Both generators are well-formed.** `circuitG` and `H` are on-curve and each has order exactly
   `P` (`P·circuitG == identity`, `P·H == identity`). This is **not** a small-subgroup or
   invalid-point finding.
3. **Not a standard published constant.** `circuitG` is neither iden3 `B8` nor Baby Jubjub `GEN`, and
   is not a small multiple of either (searched k = 1..2000 for both).
4. **The decisive test — the project's own NUMS construction**, recovered from the repository's own
   setup code (iterate SHA-256 from a seed until the digest is a valid x-coordinate; recover `y` by
   Tonelli–Shanks; fix the sign; clear the cofactor with three doublings):

   ```
   H reproduced from seed=1 (terminated on iteration 2)
   G: NOT reproduced by any seed in 0..300
   ```

Earlier auditors independently searched 40 hash-chain outputs, both `y` parities, ±cofactor clearing,
seeds 0–399, and small multiples `k ≤ 200 000` of the base point, of `H`, and of the order-8
generator. Nothing.

**Why the obvious objection is wrong.** "`H` is provably NUMS, so the pair is fine" — no. The
construction fixes `H` by public derivation, but `G` is a *free choice*. Whoever chose `G` could have
computed it as `G = e·H` for a random `e` they picked, and retained `e`. `H` being NUMS constrains
`H`, not the relationship. With `e` known, commitments open to any value, the range proof then
validates a fabricated balance perfectly honestly, and total supply loses its binding entirely.
Searching small multiples `H = k·circuitG` for k ≤ 5000 found none, but that is **not exculpatory** —
an adversarial choice would use a large random `e`, and that space is computationally infeasible.

**This is not the "discrete log relation" false-positive pattern.** That pattern applies when the
points *are* outputs of a group hash. Here one is and one demonstrably is not, and the asymmetry is
what makes the finding pointed rather than pedantic: the developers demonstrably knew how to construct
a NUMS generator — they did it for `H` — and did not do it for `G`.

**Severity rationale.** High because it defeats a security property the specification explicitly
claims and explains the importance of ("if an entity knows the relationship between the generators…
then such entity can open their commitments in any way they want"), and because binding is what holds
total supply together. It is the same category of unfalsifiable trust gap as H-12. The difference is
that here the fix is cheap and total.

**Remediation.** Re-derive `G` by the same published hash-to-curve construction already used for `H`,
with a different published seed; document both derivations; ship the derivation script; and add a CI
assertion that re-derives both and compares them against every hardcoded copy. Redeployment and
re-issuance are required, since existing commitments are under the current `G`.

---

#### H-12 — The Groth16 trusted setup is a single `groth16.Setup` call on one machine

**Severity: High · Exploitability: LIVE · Verdict: CONFIRMED**
***Also an unfalsifiable trust gap. No evidence exists that any trapdoor was retained, and the code
gives the process no opportunity to persist one deliberately.***

**Description.** `protocol_description.md` §"ZK Trusted Setup (Groth16)" describes an MPC ceremony
across Privacy Nodes and rests its security argument on the assumption that *"at least one of the
institutions will abide by the protocol and preserve the security of the trusted setup stage."* The
implementation is `n = 1`.

```
gnark-server/cmd/setup/main.go:289:      pk, vk, err := groth16.Setup(ccs)
gnark-server/keygen/generate_keys.go:31: pk, vk, err := groth16.Setup(ccs)
```

Zero matches repo-wide for `Contribution`, `Phase1`, `Phase2`, `ptau`, `ceremony`, `transcript`, `MPC`
or `beacon`. `.gitignore:2` ignores `*.ptau` and no such file exists; gnark's `mpcsetup` package is
imported nowhere. There is no contribution transcript, no attestation, no randomness beacon, and no
published hash chain a Privacy Node could check.

**The argument does not degrade — it inverts.** A ceremony's security comes from requiring *all*
participants to be corrupt before the trapdoor is recoverable. With a single party, exactly one entity
need be corrupt, careless, or compromised — and that party is not a licensed institution but whoever
ran `go run ./keygen`. The n-of-n-must-collude guarantee the specification sells to institutions does
not exist.

**Six independent trapdoors for one statement.** Compounding this, M-16 established that all six
`WithdrawVerifier1..6` prove the *identical* statement via six separate `groth16.Setup` runs.

**Scope after other validators' corrections.** This finding carries the trapdoor concern for the whole
system:

- C-06's validator **removed the proving keys from that finding** — they are public parameters by
  design and disclose nothing. The setup concern lives here, not there.
- H-16's validator established there is **no foreign setup**: the mismatched zkdvp verifiers trace by
  git history to the developers' own superseded commits, and H-16's forgery half was dropped
  entirely, transferring that concern here.

**Two things stated fairly.** (i) The committed keys themselves leak nothing — recovering the trapdoor
requires discrete log in BN254, and gnark neither returns nor persists it. The honest phrasing is
"dropped on the floor by the Go runtime, with no transcript establishing it was not observed."
(ii) `gnark-server/README.md:19` carries a prominent *"⚠️ Proving keys and Verification Keys are only
for demo purpose ‼️"*. That is a real mitigating factor and is part of why this stays High rather than
Critical. Against it: nothing in the runtime enforces the warning, the contracts tree ships the
matching verifier, and `deploy_direct.py` defaults to Rayls mainnet.

**Honest statement of the risk.** Whoever executed `groth16.Setup` was in possession of the trapdoor
at that moment and could forge a proof for **any** statement — arbitrary mint, arbitrary spend
authority, arbitrary nullifier — that the on-chain verifier would accept, with no on-chain evidence
distinguishing it from an honest proof. **This is not a demonstrated compromise.** What is
demonstrated is that the system's soundness depends entirely on an unverifiable, undocumented,
single-party act, and that every institution relying on it is asked to take that on faith while the
specification tells them a ceremony protects them.

**Remediation.** Run a real multi-party ceremony (a Powers-of-Tau phase 1 plus a per-circuit phase 2)
with each Privacy Node contributing; publish the full transcript, per-contribution attestations, and a
final beacon so any participant can verify independently. Regenerate all proving and verifying keys,
redeploy every verifier, and bind each deployed verifier to its verifying key — the control L-04
identifies as absent. Until then, the deployment should be treated as trusted-issuer rather than
trust-minimised, and documented that way to participants.

---

#### C-08 — The withdraw circuit has no solvency comparator at all

**Severity: High · Exploitability: BROKEN · Verdict: CONFIRMED-WITH-CORRECTION**
*(Downgraded from Critical: zero users can be affected on any deployment this repository can produce.
**It returns to Critical the day the bridge is enabled.**)*

**Description.** `withdraw` is the only one of the four circuits with no `prevBalance ≥ amount` check.
`PreviousSenderBalance` occurs in `withdraw/circuit.go` exactly three times:

```
$ grep -n "PreviousSenderBalance" gnark-server/pkg/circuits/withdraw/circuit.go
38:	PreviousSenderBalance     frontend.Variable       // declaration
148:	computedPreviousCommitment := utils.PedersenCommitment(api, circuit.PreviousSenderBalance, circuit.PreviousSenderRandomValue)
299:	PreviousSenderBalance     string  `json:"previous_sender_balance" ...`   // JSON DTO
```

Line 148 is its only appearance inside `Define`. That is a *binding*, not a *bound*: it proves the
prover knows an opening of the on-chain commitment and says nothing about its magnitude. There is no
`ToBinary` on it — not even a 252-bit range check — no comparator, and no indirect route.

The sibling comparison is the finding's core evidence:

```
$ for f in enygma enygma_fee deposit withdraw; do grep -n "api.Cmp" .../$f/circuit.go; done
enygma:      231/232  api.Cmp(previousVConstrained, vConstrained)      + 236 api.Cmp(vConstrained, 0)
enygma_fee:  239      api.Cmp(previousVConstrained, totalDebitConstrained) + 243 api.Cmp(vConstrained, 0)
deposit:     178      api.Cmp(previousVConstrained, vConstrained)      + 181 api.Cmp(vConstrained, 0)
withdraw:    173      api.Cmp(vConstrained, 0)                          <- ONLY the vacuous one
```

**And `api.Cmp(v, 0)` is vacuous — in all four circuits.** Read from the pinned gnark source
(`frontend/cs/r1cs/api.go:566-592`), the comparison is over the **unsigned** big-endian decomposition
of both operands at full field width; there is no sign bit anywhere. With the right operand the
constant `0`, every `bi2[i]` is `0`, so the `-1` branch is unreachable and `Cmp(v, 0) ∈ {0, 1}` for
every field element. It compiles to 3809 constraints that prove nothing:

```
constraints for the 'v >= 0' gadget: 3809
  v = 0                                                     'v >= 0' SATISFIED: true
  v = P-1 (i.e. -1 in the protocol's mod-P encoding)        'v >= 0' SATISFIED: true
  v = P-1000 (i.e. -1000, a debit of 1000)                  'v >= 0' SATISFIED: true
  v = 2^252-1 (largest value the 252-bit ToBinary admits)   'v >= 0' SATISFIED: true
  v = r-1 (field -1)                                        'v >= 0' SATISFIED: false   <- rejected by ToBinary, not by the Cmp
```

`P − 1000` — the codebase's own encoding of a debit of 1000 — passes the "`v ≥ 0`" check. In the other
three circuits this vacuity is harmless only because the *other* comparator is meaningful.

**Evidence — executed with three negative controls, against the committed keys.**

```
$ GOFLAGS=-mod=mod GOPROXY=off go run ./withdrawtest
compiling stock withdraw circuit (unmodified)...
parsed circuit inputs nbPublic=50 nbSecret=54
constraints: 102274

A  balance=0, sender delta=+1e18, DvP payout=1e18                      SATISFIED: true
B  balance=0, sender delta=P-1e18 (= -1e18 debit), DvP payout=1e18     SATISFIED: true
C  CONTROL: claimed balance 101 does not open PreviousCommit[0]        SATISFIED: false
      -> constraint #19350 is not satisfied: 1 ⋅ 8712767…545 != 15322140…324
D  CONTROL: SenderTxValue=7 but TxValues[sender]=1e18                  SATISFIED: false
      -> constraint #537 is not satisfied: 1 ⋅ 1000000000000000000 != 7
E  CONTROL: Hashes[0] is not the Poseidon preimage of the deposit      SATISFIED: false
      -> constraint #96000 is not satisfied: 1 ⋅ 17912810…704 != 0

Prove(committed WithdrawPk1.key) err = <nil>
=== Verify(committed WithdrawVk1.key) err = <nil> => SHIPPED KEYS ACCEPT: true ===
```

Row A is the finding: an account whose balance is **zero** credits itself `+1e18` and instructs the
DvP leg to pay out `1e18`. Row B shows the opposite polarity also works. C/D/E prove the harness is
genuinely constrained — the R1CS rejects a wrong balance opening, a wrong sender amount, and a wrong
deposit preimage.

**Nothing on the contract side compensates.** `_verifyPublicInputs` binds each signal slot to
`keys[accountId]`, `balances[accountId]` and `commitmentDeltas[i]` and nothing else; `_updateBalances`
does `pointAdd(oldBalance, commitmentDeltas[i])` with no aggregate or solvency check. The over-credit
is applied verbatim.

**Reachability — BROKEN, with four gates (the register counted three).**

1. `addWithdrawVerifier` has **zero callers repo-wide** — not even a test, unlike `addFeeVerifier`.
2. `POST /proof/withdraw/1..6` returns 400 unconditionally (M-03). Reproduced:
   `frontend.NewWitness(handler's witness) err = can't set fr.Element with <nil>`.
3. `addZkDvp` likewise has zero callers, and `_executeZkDvpDeposits` reverts against `address(0)`.
4. **The gate the register missed:** the six `WithdrawVerifier*.sol` live in
   `contracts/enygmaverifier/zkdvp/`, outside the sources root of the repository's only hardhat
   project. **No build in the repository ever produces a withdraw verifier artifact to deploy.**

*(Gate 3's framing needed correcting: an implementation of `depositThroughEnygma` does exist, in the
out-of-scope sibling `enygma_dvp`. The accurate statement is not "it does not exist" but "it is never
wired".)*

**Critical correction — M-03 is NOT the mask on this finding.** The register's masked-bug ledger
claims that fixing the four missing witness assignments arms C-08. It does not. After those four lines
are added, `withdraw` still reverts `VerifierNotFound`, then would revert on `_zkDvpAddress == 0`.
And **M-03 never masked anything from an attacker either** — `keys/zkdvp/WithdrawPk*.key` are
git-tracked, and the accepted proof above was produced by calling `groth16.Prove` directly, exactly as
an attacker would. What M-03 actually masks is the bug *from the developers*: because that endpoint
has never returned a proof, nobody has ever exercised the withdraw statement end to end, which is
plausibly why the missing comparator was never noticed. The danger is **sequential, not causal**.

**The fix-order instruction that matters.** Add the solvency comparator to `withdraw/circuit.go`
**before** anyone fixes `withdraw/handler.go`. Changing the circuit changes the verifying key, so it
cannot be retrofitted after a withdraw verifier is deployed — retrofitting later invalidates every
deployed withdraw verifier.

**Remediation.** Add `api.AssertIsEqual(api.IsZero(api.Add(api.Cmp(previousVConstrained, vConstrained), 1)), 0)`
matching the other three circuits, with both operands genuinely range-bounded below `P` (C-02).
Delete the vacuous `Cmp(v, 0)` gadget from all four circuits — 3809 constraints each that prove
nothing — or replace it with a real bound.

---

#### H-14 — `withdraw()` keys its verifier on `depositParams.length`, so empty state arrays void every binding

**Severity: High · Exploitability: BROKEN · Verdict: CONFIRMED**

**Description.**

```solidity
// Enygma.sol:447-480
address verifier = _withdrawVerifiers[depositParams.length];   // :454  <- keyed on depositParams
...
_verifyPublicInputs(proof.public_signal, participantIds, commitmentDeltas);  // :463
_verifyBlockNumber(proof.public_signal);                                    // :466
_consumeNullifier(proof.public_signal);                                     // :469
uint256[] memory c = _executeZkDvpDeposits(depositParams);                  // :472
_updateBalances(commitmentDeltas, participantIds);                          // :477
```

`withdraw` is the **only** entry point where the array that selects the verifier and the arrays that
carry the state bindings are different arrays. `transfer` (`:402`) and `deposit` (`:496`) key on
`commitmentDeltas.length`, and only `DEFAULT_SIZE == 6` is ever registered, so their binding array is
pinned to 6.

With `participantIds` and `commitmentDeltas` empty, `_verifyPublicInputs`'s entire body is inside
`for (i; i < participantIds.length; )` and never runs — **no public-key check, no previous-commitment
check, no tx-commitment check.** `_updateBalances`'s debit loop is likewise a no-op, but its
`lastBlockNum = epochStart` write at `:870` is **outside** the loop and executes unconditionally. And
`_executeZkDvpDeposits` still runs, forwarding caller-chosen `(amount, publicKey)` verbatim.

So the call verifies a proof, checks one block number, burns one prover-chosen nullifier, mints
caller-chosen DvP value, debits nothing, and re-stamps the epoch pointer.

**Evidence** — rebuilt independently from a fresh `solc 0.8.24` compile of the unmodified source (no
hardhat, no artefacts from the target tree, so a stale committed artifact cannot be the source of the
result), deployed to anvil, with `_withdrawVerifiers[1]` registered exactly as an operator wiring the
1-split withdraw circuit would:

```
=== T1: same-epoch, empty commitmentDeltas+participantIds, depositParams len 1 ===
lastBlockNum=0  block=20
bal(1) before = 16082409486018973026396946699769241078204110188776252722064346397351462532095 ...
  tx status: 0x1
  zkdvp totalMinted = 1000000000000000000000000 [1e24]
  bal(1) after  = 16082409486018973026396946699769241078204110188776252722064346397351462532095 ...
  ==> NO DEBIT: balance unchanged
  check() = true
-- replay same nullifier:
error code 3: execution reverted: custom error 0xcad2ae02   # NullifierAlreadyUsed()
-- fresh nullifier x4:
  zkdvp totalMinted = 5000000000000000000000000 [5e24]
  bal(1) final = <identical to "before">
```

`check()` still returns **true** — the shielded ledger's own solvency invariant does not notice value
leaving through the DvP leg.

And the same call issued after an epoch rollover destroys every balance, with **zero value
transferred**:

```
lastBlockNum=30 block=35
  bal(1) = 1608...095 ... ; bal(2) = 8850...313 ... ; bal(3) = 1327...606 ...
  check() = true
  ...mined to block 47; lastBlockNum still 30
  zero-value withdraw tx status: 0x1
  lastBlockNum now = 45
  bal(1) = 0 1
  bal(2) = 0 1
  bal(3) = 0 1
  check() -> execution reverted: custom error 0xca3e0a68   # BalanceMismatch()
  TotalSupply still = 1000000000000000000000000
```

**Refutations that failed.** *"The empty-array call needs `_withdrawVerifiers[0]`, which no sane
operator registers"* — **half true, and it does not save the contract**: `depositParams` and
`commitmentDeltas` are different arrays, so `withdraw([], proof, [{…}], [])` selects
`_withdrawVerifiers[1]` — the ordinary 1-split verifier — while still passing empty state arrays.
Executed; it succeeds. *"`_verifyBlockNumber`/`_consumeNullifier` still bind something"* — only
weakly; neither refers to any account, key or balance, and the same state transition repeats freely
with a fresh nullifier. *"The attacker cannot get a withdraw proof because the prover 400s"* — not a
gate: the proving keys are in git.

**Reachability.** Two owner transactions — `addWithdrawVerifier(v, n)` and `addZkDvp(z)` — and a
counterparty contract that is not in this repository. **No code change is required**, which
distinguishes H-14 from H-15 and C-08. Note that gate 1 alone arms the *destructive* half: it needs no
DvP value to move.

**Real-world impact, three distinct harms.** Unbounded creation of DvP-side value with zero shielded
debit, repeatable per block, invisible to `check()`. Total unrecoverable destruction of every
institution's shielded balance for the price of one zero-value transaction. And free censorship of
every other participant, since the same no-op call re-stamps `lastBlockNum` and invalidates every
in-flight proof.

**A correction the report must carry so a developer does not "fix" the wrong thing:** the attack does
**not** require `depositParams` to be empty, and therefore does not require the odd
`_withdrawVerifiers[0]` registration. `depositParams` carries the payload; the *other two* arrays are
the ones that must be empty. Rejecting `depositParams.length == 0` fixes nothing.

**Remediation.**

```solidity
require(participantIds.length == commitmentDeltas.length, "length mismatch");
require(commitmentDeltas.length == DEFAULT_SIZE, "wrong participant count");
address verifier = _withdrawVerifiers[commitmentDeltas.length];
```

plus binding `depositParams.length` and its contents into the proof's public inputs (C-09), adding the
non-participant propagation pass to `_updateBalances` (H-15), and re-stamping `lastBlockNum` only when
a balance was actually written.

---

#### H-15 — `_updateBalances` performs no epoch propagation, destroying every non-participant's balance

**Severity: High · Exploitability: BROKEN · Verdict: CONFIRMED**

**Description.** `Enygma.sol` has three balance-mutating routines. Two propagate; one does not.

| Routine | Propagates non-participants? | Sets `lastBlockNum = epochStart`? |
|---|---|---|
| `_updateBalancesForTransfer` (`:793-838`) — `transfer`, `transferWithFee` | **Yes**, `:800-813` | Yes (`:837`) |
| `_propagateBalancesExcept` (`:903-919`) — `mintSupply`, `burn` | **Yes**, `:907-917` | (caller does) |
| **`_updateBalances` (`:843-869`) — `withdraw`, `deposit`** | **No. There is no first loop at all.** | **Yes (`:868`), unconditionally** |

Balances live at `balanceCommitments[epochStart][accountId]` and are read through `getBalance`, which
always reads `balanceCommitments[lastBlockNum][accountId]`. So the moment `_updateBalances` advances
`lastBlockNum` into a fresh epoch slot, every account it did not write reads as **zero**. The old
commitments remain in storage at the previous key but no function can address them — `getBalance`,
`getPublicValues` and `check` all key on `lastBlockNum`, and there is no setter. **The loss is
permanent.**

**Evidence — the full causal chain executed end to end.** First, that the arity mismatch really is
what blocks `deposit()` today (with `addDepositVerifier` and `addZkDvp` **already called**, so the
zero-address guard was not the blocker):

```
=== M-14 mask: unmodified Enygma.sol:500 emits selector for uint256[50];
    the real DepositVerifier.sol is uint256[51] ===
  selector Enygma sends : 0x18e2c03f     # verifyProof(uint256[8],uint256[50])
  selector verifier has : 0xcdae3e76     # verifyProof(uint256[8],uint256[51])
  -> custom error 0x09bde339
  InvalidProof() selector = 0x09bde339
```

Then, with the arity fixed, seven registered banks, bank 7 funded, and a well-formed six-participant
deposit executed one epoch after the last state change:

```
=== H-15: deposit() with the [50]->[51] arity fixed, one epoch after the last tx ===
check() before = true
  lastBlockNum=80 block=86 ... mined to 98
  deposit tx status: 0x1
  lastBlockNum now = 95
  bal(1) = 1404...071 1410...000      <- participant, survives
  bal(2) = 2064...038 8728...877      <- participant, survives
  bal(6) = 7277...602 1905...123      <- participant, survives
  bal(7) = 0 1                        <- NON-PARTICIPANT: DESTROYED
  check() -> execution reverted: custom error 0xca3e0a68   # BalanceMismatch()
```

**The register's question is answered yes**, with one presentational nuance: `getBalance` reports
`(0, 1)`, not `(0,0)` — the underlying slot is `(0,0)` and `getBalance` normalises it to the neutral
element.

**Two corrections to how the mask is described.**

**(a) It is not "a two-character typo".** The minimal `50 → 51` change **does not compile**:

```
Error: Invalid type for argument in function call. Invalid implicit conversion from
uint256[51] calldata to uint256[50] calldata requested.
   --> Enygma.sol:505:29:  _verifyPublicInputs(proof.public_signal, participantIds, commitmentDeltas);
   --> Enygma.sol:508:28:  _verifyBlockNumber(proof.public_signal);
```

The real minimum fix is `DepositProof.public_signal → [51]`, the selector string at `:500`, **and**
`[51]` variants of `_verifyPublicInputs`, `_verifyBlockNumber` and `_consumeNullifier`. Still a small,
purely mechanical change any developer makes in one sitting — the masking relationship is intact — but
the report must not tell developers it is two characters, because on discovering it is five edits they
may reach for a different and possibly worse fix.

**(b) `withdraw` is the cheaper door, and it needs no code fix at all.** `withdraw` also calls
`_updateBalances`, and its verifier lookup has no arity defect. With `_withdrawVerifiers[n]`
registered, a zero-value `withdraw` across an epoch boundary wipes everyone:

```
lastBlockNum=30 block=35, then mined to 47
  zero-value withdraw tx status: 0x1
  lastBlockNum now = 45
  bal(1) = 0 1 ; bal(2) = 0 1 ; bal(3) = 0 1
  check() -> 0xca3e0a68  BalanceMismatch()
```

So H-15 has **two** doors, and M-14 masks only one of them.

**Refutations that failed.** *"The propagation happens elsewhere on the deposit path"* — no;
`_initializeBalanceIfNeeded` is called from exactly two places, neither on the deposit/withdraw path.
*"Everyone is destroyed, so it is an obviously-broken feature that testing would catch"* — no;
participants genuinely survive (banks 1/2/6 above), because `_updateBalances` reads `oldBalance`
before the pointer moves. **It is a targeted destruction of bystanders**, which is exactly what makes
it dangerous rather than obviously broken. *"Rollover is rare in practice"* — no; `deploy_node.js` and
the demo use interval 1, and an attacker chooses when to send.

**Real-world impact.** It does not even need malice: the **first honest deposit or withdraw of any new
epoch** destroys every uninvolved institution's money. `totalSupplyAmount` is untouched, so the ledger
is provably insolvent afterwards, and `check()` reverts forever — while nothing in the repository
calls `check()`.

**Remediation.** Give `_updateBalances` the same propagation pass `_updateBalancesForTransfer` has —
or better, delete `_updateBalances` and route `withdraw`/`deposit` through one shared routine so a
future divergence cannot recur. Re-stamp `lastBlockNum` only when a balance was actually written. And
copy the `_propagateBalancesExcept` loop form, **not** `_updateBalancesForTransfer`'s — the latter's
bound is `for (i; i < _totalRegisteredParties; )` over 0-based indices while account ids are 1-based,
so copying it verbatim would import H-03's off-by-one.

---

### 5.3 Medium

---

#### H-04 — Spend keys and every blinding factor are POSTed in cleartext to an unauthenticated proving service bound to all interfaces

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**Verified facts.** `gnark-server/cmd/server/main.go:11` is `router.Run(":" + cfg.Port)` with
`config/config.go:29` hardcoding `Port: "8080"`, not overridable by any env var or flag. Confirmed by
running the real binary:

```
$ lsof -nP -iTCP:8080 -sTCP:LISTEN
COMMAND   PID      USER   FD   TYPE   NODE NAME
main    61770 myaksetig    4u  IPv6   TCP *:8080 (LISTEN)
```

`*:8080` — every interface, not loopback. `pkg/api/server.go:14-29` is the complete route table: a
bare `gin.Default()` and nine `POST /proof/*` with **no middleware of any kind**. The contrast is
real: the relayer *does* have `bearerAuth` on its state-changing group, so this is an omission, not
house style. The body carries `secret_key`, `shared_secrets`, `previous_sender_balance`,
`previous_sender_random_value`, `tx_values`, `tx_random_values` and `sender_tx_value`, and
`secret_key` is the **entire** spend authority — the circuit's only authorisation constraint is
`PublicKey[senderSlot] == Poseidon(sk, sk) mod P`, with no signature and no second factor. During
validation, POSTing `"secret_key": "424242"` in cleartext returned a valid Groth16 proof.
**Negative result preserved:** there is no plaintext retention — gin's Logger never writes the body,
`Recovery` uses `DumpRequest(..., false)`, nothing touches disk; the residual is unzeroed heap.

**Three corrections that drive the downgrade.**

1. **"Can sniff or MITM every spend key in the consortium" is not reachable in any shipped
   configuration.** For traffic to be sniffable, a client must be on a different host from the prover,
   and **no shipped client can be**: `demo/main.go:54` is the const literal
   `http://127.0.0.1:8080/proof/enygma`; `go_client/config/config.go:36` is a literal (note that
   `RelayerURL` immediately above it *is* env-overridable — the prover URL deliberately is not); the
   test clients are localhost. `gnark-server/README.md` says *"This launches the **local** ZK-SNARK
   service"*. In the documented topology the secret-bearing request never leaves loopback, and
   intercepting it requires host compromise — at which point the key is readable from process memory
   anyway. **What is genuinely exposed is the listener, not the traffic.**
2. **The free proving oracle grants almost nothing.** The attacker must supply `secret_key`
   themselves, so no keys are extracted, and the proving key is public and git-tracked, so an
   attacker who wanted proofs generates them locally. The residual is resource exhaustion, which is
   **M-08** and must not be counted twice.
3. **"`sk` is never rotatable" is false.** `registerAccount` has no already-registered guard, so the
   owner *can* overwrite `publicKeys[accountId]`. It is *destructive* rather than impossible (it also
   zeroes the balance and corrupts `check()` — M-06). And per C-06 the six spend keys are already
   public constants, so grading this High on top of C-06 double-counts the same compromise.

**What remains, and why it is Medium.** The most secret-dense endpoint in the system listens on every
interface with no authentication, no TLS and no configuration knob to change either. In the documented
single-host topology nothing external reaches it. It becomes serious the moment an operator does the
obvious scaling step — moving the prover to a shared or dedicated host — at which point every request
crosses the network in cleartext carrying a bank's full spend authority, its plaintext balance and
every blinding factor, with no certificate to warn anyone and no way in the code to do it safely.
Container and pod deployments make this worse: a shared network namespace turns "localhost" into
"anything in the namespace". `AUDIT-PROCESS.md`'s Medium band — *"a serious vulnerability that only
exists if the user has configured the application in a specific, uncommon, way"* — fits exactly.
**If any bank runs the prover on a host separate from its client, this is immediately High.**

**Remediation.** Bind loopback by default (`router.Run("127.0.0.1:" + cfg.Port)`) with an explicit,
documented opt-in for anything else; if a non-loopback bind is offered, require TLS **and**
authentication (reuse the relayer's `bearerAuth`, or better mTLS); make `Port` and `ProofServerURL`
configurable so operators are not forced into an unsafe improvisation; zero the secret buffers after
witness construction; and long term, do not send `secret_key` over a socket at all.

---

#### H-06 — One static shared bearer token authenticates every bank, over plaintext HTTP, with the value published

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**What holds.** `applyRoutes(r, cfg.APIKey, h)` closes one token over the whole `/relay` group, so
every bank presents the identical string. The relayer therefore **cannot attribute a request to a
bank** — no per-caller accounting, no rate limiting, no audit trail, and no revocation short of
restarting with a new token, which simultaneously locks out every legitimate bank. The value
`enygma-test-secret` is published in the git-tracked `contracts/test` and at `demo/main.go:62`. No TLS
exists anywhere in `server.go`, so the token is recoverable on-path even if it were secret. The gate
*does* cover what matters: `/health` and `/relay/info` are unauthenticated (both read-only) and the two
value-moving routes are inside `bearerAuth`.

**Two negative results verified independently rather than inherited.** `relayer/config/config.go:67`
reads `getenv("RELAYER_API_KEY", "")` — the default is the empty string, not `change-me` — and `:94`
hard-fails on empty, so the relayer has **no default token** and refuses to start without one.
`server.go:77` uses `subtle.ConstantTimeCompare`; **there is no timing-oracle finding here.**

**Why Medium.** (1) The token does not authorise value theft — a holder still needs a valid Groth16
proof binding to current on-chain state, and H-09's validator closed five forging surfaces. Token
possession buys the ability to *submit*, not to *steal*. (2) The concrete harm is already booked as
H-10. (3) The credential exposure is already booked as C-06. H-06's independent contribution is the
**design** defect — one shared secret, no identity, no revocation.

**Remediation.** Per-bank credentials or client TLS certificates so requests are attributable and
individually revocable; TLS; and rotate the published token as part of the C-06 response.

---

#### H-07 — Account id 0 and in-range unregistered ids are accepted as payment participants

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION
(materially narrowed)**

**Mechanism.**

```solidity
// Enygma.sol:736-770
(Point[] memory balances, uint256[] memory keys) = getPublicValues(_totalRegisteredParties + 1);
...
uint256 accountId = participantIds[i];
if (uint256(public_signal[FP_PUBLIC_KEY_OFFSET + i]) != keys[accountId]) revert InvalidPublicInputs();
```

**There is no registration check on `accountId` anywhere on the transfer path.** The only thing
standing in for one is Solidity's array bounds check.

**Correction 1 — narrower than filed.** Any `accountId > N` reverts with `Panic(0x32)`, so arbitrary
high ids are *not* accepted. The set that is accepted but unregistered is exactly: **id 0**, always;
and **any id in `[1, N]` that was never actually registered**, which is reachable because
`registerAccount` takes `accountId` as a caller-supplied parameter and increments
`_totalRegisteredParties` regardless of the value used. In the documented setup (banks at 1–6, relayer
at 100) `N = 7`, so **id 7 is a second ownerless slot**, while id 100 itself is unreachable. The root
defect worth stating plainly is that **`_totalRegisteredParties` is a count being used as if it were a
high-water mark**.

**Correction 2 — `check()` *does* see the id-0 variant.** The register says the sink is invisible to
`check()`. That is wrong: `check()` sums `i = 1 .. _totalRegisteredParties`, which excludes index 0, so
value moved into id 0 drops out of the accounted sum and `check()` reverts `BalanceMismatch`. State it
as "detected but not attributed, and nothing calls `check()`" rather than as invisibility. Value routed
to **id 7**, being inside the range, *is* genuinely silent.

**Is the value recoverable?** No. Spending from a slot requires proving `pk = Poseidon(sk, sk)` for
that slot's key, and `publicKeys[0]`/`publicKeys[7]` are `0`, which would need a Poseidon preimage of
zero. **Value routed there is destroyed, not stolen — this is a griefing/value-destruction primitive,
not a theft primitive.** That, plus requiring registered-participant status, is why it drops to Medium.

**Why the attacker can do it.** Non-sender `PublicKey[i]` is unconstrained in-circuit and non-sender
`PreviousCommit[i]` need only be on-curve, so a prover populates a slot with `pk = 0` matching
`keys[0]` and satisfies the commitment comparison against `getBalance(0)`. No circuit break needed.

**Fix-order note.** L-10's validator found the CLI is consistently 0-based and would *pass*
`_verifyPublicInputsFP`. **Repairing L-08 without also fixing `generateKIndex` would route ordinary
transfers into account 0 by accident** — turning this from a griefing vector into a routine loss.

**Remediation.** Validate every participant id against registration state explicitly; reject id 0;
track a registered-id set rather than inferring range from a counter.

---

#### H-08 — Contract ownership is `private immutable` with no transfer, renounce, pause, upgrade, or getter

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

The register bundles two independent things. They grade differently and the report splits them.

**H-08a — the committed owner key: downgraded to Low.** `grep -rn 34d091c6` returns exactly five text
hits. Each was checked with its default chain: `hardhat.config.js:11` is inside `networks.hardhat`
(chainId 1337, and there is no other network block); `deploy_node.js:13` is the `OWNER_KEY ||` default
with `CHAIN_ID` and `RPC_URL` both defaulting local; `demo/main.go:61` with local defaults;
`scenario_test.go:469` with an **explicit** guard — `mustPrivKey` uses the constant *only* when the
chain URL contains `127.0.0.1` or `localhost` and otherwise calls `t.Fatal`, and the constant is even
commented *"safe to use only on local Hardhat nodes — never on mainnet."*

The decisive fact the register omits: **the mainnet-default deploy script does not use this key.**

```python
# run_scripts/deploy_direct.py:14-16
OWNER_KEY = os.environ["OWNER_KEY"]   # export OWNER_KEY=<hex> — never hardcode mainnet key
CHAIN_ID = int(os.environ.get("CHAIN_ID", "72957"))
RPC_URL  = os.environ.get("RPC_URL", "https://mainnet-rpc.rayls.com")
```

`os.environ[...]` raises on unset — no fallback. So the dangerous combination (committed default ×
remote chain) exists in neither deploy script. The address `0x0F1013e0e46B97144b25b3131668EF99858BD8D0`
(re-derived offline) has zero balance and **nonce 0** on chain 72957, so it did not deploy the live
Enygma and any transaction it signs there fails loudly for insufficient gas. Two residual holes remain,
both bounded: `demo/main.go:611-614` falls back to the committed key regardless of target chain while
`RPC_URL`/`CHAIN_ID` are freely overridable; and `demo_instructions.md:83` prints that key's address as
the expected deployer of a script whose default target is Rayls mainnet (M-07). **Low: a credential
hygiene and reuse hazard, not a live compromise.** Reached independently of, and agreeing with, C-06's
downgrade.

**H-08b — immutable ownership: Medium, and worse than the register says.**

```solidity
// Enygma.sol:58
address private immutable _owner;
```

`grep -rn "function owner\|getOwner\|_owner"` over the contracts and interfaces returns **exactly
three lines**: the declaration, the modifier comparison, and the constructor assignment.
`grep -c -i "pause|upgrade|proxy"` → **0**. So: no `transferOwnership`, no two-step handover, no
`renounceOwnership`, no `setOwner`, no pause, no upgrade path, no proxy — and **no `owner()` getter at
all**, so the administrator of a deployed instance is not even readable on-chain. The register's own
validation question ("read `owner()` on the live contract") **cannot be answered, because the function
does not exist.** For a permissioned financial contract, an unauditable admin is itself a defect.

Nine `onlyOwner` entry points are permanently held by that address. Two are catastrophic in
combination with confirmed findings: **`addVerifier` × M-01** — a `delegatecall` to a codeless address
returns success, so `addVerifier(<codeless address>)` disables proof verification for the entire
contract, forever, in one transaction, undoable only by the same compromised key; and
**`registerAccount` × C-06 §7** — re-registration overwrites the public key *and* wipes the balance,
so the owner can seize or destroy any institution's position.

**Why this is a real defect and not "immutable by design".** The contract is *not* ownerless; it has a
fully-privileged permanent administrator with mint, burn, seize and verifier-replacement authority.
Key compromise, key loss and staff turnover are **likely** events over the lifetime of a settlement
rail. The industry-standard mitigation — deploy from an EOA then hand ownership to a multisig or
timelock — is impossible after the fact. A partial mitigation the register misses: because
`_owner = msg.sender`, a Safe or timelock *can* be the owner if it performs the deployment itself, so
the defect is "no rotation ever", not "multisig impossible". Recovery after compromise requires
redeploying and migrating all state, and there is no migration function; the only way to re-establish
balances is `registerAccount` + `mintSupply`, which breaks `check()` permanently. Role separation is
absent: issuer, registrar and verifier-administrator are one EOA.

**Medium**, because it requires an independent precondition and confers no attacker capability on its
own, but when the precondition occurs the outcome is unrecoverable and the standard mitigation is
unavailable.

**Remediation.** Add a two-step `transferOwnership`, an `owner()` getter, and role separation (issuer /
registrar / verifier-admin). Deploy from a multisig or timelock so ownership is at least held by a
rotatable structure. Add a pause. Remove the committed key from all five sites and from git history
(C-06).

---

#### H-09 — The relayer is an undocumented single trusted intermediary that can censor and totally order every payment

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

**What is confirmed.** The relayer is the sole submitter, by design and in fact:

```go
// relayer/config/config.go:11-13
// The relayer is the sole party that holds a private key and submits
// transactions to the blockchain. Banks/clients never touch the key or gas.
```

And banks *cannot* bypass it: `transfer` is `onlyRegistered` on `msg.sender`, every registration path
binds the bank account ids to the **owner's** address, and only the relayer address is registered as a
submitter. No bank has an on-chain identity of its own. It is absent from the specification — verified
by count, not impression:

```
$ for f in protocol_description.md README.md demo_instructions.md; do grep -ic relay $f; done
0
0
0
```

Zero occurrences of the substring "relay" in all three documents, while `protocol_description.md` §6
describes senders submitting their own transactions. The implementation inserts a mandatory trusted
intermediary that the specified, formally-reasoned-about protocol does not contain. It can **censor**
(simply not call `Transfer`; nothing on-chain records the attempt, no client can distinguish refusal
from a chain problem, and a censored bank has no workaround) and **totally order** every submission
through one mutex — of two proofs built against the same state the second always reverts, and the
relayer picks the winner deterministically, for free, with no competing submitter. `msg.sender` in
`TransactionSuccessful` is always the relayer, so the chain's audit trail carries no per-bank
attribution, and with one shared token (H-06) the relayer's own logs cannot supply it either.

**The relayer cannot forge — five surfaces were attacked and all five are closed.** This scoping is
part of the finding's credibility and is set out in §9.

**The "no client ever verifies" half is REFUTED — and that drives the downgrade.** `demo/main.go`, the
only working end-to-end client, performs an independent on-chain check immediately after the relay
call, on its *own* RPC connection, completely outside the relayer:

```go
// demo/main.go:1174-1197
newBal, err := inst.GetBalance(&bind.CallOpts{}, big.NewInt(int64(senderIdx+1)))
expX, expY, pedErr := inst.AddPedComm(&bind.CallOpts{},
    prevBalances[senderIdx].C1, prevBalances[senderIdx].C2,
    txCommit[senderIdx].C1,     txCommit[senderIdx].C2)
if newBal.X.Cmp(expX) != 0 || newBal.Y.Cmp(expY) != 0 {
    fc.emit("verify_balance", "error", "Verify balance", "MISMATCH — got ... expected ...")
    fc.done(false, "Balance homomorphic check FAILED")
    return
}
```

A relayer returning `200 {"txHash":"0x00…"}` without submitting leaves the commitment unchanged, the
comparison fails, and the flow reports **failure**. The reference test flows a bank integrator would
copy do the same (`transaction_test.go:620-653`, `sequential_transfer_test.go:350-360`, and
`cost_report_test.go:521-540`, where a fabricated hash fails outright at `TransactionByHash`).
**Exactly one caller does not verify** — the library CLI `go_client/transaction/main.go:224-231` — and
per L-08 that CLI cannot produce a valid proof anyway, so it never reaches a genuine success either.
The residual risk is that the documented client-library path models the wrong pattern for integrators.

**One latent defect to record.** `participantIds` is never compared against
`public_signal[FP_K_INDEX_OFFSET .. +5]`, even though those slots exist for exactly that purpose. The
binding is only *incidentally* complete — registered banks have distinct public keys, so no
substitution is available today. It becomes exploitable the moment any zero-key or duplicate-key
account exists, which is what H-07 and C-05 describe.

**Why Medium.** The sharpest claimed impact — silent divergence between a bank's books and the ledger
— does not occur with the shipped client; no forging, redirection or replay is possible; and a single
trusted submitter is an explicit, documented-in-code design decision. **Not lower than Medium**
because censorship of a named institution is an explicit attacker goal in this threat model, costs the
relayer nothing, is undetectable from chain data, and the affected bank has no recourse — there is no
second relayer and it cannot sign for itself.

**Remediation.** Disclose the relayer in the protocol documentation. Allow banks to self-submit (bind
`addressToAccountId` per bank rather than all to the owner) so the relayer is a convenience rather than
a chokepoint. Run more than one. Emit the submitting bank's identity, or a per-bank credential, so the
chain carries attribution. And make `go_client/transaction/main.go` verify on-chain as the demo does.

---

#### H-10 — Any bearer-token holder can drain the relayer's funded account or starve its mutex, halting all settlement

**Severity: Medium (register said High) · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

All four mechanisms are real and **every one was demonstrated by execution** against the unmodified
relayer packages (imported via a `replace` directive, real gin engine, mock contract ABI-packing
`transfer(...)` with `EnygmaMetaData.GetAbi()` so the calldata is byte-identical to abigen's):

```
$ GOFLAGS=-mod=mod GOPROXY=off go run .

== 1. auth.GasLimit is non-zero => bind skips eth_estimateGas ==
   cfg.GasLimit=300000000
   go-ethereum v1.14.5 base.go:290 / :334  `if opts.GasLimit == 0 { estimateGasLimit(...) }`

== 2. commitments / kIndex are unbounded ==
   kIndex len=6       -> HTTP 400  calldata=3524 bytes     intrinsicGas=75548
   kIndex len=100000  -> HTTP 400  calldata=3203332 bytes  intrinsicGas=51272500
   commitments len=100000  -> HTTP 400  calldata=6403140 bytes  intrinsicGas=102469440

== 3. size the payload to geth's 128KB txMaxSize (legacypool.go:54) ==
   max kIndex entries under 128KB tx cap: 3990
   tx RLP size = 131057 bytes, calldata = 131012 bytes
   INTRINSIC GAS (paid even though the call reverts) = 2115368
   + Groth16 pairing before `revert InvalidProof` ~= 250000  =>  ~2365368 gas per request
   @  1 gwei: 303 requests empty the 0.717 balance
   @  5 gwei:  60 requests
   @ 20 gwei:  15 requests

== 4. inFlight dedup is keyed on the attacker-chosen proof[0] ==
   proof[0]="1"  (already in flight) -> HTTP 409 {"error":"duplicate transfer already in-flight"}
   proof[0]="2"  (one digit changed) -> HTTP 400  (submitted; total contract calls=21)

== 5. no rate limit: only middleware on /relay/* is bearerAuth ==
   50 back-to-back authenticated requests -> 50 reached the chain, 0 throttled

== 6. /health and /relay/info ==
   GET /health (no auth) -> 200 {"status":"ok"}
   GET /relay/info (no auth) -> 200 {"relayerAddr":"0x…","contractAddr":"0x…","chainId":72957}
```

The `HTTP 400` responses are the "transaction reverted on-chain" branch — **the reverting payload was
signed and broadcast before the relayer learned it would revert.** That is the whole finding,
reproduced.

**Correction — the magnitude was wrong by ~125×.** The register claimed "up to a full block's worth of
gas" (300 M) per request, pointing at `RELAYER_GAS_LIMIT=300000000`. That is not achievable, because
of a constraint neither auditor noticed: geth's `txMaxSize = 4 * 32KB = 128KB`
(`legacypool.go:48,54`). A larger transaction is rejected by the txpool, `bind` surfaces an error, the
handler answers 500, and **the relayer pays nothing**. The measured optimum is ~**2.4 M gas per
request**, and the ceiling is the size cap, not the gas limit — so the register's whole discussion of
which `RELAYER_GAS_LIMIT` is configured is beside the point. Rate is also limited: `txMu` is held
across `WaitMined`, so submissions are serialised at roughly one per block, making the drain take
minutes to about an hour rather than seconds. It is paced, not prevented.

**A second, stronger DoS on the same code path — not in the register.** That same `txMu` is held from
submission until the transaction is mined or `txTimeout = 45s` elapses, with no queue bound, no
concurrency cap, no HTTP timeouts (M-09) and no rate limit. An attacker looping authenticated requests
keeps the mutex permanently contended, so **every honest bank's transfer queues behind attacker traffic
indefinitely** — settlement halts even where gas is free and before the balance runs out. This is
cheaper, faster and more reliable than the gas drain.

**Why Medium.** It is availability-only and fully recoverable by refunding the account; no ledger value
is stolen (the 0.717 native balance is gas money). And in the *shipped* configuration it is largely
redundant: `contracts/test` publishes the token on line 10 and the **private key on line 9**, so anyone
who can mount this attack can simply sweep the balance with one ordinary transfer. **H-10's independent
value is the configuration the developers intend** — rotated key, tokens issued to member banks —
where a single rogue or compromised consortium member can halt settlement for everyone else, and
neither the relayer nor the chain can attribute the abuse, because one token serves all and
`msg.sender` is always the relayer. Note that `GET /health` returns a static `{"status":"ok"}`
throughout, so naive monitoring never fires.

**Remediation.** Let `bind` estimate gas (or pre-simulate with `eth_call`) so guaranteed-revert
payloads are never broadcast; bound `commitments` and `kIndex`; reject negative ids rather than
encoding them as `2²⁵⁶−1`; add per-caller rate limits and a gas budget (which requires per-bank
credentials — H-06); replace the `proof[0]` dedup key with a hash of the whole request; do not hold
`txMu` across `WaitMined`; and make `/health` report chain liveness and the relayer's balance.

---

#### M-01 — Proof verification uses `delegatecall` with no code check

**Severity: Medium (correctly lowered from High) · Exploitability: the `delegatecall` is LIVE on every
transfer; the fail-open *state* is not attacker-inducible · Verdict: CONFIRMED-WITH-CORRECTION**

All four proof sites **do** guard the zero address before calling (`Enygma.sol:454-455`, `:496-497`,
`:660-661`, `:971`), so the *unconfigured* state fails **closed** with `VerifierNotFound`. Reaching the
fail-open requires the **owner** to register a non-zero address that has no code — a `delegatecall` to
a codeless account returns `success = true`, which the contract treats as "proof valid".

**Refutations that failed.** An attacker cannot make a registered verifier codeless: the committed
verifiers contain no `SELFDESTRUCT` (grep: zero hits), and post-EIP-6780 `SELFDESTRUCT` only deletes
code within the creation transaction. Nor is the operator-error path theoretical: neither deploy script
calls any `add*Verifier`, `demo_instructions.md` never mentions `addVerifier` at all, and the address
is copied by hand out of a receipts file that every run of either script overwrites — including local
Hardhat runs, whose deterministic addresses will not have code on a production chain. And branching on
`success` is *correct* given a target with code: the generated verifiers signal failure by reverting
`ProofInvalid()`, not by returning `false`. `addZkDvp` is correctly excluded: it is a high-level call
with a `bool` return, so solc ≥ 0.8 emits an automatic `extcodesize` check and fails closed.

**The storage-context half, assessed.** All eight committed verifiers declare `verifyProof` as
`public view` with **zero** `sstore` instructions, so the storage exposure is inert *today* and
`staticcall` is behaviour-identical for every verifier this repository ships.

**Correction the register needs.** Its validation question invites a wrong conclusion: **`staticcall`
does not fix the fail-open half.** `STATICCALL` to a codeless account returns `1` with empty returndata
exactly as `DELEGATECALL` does. Switching the construct removes only the "a mis-registered *contract*
rewrites `totalSupplyX/Y`, `balanceCommitments`, `lastBlockNum`, `_nullifiers`" class. Closing the
codeless hole additionally requires an explicit `verifier.code.length != 0` check — ideally at the
**call site**, so a verifier that loses its code after registration also fails closed. Both fixes are
free and both are needed.

**Why Medium.** If reached, the impact is catastrophic and silent — every soundness check lives in the
circuit, so a codeless verifier lets any registered institution mint arbitrarily. And the mistake is
**undetectable by the normal smoke test**: with a codeless verifier a *good* proof also succeeds. But
it is not attacker-initiated and no deployment the repository produces exhibits it.

**Remediation.** Switch all four sites to `staticcall`, **and** add `verifier.code.length != 0` at both
the setter and the call site.

---

#### M-05 — Exact `lastBlockNum` equality lets any participant invalidate every outstanding proof

**Severity: Medium · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

Every mutator ends with `lastBlockNum = epochStart` (`:265`, `:304`, `:838`, `:870`), and every proof
is pinned by **exact equality** (`_verifyBlockNumberFP:776-780`, whose comment reads
`// Option 1: Exact match (strict)`). `epochInterval` is `immutable`. At `epochInterval = 1` — the
`deploy_node.js` default and the value `demo/main.go:654` prints as the demo's configuration — every
mutating transaction re-stamps `lastBlockNum`, so **at most one state-mutating transaction can succeed
per block and every other outstanding proof reverts `InvalidBlockNumber`.** That is not merely a
griefing vector; it is a hard throughput ceiling with no attacker present.

**Three corrections to the cost model that must not be carried forward as filed.**

1. **"Proof generation takes ~30 s" is wrong.** C-01's validator measured `groth16.Prove` against the
   committed key at **218–239 ms** across four runs. `README.md:77`'s "334.28 ms" is the honest figure;
   `demo_instructions.md:19`'s "~30 s" describes cold start. This weakens the victim's exposure *and*
   the attacker's advantage symmetrically.
2. **The attacker is not outside the race.** Griefing requires registered status *and* a valid proof
   pinned to the current `lastBlockNum`, so the attacker wins by transaction ordering, not for free.
3. **At `epochInterval = 30` the block-number half is much weaker than implied.** Inside an epoch
   `_currentEpochStart()` is constant, so only a mutator that *crosses a boundary* disturbs the anchor.
   "One transaction invalidates every proof anyone else holds" is unconditionally true only at
   interval 1.

**The correction that matters most: the block number is the *lesser* mechanism.** The epoch-independent
one is the previous-commitment binding — with `k = 6` of 6 registered banks, **every transfer names
every account**, so every transfer invalidates every other pending proof regardless of `epochInterval`.
Relaxing the exact block-number equality therefore does **not** fix the griefing. That binding is
inherent to this account-model design and is also the system's real anti-double-spend control (M-04),
so any liveness work must address both — and per M-04 it must not touch the commitment binding before
the nullifier and the `ModHint` quotient are fixed.

Note the documented tension with H-02 (**KW-5**): lowering `epochInterval` to shrink the blinding-reuse
privacy window makes this strictly worse. **There is no `epochInterval` that is simultaneously private
and live.** Since it is `immutable`, correcting a bad choice requires redeployment.

**Remediation.** Accept a bounded window (`lastBlockNum` within the last *n* epochs) rather than exact
equality — **but only after M-04's nullifier is restored to `Poseidon(domainSep, sk, n_block)` and
C-01's quotient is bounded.** Longer term, restructure so a payment binds only to the sender's own
prior state rather than to all six participants'.

---

#### M-06 — `registerAccount` has no guards of any kind; a repeat call destroys a bank's balance

**Severity: Medium · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION**

`Enygma.sol:191-219` is validation-free. It sets `publicKeys[accountId]`, `viewKeys[accountId]`,
`addressToAccountId[addr]`, resets `balanceCommitments[lastBlockNum][accountId]` to
`pedCom(0, randomness)`, adds that point to `totalSupply`, and increments `_totalRegisteredParties`.
There is **no already-registered guard**, no `accountId != 0` check (contradicting the comment at
`relayer/cmd/register/main.go:107-109`, "The contract only checks accountId != 0" — it checks
nothing), no `publicKey != 0`, no on-curve check, no `viewKey` length check, no proof of knowledge of
`sk^spend`, no uniqueness of `addr`, and the old `addr → accountId` mapping is not cleared.

**A repeat call for a live account therefore resets that bank's balance to `Com(0, randomness)`
(destroying it), adds a second copy of that point to `totalSupply`, and increments
`_totalRegisteredParties` a second time.** Since `_verifyPublicInputsFP` reads
`getPublicValues(_totalRegisteredParties + 1)`, the spurious increments are exactly how **H-07**'s
in-range unregistered ids get manufactured.

`AccountRegistered` at `:216` emits `(addr, _totalRegisteredParties)` — the post-increment counter,
**not** the `accountId` registered — so an off-chain monitor keyed on that event records the wrong
account. `relayer/cmd/register/main.go` passes `--account-id` (**default 100**) straight through with
`publicKey = 1`, `randomness = 0`, empty `viewKey`, and performs no read-back first.

**Refutations.** It *is* `onlyOwner`, so this is not an authorisation bypass — but the owner key is
committed (C-06/H-08) and the demo exposes `POST /run/register-bank` on an unauthenticated mux, whose
handler logs *"Bank %d already registered (OK — idempotent)"* (`demo/main.go:756`) when the repeat call
is made. **The function is documented by its own caller as idempotent and is not.** Separately,
"registering the relayer corrupts `totalSupply`" is **false** and nobody should file it: `pedCom(0,0)`
returns the identity `(0,1)`, so a `randomness = 0` registration adds nothing to `totalSupplyX/Y`.

**Correction — this does NOT cut both ways.** The register records that the missing guard is also the
only recovery from a C-04 freeze, implying a trade-off. Cross-checked against C-04's validation, there
is none: the bailout that actually works is registration at a **fresh, unused id** followed by
`mintSupply`, which an already-registered guard does not block. Re-registering at the **same** id — the
only thing a guard would block — is not a recovery at all; it zeroes the victim's balance and breaks
`check()` permanently. **The guard removes only the destructive path.**

**Remediation.** `if (publicKeys[accountId] != 0) revert AlreadyRegistered();` plus `accountId != 0`,
`publicKey != 0`, and a `viewKey.length` check; emit `accountId` rather than the counter; and have
`relayer/cmd/register/main.go` read `publicKeys(accountID)` and `addressToAccountId(relayerAddr)` first
and refuse on a collision instead of defaulting `--account-id` to 100.

---

#### M-07 — `deploy_direct.py` defaults to Rayls mainnet while the documented procedure says local Hardhat

**Severity: Medium · Exploitability: LIVE — no attacker; triggered by following the project's own
instructions · Verdict: CONFIRMED**

```python
# run_scripts/deploy_direct.py:14-16
OWNER_KEY = os.environ["OWNER_KEY"]   # export OWNER_KEY=<hex> — never hardcode mainnet key
CHAIN_ID = int(os.environ.get("CHAIN_ID", "72957"))
RPC_URL = os.environ.get("RPC_URL", "https://mainnet-rpc.rayls.com")
```

`OWNER_KEY` is the **only** required environment variable; `CHAIN_ID`, `RPC_URL` and `EPOCH_INTERVAL`
all have defaults, and the defaults are the public production chain. Meanwhile
`demo_instructions.md:42-51` says *"Start a local Hardhat node… the chain at `localhost:8545` with
`chainId=1337`"*, `:75-78` gives the deploy command **with no environment overrides of any kind**, and
`:83-84` shows expected output of `Block: 0` — a freshly started local node, which is exactly what the
operator will not get. The guard is inert: `assert w3.is_connected()` succeeds against mainnet, and
`CHAIN_ID` is used only for signing and is never compared against `w3.eth.chain_id`.
`deploy_node.js:13-15` uses sane `1337`/`127.0.0.1` defaults, so the divergence between the two sibling
scripts is the whole of the problem.

Git history sharpens the intent question: commit `3ab07d3` changed the defaults from `1337`/`127.0.0.1`
to the mainnet pair **in the same commit** that replaced a hardcoded `OWNER_KEY` with
`os.environ["MY_KEY"]` and the comment "never hardcode mainnet key". The retarget was deliberate, the
documentation was not updated, and the safety comment shows the author was thinking about exactly this
risk. Also unexplained: `relayer/.env.example` names a **different** Rayls chain id (149401), so the
repository disagrees with itself about the target.

**Counter-evidence recorded fairly.** `go_client/config/address.json`'s
`0xd908BaeE382191D15E479589D9661A9E9b55b827` returns empty `eth_getCode` on 72957, so no Enygma
instance is demonstrated at *that* address.

**Remediation.** Default `CHAIN_ID`/`RPC_URL` to `1337`/`127.0.0.1:8545` to match `deploy_node.js` and
the documentation; require an explicit opt-in for any non-local target; assert
`w3.eth.chain_id == CHAIN_ID` before the first `send_raw_transaction`; and reconcile `.env.example`'s
149401 with 72957.

---

#### M-08 — Proving server: no auth, no limits, a permanent goroutine leak, remote panics

**Severity: Medium · Exploitability: LIVE · Verdict: CONFIRMED**

`pkg/api/server.go:15-28` is the entire server: `gin.Default()` plus nine `POST` routes, with **no**
authentication, no `MaxBytesReader`, no semaphore, no rate limiter and no `Content-Type` restriction.
`cmd/server/main.go:11` is `router.Run(...)`, which constructs an all-zero `http.Server` — every
timeout unset, so a slowloris holds a connection indefinitely. `ShouldBindJSON` does not consult
`Content-Type`, so **every route is drivable from a browser page as a CORS-simple `text/plain` POST**,
which reaches even a loopback-only deployment through a victim's browser.

**The sharpest vector — memory exhaustion — was demonstrated.** `utils.ParseBigInt` discards
`SetString`'s ok flag and returns `nil` for any non-decimal input. That `nil` reaches
`frontend.NewWitness`, which errors — and gnark's `witness.Fill` returns on the first error without
draining or closing the unbuffered `chValues`, permanently blocking the producer goroutine that holds
the whole circuit assignment. gnark's own source comments *"we may leek a chan + producer go routine"*.

```
$ GOPROXY=off go run .
goroutines before=1 after 2000 failed NewWitness calls=2001 (delta 2000)
HeapAlloc before=2397 KiB after=7807 KiB
```

One permanently leaked goroutine per malformed request, surviving an explicit `runtime.GC()`, on the
**cheap** path before any proving, triggered by a ~200-byte POST such as `{"nullifier":"x", …}`.

**The CPU vector, re-scaled.** The register flagged the 100× disagreement between `README.md`
("334.28 ms") and `demo_instructions.md` ("~30 s") as the load-bearing unknown. **Resolved: the README
is right** — 218–239 ms per proof, measured. The "~30 s" figure describes cold start (circuit
compilation plus loading ≈ 141 MB of keys, five of the six withdraw setups being redundant per M-16).
So a request costs ~240 ms of CPU, not 30 s: real but ordinary, with no semaphore and no timeouts so
the queue grows without bound.

**Remote panics — Low on their own.** Binding tags permit 1–6 element arrays while handlers index
`[0..5]` unconditionally, and `FingerPrintofSharedSecrets [][]string` has no `dive`. **Negative result
preserved:** `gin.Recovery()` **is** installed, so these return 500 rather than killing the process —
though each recovered panic still leaves whatever was allocated, and the panic path is cheaper for the
attacker than the prove path.

**Why Medium.** Availability only, but the gnark server is the *only* way to produce a transfer proof,
so taking it down halts settlement for every institution; it is unauthenticated on all interfaces; and
the leak is unbounded, permanent and trivially triggered, with no self-healing short of a restart.

**Remediation.** Authenticate the routes; use an explicit `http.Server` with Read/Write/Idle timeouts;
add `http.MaxBytesReader`; put a bounded worker pool or semaphore around `groth16.Prove` with a
per-request deadline; **make `ParseBigInt` return an error and reject the request before building the
witness** (this alone removes the leak trigger); add `dive` and `len=6` to every array binding; and
require `Content-Type: application/json` so the routes are not drivable cross-origin.

---

#### M-09 — Relayer: no HTTP timeouts, no body limit, and a global mutex held across `WaitMined`

**Severity: Medium · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION (one sub-claim refuted
outright)**

**Sub-claim 1 — no timeouts, no body limit: CONFIRMED.** `relayer/main.go:28` is `r.Run(addr)` =
`http.ListenAndServe`; no `&http.Server{...}` literal exists anywhere in the relayer module, so
`ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` are all zero. The auth-ordering
point is the one that matters: `bearerAuth` is middleware on the `/relay` group, and Go dispatches into
the handler chain only *after* the header block has been fully read — so a connection that never
terminates its headers holds a goroutine and a file descriptor forever **without ever presenting a
token**. Slowloris here is genuinely unauthenticated. No `MaxBytesReader` anywhere; only
`len(req.PublicSignal) > 80` is bounded.

**Sub-claim 2 — global `txMu` across `WaitMined`: CONFIRMED.**

```go
h.txMu.Lock()
defer h.txMu.Unlock()                     // released only at handler return
tx, err := h.instance.Transfer(h.auth, commitments, transferProof, kIndex)
...
ctx, cancel := context.WithTimeout(context.Background(), txTimeout)   // txTimeout = 45s
receipt, err := bind.WaitMined(ctx, h.client, tx)                     // inside the lock
```

Strictly single-flight with a 45-second worst-case hold, no admission control, no queue bound, and
`WriteTimeout == 0` so blocked goroutines are never reaped. Head-of-line blocking needs no attacker.
The censorship-with-deniability amplification is real: a delayed transfer fails the block-number or
commitment binding and the bank sees "transfer transaction reverted on-chain", indistinguishable from
its own stale proof.

**Sub-claim 3 — "permanent nonce gap": REFUTED.** The chain was: `WaitMined` times out → the handler
forgets the transaction → it is later evicted → nonce `n` is never consumed → every later submission
queues at `n+1` **forever**. The last step does not follow. `auth.Nonce` is left nil, and go-ethereum
resolves the nonce **per submission** from the node's pending pool:

```
$ grep -n "func (c *BoundContract) getNonce" -A 8 .../go-ethereum@v1.14.5/accounts/abi/bind/base.go
378:func (c *BoundContract) getNonce(opts *TransactOpts) (uint64, error) {
379-	if opts.Nonce == nil {
380-		return c.transactor.PendingNonceAt(ensureContext(opts.Context), opts.From)
503:func (ec *Client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
505-	err := ec.c.CallContext(ctx, &result, "eth_getTransactionCount", account, "pending")
```

If transaction `n` is still pending the call returns `n+1` and the next submission queues behind it —
temporary, clearing when `n` mines. If `n` is **evicted**, the pool no longer holds it, the call
returns `n` again, and the next submission automatically reuses it. **The exact event the finding
identified as the trigger is the event that repairs the condition.** The "settlement halts
permanently with a healthy `/health`" scenario cannot occur with a nil `auth.Nonce`. It *would* occur
in a relayer that cached and incremented a local nonce; this one deliberately does not. The
contributing file `L9-nonce-gap-stalls-settlement.md` was moved to `issues/invalid/` — the only one of
97 to be invalidated — and the register's associated validation question ("what is Rayls's mempool
eviction policy?") should be struck: whatever the policy, eviction frees the nonce rather than
stranding it.

**What survives from sub-claim 3, at Info level:** on the timeout path the caller gets no `txHash`, so
a bank cannot resolve "settled" versus "never settled" (a retry carries the same nullifier and is
rejected — fail-closed, but diagnostically opaque); there is no gas bump-and-replace; and `/health`
returns a static `{"status":"ok"}` with no chain-liveness signal.

**Remediation.** Replace `r.Run` with an explicit `http.Server` carrying `ReadHeaderTimeout`,
`ReadTimeout`, `WriteTimeout` and `IdleTimeout`; add `MaxBytesReader`; release `txMu` after submission
and wait for the receipt outside the lock (or move to a per-account queue); return the `txHash` on the
timeout path; and make `/health` reflect chain connectivity, the relayer balance and pending-transaction
depth.

---

#### M-11 — ML-KEM layer: no leader, implicitly-rejected ciphertexts accepted, peer view keys manufactured

**Severity: Medium · Exploitability: LATENT · Verdict: CONFIRMED-WITH-CORRECTION**

All three defects hold independently, and **all three were demonstrated by execution** against a
verbatim copy of `go_client/agreement/manager.go`, with two `Manager` sets over two separate
`storeDir`s — exactly as two hosts running `initializeSecrets` would:

```
hostA ss_0_1 = 2034607049565772814290414775664556446935633522038229830157038966675644368045 <nil>
hostB ss_0_1 = 278150359409103707176067656086745012416917246226514007997725553441062564749 <nil>
SECRETS EQUAL: false
bank1 EK identical across hosts: false
GetOrAccept(junk ciphertext): ss=1869025443972686164660146358073524000142408184993580779441762009226187669212 err=<nil>
cached junk secret stable: true
persisted to ss_1_9.txt: true
after corrupting bank_1_ek.bin, New() ok: true EK unchanged: true
```

**Defect 1 — no leader.** `pairKey(a,b)` is order-independent (`min_max`), so both directions name the
same cache slot, but *which* peer encapsulates is fixed only by a doc comment. `GetOrEstablish`
consults `loadCached` first and, on a miss, encapsulates unconditionally. Two banks that each initiate
each persist a **different** value under the same key, and because both entry points short-circuit on
the cache, neither ever re-derives. Nothing anywhere compares the two. `GetOrAccept` — the "follower"
half — has no non-test caller at all. Masked in the demo only because `initializeSecrets` runs every
bank out of one directory.

**Defect 2 — no `id = Hash(s)` confirmation.** `protocol_description.md:135-139` is explicit that the
recipient must recompute `id' = Hash(s')` and check it. The string `id` does not occur in
`manager.go`. FIPS 203 implicit rejection means **any** correctly-sized ciphertext decapsulates with
`err == nil` and a pseudorandom key; the run above fed 1088 bytes of `0xAB` and got a secret with a nil
error, which `persistSecret` then wrote to disk and every subsequent call returns from cache. There is
no invalidation path in the package. Ciphertext files are written mode **0644**. *Scope note, so the
report does not overreach:* implicit rejection yields a key the **attacker does not know either** —
this is silent corruption, not key compromise.

**Defect 3 — peer keys never authenticated, silently manufactured (the most serious).**
`go_client/transaction/main.go:264-288` builds a `Manager` for *every* bank from **one local
directory**, and `loadOrCreate` **generates a fresh keypair for a peer whose seed file is missing**. So
the sender's process generates every peer's *private* seed locally and then encapsulates to public keys
it invented moments earlier. The peer's key never comes from `Enygma.viewKeys`: that mapping exists on
chain and a `ViewKeys` binding is generated, but a repo-wide grep finds **no caller** in `go_client`,
`relayer` or `gnark-server`. **The advertised on-chain PKI is wired to nothing.** On any host that is
not the single demo host, every pairwise secret is unilaterally invented, no counterparty can derive
its blinding factor or tag, and the failure is completely silent.

**Correction the report must carry.** The register states *"anyone with local write access to `./keys`
can plant `bank_<i>_ek.bin`"*. **This is wrong.** `loadOrCreate` reads only the seed path; `ekPath` is
write-only, and `EncapsulationKey()` derives from the seed — the run above corrupted `bank_1_ek.bin`
and the key was unchanged. The substance survives with the right filename: planting
**`bank_<i>_dk.seed`** does yield that slot's private key. But that file is mode 0600 inside a 0700
directory, so the attacker must be the operator user or root — a materially higher bar than implied.

**Why Medium despite being LATENT.** Unlike M-10, no attacker capability is required for the harm:
defects 1 and 3 are self-inflicted and manifest on the first real multi-host deployment, silently, in
the component the README and specification present as the post-quantum foundation of the whole system.
It is LATENT only because L-08 (a one-string fix) currently stops the CLI, and `go_client/agreement/`
is the only real ML-KEM code in the tree.

**Remediation.** Define and enforce a leader rule (lower id encapsulates) and have both sides confirm
with `id = Hash(s)` before caching; treat a decapsulation whose confirmation tag does not match as a
hard failure and never persist it; source every peer's encapsulation key from `Enygma.viewKeys`, and
**refuse to run** if a peer's key is absent rather than generating one; and set ciphertext file modes
to 0600.

---

#### M-12 — The documented encryption, rotation, retrieval and auditing layers do not exist

**Severity: Medium · Exploitability: LIVE (the claims are published today) · Verdict: CONFIRMED**

**The framing this finding was held to.** A research prototype is allowed to be unfinished;
"unfinished software exists" is not a finding. The narrower question tested is: *does the
documentation state these mechanisms as present properties of the system, rather than as roadmap?*
It does.

**Absence, verified by execution:**

```
$ grep -rniE "aes|gcm|hkdf" <target> --include=*.go --include=*.sol --include=*.js --include=*.py
(no matches)
```

Widening to the whole tree, the only hits outside documentation are `.gitignore:1 (*.aes)`, a `go.sum`
line, and `formal_methods/lean/EphemeralKeys.lean`, where `HKDF` is declared as an **axiom** rather
than implemented. There is no AES-GCM-256 payload encryption, no `K = HKDF(s, n_block)` rotation, no
key-agreement bulletin board, no "store encrypted payload" step, no tag-scanning or `vG → v` retrieval
path, no private issuance, and no auditor role, address, escrow or event anywhere. The one long-term
secret that does exist is derived once, cached in memory and on disk, and never rotated — the opposite
of the documented per-block rotation.

**The documentation asserts these as present.** `protocol_description.md` uses the present indicative:
`:1` *"the system **supports** an Auditing step"*; `:274` *"A transaction payload **includes** a set of
k ciphertexts (encrypted using AES-GCM-256) … **These ciphertexts are encrypted with an ephemeral
symmetric key that is rotated in every new block**"*; `:278` the HKDF formula; `:363` the retrieval
path; `:405-447` the whole §7 auditing subsystem. `README.md:4` links it as "A more formal protocol
description", and the README's own primitives diagram lists AES-GCM-256 and HKDF as branches of the
crypto stack, immediately above measured performance figures for the shipped artefacts.

The exculpatory framing was searched for and is absent:

```
$ grep -niE "future work|not yet|roadmap|planned|to be implemented|prototype|proof of concept" \
    protocol_description.md README.md
(no hits except README's two "We intend to …" sentences, about a post-quantum SNARK migration and
 proof outsourcing)
```

**That is the strongest evidence in the finding:** the authors demonstrably distinguish "we intend to"
from "the system does", and every mechanism here sits on the "the system does" side. Three of the
specification's own *security arguments* therefore rest on premises the code does not supply.

**And it could not deliver the promise even if built.** The object that opens a transaction is the
Pedersen blinding `r`, not the symmetric key `K` — and because `n_block` is the **epoch anchor**, one
disclosed `r_{i,j}` opens **every** transaction between that pair for the whole window, in both
directions (H-02). The promised granularity is off by a factor of `epochInterval` *and* by the number
of transactions in the epoch. The secret is *joint*, so disclosing your own view key discloses your
counterparties' amounts without their consent. And rotation is **structurally unimplementable**:
`registerAccount` is the only writer of `viewKeys`, and re-registering destroys the balance,
double-adds to `totalSupply` and inflates the party count (M-06). **The only auditing mode this design
can deliver is the permanent, universal one.**

**Remediation (cheap, and worth doing regardless).** Mark every unimplemented mechanism as "design,
not implemented" in both `README.md` and `protocol_description.md`. Separately, ask the maintainers
who has been shown `protocol_description.md`: if institutions have relied on it, every absent
mechanism is a mis-set expectation. And redesign the disclosure mechanism before promising scoped
auditing — per-transaction, revocable, consent-respecting disclosure is not achievable with this
construction.

---

#### M-13 — `transferWithFee` never reads signals 50–53, so the fee is destroyed with no recipient

**Severity: Medium · Exploitability: LATENT · Verdict: CONFIRMED**

`Enygma.transferWithFee` is five calls and an event. All four helpers were read; the only indices any
of them touches are the offset constants at `Enygma.sol:18-30` — public keys at 6, previous commitments
at 12, tx commitments at 24, block number at 36, nullifier at 49. **Maximum index read anywhere: 49.**
Nothing reads 50 (`Fee`), 51–52 (`SumTxCommit`) or 53 (`SumTxValuesWithFee`). The whole contract was
checked for a fee recipient — an address, a treasury variable, a credit in the update path, an event —
and **there is none. The fee has no destination.**

Meanwhile the circuit hard-asserts that the pool shrinks:

```go
// enygma_fee/circuit.go:205-222
feeCommit := utils.ScalarMul(api, utils.G, circuit.Fee)
txCommitSum = utils.PointAdd(api, txCommitSum, feeCommit)
api.AssertIsEqual(txCommitSum.X, frontend.Variable(0))
api.AssertIsEqual(txCommitSum.Y, frontend.Variable(1))
```

The circuit's own header comment (`:21-40`) says the contract is meant to choose between an off-chain
and an on-chain fee model using slots 51–53. **The contract enforces neither.** The sender is debited
`amount + fee`, the receivers are credited `amount`, and `fee` goes nowhere.

Nothing on chain constrains `Fee` at all, so a node may set `fee = 0` (free transfers — there is no fee
enforcement anywhere) or `fee = P`, which makes `feeCommit` the identity and yields a fee-free transfer
that still satisfies the circuit. And neither aggregate is usable as a check even if read:
`SumTxCommit` is asserted equal to the constant `(0,1)` two lines later, and `SumTxValuesWithFee` is an
unbounded hint remainder.

**Is `check()` permanently broken? Yes.** After one fee transfer the balance sum is short by exactly
`Fee·G` while `totalSupply` is unchanged, and nothing ever reconciles the drift; it compounds with
every subsequent fee transfer, and because the offset is a curve point, an off-chain observer cannot
recover the amount to repair it. **Does anything call `check()`? No** — every language was grepped; the
only call sites are two test files. This cuts both ways and the report should say so: it caps the
*immediate* impact (nothing halts) but it means the protocol's only solvency invariant would silently
become false and nobody would find out.

**Negative result preserved.** `_verifyFeePublicInputs` using the 50-signal offset constants against a
`[54]` array is **correct** for the fee layout (0–5 hashed secrets, 6–11 pk, 12–23 prev, 24–35 tx, 36
block, 49 nullifier); the layout was re-derived from the circuit and matches. **The bug is only the
unread slots 50–53. Do not report an offset error.**

Cross-reference: the attacker-chosen-direction version of the same unread signal is C-02's fee
instance. They must be cross-referenced, not merged — this one is what happens when everybody behaves.

**Remediation.** Decide the fee model, then implement it: either read signal 50 and credit a named fee
recipient in `_updateBalancesForTransfer`, or treat it as a burn and decrement `totalSupply`
accordingly. Either way, compare signal 50 against a contract-side `PROTOCOL_FEE` so the fee is
enforced rather than advisory — and fix the test comments and relayer documentation that claim a check
which does not exist.

---

#### C-09 — `withdraw()` forwards caller-chosen `depositParams` to the DvP contract with no binding to the proof

**Severity: Medium today (register said Critical) — **Critical-on-arming** · Exploitability: BROKEN ·
Verdict: CONFIRMED-WITH-CORRECTION**

**Mechanism, confirmed exactly as filed.** In `withdraw/circuit.go:24-47`, only
`HashedSharedSecrets`, `PublicKey`, `PreviousCommit`, `TxCommit`, `BlockNumber`, `AnonymitySet`,
`MessageTags` and `Nullifier` carry `gnark:",public"`. `Hashes[10]`, `SkDeposits[10]`,
`VPerDeposit[10]` and `Address` sit below the `// Private signals` comment — **31 private variables
governing the external leg, none of which appears in the 50-element public signal vector.** The arity
arithmetic confirms nothing else is hidden there: 6+6+12+12+1+6+6+1 = 50, matching every
`WithdrawVerifier{1..6}.sol` signature.

The entire deposit block is:

```go
for i := 0; i < 10; i++ {
    isDepositZero := api.IsZero(circuit.VPerDeposit[i])
    publicKeyFromSk := pos.Poseidon(api, []frontend.Variable{circuit.SkDeposits[i]})
    firstHash  := pos.Poseidon(api, []frontend.Variable{circuit.Address, circuit.VPerDeposit[i]})
    secondHash := pos.Poseidon(api, []frontend.Variable{firstHash, publicKeyFromSk})
    enabled := api.Sub(frontend.Variable(1), isDepositZero)
    api.AssertIsEqual(api.Mul(enabled, api.Sub(circuit.Hashes[i], secondHash)), 0)
}
```

`Hashes[i]` is itself a free private witness, so the block is **vacuous**: the prover picks
`VPerDeposit`, `SkDeposits` and `Address` freely and computes `Hashes` to match. Every occurrence of
those four identifiers was grepped: **there is no constraint anywhere relating `Σ VPerDeposit` to
`SenderTxValue`, to `TxValues[]`, or to `TxCommit[]`.**

Contract side, `_executeZkDvpDeposits` (`Enygma.sol:876-902`) forwards `depositParams[i].amount` and
`.publicKey` verbatim and silently drops `erc20Adress`; `depositParams` is caller-supplied calldata
never referenced by `_verifyPublicInputs`. So the shielded debit and the ERC-20 credit are independent
caller-chosen quantities.

**The unverified assumption the register flagged as most severity-relevant is now settled — and it
holds.** The single implementation of `IZkDvp.depositThroughEnygma` in the whole sibling tree is
`enygma_dvp/contracts/core/contracts/vaults/EnygmaErc20CoinVault.sol:49-72`:

```solidity
function depositThroughEnygma(uint256[] memory depositParams)
    public override onlyRole(DEFAULT_ENYGMA_ROLE) returns (bool, uint256)
{
    uint256 amount    = depositParams[0];      // :50   <-- taken on trust
    uint256 publicKey = depositParams[1];      // :51
    ...
    insertLeaves(commitments);                 // :67  <-- note becomes spendable
}
```

**There is no validation of `amount` on line 50 or anywhere else.** No balance check, no allowance, no
`transferFrom`, no comparison against any proof. `onlyRole(DEFAULT_ENYGMA_ROLE)` is the *entire* trust
model: the vault delegates all value validation to its privileged Enygma caller, which performs none.
The finding does not collapse.

**Reachability corrected.** Four gates, not three, and two are on the DvP side:
`_withdrawVerifiers[*] == 0` (`addWithdrawVerifier`: zero callers in *either* repository);
`_zkDvpAddress == 0` (`addZkDvp`: zero callers in either repository); **`depositThroughEnygma` is
`onlyRole(DEFAULT_ENYGMA_ROLE)` and the only granter, `EnygmaErc20CoinVault.addEnygma`, has no caller
anywhere** — so even a fully wired Enygma would be rejected by the vault; and the prover route 400s.
Gate 4 is **not an attacker gate** (the proving keys are committed), and "a contract that does not
exist in this repo" overstates the barrier — the vault exists in the sibling project that
`demo_instructions.md` tells the operator to check out.

**So the true statement is: exploitation requires no code change at all — only three owner/admin
transactions.** Unlike `deposit`, the withdraw path is structurally coherent: the `uint256[50]`
verifier arity matches the delegatecall selector, and 50 is the circuit's true public-signal count.
This is a **short-fuse latent finding**, not a distant one.

**Amplifier the original filing did not connect:** because all six withdraw proving keys come from the
*same* circuit (M-16), `depositParams.length` carries no cryptographic meaning — the caller freely
picks 1–6 and gets that many arbitrary-value DvP notes from one proof.

**Impact once armed.** Unlimited unbacked value creation on the DvP side from an arbitrarily small (or
zero) shielded debit. The note is spendable in the DvP join-split system and can be brought back into
Enygma through `deposit()`, inflating the shielded supply. The same class of gap exists symmetrically
in `deposit()`, which forwards `withdrawParam.transaction` with no binding to its own proof.

**Why Medium today, not Critical.** On every deployment either repository's tooling can produce,
`withdraw` reverts before any of this runs. Medium rather than Low because the armed impact is
catastrophic and unbounded, arming is three ordinary admin transactions with no code change, every
other artefact of the feature is finished and consistent, and **the missing constraint is invisible
from the contract** — a reviewer wiring the bridge would have no reason to suspect the circuit does not
bind the external leg. **This must be reported as "Critical the moment the bridge is wired."**

**Remediation.** Make the external leg part of the public statement: expose `Σ VPerDeposit` (or a
commitment to the whole `depositParams` array) as a public signal, assert
`Σ VPerDeposit == SenderTxValue` in the circuit, and have `_executeZkDvpDeposits` recompute that
binding from calldata and compare it before calling out. Do the same for `withdrawParam` in
`deposit()`. Independently, `EnygmaErc20CoinVault.depositThroughEnygma` should not treat an unvalidated
`uint256` from a privileged caller as an authoritative value.

---

#### M-14 — Deposit public-signal arity 51 vs 50, and slot 50 stays unread even after the obvious fix

**Severity: Medium · Exploitability: BROKEN — and it is the mask on H-15 · Verdict: CONFIRMED**

**The arity mismatch, verified at five layers:**

| Layer | Arity | Evidence |
|---|---|---|
| Deposit circuit | **51** | `deposit/circuit.go:22-31` — 6+6+12+12+1+6+6+1(Nullifier)+1(Hash) |
| `gnark-server/keys/zkdvp/DepositVerifier.sol` | **51** | `:933-936` |
| `contracts/enygmaverifier/zkdvp/DepositVerifier.sol` | **51** | `:1410-1413` |
| `Enygma.sol` call site | **50** | `:499-502` `abi.encodeWithSignature("verifyProof(uint256[8],uint256[50])", proof)` |
| `IEnygma.DepositProof` | **50** | `interfaces/IEnygma.sol:22-25` |
| zkdvp Go clients | **2** / **1** | `go_client/zkdvp/deposit.go:40-46`, `withdraw.go:36-44` |
| abigen structs | **2** / **1** | I-02 — residue of the older contract generation |

Different selectors, no fallback in either verifier, so `deposit()` reverts `InvalidProof`
**unconditionally** — a second, independent blocker on top of the unset verifier. The withdraw side is
by contrast coherent at `[50]`. The correct targets are **51 for deposit, 50 for withdraw**.

**The load-bearing second half.** `_verifyPublicInputs` takes `uint256[50] calldata` and indexes it only
via the offset constants; plus `_verifyBlockNumber` → `[36]` and `_consumeNullifier` → `[49]`.
**Highest index read anywhere on the deposit path: 49.** Slot **50** of the deposit circuit is `Hash` —
the deposit note commitment, hard-asserted in-circuit at `deposit/circuit.go:267` and derived from the
depositor's `Address`, `PkDeposit` and `SenderTxValue`. It is the **only** public value tying the
shielded leg to the DvP note being redeemed. Widening the helper signatures to `[51]` changes nothing
about which indices they read; they still stop at 49. **A maintainer applying the obvious fix gets a
`deposit()` that works, passes its proof check, updates balances and calls the DvP contract — while
the cross-chain binding between the shielded credit and the note it supposedly redeems remains
entirely absent. Nothing would fail; nothing would warn.**

**Negative result preserved.** Encoding the `DepositProof` struct against a two-argument
`verifyProof(uint256[8],uint256[N])` signature is **correct**, not a bug: the struct is a fully static
tuple, so ABI encoding places it inline as consecutive words, byte-identical to two separate static
arguments. **The 50-vs-51 element-count mismatch is the real bug; the struct-vs-two-args pattern is
not. Do not report it.**

**Remediation.** Change `IEnygma.DepositProof.public_signal` to `[51]`, the selector string at `:500`,
**and** add `[51]` variants of `_verifyPublicInputs`, `_verifyBlockNumber` and `_consumeNullifier` (the
two-character fix does not compile — see H-15). **In the same change**, add the H-15 propagation pass
and add a check of `public_signal[50]` against a value the contract can recompute from the DvP note.

---

#### M-15 — The bridge has no supply accounting, and both circuits move value in the wrong direction

**Severity: Medium · Exploitability: BROKEN · Verdict: CONFIRMED and strengthened**

**Half 1 — no supply accounting.** `deposit` and `withdraw` are the only two operations for which
`Δ ≠ identity` is the *intended* behaviour, and they are exactly the two that never touch
`totalSupplyX`, `totalSupplyY` or `totalSupplyAmount` — both function bodies were read in full. The
four writers of `totalSupply` are `initialize`, `registerAccount`, `mintSupply` and `burn`, none on the
bridge path. Meanwhile both circuits *do* move the pool: they assert
`Com(Σ TxValues, Σ TxRandomValues) == Com(selected_v, 0)`, i.e. `Σ TxCommit = senderV·G` — **not** the
identity, in contrast to the base transfer circuit which hard-asserts `Σ TxCommit == (0,1)` and is
therefore supply-neutral. So `check()` goes permanently false on the first bridge operation, and
because `senderV` derives from a **private** witness the offset is a curve point whose value an
off-chain observer cannot even compute to repair it.

**Half 2 — direction inversion, and it is worse than filed: both circuits are inverted, in mirror
image.**

```go
// deposit/circuit.go:63-87
expectedTxValue := api.Sub(pDiffConstrained, vConstrained)      // P − SenderTxValue
api.AssertIsEqual(selectedVConstrained, expectedTxValueMod)     // sender's TxValue = −SenderTxValue
```
```go
// withdraw/circuit.go:65-79
api.AssertIsEqual(selectedVConstrained, vConstrained)           // sender's TxValue = +SenderTxValue
```

A **deposit** shrinks the shielded pool and debits the depositor; a **withdrawal** grows it and credits
the withdrawer. Both are backwards: `Enygma.deposit` moves value *in* from the DvP side (it redeems the
note) and should credit and increase; `Enygma.withdraw` moves value *out* and should debit and
decrease. So the depositor **pays twice** (note redeemed *and* balance debited) and the withdrawer
**is paid twice**. The withdraw direction is the more dangerous and pairs directly with C-08 (no
solvency proof at all).

**Refutations that failed.** Every `AssertIsEqual` in `deposit/circuit.go` was grepped — the only
aggregate constraint is that Pedersen pair; `_updateBalances` writes only `balanceCommitments`;
`selected_v` really is the sender's slot in both circuits; and `P − v` is unambiguously this codebase's
own "debit" convention (the base transfer circuit uses exactly that idiom for the sender, and
`curve.GetNegative` mirrors it) — deposit applies it, withdraw omits it.

**Note the open question does not change the finding.** Nothing in the repository or specification
states the intended supply semantics of a bridge deposit; `protocol_description.md` never mentions the
bridge at all. But **whichever** semantics is intended, `check()` cannot hold across a bridge operation
as coded, and the sign inversion is wrong under either reading, because the two operations are inverted
*relative to each other* as well as relative to their names.

**Remediation.** Decide and document the bridge's supply semantics; then update `totalSupply` in both
`deposit` and `withdraw`; and swap the sign convention so deposit credits and withdraw debits. Add an
end-to-end test that asserts `check()` holds across a full deposit-then-withdraw cycle.

---

#### M-16 — `WithdrawVerifier1..6` all prove the identical statement

**Severity: Medium · Exploitability: BROKEN (the soundness half is unconditional) · Verdict: CONFIRMED**

`generate_keys.go:129-157` uses the loop variable `i` **only in output filenames**:

```go
for i := 1; i <= splitSize; i++ {
	config := withdraw.WithdrawEnygmaCircuitConfig{ NCommitment: 6 }   // constant
	withdrawCircuit := withdraw.WithdrawEnygmaCircuit{ Config: config, ... }  // every slice sized by NCommitment, never by i
	pkPath  := fmt.Sprintf("keys/zkdvp/WithdrawPk%d.key", i)      // <-- i
	vkPath  := fmt.Sprintf("keys/zkdvp/WithdrawVk%d.key", i)      // <-- i
	solPath := fmt.Sprintf("keys/zkdvp/WithdrawVerifier%d.sol", i)// <-- i
	if err := generateKeys(&withdrawCircuit, pkPath, vkPath, solPath); err != nil { ... }
}
```

`WithdrawEnygmaCircuitConfig` has one field, `NCommitment`, which is the anonymity-set size, not the
split count — the split count appears only as a commented-out `// const nSplit = 6`. The deposit arrays
are hardcoded `[10]` and the processing loop is a literal `for i := 0; i < 10`. `generateKeys` runs
`groth16.Setup(ccs)` fresh each call: same constraint system, six independent setups.

**Corroborated on the shipped artefacts:**

```
$ for f in keys/zkdvp/WithdrawVerifier*.sol; do echo "$f lines=$(wc -l <$f)"; done
WithdrawVerifier1.sol lines=972   ... WithdrawVerifier6.sol lines=972      (all six: 972)

$ sed -E 's/[0-9]+/N/g' WithdrawVerifier1.sol > w1; sed -E 's/[0-9]+/N/g' WithdrawVerifier6.sol > w6
$ diff -q w1 w6  ->  STRUCTURALLY IDENTICAL

$ grep -n "publicInputMSM\|uint256\[" WithdrawVerifier1.sol
456:    function publicInputMSM(uint256[50] calldata input)

$ ls -l keys/zkdvp/WithdrawPk*.key      # all six: 15939729 bytes
$ shasum -a256 keys/zkdvp/WithdrawVerifier*.sol   # all six differ
```

Identical structure once numeric literals are normalised, identical public-input arity, identical
proving-key sizes, differing **only** in the setup constants — the exact signature of one constraint
system with six independent trusted setups.

**Two consequences.** (1) `Enygma.withdraw`'s selection by `depositParams.length` *implies* verifier
*n* proves an *n*-split withdrawal. It does not: all six accept the same statements, so
`depositParams.length` **is bound to nothing**. This removes the last plausible defence for C-09 — the
six-verifier design cannot be the missing binding, because it is not a binding at all. And since
`Hashes`/`SkDeposits`/`VPerDeposit` are private, nothing observable ties a proof to any split count.
(2) Six separate setups mean **six independent trapdoors for one constraint system**; anyone holding
the toxic waste from any one can forge proofs accepted by that verifier. This compounds H-12 sixfold
for no benefit.

**Remediation.** This is not a fix-the-loop bug. Either make the split count a real circuit parameter
*and* a public signal bound in `_verifyPublicInputs`, or delete five of the six verifiers and the
`depositParams.length` dispatch. Keeping six independent setups of one circuit is strictly worse than
keeping one.

---

### 5.4 Low

---

#### M-02 — The demo's "ML-KEM key agreement" performs no key exchange, and its fabricated blobs are registered on-chain as view keys

**Severity: Low (register said Medium; the disagreement is settled substantially in favour of the
lower grade) · Exploitability: LIVE as a code path; no exploit · Verdict: CONFIRMED-WITH-CORRECTION**

The facts are exactly as filed. `demo/main.go:1257-1272` builds the 1184-byte "encapsulation key" as a
SHA-256 chain over a 64-byte random seed, purely for length, and discards the seed — so no
decapsulation key exists. `:1306-1322` draws each "shared secret" straight from `crypto/rand` and
assigns it into both `kaSecrets[lo][hi]` and `kaSecrets[hi][lo]`; that, not any cryptography, is why
"both sides agree". The smoking gun is `eksSnap` (`:1293`), used only for an emptiness test at `:1298`
— the peer's key is never an input to anything. `filippo.io/mlkem768` is not even in `demo/go.mod`.
The UI states three specific falsehoods verbatim: `:1307` "Encap(ek[j]) → ct (1088B) → Decap",
`:1279-1280` "private key never leaves this bank", `:1330-1331` "identical on both sides".

The consolidation's added claim is also true: `demo/main.go:719` reads `ek := s.state.kaEKs[bankIdx]`
and `:750-752` passes it as the fifth argument to `RegisterAccount`, which `Enygma.sol:199` writes to
`viewKeys[accountId]`. **The blobs really do become on-chain "ML-KEM view keys".**

**Why Low rather than Medium: the mechanism does not exist to be broken.**
`grep -rn "\.ViewKeys(" --include="*.go"` outside the generated binding returns **nothing** —
`viewKeys` is written by `registerAccount` and read by nobody, anywhere. The real ML-KEM path
(`go_client/agreement/manager.go`) exchanges the encapsulation key out of band through the local
filesystem and never touches `viewKeys`. So there is no party encapsulating against these blobs today,
and therefore no ciphertext that "nobody can ever decapsulate" — that harm is conditional on someone
implementing `protocol_description.md:135`, which nothing does. And the demo's fabricated secrets do
feed real transfers, but the demo is a single process holding all six committed spend keys, so there is
no counterparty to harm.

**Above Info** for two reasons the "it's only a demo" framing does not cover: it writes non-recoverable
garbage into a **persistent, publicly readable** on-chain field whose NatSpec (`Enygma.sol:188`)
declares it to be "ML-KEM-768 encapsulation key, 1184 bytes", so a future integrator has every reason
to trust it; and the UI asserts three specific falsehoods about the system's headline post-quantum
property, in the artefact built to be shown to evaluators.

**Remediation.** Either call `go_client/agreement` (the demo already has the `replace` directive), or
relabel the steps as simulated and stop writing the fabricated key to `registerAccount`. Do not do the
second without the first if anyone intends to implement the specification's §4.

---

#### M-03 — The withdraw handler leaves four witness fields unassigned, so all six prover routes return 400

**Severity: Low (register said Medium; the raise rested entirely on a masking claim that does not
survive) · Exploitability: LIVE (the endpoint runs and fails on every request) · Verdict:
CONFIRMED-WITH-CORRECTION**

`gnark-server/pkg/circuits/withdraw/handler.go:57-88` assigns `SenderId`, `Address`, `SenderTxValue`,
`SecretKey`, the six per-slot arrays and the three ten-element deposit arrays, and **never assigns
`BlockNumber`, `Nullifier`, `PreviousSenderBalance` or `PreviousSenderRandomValue`**. All four are
constrained inside `Define`, so they cannot be left `nil`. `deposit/handler.go:90-93` contains exactly
those four lines. Demonstrated rather than read:

```
$ GOPROXY=off go run .
handler-as-shipped  NewWitness err = can't set fr.Element with <nil>
with four additions NewWitness err = <nil>
```

That is the `err != nil` branch at `handler.go:90-94`. Unconditional, all six routes.

**Correction — M-03 is not the mask the register says it is.** The ledger asserted it arms C-08, C-09,
H-14, L-12 and H-16. It arms none of them: after the four lines, `withdraw` still reverts
`VerifierNotFound`, then on `_zkDvpAddress == 0`; `addWithdrawVerifier` and `addZkDvp` have zero
callers; the withdraw verifiers are outside the only hardhat sources root and are never compiled; and
the relayer has no withdraw route. **Nor did it ever mask anything from an attacker** — the withdraw
proving keys are git-tracked, so anyone wanting a proof calls `groth16.Prove` directly, which is
exactly how C-08's validator produced one.

What M-03 *is* is the reason the withdraw statement has **never been exercised end to end**, which is
plausibly why nobody noticed that `withdraw/circuit.go` is the only circuit without a solvency
comparator. That is a sequential hazard, not a causal gate — and it carries the fix-order instruction
in §6.

**Above Info** for one measurable security-relevant effect: every withdraw request takes the
witness-error path, and that path leaks a goroutine permanently (M-08). The broken endpoint is a
self-inflicted, *legitimate-traffic* trigger for M-08's leak. That impact is counted under M-08, not
here.

**Remediation.** Copy the four lines from `deposit/handler.go:90-93` — **but only after the solvency
comparator has been added to `withdraw/circuit.go`** (C-08).

---

#### M-04 — The nullifier implements neither the specified double-spend control nor any other

**Severity: Low (register said Medium) · Exploitability: LIVE as a divergence; no independent exploit ·
Verdict: CONFIRMED-WITH-CORRECTION**

The specification (`:246-248`, `:289`) defines `nullifier = Hash(sk_j^spend, n_block)` and states it
proves the sender is submitting "its only allowed transaction in this block" — coherent precisely
because the value depends on `(sk, block)` alone. The implementation computes
`Poseidon(Poseidon(PreviousSenderRandomValue, SecretKey) mod P, BlockNumber)`. `PreviousSenderRandomValue`
is the blinding factor of the sender's *current* balance, which changes every time that balance
changes, so the specified property is enforced by nothing.

**What actually prevents replay — and the system is not under-protected today.** Byte-identical replay
is blocked by the nullifier. Two *different* proofs spending the same balance are blocked twice over:
`_verifyPublicInputsFP:751-757` pins the previous commitment to live state, so the second proof is
stale; and independently, two proofs against the same state necessarily share `prevR`, `sk` and
`lastBlockNum`, hence share the nullifier. **So the implemented nullifier is not inert — it is
redundant with a stronger control.** This corrects the contributing filing, which called it "a
per-proof anti-replay tag … currently void".

**The one case where the nullifier is the sole control is narrower than filed.** A proof whose state
transition is the identity on every bound commitment — but **an honest prover cannot build one**: with
the honest hint, each per-slot blinding factor is non-zero, so even with all `TxValues = 0` every delta
is `Com(0, r_i) ≠ (0,1)` and every named participant's commitment moves (which is C-04's freeze, not a
no-op). The all-identity construction requires `hashModP = 0`, which needs C-01's unbounded quotient —
and under C-01 the nullifier is attacker-chosen anyway. So "nothing prevents repeated execution of a
state-preserving transaction" is true only *under C-01*, where M-04 changes nothing.

**A fix-order trap, and a correction to how it was justified.** An earlier finding recommends relaxing
the state binding so proofs reference the epoch-start commitment, "with the nullifier continuing to
provide replay protection". That relaxation *is* unsafe — but **not for the reason given**: under the
*implemented* nullifier, all of a sender's proofs in an epoch would collide on `secretRemain` and the
second would be blocked, so it would accidentally behave much like the specified design. The real
hazards are narrower: `PreviousSenderRandomValue` has **no range constraint**, so `prevR` and
`prevR + P` open the same commitment and yield different nullifiers; and under C-01 the nullifier is
prover-chosen outright. The ordering recommendation stands; its justification does not. That an
auditor independently proposed the unsafe change is evidence the trap is real.

Also carried forward: **block-number "freshness" is not freshness** — `lastBlockNum` is only re-stamped
by a mutator, so on an idle ledger a proof stays submittable indefinitely, and a proof leaked to the
unauthenticated prover or the relayer can be held and executed later.

**Remediation, in order.** (1) Restore `Nullifier = Poseidon(domainSep, sk, n_block)` — drop `prevR`,
add per-circuit domain separation. (2) Bound the `ModHint` quotient (C-01), else (1) buys nothing.
(3) Only then consider relaxing the previous-commitment binding for liveness (M-05). (4) Document the
previous-commitment binding as the real anti-replay control and update `protocol_description.md` to
match.

---

#### L-01 — Nothing binds a proof to a chain id or a contract address; every fresh deployment has an identical genesis state

**Severity: Low · Exploitability: LIVE (bootstrap window only) · Verdict: CONFIRMED**

No circuit takes a domain input — the 80-signal set is `FingerPrint | PublicKey | PreviousCommit |
TxCommit | BlockNumber | AnonymitySet | MessageTags | Nullifier`, confirmed by an independent
`frontend.Compile` run (`nbPublic=80`). None of `_verifyTransferProof`, `_verifyPublicInputsFP`,
`_verifyBlockNumberFP` or `_consumeNullifierFP` references `block.chainid`, `address(this)`, or any
constructor-set constant. Refutations that failed: the verifying key is *not* per-deployment
(`deploy_direct.py:70-72` deploys the same artifact every run and the VK is hardcoded in the verifier
source); the genesis coincidence rests on **determinism**, not on `randomness == 0` — every observed
caller passes a constant; and `msg.sender` is not bound, so **proofs are bearer instruments**. A global
harness executed the replay: *"two fresh deployments in the same epoch share lastBlockNum=120"* and
*"the identical proof was accepted by BOTH contracts"*.

Low because the exploitable window is a fresh instance's bootstrap: replaying a proof there applies
deltas to `(0,1)` balances, producing commitments to negative values and breaking `check()` forever.
No value is stolen, and the window closes on the first mint. Realistic settings: one Enygma per asset
stood up in the same batch, a v2 redeploy with the same registrations, or a test-net proof carried to a
freshly deployed mainnet instance. **Remediation:** add a domain separator (chain id and contract
address) as a public input to every circuit and check it on chain, and bind `msg.sender`. Note the
sequencing constraint from **I-03**: adding a chain id to a nullifier is exactly the change that would
hit the `t = 5` Poseidon table panic.

---

#### L-03 — No reproducible path from source to deployed bytecode

**Severity: Low · Exploitability: LIVE (a process/assurance defect; no attacker action) · Verdict:
CONFIRMED**

`contracts/enygma/.gitignore` is exactly `artifacts/` + `cache/`, so `git ls-files` returns zero
tracked artefacts and both deploy scripts crash on their first `load_artifact()` on a fresh clone. The
artefacts present in the working tree compile an **ABI-incompatible predecessor**: the tracked
`Enygma.json` has `registerAccount(addr, accountId, publicKey, randomness)` — **four** parameters, no
`viewKey` — and no `transferWithFee`, `viewKeys` or `addFeeVerifier` at all; deployed-bytecode length
16 747 bytes against 36 162 bytes of current source. Compiler settings are unpinned:
`hardhat.config.js:2` is the short-string form with no `settings` block, the only build-info records
`optimizer.enabled: false` and `evmVersion: paris`, and `Enygma.sol:2` is `pragma ^0.8.24` with no
upper bound. There is no recorded bytecode hash, no chain id in `address.json`, and no verification
step anywhere.

Something checked that the finding did not claim: with the optimizer off and the source having grown
25 kB → 36 kB, is there an EIP-170 overflow? **No** — `solc 0.8.27 --evm-version paris` (from the
project's own compiler cache) gives a 22 324-byte runtime. The current source is deployable, but with
only ~9 % headroom.

Not higher than Low because the realistic failure (deploying yesterday's ABI) breaks **loudly** —
`relayer/cmd/register/main.go:110` passes a fifth argument the stale ABI has no overload for.
**But the consequence for this audit belongs in the scope section, prominently:** nobody can state
which bytecode runs on chain 72957. **Remediation:** track artefacts or pin a reproducible build; pin
compiler version, optimizer settings and EVM version explicitly; record a bytecode hash and chain id
alongside every deployed address; and add source verification to the deploy script.

---

#### L-04 — Proving-key load errors discarded; verifying keys never used; nothing binds a deployed verifier to a key

**Severity: Low · Exploitability: LIVE · Verdict: CONFIRMED**

All four handlers do `pk, _ := utils.LoadProvingKey(...)` (`enygma/handler.go:45`,
`enygma_fee:40`, `deposit:39`, `withdraw:39`). `LoadProvingKey` returns `(nil, err)` when the file
cannot be opened, so `pk` is a nil interface and the server still prints its healthy banner; the first
request then panics inside `groth16.Prove` and is recovered into a 500 **forever**. The trigger is
mundane: every key path in `config.go:29-49` is relative, so starting the process from anywhere but
`gnark-server/` produces exactly this. A second-order variant not in the original write-up: if the file
exists but `ReadFrom` fails partway, the error is discarded too and `pk` is non-nil and partially
populated.

`grep -rn vkPath pkg/` returns four hits, all parameter declarations. No handler ever calls
`LoadVerifyingKey` or `groth16.Verify`. **The substantive point: nothing anywhere in the repository
binds a deployed verifier contract to the proving key in use** — not on chain, not in keygen, not in
either deploy script. That absence is the control gap that lets **H-16** exist undetected and would
make a malicious verifier swap invisible. The config typo `DepositVk: "./keys/zkdvp/DepositKVk.key"`
(the file is `DepositVk.key`) has **no runtime effect** precisely because `vkPath` is ignored — it is
evidence *for* the second claim rather than a defect in its own right.

**Remediation.** Check the error and `log.Fatal` on a missing key; load and use the verifying key to
self-verify each proof before returning it; and add a deploy-time assertion comparing the deployed
verifier's embedded constants against `vk.ExportSolidity` of the loaded key.

---

#### L-05 — Relayer performs no numeric range validation and zero-pads short signal arrays on one route

**Severity: Low · Exploitability: LIVE · Verdict: CONFIRMED**

**The interesting question, worked through, is why this stays Low.** `RelayTransfer` accepts any
`len(publicSignal) <= 80` and zero-fills to `[80]`. Could that produce a semantically valid but wrong
public input? **No.** `Enygma.transfer` calls `_verifyTransferProof` **first**, i.e. Groth16
verification over the full 80-element vector before any state-dependent check, and the verifier's
`publicInputMSM` commits to **all 80** slots (counted: exactly 80 `PRECOMPILE_MUL` and 80
`PRECOMPILE_ADD` staticcalls). A proof produced for a vector with a non-zero tail simply does not
verify against a zero-padded one. Padding can only *reconstruct* a correct input; it cannot manufacture
a different-but-accepted statement.

What is real: **padded requests are broadcast and paid for** (`auth.GasLimit` non-zero suppresses
`eth_estimateGas`, so the doomed transaction is signed, broadcast, mined and reverted at the relayer's
expense — a gas-drain amplifier on H-10); **no numeric range validation anywhere** (every field is
`SetString(s, 10)` or `big.NewInt(int64)` handed straight to abigen, nothing checked against BN254 `r`
or `q`, the Baby Jubjub base field, or `_totalRegisteredParties`; `kIndex: [-1]` becomes `2²⁵⁶−1`);
**route asymmetry and documentation drift** (`/relay/transfer` pads to 80 while `/relay/transfer_fee`
requires exactly 54; `types.go:8` says "up to 50 elements" and `:19-20` says "exactly 51 with the fee
at index 50" — both wrong; `config.go:51` documents a default of `300000000` while `:83` sets
`5000000`); and an **attacker-chosen dedup key** (`"transfer:" + req.Proof[0]`), which lets a token
holder 409 a *specific* competing proof for up to 45 s.

**Remediation.** Reject rather than pad; validate every numeric field against the relevant field order
and against `_totalRegisteredParties`; reject negative ids; hash the whole request for the dedup key;
and correct the four wrong doc comments.

---

#### L-06 — The README's post-quantum migration claim does not achieve what it states

**Severity: Low · Exploitability: LIVE · Verdict: CONFIRMED-WITH-CORRECTION (of the title, not the
substance)**

`README.md:64-65`: *"We intend to update the ZK module to use a quantum-secure ZK scheme, which will
make the entire system quantum-secure (as opposed to quantum-private)."*

**"quantum-private" is defensible and must not be attacked in this report.** Pedersen commitments are
*perfectly* hiding and Groth16 is perfect zero-knowledge, so amount confidentiality is
information-theoretic and survives a quantum adversary. The naive objection — "Pedersen and Baby
Jubjub are not post-quantum" — is **wrong** for privacy. (It fails for three *other* reasons, all
reported elsewhere: the confidentiality layer does not exist (M-12), sender anonymity is broken
classically (H-01), and there is no forward secrecy.)

**"will make the entire system quantum-secure" is false**, and that is the real defect. Pedersen
*binding* is computational, resting on Baby Jubjub discrete log. A participant who can compute
`d = dlog_G(H)` opens its own balance commitment to any value and then proves that false opening
**perfectly honestly** under whatever post-quantum SNARK replaced Groth16. Swapping the proof system
does not deliver quantum soundness; the on-chain balance representation itself must change. The
project's own `protocol_description.md:58` says binding rests on the commitment scheme, so the README
contradicts the specification.

**Performance figures, independently re-measured** (not taken on trust):

```
$ go run ./cmd/count
INF parsed circuit inputs  nbPublic=80 nbSecret=23
INF building constraint builder  nbConstraints=94206
enygma constraints=94206 publicVars=81 secretVars=23 internal=84175
```

README says **82 086**; actual **94 206** — 15 % higher. Verifier gas floor, counted from the generated
verifier (`grep -c PRECOMPILE_MUL` → 81, i.e. 80 staticcalls plus the declaration): 80 ECMUL (480 000)
+ 80 ECADD (12 000) + one 4-pair pairing (181 000) = **≥ 673 000 gas** before intrinsic cost and
~2.5 kB of calldata. README says **389 578** — not merely stale but arithmetically impossible for an
80-public-input Groth16 verifier.

**Remediation.** Retitle the claim ("the stated post-quantum migration path does not achieve what it
claims"), correct the figures, and add a CI check that fails when `GetNbConstraints()` drifts from the
published number.

---

#### L-07 — `formal_methods/` claims three tools prove the system; none of the three does

**Severity: Low · Exploitability: LIVE · Verdict: CONFIRMED**

`formal_methods/README.md` in its entirety: *"We use Lean, Verifpal, and Tamarin Prover to prove the
correctness and security of our system."*

- **Tamarin — artefact absent.** `find formal_methods -type f` returns exactly four files:
  `README.md`, `lean/EphemeralKeys.lean`, `verifpal/private_messaging_tags.vp`, `verifpal/README.md`.
  No `.spthy`, no directory, no output.
- **Lean — axioms about an unimplemented mechanism, and no non-trivial theorem.** Read in full: the
  file declares the types and functions as opaque `axiom`s, then asserts three security `axiom`s, and
  every "theorem" is a restatement of one of them (`can_decrypt_same_epoch` is literally
  `exact decrypt_encrypt_same_epoch c n m`). Nothing about AES-GCM, HKDF, ML-KEM or the Go code is
  proved. Worse, the mechanism modelled — the per-block epoch key — **is not implemented anywhere**
  (M-12), and the scoped-disclosure property it states as a theorem is the exact property the
  construction cannot deliver.
- **Verifpal — models classical Diffie–Hellman.** `private_messaging_tags.vp:15-33` is `pkA = G^a`,
  `pkB = G^b`, and `:47-49` derives `s_ab = pkB^a` — the primitive ML-KEM was chosen to *replace*.
  Additionally, the model does not capture the deployed construction: each principal publishes only
  its own tags, whereas the shipped circuit publishes the whole fingerprint matrix on chain, which is
  precisely the mechanism that breaks the unlinkability the model queries.

**Kept at Low deliberately**, though a raise was considered: there is no attacker capability, and the
specific over-trusted properties are already reported at their proper severities (M-12, H-01). Raising
this would double-count them. **What it should get is prominence rather than severity** — hence the
statement in §1 and §9 that **no formal verification of the implemented system exists**. If the
developers confirm this directory has been shown to a counterparty or regulator as assurance evidence,
it becomes a Medium-grade misrepresentation; that is a question only they can answer.

**Remediation.** Delete the claim or qualify it precisely: state which model covers which mechanism,
mark the Lean axioms as assumptions, and remove Tamarin from the sentence until an artefact exists.

---

#### H-03 — Off-by-one carry-forward loop against 1-based ids, plus `(0,0)` as an absorbing element

**Severity: Low (register said High) · Exploitability: LATENT (register said LIVE — that label is
wrong) · Verdict: CONFIRMED-WITH-CORRECTION**

**The bug is real.** Three loops walk the account space. Two are 1-based and carry explicit corrective
comments; the third does not:

```solidity
// Enygma.sol:579-580   check()
// AccountIds are 1-based: registered banks occupy slots 1.._totalRegisteredParties.
for (uint256 i = 1; i <= _totalRegisteredParties; ) { ... }

// Enygma.sol:909-911   _propagateBalancesExcept()  [mintSupply, burn]
// AccountIds are 1-based: iterate 1.._totalRegisteredParties (not 0..n-1)
for (uint256 i = 1; i <= totalParties; ) { _initializeBalanceIfNeeded(i); ... }

// Enygma.sol:801-802   _updateBalancesForTransfer()  [transfer, transferWithFee]
for (uint256 i; i < totalParties; ) { _initializeBalanceIfNeeded(i); ... }   // 0 .. N-1
```

**Somebody fixed two of three sites.** So `_updateBalancesForTransfer` wastes a slot on non-existent
id 0 and never touches id `_totalRegisteredParties`. If that account is also absent from
`participantIds` on a transfer that starts a new epoch, its slot is never written; `getBalance` then
masks the raw `(0,0)` to `(0,1)` and reports zero, while the raw read at `:815-820` sees `(0,0)` — and
`pointAdd((0,0), δ) = (0,0)` is **absorbing**, so every later credit is silently discarded while
`transfer()` returns `true`.

**The refutation that drives the downgrade: for which `N` is a *real* account skipped?** Only when
`max_registered_id == N`. Every configuration the repository produces was enumerated:

| Configuration | `N` | Top bank id | Real account skipped? |
|---|---|---|---|
| `demo/main.go` (banks 1–6 + relayer at **100**) | **7** | 6 | **No** |
| `transaction_test.go` (the documented Step 4 flow) | 7 | 6 | **No** |
| `epoch_test.go`, `sequential_transfer_test.go`, `cost_report_test.go` | 7 | 6 | **No** |
| `scenario_test.go` (banks 1–6, no relayer) | **6** | 6 | **Yes, bank 6** — but see below |

The relayer registration is not optional in the first three rows: `transfer` is `onlyRegistered` and
every one of them submits via the relayer, whose `cmd/register` defaults `--account-id 100`. **That one
extra registration is the sole reason no real bank sits at the top of the range — the protection is
accidental.** And even at `N == 6`, no shipped client can trigger it: only `_transferVerifiers[6]` is
ever populated, so a transfer carries exactly six deltas, and every client fills those six slots with
ids 1–6, so **all six banks are participants of every transfer** and the carry-forward branch never
matters for a real bank. That is why no test has ever caught this.

Harm therefore requires all three of: dense ids `1..N` with no extra registration; a transfer omitting
account `N`; and that transfer being the first of a new epoch. The middle condition is producible today
only by hand-crafted calldata — but it also arises **with no attacker at all** as soon as more than six
institutions join, since honest anonymity-subset selection must then omit someone, which is exactly the
model `protocol_description.md:398` describes.

**A reverse hazard checked and cleared, that no prior filing noticed.** Id 100 is outside both loops,
and is nevertheless harmless: `_verifyPublicInputsFP` calls `getPublicValues(_totalRegisteredParties + 1)`
and indexes `balances[accountId]`, so **any participant id > `N` reverts with an array out-of-bounds
panic**. An account registered above the count can never be a transfer participant, cannot hold usable
funds, and fails loudly rather than silently.

**The `(0,0)` half, graded separately at Info.** Verified by execution against a reimplementation of
`CurveBabyJubJub.pointAdd`:

```
isOnCurve((0,0)): False
(0,0)+G   = (0, 0)
(0,0)+H   = (0, 0)
(0,0)+(0,1) = (0, 0)
G+(0,0)   = (0, 0)
```

Absorbing in **both** argument positions, including against the neutral element. `isOnCurve` is
confirmed dead code (one grep hit — its own definition). But it does not stand as an independent
finding: its only reachable source is an unwritten storage slot, and every route to one is already
filed. **Attacker-supplied points cannot be `(0,0)`** — deltas are byte-compared against
circuit-verified `TxCommit`, and the twisted-Edwards addition law is complete for this curve. That
narrowing must be preserved: this must **not** be presented as attacker-supplied off-curve input.

**Remediation.** One character — `for (uint256 i = 1; i <= totalParties; )` at `:802`. Structurally,
none of these loops should key off a *count* while `registerAccount` accepts an arbitrary id: maintain
an explicit `uint256[] _registeredIds` and iterate that, or reject
`accountId != _totalRegisteredParties + 1`. Additionally, make `_initializeBalanceIfNeeded` the single
accessor (or add a `(0,0)` guard to `pointAdd`) so the verification and update paths cannot disagree
about what an empty slot means. **Re-grade to Medium immediately if the operator confirms a live
deployment with `TotalRegisteredBanks() == 6` and banks at ids 1–6, and to High if any transfer there
omits bank 6.**

---

#### L-02 — `initialize()` after `registerAccount` breaks the supply invariant

**Severity: Low (borderline Info) · Exploitability: LATENT (register said LIVE) · Verdict:
CONFIRMED-WITH-CORRECTION**

`initialize()` is `onlyOwner` with `_owner` constructor-set, unconditionally sets
`totalSupplyX = 0, totalSupplyY = 1`, and cannot be re-run. `registerAccount` and `burn` are
`onlyOwner` **without** `whenInitialized`, while `mintSupply` and all four participant mutators have
it — verified by grepping the modifier list on every external function. Neither deploy script calls
`initialize()` and `demo_instructions.md` never mentions it, so the ordering is unenforced and
undocumented. **Front-running is correctly rejected** and should not be re-litigated.

**Two corrections.** (1) *With the randomness every production caller actually passes, the ordering has
no effect at all.* `pedCom(0, 0)` is the identity `(0,1)` (both `derivePk(0)` and `derivePkH(0)` return
the initialised accumulator), and every production caller passes zero, so registering before
`initialize()` contributes nothing and `initialize()` discards nothing. **`check()` on the live
deployment is unaffected by this ordering.** (2) *The mechanism of loss is not the one described.* With
non-zero randomness, pre-`initialize` `totalSupply` is `(0,0)`, which is an **absorbing element** for
`pointAdd`, so the `r_i·H` terms are never "folded in and then discarded" — they are lost on the very
first registration, and `initialize()` merely replaces one wrong value with another. (3) The `burn`
half is not an ordering issue and duplicates **H-13**; it belongs there.

The residual real defect is the missing `whenInitialized` on `registerAccount`: the `randomness`
parameter exists precisely so an operator *can* pass a non-zero value, and the moment anyone does so
before `initialize()`, `check()` is false permanently with no recovery function.

**Remediation.** Add `whenInitialized` to `registerAccount`, or fold initialisation into the
constructor.

---

#### M-10 — The spend key and the balance opening are passed as command-line arguments

**Severity: Low (register said Medium) · Exploitability: LATENT · Verdict: CONFIRMED-WITH-CORRECTION**

`go_client/transaction/main.go:81-124` reads `sk`, `previousV` and `previousR` from `os.Args[4..6]`.
`sk` is the whole spend authority; `previousV`/`previousR` de-blind the balance. `os.Args` is
world-readable via `ps aux` and `/proc/<pid>/cmdline` and persists in shell history, CI logs and
container specs. The "the authors treat the host as multi-user" argument holds — `agreement/manager.go:127`
writes the ML-KEM seed at mode `0600`. There is no key-rotation path, so a leaked `sk` is effectively
unrevocable.

**Why not Medium.** (1) **The path is dead** — L-08 establishes three independent blockers that stop
this CLI before it can produce a proof. (2) **The secret it leaks is a public constant in the shipped
configuration** — an attacker reading `sk` out of `ps aux` learns `3`; the compromise of these keys is
entirely captured by C-06 and counting it again double-books. (3) **It requires a capability that
already implies compromise** — a local shell on the operator host, which also holds the key seeds and
(per C-06) the relayer's funded private key in a committed file.

**Why not Info.** It is a real secure-coding defect on a *documented* entry point — `go_client/README.md`
presents this under "Running the standalone CLI" as a supported way to send a payment, and the
`DEMO PURPOSE ONLY` banner in that file covers only `generateTxValues`/`generateKIndex`, **not**
`parseArguments`. It also sits in the masked-bug ledger: the L-08 fix is a one-string change, and the
moment it lands this pattern is live with whatever `sk` a real institution uses.

**Remediation.** Read `sk` from a file (mode 0600), from stdin, or from an environment variable — or
delete the CLI. Fix in the same change as L-08.

---

#### H-16 — Eight zkdvp verifiers in `contracts/` match no proving key — stale copies, fail-closed

**Severity: Low (register said High) · Exploitability: BROKEN, **and unlike H-14/H-15 there is no state
in which this becomes an attack** · Verdict: CONFIRMED-WITH-CORRECTION**

**The cryptographic claim is settled, by a third independent method.** For every Groth16 verifier
`.sol` in the tree, the baked-in `ALPHA_X` constant was parsed, both compressed BN254 encodings formed,
and every `*Vk.key` searched for either byte string:

```
sol file                                             ALPHA_X matches VK    twin in other tree
------------------------------------------------------------------------------------------------
contracts/enygma/contracts/EnygmaFeeVerifier.sol     EnygmaFeeVk.key       keys/EnygmaFeeVerifier.sol
contracts/enygma/contracts/EnygmaVerifier.sol        EnygmaVk.key          keys/EnygmaVerifier.sol
contracts/enygmaverifier/zkdvp/DepositVerifier.sol   NONE                  NONE
contracts/enygmaverifier/zkdvp/WithdrawVerifier.sol  NONE                  NONE
contracts/enygmaverifier/zkdvp/WithdrawVerifier1..6.sol NONE               NONE
gnark-server/keys/zkdvp/DepositVerifier.sol          DepositVk.key         NONE
gnark-server/keys/zkdvp/WithdrawVerifier1..6.sol     WithdrawVk1..6.key    NONE
```

**9 of 9** under `gnark-server/keys/` match; **0 of 8** under `contracts/enygmaverifier/zkdvp/` match
any of the nine. The positive control also holds at byte level. Three independent methods now agree.

**The question both original auditors flagged — bricked, or a foreign setup someone holds a trapdoor
for? — is answered: neither. The stale files are the developers' own earlier output, and they can be
dated.** Git history is decisive, and is evidence neither original filing looked for. Both trees were
regenerated together at commit **`e068010` (2026-03-17)**; the keys were regenerated twice more
(`bb29be0`, 2026-06-08, *"fix vulnerability, update keys and verification files…"*; `bd4e520`,
2026-08-03) and the `contracts/` copies were never touched again. Matching each stale file's α against
every **historical** version:

```
  contracts/.../DepositVerifier.sol    -> DepositVerifier.sol  @e068010 (2026-03-17)
  contracts/.../WithdrawVerifier.sol   -> WithdrawVerifier1.sol @e068010 (2026-03-17)
  contracts/.../WithdrawVerifier2..6.sol -> WithdrawVerifier2..6.sol @e068010 (2026-03-17)
  contracts/.../WithdrawVerifier1.sol  -> NO HISTORICAL MATCH
```

and for the eighth, `git log --all -S<ALPHA_X>` returns `c6897ad` / `58f07dd` / `12618f3`
(2025-11-03/04, "adding smart contracts") — the very first generation, which is also why it is still
`uint256[1]`. For completeness, every other `.sol` in the whole monorepo (70 files) was searched: none
of the eight α values appears anywhere else.

**Therefore the speculative half is dropped, not softened.** There is no evidence of a foreign setup,
and the party holding the toxic waste for these files is the same party that holds it for the
*current* keys — which is exactly and entirely **H-12**. The stale tree adds **no** attack surface on
top of it. Reporting "a foreign setup may exist whose trapdoor someone holds" would be a false
positive.

**The register's "arming hazard" — which is where the High severity came from — is refuted.** It
claimed that if `WithdrawVerifier1.sol` (`uint256[1]`) were deployed and an attacker picked
`depositParams.length == 1`, "the entire public-input binding collapses to one field element". **That
cannot happen.** `Enygma.withdraw` hand-encodes a fixed selector
`verifyProof(uint256[8],uint256[50])` = `0x18e2c03f`; `verifyProof(uint256[8],uint256[1])` is a
different selector; the file declares no `fallback()` and no `receive()`. The delegatecall hits no
dispatch target, reverts, and `withdraw` reverts `InvalidProof`. The analogous behaviour was confirmed
by execution on the deposit side (H-15 §2a: a `[50]` call site against a `[51]` verifier yields exactly
`InvalidProof()` `0x09bde339`). **The `[1]` file is not a soundness hazard; it is one more file that
always fails closed.**

**What remains, and is still worth reporting:** two divergent copies of the same security-critical
artefact, with the wrong one in the more obvious place (an engineer picking up the bridge finds
`contracts/enygmaverifier/zkdvp/` first, and nothing marks either as canonical); and **nothing binds a
deployed verifier to a verifying key** — no manifest, no checksum, no build step, no post-deploy
assertion. The stale tree is the visible symptom; the absent control (L-04) is the actual finding, and
it is the control that would also catch a *malicious* verifier substitution by a compromised deployer.

**Remediation.** Delete `contracts/enygmaverifier/`. Make the build copy `gnark-server/keys/**/*.sol`
into the contracts tree and fail if a checked-in copy is stale; commit a manifest of
`sha256(*Vk.key) → sha256(*.sol)` and assert it in CI. At deploy time, read back the deployed
verifier's embedded constants and compare them to the local verifying key before registering it.

---

#### L-08 — The documented `go_client` CLI can never produce a valid proof

**Severity: Low (raised from Info on the masking rationale) · Exploitability: BROKEN · Verdict:
CONFIRMED**

Three independent blockers on the path `transaction/main.go` → `proof.GenerateProof` →
`POST /proof/enygma`:

1. **Wrong JSON field, and wrong type.** `internal/types/types.go:23` serialises
   `HashedSharedSecrets []string` as `"hashed_shared_secrets"`; the endpoint requires
   `FingerPrintofSharedSecrets [][]string` tagged `"fingerprint_shared_secrets"` with
   `binding:"required"`, so gin 400s before any witness is built. **Stronger than filed:** even
   renaming the field would not fix it — the client sends a flat `[]string` where the handler needs a
   k×k matrix. (Note the other three circuits *do* use `hashed_shared_secrets []string`, so this is the
   enygma circuit having moved to the fingerprint matrix while the client library did not.)
2. **Divergent hash derivation.** `internal/randomness/operation.go:34-38` computes a **two**-input
   `Poseidon([s_j, s_j]) mod P`; the circuit constrains a **one**-input `Poseidon([SharedSecrets[i]])`.
   Different permutation width, different value.
3. **Nullifier has exactly one Poseidon layer too many.** The circuit wants `Poseidon(s_sender, block)`;
   the client sends `Poseidon(Poseidon(s_sender, s_sender) mod P, block)`.

**Refutations.** "Some other client uses this library successfully" — the opposite: every *working*
client bypasses the library and hand-rolls the payload with the correct field name (`demo/main.go:1038`,
`transaction_test.go:494`, `epoch_test.go:106`, `scenario_test.go:327`, `sequential_transfer_test.go:223`,
`cost_report_test.go:350`). "It is one typo" — no, see the type mismatch plus blockers 2 and 3, which
are semantic.

**Impact today is nil; on impact alone this is Info. It is kept at Low because of what it masks** —
M-10, M-11, L-09 and L-10 all live behind this 400, and `go_client/README.md` documents this as *the*
way to send a payment. **If it is repaired, those four must be repaired in the same change.**

**Remediation.** Fix all three divergences, and fix M-10, M-11, L-09 and L-10 in the same commit — or
delete the CLI and remove it from the README.

---

#### L-09 — Proof-generation failure is never detected; an all-zero proof is forwarded as success

**Severity: Low · Exploitability: BROKEN · Verdict: CONFIRMED**

`go_client/internal/proof/genProof.go:65-89` never reads `response.StatusCode`, has no `error` return,
and panics only on transport and JSON-syntax errors. The gnark server returns all failures as HTTP 400
`{"error": "..."}`, which unmarshals cleanly into `types.Response` (unknown fields ignored), leaving
`Proof` and `PublicSignal` nil — so **total prover failure is structurally indistinguishable from
success**, and `sendTransferViaRelayer` then POSTs eight empty strings with a Bearer token.
Fail-closed at the relayer (`PublicSignal` carries `binding:"required"` and `parseProof8` rejects
`""`), so nothing reaches the chain.

Note `demo/main.go:1063-1066` *does* check the status code — the working client got this right and only
the library path is affected, which is evidence of drift rather than a misread. Kept at Low rather than
Info because the defect is structural: a proof-producing function whose signature cannot express
failure, with the same unchecked-status pattern repeated in `go_client/utils/utils.go:86-109`. The
harm is misdiagnosis — an operator sees a plausible-looking error naming the relayer while the real
failure was the prover three steps earlier.

**Remediation.** Give `GenerateProof` an `error` return; check the status code; and validate
`len(Proof) == 8` before forwarding.

---

#### L-10 — The CLI submits 0-based circuit indices as on-chain account ids

**Severity: Low · Exploitability: BROKEN · Verdict: CONFIRMED-WITH-CORRECTION — **the stated failure
mechanism is wrong, and the corrected version is a worse latent hazard***

Both the original filing and the register claim the transaction reverts because slot 0 is compared
against `publicKeys[0]` while signal 36 carries account 1's real key. **That is not what happens.** The
CLI is internally *consistent* with a 0-based account space, so the on-chain checks pass:
`transaction/main.go:141` calls `GetPublicValues(args.QtyBanks)` and uses the result **unsliced**, and
`getPublicValues` is indexed by *accountId* starting at 0 — so the CLI's `PublicKeys[0]` is account 0's
key, not account 1's. Every working caller instead slices (`demo/main.go:829-830`,
`transaction_test.go:424-425`), which is why *they* need `kIdx64[i] = i+1`. So
`_verifyPublicInputsFP` compares `keys[i]` against `keys[i]` — **equal** — and the previous-commitment
check matches too, since `getBalance(0)` returns the neutral element, which is exactly what the CLI
fetched.

The real fail-closed guarantee is one layer earlier, in the circuit: with `senderId = 0` the circuit
asserts `PublicKey[0] == Poseidon(sk, sk) mod P` and account 0's key is `0`, which is not an obtainable
Poseidon output; with `senderId != 0`, `GenerateTxValues` always writes the negated amount to index 0
while the sender sits elsewhere, so the sender-slot assertion fails.

**Why this matters more than it looks.** Under the corrected mechanism the on-chain guard is **not**
what stops it — the contract would happily accept `participantIds = [0,1,2,3,4,5]`. So **anyone fixing
L-08 / I-06 without also changing `generateKIndex` gets a CLI that lands transactions crediting and
debiting account 0**, the unregistered sentinel: an ownerless sink whose value can never be spent and
which `check()` (loop starts at `i = 1`) cannot see. That is silent fund destruction plus a broken
supply invariant, **not** a revert. Bank 6 is also silently excluded from every CLI transfer. This
belongs in the masked-bug ledger, and it strengthens H-07 — H-07 is the control that is missing here.

**Remediation.** Map slot `i` → account id `i+1` in `generateKIndex`, in the same change as L-08.

---

#### L-11 — One nullifier set is shared by four circuits

**Severity: Low · Exploitability: BROKEN · Verdict: CONFIRMED-WITH-CORRECTION — **the register's
rebuttal is half wrong and understates it***

The register disposed of this by observing that `enygma` derives its nullifier differently from the
other three, so there is no cross-circuit collision. That disposes of `enygma`-versus-the-rest, but it
is not the claim that was made. **`transferWithFee`, `withdraw` and `deposit` collide with each
other**, by construction: all three assert `SharedSecrets[senderId] == Poseidon(prevR, sk) mod P`, then
`HashedSharedSecrets[i] == Poseidon(s_i, s_i) mod P`, then
`Nullifier == Poseidon(HashedSharedSecrets[senderId], BlockNumber)` — three textually identical blocks
(`enygma_fee:122-128/132-140/247-255`, `withdraw:89-96/100-109/177-186`,
`deposit:103-110/113-122/185-194`). So for a fixed `(prevR, sk, lastBlockNum)` the fee, withdraw and
deposit nullifiers are **the same number**, and `_nullifiers` (`Enygma.sol:104`) is one untagged mapping
written by all three consume-helpers. Two operations built against the same pre-state in one epoch —
the natural thing when batching, since the anchor is epoch-constant — collide, and the second reverts
`NullifierAlreadyUsed`.

**Interaction with M-04.** They are independent: M-04 kills the nullifier's *positive* function, while
this is a *negative* side effect that does not depend on the nullifier working as intended. They
compound in one direction: the 50/54-signal circuits publish the nullifier preimage as signals 0–5, so
once any of those paths is wired an observer can compute a victim's nullifier for the epoch and — because
the namespace is shared — burn it from **any** of the four entry points, **including the LIVE
`transfer`**. Domain separation would confine that griefing to one entry point.

**Remediation.** Add a per-circuit domain tag to the nullifier preimage — **in all four circuits at
once**, together with M-04's `prevR` removal.

---

#### L-12 — External calls precede state updates in `withdraw()` and `deposit()`; no reentrancy guard anywhere

**Severity: Low · Exploitability: BROKEN · Verdict: CONFIRMED (two amendments)**

`withdraw` (`Enygma.sol:447-480`): proof → `_verifyPublicInputs` → `_verifyBlockNumber` →
`_consumeNullifier` → **`_executeZkDvpDeposits` (`:471-473`, external)** → `_updateBalances` (`:477`).
**`deposit` has the identical shape** and the register's title omits it: `:513-520` calls
`zkDvp.withdrawThroughEnygma(...)` before `_updateBalances`. Any fix must cover both.
`grep -rn "nonReentrant|ReentrancyGuard|_locked|_entered" contracts/` returns **zero hits** across all
33 `.sol` files.

The nullifier *is* consumed before the external call, so same-proof reentrancy is blocked; but a
*different* valid proof bound to the same stale pre-state is not — `getBalance()`, `lastBlockNum` and
`check()` all still report pre-withdraw values during the window.

**The validation question is settled.** The counterparty that actually exists,
`EnygmaErc20CoinVault.depositThroughEnygma` (`enygma_dvp/.../vaults/EnygmaErc20CoinVault.sol:49-72`), is
`onlyRole(DEFAULT_ENYGMA_ROLE)` and its only external call is to an owner-configured Poseidon helper.
It **never calls back into Enygma**. So in the intended deployment this is a latent CEI violation, not
a live reentrancy path.

**Residual risk, and why not Info.** `_zkDvpAddress` is owner-set, so an attacker cannot point Enygma
at a hostile callee. But (a) a future vault or a vault upgrade that *does* read Enygma state mid-call
gets pre-withdraw values with no warning, and (b) `_updateBalances` performs no epoch propagation
(H-15), so a mid-transaction `lastBlockNum` advance leaves the outer call reading an uninitialised
`(0,0)` slot that `pointAdd` treats as absorbing (H-03) — **the two defects compose into silent balance
destruction rather than a revert.**

**Remediation.** Move `_updateBalances` before the external calls in both functions, and add a
reentrancy guard. Cheap, and should not wait for the bridge to be wired.

---

#### L-13 — zkdvp clients derive every blinding factor from constants checked into the repository

**Severity: Low today — **but the masked severity is High** · Exploitability: BROKEN · Verdict:
CONFIRMED**

`go_client/zkdvp/deposit.go:264-282` and `withdraw.go:208-240` pass hardcoded `secrets` arrays and
block numbers; neither imports the ML-KEM `agreement` package. Since
`getTxRandomAndRValues` (`go_client/utils/utils.go:193-229`) computes
`r_i = P − (Poseidon(blockNumber, s_i) mod P)` with `blockNumber` a public on-chain value and `s_i` a
repository constant, **every DvP blinding factor is computable by anyone with a checkout — the
commitments are not hiding.** Three of the six deposit secrets are the identical `1234567890`, making
slots 0/3/4 mutually linkable regardless; `depositSecret := "94"` and
`depositKey := {99,98,97,96,95,94}` are trivially brute-forceable preimages.

The derivation also does not match the circuit — the client hashes `Poseidon(blockNumber, s)` (two
inputs, no domain tag, operands reversed) while the circuits hard-assert
`Poseidon(Poseidon(21), SharedSecrets[i], BlockNumber)` — so every zkdvp proof would fail anyway. And
both scripts POST to `/relay/deposit` and `/relay/withdraw`, which the relayer never registers, so they
404 before anything else. Separately, `utils.Address` is loaded at package init from a CWD-relative
file with all errors discarded, then sliced `[2:]`, which **panics on the empty string**.

**And unlike `transaction/main.go`, these files carry no `DEMO PURPOSE ONLY` marker.** The real issue
is not "test values in a test script" but that the **library** function deriving blinding factors takes
the secrets as parameters, and the only two callers supply public constants with nothing marking them
as scaffolding. An integrator who fixes the two obvious blockers (route names, Poseidon preimage)
inherits a bridge whose commitments are openable by any observer — total loss of balance
confidentiality on the DvP leg.

**Remediation.** Source the secrets from `go_client/agreement`; fix the Poseidon preimage to match the
circuit; mark or delete the scripts; and make `utils.Address` fail loudly rather than panicking on a
slice.

---

#### L-15 — Two different points named "G", and dead commitment helpers that use the wrong one

**Severity: Low (Low/Info boundary) · Exploitability: BROKEN (all affected symbols are dead) ·
Verdict: CONFIRMED — **and the critical sub-question is settled definitively***

One `var` block in `gnark-server/utils/utils.go` declares `GBabyJub` (the standard iden3 base point
`B8`) alongside `CircuitGBabyJub` (what the circuits, client, demo and Solidity actually commit with).
Three exported helpers — `GetPK`, `GetH`, `PedersenCommitmentBabyJub` — build commitments from the
**wrong** one. A maintainer reaching for the obviously-named `utils.PedersenCommitmentBabyJub` would
compute against a different generator than the circuit does.

**Does any live path commit against the wrong generator? No — established by an exact-constant search
over the whole tree.** `B8` appears **exactly once in the entire repository**: its declaration. Its
only consumer is `GetPK`, called only by `PedersenCommitmentBabyJub`, called by nothing. Every live
commitment site uses the circuit generator: `utils/circuits.go:12`, `CurveBabyJubJub.sol:19`,
`go_client/internal/curve/curve.go:10`, `go_client/utils/utils.go:31`, `demo/main.go:119`, two test
files, and the single live server-side use at `enygma_fee/handler.go:112`. **Circuit, contract, client,
demo and tests all agree. There is no divergence to exploit, and nobody should read this as "a live
path uses the wrong G".**

One correction to the original filing: `AddPks` is **not** dead (`enygma_fee/handler.go:108, :113`),
though it is generator-agnostic. The dead list is four symbols, not five. Same file: `ModHint` ignores
its `mod` parameter and re-parses the subgroup order from a literal (now specified in five places, with
no single source of truth) and has an unreachable second `return nil`; `SavingFiles` `log.Fatalf`s on
every failure and can only ever return `nil`, so `generate_keys.go`'s `%w` wrapper is unreachable **and
a mid-write failure leaves a partial key set that a later run will happily load**; and `GetPkHash`
discards `poseidon.Hash`'s error.

**Remediation.** Delete the four dead symbols (confirming first that `enygma_dvp` does not import
them); make `ModHint` use its parameter; give the subgroup order one definition; and make `SavingFiles`
return errors and write atomically.

---

### 5.5 Info

These are real bugs with no security impact. They should still be fixed, and two of them carry
sequencing constraints that matter.

---

**I-01 — Generator `H` IS correctly NUMS-derived; only the source comment is wrong. (LIVE)**
`go_client/internal/curve/curve.go:13` documents the constant as *"randomnly generated"* — the exact
property that would break binding. It is not. The derivation was reimplemented from
`gnark-server/cmd/setup/main.go` and executed:

```
iter 1 valid= False x= 34356466678672179216206944866734405838331831190171667647615530531663699592602
iter 2 valid= True  x= 70594042219340844655004043388548011718365913142946830322468519465099706130288
 y= 8440820911505416007495774568939161580829062250408191090536473986245687707960
 8P= (10100005861917718053548237064487763771145251762383025193119768015180892676690,
      7512830269827713629724023825249861327768672768516116945507944076335453576011)
```

The `8P` output equals the hardcoded `H` in all eight copies. **This also settles a recorded
discrepancy: the search terminates on the SECOND SHA-256 iteration**, not the first — an earlier
auditor's "first iteration" note was wrong even though its constant was right. `H` is
nothing-up-my-sleeve, the seed is the literal `1` with no room to grind, and **the "commitments are not
binding ⇒ mint arbitrarily" attack that this audit's own brainstorm ranked first does not exist.** The
defect is the comment, and its demonstrated cost is that it misdirected this audit's prioritisation.
*Remediation:* fix the comment and add a CI assertion that re-derives `H` and compares it against all
eight hardcoded copies.

**I-04 — `curve.GetNegative(0)` returns `P` instead of `0`; a second copy is wrong the other way.
(LIVE)** `go_client/internal/curve/curve.go:45-50` computes `P - x` unconditionally, so
`GetNegative(0) = P`, which makes a zero-value (dummy/noise) transaction unprovable — the circuit
asserts `TxValues[sender] == P - SenderTxValue` after a 252-bit round-trip and `P` is not the canonical
representative. `go_client/utils/utils.go:71-76` special-cases zero and returns `0`: two functions with
the same name and opposite behaviour at the zero point, in one module. The recorded negative result
holds — `P` *is* the correct negation modulus, since both generators have order exactly `P`; only the
zero case is wrong. ***Sequencing constraint:*** the specification's dummy transaction is exactly the
plaintext anchor **H-02** exploits, so this bug currently *prevents* a privacy hazard. **Fix it only
together with H-02**, or things get worse, not better.

**I-05 — Base-0 vs base-10 parse divergence between the witness and the returned public signal.
(LIVE)** `enygma/handler.go:88` assigns `frontend.Variable(request.BlockNumber)` — a raw string, which
gnark converts with `SetString(v, 0)` (confirmed at the pinned version,
`internal/utils/convert.go:57`, whose doc comment says so) — while the same field is appended to the
response via `utils.ParseBigInt`, which uses base 10. So `"0100"` yields witness 64 and signal 100, and
`"0x1f"` yields witness 31 and signal `null`. The same divergence applies to `SenderId`,
`SenderTxValue` and `SecretKey`. Fails closed on chain, and honest clients emit canonical decimal — but
it is a genuine parser differential on a security boundary. *Remediation:* one base everywhere, and
reject non-canonical input at the binding layer.

**I-03 — Poseidon constant tables disagree on supported widths; `t = 5` panics in one of four.
(LATENT)** `poseidon/constants.go:11-13` accepts `t ∈ {2,3,4,5}` while `:391-393` accepts only
`{2,3,4}`, and `GetPoseidonM`/`GetPoseidonP` both carry `case 5:` arms. `PoseidonEx` fetches all four,
so a four-input hash panics in one of them. Not attacker-reachable — no circuit hashes four inputs, and
the panic fires at `frontend.Compile` time. **But the developer it bites is precisely the one adding a
chain id or contract address to a nullifier or tag** — which is the recommended fix for **L-01** and
**L-11**. The entry's more important content is the recorded **negative result**: the in-circuit
Poseidon was differentially tested against `iden3/go-iden3-crypto` for t = 2, 3, 4 with matching
digests. **Poseidon should not be re-audited.** *Remediation:* add a `case 5:` to `GetPoseidonS` or
remove `t = 5` from the other three tables.

**L-14 — Vendored OpenZeppelin `ERC20.sol` has an added uncapped `mint()` — but nothing inherits it.
(BROKEN)** *(Downgraded from Low to Info per the register's own condition: "if nothing inherits it,
this is Info.")* The modification is real and correctly described: `admin` state, a constructor
assignment, and an `external` non-`virtual` `mint(address,uint)` gated by a bare
`require(msg.sender == admin)` — no setter, no renounce, no cap — in a file whose header claims to be
stock OpenZeppelin v4.4.0. The validation question is settled: **no.** Within the target, no `is ERC20`
and no import of anything under `contracts/utils/contracts/`; the only hardhat project roots elsewhere,
so the directory is never compiled. Across the whole workspace, the modified file's revert string
`'unathorized minting request'` appears in **exactly one place — the file itself**. And the DvP
settlement token is `enygma_dvp/.../RaylsERC20.sol`, which extends the **npm** OpenZeppelin package,
with its own `mint` gated by `onlyRole(DEFAULT_OWNER_ROLE)` — grantable and revocable, materially
better than the vendored file's bare require. **The feared outcome does not obtain.** What remains is
that a local modification hides in a file that reviewers routinely skip, which is exactly what the
audit instructions asked to be checked — and the more valuable half is the recorded negative result:
**eight of nine vendored OpenZeppelin files are faithful** to the upstream tags their headers claim,
differing only in pragma pinning, import paths and doc wording. *Remediation:* delete the three added
lines, or move `mint` into a project-owned subclass.

**I-02 — abigen bindings declare struct arities contradicting their own embedded ABI. (BROKEN)**
`relayer/contracts/enygma.go:42` declares `IEnygmaDepositProof.PublicSignal [2]*big.Int` and `:71`
declares `IEnygmaWithdrawProof.PublicSignal [1]*big.Int`, while the ABI embedded in the same file uses
`uint256[50]` for both. **`go_client/contracts/enygma.go` carries the identical defect at the identical
lines** — the register cites only the relayer copy; **both must be regenerated.** Nothing calls the
affected methods. These are exactly the old `IEnygma.WithdrawProof`/`DepositProof` arities, i.e.
regeneration residue from the older contract generation, not a mystery. *Remediation:* regenerate both
bindings from the current ABI.

**I-06 — The CLI's transfer deltas are a hardcoded array; the `<value>` argument is ignored. (BROKEN)**

```go
// go_client/transaction/main.go:236-246
func GenerateTxValues(value *big.Int) []*big.Int {
	vNegate := curve.GetNegative(value)
	return []*big.Int{vNegate, big.NewInt(60), big.NewInt(40), big.NewInt(0), big.NewInt(0), big.NewInt(0)}
}
```

The credits are literals summing to 100, so `<value>` only balances at exactly 100, and the negated
sender amount always goes to index 0 regardless of `senderId`. Fail-closed confirmed in the circuit
(the conservation and sender-slot assertions all fail), and both functions sit under a
`DEMO PURPOSE ONLY` banner. ***Cross-reference that matters:*** per L-10, the contract-side participant
check does **not** reject the CLI's `participantIds = [0..5]`, so **these circuit assertions are the
only thing keeping the CLI fail-closed**. Fixing I-06 without fixing L-10's id mapping produces landed
transactions that credit and debit the unregistered account 0.

**I-07 — Client config accepts an empty or garbage contract address and never binds it to a network.
(BROKEN)** `go_client/config/config.go:21-52` returns whatever string sits under the `address` key with
no hex-length, checksum or code check, and the caller passes it to `common.HexToAddress`, which
silently yields the zero address for `""`. There is also no network binding: the chain RPC URL and the
prover URL are hardcoded consts while the relayer URL comes from an independent env var, so the client
can read state from one chain while the relayer submits to another with nothing detecting it. **The
relayer publishes exactly the cross-check data needed** — `GET /relay/info`, unauthenticated — **and no
client ever calls it.** All outcomes are fail-closed. *Remediation:* validate the address; and have the
client call `/relay/info` at startup and refuse to run on a chain-id or contract mismatch — which also
addresses part of L-01's missing deployment binding.

---

## 6. The masked-bug ledger

**This is the most actionable section of this report for a developer.**

A bug that is unreachable because of an **unrelated, obvious, one-line defect** is more dangerous than
a bug that is unreachable by design — because the obvious defect will be fixed first, by someone who
has no reason to think they are touching security, and it silently arms the subtle one. Every such
pairing found in this codebase is listed below, with the *precise* unmasking change and the *precise*
thing behind it. Validation corrected the register's ledger in four places; those corrections are
marked.

### 6.1 Fixing the deposit arity arms a balance-destroying bug

**The mask:** `Enygma.sol:500` emits the selector for `verifyProof(uint256[8],uint256[50])`
(`0x18e2c03f`); the deposit circuit and both committed `DepositVerifier.sol` are `uint256[51]`
(`0xcdae3e76`). Neither verifier has a fallback, so `deposit()` reverts `InvalidProof`
unconditionally. This *looks* like a typo.

**Correction to the register: it is not "a two-character change".** Applying `50 → 51` in the two
obvious places **does not compile**:

```
Error: Invalid type for argument in function call. Invalid implicit conversion from
uint256[51] calldata to uint256[50] calldata requested.
   --> Enygma.sol:505:29:  _verifyPublicInputs(proof.public_signal, participantIds, commitmentDeltas);
   --> Enygma.sol:508:28:  _verifyBlockNumber(proof.public_signal);
```

The real minimum is five edits: `IEnygma.DepositProof.public_signal → [51]`, the selector string, and
`[51]` variants of `_verifyPublicInputs`, `_verifyBlockNumber` and `_consumeNullifier`. Still a small,
purely mechanical change any developer makes in one sitting — but **the report must not tell developers
it is two characters**, because on discovering it is five edits they may reach for a different and
possibly worse fix.

**What that change arms, once a deposit verifier is registered:**

| Armed | Why it stays broken after the arity fix |
|---|---|
| **H-15** (High) — `_updateBalances` performs no epoch propagation, so the first deposit after an epoch boundary **zeroes every non-participant's balance**. Executed: participants 1/2/6 survive, non-participant 7 reads `(0,1)`, `check()` reverts `0xca3e0a68` forever. | Nothing about the arity fix touches `_updateBalances`. |
| **M-14's own second half** — `_verifyPublicInputs` reads indices ≤ 49, so the deposit circuit's `Hash` at slot **50** — the only public value tying the shielded leg to the DvP note — **remains unread even at `[51]`**. A naive arity fix makes `deposit()` *work* while leaving the cross-chain binding absent. Nothing would fail; nothing would warn. | Widening the helper signatures does not change which indices they read. |
| **L-11** (Low) — `_nullifiers` is one untagged global mapping; the fee, withdraw and deposit circuits derive **identical** nullifiers, so two operations against one pre-state collide. | Only becomes reachable once a second circuit can write to `_nullifiers`. |

**This is the single most dangerous pattern in the codebase**: a typo-shaped mask over something that
destroys every institution's balance.

**Correction: M-14 is only *one of two doors* onto H-15.** `withdraw` also calls `_updateBalances` and
its verifier lookup has no arity defect, so a **zero-value `withdraw` across an epoch boundary wipes
everyone with no code change at all** — one owner transaction (`addWithdrawVerifier`) opens that door.
Executed; `check()` reverts afterwards. State both doors.

### 6.2 Fixing four missing lines in the withdraw handler — and the four gates that are actually load-bearing

**The mask:** `gnark-server/pkg/circuits/withdraw/handler.go:57-88` never assigns `BlockNumber`,
`Nullifier`, `PreviousSenderBalance` or `PreviousSenderRandomValue`, so `frontend.NewWitness` errors and
all six `/proof/withdraw/*` routes return HTTP 400 unconditionally. `deposit/handler.go:90-93` already
contains exactly those four lines. A developer will copy them across in minutes.

**Two corrections the register's ledger needs — both from independent validators, agreeing.**

1. **Fixing M-03 does NOT arm C-08, C-09, H-14 or H-16.** After the four lines, `withdraw` still
   reverts `VerifierNotFound`, then on `_zkDvpAddress == 0`. There are **four independent gates** on
   the bridge, and the handler is not one of them:
   - `addWithdrawVerifier` and `addZkDvp` have **zero callers anywhere in either repository** — not a
     script, not a test, not a doc.
   - The six `WithdrawVerifier*.sol` live in `contracts/enygmaverifier/zkdvp/`, **outside** the sources
     root of the repository's only hardhat project, so **no build ever produces a withdraw verifier
     artifact to deploy**.
   - On the DvP side, `depositThroughEnygma` is `onlyRole(DEFAULT_ENYGMA_ROLE)`, and the only granter
     (`EnygmaErc20CoinVault.addEnygma`) has **no caller anywhere** — so even a fully wired Enygma would
     be rejected by the vault.
   - `Enygma.deposit`'s `[50]`-vs-`[51]` selector (§6.1) blocks the other half of the bridge
     independently.
2. **M-03 never masked anything from an *attacker*.** `gnark-server/keys/zkdvp/WithdrawPk*.key` are
   git-tracked, so anyone wanting a withdraw proof calls `groth16.Prove` directly — which is exactly
   how C-08's accepted proof was produced during validation. The handler is a convenience the attacker
   does not use.

**So what does M-03 actually mask? The bug from the developers.** Because that endpoint has never
returned a proof, **nobody has ever exercised the withdraw statement end to end** — which is plausibly
why nobody noticed that `withdraw/circuit.go` is the only one of four circuits with no solvency
comparator (**C-08**). The hazard is **sequential, not causal**: the four-line fix is step one of the
path that ends with the bridge enabled, and it is exactly the kind of change that ships without
security review.

**Behind the four gates sit C-08 (no `prevBalance ≥ amount` check anywhere), C-09 (the external leg
unbound to the proof — Critical the moment the bridge is wired), H-14 (empty arrays void every state
binding), H-15 (via the withdraw door), L-12 (external call before state update) and M-16 (six
trapdoors for one statement).**

### 6.3 Fixing one JSON field name arms the whole client CLI and ML-KEM surface

**The mask:** `go_client/internal/types/types.go:23` sends `hashed_shared_secrets` where the enygma
endpoint requires `fingerprint_shared_secrets` — and, per L-08, sends a flat `[]string` where a k×k
`[][]string` is required, plus two semantic hash divergences. Today the CLI has **no live caller at
all**.

**What repairing it arms simultaneously:** **M-10** (spend key on the command line, live with whatever
key a real institution uses), **M-11** (all three ML-KEM defects — no leader, implicit-rejection
ciphertexts cached forever, peer keys manufactured locally — since `go_client/agreement/` is reachable
*only* through this CLI), **L-09** (all-zero proof forwarded as success), **I-06** (hardcoded deltas),
and **L-10**.

**Correction, and it is the sharpest one in this ledger.** L-10's stated failure mechanism was wrong.
The CLI is internally *consistent* with a 0-based account space, so **`_verifyPublicInputsFP` accepts
`participantIds = [0,1,2,3,4,5]`** — the contract-side check is **not** what stops it. The only thing
keeping the CLI fail-closed is a pair of circuit assertions. Therefore:

> **Repairing L-08 (or I-06) without also fixing `generateKIndex` produces a CLI that lands
> transactions crediting and debiting account 0 — the unregistered sentinel, an ownerless sink whose
> value can never be spent and which `check()` (loop starts at `i = 1`) cannot see. That is silent fund
> destruction on every ordinary payment, not a revert.**

Bank 6 is also silently excluded from every CLI transfer.

### 6.4 One owner transaction arms the fee path

**The mask:** nothing calls `addFeeVerifier` outside a single test, and no deploy script deploys
`EnygmaFeeVerifier`. **Everything else about the feature ships**: an `external` function on a live
contract, the verifier committed inside the hardhat sources root, both keys in git, `POST /proof/enygma_fee`
registered, `POST /relay/transfer_fee` registered, abigen bindings generated in both client packages,
and an end-to-end test. **Enabling fees *is* that transaction**, and feature flags get flipped by
operations without code review.

**What it arms:** **C-02's fee instance** — arbitrary supply minting with no accomplice, demonstrated
against the committed `EnygmaFeePk.key`/`EnygmaFeeVk.key` whose Solidity export **is** the committed
`EnygmaFeeVerifier.sol` — and **M-13** (every fee transfer destroys the fee with no recipient and
breaks `check()` permanently). Note the client-side half is **live today**: the relayer already serves
`/relay/transfer_fee`.

**`_feeVerifier` is `private` with no getter. Its live value could not be read offline. One
`eth_getStorageAt` settles whether C-02's fee instance is already exploitable.**

### 6.5 Masks that are unlikely to be lifted — for contrast

Do **not** treat these as one commit away: the modified vendored `ERC20.sol` (**L-14** — nothing
anywhere inherits it, and the DvP settlement token uses the npm OpenZeppelin package instead); the
dead `GBabyJub`/`GetPK`/`GetH`/`PedersenCommitmentBabyJub` helpers (**L-15** — an exact-constant search
over the whole tree found `B8` exactly once, at its own declaration); and `CurveBabyJubJub.isOnCurve`,
`IEnygma.SnarkProof` and `contracts/enygma/interfaces/IERC20.sol`, all confirmed dead.

### 6.6 A safe fix ORDER

Fixes within a group are independent; groups must be done in order. **Every rule here exists because
doing it the other way round arms something worse.**

**Order 0 — before touching any code.** Execute the credential response (C-06): rotate, rewrite
history, redeploy. It does not interact with anything below and it is the only item that is urgent
today.

**Order 1 — the circuits, all in one change, before any verifier is redeployed.** Circuit changes alter
the verifying key, so they cannot be retrofitted after deployment.
1. Bound the `ModHint` quotient at all 28 sites (C-01).
2. Bound every monetary scalar `< P`, not `< 2^252` (C-02 — which also closes the fee-path mint and
   half of H-13).
3. Range-check **every** `TxValues[i]`, not only the sender's (C-03).
4. **Add the solvency comparator to `withdraw/circuit.go` (C-08) — this must land *before* anyone
   fixes `withdraw/handler.go`.**
5. Bind each recipient's shared secret to something the recipient controls (C-04).
6. Restore `Nullifier = Poseidon(domainSep, sk, n_block)` with per-circuit domain separation (M-04,
   L-11) — **and add the missing `case 5:` to `GetPoseidonS` first (I-03), or the compile panics.**
7. Bind the external leg into the public statement (C-09).
Then: new trusted setup — as an **MPC ceremony** (H-12) — re-derive `G` by a published NUMS
construction (H-11), regenerate every verifier, and redeploy.

**Order 2 — the contract, in one change.**
1. Reject duplicate `participantIds`, require `participantIds.length == commitmentDeltas.length`, and
   bind `participantIds` to the proof's k-index signals (C-05, H-14).
2. Validate every participant id against registration state; reject id 0 (H-07).
3. Fix the loop bound at `:802` to `1 .. totalParties` (H-03) — **and when giving `_updateBalances` its
   missing propagation pass (H-15), copy the `_propagateBalancesExcept` form, not
   `_updateBalancesForTransfer`'s, or you import that same off-by-one.**
4. Make `burn` proof-carrying **before** correcting its supply accounting (H-13) — doing the accounting
   first makes `check()` pass after an over-burn and hides the wrap.
5. `staticcall` **and** an explicit `code.length != 0` check at all four verifier call sites — one
   without the other does not close it (M-01).
6. Add an already-registered guard, `accountId != 0` and `publicKey != 0` to `registerAccount`; emit
   the id rather than the counter (M-06). There is **no** trade-off with C-04 recovery: the guard does
   not block registration at a fresh id, which is the only bailout that works.
7. Add `whenInitialized` to `registerAccount` (L-02); add ownership transfer, a getter and role
   separation (H-08).
8. Fix the deposit arity **together with** H-15's propagation pass and a check of signal 50 (§6.1).

**Order 3 — the bridge, only after Orders 1 and 2.** Fix `withdraw/handler.go`'s four lines (M-03);
compile the withdraw verifiers into a real build; delete `contracts/enygmaverifier/` (H-16); make the
split count a real circuit parameter or delete five of six verifiers (M-16); fix the bridge's supply
accounting and its inverted directions (M-15); move state updates before external calls (L-12).

**Order 4 — the client library, in one change.** Fix the JSON field name and both hash divergences
(L-08) **together with** `generateKIndex`'s 0-vs-1-based ids (L-10), the hardcoded deltas (I-06), the
spend key on the command line (M-10), the unchecked HTTP status (L-09), and all three ML-KEM defects
(M-11). **Any subset of these shipped alone is worse than shipping none of them.**

**Order 5 — services, deployment and documentation.** Independent of the above and safe to do at any
time: bind the prover to loopback and authenticate it (H-04, M-08); add relayer timeouts, body limits,
per-bank credentials and rate limits (M-09, H-06, H-10); bind the demo to loopback and authenticate its
`/run/*` routes (H-05); default `deploy_direct.py` to a local chain (M-07); pin compiler settings and
record bytecode hashes (L-03); bind deployed verifiers to their verifying keys (L-04); and mark every
unimplemented mechanism in `README.md`, `protocol_description.md` and `formal_methods/` as design
rather than implementation (M-12, L-06, L-07).

**The privacy redesign (H-01, H-02) is not in any of the layers above, and that is deliberate.** It is
a design change, not a patch (§7.5 item 3): per-transaction, direction-asymmetric blinding, and
removing the fingerprint matrix and the symmetric message tags from public data. It touches the
circuit's asserted tag derivation, so *if* it is undertaken it must land inside **Order 1**, in the
same circuit change and before the new trusted setup — it cannot be retrofitted after the verifiers
are redeployed. H-01's mechanism 2 (the sparse fingerprint matrix) is separately a one-line client fix
and belongs with **Order 4**.

**One cross-cutting sequencing rule that does not fit the layers.** `curve.GetNegative(0)` returning
`P` (**I-04**) currently makes zero-value dummy transactions unprovable — which *accidentally prevents*
the privacy attack in **H-02**, because the specification's own dummy-transaction advice hands an
observer the anchor that opens a whole epoch. **Fix I-04 only together with H-02**, never before —
which, given the paragraph above, means I-04 waits on the privacy redesign rather than shipping as an
easy one-line correctness fix.

---

## 7. Remediation roadmap

§6.6 gives the **fix order**, which is a correctness constraint. This section gives the **priority
order**, which is a risk one. Where the two disagree, §6.6 wins: a fix applied out of order can be
worse than no fix.

### 7.1 Immediate — today, before any code change

**1. Treat the committed relayer key as compromised and respond as to a live incident.**
`0xEa8D34E0aAC0308F58b740768C82e2411A38C2b7`, derived from
`RELAYER_PRIVATE_KEY=b30e25be…36f58` at `contracts/test:9`, holds ≈ **0.717** of the native token at
**nonce 216** on chain **72957**. Move the balance now; stop the relayer using it; and if that address
is registered as an Enygma participant, clear its registration.

**2. Rewrite git history — deleting the file is not remediation.** The key is present in commit
**`bd4e520`** and its ancestors, so it remains fully recoverable from any clone regardless of what HEAD
looks like. **Rotation or history rewriting is required; file deletion is not sufficient.** This
repository has already demonstrated the failure mode: commits `cc808a2` and `3fd294d` (November 2025)
deliberately removed the *owner* key from the working tree, and it is still recoverable and still
present at HEAD in five other places today. Force-push, and invalidate every existing clone across both
remotes.

**3. The scrub must include the binaries.** `enygma_payments/demo/demo` is a **git-tracked 14 MB
compiled Go binary that embeds the owner key, the bearer token and the bank spend keys.** A
`git filter-repo` path list assembled from a grep of *text* files will silently miss it. There is also
a tracked registration binary. Enumerate tracked binaries explicitly.

**4. Rotate the bearer token, and understand that the spend keys cannot be cleanly rotated.**
`enygma-test-secret` appears in `contracts/test:10`, `demo/main.go:62`, `demo/run.sh:5`,
`transaction_test.go:225` and inside the tracked binary. As for the six bank spend keys
(`{424242, 1, 2, 3, 4, 5}`): rotation means re-calling `registerAccount`, which **also resets the
account's balance commitment to `Com(0, randomness)`, adds that point to `totalSupply` a second time,
and increments `_totalRegisteredParties`** (`Enygma.sol:207-215`). **On any deployment carrying real
balances, redeployment — not key rotation — is the honest remediation.** That is what makes C-06 a
redeployment-grade incident rather than a credential-rotation-grade one.

**5. Install a secret scanner.** There is no `.gitleaks.toml`, no `.pre-commit-config.yaml`, and no
`.github/workflows`. A manual scrub has already failed twice.

**6. Answer two questions that could not be answered offline**, each one storage read:
`eth_getStorageAt` the `_feeVerifier` slot — if non-zero, C-02's fee instance is exploitable **today**
and becomes the most urgent technical item in this report; and read `_totalRegisteredParties` against
the highest registered bank id — if they are equal, H-03 re-grades upward immediately. Note that
`owner()` **cannot** be read: no getter exists (H-08).

### 7.2 Urgent — the two circuit soundness fixes

These are the changes that make the live transfer path sound. They must land together, in the order in
§6.6 Order 1, and they require a **new trusted setup and redeployment of every verifier**.

1. **Bound the modular-reduction quotient** at all 28 sites (**C-01**) — `q ≤ 7`, a three-bit
   decomposition, essentially free. Add the regression test that overrides the hint and asserts
   failure; it fails today.
2. **Bound every monetary scalar below `P`, not below `2^252`** (**C-02**). One rule, nine lines,
   closes C-02, the fee-path mint and half of H-13. `grep -n 'ToBinary(.*252)'` finds every candidate.

Then, in the same circuit change because they cannot be retrofitted separately: range-check every
`TxValues[i]` (**C-03**), add the withdraw solvency comparator (**C-08**), bind recipients' shared
secrets (**C-04**), and restore the nullifier derivation (**M-04**, **L-11**).

### 7.3 Urgent — the contract fixes

Deployable without a new setup, but they change contract bytecode, so they belong to the same
redeployment: duplicate-id rejection and length equality (**C-05**, **H-14**); participant-id
validation (**H-07**); the loop bound at `:802` (**H-03**); proof-carrying `burn` (**H-13**);
`staticcall` plus a code check (**M-01**); `registerAccount` guards (**M-06**); `whenInitialized`
(**L-02**); ownership transfer, an `owner()` getter and role separation (**H-08**); and the deposit
arity together with the propagation pass and the slot-50 check (**M-14**, **H-15**).

### 7.4 Important — services and deployment hygiene

Independent of the redeployment and safe to do immediately: loopback binds and authentication for the
proving server and the demo (**H-04**, **H-05**, **M-08**); relayer timeouts, body limits, per-bank
credentials, rate limits and a meaningful `/health` (**M-09**, **H-06**, **H-10**); local defaults for
`deploy_direct.py` and a chain-id assertion (**M-07**); pinned compiler settings and recorded bytecode
hashes (**L-03**); and a build/CI binding between each deployed verifier and its verifying key
(**L-04**, **H-16**).

### 7.5 Design-level — the items that cannot be patched

These are not code fixes and will take longest. They should start now because they gate any claim the
product makes about being trust-minimised.

1. **The trusted setup (H-12).** Run a real multi-party ceremony — Powers-of-Tau phase 1 plus a
   per-circuit phase 2 — with each Privacy Node contributing, and publish the transcript,
   per-contribution attestations and a final beacon. Until then, document the deployment to
   participants as *trusted-issuer*, not trust-minimised. This is the single largest gap between what
   the specification promises institutions and what the software provides.
2. **The provenance of `G` (H-11).** Re-derive it by the same published NUMS construction already used
   correctly for `H`, document both derivations, ship the derivation script, and add a CI assertion
   binding all eight hardcoded copies to it. Cheap, total, and it removes an unfalsifiable trust
   assumption entirely.
3. **The privacy design (H-01, H-02).** Per-transaction, direction-asymmetric blinding; removal of the
   fingerprint matrix and the symmetric tags from public data; genuine anonymity-set *selection*. Note
   that the exposure is retroactive — fixing this does not un-deanonymise existing calldata.
4. **The disclosure design (M-12).** Scoped, revocable, per-transaction regulatory disclosure is not
   achievable with this construction. Redesign it before promising it.
5. **The epoch-length tension (KW-5).** There is no `epochInterval` that is simultaneously private
   (H-02) and live (M-05), and it is `immutable`. Resolve it in the design, not the configuration.
6. **Documentation (M-12, L-06, L-07).** Mark every unimplemented mechanism as design rather than
   implementation; correct the post-quantum migration claim and the performance figures; and either
   remove or precisely qualify the `formal_methods/` claim.

---

## 8. Threat model

This section is the Invariant-Centric Threat Model (ICTM) produced during the audit, condensed.
Its purpose is not to enumerate attacks; it is to state, in language a risk officer can check, **what
users are told they get versus what they get**. ICTM starts from an assumption of *no security
whatsoever* and admits a property only once the audit is confident it is genuinely guaranteed — so the
short list in §8.3 is the honest total, not an omission. Per ICTM, **every row in §8.4 is a bug**:
either the property should be provided, or the documentation should stop implying it.

### 8.1 Adversaries

| Label | Who | What they can do |
|---|---|---|
| **WATCHER** | Anyone at all. | Read every transaction and every piece of contract state on the public chain, forever. No credentials, no network position. |
| **MEMBER** | A participating institution, or anyone holding a member's credentials. | Everything WATCHER can, plus submit payments naming any other member as a participant. |
| **NEIGHBOUR** | Anyone who can reach the operator's internal network, or get an operator to visit a web page. | Plus: reach the proving service and the demo service on their ports, and make the operator's browser issue requests to them. |
| **RELAYER** | Whoever operates or compromises the relayer host. | Plus: see every payment before it reaches the chain, decide whether and in what order it is submitted, and report any outcome it likes. |
| **ISSUER** | The contract owner. | Create money, register and re-register accounts, destroy balances, choose which proof-checking contracts are trusted. **Cannot be replaced or removed.** |
| **READER** | Anyone who has read the source repository. | Holds, today, the relayer's signing key, the relayer's access token, and every bank's spending key (**C-06**). |

**The most important structural fact in this model: READER ⊇ MEMBER.** Anyone who has read the
repository holds credentials equivalent to a member institution's. Most properties below fail for
MEMBER, and therefore fail for the public.

### 8.2 Scenarios

**PILOT** — what the repository's own tooling and documentation produce: contracts on a live public
chain (Rayls 72957), the demo service as the working client, one relayer, six banks. *Every property
below is judged against this.* **CLEAN-KEYS** — a hypothetical deployment with every committed
credential regenerated; used only to separate "broken because the keys are published" from "broken
because the design is broken". **BRIDGED** — the DvP bridge switched on; **no such deployment can
exist today**. **FEE-ENABLED** — fees switched on; also does not exist today, but is **one owner
transaction away**.

### 8.3 Properties that HOLD

These are the only properties this audit is confident the system actually provides today.

| # | Property, in plain language | Status |
|---|---|---|
| **SI-1** | **The issuer cannot create money in secret.** Every act of issuance leaves a permanent public record naming the amount and the recipient. | **HOLDS** — genuinely enforced. It is also why SP-6 does *not* hold: an account that has only ever received issuance has a publicly computable balance. |
| **SI-2** | **An onlooker cannot tell how much moved in a single payment** — provided that payer makes no other payment for the rest of that settlement window and sends no zero-value padding. | **HOLDS, narrowly.** The concealment mechanism itself is sound and would resist a quantum computer. The proviso is the problem (**H-02**). |
| **SI-3** | **The exact same payment instruction cannot be submitted twice within one settlement window.** | **HOLDS**, but by accident rather than by the intended mechanism: what blocks it is that each payment changes every named participant's balance record. The control the specification describes is not implemented (**M-04**). |
| **SI-4** | **One of the two mathematical constants underpinning concealment was chosen in a publicly verifiable, tamper-evident way**, and anyone can re-derive it from a published starting point. | **HOLDS.** Recorded so it is not re-investigated. It says nothing about the *second* constant (**H-11**). |
| **SI-5** | **Where the production client library performs bank-to-bank key exchange, it uses a genuine post-quantum algorithm**, so a recording made today reveals nothing to a future quantum computer. | **HOLDS for the library.** It does **not** hold for the working demonstration client, which fabricates the exchange (**M-02**), nor is that library reachable today (**L-08**). |
| **SI-6** | **Only accounts the issuer has registered can be given a spending key**, and the relayer's access token is compared in a way that leaks nothing through timing and cannot be disabled by leaving it blank. | **HOLDS.** A narrow property: the token authenticates *the consortium*, not *a bank* (**H-06**). |

### 8.4 What users are told they get, and do not

Each row is written the way a risk officer would state the requirement.

**Integrity of money**

| # | The belief | Verdict |
|---|---|---|
| **SP-1** | *"Only the issuer can create money."* | **BROKEN-BY C-01, C-02, C-05** (each independently, all exploitable today), **C-02's fee instance** (one setup step away), **H-13** (after any over-issued write-off). Any single member can create money for itself and spend it into the honest banking system. Three of the live routes were demonstrated by execution against unmodified production code. |
| **SP-2** | *"No one can take money out of my account without my authorisation."* | **BROKEN-BY C-03.** A payer chooses the amounts credited to every named participant, and nothing requires them to be positive. The victim never signs anything. |
| **SP-3** | *"Every payment adds up: what leaves one account arrives in another."* | **BROKEN-BY C-05, H-03** (value silently vanishes), **C-03 + H-07** (value parked on an account belonging to nobody), **M-13** and **M-15** (fee-enabled and bridged scenarios). |
| **SP-4** | *"Anyone can verify that the total money in issue equals the sum of all balances."* | **BROKEN-BY H-13, M-13, M-15, L-02, M-06.** The reconciliation function exists but nothing ever calls it, it aborts rather than reporting a result so it cannot be used as a guard, it is mathematically incapable of detecting the wrap-around family of attacks, and it becomes permanently false after the first write-off, fee payment, bridge movement or out-of-order setup. Once operators learn to ignore it, nothing detects anything. |
| **SP-5** | *"Balances not involved in a payment are carried forward unchanged."* | **BROKEN-BY H-03** (conditionally — see that finding) and **H-15** (in the bridged scenario, one bridge movement zeroes every uninvolved account). |

**Confidentiality**

| # | The belief | Verdict |
|---|---|---|
| **SP-6** | *"My balance is hidden from everyone but me."* | **BROKEN-BY H-02.** Not initially hidden at all: account setup and issuance both publish the information needed to compute the exact balance, so a balance is public until its first private payment — and permanently public if the account only ever receives issuance. The specification's answer, private issuance, is unimplemented (**M-12**). |
| **SP-7** | *"An onlooker cannot tell which of the six banks made a payment."* | **BROKEN-BY H-01**, by separate mechanisms, any one sufficient. k = 6 collapses to k = 1 for every adversary including the weakest. Demonstrated from transaction data alone. |
| **SP-8** | *"An onlooker cannot tell how much was paid."* | **BROKEN-BY H-02** for any bank paying more than once in a window. Worse: the specification's own privacy advice — send a zero-value padding payment — hands the onlooker the exact key needed to open every payment that bank makes or receives for the whole window. Executed end to end. |
| **SP-9** | *"Who banks with whom is our own business."* | **BROKEN-BY H-01** (mechanism 3). Every payment publishes a permanent, never-rotating fingerprint of each pair of institutions that have established a bilateral relationship. |
| **SP-10** | *"The details of each payment are encrypted end-to-end."* | **BROKEN-BY M-12.** No encryption of any kind exists anywhere in the system. |
| **SP-11** | *"My spending key never leaves my institution."* | **BROKEN-BY H-04** (sent in the clear over plain HTTP to a service with no login, listening on every interface), **H-05** (the working client publishes complete spending keys on a stream readable by any web page the operator visits), and **M-10**. And it can never be cleanly changed: there is no key-rotation function. |
| **SP-12** | *"Payment detail is protected against a future quantum computer."* | **PARTLY — and not usefully.** The key exchange is genuinely post-quantum (SI-5), but there is nothing for it to protect (**M-12**), who-paid-whom is already public without any quantum computer (**H-01**), there is no forward secrecy, and the published claim that a future upgrade "will make the entire system quantum-secure" is false (**L-06**). |

**Availability and control**

| # | The belief | Verdict |
|---|---|---|
| **SP-13** | *"No other participant can freeze or destroy my funds."* | **BROKEN-BY C-04.** Any member can, in one ordinary-looking payment costing it nothing, permanently render every other member's entire balance unspendable, with no protocol recovery. Also **H-03**, **H-15**. |
| **SP-14** | *"Payments I submit will be settled."* | **BROKEN-BY M-05** (any member can invalidate every other member's in-flight payment for one transaction's cost), **H-10** (any token holder can empty the relayer's funding account or starve its mutex), **M-09** (single-threaded settlement path with no timeouts, while `/health` reports "ok" regardless), and **H-09** (the relayer can simply decline). |
| **SP-15** | *"Only my institution can submit payments on my behalf."* | **BROKEN-BY H-06.** One shared token authenticates every bank, its value is published, and it travels unencrypted. The relayer cannot tell which bank is calling and cannot revoke one without revoking all. |
| **SP-16** | *"The proving service is an internal component nobody else can drive."* | **BROKEN-BY H-04** (no login of any kind) and **M-08** (no size limits, no timeouts, no rate limiting, a resource leak on every malformed request — and drivable by any web page the operator visits, even on a loopback-only bind). |
| **SP-17** | *"If the operator's credentials were exposed, they could be replaced."* | **BROKEN-BY H-08.** Ownership is fixed permanently at deployment with no transfer, renouncement, recovery — or even a getter. Spending keys have no rotation function. Published credentials cannot be un-published. |
| **SP-18** | *"Proof checking cannot be turned off."* | **BROKEN-BY M-01**, but only through issuer error: pointing the contract at an address with no code makes every payment "valid". The unconfigured state fails closed, so this is not attacker-triggerable. The related and more serious point is that the proof-checking program runs with write access to the ledger's own storage, which it does not need. |

**Governance and assurance**

| # | The belief | Verdict |
|---|---|---|
| **SP-19** | *"The one-time setup ceremony underpinning all proofs was performed by several regulated institutions, so no single party can forge payments."* | **BROKEN-BY H-12.** It was one command, on one machine, run by one unnamed person. The documented security argument does not merely go unimplemented — it **inverts**, from "at least one of several institutions is honest" to "this one anonymous person is honest". No participating institution has any technical means to check. The developers do carry an honest warning that the keys are for demonstration only; nothing in the software enforces it. |
| **SP-20** | *"A regulator can be given a supervised view of the network."* | **BROKEN-BY M-12.** No auditor role, no disclosure mechanism, no oversight surface exists — and the design could not deliver what is promised even if built: the only thing that can be disclosed opens *every* payment between that pair of banks for the whole window, in both directions, permanently, without the counterparty's knowledge or consent. |
| **SP-21** | *"When someone pays me, I can tell that I was paid and how much."* | **BROKEN-BY M-12** (no receiving or scanning software exists) and **C-04** (even correct software could not open the payment, because the payer chooses the recipient's concealment value unilaterally). |
| **SP-22** | *"The design has been formally verified."* | **BROKEN-BY L-07.** Of the three tools claimed, one has no artefact at all, one consists of unproven assumptions about a mechanism that was never built, and one models a completely different cryptographic primitive. |
| **SP-23** | *"The published cost and performance figures describe the software we would run."* | **BROKEN-BY L-06.** Measured: about 15 % more computation and at least 1.7× the on-chain cost stated. |
| **SP-24** | *"We can tell which version of the software is running on the ledger."* | **BROKEN-BY L-03.** No reproducible build, no recorded fingerprint, no verification step, and the only compiled artefacts in the repository are of an older, incompatible version. **No one in this audit could establish which program is actually deployed** — a fact that qualifies every other statement about the live system. |

### 8.5 Known weaknesses — deliberate design properties, not defects

Recorded so reviewers do not spend time on them and institutions are not surprised by them.

- **KW-1 — Participation is never hidden.** Which six institutions are named in a payment is published
  in the clear under stable, real-world identities. Even perfect concealment would still publish who
  transacted with whom, when, and how often.
- **KW-2 — Any bank named in a payment can identify the payer.** By design: the specification
  describes exactly this as how a recipient discovers it has been paid. Concealment of the payer is
  claimed only against parties *outside* the named group. **Because this audit's primary threat actor
  is a participating institution, this deserves prominence rather than a footnote.**
- **KW-3 — A complete, permanent history of every account's concealed balance is public.** Concealment
  protects the amounts, not their existence, timing or frequency.
- **KW-4 — The specification argues a misbehaving payer can only harm itself.** **That argument runs
  backwards in the implementation:** the recipient can never open a payment the payer does not want
  opened, and the payer pays nothing. The software defect it corresponds to is **C-04**.
- **KW-5 — There is no settlement-window length that is both private and live.** Shortening it to
  reduce concealment-value reuse (**H-02**) makes in-flight-payment invalidation (**M-05**) strictly
  worse; at the shortest setting the whole consortium is capped at one payment per block. A genuine
  design tension with no configuration answer — and `epochInterval` is `immutable`.
- **KW-6 — The issuer is unconditionally trusted and cannot be replaced.** A permissioned-ledger
  decision, not a defect — but institutions should understand it is absolute and irrevocable
  (**H-08**).
- **KW-7 — The relayer is a mandatory, undocumented single point of trust and failure.** It appears
  nowhere in the specification or the README (zero occurrences of the string), yet no bank can transact
  without it and it holds the only key that can write to the ledger (**H-09**).
- **KW-8 — Large parts of the documented system are not built.** Payment-detail encryption, key
  rotation, the receiving path, private issuance, the oversight subsystem and the DvP bridge are all
  described and all absent (**M-12**). A reader of the specification is reading a design document, not
  a description of the software.

### 8.6 Where this threat model is itself likely to be wrong

1. **It is judged against a deployment nobody has identified** (L-03, SP-24). Every property is judged
   against the repository, whose newest compiled artefact is an incompatible predecessor.
2. **Two properties turn on single unread values.** SP-1 depends on whether the fee facility is
   switched on; SP-5 depends on one registration counter. Two storage reads would settle both.
3. **Everything about the bridge is judged from one side.** The counterpart contract is out of scope
   and was read only to answer two specific questions.
4. **It says nothing about operational controls outside the repository.** If the operator runs the
   relayer behind an authenticating proxy or the prover on an isolated host, several availability and
   confidentiality properties improve materially. Nothing in the repository configures such controls,
   and this model assumes none.
5. **It draws the boundary at "a bank".** It does not model an individual employee, a compromised
   developer laptop, or the supply chain — even though the repository ships two committed compiled
   binaries, which is a surface this model does not cover.

---

## 9. What we verified as sound

This section is not padding. It has two jobs: to tell developers **where not to spend effort**, and to
show that this audit distinguished real problems from apparent ones. Several of the items below were
this audit's own leading hypotheses before they were checked, and one of them was the *top-ranked*
concern in the initial brainstorm. Each was investigated, disproved, and recorded so that it is not
re-investigated by the next reviewer.

**The generator `H` is a valid nothing-up-my-sleeve point.** Re-derived independently **five times**
across the audit, from the repository's own SHA-256 construction, and matching the hardcoded constant
in all eight copies exactly. The seed is the literal `1` with no room to grind. **The attack this
audit's brainstorm ranked first — "H is not hash-to-curve, therefore commitments are not binding,
therefore mint arbitrarily" — does not exist.** Only the source comment ("randomnly generated") is
wrong, which is **I-01**, an Info-severity documentation defect whose demonstrated cost was that it
misdirected this audit's own prioritisation. Note carefully that this says nothing about the *other*
generator: **H-11 concerns `G`, which is a genuinely different question**, and the same NUMS
construction was run for seeds 0–300 without reproducing it.

**The twisted Edwards addition law is complete for this curve.** `a` is a quadratic residue and `d` a
non-residue mod `Q`, so `CurveBabyJubJub.pointAdd` has no exceptional inputs. Confirmed independently
by three auditors and re-checked during validation. **Attacker-supplied points cannot be off-curve
either** — `commitmentDeltas` are byte-compared against circuit-verified `TxCommit`, and all four
circuits assert on-curveness. The `(0,0)` absorbing behaviour in **H-03** is therefore narrowed to a
single source, an uninitialised storage slot, and must **not** be presented as an attacker-supplied
invalid-point attack.

**Poseidon matches the reference implementation.** The in-circuit Poseidon was differentially tested
against `iden3/go-iden3-crypto` for widths t = 2, 3 and 4, with exact digests recorded, and all match.
Independent arithmetic against the same library during the `H` re-derivation was consistent with the
repository's constants. **Poseidon should not be re-audited.** The only defect found is **I-03**, a
disagreement between constant tables about the unreachable width t = 5.

**Nine of the ten OpenZeppelin-derived files are faithful.** The nine vendored files under
`contracts/utils/` (five contracts, four interfaces) plus `contracts/enygma/interfaces/IERC20.sol`
were diffed against the exact upstream tags their headers claim: they differ only in
pragma pinning, import paths and doc wording. **Exactly one file is modified** — three added lines in
`ERC20.sol` — and **nothing anywhere inherits it**: no `is ERC20` in the target, no import of
`contracts/utils/contracts/` by any contract, the directory is outside the only hardhat sources root,
and the file's distinctive revert string appears in exactly one place in the entire workspace — the
file itself. The DvP settlement token uses the npm OpenZeppelin package with an AccessControl-gated
`mint`. So **L-14 resolves to Info**, and the negative result — the vendored library was not broadly
tampered with — is the more valuable half.

**The relayer cannot forge, alter, redirect or replay a transfer.** Five surfaces were attacked and all
five are closed. (1) *Mutating deltas, signals or the proof* — `_verifyPublicInputsFP:759-764` pins each
delta to a proof-bound public signal, and all 80 signals are inputs to the pairing, so the relayer's
zero-padding gives it no slack. (2) *Substituting an account id* — although `participantIds` is indeed
never compared against the k-index signals, the substitution requires **both** a matching public key
and a matching balance, and registered banks have distinct Poseidon-derived keys. (3) *Truncating or
extending `participantIds`* — the most promising attack, because the two arrays are checked in
*different* loops; it nonetheless **reverts**, because `_updateBalancesForTransfer` indexes
`participantIds[i]` for `i < commitmentDeltas.length` and Solidity 0.8 turns the out-of-bounds calldata
access into `Panic(0x32)`; the reverse direction reverts symmetrically. The two lengths are therefore
forced equal. (4) *Replay* — blocked by the nullifier, the previous-commitment binding and the block
number; and no withdraw route exists on the relayer, so the withdraw-replay amendment does not reach it.
(5) *Front-running a nullifier* — the value is a public signal of a proof the relayer cannot produce.
**H-09 claims only censorship, ordering and fabricated success, and says so.**

**The demo client DOES verify balances homomorphically — the "nothing checks the relayer" claim is
false.** `demo/main.go:1174-1197` re-reads the sender's balance on its **own** RPC connection after the
relay call and applies `AddPedComm` to check it against the expected commitment, failing the flow on a
mismatch. The reference test flows do the same, and `cost_report_test.go:521-540` additionally does a
`TransactionByHash` lookup, which a fabricated hash fails outright. Only the non-functional library CLI
takes the relayer's word. **This refutation is what downgraded H-09 from High to Medium.**

**The relayer's bearer-token comparison is constant time and cannot be left blank.**
`relayer/server/server.go:77` uses `subtle.ConstantTimeCompare`, and `config.go:67`/`:94` default the
token to the empty string and then **hard-fail on it**. There is **no timing oracle and no weak
server default** here; the `change-me` literal is client-side only. H-06's substance is the *design*
(one shared secret, no identity, no revocation), not an implementation flaw.

**`check()` does detect the id-0 sink.** The register recorded that value routed to account 0 is
invisible to the supply invariant. That is wrong: `check()` sums `i = 1 .. _totalRegisteredParties`,
which excludes index 0, so the value drops out of the accounted sum and `check()` reverts
`BalanceMismatch`. It detects but cannot attribute — and, separately, **nothing in the repository
outside tests ever calls `check()`**. State it that way rather than as invisibility. (Value routed to
an *in-range* unregistered id, such as 7, genuinely is silent.)

**The mismatched zkdvp verifiers are the developers' own superseded output, not a foreign setup.** Git
history dates seven of the eight stale files to the project's own key regeneration at commit `e068010`
(2026-03-17) and the eighth to its first commits (November 2025); a search of every `.sol` in the whole
monorepo found none of the eight α constants anywhere else. **The speculative "a trapdoor for these
verifiers may exist in unknown hands" half of H-16 was therefore withdrawn entirely, not softened** —
and the trapdoor concern that does exist is exactly and only H-12. The `[1]`-arity file is likewise not
a soundness hazard: the contract hand-encodes a fixed `[50]` selector and the file declares no
fallback, so it fails closed. **H-16 dropped from High to Low on those two refutations.**

**Other recorded negative results, so they are not re-derived.** `pedCom(0,0)` returns the identity
`(0,1)`, so relayer registration does **not** corrupt the supply invariant — nobody should file that.
All four proof-verification sites **do** guard `verifier == address(0)`; the unconfigured state fails
**closed**, and there is no "unconfigured verifier fails open" bug. `Enygma.sol` has no
`fallback`/`receive`, so a selector mismatch reverts rather than silently succeeding. `addZkDvp` is
correctly excluded from M-01: solc emits an automatic `extcodesize` check for its high-level call.
Encoding a proof struct against a two-argument `verifyProof` signature is **correct**, not an ABI
mismatch — the struct is a fully static tuple. `initialize()` is **not** front-runnable
(`onlyOwner`, `_owner` immutable and constructor-set) — both auditors who suspected it tested and
rejected the hypothesis. `api.ToBinary(scalar, 256)` inside `ScalarMul` is **not** an independent
aliasing source: gnark clamps to 254 digits and emits the reducedness constraint. The client's
randomness package is a **deterministic KDF by design**, not weak randomness. There is no `math/rand`
or time-seeding anywhere in non-test Go outside generated bindings. `gin.Recovery()` **is** installed
on both HTTP services, so remote panics return 500 rather than killing the process. `CurveBabyJubJub.isOnCurve`
is mathematically correct (and dead). The main transfer verifier **is** in sync with its proving key —
`EnygmaVerifier.sol` and `EnygmaFeeVerifier.sol` both reproduce byte-for-byte from their committed
verifying keys, which is the positive control that validates the method used to condemn the zkdvp set.
And `withdraw`'s public-input arity is **correct** at 50 — only `deposit` is mis-arited.

**And one thing that is not sound but is worth stating precisely:** the specification's dummy-transaction
privacy advice is currently *inert* because of the `GetNegative(0)` bug (**I-04**). That bug is
protecting the system today. It must not be fixed on its own.

---

## 10. Appendix — audit statistics

### 10.1 Issue accounting

| | count |
|---|---:|
| Raw plausible-issue files produced by local and global auditing | **97** |
| Consolidated register entries (deduplicated, severity-calibrated) | **63** |
| Input files merged into another entry | 34 |
| Entries withdrawn as false positives at consolidation | 0 |
| Files referenced by a global analysis but never written to disk | 1 |
| **Issue files confirmed after validation** (`issues/confirmed/`) | **96** |
| **Issue files invalidated after validation** (`issues/invalid/`) | **1** |
| Issue files left unvalidated (`issues/plausible/`) | **0** |
| **Findings reported here** (C-07 merged into C-02) | **62** |

The single invalidated file is `L9-nonce-gap-stalls-settlement.md` (M-09 sub-claim 3), refuted by
reading the pinned `go-ethereum` source: the relayer leaves `auth.Nonce` nil, so every submission
re-queries the pending nonce and the eviction event the claim depended on is what repairs the
condition. The one file referenced but never written was a casualty of an API limit during the global
auditors' wrap-up; its substance is preserved inside H-01 and H-09.

### 10.2 Final severity distribution

| Severity | LIVE | LATENT | BROKEN | **Total** |
|---|---:|---:|---:|---:|
| **Critical** | 6 | 0 | 0 | **6** |
| **High** | 6 | 0 | 3 | **9** |
| **Medium** | 13 | 2 | 4 | **19** |
| **Low** | 9 | 3 | 8 | **20** |
| **Info** | 3 | 1 | 4 | **8** |
| **Total** | **37** | **6** | **19** | **62** |

### 10.3 Severity and reachability changes made at validation

Eighteen findings moved: **fifteen severity changes, every one downwards**; **three reachability
re-classifications** (H-03 and L-02 down, H-13 up — H-13's severity itself did not change); and **one
merge**. H-03 appears in both the severity and the reachability count. Validation is where this
audit's calibration was done.

| Finding | Register | Final | Reason |
|---|---|---|---|
| C-07 | Critical | **merged into C-02** | Not independent: every fee-path mint requires C-02's aliasing, and fixing C-02 closes it |
| C-08 | Critical | **High** | Zero users affected on any deployment the repository can produce; four gates, and a source change is needed |
| C-09 | Critical | **Medium** | Same, plus the DvP-side role gate; **Critical-on-arming** |
| H-03 | High / LIVE | **Low / LATENT** | Fires on no configuration the repository produces; the protection is accidental (the relayer's id-100 registration) |
| H-04 | High | **Medium** | No shipped client can send secrets to a remote prover; the keys are already public via C-06 |
| H-06 | High | **Medium** | The token buys submission, not theft; the concrete harm is H-10 and the exposure is C-06 |
| H-07 | High | **Medium** | Narrowed to id 0 and unregistered ids ≤ N; value is destroyed, not stolen; `check()` sees the id-0 case |
| H-08 | High | **Medium** | Split: the committed key is Low (all consumers default local, nonce 0 on 72957); immutable ownership is the substance |
| H-09 | High | **Medium** | The "no client verifies" half **refuted** — the shipped client does verify homomorphically |
| H-10 | High | **Medium** | Magnitude corrected ~125× (128 KB tx cap → ~2.4 M gas, not 300 M); availability-only; redundant with C-06 today |
| H-13 | High / LATENT | **High / LIVE** | Raised in reachability: `burn` is a normal issuer operation, not a test-only path |
| H-16 | High | **Low** | Both High-carrying claims refuted: no foreign setup (git history), and the `[1]` verifier fails closed |
| M-02 | Medium | **Low** | Nothing anywhere reads `viewKeys`; the mechanism does not exist to be broken |
| M-03 | Medium | **Low** | The masking claim that justified the raise does not survive; it arms nothing by itself |
| M-04 | Medium | **Low** | No user can lose or duplicate value today; the previous-commitment binding is a stronger control |
| M-10 | Medium | **Low** | The path is dead, and the secret it leaks is already a public constant (C-06) |
| L-02 | Low / LIVE | **Low / LATENT** | With the randomness every production caller passes, the ordering has no effect |
| L-14 | Low | **Info** | Nothing anywhere inherits the modified file; the DvP token uses npm OpenZeppelin |

### 10.4 Phases run

| Phase | Scope |
|---|---|
| 0 — Survey | Whole target; produced the system description |
| 1 — Brainstorm | Attacker goals and candidate weaknesses, expanded throughout |
| 2 — Threat model | ICTM, refined at consolidation and again after validation |
| 3 — Local audit | **13 module batches**, executed as **10 local-auditor passes** (three adjacent batch pairs were reviewed together), covering every non-excluded file (contracts, circuits, setup and keygen, Poseidon, client crypto, key agreement, zkdvp client, relayer, deployment, demo) |
| 4 — Global audit | **6 focus areas** (G1–G6): reachability and severity calibration; supply invariants and the bridge; privacy and deanonymisation; unregistered ids, fee-path minting and the fail-open pattern; nullifiers, replay and domain separation; documentation-versus-implementation |
| 5 — Consolidation | 97 raw files → 63 findings, cross-checked against every recorded negative result |
| 6 — Validation | **63 dedicated adversarial passes**, refute-first, with executed proofs of concept |
| 7 — Report | This document |

### 10.5 Reproduction artefacts

Executed harnesses are preserved alongside the audit state, including: the `ModHint` override
demonstration and its five-test Foundry suite against the real contracts (C-01); the subgroup-aliasing
harness with three constraint-level negative controls and a `cast call` against the deployed verifier
bytecode (C-02); the stock-server attack, the anvil end-to-end run and the independent commitment
recomputation (C-03); the five-stage freeze demonstration (C-04); the rollover exploit with its matched
same-epoch control (C-05); the spending-witness proof from committed credentials (C-06); the
withdraw-circuit satisfiability suite with three negative controls (C-08); the two-transfer
deanonymisation and amount-recovery runs against the real proving server (H-01, H-02); the relayer
gas-drain and dedup-bypass harness (H-10); the `solc`-plus-anvil empty-array and epoch-wipe tests
(H-14, H-15); the α-constant verifier-matching script and its git-history provenance search (H-16); the
goroutine-leak measurement (M-08); the two-host ML-KEM divergence run (M-11); and the `H` re-derivation
(I-01).

### 10.6 Closing note

This audit was performed by AI and this report was written by AI. Every finding was subjected to an
adversarial validation pass instructed to refute it first, and the corrections that pass produced —
one merge, fifteen severity changes (all downwards), three reachability re-classifications, one
invalidation, and several strengthenings from "argued" to "executed" — are recorded in the findings
themselves. Bugs will have been missed. The absence of a
finding here is not evidence that a defect does not exist, and no statement in this report about the
live deployment on chain 72957 should be read as settled, because **no one in this audit was able to
establish which program is running there**.

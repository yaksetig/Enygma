# Flow 06 — ZkDvP Atomic Swap (ERC20 ↔ ERC20)

## Overview

The ZkDvP swap lets Alice exchange one type of token (e.g., 5 USDT with `tokenId=10`) for Bob's
different token type (e.g., 1 concert ticket with `tokenId=25`), all within the same vault — without
any trusted intermediary and without either party having to trust the other to act first.

Both assets use the standard ERC20 JoinSplit commitment formula with different `tokenId` values:

```
commitment = Poseidon4(pk_spend, saltBField, amount, tokenId)
```

The protocol is **asymmetric and two-phase**:

- **Phase 1 (Alice initiates)**: Alice generates a ZK proof spending her USDT note and submits it
  on-chain as a pending swap, pre-committing to both outputs.
- **Phase 2 (Bob completes)**: Bob scans the on-chain event, verifies the pre-committed outputs,
  generates his own ZK proof, and triggers atomic settlement.

---

## Atomicity — Cross-commitment linking

Atomicity is enforced by **cross-commitment linking**, not a shared `swapId`:

```
stMessage(Alice) = C'            (Alice's expected concert ticket commitment)
firstOutput(Alice) = CommitmentB  (Alice's USDT payment for Bob)
stMessage(Bob)   = CommitmentB   (links Bob's proof back to Alice's pending proof)
firstOutput(Bob) = C'            (Bob delivers exactly the ticket commitment Alice pre-committed to)
```

The on-chain `_settleOnGroupPair` verifies:

```
receipt_alice.statement[0] == receipt_bob.statement[7]   // stMsg(Alice) == firstOut(Bob)
receipt_bob.statement[0]   == receipt_alice.statement[7] // stMsg(Bob)   == firstOut(Alice)
```

A party cannot alter the outputs after the other party's proof is submitted — any mismatch reverts.

---

## Non-interactivity

After a one-time public-key exchange, Alice initiates without waiting for Bob:

| Party | Needs from the other party                  | Can proceed without knowing       |
| ----- | ------------------------------------------- | --------------------------------- |
| Alice | `pk_bob` (spend), `encapKey_bob` (view)     | Bob's proof or ticket note        |
| Bob   | `pk_alice` (spend), `encapKey_alice` (view) | Alice's proof or USDT note        |

Bob responds asynchronously after seeing the `PendingProofAddedToVault` event on-chain.

---

## Participants

| Participant  | Role                                                                                          |
| ------------ | --------------------------------------------------------------------------------------------- |
| Alice        | Initiator — spends her USDT note, pre-commits to receiving the concert ticket                 |
| Bob          | Completer — verifies Alice's pre-commitments, spends his ticket note, triggers settlement     |
| Gnark Server | Generates both Groth16 JoinSplit proofs (ERC20 circuit, different tokenIds)                  |
| EnygmaDvp    | Stores Alice's proof as PENDING, settles atomically when Bob's matching proof arrives         |

---

## Diagram

```mermaid
sequenceDiagram
    participant Alice
    participant Gnark as Gnark Server
    participant DVP as EnygmaDvp
    participant Bob

    rect rgb(220, 235, 255)
        Note over Alice,Bob: Step 1 — Agree on swap terms (off-chain)

        Alice->>Bob: pk_alice, encapKey_alice (view)
        Bob->>Alice: pk_bob, encapKey_bob (view)
        Note over Alice,Bob: Alice sends 5 USDT (tokenId=10) ↔ Bob sends 1 ticket (tokenId=25)
    end

    rect rgb(220, 255, 220)
        Note over Alice,Gnark: Step 2 — Alice initiates the swap (Phase 1)

        Alice->>Alice: Encapsulate(encapKey_bob)
        Note over Alice: saltB = 0x4f2a..., ctI = 0x9d3e...
        Alice->>Alice: CommitmentB = Poseidon4(pk_bob, SaltBToField(saltB), 5, 10)
        Note over Alice: CommitmentB = 9102837465... (USDT for Bob)

        Alice->>Alice: saltStar = random
        Alice->>Alice: C' = Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25)
        Note over Alice: C' = 4829301756... (ticket for Alice)

        Alice->>Alice: ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)

        Alice->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: StMessage = C' = 4829301756...
        Note over Gnark: assert Poseidon4(pk_alice, saltIn, 5, 10) == cmt_usdt_alice
        Note over Gnark: assert MerkleProof(cmt_usdt_alice, path) == root
        Note over Gnark: assert Poseidon4(pk_bob, saltBField, 5, 10) == CommitmentB
        Note over Gnark: assert 5 == 5 + 0

        Gnark-->>Alice: proof = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Alice: stmt = [C', tree0, root, null_alice, 0, 0, 0, CommitmentB, dummy]

        Alice->>DVP: submitPartialSettlement(receipt_alice, vaultId=0, groupId=FUNGIBLES)
        Note over DVP: stored: _pendingTx[CommitmentB] = {targetId: C', vault: 0}
        DVP-->>Alice: emit PendingProofAddedToVault(vault=0, group=0, C', receipt)
    end

    rect rgb(255, 240, 220)
        Note over Bob,DVP: Step 3 — Bob completes the swap (Phase 2)

        Bob->>Bob: saltB' = Decapsulate(decapKey_bob, ctI)
        Bob->>Bob: (tokenId=25, amount=1, saltStar) = DecryptSwapPayload(saltB', ctII)
        Bob->>Bob: assert Poseidon4(pk_bob, SaltBToField(saltB'), 5, 10) == CommitmentB ✓
        Bob->>Bob: assert Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) == C' ✓

        Bob->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: StMessage = CommitmentB = 9102837465...
        Note over Gnark: assert Poseidon4(pk_bob, saltBob, 1, 25) == cmt_ticket_bob
        Note over Gnark: assert MerkleProof(cmt_ticket_bob, path) == root
        Note over Gnark: assert Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) == C'
        Note over Gnark: assert 1 == 1 + 0

        Gnark-->>Bob: proof = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Bob: stmt = [CommitmentB, tree0, root, null_bob, 0, 0, 0, C', dummy]

        Bob->>DVP: submitPartialSettlement(receipt_bob, vaultId=0, groupId=FUNGIBLES)
        Note over DVP: lookup: _pendingTx[CommitmentB].targetId = C' == receipt_bob.firstOut ✓
        Note over DVP: exchangeOnGroupPair(receipt_alice, receipt_bob, vault0, vault0)
        Note over DVP: assert stmt_alice[0]=C' == stmt_bob[7]=C' ✓
        Note over DVP: assert stmt_bob[0]=CommitmentB == stmt_alice[7]=CommitmentB ✓
        Note over DVP: setNullifier(tree0, null_alice)
        Note over DVP: setNullifier(tree0, null_bob)
        Note over DVP: insertLeaves([CommitmentB, dummy])
        Note over DVP: insertLeaves([C', dummy])

        DVP-->>Bob: emit Commitment(vault=0, tree0, CommitmentB)
        DVP-->>Alice: emit Commitment(vault=0, tree0, C')
        DVP-->>Alice: emit Nullifier(vault=0, tree0, null_alice)
        DVP-->>Bob: emit Nullifier(vault=0, tree0, null_bob)
    end

    rect rgb(240, 220, 255)
        Note over Alice,Bob: Step 4 — Scan for received notes

        Bob->>Bob: ScanForErc20Notes(decapKey_bob, pk_bob, events)
        Note over Bob: Decapsulate(decapKey_bob, ctI) → saltB
        Note over Bob: DecryptPayload(saltB, ctII_deposit) → tokenId=10, amount=5
        Note over Bob: Poseidon4(pk_bob, saltBField, 5, 10) == CommitmentB ✓

        Note over Alice: Alice already knows saltStar (she generated it)
        Note over Alice: Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) == C' ✓
        Note over Alice: Alice spends C' using saltStarField as WtSaltsIn
    end
```

---

## Key references

| Symbol                    | File                                                         | Line |
| ------------------------- | ------------------------------------------------------------ | ---- |
| `ZkDvpInitiateSwap`       | `src/core/prover_erc.go`                                     | 231  |
| `Erc20JoinSplitProofFromSalts` | `src/core/prover_erc.go`                                | 680  |
| `ScanForZkDvpSwap`        | `src/core/scan.go`                                           | 237  |
| `EncryptSwapPayload`      | `src/core/utils.go`                                          | 264  |
| `DecryptSwapPayload`      | `src/core/utils.go`                                          | —    |
| `Erc20CommitmentV2`       | `src/core/utils.go`                                          | 563  |
| `GetNullifier`            | `src/core/utils.go`                                          | —    |
| `Encapsulate`             | `src/core/utils.go`                                          | 216  |
| `SaltBToField`            | `src/core/utils.go`                                          | 239  |
| `submitPartialSettlement` | `contracts/core/contracts/EnygmaDvp.sol`                     | 598  |
| `_settleOnGroupPair`      | `contracts/core/contracts/EnygmaDvp.sol`                     | 798  |
| `exchangeOnGroupPair`     | `contracts/core/contracts/EnygmaDvp.sol`                     | 773  |
| `Erc20Circuit.Define`     | `gnark_circuits/templates/ERC20.go`                          | —    |
| Unit test                 | `test/06_v2_swap_erc721_erc20_test.go`                       | —    |
| Full ZkDvp test           | `test/12_v2_zkdvp_swap_test.go`                              | —    |

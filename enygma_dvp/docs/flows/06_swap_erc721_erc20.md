# Flow 06 — ZkDvP Atomic Swap (ERC20 ↔ ERC721)

## Overview

The ZkDvP swap lets Alice exchange ERC20 tokens (e.g., 5 USDT) for Bob's ERC721 NFT
(e.g., a concert ticket with `tokenId=25`) — without any trusted intermediary and without
either party having to trust the other to act first.

The protocol is **asymmetric and two-phase**:

- **Phase 1 (Alice initiates)**: Alice generates an ERC20 JoinSplit proof spending her USDT note
  and submits it on-chain as a pending swap, pre-committing to both outputs.
- **Phase 2 (Bob completes)**: Bob scans the on-chain event, verifies the pre-committed outputs,
  generates an ERC721 OwnershipProof, and triggers atomic settlement.

---

## Commitment formulas

```
Alice's USDT note:  Poseidon4(pk_alice, saltA, amount, tokenId=0)       // ERC20 JoinSplit
Bob's ticket note:  Poseidon4(pk_bob,   saltB, 1, tokenId=25)           // ERC721 Ownership
C' (Alice receives): Poseidon4(pk_alice, saltStar, 1, tokenId=25)       // ≡ Erc721Commitment
```

Note: `Erc721Commitment(tokenId, pk, salt) = Poseidon4(pk, salt, 1, tokenId)` — same formula
as `Erc20CommitmentV2(pk, salt, 1, tokenId)`. Alice uses this equivalence to pre-compute C'
inside her ERC20 JoinSplit proof.

---

## Atomicity — Cross-commitment linking

```
stMessage(Alice) = C'             // Alice's expected ERC721 ticket commitment
firstOutput(Alice) = CommitmentB  // Alice's USDT payment for Bob

stMessage(Bob)   = CommitmentB    // links Bob's ERC721 proof back to Alice's pending proof
firstOutput(Bob) = C'             // Bob delivers exactly the ticket commitment Alice pre-computed
```

The on-chain `_settleOnGroupPair` verifies (ERC20 has 2 inputs, ERC721 has 1 input):

```
receipt_alice.statement[0] == receipt_bob.statement[4]   // stMsg(Alice)=C'          == firstOut(Bob)=C'          ✓
receipt_bob.statement[0]   == receipt_alice.statement[7]  // stMsg(Bob)=CommitmentB   == firstOut(Alice)=CommitmentB ✓
```

---

## Settlement path

When Bob (NON_FUNGIBLES) submits second via `submitPartialSettlement`, the contract:

1. Looks up the stored pending proof: `_pendingTx[CommitmentB] = {vaultId: 0, groupId: FUNGIBLES}`
2. Detects current group = NON_FUNGIBLES → delivery submitted second
3. Calls `swapOnGroupPair(aliceERC20, bobERC721, vault=0, vault=1, FUNGIBLES, NON_FUNGIBLES)`

The swap pair `(FUNGIBLES, NON_FUNGIBLES)` is registered at deployment via `registerSwapGroupPair`.

---

## Participants

| Participant  | Role                                                                                            |
| ------------ | ----------------------------------------------------------------------------------------------- |
| Alice        | Initiator — spends her ERC20 USDT note, pre-commits to receiving the ERC721 ticket             |
| Bob          | Completer — verifies Alice's pre-commitments, spends his ERC721 note, triggers settlement      |
| Gnark Server | Generates Alice's ERC20 JoinSplit proof and Bob's ERC721 OwnershipProof                        |
| EnygmaDvp    | Stores Alice's proof as PENDING, settles atomically when Bob's matching proof arrives           |

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
        Note over Alice,Bob: Alice sends 5 USDT (ERC20) ↔ Bob sends concert ticket (ERC721, tokenId=25)
    end

    rect rgb(220, 255, 220)
        Note over Alice,Gnark: Step 2 — Alice initiates the swap (Phase 1)

        Alice->>Alice: Encapsulate(encapKey_bob)
        Note over Alice: saltB = 0x4f2a..., ctI = 0x9d3e...
        Alice->>Alice: CommitmentB = Poseidon4(pk_bob, SaltBToField(saltB), 5, 0)
        Note over Alice: CommitmentB = 9102837465... (USDT for Bob)

        Alice->>Alice: saltStar = random
        Alice->>Alice: C' = Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25)
        Note over Alice: C' = 4829301756... (= Erc721Commitment(25, pk_alice, saltStar))

        Alice->>Alice: ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)

        Alice->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: StMessage = C' = 4829301756...
        Note over Gnark: assert Poseidon4(pk_alice, saltIn, 5, 0) == cmt_usdt_alice
        Note over Gnark: assert MerkleProof(cmt_usdt_alice, path) == root20
        Note over Gnark: assert Poseidon4(pk_bob, saltBField, 5, 0) == CommitmentB
        Note over Gnark: assert 5 == 5 + 0

        Gnark-->>Alice: proof20 = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Alice: stmt20 = [C', tree0, root20, null_alice, 0, 0, 0, CommitmentB, dummy]

        Alice->>DVP: submitPartialSettlement(receipt_alice, vaultId=0, groupId=FUNGIBLES)
        Note over DVP: stored: _pendingTx[CommitmentB] = {targetId: C', vault: 0, group: FUNGIBLES}
        DVP-->>Alice: emit PendingProofAddedToVault(vault=0, group=FUNGIBLES, C', receipt)
    end

    rect rgb(255, 240, 220)
        Note over Bob,DVP: Step 3 — Bob completes the swap (Phase 2)

        Bob->>Bob: saltB' = Decapsulate(decapKey_bob, ctI)
        Bob->>Bob: (tokenId=25, amount=1, saltStar) = DecryptSwapPayload(saltB', ctII)
        Bob->>Bob: assert Poseidon4(pk_bob, SaltBToField(saltB'), 5, 0) == CommitmentB ✓
        Bob->>Bob: assert Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) == C' ✓

        Bob->>Gnark: POST /proof/ownershipERC721
        Note over Gnark: StMessage = CommitmentB = 9102837465...
        Note over Gnark: assert Poseidon4(pk_bob, saltBob, 1, 25) == cmt_ticket_bob
        Note over Gnark: assert MerkleProof(cmt_ticket_bob, path) == root721
        Note over Gnark: assert Poseidon4(pk_alice, SaltBToField(saltStar), 1, 25) == C'

        Gnark-->>Bob: proof721 = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Bob: stmt721 = [CommitmentB, tree1, root721, null_bob, C']

        Bob->>DVP: submitPartialSettlement(receipt_bob, vaultId=1, groupId=NON_FUNGIBLES)
        Note over DVP: lookup: _pendingTx[CommitmentB].targetId = C' == receipt_bob.stmt[4] ✓
        Note over DVP: groupId=NON_FUNGIBLES → swapOnGroupPair(aliceERC20, bobERC721, 0, 1, FUNGIBLES, NON_FUNGIBLES)
        Note over DVP: assert stmt20[0]=C' == stmt721[4]=C' ✓
        Note over DVP: assert stmt721[0]=CommitmentB == stmt20[7]=CommitmentB ✓
        Note over DVP: setNullifier(tree0, null_alice)
        Note over DVP: setNullifier(tree1, null_bob)
        Note over DVP: insertLeaves(vault0, [CommitmentB, dummy])
        Note over DVP: insertLeaves(vault1, [C'])

        DVP-->>Bob: emit Commitment(vault=0, tree0, CommitmentB)
        DVP-->>Alice: emit Commitment(vault=1, tree1, C')
        DVP-->>Alice: emit Nullifier(vault=0, tree0, null_alice)
        DVP-->>Bob: emit Nullifier(vault=1, tree1, null_bob)
    end

    rect rgb(240, 220, 255)
        Note over Alice,Bob: Step 4 — Scan for received notes

        Bob->>Bob: ScanForErc20Notes(decapKey_bob, pk_bob, events)
        Note over Bob: Decapsulate(decapKey_bob, ctI_deposit) → saltB
        Note over Bob: Poseidon4(pk_bob, saltBField, 5, 0) == CommitmentB ✓

        Note over Alice: Alice already knows saltStar (she generated it)
        Note over Alice: Erc721Commitment(25, pk_alice, saltStarField) == C' ✓
        Note over Alice: Alice spends C' using saltStarField as WtSaltsIn
    end
```

---

## Key references

| Symbol                         | File                                     | Line |
| ------------------------------ | ---------------------------------------- | ---- |
| `ZkDvpInitiateSwap`            | `src/core/prover_erc.go`                 | 231  |
| `Erc721OwnershipProofFromSalt` | `src/core/prover_erc.go`                 | 610  |
| `ScanForZkDvpSwap`             | `src/core/scan.go`                       | 237  |
| `EncryptSwapPayload`           | `src/core/utils.go`                      | 264  |
| `DecryptSwapPayload`           | `src/core/utils.go`                      | —    |
| `Erc721Commitment`             | `src/core/utils.go`                      | 577  |
| `Erc20CommitmentV2`            | `src/core/utils.go`                      | 563  |
| `Encapsulate`                  | `src/core/utils.go`                      | 216  |
| `SaltBToField`                 | `src/core/utils.go`                      | 239  |
| `submitPartialSettlement`      | `contracts/core/contracts/EnygmaDvp.sol` | 598  |
| `_settleOnGroupPair`           | `contracts/core/contracts/EnygmaDvp.sol` | 798  |
| `swapOnGroupPair`              | `contracts/core/contracts/EnygmaDvp.sol` | 727  |
| `Erc20Circuit.Define`          | `gnark_circuits/templates/ERC20.go`      | —    |
| `Erc721Circuit.Define`         | `gnark_circuits/templates/ERC721.go`     | —    |
| Unit test                      | `test/06_v2_swap_erc721_erc20_test.go`   | —    |
| Full ZkDvp test                | `test/12_v2_zkdvp_swap_test.go`          | —    |

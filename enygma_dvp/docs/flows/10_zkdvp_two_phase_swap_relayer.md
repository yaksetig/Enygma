# Flow 10 — ZkDvP Two-Phase Swap with Relayer (ERC20 ↔ ERC721)

## Overview

Alice swaps 5 USDT (ERC20, tokenId=0) for Bob's concert ticket (ERC721, tokenId=25).
Both parties generate their ZK proofs independently and off-chain. A **relayer** collects
both `ProofReceipt`s and submits them in a single atomic call to `EnygmaDvp.swap()`.

Compared to [Flow 06](./06_swap_erc721_erc20.md) which uses two independent
`submitPartialSettlement` calls (on-chain coordination), this flow uses one atomic
`swap()` call — the relayer coordinates off-chain so only one transaction lands on-chain.

Commitment formulae:

```
ERC20 note:  Poseidon4(pk_spend, saltBField, amount, tokenId=0)
ERC721 note: Poseidon4(pk_spend, saltBField, amount=1, tokenId)
```

---

## Atomicity

`_settleOnGroupPair` verifies cross-commitment consistency before settling:

```
alicePaymentReceipt.stmt[0] == bobDeliveryReceipt.stmt[4]   // C' == C'
bobDeliveryReceipt.stmt[0]  == alicePaymentReceipt.stmt[7]  // CommitmentB == CommitmentB
```

Mapping to this swap:

```
stMessage(Alice) = C'            ← Alice's expected ERC721 ticket, equals Bob's output at stmt[4]
firstOutput(Alice) = CommitmentB ← Alice's USDT payment for Bob, equals Bob's stMessage at stmt[0]
stMessage(Bob)   = CommitmentB   ← links Bob's proof to Alice's payment
firstOutput(Bob) = C'            ← Bob delivers exactly the ticket Alice pre-computed
```

Any mismatch between the two receipts reverts the entire transaction.

---

## Statement layouts

**ERC20 payment receipt** (2-in / 2-out, non-interleaved, 9 elements):

```
[msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1]
 [0]   [1]    [2]   [3]   [4]    [5]    [6]    [7]   [8]
                                                 ↑ CommitmentB (Bob's USDT) at index 7
```

**ERC721 delivery receipt** (1-in / 1-out, 5 elements):

```
[msg, treeNum, merkleRoot, nullifier, cmt]
 [0]   [1]      [2]         [3]       [4]
                                       ↑ C' (Alice's ticket) at index 4
```

---

## Relayer

The relayer submits the transaction on behalf of both parties using its own Ethereum key.

It **cannot**: forge or alter proofs (on-chain Groth16 verifier rejects), steal funds
(outputs are bound to recipients' public keys), or see private inputs.

It **can**: choose when to submit (liveness trust only) and pays gas.

---

## Participants

| Participant  | Role                                                                                    |
| ------------ | --------------------------------------------------------------------------------------- |
| Alice        | Initiator — spends her USDT note, pre-commits to receiving the concert ticket           |
| Bob          | Completer — decrypts swap payload, spends his ticket note                               |
| Gnark Server | Generates Alice's ERC20 JoinSplit proof and Bob's ERC721 Ownership proof                |
| Relayer      | Collects both ProofReceipts, submits `EnygmaDvp.swap()` with its own Ethereum key      |
| EnygmaDvp    | Verifies both proofs, checks cross-commitment consistency, settles atomically in one tx |

---

## Diagram

```mermaid
sequenceDiagram
    participant Alice
    participant Gnark as Gnark Server
    participant Relayer
    participant DVP as EnygmaDvp
    participant Bob

    rect rgb(220, 235, 255)
        Note over Alice,Bob: Step 1 — Alice deposits 5 USDT into ERC20 vault

        Alice->>DVP: ERC20.mint + approve
        Alice->>Alice: Encapsulate(aliceViewKey) → saltB, capsule
        Alice->>Alice: aliceUSDTCmt = Poseidon4(pk_alice, saltBField, 5, 0)
        Alice->>DVP: erc20Vault.depositV2([5, aliceUSDTCmt], capsule, ctII)
        DVP-->>Alice: emit Commitment(vaultId=0, aliceUSDTCmt)
        Alice->>Alice: loadVaultMerkleTree() → aliceMerkleProof
    end

    rect rgb(220, 255, 220)
        Note over Bob,DVP: Step 2 — Bob deposits concert ticket into ERC721 vault

        Bob->>DVP: ERC721.mint + approve
        Bob->>Bob: RandomInField() → bobTicketSalt
        Bob->>Bob: bobTicketCmt = Poseidon4(pk_bob, bobTicketSalt, 1, 25)
        Bob->>DVP: erc721Vault.deposit([tokenId=25, bobTicketCmt])
        DVP-->>Bob: emit Commitment(vaultId=1, bobTicketCmt)
        Bob->>Bob: loadVaultMerkleTree() → bobMerkleProof
    end

    rect rgb(255, 240, 200)
        Note over Alice,Bob: Step 3 — Pre-compute cross-commitments (off-chain)

        Alice->>Alice: Encapsulate(bobViewKey) → saltB, ctI
        Alice->>Alice: CommitmentB = Poseidon4(pk_bob, saltBField, 5, 0)
        Note over Alice: CommitmentB = USDT note Bob will receive

        Alice->>Alice: GenerateRandomValue() → saltStar
        Alice->>Alice: C' = Poseidon4(pk_alice, saltStarField, 1, 25)
        Note over Alice: C' = ticket commitment Alice expects to receive

        Alice->>Alice: ctII = EncryptSwapPayload(saltB, tokenId=25, amount=1, saltStar)
    end

    rect rgb(240, 220, 255)
        Note over Alice,Gnark: Step 4 — Alice generates ERC20 JoinSplit proof

        Alice->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: StMessage     = C'
        Note over Gnark: WtSaltsOut[0] = saltBField  (pre-determined → produces CommitmentB)
        Note over Gnark: StMerkleRoots = aliceMerkleProof.Root

        Gnark-->>Alice: proof = [8 field elements]
        Gnark-->>Alice: stmt  = [C', tree0, tree1, root0, root1,
        Note over Alice:         null0, null1, CommitmentB, dummy]   (9 elements)

        Alice->>Alice: assert stmt[0] == C' ✓
        Alice->>Alice: assert stmt[7] == CommitmentB ✓
        Alice->>Relayer: send ProofReceipt (proof + stmt)
    end

    rect rgb(220, 255, 240)
        Note over Bob,Gnark: Step 5 — Bob scans swap event and generates ERC721 proof

        Bob->>Bob: ScanForZkDvpSwap(decapKey_bob, ctI, ctII)
        Bob->>Bob: Decapsulate(decapKey_bob, ctI) → saltB'
        Bob->>Bob: DecryptSwapPayload(saltB', ctII) → tokenId=25, amount=1, saltStar
        Bob->>Bob: assert Poseidon4(pk_bob, saltBField, 5, 0) == CommitmentB ✓
        Bob->>Bob: assert Poseidon4(pk_alice, saltStarField, 1, 25) == C' ✓

        Bob->>Gnark: POST /proof/ownershipERC721
        Note over Gnark: StMessage  = CommitmentB
        Note over Gnark: WtSaltOut  = saltStarField  (pre-determined → produces C')
        Note over Gnark: StMerkleRoots = bobMerkleProof.Root

        Gnark-->>Bob: proof = [8 field elements]
        Gnark-->>Bob: stmt  = [CommitmentB, treeNum, root, nullifier, C']   (5 elements)

        Bob->>Bob: assert stmt[0] == CommitmentB ✓
        Bob->>Bob: assert stmt[4] == C' ✓
        Bob->>Relayer: send ProofReceipt (proof + stmt)
    end

    rect rgb(255, 220, 220)
        Note over Relayer,DVP: Step 6 — Relayer submits atomic swap

        Relayer->>Relayer: build endpoints.ProofReceipt (Alice — ERC20 payment)
        Relayer->>Relayer: build endpoints.ProofReceipt (Bob — ERC721 delivery)

        Relayer->>DVP: endpoints.Swap(alicePaymentReceipt, bobDeliveryReceipt,\n  paymentVaultId=0, deliveryVaultId=1)

        Note over DVP: _settleOnGroupPair:
        Note over DVP: alicePaymentReceipt.stmt[0]=C' == bobDeliveryReceipt.stmt[4]=C' ✓
        Note over DVP: bobDeliveryReceipt.stmt[0]=CommitmentB == alicePaymentReceipt.stmt[7]=CommitmentB ✓
        Note over DVP: verifyProof(aliceReceipt) ✓
        Note over DVP: verifyProof(bobReceipt)   ✓
        Note over DVP: Erc20CoinVault: insertCommitments + nullify
        Note over DVP: Erc721CoinVault: insertCommitments + nullify

        DVP-->>Bob: emit Commitment(vaultId=0, CommitmentB)
        DVP-->>Alice: emit Commitment(vaultId=1, C')
        DVP-->>Alice: emit Nullifier (alice's USDT note spent)
        DVP-->>Bob: emit Nullifier (bob's ticket note spent)
        DVP-->>Relayer: emit Settled(C', CommitmentB)
        Relayer-->>Alice: relayerCmts[2] = C'
        Relayer-->>Bob: relayerCmts[0] = CommitmentB
    end

    rect rgb(235, 235, 235)
        Note over Alice,Bob: Step 7 — Verify received notes

        Bob->>Bob: recompute Poseidon4(pk_bob, swap.SaltBField, 5, 0) == CommitmentB ✓
        Note over Bob: Bob can spend CommitmentB (his USDT note)

        Alice->>Alice: recompute Poseidon4(pk_alice, saltStarField, 1, 25) == C' ✓
        Note over Alice: Alice can spend C' (her ERC721 ticket note)
    end
```

---

## Key references

| Symbol                         | File                                                              | Line |
| ------------------------------ | ----------------------------------------------------------------- | ---- |
| `Erc20JoinSplitProofFromSalts` | `src/core/prover_erc.go`                                         | 680  |
| `Erc721OwnershipProofFromSalt` | `src/core/prover_erc.go`                                         | —    |
| `ScanForZkDvpSwap`             | `src/core/scan.go`                                               | 237  |
| `EncryptSwapPayload`           | `src/core/utils.go`                                              | 264  |
| `Erc20CommitmentV2`            | `src/core/utils.go`                                              | 563  |
| `Erc721Commitment`             | `src/core/utils.go`                                              | —    |
| `Encapsulate` / `SaltBToField` | `src/core/utils.go`                                              | 216  |
| `endpoints.Swap`               | `src/core/endpoints/relayer.go`                                  | 183  |
| `endpoints.ProofReceipt`       | `src/core/endpoints/relayer.go`                                  | 61   |
| `swap`                         | `contracts/core/contracts/EnygmaDvp.sol`                         | 707  |
| `_settleOnGroupPair`           | `contracts/core/contracts/EnygmaDvp.sol`                         | 798  |
| Integration test               | `test/05_v2_zkdvp_two_phase_swap_relayer_test.go`                | —    |
| ZkDvP without relayer          | `docs/flows/06_swap_erc721_erc20.md`                             | —    |

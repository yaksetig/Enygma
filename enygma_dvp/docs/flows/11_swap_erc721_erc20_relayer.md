# Flow 11 — Atomic DVP Swap with Relayer: ERC721 ↔ ERC20

## Overview

Alice has an ERC721 NFT (e.g. tokenId=2347) and Bob has ERC20 tokens (e.g. 100).
They want to swap atomically — either both sides settle or neither does.

Compared to [Flow 09](./09_swap_erc1155nonfungible_erc20.md), the delivery asset is in
`Erc721CoinVault` (vaultId=1) and no asset group pre-registration is required.

Compared to [Flow 10](./10_zkdvp_two_phase_swap_relayer.md), the direction is reversed:
Bob pays ERC20, Alice delivers ERC721. Alice deposits the ERC20 tokens on Bob's behalf
via `depositV2` so Bob has a spendable note without needing `privateMint`.

Commitment formulae:

```
ERC721 note: Poseidon4(pk_spend, saltBField, amount=1, tokenId)
ERC20 note:  Poseidon4(pk_spend, saltBField, amount, tokenId=0)
```

---

## Atomicity

`_settleOnGroupPair` verifies cross-commitment consistency before settling:

```
bobPaymentReceipt.stmt[0]    == aliceDeliveryReceipt.stmt[4]   // bobNFTCmt == bobNFTCmt
aliceDeliveryReceipt.stmt[0] == bobPaymentReceipt.stmt[7]      // aliceERC20Cmt == aliceERC20Cmt
```

Mapping to this swap:

```
stMessage(Bob)   = bobNFTCmt      ← pre-computed by Bob, equals Alice's ERC721 output at stmt[4]
stMessage(Alice) = aliceERC20Cmt  ← pre-computed by Alice, equals Bob's ERC20 first output at stmt[7]
```

Any mismatch between the two receipts reverts the entire transaction.

---

## Statement layouts

**ERC20 payment receipt** (2-in / 2-out, non-interleaved, 9 elements):

```
[msg, tree0, tree1, root0, root1, null0, null1, cmt0, cmt1]
 [0]   [1]    [2]   [3]   [4]    [5]    [6]    [7]   [8]
                                                 ↑ aliceERC20Cmt at index 7
```

**ERC721 delivery receipt** (1-in / 1-out, 5 elements):

```
[msg, treeNum, merkleRoot, nullifier, cmt]
 [0]   [1]      [2]         [3]       [4]
                                       ↑ bobNFTCmt at index 4
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
| Alice        | Sells ERC721 NFT, wants ERC20 payment — also funds Bob's initial ERC20 note via depositV2 |
| Bob          | Buys NFT with ERC20 tokens                                                              |
| Gnark Server | Generates Alice's ERC721 Ownership proof and Bob's ERC20 JoinSplit proof                |
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
        Note over Alice,DVP: Step 1 — Alice deposits ERC721 NFT

        Alice->>DVP: ERC721.mint + approve
        Alice->>Alice: RandomInField() → aliceNFTSalt
        Alice->>Alice: aliceNFTCmt = Poseidon4(pk_alice, aliceNFTSalt, 1, tokenId)
        Alice->>DVP: erc721Vault.deposit([tokenId, aliceNFTCmt])
        DVP-->>Alice: emit Commitment(vaultId=1, aliceNFTCmt)
        Alice->>Alice: loadVaultMerkleTree() → aliceNFTProof
    end

    rect rgb(220, 255, 220)
        Note over Alice,DVP: Step 2 — Alice deposits ERC20 with Bob's commitment (depositV2)

        Alice->>DVP: ERC20.mint + approve
        Alice->>Alice: Encapsulate(bobViewKey) → bobDepositSaltB, capsule
        Alice->>Alice: bobInputCmt = Poseidon4(pk_bob, saltBField, 100, 0)
        Alice->>DVP: erc20Vault.depositV2([100, bobInputCmt], capsule, ctII)
        DVP-->>Bob: emit Commitment(vaultId=0, bobInputCmt)
        Alice->>Alice: loadVaultMerkleTree() → bobErc20MerkleProof
    end

    rect rgb(255, 240, 200)
        Note over Alice,Bob: Step 3 — Pre-compute cross-commitments (off-chain)

        Alice->>Alice: Encapsulate(bobViewKey) → saltBForNFT, ctINFT
        Alice->>Alice: bobNFTCmt = Poseidon4(pk_bob, saltBForNFT, 1, tokenId)
        Note over Alice: bobNFTCmt = Bob's future NFT note

        Alice->>Alice: Encapsulate(aliceViewKey) → saltBForPayment, ctIPayment
        Alice->>Alice: aliceERC20Cmt = Poseidon4(pk_alice, saltBForPayment, 100, 0)
        Note over Alice: aliceERC20Cmt = Alice's future ERC20 payment
    end

    rect rgb(240, 220, 255)
        Note over Alice,Gnark: Step 4 — Alice generates ERC721 Ownership proof

        Alice->>Gnark: POST /proof/ownershipERC721
        Note over Gnark: StMessage  = aliceERC20Cmt
        Note over Gnark: WtSaltOut  = saltBForNFT  (pre-determined → produces bobNFTCmt)
        Note over Gnark: StMerkleRoots = aliceNFTProof.Root

        Gnark-->>Alice: proof = [8 field elements]
        Gnark-->>Alice: stmt  = [aliceERC20Cmt, treeNum, root, nullifier, bobNFTCmt]   (5 elements)

        Alice->>Alice: assert stmt[4] == bobNFTCmt ✓
        Alice->>Relayer: send ProofReceipt (proof + stmt)
    end

    rect rgb(220, 255, 240)
        Note over Bob,Gnark: Step 5 — Bob generates ERC20 JoinSplit proof

        Bob->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: StMessage     = bobNFTCmt
        Note over Gnark: WtSaltsOut[0] = saltBForPayment  (pre-determined → produces aliceERC20Cmt)
        Note over Gnark: StMerkleRoots = bobErc20MerkleProof.Root

        Gnark-->>Bob: proof = [8 field elements]
        Gnark-->>Bob: stmt  = [bobNFTCmt, tree0, tree1, root0, root1,
        Note over Bob:         null0, null1, aliceERC20Cmt, dummy]   (9 elements)

        Bob->>Bob: assert stmt[7] == aliceERC20Cmt ✓
        Bob->>Bob: assert aliceResult.stmt[0] == bobResult.stmt[7] ✓
        Bob->>Bob: assert bobResult.stmt[0]   == aliceResult.stmt[4] ✓
        Bob->>Relayer: send ProofReceipt (proof + stmt)
    end

    rect rgb(255, 220, 220)
        Note over Relayer,DVP: Step 6 — Relayer submits atomic swap

        Relayer->>Relayer: build endpoints.ProofReceipt (Bob — ERC20 payment)
        Relayer->>Relayer: build endpoints.ProofReceipt (Alice — ERC721 delivery)

        Relayer->>DVP: endpoints.Swap(bobPaymentReceipt, aliceDeliveryReceipt,\n  paymentVaultId=0, deliveryVaultId=1)

        Note over DVP: _settleOnGroupPair:
        Note over DVP: bobPaymentReceipt.stmt[0]=bobNFTCmt == aliceDeliveryReceipt.stmt[4]=bobNFTCmt ✓
        Note over DVP: aliceDeliveryReceipt.stmt[0]=aliceERC20Cmt == bobPaymentReceipt.stmt[7]=aliceERC20Cmt ✓
        Note over DVP: verifyProof(bobReceipt)   ✓
        Note over DVP: verifyProof(aliceReceipt) ✓
        Note over DVP: Erc20CoinVault:  insertCommitments + nullify
        Note over DVP: Erc721CoinVault: insertCommitments + nullify

        DVP-->>Alice: emit Commitment(vaultId=0, aliceERC20Cmt)
        DVP-->>Bob: emit Commitment(vaultId=1, bobNFTCmt)
        DVP-->>Bob: emit Nullifier (bob's ERC20 note spent)
        DVP-->>Alice: emit Nullifier (alice's NFT note spent)
        DVP-->>Relayer: emit Settled(bobNFTCmt, aliceERC20Cmt)
        Relayer-->>Alice: relayerCmts[0] = aliceERC20Cmt
        Relayer-->>Bob: relayerCmts[2] = bobNFTCmt
    end

    rect rgb(235, 235, 235)
        Note over Alice,Bob: Step 7 — Scan for received notes

        Bob->>Bob: ScanForErc721Notes(bobDecapsKey, pk_bob, events)
        Note over Bob: Decapsulate(bobDecapsKey, ctINFT) → saltBForNFT
        Note over Bob: DecryptPayload(saltBForNFT, ctIINFT) → tokenId
        Note over Bob: Poseidon4(pk_bob, saltBForNFT, 1, tokenId) == bobNFTCmt ✓

        Alice->>Alice: ScanForErc20Notes(aliceDecapsKey, pk_alice, events)
        Note over Alice: Decapsulate(aliceDecapsKey, ctIPayment) → saltBForPayment
        Note over Alice: DecryptPayload(saltBForPayment, ctIIPayment) → tokenId=0, amount=100
        Note over Alice: Poseidon4(pk_alice, saltBForPayment, 100, 0) == aliceERC20Cmt ✓
    end
```

---

## Key references

| Symbol                         | File                                                              | Line |
| ------------------------------ | ----------------------------------------------------------------- | ---- |
| `Erc721OwnershipProofFromSalt` | `src/core/prover_erc.go`                                         | —    |
| `Erc20JoinSplitProofFromSalts` | `src/core/prover_erc.go`                                         | 680  |
| `Erc721Commitment`             | `src/core/utils.go`                                              | —    |
| `Erc20CommitmentV2`            | `src/core/utils.go`                                              | 563  |
| `ScanForErc721Notes`           | `src/core/scan.go`                                               | —    |
| `ScanForErc20Notes`            | `src/core/scan.go`                                               | 62   |
| `Encapsulate` / `SaltBToField` | `src/core/utils.go`                                              | 216  |
| `endpoints.Swap`               | `src/core/endpoints/relayer.go`                                  | 183  |
| `endpoints.ProofReceipt`       | `src/core/endpoints/relayer.go`                                  | 61   |
| `swap`                         | `contracts/core/contracts/EnygmaDvp.sol`                         | 707  |
| `_settleOnGroupPair`           | `contracts/core/contracts/EnygmaDvp.sol`                         | 798  |
| Integration test               | `test/08_v2_swap_erc721_erc20_relayer_test.go`                   | —    |
| Without relayer (reference)    | `test/08_v2_swap_erc721_erc20_onchain_test.go`                   | —    |

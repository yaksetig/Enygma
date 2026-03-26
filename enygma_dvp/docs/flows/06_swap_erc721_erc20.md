# Flow 06 — Atomic DVP Swap (ERC721 ↔ ERC20)

## Overview

The atomic DVP swap lets Alice sell an NFT to Bob in exchange for ERC20 tokens — without
any trusted intermediary and without either party having to trust the other to act first.

Both parties generate their proofs **independently and in any order**. The contract only
settles when it receives both proofs carrying the same `swapId`, enforcing atomicity:
either the full exchange happens, or nothing does.

The `swapId` is the only public linking value between the two proofs:

```
// temporary solution
swapId = Poseidon4(contractAddr721, tokenId, contractAddr20, paymentAmount)
```

Both parties compute this from the agreed swap terms. It is embedded as `stMessage` in
both proofs and verified on-chain.

---

## Non-interactivity

After a one-time public-key exchange, neither party waits for the other:

| Party | Needs from the other party                  | Can proceed without knowing       |
| ----- | ------------------------------------------- | --------------------------------- |
| Alice | `pk_bob` (spend), `encapKey_bob` (view)     | Bob's ERC20 proof or payment note |
| Bob   | `pk_alice` (spend), `encapKey_alice` (view) | Alice's ERC721 proof or NFT note  |

---

## Atomicity

```
swapId = Poseidon4(contractAddr721, tokenId, contractAddr20, paymentAmount)
```

Both proofs encode `swapId` as `StMessage`. The DVP contract verifies both proofs share
the same `swapId` before settling. A partial submission is rejected.

---

## Participants

| Participant  | Role                                                                   |
| ------------ | ---------------------------------------------------------------------- |
| Alice        | NFT seller — spends her ERC721 note, receives ERC20 payment from Bob   |
| Bob          | NFT buyer — spends his ERC20 note, receives the ERC721 note from Alice |
| Gnark Server | Generates both Groth16 proofs (ERC721 ownership + ERC20 JoinSplit)     |
| EnygmaDvp    | Verifies both proofs atomically and settles the swap                   |

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

        Alice->>Bob: pk_alice, encapKey_alice
        Bob->>Alice: pk_bob, encapKey_bob

        Alice->>Alice: swapId = Poseidon4(addr721, tokenId=9999, addr20, amount=50)
        Bob->>Bob: swapId = Poseidon4(addr721, tokenId=9999, addr20, amount=50)
        Note over Alice,Bob: swapId = 7384920165... (same, computed independently)
    end

    rect rgb(220, 255, 220)
        Note over Alice,Gnark: Step 2 — Alice's proof (ERC721 ownership)

        Alice->>Alice: Encapsulate(encapKey_bob)
        Note over Alice: saltB_bob = 0x4f2a...
        Note over Alice: ctI_bob   = 0x9d3e...
        Alice->>Alice: Erc721Commitment(tokenId=9999, pk_bob, saltBField_bob)
        Note over Alice: cmt_nft_bob = 3748291065...

        Alice->>Gnark: POST /proof/ownershipERC721
        Note over Gnark: stMessage = swapId = 7384920165...
        Note over Gnark: assert Poseidon4(pk_alice, saltIn, 1, 9999) == cmt_nft_alice
        Note over Gnark: assert MerkleProof(cmt_nft_alice, path) == root721
        Note over Gnark: assert Poseidon4(pk_bob, saltBField_bob, 1, 9999) == cmt_nft_bob

        Gnark-->>Alice: proof721 = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Alice: stmt721 = [swapId, tree0, root721, null_alice, cmt_nft_bob]
    end

    rect rgb(220, 255, 220)
        Note over Bob,Gnark: Step 3 — Bob's proof (ERC20 JoinSplit)

        Bob->>Bob: Encapsulate(encapKey_alice)
        Note over Bob: saltB_alice = 0x7c1d...
        Note over Bob: ctI_alice   = 0xa2f5...
        Bob->>Bob: Erc20CommitmentV2(pk_alice, saltBField_alice, amount=50, tokenId=0)
        Note over Bob: cmt_payment_alice = 9102837465...

        Bob->>Gnark: POST /proof/joinSplitERC20
        Note over Gnark: stMessage = swapId = 7384920165...
        Note over Gnark: assert Poseidon4(pk_bob, saltIn, 50, 0) == cmt_erc20_bob
        Note over Gnark: assert MerkleProof(cmt_erc20_bob, path) == root20
        Note over Gnark: assert Poseidon4(pk_alice, saltBField_alice, 50, 0) == cmt_payment_alice
        Note over Gnark: assert 50 == 50 + 0

        Gnark-->>Bob: proof20 = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Bob: stmt20 = [swapId, tree0, root20, null_bob, 0, 0, 0, cmt_payment_alice, cmt_dummy]
    end

    rect rgb(255, 240, 220)
        Note over Alice,DVP: Step 4 — Atomic settlement on-chain

        Alice->>DVP: settleSwap(receipt721, receipt20, [ctI_bob], [ctII_bob], [ctI_alice], [ctII_alice])
        Note over DVP: stmt721[0] == stmt20[0] == swapId ✓
        Note over DVP: IVerifier.verifyProof(VK_ERC721, proof721, stmt721) ✓
        Note over DVP: IVerifier.verifyProof(VK_ERC20, proof20, stmt20) ✓
        Note over DVP: isValidRoot(tree721, root721) ✓
        Note over DVP: isValidRoot(tree20, root20) ✓
        Note over DVP: setNullifier(tree721, null_alice)
        Note over DVP: setNullifier(tree20, null_bob)
        Note over DVP: insertLeaves([cmt_nft_bob])
        Note over DVP: insertLeaves([cmt_payment_alice, cmt_dummy])

        DVP-->>Bob: emit EncryptedNote(vault721, cmt_nft_bob, ctI_bob, ctII_bob)
        DVP-->>Alice: emit EncryptedNote(vault20, cmt_payment_alice, ctI_alice, ctII_alice)
        DVP-->>Alice: emit Nullifier(vault721, tree721, null_alice)
        DVP-->>Bob: emit Nullifier(vault20, tree20, null_bob)
    end

    rect rgb(240, 220, 255)
        Note over Alice,Bob: Step 5 — Scan for received notes

        Bob->>Bob: ScanForErc721Notes(decapKey_bob, pk_bob, events)
        Note over Bob: Decapsulate(decapKey_bob, ctI_bob) → saltB_bob
        Note over Bob: DecryptPayload(saltB_bob, ctII_bob) → contractAddr721, tokenId=9999
        Note over Bob: Erc721Commitment(9999, pk_bob, saltBField_bob) == cmt_nft_bob ✓

        Alice->>Alice: ScanForErc20Notes(decapKey_alice, pk_alice, events)
        Note over Alice: Decapsulate(decapKey_alice, ctI_alice) → saltB_alice
        Note over Alice: DecryptPayload(saltB_alice, ctII_alice) → tokenId=0, amount=50
        Note over Alice: Erc20CommitmentV2(pk_alice, saltBField_alice, 50, 0) == cmt_payment_alice ✓
    end
```

---

## Key references

| Symbol                 | File                                     | Line |
| ---------------------- | ---------------------------------------- | ---- |
| `Erc721OwnershipProof` | `src/core/prover_erc.go`                 | 512  |
| `Erc20JoinSplitProof`  | `src/core/prover_erc.go`                 | —    |
| `Erc721Commitment`     | `src/core/utils.go`                      | 577  |
| `Erc20CommitmentV2`    | `src/core/utils.go`                      | 563  |
| `GetNullifier`         | `src/core/utils.go`                      | —    |
| `Encapsulate`          | `src/core/utils.go`                      | 216  |
| `ScanForErc721Notes`   | `src/core/scan.go`                       | —    |
| `ScanForErc20Notes`    | `src/core/scan.go`                       | —    |
| `Erc721Circuit.Define` | `gnark_circuits/templates/ERC721.go`     | —    |
| `Erc20Circuit.Define`  | `gnark_circuits/templates/ERC20.go`      | —    |
| `settleSwap`           | `contracts/core/contracts/EnygmaDvp.sol` | —    |
| Integration test       | `test/06_v2_swap_erc721_erc20_test.go`   | —    |

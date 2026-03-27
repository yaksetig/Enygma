# Flow 07 — ERC1155 Fungible Transfer (erc1155FungibleJoinSplit)

## Overview

The ERC1155 fungible transfer lets Alice privately send tokens to Bob using the same
JoinSplit pattern as ERC20 (Flow 03), with two key differences:

1. **Asset group tree** — a second Merkle tree whose leaves are registered token type IDs.
   The circuit verifies the transferred token type is registered, preventing proofs over
   fake or unregistered token types.

2. **Commitment formula** — `contractAddress` is **not** embedded in the commitment.
   Instead it is validated through the asset group tree membership proof.

The same flow composes into a custody chain: Alice → Bob → Carol, each generating an
independent JoinSplit proof spending the previous output note.

---

## Key difference from ERC20 (Flow 03)

|                    | ERC20 JoinSplit (Flow 03)                | ERC1155 Fungible (Flow 07)                                                         |
| ------------------ | ---------------------------------------- | ---------------------------------------------------------------------------------- |
| Commitment formula | `Poseidon4(pk, salt, amount, tokenId=0)` | `Poseidon4(pk, salt, amount, tokenId)`                                             |
| Contract binding   | Not enforced in circuit                  | `uid = Poseidon2(Poseidon2(contractAddr, tokenId), 0)` checked in asset group tree |
| Asset group tree   | None                                     | Required — proves token type is registered                                         |
| Endpoint           | `/proof/joinSplitERC20`                  | `/proof/erc1155FungibleJoinSplit`                                                  |
| Statement length   | 9 elements                               | 9 elements (same layout)                                                           |

---

## Commitment and UID formulas

```
uid             = Poseidon2(Poseidon2(contractAddr, tokenId), 0)
commitment      = Poseidon4(pk_spend, saltBField, amount, tokenId)
nullifier       = Poseidon2(sk_spend, leafIndex)
```

---

## Circuit

**File:** `gnark_circuits/templates/ERC1155.go`

### Public inputs (statement)

| Index | Name                 | Value                                   |
| ----- | -------------------- | --------------------------------------- |
| 0     | `StMessage`          | Arbitrary (e.g. `1`)                    |
| 1     | `StTreeNumber[0]`    | Tree number for Alice's input note      |
| 2     | `StMerkleRoots[0]`   | Merkle root for Alice's note membership |
| 3     | `StNullifiers[0]`    | `Poseidon2(sk_alice, leafIndex)`        |
| 4     | `StTreeNumber[1]`    | `0` (dummy)                             |
| 5     | `StMerkleRoots[1]`   | `0` (dummy)                             |
| 6     | `StNullifiers[1]`    | `0` (dummy)                             |
| 7     | `StCommitmentOut[0]` | Bob's output commitment                 |
| 8     | `StCommitmentOut[1]` | Dummy zero-value commitment             |

### Private witnesses

| Name                       | Value                                                       |
| -------------------------- | ----------------------------------------------------------- |
| `WtPrivateKeysIn[0]`       | `sk_alice` — proves ownership of the input note             |
| `WtValuesIn[0]`            | Amount in Alice's note                                      |
| `WtSaltsIn[0]`             | `saltBField` from when Alice received the note              |
| `WtPathElements[0][j]`     | Merkle sibling hashes for Alice's leaf (token tree)         |
| `WtPathIndices[0]`         | Leaf index of Alice's note                                  |
| `WtErc1155ContractAddress` | ERC1155 contract address — used to check UID in asset group |
| `WtErc1155TokenId`         | Token type ID                                               |
| `WtPublicKeysOut[0]`       | `pk_bob` — spend public key of the recipient                |
| `WtSaltsOut[0]`            | `saltBField` derived from `Encapsulate(bob.viewEncapKey)`   |
| `WtValuesOut[0]`           | Amount for Bob                                              |
| Asset group path           | Merkle path proving `uid` is in the asset group tree        |

---

## Participants

| Participant  | Role                                                                    |
| ------------ | ----------------------------------------------------------------------- |
| Alice        | Sender — spends her ERC1155 note and creates Bob's output note          |
| Bob          | Recipient — scans `EncryptedNote` to discover the note addressed to him |
| Gnark Server | Generates the Groth16 ERC1155 JoinSplit proof                           |
| EnygmaDvp    | Verifies the proof, nullifies Alice's note, inserts Bob's commitment    |

---

## Diagram

```mermaid
sequenceDiagram
    participant Alice
    participant Gnark as Gnark Server
    participant DVP as EnygmaDvp
    participant Bob

    rect rgb(220, 235, 255)
        Note over Alice: Step 1 — Register token type + prepare notes off-chain

        Alice->>Alice: uid = Poseidon2(Poseidon2(contractAddr, tokenId=42), 0)
        Note over Alice: uid = 8273641092...
        Alice->>Alice: InsertLeaf(uid) into asset group tree
        Note over Alice: assetGroupRoot = 5647382910...

        Alice->>Alice: cmt_alice = Poseidon4(tokenId=42, amount=50, pk_alice, aliceSalt)
        Note over Alice: cmt_alice = Poseidon4(pk_alice, aliceSalt, 50, 42) = 4729183650...

        Alice->>Alice: Encapsulate(bob.viewEncapKey)
        Note over Alice: saltB_bob = 0x4f2a...c801
        Note over Alice: ctI_bob   = 0x9d3e...f420
        Alice->>Alice: SaltBToField(saltB_bob)
        Note over Alice: saltBField_bob = 7294810362...
        Alice->>Alice: EncryptPayload(saltB_bob, tokenId=42, amount=50)
        Note over Alice: ctII_bob = 0xb5c6...d7e8
        Alice->>Alice: cmt_bob = Poseidon4(tokenId=42, amount=50, pk_bob, saltBField_bob)
        Note over Alice: cmt_bob = Poseidon4(pk_bob, 7294..., 50, 42) = 3748291065...

        Alice->>Alice: GetNullifier(sk_alice, leafIndex=0)
        Note over Alice: nullifier = Poseidon2(sk_alice, 0) = 6182930475...
    end

    rect rgb(220, 255, 220)
        Note over Alice,Gnark: Step 2 — Generate ZK proof

        Alice->>Gnark: POST /proof/erc1155FungibleJoinSplit
        Note over Gnark: public: msg=1, tree0=0, root0=1829..., null0=6182..., tree1=0, root1=0, null1=0, cmt0=3748..., cmt1=dummy
        Note over Gnark: private: sk_alice, amount_in=50, salt_in=aliceSalt, path, contractAddr, tokenId=42, pk_bob, salt_out=7294..., assetGroupPath
        Note over Gnark: assert Poseidon4(pk_alice, aliceSalt, 50, 42) == cmt_alice
        Note over Gnark: assert MerkleProof(cmt_alice, path) == root0
        Note over Gnark: assert Poseidon2(sk_alice, 0) == 6182...
        Note over Gnark: assert MerkleProof(uid, assetGroupPath) == assetGroupRoot
        Note over Gnark: assert Poseidon4(pk_bob, 7294..., 50, 42) == cmt_bob
        Note over Gnark: assert 50 == 50 + 0

        Gnark-->>Alice: proof = [Ax,Ay,Bx1,Bx0,By1,By0,Cx,Cy]
        Gnark-->>Alice: statement = [1, 0, 1829..., 6182..., 0, 0, 0, 3748..., dummy]
    end

    rect rgb(255, 240, 220)
        Note over Alice,DVP: Step 3 — Submit on-chain

        Alice->>DVP: transferV2(receipt, [ctI_bob, ctI_dummy], [ctII_bob, ctII_dummy])
        Note over DVP: IVerifier.verifyProof(VK_ERC1155_FUNGIBLE, proof, statement)
        Note over DVP: isValidRoot(tree0, root0) ✓
        Note over DVP: isValidNullifier(tree0, null0) ✓
        Note over DVP: insertLeaves([3748..., dummy])
        Note over DVP: setNullifier(tree0, 6182...)

        DVP-->>Bob: emit EncryptedNote(vaultId, 3748..., ctI_bob, ctII_bob)
        DVP-->>Alice: emit Nullifier(vaultId, tree0, 6182...)
    end

    rect rgb(240, 220, 255)
        Note over Bob: Step 4 — Bob scans for his note

        Bob->>Bob: ScanForErc1155Notes(decapKey_bob, pk_bob, events)
        Note over Bob: Decapsulate(decapKey_bob, ctI_bob) → saltB_bob
        Note over Bob: DecryptPayload(saltB_bob, ctII_bob) → tokenId=42, amount=50
        Note over Bob: SaltBToField(saltB_bob) → saltBField = 7294810362...
        Note over Bob: Erc1155Commitment(42, 50, pk_bob, saltBField) == 3748... ✓
        Note over Bob: Bob can now spend this note (WtSaltsIn=7294..., WtValuesIn=50)
    end
```

---

## Key references

| Symbol                          | File                                                        | Line |
| ------------------------------- | ----------------------------------------------------------- | ---- |
| `Erc1155FungibleJoinSplitProof` | `src/core/prover_erc.go`                                    | 802  |
| `Erc1155Commitment`             | `src/core/utils.go`                                         | 596  |
| `Erc1155UniqueId`               | `src/core/utils.go`                                         | 582  |
| `GetNullifier`                  | `src/core/utils.go`                                         | —    |
| `Encapsulate`                   | `src/core/utils.go`                                         | 216  |
| `SaltBToField`                  | `src/core/utils.go`                                         | 239  |
| `EncryptPayload`                | `src/core/utils.go`                                         | 317  |
| `ScanForErc1155Notes`           | `src/core/scan.go`                                          | —    |
| `Erc1155Circuit.Define`         | `gnark_circuits/templates/ERC1155.go`                       | —    |
| `NewHandler` (erc1155Fungible)  | `gnark_circuits/server/circuits/erc1155Fungible/handler.go` | —    |
| `transferV2`                    | `contracts/core/contracts/vaults/Erc1155CoinVault.sol`      | —    |
| Integration test                | `test/07_v2_erc1155_fungible_test.go`                       | —    |

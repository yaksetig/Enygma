# zkDVP — Flow Documentation

This directory contains a detailed description of every flow supported by the Enygma DvP system,
including sequence diagrams, step-by-step function call traces with example values, and contract
references.

---

## Flows

| # | Flow | Asset | Circuit | Status |
|---|------|-------|---------|--------|
| [01](./flows/01_deposit.md) | Deposit | ERC20 | None | ✅ |
| [02](./flows/02_private_mint.md) | Private Mint | ERC20 | privateMint | ✅ |
| 03 | Transfer (JoinSplit) | ERC20 | joinSplitERC20 | 🔜 |
| 04 | Withdraw | ERC20 | joinSplitERC20 | 🔜 |
| 05 | Transfer | ERC721 | ownershipERC721 | 🔜 |
| 06 | Atomic DVP Swap | ERC721 ↔ ERC20 | ownershipERC721 + joinSplitERC20 | 🔜 |
| 07 | Transfer (JoinSplit) | ERC1155 Fungible | erc1155Fungible | 🔜 |
| 08 | Transfer | ERC1155 Non-Fungible | erc1155NonFungible | 🔜 |
| 09 | Transfer with Broker | ERC1155 Fungible | erc1155FungibleWithBroker | 🔜 |
| 10 | Transfer with Auditor | ERC1155 Fungible | erc1155FungibleAuditor | 🔜 |
| 11 | Transfer with Auditor | ERC1155 Non-Fungible | erc1155NonFungibleAuditor | 🔜 |
| 12 | Sealed-Bid Auction | ERC721 | auctionInit + auctionBid + ... | 🔜 |
| 13 | Broker Registration | — | brokerRegistration | 🔜 |

---

## Common Primitives

All flows share the same cryptographic building blocks:

| Primitive | Purpose | Implementation |
|---|---|---|
| **Poseidon hash** | Commitment and nullifier computation | `src/core/utils.go` |
| **ML-KEM-768** | Non-interactive note delivery (encapsulate / decapsulate) | `src/core/utils.go:216` |
| **ChaCha20-Poly1305** | Encrypt tokenId and amount inside `ctII` | `src/core/utils.go:317` |
| **Groth16** | ZK proof generation and verification | `gnark_circuits/server/circuits/` |
| **Incremental Merkle tree** | On-chain membership proofs (depth 8) | `contracts/core/` |

### Commitment formula (ERC20)

```
commitment = Poseidon4(pk_spend, saltBField, amount, tokenId)
```

### Nullifier formula

```
nullifier = Poseidon2(sk_spend, leafIndex)
```

A nullifier is unique per note and per owner. Publishing it on-chain marks the note as spent
without revealing which commitment it corresponds to.

---

## How to read these docs

Each flow document contains:

1. **Overview** — what the flow does and why
2. **Participants** — who is involved and in what role
3. **Diagram** — Mermaid sequence diagram with example values
4. **Step-by-step** — every function called, with file path, line number, and concrete inputs/outputs
5. **Contract references** — table of every Solidity function and event touched

# Invariant Map

> Enygma | 16 guards | 11 inferred properties | 7 not enforced on-chain

---

## 1. Enforced Guards (Reference)

#### G-1
`require(_groth16Verifier == address(0), "Verifier: already initialized")` · `enygma_dvp/contracts/core/contracts/Verifier.sol:61` · Prevents replacement of the pairing-verifier dependency after initialization.

#### G-2
`require(verificationKeyIndex_ < _vkLength, "Verifier: VK not registered")` · `enygma_dvp/contracts/core/contracts/Verifier.sol:110` · Prevents proof dispatch against an unregistered VK slot.

#### G-3
`require(_hashContractAddress == address(0), "Vault: already initialized")` · `enygma_dvp/contracts/core/contracts/vaults/AbstractCoinVault.sol:101` · Makes vault dependency setup a one-shot transition.

#### G-4
`require(lockedNullifiers[_treeNumber][_nullifierId] == false, "Merkle: Nullifier already locked.")` · `enygma_dvp/contracts/core/contracts/vaults/Merkle.sol:83` · Prevents concurrent reservation of the same note position.

#### G-5
`require(nullifiers[_treeNumber][_nullifierId] == false, "Merkle: Nullifier slot already set.")` · `enygma_dvp/contracts/core/contracts/vaults/Merkle.sol:121` · Makes a spent-note marker irreversible within a tree.

#### G-6
`if (!isValidRoot(...)) revert InvalidMerkleRoot()` · `enygma_dvp/contracts/core/contracts/vaults/Erc20CoinVault.sol:337` · Requires spends to reference an accepted historical root when root checking is enabled.

#### G-7
`if (isValidNullifier(...)) revert InvalidNullifier()` · `enygma_dvp/contracts/core/contracts/vaults/Erc20CoinVault.sol:346` · Rejects replay of a previously consumed note.

#### G-8
`if (_pendingProofReceipts[utxoUniqueId].statement.length != 0) revert ProofReceiptAlreadyAdded()` · `enygma_dvp/contracts/core/contracts/vaults/AbstractCoinVault.sol:274` · Prevents replacement of a stored pending receipt.

#### G-9
`require(expiry > block.timestamp, "SwapRelayer: expiry must be in the future")` · `enygma_dvp/contracts/core/contracts/SwapRelayer.sol:106` · Prevents immediately cancellable pending swaps.

#### G-10
`if (_status == STATUS_INITIALIZED) revert AlreadyInitialized()` · `enygma_payments/contracts/enygma/contracts/Enygma.sol:169` · Makes institutional-ledger initialization one shot.

#### G-11
`if (_nullifiers[nullifier]) revert NullifierAlreadyUsed()` · `enygma_payments/contracts/enygma/contracts/Enygma.sol:787` · Rejects reuse of an institutional proof nullifier.

#### G-12
`if (proofBlockNumber != lastBlockNum) revert InvalidBlockNumber()` · `enygma_payments/contracts/enygma/contracts/Enygma.sol:962` · Binds bridge proofs to the active balance snapshot.

#### G-13
`if (_auctions[auctionId].state != AuctionState.PENDING_SETTLEMENT) revert NoPendingSettlement()` · `enygma_dvp_auctions/contracts/core/contracts/EnygmaAuction.sol:515` · Restricts challenges to a live optimistic claim.

#### G-14
`if (block.timestamp >= claim.challengeDeadline) revert ChallengeWindowClosed()` · `enygma_dvp_auctions/contracts/core/contracts/EnygmaAuction.sol:517` · Bounds when a pending settlement can be challenged.

#### G-15
`require(payoutCommit == winnerBid.commitB, "EnygmaAuction: payout commitment mismatch")` · `enygma_dvp_auctions/contracts/core/contracts/EnygmaAuction.sol:790` · Binds seller payout to the winning bid's precommitted destination.

#### G-16
`if (_tagExists[tagKey]) revert TagAlreadyExists(...)` · `enygma_retail_payments/private_tags/contracts/TagRegistry.sol:59` · Prevents duplicate publication of a tag within a block.

## 2. Inferred Invariants (Single-Contract)

#### I-1

`Conservation` · On-chain: **No**

> Institutional total supply should equal the aggregate balance commitments after mint and burn.

**Derivation** — Δ-pair: `mintSupply` updates both supply and recipient balance at `Enygma.sol:238-265`, while `burn` updates only the account balance at `Enygma.sol:276-304`.

**If violated** — supply getters and the contract's aggregate `check()` disagree with active balances.

#### I-2

`Conservation` · On-chain: **No**

> Every registered institutional balance must survive an epoch rollover unless that account is a participant.

**Derivation** — guard/write-site analysis: IDs are documented 1-based at `Enygma.sol:910`, while `_updateBalancesForTransfer` copies `0..N-1` at `Enygma.sol:800-816` and `_updateBalances` copies participants only at `Enygma.sol:844-870`.

**If violated** — nonparticipant balances disappear from the active snapshot.

#### I-3

`StateMachine` · On-chain: **Yes**

> A DvP nullifier transitions from fresh to spent once per Merkle tree and has no reverse path.

**Derivation** — edge: `false@Merkle.sol:121 → true@Merkle.sol:126`.

**If violated** — the same private note could be spent repeatedly.

#### I-4

`StateMachine` · On-chain: **Yes**

> Vault initialization can set its hash dependency only once.

**Derivation** — edge: `address(0)@AbstractCoinVault.sol:101 → hashContractAddress@AbstractCoinVault.sol:104`.

**If violated** — proof/commitment semantics could change after deposits.

#### I-5

`Bound` · On-chain: **No**

> A registered ERC1155 token's total supply must not exceed its declared `maxTotalSupply`.

**Derivation** — guard-lift: `_mint` checks the cap only when fungibility is `NON_FUNGIBLE` at `RaylsERC1155.sol:242-251`; fungible write sites at `RaylsERC1155.sol:266-272` lack an equivalent bound.

**If violated** — a fungible token can exceed its published issuance cap.

#### I-6

`StateMachine` · On-chain: **No**

> Registered ERC1155 metadata should be immutable unless an explicit governed update path exists.

**Derivation** — write-site analysis: `registerNewToken` writes `_metadatas[newTokenId]` before checking its state at `RaylsERC1155.sol:204-213`.

**If violated** — token type, cap, decimals, and recorded supply can be replaced.

#### I-7

`StateMachine` · On-chain: **Yes**

> Standalone auctions transition through `INACTIVE → BIDDING → PENDING_SETTLEMENT → SETTLED`, with cancellation branches from `BIDDING`.

**Derivation** — edges: `EnygmaAuction.sol:201-228`, `:441-494`, `:515-555`, `:585-637`, `:759-769`.

**If violated** — locked NFT or bid notes could be settled or returned twice.

## 3. Inferred Invariants (Cross-Contract)

#### X-1

On-chain: **No**

> Vaults assume that every call to `IVerifier.verifyProof` establishes proof validity.

**Caller side** — `Erc20CoinVault.sol:364-406` — return values are discarded before state changes.

**Callee side** — `GenericGroth16Verifier.sol:120-131` and `Verifier.sol:100-116` — a well-formed pairing-invalid proof returns `false` without reverting.

**If violated** — attacker-selected nullifiers, outputs, and withdrawals proceed without a valid witness.

#### X-2

On-chain: **No**

> Caller-supplied institutional participant IDs should match the proof's public anonymity-set IDs.

**Caller side** — `Enygma.sol:396-437` — participant IDs drive account selection and state updates.

**Callee side** — `Enygma.sol:737-764` — public keys, balances, and deltas are compared, but `FP_K_INDEX_OFFSET` is not read.

**If violated** — aliasing or duplicate account IDs can make proof and state-update identities diverge.

#### X-3

On-chain: **No**

> ERC20 vault accounting assumes `transferFrom` and `transfer` move exactly the requested amount and succeed or revert.

**Caller side** — `Erc20CoinVault.sol:60-64`, `:227`, `:282` — return values and balance deltas are not checked.

**Callee side** — the vault admits an owner-configured external token at `AbstractCoinVault.sol:102-108`; token behavior is outside the vault's enforcement.

**If violated** — note value can diverge from custodial token balance.

#### X-4

On-chain: **No**

> A relayer-requested nullifier lock should correspond to an already validated receipt.

**Caller side** — `SwapRelayer.sol:74-112` forwards a caller-provided receipt directly to the lock API.

**Callee side** — `EnygmaDvp.sol:531-544` indexes and locks receipt nullifiers without calling the vault's receipt verifier.

**If violated** — fabricated receipts can reserve arbitrary nullifier slots.

## 4. Economic Invariants

#### E-1

On-chain: **No**

> Custodied vault assets can leave only when a valid proof authorizes the consumed note and recipient.

**Follows from** — `I-3` + `X-1` + `X-3`.

**If violated** — vault assets can be transferred without proof-backed ownership.

#### E-2

On-chain: **No**

> The institutional ledger's aggregate balances remain equal to declared supply across registration, mint, transfer, bridge, and burn.

**Follows from** — `I-1` + `I-2` + `X-2`.

**If violated** — commitments can represent lost or excess value relative to declared supply.

#### E-3

On-chain: **No**

> A pending swap cannot deny availability of a note unless its proof is valid.

**Follows from** — `I-3` + `X-4`.

**If violated** — the settlement layer can be used as a persistent note-locking primitive.

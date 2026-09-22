# Entry Point Map

> Enygma | 100+ state-changing entry points | permissionless, relayer-gated, auctioneer-gated, and owner-only surfaces

---

## Protocol Flow Paths

### DvP setup and settlement

`Verifier.initializeVerifier()` → `EnygmaDvp.initializeDvp()` → `registerAssetGroup()` → `registerVault()` → `registerSwapGroupPair()`

`vault.deposit()` → `submitPartialSettlement()` → counterparty proof → vault nullification + commitment insertion

`SwapRelayer.submitReceipt()` → `EnygmaDvp.lockReceiptNullifiers()` → second receipt → `EnygmaDvp.swap()`

### Institutional payment

`Enygma.initialize()` → `registerAccount()` → verifier registration → `mintSupply()`

`[setup above]` → `transfer()` / `transferWithFee()` → proof check → nullifier consumption → epoch balance update

`[setup above]` → `withdraw()` / `deposit()` → verifier delegatecall → DvP bridge call → epoch update

### Auction settlement

`AuctionCoinVault.grantAuctionRole()` → `initAuction()` → `submitBid()` → `submitBatch()` → `settleOptimistic()`

`[claim above]` → challenge proof verification or challenge-window expiry → vault settlement

`[auction above]` → settlement deadline passes → `recoverAuction()` → bidder `reclaimBid()`

## Permissionless

| Contract | Entry point | State/value effect | Guarding mechanism |
|---|---|---|---|
| `ChannelRegistry` | `openChannel` | Appends encrypted channel | Caller-scoped uniqueness |
| DvP `UserRegistry` | `register` | Publishes spend/view keys | Caller sentinel |
| `SwapRelayer` | `submitReceipt` | Stores receipt and requests nullifier lock | Slot/expiry checks |
| `SwapRelayer` | `cancelSwap` | Unlocks pending leg | Stored party + expiry |
| `EnygmaDvp` | `submitPartialSettlement` | Stores or settles proof receipt | Proof/root/nullifier/deadline checks |
| `EnygmaDvp` | `payment*` | Nullifies inputs and inserts outputs | Proof receipt checks |
| `EnygmaDvp` | `claimSwapTimeout` | Consumes pending input and inserts revert note | Initiator + deadline |
| `EnygmaDvp` | `checkAndRegisterChallenge` | Burns challenge namespace | None beyond freshness |
| ERC20 vault | `deposit*`, `transfer*`, `withdraw*`, `verifyOwnership` | Custodies/transfers ERC20 or updates notes | Approval and proof receipt checks |
| ERC721 vault | `deposit`, `transfer`, `withdraw`, `verifyOwnership` | Custodies/transfers NFT or updates notes | Ownership and proof receipt checks |
| ERC1155 vault | `deposit`, `transfer`, `withdraw`, `verifyOwnership` | Custodies/transfers balances or updates notes | Approval and proof receipt checks |
| `RaylsERC1155` | `registerNewToken` | Writes token metadata/type state | Parameter checks only |
| Legacy auction | `registerAuctioneer`, `newAuction`, `submitBid`, opening functions, `declareWinner` | Full auction lifecycle and vault settlement | Mixed proof/state checks |
| Standalone auction | `initAuction`, `submitBid`, `withdrawBid`, `challengeSettlement`, `finalizeSettlement`, `revertAuction`, `recoverAuction`, `reclaimBid` | Locks, settles, or returns private notes | Proof/state/time/preimage checks |
| Retail `UserRegistry` | `register` | Appends key bundle and receives fee | Caller + lengths + minimum fee |
| `TagChannelRegistry` | `openChannel` | Appends ciphertext channel | Length/bitmap checks |
| `TagRegistry` | `publishTag` | Appends block-indexed tag | Duplicate/rate-limit checks |

## Role-Gated

| Role | Contract | Entry points | State/value effect |
|---|---|---|---|
| Authorized relayer | `EnygmaDvp` | `lockReceiptNullifiers`, `unlockReceiptNullifiers`, `swap`, `swapOnGroupPair`, `exchange*` | Locks or settles shielded notes |
| DvP contract | `AbstractCoinVault` / `AssetGroup` | initialization, commitment, nullifier, lock, membership functions | Mutates note trees and spend state |
| Enygma bridge | `EnygmaErc20CoinVault` | `depositThroughEnygma`, `withdrawThroughEnygma` | Inserts or nullifies bridge notes |
| Auction contract | `AuctionCoinVault` | `lockCoin`, `unlockCoin`, `nullifyCoin`, `registerCoins` | Mutates auction note state |
| Auctioneer | Standalone `EnygmaAuction` | `submitBatch`, `settleOptimistic` | Records winners and pending settlement |
| Registered institution | Institutional `Enygma` | `transfer`, `transferWithFee`, `withdraw`, `deposit` | Updates confidential balances/bridge state |

## Admin-Only

| Subsystem | Representative functions | State modified |
|---|---|---|
| DvP | initialize, verifier/vault/group/auditor/relayer registration, private mint | Global routing and verification configuration |
| DvP verifier | `initializeVerifier`, `addVerificationKey` | Generic verifier and ordered VK registry |
| Token contracts | ERC20/ERC721/ERC1155 mint functions | Token supply and ownership |
| Institutional ledger | initialize, account/verifier/DvP registration, mint, burn | Accounts, proof dispatch, supply/balances |
| Standalone auction | auctioneer grants/revokes, challenge-window setter | Auction authority and timing |
| Retail registry | fee setter/withdrawal | Fee policy and accumulated ETH |

Inherited OpenZeppelin role management and token approval/transfer entry points remain part of the deployed access surface.

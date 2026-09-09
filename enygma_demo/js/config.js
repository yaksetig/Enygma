export const PROTOCOLS = {
  institutional: {
    name: "Institutional Payments", short: "Institutional", icon: "IP",
    description: "Private bilateral channels, controlled operations, and cross-network settlement for institutions.",
    contracts: ["EnygmaHub", "ChannelRegistry", "PolicyController", "BridgeGateway"],
    actions: ["channels", "payment", "freeze", "bridge"]
  },
  retail: {
    name: "Retail Payments", short: "Retail", icon: "RP",
    description: "Private consumer payments, recipient discovery, and transaction-scoped audit access.",
    contracts: ["RetailRegistry", "PrivatePayments", "CommitmentTree", "TokenVault"],
    actions: ["prepare-recipient", "payment", "scan", "traffic"]
  },
  dvp: {
    name: "Delivery versus Payment", short: "DvP", icon: "DvP",
    description: "Confidential issuance, locking, atomic asset settlement, timeout recovery, and unshielding.",
    contracts: ["EnygmaDvP", "AssetRegistry", "CashVault", "SecuritiesVault"],
    actions: ["mint-security", "shield-security", "mint-cash", "shield-cash", "propose-terms", "accept-terms", "instantiate-transfer", "lock-security", "timeout", "unshield"]
  },
  auctions: {
    name: "Sealed-bid Auctions", short: "Auctions", icon: "SA",
    description: "Auction-specific roles, private funded bids, highest-bid proofs, and atomic asset settlement.",
    contracts: ["SealedAuction", "AuctionVerifier", "BidVault", "SettlementVault"],
    actions: ["auctioneer", "register-auctioneer", "mint-nft", "list-asset", "mint-cash", "shield-cash", "submit-bid", "collect-bids", "close-bidding", "prove-winner", "challenge", "settle"]
  }
};

export const PROTOCOL_IDS = Object.keys(PROTOCOLS);

export const PARTY_NAMES = [
  "You", "Atlas Bank", "Boreal Markets", "Cedar Treasury", "Delta Custody",
  "Ember Capital", "Fjord Securities", "Grove Payments", "Helix Exchange", "Ion Finance"
];

export const SETUP_STEPS = [
  ["Deploy", "System operator"],
  ["Auditor key", "Auditor"],
  ["Configure", "System operator"],
  ["Identity", "Protocol participant"],
  ["Register", "Each participant"]
];

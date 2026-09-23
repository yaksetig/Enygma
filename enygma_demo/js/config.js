// Every deployment, in script order, including the asset contracts and repeated instances.
const contract = (name, description, instance = "", support = false, options = {}) => ({ name, description, instance, support, ...options });
const poseidonT3 = contract("PoseidonT3", "Hashes pairs of values to build the commitment trees used by private notes.", "", true);
const poseidonT5 = contract("PoseidonT5", "Hashes four values together to create a private note commitment from its owner, salt, token and amount.", "", true);
const poseidonWrapper = contract("PoseidonWrapper", "Gives the protocol a shared interface to its two-input and four-input hashing libraries.", "", true, { libraries: ["PoseidonT3", "PoseidonT5"] });
const groth16 = contract("GenericGroth16Verifier", "Performs the mathematical checks that establish whether a zero-knowledge proof is valid.", "", true);
const verifier = contract("Verifier", "Stores the verification key for each supported circuit and sends proofs to the shared proof checker.", "", true);
const privateMint = contract("PrivateMintVerifier", "Checks the zero-knowledge proof required to mint a private note without publishing its hidden details.", "", true);
const dvp = contract("EnygmaDvp", "Coordinates private payments and asset exchanges, verifies their proofs, and updates the registered vaults together.", "", false, { constructorArgs: ["PoseidonWrapper", "GenericGroth16Verifier"] });
const vaultOptions = { constructorArgs: ["EnygmaDvp"] };
const erc20Vault = contract("Erc20CoinVault", "Holds deposited ERC-20 tokens and tracks private note commitments and spent notes for transfers and withdrawals.", "", false, vaultOptions);
const erc20 = contract("RaylsERC20", "Fungible token deployed before its custody vault.", "", false, { asset: true, constructorArgs: ["TestERC20", "Rayls20"] });
const initialize = (target, method, args = [], repeat = 1) => ({ target, method, args, repeat });
const initializeVerifier = initialize("Verifier", "initializeVerifier", ["GenericGroth16Verifier"]);
const initializeDvp = initialize("EnygmaDvp", "initializeDvp", ["Verifier"]);
const registerMint = initialize("EnygmaDvp", "registerPrivateMintVerifier", ["PrivateMintVerifier"]);
const registerVault = (vault, asset, dimensions) => initialize("EnygmaDvp", "registerVault", [vault, asset, dimensions, 8]);

export function initializationSteps(protocolId) {
  return PROTOCOLS[protocolId].initialization.flatMap(step => Array.from({ length: step.repeat }, (_, index) => ({
    target: step.target,
    method: step.method,
    args: step.repeat > 1 ? [`${step.args[0]} ${index + 1}`] : step.args
  })));
}

// Primitive names and input order follow the protocol code, not the host chain's wallet keys.
// Spend keys/notes: enygma_dvp/src/core/utils.go; institutional keys:
// enygma_payments/go_client/cmd/register_bank/main.go and gnark-server/pkg/circuits/enygma/circuit.go.
const notePrimitives = {
  spend: "Poseidon(sk_spend)",
  view: "ML-KEM-768",
  commitment: "Poseidon(pk_spend, salt, amount, token_id)",
  derivation: "HKDF-SHA256",
  encryption: "AES-256-GCM",
  proof: "Groth16 over BN254",
  tree: "Poseidon(left, right)"
};
export const PROTOCOL_PRIMITIVES = {
  institutional: {
    spend: "Poseidon(sk_spend, sk_spend) mod ℓ",
    view: "ML-KEM-768",
    commitment: "Pedersen on BabyJubJub: C = v·G + r·H",
    derivation: "Poseidon",
    proof: "Groth16 over BN254"
  },
  retail: { ...notePrimitives, tag: "Poseidon(block_number, pk_spend, ss_field)" },
  dvp: { ...notePrimitives, swapEncryption: "ChaCha20-Poly1305" },
  auctions: { ...notePrimitives }
};

export function spendPublicKeyFor(identity, protocolId) {
  return protocolId === "institutional" ? identity?.institutionalSpendPublicKey : identity?.spendPublicKey;
}

export const PROTOCOLS = {
  institutional: {
    name: "Institutional Payments", short: "Institutional", icon: "IP",
    description: "Confidential payments through commitment batches, verified balance updates, and authorized audit access.",
    deploymentSource: "enygma_payments/run_scripts/deploy_node.js",
    initializationSource: "enygma_payments/demo/main.go",
    deploymentNote: "Deploy Enygma with its epoch interval, deploy its transfer verifier, then initialize Enygma and register the verifier.",
    contracts: [
      contract("Enygma", "Maintains institutional accounts, public keys and confidential balance commitments; applies proven transfers and operator controls.", "", false, { constructorArgs: [1] }),
      contract("Verifier", "Checks proofs for confidential institutional transfers. This is the verifier deployed from EnygmaVerifier.sol.", "EnygmaVerifier", true)
    ],
    initialization: [initialize("Enygma", "initialize"), initialize("Enygma", "addVerifier", ["EnygmaVerifier"])],
    actions: ["channels", "fund", "calculate", "prove", "post", "pause-contract", "resume-contract", "freeze-user", "unfreeze-user"]
  },
  retail: {
    name: "Retail Payments", short: "Retail", icon: "RP",
    description: "Private consumer payments, recipient discovery, and transaction-scoped audit access.",
    deploymentSource: "enygma_retail_payments/scripts/deploy.go",
    initializationSource: "enygma_retail_payments/scripts/init.go",
    deploymentNote: "Deploy the hashing libraries and verifiers, the payment coordinator, the ERC-20 asset and vault, then the participant and private-tag registries.",
    contracts: [
      poseidonT3, poseidonT5, poseidonWrapper, groth16, verifier, privateMint, dvp, erc20, erc20Vault,
      contract("UserRegistry", "Publishes participant spend and view public keys, plus encrypted key material for authorized auditing. Spend secrets remain private."),
      contract("TagChannelRegistry", "Publishes encrypted channel setup records and a recipient-candidate bitmap so wallets can discover their private channels."),
      contract("TagRegistry", "Publishes payment tags and encrypted payloads, indexed by block, so recipients can find and decrypt incoming messages.")
    ],
    initialization: [initializeVerifier, initializeDvp, ...["Payment", "Payment2in", "PaymentFee", "PaymentRelayerFeePublic"].map(name => initialize("Verifier", "addVerificationKey", [name])), registerMint, registerVault("Erc20CoinVault", "RaylsERC20", 1)],
    actions: ["mint-cash", "shield", "configure-tags", "payment", "scan"]
  },
  dvp: {
    name: "Delivery versus Payment", short: "DvP", icon: "DvP",
    description: "Confidential issuance, locking, atomic asset settlement, timeout recovery, and unshielding.",
    deploymentSource: "enygma_dvp/scripts/deploy.go",
    initializationSource: "enygma_dvp/scripts/init.go",
    deploymentNote: "Deploy the verifier suite and coordinator, three asset contracts, four vaults, and two separate asset-group instances. Initialization binds their verification keys, assets, and permitted group pairs.",
    contracts: [
      poseidonT3, groth16, verifier, privateMint, poseidonT5, poseidonWrapper, dvp,
      erc20,
      contract("RaylsERC721", "Non-fungible token deployed before its custody vault.", "", false, { asset: true, constructorArgs: ["TestERC721", "Rayls721"] }),
      contract("RaylsERC1155", "Multi-token asset contract deployed before its custody vault.", "", false, { asset: true, constructorArgs: ["Rayls1155"] }),
      erc20Vault,
      contract("Erc721CoinVault", "Holds deposited NFTs and tracks their private ownership notes for transfers and withdrawals.", "", false, vaultOptions),
      contract("Erc1155CoinVault", "Holds deposited ERC-1155 assets and tracks private notes for their token identifiers and quantities.", "", false, vaultOptions),
      contract("EnygmaErc20CoinVault", "Connects institutional Enygma balances to DvP private notes through the Enygma deposit and withdrawal integration.", "", false, vaultOptions),
      contract("AssetGroup", "Tracks membership of fungible assets and vaults so exchanges can operate on an approved asset group.", "FungibleAssetGroup", false, vaultOptions),
      contract("AssetGroup", "Tracks membership of non-fungible assets and vaults so exchanges can operate on an approved asset group.", "NonFungibleAssetGroup", false, vaultOptions)
    ],
    initialization: [
      initializeVerifier, initialize("Verifier", "grantRole", ["DEFAULT_OWNER_ROLE", "EnygmaDvp"]), initializeDvp,
      ...["Payment", "OwnershipErc721", "OwnershipErc1155NonFungible", "OwnershipErc1155Fungible", "JoinSplitErc1155", "BatchErc1155", "JoinSplitErc20_10_2", "AuctionInit", "AuctionBid", "AuctionNotWinningBid", "AuctionPrivateOpening", "BrokerRegistration", "LegitBroker", "JoinSplitErc20WithBrokerV1", "JoinSplitErc1155WithBrokerV1", "JoinSplitErc1155WithAuditor", "OwnershipErc1155NonFungibleWithAuditor", "BatchErc1155NonFungibleWithAuditor", "OwnershipErc721WithAuditor", "JoinSplitErc20WithAuditor", "JoinSplitErc20_10_2_WithAuditor", "AuctionInit_Auditor", "AuctionBid_Auditor", "DvPInitiator", "DvPDestination"].map(name => initialize("EnygmaDvp", "registerNewVerificationKey", [name])), registerMint,
      registerVault("Erc20CoinVault", "RaylsERC20", 1), registerVault("Erc721CoinVault", "RaylsERC721", 1),
      registerVault("Erc1155CoinVault", "RaylsERC1155", 2), registerVault("EnygmaErc20CoinVault", "RaylsERC20", 1),
      initialize("EnygmaDvp", "registerAssetGroup", ["FungibleAssetGroup", "Fungibles", true, 8]),
      initialize("EnygmaDvp", "registerAssetGroup", ["NonFungibleAssetGroup", "NonFungibles", false, 8]),
      initialize("EnygmaDvp", "registerExchangeGroupPair", [0, 0]), initialize("EnygmaDvp", "registerSwapGroupPair", [0, 1]),
      initialize("EnygmaDvp", "addVaultToGroup", [0, 0]), initialize("EnygmaDvp", "addVaultToGroup", [1, 1]),
      initialize("EnygmaDvp", "addVaultToGroup", [3, 0]), initialize("EnygmaErc20CoinVault", "addEnygma", ["Existing Enygma deployment"])
    ],
    actions: ["mint-security", "shield-security", "mint-cash", "shield-cash", "propose-terms", "accept-terms", "instantiate-transfer", "lock-security", "timeout", "unshield"]
  },
  auctions: {
    name: "Sealed-bid Auctions", short: "Auctions", icon: "SA",
    description: "Auction-specific roles, private funded bids, highest-bid proofs, and atomic asset settlement.",
    deploymentSource: "enygma_dvp_auctions/scripts/deploy.go",
    initializationSource: "enygma_dvp_auctions/scripts/init.go",
    deploymentNote: "Deploy the verifier suite, separate NFT and USDC note vaults, and the auction coordinator. Register six auction circuit keys and grant the coordinator its role on both vaults.",
    contracts: [
      poseidonT3, poseidonT5, poseidonWrapper, groth16, verifier,
      contract("AuctionCoinVault", "Tracks NFT note commitments, locks and spent notes for auctioned assets.", "NftVault", false, { constructorArgs: ["PoseidonWrapper", 8] }),
      contract("AuctionCoinVault", "Tracks USDC note commitments, locked bids and spent notes for payouts and bid recovery.", "UsdcVault", false, { constructorArgs: ["PoseidonWrapper", 8] }),
      contract("EnygmaAuction", "Manages sealed bids, deadlines, winner claims and challenges, then coordinates settlement or recovery through the two note vaults.", "", false, { constructorArgs: ["Verifier", "UsdcVault", "NftVault", 0, 1, 2, 3, 4, 5] })
    ],
    initialization: [initializeVerifier,
      ...["AuctionLock", "AuctionBid", "AuctionBatch", "AuctionFinal", "AuctionRevert", "AuctionWithdraw"].map(name => initialize("Verifier", "addVerificationKey", [name])),
      initialize("NftVault", "grantAuctionRole", ["EnygmaAuction"]), initialize("UsdcVault", "grantAuctionRole", ["EnygmaAuction"])
    ],
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

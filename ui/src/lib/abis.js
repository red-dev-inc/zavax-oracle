import networkConfig from '@network-config'

export const FEE_TOKEN_ABI = [
  'function name() view returns (string)',
  'function symbol() view returns (string)',
  'function decimals() view returns (uint8)',
  'function balanceOf(address) view returns (uint256)',
  'function allowance(address owner, address spender) view returns (uint256)',
  'function approve(address spender, uint256 amount) returns (bool)',
  'function transfer(address to, uint256 amount) returns (bool)',
  'event Approval(address indexed owner, address indexed spender, uint256 value)',
]

export const GATEWAY_ABI = [
  'function feeToken() view returns (address)',
  'function feeAmount() view returns (uint256)',
  'function requestHeight(uint256 height, bytes32 validationId) returns (bytes32 warpMessageID)',
  'event HeightRequested(address indexed sender, uint256 indexed height, bytes32 warpMessageID, bytes32 validationId, address payoutWallet)',
]

// Per-network values (chain ID, RPC, contract addresses, VM chain) live in
// config/<network>/client.json; vite.config.js aliases the selected one here.
export const DEFAULT_CONFIG = {
  ...networkConfig,
  queryMethod: 'zavax.getBlockByHeight',
  postMethod:  'zavax.postWarpMessage',
}

// The node list is no longer hardcoded here — see fetchValidators() in
// ./vm.js, which asks the server for the subnet's currently active P-Chain
// validators (server.cjs's /api/validators). Each entry has:
//   id           small-int key into server.cjs's NODE_URLS registry, used to
//                pick which node's RPC the UI *reads* block/height results from
//                (validators without their own entry in config/<network>/
//                server.json fall back to a default read node)
//   label        human label (falls back to the raw NodeID if unlabeled)
//   validationId the validator's real ACP-99 validationID (bytes32 hex, fresh
//                from P-Chain), passed to requestHeight() so the contract
//                resolves the payout wallet itself via
//                StakingManager.getValidatorRewardInfo(validationId)

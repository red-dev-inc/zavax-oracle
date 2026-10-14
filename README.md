# Redbridge Oracle

A Warp-based cross-chain oracle that bridges Zcash block data onto an Avalanche subnet. Zcash block heights are requested from Avalanche C-Chain, delivered to a custom Snowman VM through the Avalanche Interchain Messaging (ICM) protocol, and made available for on-chain reads.

**In plain words:** a user pays a small fee on Avalanche and asks "what's in Zcash block #N?". A group of validator servers — each running its own Zcash node — independently look up that block, agree on the answer, and record it on the `redbridge-oracle` Avalanche L1, where anyone can read it.

Clone the repository `git@gitlab-server.red.dev:red-bridge/redbridge-oracle.git`

## Start here

| I want to… | Read |
|---|---|
| **Run a validator node** (quickest, Docker, step by step) | [redbridge-oracle/EASY_DEPLOYMENT.md](redbridge-oracle/EASY_DEPLOYMENT.md) |
| Run a validator node with full control (build from source, no Docker, custom flags) | [redbridge-oracle/DEPLOYMENT.md](redbridge-oracle/DEPLOYMENT.md) |
| Look up Docker image settings (env vars, ports, existing node) | [redbridge-oracle/docker/README.md](redbridge-oracle/docker/README.md) |
| Develop / test the VM locally | [redbridge-oracle/README.md](redbridge-oracle/README.md) and [Quick Start](#quick-start-developers) below |
| Deploy or change the smart contracts | [contracts/README.md](contracts/README.md) |

## Project layout

| Folder | What's in it |
|---|---|
| [`redbridge-oracle/`](redbridge-oracle/README.md) | The custom Avalanche VM (Go) that bridges Zcash block data onto the Avalanche L1. See its [README.md](redbridge-oracle/README.md) to build/run it locally, and [DEPLOYMENT.md](redbridge-oracle/DEPLOYMENT.md) for server specs, manual install, and Docker deployment. |
| [`contracts/`](contracts/README.md) | Solidity contracts (Foundry) — fee token, message/height gateways that emit the warp messages the VM consumes. |
| [`icm-services/`](icm-services/README.md) | Fork of `ava-labs/icm-services` — the ICM relayer that delivers warp messages from C-Chain to the VM. |
| [`ui/`](ui/README.md) | React/Vite frontend for querying Zcash block data through the VM. |
| [`mock-zcashd/`](mock-zcashd/README.md) | Mock Zcash JSON-RPC server, used for local testing without a real `zcashd`/Zebra node. |
| [`security/`](security/) | Threat model, pen-test report, and deployment/network notes ([`deployment-notes.md`](security/deployment-notes.md) covers firewall and RPC-proxy config for the live fleet). |

## Application Flow

```
User (MetaMask / UI)
  │  1. Approve RBR fee  →  2. requestHeight(zcashHeight)
  ▼
ZcashHeightGateway.sol  (Fuji C-Chain)
  │  emits WarpMessenger.sendWarpMessage(abi.encode(height, sender))
  ▼
ICM Relayer  (icm-services fork)
  │  collects BLS signatures from subnet validators
  │  calls zavax.postWarpMessage(warpHex) on VM
  ▼
redbridge-oracle VM  (Snowman ChainVM, Go)
  │  verifies BLS sig → decodes height → fetches Zcash RPC → BuildBlock → Accept
  │  emits outbound warp message (wallet + zcashHeight) back to C-Chain
  ▼
VMWarpRelayer  (icm-services fork)
  │  collects BLS signatures, delivers to VMWarpReceiver.receiveWarpMessage()
  ▼
VMWarpReceiver.sol  (Fuji C-Chain)
  │  increments messageCount[wallet]
  ▼
User reads:  zavax.getBlockByHeight(height)  →  full Zcash block data
```

## Deployed Addresses (Fuji Testnet)

| Resource | Value | Used for |
|---|---|---|
| **Subnet (L1) ID** | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` | Tracking the L1 (`--track-subnets`) and **joining as a validator** |
| Blockchain ID | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` | VM RPC URL: `/ext/bc/<id>/rpc` |
| VM ID | `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` | Plugin binary filename |
| Docker image | `reddev2020/redbridge-oracle-node:v1.0.0-fuji` | Validator node |
| RBRToken (proxy) | `0x9E986B5Aec7F256A33D9172cedACe64b7FCEd4DC` | Fee token |
| ZcashHeightGateway (proxy) | `0xbe9B526e00a6a4172E88A3DcE083e2512ABf747c` | Request a Zcash height |
| VMWarpReceiver (proxy) | `0x0D05e998197487bE5a4104CB706dF92CCB41b14d` | Delivery receipts on C-Chain |
| RBRRewardCalculator (proxy) | `0x8D3Ec78dD8C478c98537E2E5f3206fDBB0Ba4925` | Validator rewards |

## Prerequisites

### Server (validator nodes)

| | Minimum | Recommended |
|---|---|---|
| **OS** | Ubuntu 22.04 LTS (x86_64) | **Ubuntu 26.04 LTS (x86_64) — latest LTS** |
| CPU | 8 vCPU | 16 vCPU |
| RAM | 16 GB | 32 GB |
| Storage | 1 TB NVMe SSD | 2 TB NVMe SSD |
| Network | 200 Mbps, static public IP | 1 Gbps, static public IP |

We recommend the **latest Ubuntu LTS release (26.04)** for any new server — it has the longest security-support window. Ubuntu 24.04 LTS is also supported. Development machines can be any recent Linux or macOS.

### Tools

| Tool | Version | Needed for |
|---|---|---|
| Docker + Compose plugin | any recent | Running a validator node the easy way |
| Go | 1.25.10 (see `redbridge-oracle/go.mod`) | Building the VM / relayer from source |
| AvalancheGo | v1.15.0 | Manual (non-Docker) node install, local test network |
| Foundry (`forge`, `cast`) | Latest | Contracts |
| Node.js | 18+ | UI, mock-zcashd |
| Zcash node (Zebra) | latest | **Every validator** — see below |

## Zcash Node (required for validators)

Every validator must have its **own synced Zcash Mainnet node with JSON-RPC enabled**. The VM re-checks every Zcash block against it before accepting a block; a node that can't reach Zcash starts normally but rejects every block and cannot validate.

| Option | Use it? |
|---|---|
| **Zebra on the same server** | ✅ Recommended — simplest and most reliable |
| **Zebra on another server you control** | ✅ Fine — keep its RPC port (8232) private |
| **zcashd** (legacy) | ⚠️ Works, but deprecated — prefer Zebra |
| **Third-party public Zcash RPC** | ❌ Not recommended — you'd trust their data instead of verifying it |
| **`mock-zcashd/`** (this repo) | ❌ Local development/testing only |

Zcash settings passed to the node:

| Setting | Required? | Default | Meaning |
|---|---|---|---|
| `ZCASH_RPC_ENDPOINT` | **Yes** | `http://127.0.0.1:8232` | Address of your Zcash node's RPC |
| `ZCASH_RPC_USER` | Optional | *(empty)* | Only if your Zcash RPC requires a username |
| `ZCASH_RPC_PASSWORD` | Optional | *(empty)* | Only if your Zcash RPC requires a password |
| `ZCASH_CONFIRMATION_DEPTH` | Optional | `24` | Blocks newer than this many from the Zcash tip are refused. Keep the default on all validators |

Setup details (Zebra config, Docker networking, checks): [redbridge-oracle/DEPLOYMENT.md §3.3](redbridge-oracle/DEPLOYMENT.md#33-zcash-node--required-with-options).

## Join as a Validator

1. Start a node and wait until it's fully synced — follow [EASY_DEPLOYMENT.md](redbridge-oracle/EASY_DEPLOYMENT.md) (Steps 1–4).
2. Get your node's `nodeID` and BLS key (`nodePOP`) with `info.getNodeID`.
3. Register it at <https://build.avax.network/console/add-validator/> on **Fuji** with **Subnet ID `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`**. The registration is signed with the subnet control key held by the red.dev team — external operators send their `nodeID` and `nodePOP` to the team.

Full instructions: [EASY_DEPLOYMENT.md → Step 6](redbridge-oracle/EASY_DEPLOYMENT.md#step-6--join-as-a-validator).

## Quick Start (developers)

### 1 — VM (redbridge-oracle)

```bash
cd redbridge-oracle

# Build the plugin binary → build/plugins/<VM_ID>
./scripts/build.sh

# Run all tests
go test ./vm/... ./zcash/... -v -race

# Start a local 5-validator ANR network (uses built-in mock Zcashd)
./scripts/run.sh

# Use a real Zcash node instead
ZCASH_RPC_ENDPOINT="http://user:pass@127.0.0.1:8232" ./scripts/run.sh
```

### 2 — Contracts

```bash
cd contracts
forge install      # pull git submodule dependencies (once)
forge build
forge test -vvv

# Deploy to Fuji (set PRIVATE_KEY first)
export PRIVATE_KEY=<key>
forge script script/DeployRBRToken.s.sol:DeployRBRToken \
  --rpc-url fuji --broadcast --verify
```

See [contracts/README.md](contracts/README.md) for the full deployment guide.

### 3 — ICM Relayer

```bash
cd icm-services
go build ./relayer/...

# Run with the project relayer config
./relayer/relayer --config-file ../redbridge-oracle/relayer-config.json
```

### 4 — UI

```bash
cd ui
npm install

# Development (two terminals)
npm run server   # Express proxy on port 3001
npm run dev      # Vite on port 5174

# Production
npm run build && npm run server
```

## Testing a Bridge Request (Fuji)

```bash
export PRIVATE_KEY=<key>
export RPC=https://api.avax-test.network/ext/bc/C/rpc
export GATEWAY=0xbe9B526e00a6a4172E88A3DcE083e2512ABf747c
export RBRTOKEN=0x9E986B5Aec7F256A33D9172cedACe64b7FCEd4DC

# 1. Approve RBR fee (1 RBR)
cast send $RBRTOKEN "approve(address,uint256)" $GATEWAY 1000000000000000000 \
  --private-key $PRIVATE_KEY --rpc-url $RPC

# 2. Request a Zcash block height
cast send $GATEWAY "requestHeight(uint256)" 2900000 \
  --private-key $PRIVATE_KEY --rpc-url $RPC

# 3. Poll until accepted (relayer delivers in ~30 s)
VM_RPC="http://208.83.233.9:9654/ext/bc/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/rpc"
curl -s -X POST "$VM_RPC" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":2900000}}'

# 4. Verify delivery on C-Chain
RECEIVER=0x0D05e998197487bE5a4104CB706dF92CCB41b14d
cast call $RECEIVER "messageCount(address)(uint256)" <your-wallet> --rpc-url $RPC
```

## Module Versions

| Module | Version |
|---|---|
| avalanchego | v1.15.0 |
| Go | 1.25.10 |
| Solidity | ^0.8.30 (Foundry) |

# Redbridge Oracle

Redbridge Oracle brings Zcash block data onto an Avalanche L1. A user pays a
small RBR fee on the Avalanche C-Chain and requests a Zcash block height. The
request reaches our custom Snowman VM through Avalanche Interchain Messaging
(ICM/Warp). Each validator looks up that block on its own Zcash node, and once
the validators agree, the block is stored on the `redbridge-oracle` L1, where
anyone can read it.

Clone the repo with `git@gitlab-server.red.dev:red-bridge/redbridge-oracle.git`.

## Where to start

- **Running a validator?** Start with
  [EASY_DEPLOYMENT.md](redbridge-oracle/EASY_DEPLOYMENT.md). It walks you
  through a Docker setup step by step. If you'd rather build from source, run
  without Docker or pass custom flags, see
  [DEPLOYMENT.md](redbridge-oracle/DEPLOYMENT.md). All the image settings are
  in [docker/README.md](redbridge-oracle/docker/README.md).
- **Working on the VM?** See [redbridge-oracle/README.md](redbridge-oracle/README.md)
  and the [development](#development) section below.
- **Working on the contracts?** See [contracts/README.md](contracts/README.md).

## Repository layout

- [`redbridge-oracle/`](redbridge-oracle/README.md): the Avalanche VM (Go). It
  also has the deployment guides and the Docker image.
- [`contracts/`](contracts/README.md): Solidity contracts (Foundry). These are
  the RBR fee token and the gateways that emit the Warp messages the VM
  consumes.
- [`icm-services/`](icm-services/README.md): our fork of `ava-labs/icm-services`,
  which provides the relayer that moves Warp messages between the C-Chain and
  the VM.
- [`ui/`](ui/README.md): a React/Vite frontend for querying bridged blocks.
- [`mock-zcashd/`](mock-zcashd/README.md): a fake Zcash JSON-RPC server for
  local testing.
- [`security/`](security/): the threat model, the pen-test report and network
  notes. [`deployment-notes.md`](security/deployment-notes.md) covers the
  firewall and RPC proxy setup for the live fleet.

## How a request flows

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

## Fuji deployment

| | |
|---|---|
| Subnet (L1) ID | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` |
| Blockchain ID | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` |
| VM ID | `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` |
| Docker image | `reddev2020/redbridge-oracle-node:v1.0.0-fuji` |
| RBRToken (proxy) | `0x9E986B5Aec7F256A33D9172cedACe64b7FCEd4DC` |
| ZcashHeightGateway (proxy) | `0xbe9B526e00a6a4172E88A3DcE083e2512ABf747c` |
| VMWarpReceiver (proxy) | `0x0D05e998197487bE5a4104CB706dF92CCB41b14d` |
| RBRRewardCalculator (proxy) | `0x8D3Ec78dD8C478c98537E2E5f3206fDBB0Ba4925` |

You need the subnet ID for `--track-subnets` and when you register as a
validator. The blockchain ID is part of the RPC URL
(`/ext/bc/<id>/rpc`), and the VM ID is used as the plugin's filename.

## Requirements

### Validator servers

The minimum is 8 vCPU, 16 GB RAM, a 1 TB NVMe SSD and 200 Mbps. We recommend
16 vCPU, 32 GB RAM, a 2 TB NVMe SSD and 1 Gbps. Either way, the server needs a
static public IP.

For new servers, use Ubuntu 26.04 LTS, since it has the longest support window.
24.04 and 22.04 also work. For development, any recent Linux or macOS is fine.

### Tools

- Docker with the Compose plugin, if you run a node with Docker
- Go 1.25.10 (see `redbridge-oracle/go.mod`) to build the VM or the relayer
- AvalancheGo v1.15.0 for a manual install or the local test network
- Foundry (`forge`, `cast`) for the contracts
- Node.js 18+ for the UI and mock-zcashd
- Zebra (latest). Every validator needs one, as explained in the next section

### Zcash node

Every validator needs its own synced Zcash Mainnet node with JSON-RPC enabled.
The VM checks each Zcash block against that node before it accepts a block. A
node without a working Zcash connection still starts, but it rejects every
block, so it can't validate.

We recommend running Zebra on the same server. Zebra on another machine you
control also works, as long as you keep port 8232 private. zcashd works too but
is deprecated. Don't point the node at a third-party public RPC, because then
you'd be trusting their data instead of checking it. `mock-zcashd/` is only for
local development.

The node reads these settings:

- `ZCASH_RPC_ENDPOINT` (required, default `http://127.0.0.1:8232`)
- `ZCASH_RPC_USER` / `ZCASH_RPC_PASSWORD`, if your RPC requires them
- `ZCASH_CONFIRMATION_DEPTH` (default `24`). Blocks closer than this to the
  Zcash tip are refused. Keep it the same on every validator

For the Zebra config, Docker networking and how to check the setup, see
[DEPLOYMENT.md §3.3](redbridge-oracle/DEPLOYMENT.md#33-zcash-node--required-with-options).

## Joining as a validator

1. Start a node and wait for it to sync. Steps 1–5 of
   [EASY_DEPLOYMENT.md](redbridge-oracle/EASY_DEPLOYMENT.md) cover this.
2. Get your `nodeID` and BLS key (`nodePOP`) from `info.getNodeID`.
3. Register at <https://build.avax.network/console/add-validator/> on Fuji
   with subnet ID `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`.

The full walkthrough, with screenshots, is in
[EASY_DEPLOYMENT.md, step 6](redbridge-oracle/EASY_DEPLOYMENT.md#6-join-as-a-validator).

## Development

### VM

```bash
cd redbridge-oracle

./scripts/build.sh                         # → build/plugins/<VM_ID>
go test ./vm/... ./zcash/... -v -race

# Local 5-validator network (uses the built-in mock Zcash server)
./scripts/run.sh

# ...or against a real Zcash node
ZCASH_RPC_ENDPOINT="http://user:pass@127.0.0.1:8232" ./scripts/run.sh
```

### Contracts

```bash
cd contracts
forge install      # first time only, pulls submodules
forge build
forge test -vvv

# Deploy to Fuji
export PRIVATE_KEY=<key>
forge script script/DeployRBRToken.s.sol:DeployRBRToken \
  --rpc-url fuji --broadcast --verify
```

For the full deployment process, see [contracts/README.md](contracts/README.md).

### Relayer

```bash
cd icm-services
go build ./relayer/...
./relayer/relayer --config-file ../redbridge-oracle/relayer-config.json
```

### UI

```bash
cd ui
npm install

# dev: run these in two terminals
npm run server   # Express proxy on :3001
npm run dev      # Vite on :5174

# production
npm run build && npm run server
```

## Testing a request on Fuji

```bash
export PRIVATE_KEY=<key>
export RPC=https://api.avax-test.network/ext/bc/C/rpc
export GATEWAY=0xbe9B526e00a6a4172E88A3DcE083e2512ABf747c
export RBRTOKEN=0x9E986B5Aec7F256A33D9172cedACe64b7FCEd4DC

# Approve the 1 RBR fee
cast send $RBRTOKEN "approve(address,uint256)" $GATEWAY 1000000000000000000 \
  --private-key $PRIVATE_KEY --rpc-url $RPC

# Request a Zcash height
cast send $GATEWAY "requestHeight(uint256)" 2900000 \
  --private-key $PRIVATE_KEY --rpc-url $RPC

# After ~30s the relayer should have delivered it
VM_RPC="http://208.83.233.9:9654/ext/bc/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/rpc"
curl -s -X POST "$VM_RPC" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":2900000}}'

# Check the delivery receipt on the C-Chain
RECEIVER=0x0D05e998197487bE5a4104CB706dF92CCB41b14d
cast call $RECEIVER "messageCount(address)(uint256)" <your-wallet> --rpc-url $RPC
```

## Versions

AvalancheGo v1.15.0, Go 1.25.10, Solidity ^0.8.30.

# Zcash Bridge VM

A production-grade custom Avalanche VM that bridges Zcash block data onto an Avalanche L1 using **Snowman (linear chain) consensus**. Compatible with **AvalancheGo v1.14.0**.

## Architecture


```
┌─────────────────────────────────────────────────────────┐
│                    AvalancheGo Node                     │
│                                                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │              Zcash Bridge VM (plugin)             │  │
│  │                                                   │  │
│  │  ┌──────────┐  ┌──────────┐  ┌────────────────┐   │  │
│  │  │  API     │  │ Mempool  │  │  State Manager │   │  │
│  │  │ (JSON-   │──│ (dedup   │──│  (prefixdb)    │   │  │
│  │  │  RPC)    │  │  queue)  │  │                │   │  │
│  │  └──────────┘  └──────────┘  └────────────────┘   │  │
│  │       │              │               │            │  │
│  │  ┌────▼──────────────▼───────────────▼─────────┐  │  │
│  │  │         VM Core (vm.go)                     │  │  │
│  │  │  Initialize / BuildBlock / Verify / Accept  │  │  │
│  │  └─────────────────┬──────────────────────────┘   │  │
│  │                    │                              │  │
│  │  ┌─────────────────▼──────────────────────────┐   │  │
│  │  │         Zcash RPC Client                   │   │  │
│  │  │  (retries, timeout, mockable interface)    │   │  │
│  │  └────────────────────────────────────────────┘   │  │
│  └───────────────────────────────────────────────────┘  │
│                          │                              │
│              Snowman Consensus Engine                   │
└──────────────────────────┬──────────────────────────────┘
                           │
                    ┌──────▼──────┐
                    │  Zcash Node │
                    │  (JSON-RPC) │
                    └─────────────┘
```

```
ZcashHeightGateway (C-Chain)
  │  warp message: abi.encode(height, sender)
  ▼
ICM Relayer  →  zavax.postWarpMessage(warpHex)
  ▼
VM  (BLS verify → decode height → fetch Zcash RPC → BuildBlock)
  ▼
Snowman Consensus  (5 validators)
  ▼
Accept():  persist block + emit outbound warp message
  ▼
VMWarpRelayer  →  VMWarpReceiver.receiveWarpMessage()  (C-Chain)
```

### Key Design Points

- **ICM-driven bridging**: heights enter the VM only through BLS-verified warp messages. Direct API calls cannot trigger bridging.
- **Warp self-signing**: messages from C-Chain are signed by this subnet's own validators (`vm.ctx.SubnetID`).
- **Deterministic blocks**: no `time.Now()` — timestamps come from the Zcash block, ensuring all validators produce byte-identical blocks.
- **Confirmation depth**: the VM rejects heights within 24 blocks of the Zcash tip (configurable).
- **Outbound persistence**: outbound warp messages are written to LevelDB before emitting so they survive restarts.

## Project Structure

```
redbridge-oracle/
├── plugin/
│   └── main.go           # AvalancheGo plugin entry point (rpcchainvm)
├── vm/
│   ├── vm.go             # VM lifecycle: Initialize, BuildBlock, consensus hooks
│   ├── block.go          # Block structure, serialization, Verify/Accept/Reject
│   ├── state.go          # LevelDB storage: blocks, height index, outbound messages
│   ├── mempool.go        # Thread-safe height queue with dedup
│   ├── api.go            # JSON-RPC handler (getBlockByHeight, postWarpMessage, …)
│   ├── warp_ingress.go   # BLS verification + AddressedCall unwrap + payload decode
│   ├── warp_payload.go   # ABI codec helpers (InboundPayload, OutboundPayload)
│   ├── config.go         # Config struct from vm-config.json
│   ├── metrics.go        # Prometheus counters and histograms
│   ├── vm_test.go        # Core VM unit tests
│   └── vm_warp_test.go   # Warp ingress / outbound message tests
└── zcash/
    ├── client.go         # Zcash JSON-RPC client with retries
    └── mock.go           # Mock client for unit tests
```

## Block Binary Format

```
Offset  Size   Field
0       1      version (0x01)
1       32     parentID
33      8      height (big-endian uint64)
41      8      timestamp (big-endian int64)
49      8      zcashHeight (big-endian uint64)
57      2      zcashHashLen (big-endian uint16)
59      N      zcashHash (UTF-8)
```

Block ID = `SHA-256(serialized_bytes)`

## Configuration

`vm-config.json` is passed as `configBytes` during VM initialization:

```json
{
  "zcashRpcEndpoint": "http://127.0.0.1:8232",
  "zcashRpcUser":     "zcashrpc",
  "zcashRpcPassword": "secret",
  "confirmationDepth": 24
}
```

| Field | Default | Description |
|---|---|---|
| `zcashRpcEndpoint` | `http://127.0.0.1:8232` | Zcash node JSON-RPC URL |
| `zcashRpcUser` | (empty) | RPC auth username |
| `zcashRpcPassword` | (empty) | RPC auth password |
| `confirmationDepth` | `24` | Min confirmations before a height is accepted |

## API

All methods are served at `/ext/bc/<BLOCKCHAIN_ID>/rpc`.

---

### `zavax.getBlockByHeight`

Read Zcash block data for an accepted height, or check its bridging status.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1000}}'
```

**Response — accepted:**
```json
{
  "result": {
    "timestamp": "1748293847",
    "data": { "hash": "000000...", "height": 1000, ... },
    "height": "42",
    "id": "2XkFB5qST5...",
    "parentID": "2Yq3mRs..."
  }
}
```

**Response — pending / not found:**
```json
{ "result": { "status": "pending" } }
{ "result": { "status": "not_found" } }
```

---

### `zavax.postWarpMessage`

Called by the ICM relayer to deliver a BLS-signed warp message from C-Chain.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.postWarpMessage","params":{"warpMessage":"0x..."}}'
```

**Response:**
```json
{ "result": { "status": "pending", "height": 1000 } }
```

Possible statuses: `pending`, `already_accepted`.

---

### `zavax.getBlock`

Fetch a block by its Avalanche block ID (omit `blockId` for the latest accepted block).

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{"blockId":"2XkFB5..."}}'
```

---

### `zavax.reconcileBlocks`

Compare every stored Zcash block hash against the live Zcash node and report mismatches.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.reconcileBlocks","params":{}}'
```

---

### `zavax.getOutboundWarpMessages`

Return pending outbound warp messages at or above a given Avalanche block height (used by the VMWarpRelayer cursor).

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getOutboundWarpMessages","params":{"fromHeight":0}}'
```

---

### `zavax.deleteOutboundWarpMessage`

Remove a delivered outbound message from the in-memory outbox and LevelDB (called by VMWarpRelayer after delivery).

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.deleteOutboundWarpMessage","params":{"messageId":"0x...","blockHeight":42}}'
```

---

### `/warp` — `warp_getMessageSignature`

BLS-sign an outbound warp message. Called by the VMWarpRelayer on each validator's `/warp` endpoint.

```bash
curl -s -X POST <NODE_URL>/ext/bc/<BLOCKCHAIN_ID>/warp \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"warp_getMessageSignature","params":{"messageID":"0x...","unsignedMessage":"0x..."}}'
```

## Building

```bash
# Build the VM plugin binary → build/plugins/<VM_ID>
./scripts/build.sh

# Run all tests
go test ./vm/... ./zcash/... -v -race

# Run a single test
go test ./vm/... -run TestBuildBlock -v
```

## Running a Local Network

`scripts/run.sh` bootstraps a **5-validator Avalanche network** via
[Avalanche Network Runner](https://github.com/ava-labs/avalanche-network-runner),
deploys the VM as a custom Avalanche L1 chain, and prints ready-to-use curl commands.

### Quick Start (Mock Zcash)

By default the script starts a built-in mock Zcash RPC server so no
external node is needed:

```bash
./scripts/run.sh

# Use a real Zcash node
ZCASH_RPC_ENDPOINT="http://user:pass@127.0.0.1:8232" ./scripts/run.sh
```

| Variable | Default | Description |
|----------|---------|-------------|
| `ZCASH_RPC_ENDPOINT` | *(empty — use mock)* | Zcash node JSON-RPC URL. When set, the mock server is skipped entirely. |
| `ZCASH_CONF_DEPTH` | `24` | Confirmation depth passed to the VM config. |
| `AVALANCHEGO_PATH` | `$GOPATH/bin/avalanchego` | Path to an existing AvalancheGo binary. If missing, the script builds v1.15.0 from source. |
| `ANR_PORT` | `8097` | ANR gRPC server port. |
| `MOCK_ZCASH_PORT` | `18232` | Port for the built-in mock Zcash server (only used when `ZCASH_RPC_ENDPOINT` is empty). |

### Example Curl Commands

Once the network is running the script prints the full RPC URL. You can
also use it directly:

```bash
# Bridge Zcash block height 1000
curl -s -X POST 'http://127.0.0.1:9650/ext/bc/<BLOCKCHAIN_ID>/rpc' \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1000}}' \
  | python3 -m json.tool

# Test too-recent height rejection (mock has 2000 blocks, depth=24)
curl -s -X POST 'http://127.0.0.1:9650/ext/bc/<BLOCKCHAIN_ID>/rpc' \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1990}}' \
  | python3 -m json.tool
```

> Replace `<BLOCKCHAIN_ID>` with the ID printed by the script, or query
> it from ANR: `avalanche-network-runner control status --endpoint=0.0.0.0:8097`

Press **Ctrl-C** to stop all nodes and clean up.

## Registering with AvalancheGo

> **Prerequisite:** every node running this VM needs its own synced Zcash node
> (Zebra) with JSON-RPC enabled, configured via `zcashRpcEndpoint`. `Block.Verify()`
> re-fetches each block's Zcash data from it, so without a working Zcash RPC the
> node rejects every block. For production deployment (Docker or manual) see
> [DEPLOYMENT.md](DEPLOYMENT.md) / [EASY_DEPLOYMENT.md](EASY_DEPLOYMENT.md).

1. Build the plugin binary.
2. Determine your VM ID (e.g., by hashing the VM name).
3. Add the binary path to AvalancheGo's VM alias config:

```json
{
  "YOUR_VM_ID": "/path/to/zcash-bridge-vm"
}
```

4. Create a Avalanche L1 and blockchain using the Avalanche Platform API.

## Prometheus Metrics

| Metric | Type | Description |
|---|---|---|
| `zcash_bridge_blocks_built_total` | Counter | Blocks built by BuildBlock |
| `zcash_bridge_blocks_accepted_total` | Counter | Blocks accepted via consensus |
| `zcash_bridge_blocks_rejected_total` | Counter | Blocks rejected via consensus |
| `zcash_bridge_zcash_fetch_duration_seconds` | Histogram | Zcash RPC fetch latency |
| `zcash_bridge_duplicate_requests_total` | Counter | Duplicate height requests |
| `zcash_bridge_api_requests_total` | Counter | API request counts (labels: method, status) |

## Test Coverage

- Block serialization round-trip
- Block ID determinism
- Genesis creation
- Full warp flow: postWarpMessage → BuildBlock → Accept → getBlockByHeight
- Outbound warp emission and persistence
- Duplicate height rejection
- 24-block finality rule enforcement
- Mempool dedup
- Concurrent API access
- Timestamp determinism (`max(parent+1, zcashTime)`)

## License

MIT

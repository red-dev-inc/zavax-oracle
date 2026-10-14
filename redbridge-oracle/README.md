# redbridge-oracle VM

This is the custom Avalanche VM behind Redbridge Oracle. It runs as an
AvalancheGo plugin, uses Snowman (linear chain) consensus and stores Zcash
block data on an Avalanche L1. It's built against AvalancheGo v1.15.0.

If you just want to run a validator, start with
[EASY_DEPLOYMENT.md](EASY_DEPLOYMENT.md). This README covers how the VM works
and how to develop it.

## How it works

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

A request goes through the system like this:

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

Some design decisions to be aware of:

- Heights can only enter the VM through a BLS-verified Warp message. You can't
  trigger a bridge by calling the API directly.
- Messages coming from the C-Chain are checked against the signatures of this
  subnet's own validators (`vm.ctx.SubnetID`).
- Block building never calls `time.Now()`. The timestamp comes from the Zcash
  block, so every validator builds byte-identical blocks.
- Heights within 24 blocks of the Zcash tip are rejected. The depth is
  configurable.
- Outbound Warp messages are written to LevelDB before they're emitted, so they
  survive a restart.

## Layout

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
│   ├── uptime_*.go       # Validator uptime tracking
│   ├── config.go         # Config struct from vm-config.json
│   ├── metrics.go        # Prometheus counters and histograms
│   └── *_test.go
└── zcash/
    ├── client.go         # Zcash JSON-RPC client with retries
    └── mock.go           # Mock client for unit tests
```

## Block format

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

The block ID is `SHA-256` of the serialized bytes.

## Configuration

The VM reads its config from `vm-config.json`, which AvalancheGo passes in as
`configBytes`:

```json
{
  "zcashRpcEndpoint": "http://127.0.0.1:8232",
  "zcashRpcUser":     "zcashrpc",
  "zcashRpcPassword": "secret",
  "confirmationDepth": 24
}
```

`zcashRpcEndpoint` defaults to `http://127.0.0.1:8232`. The user and password
are empty by default and are only needed if your Zcash RPC requires them.
`confirmationDepth` defaults to 24.

## API

Every method is served at `/ext/bc/<BLOCKCHAIN_ID>/rpc`, except
`warp_getMessageSignature`, which is served at `/warp`.

### zavax.getBlockByHeight

Returns the Zcash block data for an accepted height. If the height hasn't been
accepted, it returns the height's status instead.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1000}}'
```

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

If the height hasn't been bridged, you get `{"status": "pending"}` or
`{"status": "not_found"}` instead.

### zavax.postWarpMessage

The ICM relayer calls this method to deliver a signed Warp message from the
C-Chain. It returns `pending` or `already_accepted`.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.postWarpMessage","params":{"warpMessage":"0x..."}}'
# → { "result": { "status": "pending", "height": 1000 } }
```

### zavax.getBlock

Looks up a block by its Avalanche block ID. If you leave out `blockId`, it
returns the latest accepted block.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{"blockId":"2XkFB5..."}}'
```

### zavax.reconcileBlocks

Compares every stored Zcash hash with the live Zcash node and reports any
mismatches.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.reconcileBlocks","params":{}}'
```

### zavax.getOutboundWarpMessages / zavax.deleteOutboundWarpMessage

The VMWarpRelayer uses these two methods. `getOutboundWarpMessages` returns the
pending outbound messages at or above an Avalanche height.
`deleteOutboundWarpMessage` removes a message from memory and from LevelDB
once it has been delivered.

```bash
curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getOutboundWarpMessages","params":{"fromHeight":0}}'

curl -s -X POST <RPC_URL> \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.deleteOutboundWarpMessage","params":{"messageId":"0x...","blockHeight":42}}'
```

### warp_getMessageSignature

Signs an outbound Warp message with the node's BLS key. The VMWarpRelayer
calls this on every validator.

```bash
curl -s -X POST <NODE_URL>/ext/bc/<BLOCKCHAIN_ID>/warp \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"warp_getMessageSignature","params":{"messageID":"0x...","unsignedMessage":"0x..."}}'
```

## Building and testing

```bash
./scripts/build.sh                          # → build/plugins/<VM_ID>
go test ./vm/... ./zcash/... -v -race
go test ./vm/... -run TestBuildBlock -v     # single test
```

The tests cover block serialization and ID determinism, genesis, the full
flow (postWarpMessage → BuildBlock → Accept → getBlockByHeight), outbound
message persistence, duplicate heights, the 24-block depth rule, mempool dedup,
concurrent API access and timestamp determinism (`max(parent+1, zcashTime)`).

## Running a local network

`scripts/run.sh` uses
[Avalanche Network Runner](https://github.com/ava-labs/avalanche-network-runner)
to start a 5-validator network, deploys the VM as an L1 and prints the RPC URL.
By default it also starts a mock Zcash server, so you don't need a real Zcash
node.

```bash
./scripts/run.sh

# against a real Zcash node instead
ZCASH_RPC_ENDPOINT="http://user:pass@127.0.0.1:8232" ./scripts/run.sh
```

The script reads these environment variables:

- `ZCASH_RPC_ENDPOINT`: a real Zcash node. If it's set, the mock server is
  skipped.
- `ZCASH_CONF_DEPTH`: the confirmation depth (default `24`).
- `AVALANCHEGO_PATH`: the AvalancheGo binary (default `$GOPATH/bin/avalanchego`).
  If it's missing, the script builds v1.15.0 from source.
- `ANR_PORT`: the ANR gRPC port (default `8097`).
- `MOCK_ZCASH_PORT`: the mock Zcash port (default `18232`).

Once the network is up, you can try it out. The mock chain has 2000 blocks, so
height 1990 is within the 24-block depth and should be rejected:

```bash
curl -s -X POST 'http://127.0.0.1:9650/ext/bc/<BLOCKCHAIN_ID>/rpc' \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1000}}' \
  | python3 -m json.tool

curl -s -X POST 'http://127.0.0.1:9650/ext/bc/<BLOCKCHAIN_ID>/rpc' \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":1990}}' \
  | python3 -m json.tool
```

The script prints the blockchain ID. You can also get it with
`avalanche-network-runner control status --endpoint=0.0.0.0:8097`. Press Ctrl-C
to stop everything.

## Running it under AvalancheGo yourself

Every node that runs this VM needs its own synced Zcash node with RPC enabled.
`Block.Verify()` fetches the Zcash data again for every block, so without a
working Zcash connection the node rejects everything. For production setups,
see [DEPLOYMENT.md](DEPLOYMENT.md).

1. Build the plugin.
2. Work out the VM ID (for example, by hashing the VM name).
3. Add the binary to AvalancheGo's VM alias config:

   ```json
   {
     "YOUR_VM_ID": "/path/to/zcash-bridge-vm"
   }
   ```

4. Create the L1 and blockchain through the Platform API.

## Metrics

The VM exports these Prometheus metrics:

- `zcash_bridge_blocks_built_total`, `zcash_bridge_blocks_accepted_total` and
  `zcash_bridge_blocks_rejected_total` (counters)
- `zcash_bridge_zcash_fetch_duration_seconds` (histogram of Zcash RPC latency)
- `zcash_bridge_duplicate_requests_total` (counter)
- `zcash_bridge_api_requests_total` (counter, labeled by `method` and `status`)

## License

MIT

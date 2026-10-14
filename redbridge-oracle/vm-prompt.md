# Prompt: Build a Custom Avalanche Message VM

**Build a custom Avalanche HyperSDK-free VM (Snowman/linear chain) called `message-vm` in Go, targeting AvalancheGo v1.15.0, that lets users post and retrieve text messages by block height.**

## Functional Requirements

**API (JSON-RPC 2.0 over `/rpc`):**
- `msg.postMessage` — accepts `{"message": "hello world"}` (max 1024 bytes). Enqueues the message into a mempool, notifies the consensus engine via `PendingTxs`, and returns `{"status": "pending"}`. If the exact same message is already pending, return `{"status": "already_pending"}`.
- `msg.getMessage` — accepts `{"height": 5}`. If the block at that Avalanche chain height is already accepted, return `{"height": 5, "message": "hello world", "timestamp": 1713450000, "blockID": "..."}`. If height 0, return the genesis block (empty message). If the height doesn't exist yet, return an error.

**Consensus / Block Building:**
- Implement `block.ChainVM` (Snowman interface) — `Initialize`, `BuildBlock`, `ParseBlock`, `GetBlock`, `SetPreference`, `LastAccepted`, `Shutdown`, `CreateHandlers` / `NewHTTPHandler`, `WaitForEvent`, `HealthCheck`, `SetState`, `Version`.
- `BuildBlock` pops a message from the mempool, creates a block with fields: `parentID`, `height` (parent+1), `timestamp` (deterministic: `max(parentTimestamp+1, time.Now().Unix())`), and `message` (the text payload).
- Block bytes use **custom binary serialization** (version byte + fields), not JSON. Block ID = `sha256(bytes)`.
- `Verify` checks: parent exists, height = parent+1, timestamp ≥ parent timestamp, message is non-empty and ≤ 1024 bytes.
- `Accept` persists the block to state and updates `lastAccepted`/`preferred`.
- `Reject` cleans up.
- Genesis block is created in `Initialize` with height 0 and an empty message.

**State (using AvalancheGo's `database` + `prefixdb`):**
- `blockDB` — blockID → block bytes
- `heightDB` — height (8-byte big-endian) → blockID
- `metaDB` — stores `lastAccepted`
- In-memory block cache (`map[ids.ID]*Block`)
- Methods: `GetBlock`, `PutBlock`, `CacheBlock`, `GetBlockByHeight`, `GetLastAccepted`, `SetLastAccepted`

**Mempool:**
- Thread-safe FIFO queue with deduplication (seen set based on message hash).
- Methods: `Push`, `Pop`, `Has`, `Remove`, `Requeue`, `Len`

**Metrics (Prometheus):**
- Counters: `blocks_built`, `blocks_accepted`, `blocks_rejected`, `duplicate_requests`
- CounterVec: `api_requests` (labels: method, status)

**Config:**
```go
type Config struct {
    MaxMessageSize int `json:"maxMessageSize"` // default 1024
}
```

## Project Structure
```
message-vm/
├── go.mod
├── plugin/main.go              # rpcchainvm.Serve entry point
├── vm/
│   ├── vm.go                   # VM struct, Initialize, BuildBlock, consensus methods
│   ├── block.go                # Block struct, Verify, Accept, Reject, marshal/unmarshal
│   ├── state.go                # Database layer with prefixdb
│   ├── mempool.go              # FIFO queue with dedup
│   ├── api.go                  # JSON-RPC handler (msg.postMessage, msg.getMessage)
│   ├── config.go               # Config struct with defaults
│   ├── metrics.go              # Prometheus metrics
│   └── vm_test.go              # Unit tests (see below)
├── build/
│   ├── genesis.json            # {} (empty, genesis handled in code)
│   └── vm-config.json          # {"maxMessageSize": 1024}
├── scripts/
│   ├── build.sh                # go build -o plugin binary with VM ID
│   ├── run.sh                  # Full local orchestration: build plugin → ensure AvalancheGo v1.15.0 → ensure ANR v1.8.3 → start 5-node network → print curl examples → wait
│   └── run_e2e.sh              # Same as run.sh but runs e2e tests then tears down
└── tests/
    └── e2e/
        └── e2e_test.go         # Build-tagged e2e tests against live ANR network
```

## Unit Tests (vm/vm_test.go)
Use `newTestVM(t)` helper with in-memory database. Cover:
1. `TestBlockMarshalRoundTrip` — serialize/deserialize identity
2. `TestBlockIDDeterminism` — same input → same block ID
3. `TestGenesisCreation` — fresh VM has height-0 genesis block
4. `TestBuildVerifyAccept` — post message → BuildBlock → Verify → Accept → getMessage returns it
5. `TestDuplicateMessageDedup` — same message returns `already_pending`
6. `TestGetMessageAccepted` — accepted block returned by height
7. `TestGetMessageNotFound` — non-existent height returns error
8. `TestMempoolDedup` — push same message twice, only one in queue
9. `TestMempoolPopOrder` — FIFO ordering
10. `TestParseBlockRoundTrip` — BuildBlock bytes → ParseBlock → identical block
11. `TestConcurrentAPI` — 20 goroutines posting messages simultaneously
12. `TestMultipleBlocksSequential` — chain of 3 blocks, verify heights
13. `TestBlockReject` — rejected block has correct status
14. `TestVerifyRejectsEmptyMessage` — empty message fails Verify
15. `TestVerifyRejectsOversizedMessage` — message > maxSize fails Verify

## E2E Tests (tests/e2e/e2e_test.go)
Build tag `//go:build e2e`. Discover endpoint from `BLOCKCHAIN_ENDPOINT` env var or ANR status. Tests:
1. `PostAndRetrieveMessage` — post a message, poll until accepted, retrieve by height
2. `DuplicatePost` — post same message twice, second returns `already_pending`
3. `MultipleMessages` — post 3 messages, wait for all to be accepted at sequential heights
4. `GetGenesisBlock` — height 0 returns genesis
5. `GetNonExistentHeight` — returns error

## Scripts Details

**build.sh:** Compute VM ID from "message-vm" using `cb58` encoding. Build `go build -o build/plugins/$VM_ID ./plugin`.

**run.sh:** (~300 lines)
1. Run `build.sh`
2. Check for AvalancheGo v1.15.0 at `$GOPATH/src/github.com/ava-labs/avalanchego/build/avalanchego`, build from source if missing
3. Check for ANR (avalanche-network-runner) v1.8.3, download if missing
4. Start ANR gRPC server on `:12352`
5. Launch 5-node network with custom blockchain using the plugin, genesis.json, and vm-config.json
6. Wait for network health
7. Discover blockchain endpoint from ANR status
8. Print example curl commands for `msg.postMessage` and `msg.getMessage`
9. Trap SIGINT to stop network and clean up

**run_e2e.sh:** (~150 lines) Same setup as run.sh, but exports `BLOCKCHAIN_ENDPOINT` and runs `go test -tags e2e ./tests/e2e/ -v`, then tears down.

## Key Implementation Patterns
- **No external dependencies** beyond AvalancheGo and Prometheus client
- **`WaitForEvent` pattern** (v1.15.0): use `pendingMsgs chan common.Message` for engine notifications, return messages from `WaitForEvent()`
- **Deterministic timestamps**: `max(parentTimestamp+1, time.Now().Unix())` — ensures monotonic increase
- **Mempool watchdog goroutine**: every 10s, if mempool non-empty, re-send `PendingTxs` notification as safety net
- **All networking callbacks** (`Connected`, `Disconnected`, `AppRequest`, etc.) are no-ops
- **Compile-time interface check**: `var _ block.ChainVM = (*VM)(nil)`
- **Custom binary block format**, not JSON or protobuf
- Use `rpcchainvm.Serve()` in plugin/main.go
- Use `prefixdb.New()` for database partitioning

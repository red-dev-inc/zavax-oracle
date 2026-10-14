#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────
# run.sh – Bootstrap 5 Avalanche validator nodes, deploy the Zcash
#           Bridge VM, and print the blockchain endpoint for curl testing.
#
# The script keeps running after deploy so you can send requests.
# Press Ctrl-C to tear everything down.
#
# Usage:
#   ./scripts/run.sh
#
# Environment overrides:
#   AVALANCHEGO_PATH   – path to an existing avalanchego binary
#   ANR_PORT           – ANR gRPC port          (default 8097)
#   MOCK_ZCASH_PORT    – mock Zcash RPC port    (default 18232)
#   ZCASH_RPC_ENDPOINT – real Zcash node URL     (e.g. http://127.0.0.1:8232)
#                        When set, the mock server is skipped and
#                        the VM connects to a live Zcash node.
#   ZCASH_CONF_DEPTH   – confirmation depth      (default 24)
# ──────────────────────────────────────────────────────────────────────
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Configuration ────────────────────────────────────────────────────
AVALANCHEGO_VERSION="v1.15.0"
ANR_VERSION="v1.8.3"
GOPATH="${GOPATH:-$HOME/go}"
BIN_DIR="$GOPATH/bin"
AVALANCHEGO_PATH="${AVALANCHEGO_PATH:-$BIN_DIR/avalanchego}"
ANR_PORT="${ANR_PORT:-8097}"
ANR_GW_PORT="$((ANR_PORT + 1))"
MOCK_ZCASH_PORT="${MOCK_ZCASH_PORT:-18232}"
ZCASH_RPC_ENDPOINT="${ZCASH_RPC_ENDPOINT:-}"       # empty = use mock
ZCASH_CONF_DEPTH="${ZCASH_CONF_DEPTH:-24}"
VM_NAME="redbridge"
# VM ID = CB58(zeroPad32("redbridge")) — how ANR computes it
VM_ID="sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT"
PLUGIN_DIR="$PROJECT_ROOT/build/plugins"

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

log()  { echo -e "${GREEN}[run]${NC} $*"; }
info() { echo -e "${CYAN}[run]${NC} $*"; }
warn() { echo -e "${YELLOW}[run]${NC} $*"; }
err()  { echo -e "${RED}[run]${NC} $*" >&2; }

# ── Cleanup on exit ─────────────────────────────────────────────────
PIDS_TO_KILL=()
cleanup() {
    echo ""
    log "Shutting down..."
    # Stop the ANR network gracefully
    if command -v avalanche-network-runner &>/dev/null; then
        avalanche-network-runner control stop \
            --endpoint="0.0.0.0:${ANR_PORT}" 2>/dev/null || true
    fi
    for pid in "${PIDS_TO_KILL[@]}"; do
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    done
    log "Done. Goodbye!"
}
trap cleanup EXIT INT TERM

# ── Step 1: Build the VM plugin ─────────────────────────────────────
log "Building ${VM_NAME} plugin..."
rm -rf "$PLUGIN_DIR"
mkdir -p "$PLUGIN_DIR"
cd "$PROJECT_ROOT"
go build -o "$PLUGIN_DIR/${VM_ID}" ./plugin
log "Plugin built → $PLUGIN_DIR/${VM_ID}"

# ── Step 2: Zcash RPC – mock or real ─────────────────────────────────
if [[ -z "$ZCASH_RPC_ENDPOINT" ]]; then
    # No real endpoint supplied → start the built-in mock server
    log "Starting mock Zcash RPC server on :${MOCK_ZCASH_PORT}..."
    go run "$PROJECT_ROOT/tests/e2e/cmd/mock_zcash/main.go" &
    MOCK_ZCASH_PID=$!
    PIDS_TO_KILL+=("$MOCK_ZCASH_PID")
    sleep 1

    if ! kill -0 "$MOCK_ZCASH_PID" 2>/dev/null; then
        err "Mock Zcash server failed to start"
        exit 1
    fi
    ZCASH_RPC_ENDPOINT="http://127.0.0.1:${MOCK_ZCASH_PORT}"
    log "Mock Zcash RPC running (PID=$MOCK_ZCASH_PID) → ${ZCASH_RPC_ENDPOINT}"
else
    log "Using real Zcash node at ${ZCASH_RPC_ENDPOINT}"
    # Verify connectivity (getblockcount is the cheapest RPC)
    if ! curl -sf --max-time 5 -X POST "$ZCASH_RPC_ENDPOINT" \
        -H 'content-type:application/json' \
        -d '{"jsonrpc":"1.0","id":"probe","method":"getblockcount","params":[]}' \
        >/dev/null 2>&1; then
        warn "Could not reach Zcash node at ${ZCASH_RPC_ENDPOINT} – continuing anyway"
    else
        log "Zcash node reachable ✓"
    fi
fi

# ── Step 3: Ensure AvalancheGo binary exists ────────────────────────
if [[ ! -x "$AVALANCHEGO_PATH" ]]; then
    log "AvalancheGo not found – building ${AVALANCHEGO_VERSION} from source..."
    AVAGO_SRC="${TMPDIR:-/tmp}/avalanchego-src"
    rm -rf "$AVAGO_SRC"
    git clone --depth 1 --branch "${AVALANCHEGO_VERSION}" \
        https://github.com/ava-labs/avalanchego.git "$AVAGO_SRC"
    pushd "$AVAGO_SRC" >/dev/null
    ./scripts/build.sh
    popd >/dev/null
    mkdir -p "$BIN_DIR"
    cp "$AVAGO_SRC/build/avalanchego" "$BIN_DIR/avalanchego"
    AVALANCHEGO_PATH="$BIN_DIR/avalanchego"
    log "AvalancheGo built and installed to $AVALANCHEGO_PATH"
fi
log "AvalancheGo → $AVALANCHEGO_PATH"

# ── Step 4: Ensure ANR binary exists ────────────────────────────────
ANR_BIN="$HOME/bin/avalanche-network-runner"
if [[ ! -x "$ANR_BIN" ]]; then
    log "ANR not found – downloading ${ANR_VERSION} pre-built binary..."
    mkdir -p "$HOME/bin"
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
    esac
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ANR_URL="https://github.com/ava-labs/avalanche-network-runner/releases/download/${ANR_VERSION}/avalanche-network-runner_${ANR_VERSION#v}_${OS}_${ARCH}.tar.gz"
    log "Downloading from $ANR_URL"
    curl -sSfL "$ANR_URL" | tar xz -C "$HOME/bin" avalanche-network-runner
    chmod +x "$ANR_BIN"
    log "ANR installed to $ANR_BIN"
fi
export PATH="$HOME/bin:$PATH"
log "ANR → $ANR_BIN"

# ── Step 5: Start ANR gRPC server ───────────────────────────────────
log "Starting ANR gRPC server on :${ANR_PORT}..."
export AVALANCHEGO_EXEC_PATH="$AVALANCHEGO_PATH"
export AVALANCHEGO_PLUGIN_PATH="$PLUGIN_DIR"
"$ANR_BIN" server \
    --log-level warn \
    --port=":${ANR_PORT}" \
    --grpc-gateway-port=":${ANR_GW_PORT}" &
ANR_PID=$!
PIDS_TO_KILL+=("$ANR_PID")
sleep 2

if ! kill -0 "$ANR_PID" 2>/dev/null; then
    err "ANR server failed to start"
    exit 1
fi
log "ANR server running (PID=$ANR_PID)"

# ── Step 6: Launch 5-node network with the custom VM ────────────────
# Write VM config to a file so ANR can pass it as chain_config
VM_CONFIG_FILE="$PROJECT_ROOT/build/vm-config.json"
cat > "$VM_CONFIG_FILE" << VMCFG
{"zcashRpcEndpoint":"${ZCASH_RPC_ENDPOINT}","confirmationDepth":${ZCASH_CONF_DEPTH}}
VMCFG

# Create a minimal genesis file (empty JSON object; VM handles defaults)
GENESIS_FILE="$PROJECT_ROOT/build/genesis.json"
echo '{}' > "$GENESIS_FILE"

log "Starting 5-node local network with ${VM_NAME}..."
"$ANR_BIN" control start \
    --endpoint="0.0.0.0:${ANR_PORT}" \
    --avalanchego-path="$AVALANCHEGO_PATH" \
    --number-of-nodes=5 \
    --plugin-dir="$PLUGIN_DIR" \
    --blockchain-specs='[{"vm_name":"'"${VM_NAME}"'","genesis":"'"${GENESIS_FILE}"'","chain_config":"'"${VM_CONFIG_FILE}"'"}]'

# ── Step 7: Wait for health & discover endpoints ────────────────────
log "Waiting for network to become healthy..."
for i in $(seq 1 30); do
    if "$ANR_BIN" control health --endpoint="0.0.0.0:${ANR_PORT}" 2>/dev/null | grep -q "healthy"; then
        break
    fi
    sleep 2
done

log "Fetching network status..."
STATUS=$("$ANR_BIN" control status --endpoint="0.0.0.0:${ANR_PORT}" 2>&1) || true

# Try to extract a blockchain endpoint from the status output
BLOCKCHAIN_ENDPOINT=""
while IFS= read -r line; do
    if [[ "$line" == *"http"* && "$line" == *"/ext/bc/"* ]]; then
        BLOCKCHAIN_ENDPOINT="$(echo "$line" | grep -oP 'http[^\s"]+/ext/bc/[^\s"]+' | head -1)"
        break
    fi
done <<< "$STATUS"

# Also grab any node URI for the base API
NODE_URI=""
while IFS= read -r line; do
    if [[ "$line" == *"http://127.0.0.1"* ]]; then
        NODE_URI="$(echo "$line" | grep -oP 'http://127\.0\.0\.1:\d+' | head -1)"
        break
    fi
done <<< "$STATUS"

# If we couldn't auto-detect, try querying the status for blockchain IDs
if [[ -z "$BLOCKCHAIN_ENDPOINT" && -n "$NODE_URI" ]]; then
    # List all blockchains from the P-Chain
    BLOCKCHAINS=$(curl -s -X POST --data '{
        "jsonrpc":"2.0",
        "id":1,
        "method":"platform.getBlockchains",
        "params":{}
    }' -H 'content-type:application/json' "${NODE_URI}/ext/bc/P" 2>/dev/null || echo "")

    BLOCKCHAIN_ID=$(echo "$BLOCKCHAINS" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    for bc in data.get('result', {}).get('blockchains', []):
        if bc.get('vmID','') != '' and 'zcash' in bc.get('name','').lower():
            print(bc['id'])
            break
        if bc.get('vmID','') != '' and bc['id'] not in ('11111111111111111111111111111111LpoYY','2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5','2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM'):
            print(bc['id'])
            break
except:
    pass
" 2>/dev/null || echo "")

    if [[ -n "$BLOCKCHAIN_ID" ]]; then
        BLOCKCHAIN_ENDPOINT="${NODE_URI}/ext/bc/${BLOCKCHAIN_ID}"
    fi
fi

# ── Step 8: Print results ───────────────────────────────────────────
echo ""
echo -e "${BOLD}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}  Zcash Bridge VM – 5-Node Local Network Ready${NC}"
echo -e "${BOLD}═══════════════════════════════════════════════════════════════${NC}"
echo ""

if [[ -n "$NODE_URI" ]]; then
    echo -e "  ${CYAN}Node URI:${NC}          $NODE_URI"
fi

if [[ -n "$BLOCKCHAIN_ENDPOINT" ]]; then
    RPC_URL="${BLOCKCHAIN_ENDPOINT}/rpc"
    echo -e "  ${CYAN}Blockchain RPC:${NC}    $RPC_URL"
    echo -e "  ${CYAN}Zcash Backend:${NC}     $ZCASH_RPC_ENDPOINT"
    echo -e "  ${CYAN}Confirm Depth:${NC}     $ZCASH_CONF_DEPTH"
    echo ""
    echo -e "${BOLD}── Example curl commands ──────────────────────────────────────${NC}"
    echo ""
    echo -e "  ${GREEN}# Request a Zcash block height (will return 'pending', then 'accepted'):${NC}"
    echo "  curl -s -X POST '${RPC_URL}' \\"
    echo "    -H 'content-type: application/json' \\"
    echo "    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"zavax.getBlockByHeight\",\"params\":{\"height\":100}}' | python3 -m json.tool"
    echo ""
    echo -e "  ${GREEN}# Query again to see accepted status:${NC}"
    echo "  curl -s -X POST '${RPC_URL}' \\"
    echo "    -H 'content-type: application/json' \\"
    echo "    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"zavax.getBlockByHeight\",\"params\":{\"height\":100}}' | python3 -m json.tool"
    echo ""
    echo -e "  ${GREEN}# Test too-recent height rejection (mock has 2000 blocks, depth=24):${NC}"
    echo "  curl -s -X POST '${RPC_URL}' \\"
    echo "    -H 'content-type: application/json' \\"
    echo "    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"zavax.getBlockByHeight\",\"params\":{\"height\":1990}}' | python3 -m json.tool"
    echo ""
    echo -e "  ${GREEN}# Test height-0 rejection:${NC}"
    echo "  curl -s -X POST '${RPC_URL}' \\"
    echo "    -H 'content-type: application/json' \\"
    echo "    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"zavax.getBlockByHeight\",\"params\":{\"height\":0}}' | python3 -m json.tool"
else
    warn "Could not auto-detect blockchain endpoint."
    echo ""
    echo -e "  Check status manually:"
    echo "    $ANR_BIN control status --endpoint=0.0.0.0:${ANR_PORT}"
    echo ""
    echo "  Then use the blockchain URI from the output, e.g.:"
    echo "    curl -s -X POST 'http://127.0.0.1:9650/ext/bc/<BLOCKCHAIN_ID>/rpc' \\"
    echo "      -H 'content-type: application/json' \\"
    echo "      -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"zavax.getBlockByHeight\",\"params\":{\"height\":100}}'"
fi

echo ""
echo -e "${BOLD}═══════════════════════════════════════════════════════════════${NC}"
echo -e "  ${YELLOW}Press Ctrl-C to stop the network and clean up.${NC}"
echo -e "${BOLD}═══════════════════════════════════════════════════════════════${NC}"
echo ""

# Export for any child processes
if [[ -n "${BLOCKCHAIN_ENDPOINT:-}" ]]; then
    export BLOCKCHAIN_ENDPOINT="${BLOCKCHAIN_ENDPOINT}/rpc"
fi

# Keep the script alive so the network stays up
wait

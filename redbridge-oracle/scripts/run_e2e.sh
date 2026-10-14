#!/usr/bin/env bash
# Script to set up and run a 5-node Avalanche local network with the
# Zcash Bridge VM, then send a test getZcashHeight request.
#
# Prerequisites:
#   - Go 1.22+
#   - AvalancheGo v1.15.0 binary (or it will be built from source)
#   - avalanche-network-runner server (or it will be installed)
#
# Usage:
#   ./scripts/run_e2e.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Configuration
AVALANCHEGO_VERSION="v1.15.0"
ANR_VERSION="v1.8.3"
GOPATH="${GOPATH:-$HOME/go}"
BIN_DIR="$GOPATH/bin"
AVALANCHEGO_PATH="${AVALANCHEGO_PATH:-}"
VM_NAME="redbridge"
VM_ID="${VM_ID:-sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT}" # zeroPad32("redbridge") CB58
PLUGIN_DIR="$PROJECT_ROOT/build/plugins"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${GREEN}[e2e]${NC} $*"; }
warn() { echo -e "${YELLOW}[e2e]${NC} $*"; }
err() { echo -e "${RED}[e2e]${NC} $*" >&2; }

cleanup() {
    log "Cleaning up..."
    # Stop ANR server if running
    if [[ -n "${ANR_PID:-}" ]]; then
        kill "$ANR_PID" 2>/dev/null || true
        wait "$ANR_PID" 2>/dev/null || true
    fi
    # Stop mock Zcash RPC if running
    if [[ -n "${MOCK_ZCASH_PID:-}" ]]; then
        kill "$MOCK_ZCASH_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# ---------- Step 1: Build the VM plugin ----------
log "Building zcash-bridge-vm plugin..."
cd "$PROJECT_ROOT"
rm -rf "$PLUGIN_DIR"
mkdir -p "$PLUGIN_DIR"
go build -o "$PLUGIN_DIR/${VM_ID}" ./plugin
log "Plugin built: $PLUGIN_DIR/${VM_ID}"

# ---------- Step 2: Start a mock Zcash RPC server ----------
log "Starting mock Zcash RPC server on :18232..."
go run "$PROJECT_ROOT/tests/e2e/cmd/mock_zcash/main.go" &
MOCK_ZCASH_PID=$!
sleep 1
log "Mock Zcash RPC server running (PID=$MOCK_ZCASH_PID)"

# ---------- Step 3: Ensure AvalancheGo binary is available ----------
if [[ -z "$AVALANCHEGO_PATH" ]]; then
    AVALANCHEGO_PATH="$BIN_DIR/avalanchego"
fi

if [[ ! -x "$AVALANCHEGO_PATH" ]]; then
    log "AvalancheGo not found at $AVALANCHEGO_PATH"
    log "Building AvalancheGo ${AVALANCHEGO_VERSION} from source..."
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
fi
log "AvalancheGo binary: $AVALANCHEGO_PATH"

# ---------- Step 4: Ensure ANR is available ----------
ANR_BIN="$HOME/bin/avalanche-network-runner"
if [[ ! -x "$ANR_BIN" ]]; then
    log "Downloading avalanche-network-runner ${ANR_VERSION} pre-built binary..."
    mkdir -p "$HOME/bin"
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
    esac
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ANR_URL="https://github.com/ava-labs/avalanche-network-runner/releases/download/${ANR_VERSION}/avalanche-network-runner_${ANR_VERSION#v}_${OS}_${ARCH}.tar.gz"
    curl -sSfL "$ANR_URL" | tar xz -C "$HOME/bin" avalanche-network-runner
    chmod +x "$ANR_BIN"
fi
export PATH="$HOME/bin:$PATH"
log "ANR binary: $ANR_BIN"

# ---------- Step 5: Start ANR gRPC server ----------
log "Starting ANR server on :8097..."
"$ANR_BIN" server \
    --log-level info \
    --port=":8097" \
    --grpc-gateway-port=":8098" &
ANR_PID=$!
sleep 2
log "ANR server running (PID=$ANR_PID)"

# ---------- Step 6: Start a 5-node network ----------
log "Starting 5-node local network..."
VM_CONFIG_FILE="$PROJECT_ROOT/build/vm-config.json"
cat > "$VM_CONFIG_FILE" << VMCFG
{"zcashRpcEndpoint":"http://127.0.0.1:18232","confirmationDepth":24}
VMCFG
GENESIS_FILE="$PROJECT_ROOT/build/genesis.json"
echo '{}' > "$GENESIS_FILE"

export AVALANCHEGO_EXEC_PATH="$AVALANCHEGO_PATH"
export AVALANCHEGO_PLUGIN_PATH="$PLUGIN_DIR"
"$ANR_BIN" control start \
    --endpoint="0.0.0.0:8097" \
    --avalanchego-path="$AVALANCHEGO_PATH" \
    --number-of-nodes=5 \
    --plugin-dir="$PLUGIN_DIR" \
    --blockchain-specs='[{"vm_name":"'"${VM_NAME}"'","genesis":"'"${GENESIS_FILE}"'","chain_config":"'"${VM_CONFIG_FILE}"'"}]'

log "Waiting for network health..."
sleep 15

# ---------- Step 7: Get node endpoints ----------
log "Fetching node status..."
"$ANR_BIN" control status --endpoint="0.0.0.0:8097"

# ---------- Step 8: Run the Go e2e test ----------
log "Running e2e test..."
cd "$PROJECT_ROOT"
go test -v -timeout 120s -tags e2e ./tests/e2e/ \
    -avalanchego-path="$AVALANCHEGO_PATH" \
    -anr-endpoint="0.0.0.0:8097" \
    -vm-id="$VM_ID"

log "E2E test complete!"

# ---------- Step 9: Teardown ----------
log "Stopping network..."
"$ANR_BIN" control stop --endpoint="0.0.0.0:8097" || true

log "All done!"

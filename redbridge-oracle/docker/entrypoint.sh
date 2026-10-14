#!/bin/sh
# ─────────────────────────────────────────────────────────────────────────
# entrypoint.sh — materializes the redbridge-oracle VM's per-chain config.json
# from environment variables (if REDBRIDGE_CHAIN_ID is set), installs the VM
# plugin into AVAGO_PLUGIN_DIR, then execs
# AvalancheGo. See ../DEPLOYMENT.md §4.4 for the variable reference.
# ─────────────────────────────────────────────────────────────────────────
set -eu

DATA_DIR="${AVAGO_DATA_DIR:-/root/.avalanchego}"
CHAIN_CONFIG_DIR="${DATA_DIR}/configs/chains"
PLUGIN_DIR="${AVAGO_PLUGIN_DIR:-${DATA_DIR}/plugins}"

# Install the baked VM plugin into the plugin dir on every start. The image
# keeps it outside the data volume (REDBRIDGE_PLUGIN_SRC), so this works for a
# fresh volume, an existing node's bind-mounted data dir (other plugins in it
# are left untouched), and an image upgrade (an older binary is replaced).
# Copy-then-rename so a busy/old binary at the target is swapped atomically.
mkdir -p "${PLUGIN_DIR}"
cp -f "${REDBRIDGE_PLUGIN_SRC}" "${PLUGIN_DIR}/.${VM_ID}.tmp"
chmod 0755 "${PLUGIN_DIR}/.${VM_ID}.tmp"
mv -f "${PLUGIN_DIR}/.${VM_ID}.tmp" "${PLUGIN_DIR}/${VM_ID}"
echo "[entrypoint] installed VM plugin -> ${PLUGIN_DIR}/${VM_ID}"

if [ -n "${REDBRIDGE_CHAIN_ID:-}" ]; then
    mkdir -p "${CHAIN_CONFIG_DIR}/${REDBRIDGE_CHAIN_ID}"
    CONFIG_PATH="${CHAIN_CONFIG_DIR}/${REDBRIDGE_CHAIN_ID}/config.json"

    cat > "${CONFIG_PATH}" <<EOF
{
  "zcashRpcEndpoint": "${ZCASH_RPC_ENDPOINT:-http://127.0.0.1:8232}",
  "zcashRpcUser": "${ZCASH_RPC_USER:-}",
  "zcashRpcPassword": "${ZCASH_RPC_PASSWORD:-}",
  "confirmationDepth": ${ZCASH_CONFIRMATION_DEPTH:-24}
}
EOF
    chmod 600 "${CONFIG_PATH}"
    echo "[entrypoint] wrote VM chain config -> ${CONFIG_PATH}"
else
    echo "[entrypoint] REDBRIDGE_CHAIN_ID not set — skipping VM chain config generation" \
         "(mount ${CHAIN_CONFIG_DIR}/<chainID>/config.json yourself if needed)"
fi

exec /avalanchego/build/avalanchego "$@"

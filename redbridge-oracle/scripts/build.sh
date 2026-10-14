#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────
# build.sh – Build the Zcash Bridge VM plugin binary.
#
# Usage:
#   ./scripts/build.sh [OUTPUT_DIR] [VM_ID]
#
# Arguments:
#   OUTPUT_DIR  – directory to place the binary (default: ./build/plugins)
#   VM_ID       – filename for the binary      (default: sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT)
#
# Examples:
#   ./scripts/build.sh
#   ./scripts/build.sh ./build/plugins sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
#   ./scripts/build.sh /tmp/my-plugins customVmId
# ──────────────────────────────────────────────────────────────────────
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DEFAULT_VM_ID="sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT"

OUTPUT_DIR="${1:-$PROJECT_ROOT/build/plugins}"
VM_ID="${2:-$DEFAULT_VM_ID}"
OUTPUT_PATH="$OUTPUT_DIR/$VM_ID"

echo "Building Zcash Bridge VM..."
echo "  output: $OUTPUT_PATH"

mkdir -p "$OUTPUT_DIR"
go build -o "$OUTPUT_PATH" "$PROJECT_ROOT/plugin"

echo "Build complete: $OUTPUT_PATH"

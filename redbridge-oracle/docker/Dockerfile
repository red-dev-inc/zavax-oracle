# ─────────────────────────────────────────────────────────────────────────
# redbridge-oracle validator image: AvalancheGo v1.15.0 + the zcash-bridge-vm
# plugin. The plugin is baked at /opt/redbridge/plugins/<VM_ID>, outside the
# data volume, and entrypoint.sh installs it into AVAGO_PLUGIN_DIR on every
# start — so it works with a fresh volume, an existing node's data dir, and
# after image upgrades.
#
# Build from the redbridge-oracle/ module root (the directory containing go.mod):
#   docker build -f docker/Dockerfile -t redbridge-oracle-node:v1.15.0 .
# ─────────────────────────────────────────────────────────────────────────

ARG AVALANCHEGO_VERSION=v1.15.0

# ── Stage 1: build the VM plugin binary ────────────────────────────────
FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY plugin/ ./plugin/
COPY vm/ ./vm/
COPY zcash/ ./zcash/

ARG VM_ID=sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
RUN CGO_ENABLED=1 go build -o /out/${VM_ID} ./plugin

# ── Stage 2: AvalancheGo runtime + plugin ──────────────────────────────
FROM avaplatform/avalanchego:${AVALANCHEGO_VERSION}
ARG VM_ID=sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
ARG SUBNET=
ARG CHAIN_ID=
# Defaults to mainnet (AvalancheGo's own default) so a bare `docker build`
# with no --build-arg matches production; CI passes NETWORK_ID=fuji for the
# development image.
ARG NETWORK_ID=mainnet
ENV VM_ID=${VM_ID} \
    REDBRIDGE_PLUGIN_SRC=/opt/redbridge/plugins/${VM_ID} \
    AVAGO_DATA_DIR=/root/.avalanchego \
    AVAGO_PLUGIN_DIR=/root/.avalanchego/plugins \
    AVAGO_HTTP_HOST=0.0.0.0 \
    AVAGO_HTTP_PORT=9650 \
    AVAGO_STAKING_PORT=9651 \
    AVAGO_NETWORK_ID=${NETWORK_ID} \
    AVAGO_TRACK_SUBNETS=${SUBNET} \
    REDBRIDGE_CHAIN_ID=${CHAIN_ID} \
    ZCASH_RPC_USER= \
    ZCASH_RPC_PASSWORD= \
    ZCASH_CONFIRMATION_DEPTH=24

# curl is needed for the container healthcheck; the base image doesn't ship one.
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

# Kept outside /root/.avalanchego: anything under the data dir is hidden by a
# bind mount (an existing node's data dir) and never refreshed in a named
# volume that already has content (image upgrades).
COPY --from=builder /out/${VM_ID} /opt/redbridge/plugins/${VM_ID}

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME ["/root/.avalanchego"]
EXPOSE 9650 9651

ENTRYPOINT ["/entrypoint.sh"]

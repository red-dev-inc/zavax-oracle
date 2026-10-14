# redbridge-oracle — Node & VM Deployment Guide

This is the **complete** guide to running an AvalancheGo validator node with the
`redbridge-oracle` custom VM. It covers server sizing, the Zcash node, the Docker
install, the manual (non-Docker) install, and joining as a validator.

## Which guide should I use?

| You want to… | Use |
|---|---|
| Get a Fuji validator running quickly with Docker and default settings | **[EASY_DEPLOYMENT.md](EASY_DEPLOYMENT.md)** — step by step, no prior experience assumed |
| A reference for the Docker image itself (env vars, ports, existing node) | **[docker/README.md](docker/README.md)** |
| Build the image yourself, install without Docker, tune AvalancheGo flags, switch networks | **This document** |
| Run a local 5-node test network for development | [README.md → Running a Local Network](README.md#running-a-local-network) |

---

## 1. How It Fits Together

Each validator server runs AvalancheGo (with the VM plugin) and a Zcash node:

```
┌─────────────────────────────────────────────────────┐
│ Validator host                                      │
│                                                     │
│  AvalancheGo (v1.15.0)                              │
│    ├─ Primary Network (P/C/X-Chain, Fuji)           │
│    └─ redbridge-oracle Avalanche L1 chain           │
│         └─ redbridge-oracle VM plugin (Go binary)   │
│                    │                                │
│                    │ JSON-RPC (port 8232, private)  │
│                    ▼                                │
│  Zebra  (Zcash Mainnet node — the data source)      │
└─────────────────────────────────────────────────────┘
```

The VM plugin is a separate program that AvalancheGo launches (over the
`rpcchainvm` protocol) — it is **not** compiled into AvalancheGo. AvalancheGo
finds it by looking for a file named after the VM ID inside its plugin directory.

### Key values

| Name | Value | Used for |
|---|---|---|
| Avalanche network | `fuji` | `--network-id` / `AVAGO_NETWORK_ID` |
| **Subnet (L1) ID** | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` | `--track-subnets` / `AVAGO_TRACK_SUBNETS`, and **joining as a validator** (§6) |
| Blockchain ID | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` | RPC URL `/ext/bc/<id>/rpc`, chain config folder, `REDBRIDGE_CHAIN_ID` |
| VM name | `redbridge` | — |
| VM ID | `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` (`CB58(zeroPad32("redbridge"))`) | Plugin filename: `<plugin-dir>/sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` |
| Zcash network | Zcash **Mainnet** | Your Zebra node's network |

---

## 2. Server Specification

Sized for validating the `redbridge-oracle` L1 while syncing the Avalanche Fuji
Primary Network **and** a Zcash Mainnet node (Zebra) on the same machine.

| Resource | Minimum | Recommended |
|---|---|---|
| CPU | 8 vCPU | 16 vCPU |
| RAM | 16 GB | 32 GB |
| Storage | 1 TB NVMe SSD | 2 TB NVMe SSD |
| Network | 200 Mbps, static/reserved public IP | 1 Gbps, static/reserved public IP |
| OS | Ubuntu 22.04 LTS (x86_64) | **Ubuntu 26.04 LTS (x86_64) — latest LTS** |

Notes:
- **Operating system:** use the **latest Ubuntu LTS (26.04)** for new servers —
  it has the longest support window. Ubuntu 24.04 LTS and 22.04 LTS are also
  supported. Use the server (not desktop) edition.
- These numbers follow Ava Labs' published AvalancheGo validator requirements,
  sized up to leave room for Zebra on the same host.
- Disk is dominated by the Avalanche Primary Network state and the Zcash chain
  (roughly 300 GB for Zebra). The `redbridge-oracle` VM's own database is a few
  hundred MB at most.
- If Zebra runs on a different server, the minimum column is comfortable for the
  validator alone.
- ARM64 is supported by AvalancheGo but is not this project's reference
  platform — stick to x86_64 unless you've validated the VM build on ARM.

---

## 3. Prerequisites

### 3.1 Software

| Component | Version | Needed for | Notes |
|---|---|---|---|
| Ubuntu | **26.04 LTS** (recommended), 24.04 / 22.04 LTS | Both | See §2 |
| Docker + Compose plugin | any recent | Docker install (§4) | `curl -fsSL https://get.docker.com \| sudo sh`. All `docker` commands in this guide are run with `sudo` |
| git | any recent | Building the image (§4.2) / manual install (§5) | Not needed if you only pull the published image |
| Go | **1.25.10** | Manual install only | Must satisfy `go.mod` (`go 1.25.10`) |
| AvalancheGo | **v1.15.0** | Manual install only | Pinned to the version the VM's `go.mod` and rpcchainvm/warp interfaces are built against |
| build-essential / gcc | any recent | Manual install only | AvalancheGo and the VM use cgo |
| curl + jq | any recent | **Both** | The checks in this guide pipe JSON responses through `jq`: `sudo apt install -y curl jq` |
| Zebra (Zcash node) | latest release with JSON-RPC | **Both** | See §3.3 — required |

### 3.2 Network ports

| Port | Purpose | Who must reach it |
|---|---|---|
| **9651/tcp** | AvalancheGo P2P / staking | **The internet** — required for validators |
| **9650/tcp** | AvalancheGo HTTP RPC (VM served at `/ext/bc/<blockchainID>/rpc`) | **Localhost / private network only.** Never expose directly; put the RPC proxy in front (see [`security/deployment-notes.md`](../security/deployment-notes.md)) |
| **8232/tcp** | Zebra JSON-RPC | **Only the AvalancheGo node** (from inside the container, for Docker). Never the internet |
| **8233/tcp** | Zebra (Zcash) P2P | The internet — only if Zebra runs on this host |

### 3.3 Zcash node — required, with options

#### Why it's required

Every validator needs its **own synced Zcash node with JSON-RPC enabled**, for
both the Docker and the manual install. `Block.Verify()` re-fetches each block's
Zcash data from the local Zcash RPC and compares hashes, so a node without a
working Zcash RPC rejects every `redbridge-oracle` block, can't follow the chain,
and can't validate. AvalancheGo itself still starts and the Primary Network
still syncs, which makes this easy to miss.

#### Options

| Option | Description | Recommendation |
|---|---|---|
| **A. Zebra on the same host** | Run Zebra alongside AvalancheGo. | ✅ **Recommended.** Lowest latency, fewest moving parts. Size the disk per §2. |
| **B. Zebra on a separate host you control** | Point the validator at a Zebra node on another server over a private network/VPN. | ✅ Fine. Firewall 8232 so only your validator(s) can reach it. One Zebra can serve several of your own validators. |
| **C. zcashd** | The legacy Zcash node. The VM only calls `getblockcount`, `getblockhash` and `getblock` (verbosity 1), which zcashd supports. | ⚠️ Works, but zcashd is deprecated in favour of Zebra — don't choose it for new servers. |
| **D. Third-party / public Zcash RPC** | Someone else's hosted Zcash API. | ❌ Not recommended. Your validator would trust their data instead of verifying it independently, and you'd depend on their uptime and rate limits. |
| **E. `mock-zcashd`** | The mock server in this repo (`../mock-zcashd/`). | ❌ **Local testing only.** Its data doesn't match real Zcash, so a validator using it rejects every real block. |

#### Zebra configuration

Install Zebra following the official docs: <a href="https://zebra.zfnd.org/" target="_blank" rel="noopener noreferrer">zebra.zfnd.org</a>. It must
follow **Zcash Mainnet** and have its RPC server enabled. The relevant part of
`zebrad.toml`:

```toml
[network]
network = "Mainnet"

[rpc]
# Manual install, Zebra on the same host: "127.0.0.1:8232" is enough.
# Docker, Zebra on the same host: the container can't reach the host's
# 127.0.0.1, so listen on "0.0.0.0:8232" (or the docker bridge, "172.17.0.1:8232")
# and block 8232 from the internet with your firewall.
listen_addr = "127.0.0.1:8232"

# Zebra 2.x enables cookie authentication by default. The VM supports plain
# HTTP Basic auth (user/password) or no auth, not a rotating cookie file —
# disable cookie auth and protect 8232 with the firewall.
enable_cookie_auth = false
```

Wait for Zebra to **fully sync** (at least past every height that will be
bridged — in practice, to the tip). A first sync can take a day or more.

#### Zcash settings in the VM

| VM config key (manual) | Env var (Docker) | Required? | Default | Description |
|---|---|---|---|---|
| `zcashRpcEndpoint` | `ZCASH_RPC_ENDPOINT` | **Yes** | `http://127.0.0.1:8232` | Zebra's JSON-RPC URL. For Docker, the default is the container itself and is **almost never right** — see below. |
| `zcashRpcUser` | `ZCASH_RPC_USER` | Only if your Zcash RPC requires auth | *(empty)* | Basic-auth username. Leave it out if the Zcash RPC has no auth (the Zebra config above) — no auth header is sent. |
| `zcashRpcPassword` | `ZCASH_RPC_PASSWORD` | Only if your Zcash RPC requires auth | *(empty)* | Basic-auth password. Only used when a username is set. |
| `confirmationDepth` | `ZCASH_CONFIRMATION_DEPTH` | Optional | `24` | Heights within this many blocks of the Zcash tip are refused. **Keep the same value on every validator** (the default, 24). |

The VM retries each Zcash RPC call up to 3 times (2 s apart, 30 s timeout per
request).

**Choosing `ZCASH_RPC_ENDPOINT` for Docker:**

| Zebra runs on | Value | Extra flag |
|---|---|---|
| The same host | `http://host.docker.internal:8232` | `--add-host=host.docker.internal:host-gateway` (or `extra_hosts:` in compose) |
| Another host | `http://<zebra-private-ip>:8232` | — |

`127.0.0.1` inside the container is the container, not the host.

#### Check it before starting the node

From the node host (or from inside the container for Docker):

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":[]}' \
  -H 'content-type:application/json' http://<zebra-host>:8232 | jq
```

Expected:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": 3123456
}
```

Add `-u <user>:<password>` to `curl` if you configured RPC auth.

### 3.4 Accounts / credentials

- **Staking keys** — AvalancheGo creates `staker.key`/`signer.key` on first start.
  They are your NodeID; back them up (§4.5 / §7).
- **Validator requirements** — anyone can join the `redbridge-oracle` L1 as a
  validator, no approval needed. You need **100,000 RBR** tokens (contact the
  redbridge team) and enough **P-Chain AVAX** for the registration fee and the
  validator's balance (§6).
- **Zcash RPC credentials** — only if your Zebra/zcashd has RPC auth enabled.

---

## 4. Docker Deployment

> Just want it running? [EASY_DEPLOYMENT.md](EASY_DEPLOYMENT.md) is the
> step-by-step version of this section using the published image.

Docker assets live in [`docker/`](docker/):

```
docker/
├── Dockerfile          # multi-stage: build VM plugin, layer onto the avalanchego image
├── docker-compose.yml  # single-node example with a named volume for persistence
├── .env                # all node config — the source of truth for compose (§4.4)
├── entrypoint.sh       # installs the plugin, writes the VM chain config, then starts avalanchego
└── README.md           # image reference (Docker Hub readme)
```

### 4.1 How the image works

1. **Build stage** (`golang:1.25`) compiles the `redbridge-oracle` VM plugin.
2. **Runtime stage** is the official `avaplatform/avalanchego:v1.15.0` image —
   AvalancheGo is **not** rebuilt, keeping builds fast and reproducible. The
   plugin is copied to `/opt/redbridge/plugins/<VM_ID>`, deliberately **outside**
   the data volume.
3. **On every start**, `entrypoint.sh`:
   - copies the plugin into `AVAGO_PLUGIN_DIR` (`/root/.avalanchego/plugins` by
     default), replacing any older copy;
   - writes `$AVAGO_DATA_DIR/configs/chains/$REDBRIDGE_CHAIN_ID/config.json` from the
     `ZCASH_*` variables;
   - starts AvalancheGo, passing through any extra CLI arguments.

Keeping the plugin outside the volume is what lets the same image work with a
fresh named volume, with an existing node's bind-mounted data directory (which
would otherwise hide a plugin baked under `/root/.avalanchego`), and after an
image upgrade (Docker never refreshes a named volume that already has content).

Everything under the data directory — staking keys, databases, chain config — is
persisted in the volume.

### 4.2 Image tags and building

Published tags (built by CI, `.gitlab-ci.yml`):

| Tag | Built from | Network baked in | Use it? |
|---|---|---|---|
| `reddev2020/redbridge-oracle-node:v1.0.0-fuji` | `develop` | `fuji` | ✅ **Yes** — complete Fuji deployment |
| `reddev2020/redbridge-oracle-node:v1.0.0-fuji-<feature>` | `feat/*` branches | `fuji` | Testing only |
| `reddev2020/redbridge-oracle-node:v1.0.0` | `production` | `mainnet` | ❌ Not yet — see below |

CI passes these build args: `NETWORK_ID`, `VM_ID`, `SUBNET`
(`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`) and `CHAIN_ID`
(`8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq`). They become the image's
defaults for `AVAGO_NETWORK_ID`, `AVAGO_TRACK_SUBNETS` and `REDBRIDGE_CHAIN_ID`.

> **The `mainnet` tag is not a working mainnet node.** `redbridge-oracle` is not
> deployed on Avalanche mainnet yet, and `AVAGO_TRACK_SUBNETS`/`REDBRIDGE_CHAIN_ID`
> are baked to the **Fuji** IDs regardless of tag. See "Switching networks" (§4.4).

To build it yourself, pass the same args — **without `SUBNET` and `CHAIN_ID`
the image has no tracked subnet or chain config baked in** (the `Dockerfile`
defaults are empty):

Clone the repo:

```bash
git clone https://github.com/red-dev-inc/redbridge-oracle.git /opt/redbridge-oracle
```

```bash
cd /opt/redbridge-oracle/redbridge-oracle
```

Build:

```bash
sudo docker build -f docker/Dockerfile \
  --build-arg NETWORK_ID=fuji \
  --build-arg SUBNET=EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj \
  --build-arg CHAIN_ID=8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq \
  -t redbridge-oracle-node:local-fuji .
```

`NETWORK_ID` defaults to `mainnet` (AvalancheGo's own default) if omitted. It
only sets the *default* — it's always overridable at runtime (§4.4).

### 4.3 Run with docker compose

Go to the Docker folder:

```bash
cd /opt/redbridge-oracle/redbridge-oracle/docker
```

Edit `.env` — set `ZCASH_RPC_ENDPOINT` (required) and anything else you need:

```bash
nano .env
```

Start (builds the image from source on first run, then starts it):

```bash
sudo docker compose up -d
```

Watch logs (Ctrl-C to stop watching):

```bash
sudo docker compose logs -f
```

> The provided `docker-compose.yml` **builds** the image from this repo. To use
> the published image instead, use the compose file in
> [EASY_DEPLOYMENT.md → Prefer docker compose?](EASY_DEPLOYMENT.md#prefer-docker-compose).

If Zebra runs on the same host, add this under the service in
`docker-compose.yml` and set `ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232`
in `.env`:

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

Ports come from `.env` (`AVAGO_STAKING_PORT`, `AVAGO_HTTP_PORT`), so changing
them there moves the published port, the container's listen port and the
healthcheck together:
- `AVAGO_STAKING_PORT` (default `9651`) — published on all host interfaces (public).
- `AVAGO_HTTP_PORT` (default `9650`) — bound to `127.0.0.1` on the host. Put the
  RPC proxy in front; don't publish it to the internet.

### 4.4 Configuration reference

**All compose configuration lives in [`docker/.env`](docker/.env)**, documented
inline.

| Variable | Required? | Default | Description |
|---|---|---|---|
| `ZCASH_RPC_ENDPOINT` | **Yes** | `http://127.0.0.1:8232` (the container — almost never right) | Zcash JSON-RPC URL (§3.3) |
| `ZCASH_RPC_USER` | Only if your Zcash RPC requires auth | *(empty)* | Zcash RPC username |
| `ZCASH_RPC_PASSWORD` | Only if your Zcash RPC requires auth | *(empty)* | Zcash RPC password |
| `ZCASH_CONFIRMATION_DEPTH` | Optional | `24` | Keep identical on all validators |
| `AVAGO_NETWORK_ID` | Optional | `fuji` (compose) / baked tag default (`docker run`) | `fuji`, `mainnet`, or a numeric local ID |
| `AVAGO_TRACK_SUBNETS` | Optional | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` | Subnet(s) to track, comma-separated |
| `REDBRIDGE_CHAIN_ID` | Optional | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` | Chain whose VM `config.json` the entrypoint writes. Unset it to manage that file yourself |
| `AVAGO_HTTP_PORT` | Optional | `9650` | RPC port |
| `AVAGO_STAKING_PORT` | Optional | `9651` | P2P/staking port |
| `AVAGO_PLUGIN_DIR` | Optional | `/root/.avalanchego/plugins` | Where the entrypoint installs the plugin. Set it as an **env var**, not a `--plugin-dir` CLI arg (the entrypoint wouldn't see it) |

**Any other AvalancheGo flag** works too. AvalancheGo reads every flag as an env
var: `--flag-name` → `AVAGO_FLAG_NAME` (dashes → underscores, uppercased), e.g.
`--log-level` → `AVAGO_LOG_LEVEL`. See the
<a href="https://docs.avax.network/nodes/configure/configs-flags" target="_blank" rel="noopener noreferrer">AvalancheGo config reference</a>.
Add it under `environment:` in `docker-compose.yml` (or `-e` on `docker run`), or
pass it as a CLI argument after the image name / via compose's `command:` — the
entrypoint forwards those to AvalancheGo, and a CLI argument wins if a flag is
set both ways:

```bash
sudo docker run --rm reddev2020/redbridge-oracle-node:v1.0.0-fuji --version
```

**`AVAGO_NETWORK_ID` has two layers:** the build-time default baked into the
image (§4.2) and the runtime value. Under compose the runtime value from `.env`
(`fuji` by default) always wins, whichever tag you built. Plain `docker run` uses
the baked default unless you pass `-e AVAGO_NETWORK_ID=...`.

**Switching networks:** `AVAGO_TRACK_SUBNETS` and `REDBRIDGE_CHAIN_ID` belong to the
network in `AVAGO_NETWORK_ID`; the defaults are the **Fuji** IDs and won't
resolve on `mainnet`. When `redbridge-oracle` is deployed to mainnet, change all
three together, and start from a **fresh volume** — a data directory bootstrapped
against Fuji can't be reused for mainnet.

#### Plain `docker run` equivalent

Pull the image:

```bash
sudo docker pull reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

Start the node:

```bash
sudo docker run -d --name redbridge-oracle \
  --add-host=host.docker.internal:host-gateway \
  -e ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232 \
  -p 9651:9651 \
  -p 127.0.0.1:9650:9650 \
  -v redbridge-data:/root/.avalanchego \
  --restart unless-stopped \
  reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

- Drop `--add-host` and use `http://<zebra-ip>:8232` if Zebra is on another host.
- If your Zcash RPC requires auth, add `-e ZCASH_RPC_USER=...` and
  `-e ZCASH_RPC_PASSWORD=...` (see the table in §4.4).
- If you change `AVAGO_HTTP_PORT`/`AVAGO_STAKING_PORT`, change both the `-e` value
  and the matching `-p` mapping — Docker doesn't keep them in sync for you.

### 4.5 Persistence

`AVAGO_DATA_DIR` is `/root/.avalanchego`, so whatever you mount there is
AvalancheGo's data directory — staking keys, DB, chain/subnet configs.

- **Fresh node:** use a named volume (`-v redbridge-data:/root/.avalanchego`, or
  `avago-data` in the provided compose file). Docker manages it for you.
- **Existing node** (a manual install from §5 or any AvalancheGo node, e.g. at
  `/home/avalanche/.avalanchego`): bind-mount that directory instead, and the
  container keeps the same NodeID, staking keys and DB. The entrypoint adds the
  `redbridge-oracle` plugin to its `plugins/` and leaves other plugins alone.

  ```bash
  -v /home/avalanche/.avalanchego:/root/.avalanchego
  ```

  or in `docker-compose.yml`, replace `avago-data:/root/.avalanchego` with
  `/home/avalanche/.avalanchego:/root/.avalanchego` (and drop the top-level
  `avago-data:` volume).

  When doing this:
  - **Stop the existing `avalanchego` first** — two processes must never open the
    same DB:

    ```bash
    sudo systemctl stop avalanchego
    ```

    ```bash
    sudo systemctl disable avalanchego
    ```
  - **Keep tracking what it already tracks:** set `AVAGO_TRACK_SUBNETS` to its
    existing subnet IDs plus `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`
    (comma-separated) and use the same `AVAGO_NETWORK_ID`.
  - **Carry over old settings:** flags the old node set via CLI or `--config-file`
    (public IP, ports, …) aren't picked up — pass them as `AVAGO_*` env vars.
  - **Chain config is regenerated** on every start from the `ZCASH_*` vars.
  - **Ownership:** the image runs as root, so new files are root-owned. Before
    going back to the systemd service, run
    `sudo chown -R avalanche:avalanche /home/avalanche/.avalanchego`.

**Back up `/root/.avalanchego/staking`** (`staker.key` + `signer.key`) either way:

```bash
sudo docker cp redbridge-oracle:/root/.avalanchego/staking ./redbridge-staking-backup
```

### 4.6 Verify

Logs (Ctrl-C to stop watching):

```bash
sudo docker logs -f redbridge-oracle
```

Then run the same `curl` checks as §5.9, against whichever host/port you published.

---

## 5. Manual Installation (no Docker)

Installs AvalancheGo and the `redbridge-oracle` plugin directly on Ubuntu and runs
AvalancheGo under systemd as a dedicated non-root user. Zebra (§3.3) is assumed to
be installed and synced already.

### 5.1 Base packages, service user and directories

Install packages:

```bash
sudo apt update && sudo apt install -y build-essential git curl jq
```

Create the service user:

```bash
sudo useradd -m -s /usr/sbin/nologin avalanche
```

Create the data directories:

```bash
sudo mkdir -p /home/avalanche/.avalanchego/{plugins,configs/chains,configs/subnets}
sudo chown -R avalanche:avalanche /home/avalanche/.avalanchego
```

### 5.2 Install Go 1.25.10

```bash
curl -fsSLO https://go.dev/dl/go1.25.10.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.10.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
source /etc/profile.d/go.sh
```

Check it — expect `go version go1.25.10 linux/amd64`:

```bash
go version
```

### 5.3 Install AvalancheGo v1.15.0

```bash
git clone --depth 1 --branch v1.15.0 https://github.com/ava-labs/avalanchego.git /tmp/avalanchego
cd /tmp/avalanchego
./scripts/build.sh
sudo install -o avalanche -g avalanche -m 0755 build/avalanchego /usr/local/bin/avalanchego
```

(A prebuilt v1.15.0 binary from the AvalancheGo releases page works too.)

### 5.4 Build and install the `redbridge-oracle` VM plugin

```bash
git clone https://github.com/red-dev-inc/redbridge-oracle.git /opt/redbridge-oracle
cd /opt/redbridge-oracle/redbridge-oracle
go build -o /tmp/redbridge-vm ./plugin
sudo install -o avalanche -g avalanche -m 0755 /tmp/redbridge-vm \
  /home/avalanche/.avalanchego/plugins/sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
```

Alternatively, `./scripts/build.sh <plugin-dir> <vm-id>` builds straight into the
plugin directory:

```bash
./scripts/build.sh /home/avalanche/.avalanchego/plugins \
  sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
sudo chown avalanche:avalanche \
  /home/avalanche/.avalanchego/plugins/sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
```

The filename **must** be the VM ID exactly — AvalancheGo resolves plugins by ID.

### 5.5 Configure the chain (VM config)

AvalancheGo reads per-chain VM config from
`~/.avalanchego/configs/chains/<blockchainID>/config.json`. For the deployed Fuji
chain (`8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq`):

```bash
CHAIN_DIR=/home/avalanche/.avalanchego/configs/chains/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq
sudo -u avalanche mkdir -p "$CHAIN_DIR"
sudo -u avalanche tee "$CHAIN_DIR/config.json" <<'EOF'
{
  "zcashRpcEndpoint": "http://127.0.0.1:8232",
  "confirmationDepth": 24
}
EOF
sudo chmod 600 "$CHAIN_DIR/config.json"
```

Add `"zcashRpcUser"` / `"zcashRpcPassword"` only if your Zcash RPC uses auth;
change `zcashRpcEndpoint` if Zebra is on another host. See the settings table in
§3.3.

If you are bootstrapping a **new** L1/chain rather than joining this one,
substitute the blockchain ID returned by `platform.createBlockchain`.

### 5.6 Track the subnet

A node only follows the `redbridge-oracle` chain if it tracks the subnet ID
`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` (`--track-subnets`, set in the
systemd unit below). Tracking is separate from being a registered validator (§6).

### 5.7 systemd service

```ini
# /etc/systemd/system/avalanchego.service
[Unit]
Description=AvalancheGo Node (redbridge-oracle validator)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=avalanche
Group=avalanche
LimitNOFILE=32768
ExecStart=/usr/local/bin/avalanchego \
  --network-id=fuji \
  --http-host=127.0.0.1 \
  --http-port=9650 \
  --staking-port=9651 \
  --track-subnets=EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj \
  --data-dir=/home/avalanche/.avalanchego \
  --log-level=info
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

`--http-host=127.0.0.1` keeps the RPC port off the public interface; the RPC proxy
(OpenResty) is what's exposed externally, per
[`security/deployment-notes.md`](../security/deployment-notes.md). The staking port
(9651) is the one that must be public.

Load and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now avalanchego
```

Logs (Ctrl-C to stop watching):

```bash
sudo journalctl -u avalanchego -f
```

### 5.8 Firewall (UFW quick version)

AvalancheGo P2P — public:

```bash
sudo ufw allow 9651/tcp
```

Zebra P2P — only if Zebra runs here:

```bash
sudo ufw allow 8233/tcp
```

SSH (or your custom SSH port) — **before** enabling, so you don't lock yourself out:

```bash
sudo ufw allow OpenSSH
```

Enable the firewall:

```bash
sudo ufw enable
```

9650 and 8232 stay closed. The full production rules (including the RPC proxy)
are in [`security/deployment-notes.md`](../security/deployment-notes.md).

### 5.9 Verify

**Bootstrapped?** All four (P, X, C and the redbridge-oracle chain) must print
`{"isBootstrapped":true}`:

```bash
for chain in P X C 8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq; do
  printf '%-52s ' "$chain"
  curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"}}" \
    -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq -c '.result // .error'
done
```

**redbridge-oracle chain is serving RPC** — block data, or
`{"status":"not_found"}` / `{"status":"pending"}`:

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{}}' \
  -H 'content-type:application/json' \
  http://127.0.0.1:9650/ext/bc/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/rpc | jq
```

---

## 6. Join as a Validator

Once every chain reports `isBootstrapped: true` and the VM RPC responds, register
the node as a validator of the `redbridge-oracle` L1.

> **💰 You need AVAX on the P-Chain.** Registering a validator is a P-Chain
> transaction, so the wallet that submits it must hold enough **P-Chain** AVAX to
> pay the transaction fee **and** fund the validator's balance. An L1 validator
> pays a continuous fee from that balance; if it runs out, the validator becomes
> inactive and stops earning uptime. AVAX on the C-Chain doesn't count — transfer
> it from C-Chain to P-Chain first (on Fuji, use test AVAX from the faucet).

> **🪙 You need 100,000 RBR tokens.** Joining the redbridge-oracle L1 as a
> validator requires **100,000 RBR** (the redbridge-oracle token). To get RBR,
> please contact the **redbridge team** before you register.

**1. Get the node's identity** (works the same for Docker and manual installs):

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq
```

Note `nodeID`, `nodePOP.publicKey` and `nodePOP.proofOfPossession`.

**2. Register** via the Avalanche Builder Console:
<a href="https://build.avax.network/console/add-validator/" target="_blank" rel="noopener noreferrer">build.avax.network/console/add-validator</a>

| Field | Value |
|---|---|
| Network | Fuji |
| **Subnet ID** | **`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`** |
| Node ID | `nodeID` from step 1 |
| BLS public key / PoP | `nodePOP.publicKey` / `nodePOP.proofOfPossession` |

Anyone can register a validator — no approval from the L1 owner is needed, as
long as you have the 100,000 RBR tokens and enough P-Chain AVAX (see above).

**3. Confirm:**

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getCurrentValidators","params":{"subnetID":"EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj"}}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/bc/P | jq
```

**4. Tell the relayer operator** — if this is a new or replacement validator,
the ICM relayer's `manually-tracked-peers` / destination RPC endpoint may need
updating (see `relayer-config.json`).

---

## 7. Post-Install Checklist

- [ ] Server runs Ubuntu LTS (26.04 recommended), x86_64, with a static public IP
- [ ] Zebra is on **Zcash Mainnet**, fully synced, and reachable from AvalancheGo (§3.3 check passes)
- [ ] `info.isBootstrapped` is `true` for `P`, `X`, `C` and `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq`
- [ ] `zavax.getBlockByHeight` responds (block data, `pending` or `not_found`)
- [ ] Logs show no repeated Zcash RPC errors
- [ ] `--track-subnets` / `AVAGO_TRACK_SUBNETS` includes `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`
- [ ] Staking keys (`staker.key` + `signer.key`) backed up off the server
- [ ] `9651` **is** reachable from the internet; `9650` and `8232` are **not**
- [ ] Firewall rules from [`security/deployment-notes.md`](../security/deployment-notes.md) applied
- [ ] Enough AVAX on the **P-Chain** for the registration fee and validator balance
- [ ] 100,000 RBR tokens (contact the redbridge team)
- [ ] Node registered as a validator on subnet `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` (§6)
- [ ] Relayer operator informed if this is a new/replacement validator

---

## 8. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| P/X/C bootstrap but the redbridge-oracle chain doesn't | VM can't verify blocks — Zcash RPC unreachable, unsynced, or on the wrong network | Check §3.3; look for `zcash rpc error` in the logs |
| `http status 401` in logs | Wrong RPC credentials, or Zebra cookie auth enabled | Fix user/password, or `enable_cookie_auth = false` |
| `connection refused` to `127.0.0.1:8232` (Docker) | The container's localhost isn't the host | Use `host.docker.internal` + `--add-host` (§3.3) |
| Few/no peers | 9651 blocked | Open 9651/tcp in UFW **and** the cloud provider firewall |
| `plugin not found` / chain fails to start | Plugin filename isn't the VM ID, or wrong plugin dir | Filename must be `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` (§5.4) |
| `permission denied ... docker.sock` | `docker` run without `sudo` | Prefix the command with `sudo` |
| Registration fails with an insufficient-funds error | Not enough AVAX on the **P-Chain** | Transfer AVAX from C-Chain to P-Chain, then retry (§6) |
| Docker image starts but doesn't track the subnet | Self-built image without `SUBNET`/`CHAIN_ID` build args | Rebuild with the args (§4.2) or set `AVAGO_TRACK_SUBNETS`/`REDBRIDGE_CHAIN_ID` at runtime |

---

## 9. References

- [EASY_DEPLOYMENT.md](EASY_DEPLOYMENT.md) — quickest Docker setup, step by step
- [docker/README.md](docker/README.md) — Docker image reference
- [README.md](README.md) — VM internals, API, local test network via `scripts/run.sh`
- [`security/deployment-notes.md`](../security/deployment-notes.md) — firewall and RPC proxy configuration
- <a href="https://docs.avax.network/nodes/configure/configs-flags" target="_blank" rel="noopener noreferrer">AvalancheGo config flags</a>
- <a href="https://zebra.zfnd.org/" target="_blank" rel="noopener noreferrer">Zebra documentation</a>

# redbridge-oracle Node (Docker image)

**AvalancheGo v1.15.0 + the `redbridge-oracle` VM plugin**, packaged on top of the
official `avaplatform/avalanchego` image. The VM bridges Zcash block data onto an
Avalanche L1 using Warp messaging. Run this image to operate a `redbridge-oracle`
validator node.

> New to this? Follow **[EASY_DEPLOYMENT.md](../EASY_DEPLOYMENT.md)** — it walks
> through every step (server, Docker, Zcash node, start, verify, join) in plain
> language, with default settings and locations. This page is the reference for
> the image itself.

## At a glance

| | |
|---|---|
| Image | `reddev2020/redbridge-oracle-node` |
| Tag to use | **`v1.0.0-fuji`** (Avalanche Fuji testnet) |
| Avalanche network | `fuji` |
| **Subnet (L1) ID** | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` |
| Blockchain ID | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` |
| VM ID | `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` (VM name `redbridge`) |
| Zcash network | Zcash Mainnet |

### Tags

| Tag | Network baked in | Use it? |
|---|---|---|
| `v1.0.0-fuji` | `fuji` | ✅ **Yes** — ready-to-run Fuji node. Subnet ID, chain ID and network are all built in. |
| `v1.0.0` | `mainnet` | ❌ **Not yet.** `redbridge-oracle` isn't deployed on mainnet, and this tag still carries the Fuji subnet/chain IDs. See "Switching networks" below. |

The VM plugin is stored in the image at `/opt/redbridge/plugins/<VM_ID>` (outside
the data volume) and installed into `AVAGO_PLUGIN_DIR` on **every** container
start — no manual plugin setup, whether you use a fresh volume, an existing
node's data directory, or a newer image.

---

## Prerequisites

### Storage location

The node database and the Zcash chain together need well over 1 TB, so keep them
on a large data disk rather than the OS disk. **In our setup that disk is
mounted at `/mnt/blockstorage`**; yours may be different (e.g. `/data`,
`/srv`).

All commands in this README refer to it as **`$STORAGE_DIR`**. Set it once to your
own path before running them:

```bash
export STORAGE_DIR=/mnt/blockstorage    # ← change to your data disk's mount point
```

Optional — keep it for future shells too:

```bash
echo "export STORAGE_DIR=$STORAGE_DIR" >> ~/.bashrc
```

Check the disk is mounted and has free space:

```bash
df -h "$STORAGE_DIR"
```

> If you open a new terminal without the `~/.bashrc` line, run the `export` again —
> otherwise `$STORAGE_DIR` is empty and paths like `$STORAGE_DIR/zebra` become
> `/zebra` on the OS disk.

### Server

| | Minimum | Recommended |
|---|---|---|
| OS | Ubuntu 22.04 LTS (x86_64) | **Ubuntu 26.04 LTS (x86_64) — latest LTS** |
| CPU | 8 vCPU | 16 vCPU |
| RAM | 16 GB | 32 GB |
| Storage | 1 TB NVMe SSD | 2 TB NVMe SSD |
| Network | 200 Mbps, static/reserved public IP | 1 Gbps, static/reserved public IP |

Ubuntu 24.04 LTS also works.

### Tools (`curl`, `jq`)

The checks in this README use `curl` and pipe the JSON response through `jq` so
it prints readably:

```bash
sudo apt update && sudo apt install -y curl jq
```

### Docker

Install Docker Engine (with the Compose plugin) using Docker's official script.
All `docker` commands in this README are run with `sudo`.

**1. Install Docker Engine + Compose plugin:**

```bash
curl -fsSL https://get.docker.com | sudo sh
```

**2. Start Docker now and on every boot:**

```bash
sudo systemctl enable --now docker
```

**3. Check it works:**

```bash
sudo docker --version
```

```bash
sudo docker run --rm hello-world
```

### Zcash node (Zebra) — required

**A synced Zcash Mainnet node (Zebra) with JSON-RPC enabled must be reachable from
inside the container.** The VM re-fetches every Zcash block from your own Zcash
RPC to verify blocks proposed by other validators. Without it the node starts,
the Primary Network syncs, but every `redbridge-oracle` block is rejected and the
node cannot validate.

| Option | Recommended? |
|---|---|
| **A.** Zebra on the same host (Docker, below) | ✅ Yes — simplest |
| **B.** Zebra on another host you control (private network) | ✅ Fine — keep 8232 firewalled |
| **C.** zcashd (legacy) | ⚠️ Works (VM only uses `getblockcount`, `getblockhash`, `getblock`), but deprecated |
| **D.** Third-party public Zcash RPC | ❌ No — you'd trust their data instead of verifying it |
| **E.** `mock-zcashd` from this repo | ❌ Local testing only |

#### Run Zebra with Docker (option A)

Zebra needs roughly **300 GB** of disk for Zcash Mainnet; the examples below keep
its data under `$STORAGE_DIR` (see [Storage location](#storage-location)).

**1. Create the data and config directories.** The Zebra image runs as a
non-root user (UID `10001`), so it must own them:

```bash
sudo mkdir -p $STORAGE_DIR/.zebra/data $STORAGE_DIR/zebra/config
sudo chown -R 10001:10001 $STORAGE_DIR/.zebra $STORAGE_DIR/zebra
```

**2. Write the Zebra config** (`zebrad.toml`). Settings not listed keep Zebra's
defaults:

```bash
sudo tee $STORAGE_DIR/zebra/config/zebrad.toml > /dev/null <<'TOML'
[network]
network = "Mainnet"
listen_addr = "0.0.0.0:8233"            # Zcash P2P — open 8233/tcp to the internet

[state]
cache_dir = "/var/cache/zebrad-cache"   # matches the data volume mount below

[rpc]
# Listen on all interfaces *inside the Zebra container*; who can reach it from
# outside is decided by the -p mapping in step 3.
listen_addr = "0.0.0.0:8232"
# Zebra 2.x enables cookie auth by default. The redbridge-oracle VM supports
# username/password or no auth, not Zebra's cookie file — so turn it off.
enable_cookie_auth = false
TOML
sudo chown 10001:10001 $STORAGE_DIR/zebra/config/zebrad.toml
```

**3. Start Zebra:**

```bash
sudo docker run -d --name zebra \
  -p 8233:8233 \
  -p 172.17.0.1:8232:8232 \
  -v $STORAGE_DIR/.zebra/data:/var/cache/zebrad-cache \
  -v $STORAGE_DIR/zebra/config:/home/zebra/.config \
  --restart unless-stopped \
  zfnd/zebra:latest
```

| Flag | Why |
|---|---|
| `-p 8233:8233` | Zcash P2P — public, so Zebra can find peers and sync. |
| `-p 172.17.0.1:8232:8232` | JSON-RPC, published **only on the Docker bridge** (`172.17.0.1`, the address `host.docker.internal` resolves to). The `redbridge-oracle` container can reach it; the internet can't. |
| `-v .../.zebra/data:/var/cache/zebrad-cache` | Chain state — survives restarts and image upgrades. |
| `-v .../zebra/config:/home/zebra/.config` | Makes Zebra load your `zebrad.toml` from step 2. |

> ⚠️ **Don't use a plain `-p 8232:8232`.** That publishes the unauthenticated RPC on
> every interface, and Docker's port rules bypass `ufw` — so it would be open to the
> internet even with a firewall. If your Docker bridge isn't `172.17.0.1`
> (check with `ip -4 addr show docker0`), use that address instead.

**4. Wait for Zebra to fully sync** — a fresh Mainnet sync takes many hours to a
couple of days. Follow progress (look for `sync_percent` reaching ~100%; press
**Ctrl-C** to stop watching):

```bash
sudo docker logs -f zebra
```

**5. Check the RPC is reachable** (once synced, the height matches a block
explorer such as <a href="https://mainnet.zcashexplorer.app/" target="_blank" rel="noopener noreferrer">mainnet.zcashexplorer.app</a>):

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":[]}' \
  -H 'content-type:application/json' http://172.17.0.1:8232 | jq
```

Expected:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": 3123456
}
```

Zebra docs: <a href="https://zebra.zfnd.org/" target="_blank" rel="noopener noreferrer">zebra.zfnd.org</a>. For Zebra on another host (option B) or a
non-Docker install, see [DEPLOYMENT.md §3.3](../DEPLOYMENT.md#33-zcash-node--required-with-options).

> **`127.0.0.1` inside the container is the container itself, not your host.** If
> Zebra runs on the same host, start `redbridge-oracle` with
> `--add-host=host.docker.internal:host-gateway` and
> `ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232` (as in the Quick start).
> With the config above, Zebra has no RPC auth, so no RPC username or password
> is needed.

---

## Quick start

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

That's a complete Fuji node. What you **must** supply:

| Flag | Why |
|---|---|
| `-e ZCASH_RPC_ENDPOINT=...` | Your Zcash node. If Zebra is on another host, use `http://<zebra-ip>:8232` and drop `--add-host`. |
| `-p 9651:9651` | Staking/P2P port — **must be public**. |
| `-p 127.0.0.1:9650:9650` | RPC port — **local only**. Ports are never baked into the image. |
| `-v redbridge-data:/root/.avalanchego` | Keeps the database and your validator keys across restarts/upgrades. A named volume stored under `/var/lib/docker/volumes/` by default — see [Persistence](#persistence) to use your own directory or an existing AvalancheGo data folder. |

If your Zcash RPC **does** require a username and password, add
`-e ZCASH_RPC_USER=...` and `-e ZCASH_RPC_PASSWORD=...` — see
[Zcash (VM) settings](#zcash-vm-settings).

Already running an AvalancheGo node? See [Using an existing node](#using-an-existing-node).

### Using different ports

If you change `AVAGO_HTTP_PORT`/`AVAGO_STAKING_PORT`, change both the `-e` value
**and** the matching `-p` mapping — Docker doesn't keep them in sync. Example
with `9661` (staking) and `9660` (HTTP):

```bash
sudo docker run -d --name redbridge-oracle \
  --add-host=host.docker.internal:host-gateway \
  -e ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232 \
  -e AVAGO_STAKING_PORT=9661 \
  -e AVAGO_HTTP_PORT=9660 \
  -p 9661:9661 \
  -p 127.0.0.1:9660:9660 \
  -v redbridge-data:/root/.avalanchego \
  --restart unless-stopped \
  reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

The `curl` checks below then use `http://127.0.0.1:9660` instead of `9650`.

---

## Ports

| Port | Purpose | Publish policy |
|---|---|---|
| `9651` | AvalancheGo P2P / staking | **Public.** Must be reachable from the internet. |
| `9650` | AvalancheGo HTTP RPC (VM at `/ext/bc/<blockchainID>/rpc`) | **Never public.** Bind to `127.0.0.1` (as above); use a protected reverse proxy if RPC must be served externally. |
| `8232` | Zebra JSON-RPC (not part of this image) | **Never public.** Only the container needs it. |

---

## Environment variables

### Zcash (VM) settings

| Variable | Required? | Default | Description |
|---|---|---|---|
| `ZCASH_RPC_ENDPOINT` | **Yes** | `http://127.0.0.1:8232` (the container — almost never right) | Zcash JSON-RPC URL |
| `ZCASH_RPC_USER` | Only if your Zcash RPC requires auth | *(empty)* | RPC username. If empty, no auth header is sent. Not needed for the Zebra setup in this README |
| `ZCASH_RPC_PASSWORD` | Only if your Zcash RPC requires auth | *(empty)* | RPC password. Not needed for the Zebra setup in this README |
| `ZCASH_CONFIRMATION_DEPTH` | Optional | `24` | Heights within this many blocks of the Zcash tip are refused. Keep the default — all validators should match |

### AvalancheGo / node settings

| Variable | Required? | Default | Description |
|---|---|---|---|
| `AVAGO_NETWORK_ID` | Optional | Baked into the tag — `fuji` for `*-fuji`, `mainnet` otherwise | `fuji`, `mainnet`, or a numeric local ID |
| `AVAGO_TRACK_SUBNETS` | Optional | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` | Subnet ID(s) to track, comma-separated |
| `REDBRIDGE_CHAIN_ID` | Optional | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` | Chain whose VM `config.json` the entrypoint writes |
| `AVAGO_HTTP_PORT` | Optional | `9650` | RPC port |
| `AVAGO_STAKING_PORT` | Optional | `9651` | P2P/staking port |
| `AVAGO_HTTP_HOST` | Optional | `0.0.0.0` | Leave as is — restrict access on the host with `-p 127.0.0.1:...` instead |
| `AVAGO_PLUGIN_DIR` | Optional | `/root/.avalanchego/plugins` | Where the entrypoint installs the VM. Set via `-e`, not a `--plugin-dir` CLI arg |

**Any other AvalancheGo flag** can be set the same way: `--flag-name` →
`AVAGO_FLAG_NAME` (e.g. `--log-level=debug` → `-e AVAGO_LOG_LEVEL=debug`). See the
<a href="https://docs.avax.network/nodes/configure/configs-flags" target="_blank" rel="noopener noreferrer">AvalancheGo config reference</a>.

On start, the entrypoint writes the `ZCASH_*` values to
`$AVAGO_DATA_DIR/configs/chains/$REDBRIDGE_CHAIN_ID/config.json`. To manage that file
yourself, set `-e REDBRIDGE_CHAIN_ID=` (empty) and mount your own `config.json`.

**Switching networks:** the default `AVAGO_TRACK_SUBNETS`/`REDBRIDGE_CHAIN_ID` are the
**Fuji** IDs and don't exist on mainnet. When `redbridge-oracle` is on mainnet,
override `AVAGO_NETWORK_ID`, `AVAGO_TRACK_SUBNETS` and `REDBRIDGE_CHAIN_ID` together and
use a **fresh volume** — a Fuji data directory can't be reused for mainnet.

---

## Persistence

`/root/.avalanchego` (inside the container) holds everything the node needs to
keep:

| Path under `/root/.avalanchego` | What it is | Lose it and… |
|---|---|---|
| `staking/` (`staker.key`, `staker.crt`, `signer.key`) | Your **NodeID** and BLS key | You get a new NodeID and must re-register as a validator |
| `db/` | Primary Network + `redbridge-oracle` chain databases | The node re-syncs from scratch (hours) |
| `configs/chains/<chainID>/config.json` | VM config, written by the entrypoint on every start | Nothing — it is regenerated |
| `plugins/<VM_ID>` | The VM binary, copied in by the entrypoint on every start | Nothing — it is reinstalled |
| `logs/` | AvalancheGo and chain logs | Only old logs |

If you run the container **without** `-v`, Docker still creates an anonymous
volume, but it's easy to lose (`sudo docker rm -v`, `sudo docker volume prune`) —
so always mount one of the two options below.

### Option 1 — named volume `redbridge-data` (default in this README)

```bash
-v redbridge-data:/root/.avalanchego
```

- Docker creates the volume on the first `docker run` and keeps it after the
  container is stopped or removed.
- The data lives in Docker's own storage area, by default
  **`/var/lib/docker/volumes/redbridge-data/_data`** on the host. Check the real
  path with:

  ```bash
  sudo docker volume inspect redbridge-data --format '{{ .Mountpoint }}'
  ```

- `/var/lib/docker` is usually on the **OS disk**. The node database grows to
  hundreds of GB, so if your large disk is mounted elsewhere (your
  `$STORAGE_DIR`), use Option 2 or move Docker's `data-root` to that disk.
- ⚠️ `sudo docker volume rm redbridge-data` (or `sudo docker volume prune`)
  **deletes your staking keys**. Back them up first (see below).

### Option 2 — a host directory (bind mount)

Create the directory:

```bash
sudo mkdir -p $STORAGE_DIR/.avalanchego-node1
```

Then use this `-v` flag in `docker run`:

```bash
-v $STORAGE_DIR/.avalanchego-node1:/root/.avalanchego
```

- You choose where the data lives, so it's easy to put it on the data disk, back
  it up, or inspect it with normal tools.
- The container runs as root, so files it creates are owned by `root`.

### What if the folder or volume already has data?

The image **never wipes or overwrites** existing data. On start it only
(re)installs `plugins/<VM_ID>` and rewrites
`configs/chains/<REDBRIDGE_CHAIN_ID>/config.json`; everything else — keys, databases,
other plugins, other chain configs — is used as it is.

| What's already there | What happens | What to do |
|---|---|---|
| Nothing (new volume / empty directory) | AvalancheGo generates **new** staking keys (new NodeID) and syncs from scratch. | Nothing — this is a fresh node. Back up the new keys. |
| Data from a previous `redbridge-oracle` container | Same NodeID, sync continues where it stopped. This is how upgrades work. | Re-run with the **same** `-v` (see *Upgrading* below). |
| An existing AvalancheGo node's data dir (e.g. `~/.avalanchego` from a systemd install) | Same NodeID, keys and DB; the node starts tracking the `redbridge-oracle` subnet too. | Follow [Using an existing node](#using-an-existing-node) — stop the old node first. |
| Only the staking keys of an existing node (you want a fresh DB) | — | Copy just `staking/` into an empty directory and mount that: `sudo mkdir -p $STORAGE_DIR/.avalanchego-node1 && sudo cp -a ~/.avalanchego/staking $STORAGE_DIR/.avalanchego-node1/` |
| Data you don't want to reuse | It *will* be reused if you mount it. | Mount a **different** volume name or directory. Don't delete the old one until its `staking/` keys are backed up — they may belong to a registered validator. |

Rules that apply whatever you mount:

- **Never** let two AvalancheGo processes open the same data directory at once
  (e.g. the systemd service and this container) — it corrupts the database.
- **Never** run two nodes with the same `staking/` keys at the same time — they
  share one NodeID and fight on the network.
- Use the same network (`fuji`) the data was created for. Fuji data can't be
  reused for mainnet.

### Upgrading the image

Remove the container (not the volume) and start the new tag with the same `-v`.

**1. Pull the new image:**

```bash
sudo docker pull reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

**2. Stop and remove the old container** (no `-v`: the data is kept):

```bash
sudo docker stop redbridge-oracle && sudo docker rm redbridge-oracle
```

**3. Start it again** with your original `sudo docker run ...` command — same
`-v` — using the new tag.

The new VM plugin is installed automatically on start.

### Moving from the named volume to a host directory

**1. Stop the node:**

```bash
sudo docker stop redbridge-oracle
```

**2. Create the new directory:**

```bash
sudo mkdir -p $STORAGE_DIR/.avalanchego-node1
```

**3. Copy the data across:**

```bash
sudo docker run --rm \
  -v redbridge-data:/from:ro \
  -v $STORAGE_DIR/.avalanchego-node1:/to \
  alpine cp -a /from/. /to/
```

**4. Remove the old container:**

```bash
sudo docker rm redbridge-oracle
```

**5. Start again** with `-v $STORAGE_DIR/.avalanchego-node1:/root/.avalanchego`.

Keep the old volume until the node runs fine from the new path, then remove it:

```bash
sudo docker volume rm redbridge-data
```

### Back up the staking keys

**Back up the staking keys** (`staker.key`, `staker.crt` and `signer.key`) and
keep them off the server — losing them means losing your NodeID and
re-registering as a validator:

```bash
sudo docker cp redbridge-oracle:/root/.avalanchego/staking ./redbridge-staking-backup
```

### Using an existing node

To make an AvalancheGo node you already run (same NodeID) validate
`redbridge-oracle`, bind-mount its data directory instead of a named volume. The
container keeps its NodeID, staking keys and DB; the entrypoint adds the
`redbridge-oracle` plugin and leaves other plugins (e.g. Subnet-EVM) alone.

**1. Stop the existing node** — two processes must never open the same DB:

```bash
sudo systemctl stop avalanchego
```

**2. Run the container on the same data directory.** Use the same network, and
keep any subnets the node already tracks, adding the `redbridge-oracle` subnet ID:

```bash
sudo docker run -d --name redbridge-oracle \
  --add-host=host.docker.internal:host-gateway \
  -e AVAGO_TRACK_SUBNETS=<subnets-already-tracked>,EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj \
  -e ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232 \
  -p 9651:9651 \
  -p 127.0.0.1:9650:9650 \
  -v /home/avalanche/.avalanchego:/root/.avalanchego \
  --restart unless-stopped \
  reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

**3. Stop the host service from starting again** and fighting over the DB:

```bash
sudo systemctl disable avalanchego
```

Notes:
- If the node tracks no other subnets, use just
  `-e AVAGO_TRACK_SUBNETS=EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`.
- If your Zcash RPC requires auth, add `ZCASH_RPC_USER` / `ZCASH_RPC_PASSWORD` —
  see [Zcash (VM) settings](#zcash-vm-settings).
- The entrypoint rewrites `configs/chains/<REDBRIDGE_CHAIN_ID>/config.json` on every start.
- Settings the old node set via CLI flags or `--config-file` (public IP, ports, …)
  aren't carried over — pass them as `-e AVAGO_*`.
- The container runs as root, so new files are root-owned. Before going back to
  the host service, run `sudo chown -R avalanche:avalanche /home/avalanche/.avalanchego`.

---

## Verify it's running

**Logs** (press **Ctrl-C** to stop watching — the node keeps running):

```bash
sudo docker logs -f redbridge-oracle
```

**Bootstrapped?** All four must print `{"isBootstrapped":true}` (the first sync
can take hours):

```bash
for chain in P X C 8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq; do
  printf '%-52s ' "$chain"
  curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"}}" \
    -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq -c '.result // .error'
done
```

**`redbridge-oracle` chain serving VM RPC** — returns block data, or
`{"status":"not_found"}` / `{"status":"pending"}`:

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{}}' \
  -H 'content-type:application/json' \
  http://127.0.0.1:9650/ext/bc/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/rpc | jq
```

---

## Join as a validator

Once all four chains report `isBootstrapped: true` and the VM RPC responds,
register the node as a validator of the `redbridge-oracle` L1.

> **💰 You need AVAX on the P-Chain.** Registering a validator is a P-Chain
> transaction, so the wallet that submits it must hold enough **P-Chain** AVAX to
> pay the transaction fee **and** fund the validator's balance. An L1 validator
> pays a continuous fee from that balance; if it runs out, the validator becomes
> inactive and stops earning uptime. AVAX on the C-Chain doesn't count — transfer
> it from C-Chain to P-Chain first (on Fuji, use test AVAX from the faucet).

> **🪙 You need 100,000 RBR tokens.** Joining the redbridge-oracle L1 as a
> validator requires **100,000 RBR** (the redbridge-oracle token). To get RBR,
> please contact the **redbridge team** before you register.

**1. Get the node's ID and BLS key:**

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq
```

Note `nodeID`, `nodePOP.publicKey` and `nodePOP.proofOfPossession`.

**2. Register** in the Avalanche Builder Console —
<a href="https://build.avax.network/console/add-validator/" target="_blank" rel="noopener noreferrer">build.avax.network/console/add-validator</a> — with:

| Field | Value |
|---|---|
| Network | Fuji |
| **Subnet ID** | **`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`** |
| Node ID | `nodeID` from step 1 |
| BLS public key / PoP | `nodePOP.publicKey` / `nodePOP.proofOfPossession` |

Anyone can register a validator — no approval from the L1 owner is needed, as
long as you have the 100,000 RBR tokens and enough P-Chain AVAX (see above).

**3. Confirm** your `nodeID` is listed:

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getCurrentValidators","params":{"subnetID":"EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj"}}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/bc/P | jq
```

---

## Post-install checklist

- [ ] Ubuntu LTS server (26.04 recommended), static public IP
- [ ] Zebra on Zcash Mainnet, fully synced, reachable from inside the container
- [ ] `info.isBootstrapped` is `true` for `P`, `X`, `C` and the `redbridge-oracle` chain
- [ ] `zavax.getBlock` responds
- [ ] Staking keys (`staker.key` + `signer.key`) backed up off the server
- [ ] Port `9651` **is** reachable from the internet; `9650` and `8232` are **not**
- [ ] `AVAGO_TRACK_SUBNETS` includes `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`
- [ ] Enough AVAX on the **P-Chain** for the registration fee and validator balance
- [ ] 100,000 RBR tokens (contact the redbridge team)
- [ ] Node registered as a validator on subnet `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`

---

## More

- [EASY_DEPLOYMENT.md](../EASY_DEPLOYMENT.md) — step-by-step, beginner-friendly setup with default locations and troubleshooting
- [DEPLOYMENT.md](../DEPLOYMENT.md) — building the image from source, compose with `.env`, manual (non-Docker) install, every flag override

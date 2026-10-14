# Easy Deployment — Run a redbridge-oracle Validator Node with Docker

This is the **shortest path** to a working `redbridge-oracle` validator node. You
don't need to clone the code, build anything, or edit config files — you install
Docker, start a Zcash node and the ready-made `redbridge-oracle` image, using the
**default settings and locations** below. Copy each box, paste it into your
terminal, and move on to the next.

> **Who this guide is for:** anyone who can log in to a Linux server and paste
> commands into a terminal. Every step says what it does and what you should see.
> If you need more control (mainnet, a non-Docker install, custom AvalancheGo
> flags), use the full [DEPLOYMENT.md](DEPLOYMENT.md) or the image reference
> [docker/README.md](docker/README.md) instead.

---

## What you'll end up with

One server running two Docker containers side by side:

```
┌────────────────────────── Your server ───────────────────────────┐
│                                                                  │
│   Docker container "zebra"          Docker container             │
│   Zcash node — keeps a    ◀──────   "redbridge-oracle"           │
│   copy of the Zcash       reads      AvalancheGo + the           │
│   blockchain              blocks     redbridge-oracle VM         │
│   (port 8233 public,                 (port 9651 public,          │
│    port 8232 private)                 port 9650 private)         │
└──────────────────────────────────────────────────────────────────┘
```

- **AvalancheGo** is the Avalanche node software. It connects to the Avalanche
  **Fuji** test network.
- **The redbridge-oracle VM** is a plugin inside AvalancheGo. It copies Zcash
  block data onto our Avalanche L1 (a separate blockchain on Avalanche).
- **The Zcash node (Zebra)** is how your validator checks, on its own, that Zcash
  block data proposed by other validators is genuine.

### Key values (you'll see these throughout)

| Name | Value | What it's for |
|---|---|---|
| Avalanche network | `fuji` (testnet) | The network the node joins |
| **Subnet (L1) ID** | `EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj` | Identifies the redbridge-oracle L1 — needed when you **join as a validator** |
| Blockchain ID | `8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq` | Identifies the redbridge-oracle chain — used in RPC URLs |
| VM ID | `sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT` | Identifies the VM plugin (handled for you by the image) |
| Zcash network | Zcash **Mainnet** | The Zcash chain your Zcash node must follow |
| Docker image | `reddev2020/redbridge-oracle-node:v1.0.0-fuji` | The image you'll run |

All of these are already built into the image. The only time you need to enter
them yourself is when joining as a validator in Step 6.

### Default configuration and locations

This guide uses these defaults. Everything big lives on the data disk, mounted at
**`/mnt/blockstorage`**.

| What | Default |
|---|---|
| Data disk (`$STORAGE_DIR`) | `/mnt/blockstorage` |
| Zebra chain data | `/mnt/blockstorage/.zebra/data` |
| Zebra config file | `/mnt/blockstorage/zebra/config/zebrad.toml` |
| redbridge-oracle node data (database + **validator keys**) | `/mnt/blockstorage/.avalanchego-node1` |
| Staking key backup | `~/redbridge-staking-backup` (then copy it off the server) |
| Zebra container name | `zebra` |
| redbridge-oracle container name | `redbridge-oracle` |
| Zcash RPC (as seen by the redbridge-oracle container) | `http://host.docker.internal:8232` — no username/password |
| Public ports (open to the internet) | `9651/tcp` (Avalanche P2P), `8233/tcp` (Zcash P2P) |
| Private ports (this server only) | `9650` (Avalanche RPC), `8232` (Zcash RPC) |

> Your data disk is mounted somewhere else? Use your path in Step 1.3 — every
> later command uses `$STORAGE_DIR`, so nothing else changes.

---

## Before you start — checklist

| # | You need | Details |
|---|---|---|
| 1 | **A server** | Minimum 8 vCPU / 16 GB RAM / 1 TB NVMe SSD. Recommended 16 vCPU / 32 GB RAM / 2 TB NVMe SSD. x86_64 (Intel/AMD). |
| 2 | **Operating system** | **Ubuntu 26.04 LTS (recommended — latest LTS).** Ubuntu 24.04 LTS and 22.04 LTS also work. |
| 3 | **A static public IP** | Other validators must be able to reach you at the same address every time. |
| 4 | **A data disk** | Mounted at `/mnt/blockstorage` (or your own path), with well over 1 TB free. |
| 5 | **Open firewall ports** | **9651/tcp** and **8233/tcp** inbound from the internet — in your cloud provider's firewall / security group too. |
| 6 | **Time** | Zebra's first Zcash sync can take a day or more; Avalanche Fuji sync several hours. |
| 7 | **AVAX on the P-Chain** | Needed to register as a validator — see Step 6. |
| 8 | **100,000 RBR tokens** | Needed to join as a validator — contact the redbridge team. See Step 6. |

---

## Step 1: Prepare the server

### 1.1 Update the system and install tools

`curl` makes the API calls in this guide and `jq` prints their answers readably:

```bash
sudo apt update && sudo apt upgrade -y
```

```bash
sudo apt install -y curl jq
```

### 1.2 Install Docker

Install Docker (official install script from docker.com):

```bash
curl -fsSL https://get.docker.com | sudo sh
```

Start Docker now and on every boot:

```bash
sudo systemctl enable --now docker
```

Check Docker works:

```bash
sudo docker --version
```

Expected: `Docker version 2x.x.x, build ...`

All `docker` commands in this guide are run with `sudo`.

### 1.3 Set the data disk location

```bash
export STORAGE_DIR=/mnt/blockstorage
```

Keep it for future terminals too:

```bash
echo "export STORAGE_DIR=$STORAGE_DIR" >> ~/.bashrc
```

Check the disk is mounted and has space:

```bash
df -h "$STORAGE_DIR"
```

> If you open a new terminal before adding the `~/.bashrc` line, run the
> `export` again — otherwise `$STORAGE_DIR` is empty and data lands on the OS disk.

### 1.4 Open the firewall (if you use `ufw`)

```bash
sudo ufw allow 9651/tcp
```

```bash
sudo ufw allow 8233/tcp
```

Let the redbridge-oracle container reach Zebra's RPC over the private Docker
bridge (this does **not** expose 8232 to the internet). Without it, `ufw` silently
drops container→host traffic and the node fails with `context deadline exceeded`
when it calls Zebra:

```bash
sudo ufw allow in on docker0 to 172.17.0.1 port 8232 proto tcp
```

Also open 9651 and 8233 in your cloud provider's firewall / security group.
**Don't** open 9650 or 8232 there.

---

## Step 2: Set up the Zcash node (Zebra)

### Why is this needed?

Every validator double-checks every block. When another validator proposes
"Zcash block #1000 has hash X", your node asks **its own** Zcash node for
block #1000 and compares. If your node can't reach a Zcash node, it rejects
every block and **cannot validate** — even though AvalancheGo still starts and
looks healthy. This is the most common setup mistake.

This guide runs Zebra (the Zcash Foundation's node software) in Docker on the same server
— the simplest and most reliable option. It needs about **300 GB** of disk.

### 2.1 Create Zebra's folders

The Zebra image runs as user ID `10001`, so that user must own its folders:

```bash
sudo mkdir -p $STORAGE_DIR/.zebra/data $STORAGE_DIR/zebra/config
```

```bash
sudo chown -R 10001:10001 $STORAGE_DIR/.zebra $STORAGE_DIR/zebra
```

### 2.2 Write the Zebra config

This writes `$STORAGE_DIR/zebra/config/zebrad.toml` with the default settings:

```bash
sudo tee $STORAGE_DIR/zebra/config/zebrad.toml > /dev/null <<'TOML'
[network]
network = "Mainnet"
listen_addr = "0.0.0.0:8233"            # Zcash P2P — open 8233/tcp to the internet

[state]
cache_dir = "/var/cache/zebrad-cache"   # matches the data volume mount below

[rpc]
# Listen on all interfaces *inside the Zebra container*; who can reach it from
# outside is decided by the -p mapping in step 2.3.
listen_addr = "0.0.0.0:8232"
# Zebra 2.x turns on cookie authentication by default. The redbridge-oracle VM
# can't use Zebra's cookie file, so turn it off. Port 8232 is only published on
# the private Docker bridge, so it is never reachable from the internet.
enable_cookie_auth = false
TOML
```

```bash
sudo chown 10001:10001 $STORAGE_DIR/zebra/config/zebrad.toml
```

### 2.3 Start Zebra

```bash
sudo docker run -d --name zebra \
  -p 8233:8233 \
  -p 172.17.0.1:8232:8232 \
  -v $STORAGE_DIR/.zebra/data:/var/cache/zebrad-cache \
  -v $STORAGE_DIR/zebra/config:/home/zebra/.config \
  --restart unless-stopped \
  zfnd/zebra:latest
```

| Line | Meaning |
|---|---|
| `-p 8233:8233` | Zcash P2P — public, so Zebra can find peers and sync. |
| `-p 172.17.0.1:8232:8232` | Zcash RPC, published **only on the private Docker bridge**. The redbridge-oracle container can reach it; the internet can't. |
| `-v .../.zebra/data:...` | Zcash chain data — kept across restarts and upgrades. |
| `-v .../zebra/config:...` | Makes Zebra load the config from step 2.2. |

> ⚠️ **Don't change it to a plain `-p 8232:8232`.** That would publish the RPC
> to the internet — Docker's port rules bypass `ufw`. If your Docker bridge
> isn't `172.17.0.1` (check with `ip -4 addr show docker0`), use that address.

### 2.4 Wait for Zebra to sync

A first sync takes many hours to a couple of days. Watch progress — look for
`sync_percent` reaching ~100% (press **Ctrl-C** to stop watching; Zebra keeps
running):

```bash
sudo docker logs -f zebra
```

### 2.5 Check Zebra's RPC

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

`result` is the latest Zcash block your node knows about. Compare it with a public
explorer such as <a href="https://mainnet.zcashexplorer.app/" target="_blank" rel="noopener noreferrer">mainnet.zcashexplorer.app</a>
— once synced, they should be (nearly) equal.

<details>
<summary>Other ways to provide a Zcash node</summary>

| Option | What it means | Recommended? |
|---|---|---|
| **A. Zebra on the same server** (this guide) | Zebra in Docker, as above. | ✅ **Yes — simplest and most reliable.** |
| **B. Zebra on another server you control** | Point the validator at `http://<that-server-private-ip>:8232` over a private network. | ✅ Fine — keep port 8232 private (firewall/VPN). |
| **C. zcashd instead of Zebra** | The older Zcash node software. The VM only uses standard calls (`getblockcount`, `getblockhash`, `getblock`), so it works. | ⚠️ Works, but zcashd is being retired — prefer Zebra. |
| **D. Someone else's public Zcash RPC** | A third-party Zcash API. | ❌ Not recommended — you'd be trusting their data instead of checking it yourself. |
| **E. mock-zcashd** | The fake Zcash server in this repo. | ❌ **Local testing only.** A validator using it will reject all real blocks. |

Zebra documentation: <a href="https://zebra.zfnd.org/" target="_blank" rel="noopener noreferrer">zebra.zfnd.org</a>

</details>

---

## Step 3: Start the redbridge-oracle node

### 3.1 Create the node's data folder

This folder holds the blockchain database **and your validator identity keys**:

```bash
sudo mkdir -p $STORAGE_DIR/.avalanchego-node1
```

### 3.2 Download the image

```bash
sudo docker pull reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

### 3.3 Start the node

```bash
sudo docker run -d --name redbridge-oracle \
  --add-host=host.docker.internal:host-gateway \
  -e ZCASH_RPC_ENDPOINT=http://host.docker.internal:8232 \
  -p 9651:9651 \
  -p 127.0.0.1:9650:9650 \
  -v $STORAGE_DIR/.avalanchego-node1:/root/.avalanchego \
  --restart unless-stopped \
  reddev2020/redbridge-oracle-node:v1.0.0-fuji
```

### What each line means

| Line | Meaning |
|---|---|
| `sudo docker run -d` | Start the container in the background. |
| `--name redbridge-oracle` | Name the container so you can refer to it later. |
| `--add-host=host.docker.internal:host-gateway` | Lets the container reach Zebra on this server at the name `host.docker.internal`. |
| `-e ZCASH_RPC_ENDPOINT=...` | Where your Zcash node is (Step 2). |
| `-p 9651:9651` | Opens the **staking/P2P** port to the internet. Other validators talk to you here. **Must be public.** |
| `-p 127.0.0.1:9650:9650` | Opens the **RPC** port to this server only. **Must stay private** — never expose it to the internet. To serve RPC publicly, put a protected reverse proxy in front (see [`security/deployment-notes.md`](../security/deployment-notes.md)). |
| `-v $STORAGE_DIR/.avalanchego-node1:/root/.avalanchego` | Stores everything (blockchain data and **your validator identity keys**) on the data disk, so nothing is lost when the container restarts or is upgraded. |
| `--restart unless-stopped` | Start automatically after a crash or server reboot. |
| `reddev2020/redbridge-oracle-node:v1.0.0-fuji` | The image to run. The `-fuji` tag means "join the Fuji testnet". |

Everything else (network, Subnet ID, Blockchain ID, VM plugin) is built into the
image.

### Zcash settings the node accepts

The command above already has everything needed for the default Zebra setup. The
full list, for reference:

| Setting | Required? | What to put |
|---|---|---|
| `ZCASH_RPC_ENDPOINT` | **Required** | The address of your Zcash node. `http://host.docker.internal:8232` for Zebra on this server; `http://<that-server-private-ip>:8232` for Zebra on another server (then drop `--add-host`). |
| `ZCASH_RPC_USER` | Only if your Zcash node requires a username | Not needed for the default Zebra setup. Add as `-e ZCASH_RPC_USER=...` |
| `ZCASH_RPC_PASSWORD` | Only if your Zcash node requires a password | Not needed for the default Zebra setup. Add as `-e ZCASH_RPC_PASSWORD=...` |
| `ZCASH_CONFIRMATION_DEPTH` | Optional (default `24`) | How many newer Zcash blocks must exist before a block can be bridged. **Leave it at 24** — all validators should use the same value. |

> ⚠️ **Don't use `http://127.0.0.1:8232`.** Inside a Docker container,
> `127.0.0.1` means "the container itself", not your server — the node will not
> find Zebra.

> **About the mainnet image:** an image tagged `v1.0.0` (without `-fuji`) also
> exists, but redbridge-oracle is **not yet deployed on Avalanche mainnet**, so
> that image is not usable yet. Use the `-fuji` tag.

> **Already running an AvalancheGo node** and want the same node (same NodeID)
> to validate redbridge-oracle? Follow
> [docker/README.md → Using an existing node](docker/README.md#using-an-existing-node)
> instead of the command above.

---

## Step 4: Back up your validator identity

Do this once, right after the first start. Your node's identity is in the
`staking` folder (`staker.key`, `staker.crt`, `signer.key`). **If you lose these
files, you lose your NodeID and have to register as a validator all over again.**

```bash
sudo cp -a $STORAGE_DIR/.avalanchego-node1/staking ~/redbridge-staking-backup
```

Copy the `~/redbridge-staking-backup` folder somewhere safe and **off this
server** (it is a secret — treat it like a password).

---

## Step 5: Check that it's working

### 5.1 Watch the logs

Press **Ctrl-C** to stop watching — the node keeps running.

```bash
sudo docker logs -f redbridge-oracle
```

Near the top you should see:

```
[entrypoint] installed VM plugin -> /root/.avalanchego/plugins/sP6SrkPy8HGHCqRwuxuvCPPUKSxLX5hZtozvNPQxFsLfAZgiT
[entrypoint] wrote VM chain config -> /root/.avalanchego/configs/chains/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/config.json
```

### 5.2 Wait for it to sync ("bootstrap")

On first start the node downloads the Avalanche Fuji blockchain. **This can take
several hours.** Check progress with:

```bash
for chain in P X C 8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq; do
  printf '%-52s ' "$chain"
  curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"}}" \
    -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq -c '.result // .error'
done
```

You're done waiting when **all four lines** show `{"isBootstrapped":true}`:

| Chain | What it is |
|---|---|
| `P` | Platform chain — validators and subnets |
| `X` | Exchange chain |
| `C` | Contract chain (EVM) |
| `8iARaBqk…` | The redbridge-oracle chain |

### 5.3 Ask the redbridge-oracle chain for data

#### Get latest block from the redbridge-oracle
```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{}}' \
  -H 'content-type:application/json' \
  http://127.0.0.1:9650/ext/bc/8iARaBqk63EqdbRu7KLu348JkfyFHgw2JkbfsbMWK2r9JE2Dq/rpc | jq
```

Any of these answers means the chain is working:

- Zcash block data (`"data": { "hash": ..., "height": 1000, ... }`) — this
  height has been bridged.
- `{"status":"not_found"}` or `{"status":"pending"}` — the chain is working;
  that height just hasn't been bridged yet.

---

## Step 6: Join as a validator

Once **all chains are bootstrapped** (Step 5.2), the node can be registered as a
validator of the redbridge-oracle L1.

> **💰 You need AVAX on the P-Chain.** Registering a validator is a P-Chain
> transaction, so the wallet that submits it must hold enough **P-Chain** AVAX to
> pay the transaction fee **and** fund the validator's balance. An L1 validator
> pays a small continuous fee from that balance; if it runs out, the validator
> becomes inactive and stops earning uptime. AVAX on the C-Chain doesn't count —
> transfer it from C-Chain to P-Chain first (on Fuji, use test AVAX from the
> faucet).

> **🪙 You need 100,000 RBR tokens.** Joining the redbridge-oracle L1 as a
> validator requires **100,000 RBR** (the redbridge-oracle token). To get RBR,
> please contact the **redbridge team** before you register.

**1. Get your node's identity:**

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/info | jq
```

This returns:
- `nodeID` — e.g. `NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg`
- `nodePOP.publicKey` and `nodePOP.proofOfPossession` — your BLS key and its proof

**2. Register the node** using the Avalanche Builder Console:
<a href="https://build.avax.network/console/add-validator/" target="_blank" rel="noopener noreferrer">build.avax.network/console/add-validator</a>

Connect your wallet (e.g. Core) with the **Testnet** toggle on. The wallet must
hold the 100,000 RBR on the C-Chain and AVAX on the P-Chain. The console walks
you through four screens:

**2a. Select the L1 subnet**

In **Subnet ID**, enter
**`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`**. The console detects the
validator manager (**PoS · ERC20**) and shows its details on the right: the
staking token, min stake **100,000**, min delegation fee **1%**. Click **Next**.

![Step 2a — Select L1 Subnet](docs/images/join-validator/1-select-subnet.png)

**2b. Initiate validator registration — approve RBR, then initiate**

Fill in the form:

| Field | Value |
|---|---|
| Node ID | `nodeID` from step 1 |
| BLS public key / proof of possession | `nodePOP.publicKey` / `nodePOP.proofOfPossession` from step 1 |
| Validator Balance (P-Chain AVAX) | AVAX that pays the validator's continuous fee (e.g. `0.1` lasts ~2 days at ~1.33 AVAX/month; add more to run longer) |
| Remaining Balance / Deactivation Owner Addresses | Your P-Chain address (pre-filled from the wallet) |
| Stake Amount | `100000` |
| Delegation Fee (Basis Points) | `100` (1%, the minimum) |
| Min Stake Duration (seconds) | `0` |
| Reward Recipient | EVM address that receives staking rewards (defaults to the connected wallet) |

![Step 2b — Initiate Validator Registration form](docs/images/join-validator/2-initiate-registration.png)

Click **Approve** first. Your wallet asks you to approve the staking manager
spending **100,000 RBR** — confirm it:

![Step 2b — Approve 100,000 RBR in Core](docs/images/join-validator/3-approve-rbr-core.png)

Once the button shows **Approved (100000 TRBR)**, click **Initiate Validator
Registration** and confirm that transaction too. When it completes, click
**Next**.

![Step 2b — Approved, then initiate registration](docs/images/join-validator/4-initiate-validator-registration.png)

**2c. P-Chain registration — sign and submit to the P-Chain**

The console fills in the `initiateValidatorRegistration` transaction hash and
extracts the Warp message from it.

> **⚠️ Make sure your P-Chain balance is at least 0.5 AVAX before you continue.**
> This transaction pays the P-Chain fee and funds the validator balance you set
> in 2b. If the balance is too low the console shows *"Insufficient P-Chain
> balance for transaction fees"* — move AVAX from the C-Chain to the P-Chain
> (the **C/P Bridge** tool in the console's left menu) or use the faucet, then
> come back.

Click **Sign & Submit to P-Chain** and confirm in your wallet. The console
collects BLS signatures from the L1 validators and submits the
`RegisterL1ValidatorTx`.

![Step 2c — P-Chain Registration](docs/images/join-validator/5-pchain-registration.png)

**2d. Complete validator registration**

The **P-Chain Transaction ID** from 2c is filled in for you. Click **Complete
Validator Registration** and confirm in your wallet. This calls
`completeValidatorRegistration()` on the staking manager and finishes the
registration.

![Step 2d — Complete Registration](docs/images/join-validator/6-complete-registration.png)

Anyone can register a validator — no approval from the L1 owner is needed, as
long as you have the 100,000 RBR tokens and enough P-Chain AVAX (see above).

**3. Confirm** — after registration, check that your node appears in the
validator list:

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getCurrentValidators","params":{"subnetID":"EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj"}}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/bc/P | jq
```

Your `nodeID` should be in the result.

---

## Step 7: Remove your validator (leave the L1)

Do this when you want to stop validating and get your **100,000 RBR stake**
back. Removal is the same kind of multi-step flow as joining, in the Avalanche
Builder Console:
<a href="https://build.avax.network/console/remove-validator/select-subnet" target="_blank" rel="noopener noreferrer">build.avax.network/console/remove-validator</a>

> **⚠️ Keep the node running until removal is complete.** Stop or delete the
> container only after the last step succeeds — the console collects signatures
> from the L1 validators while you go through the flow.

Connect the **same wallet** you registered with (the owner of the stake), with
the **Testnet** toggle on, then:

1. **Select the L1 subnet** — enter
   **`EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj`**, the same Subnet ID
   as in Step 6. Pick your validator by its `nodeID`.
2. **Initiate validator removal** — confirm the transaction in your wallet. This
   calls the staking manager on the C-Chain to start the removal.
3. **P-Chain update** — sign and submit the P-Chain transaction. As with joining,
   the wallet needs a little **P-Chain** AVAX for the fee.
4. **Complete validator removal** — confirm the final transaction. Your staked
   RBR (and any staking rewards) is returned to your wallet, and any unused
   validator balance on the P-Chain goes back to the **Remaining Balance Owner**
   address you set in Step 6.

**Confirm** — your `nodeID` should no longer appear in the validator list:

```bash
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getCurrentValidators","params":{"subnetID":"EAGjhetxF6gAo3AhYCFuw5e1vJbVY2QMNNHabrubeuBvUjBfj"}}' \
  -H 'content-type:application/json' http://127.0.0.1:9650/ext/bc/P | jq
```

After that you can stop the node (Step 8). Keep your Step 4 key backup if you
might rejoin later with the same NodeID.

---

## Step 8: Everyday tasks

See if it's running:

```bash
sudo docker ps --filter name=redbridge-oracle
```

Watch logs (Ctrl-C to stop watching):

```bash
sudo docker logs -f redbridge-oracle
```

Stop:

```bash
sudo docker stop redbridge-oracle
```

Start again:

```bash
sudo docker start redbridge-oracle
```

Restart:

```bash
sudo docker restart redbridge-oracle
```

Watch Zebra's logs:

```bash
sudo docker logs -f zebra
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `isBootstrapped` stays `false` for hours | Normal on first start — Fuji is still downloading. | Wait; watch `sudo docker logs -f redbridge-oracle`. |
| Logs show `zcash rpc error`, `connection refused`, or `getblockcount` errors | The node can't reach your Zcash node. | Check `ZCASH_RPC_ENDPOINT` (not `127.0.0.1`!), that the `zebra` container is running and synced, and that Zebra's RPC is published on `172.17.0.1:8232` (Step 2.3). |
| Logs show `context deadline exceeded (Client.Timeout exceeded while awaiting headers)` for `host.docker.internal:8232`, then `FATAL ... shutting down chain` | `ufw` is dropping traffic from the container to Zebra on the Docker bridge. `curl` from the host (Step 2.5) still works, which hides the problem. | `sudo ufw allow in on docker0 to 172.17.0.1 port 8232 proto tcp` (Step 1.4), then `sudo docker restart redbridge-oracle`. |
| Logs show `http status 401` | Zebra cookie auth is on, or wrong Zcash username/password. | Set `enable_cookie_auth = false` in Zebra (Step 2.2), or fix `ZCASH_RPC_USER`/`ZCASH_RPC_PASSWORD`. |
| Logs show `-8` / "block height out of range" errors | Zebra is not fully synced yet. | Wait for Zebra to finish syncing (Step 2.4). |
| Node has few or no peers | Port 9651 is blocked. | Open 9651/tcp inbound in your server firewall **and** your cloud provider's firewall / security group. |
| redbridge-oracle chain never bootstraps but P/X/C do | The node can't verify redbridge-oracle blocks — almost always the Zcash node is unreachable or not synced. | Re-check Step 2 and look for Zcash errors in the logs. |
| `permission denied ... docker.sock` | The command was run without `sudo`. | Prefix the `docker` command with `sudo`. |
| `jq: command not found` | `jq` isn't installed. | Run `sudo apt install -y jq` (Step 1.1). |
| Registration fails with an insufficient-funds error | Not enough AVAX on the **P-Chain**. | Transfer AVAX from C-Chain to P-Chain, then retry (Step 6). |

---

## Glossary

| Term | Meaning |
|---|---|
| **AvalancheGo** | The official Avalanche node software. |
| **Fuji** | Avalanche's public test network. |
| **P-Chain** | Avalanche's Platform chain — where validators are registered and their fees are paid. |
| **L1 / Subnet** | An independent blockchain network running on Avalanche, with its own validators. redbridge-oracle is one. |
| **VM (Virtual Machine)** | The program that defines how a chain works. redbridge-oracle's VM copies Zcash block data. |
| **Validator** | A node that votes on which blocks are accepted. |
| **Bootstrapping** | The node's initial download of a blockchain. |
| **Zebra** | The Zcash Foundation's Zcash node software. |
| **RPC** | The node's API — how programs (and `curl`) ask it questions. |
| **NodeID / staking keys** | Your node's permanent identity (`staker.key`, `signer.key`). |

---

## Need more control?

This guide makes every choice for you. When you need something different, use
one of these:

| If you want to… | Read |
|---|---|
| Keep data somewhere else, use a Docker volume, change ports, see every environment variable, upgrade or move your data, or reuse an AvalancheGo node you already run (same NodeID) | [docker/README.md](docker/README.md) — the reference for the Docker image |
| Build the image yourself, install without Docker (systemd), override any AvalancheGo flag, switch networks, or set up a production firewall / RPC proxy | [DEPLOYMENT.md](DEPLOYMENT.md) — the complete deployment guide |

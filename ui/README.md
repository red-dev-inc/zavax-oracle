# Redbridge Oracle UI

A React frontend for querying Zcash block data via the Redbrige oracle. Users connect a MetaMask wallet on Avalanche Fuji Testnet, pay an RBR fee, and retrieve Zcash block information bridged through the redbridge-oracle VM.

## Architecture

```
Browser (React + ethers.js)
  └─► Express proxy server (server.cjs, port 3001)
        └─► Redbridge Oracle VM nodes (JSON-RPC)
```

The Express server keeps VM node URLs server-side (never exposed to the client) and proxies three API routes:

| Route | Purpose |
|---|---|
| `POST /api/block` | Query a Zcash block by height |
| `POST /api/height` | Get the latest VM block height |
| `POST /api/reconcile` | Reconcile / recheck all stored blocks |

## Prerequisites

- Node.js 18+
- MetaMask browser extension
- Avalanche Fuji Testnet configured in MetaMask (the app adds it automatically on first connect)
- RBR token balance on Fuji C-Chain (contract: `0x87F2c3c44B9bBA78995367D7FeA7BFC493c7C7bA`)

## Development

Run the Vite dev server and the Express proxy in two separate terminals:

```bash
# Terminal 1 — Express proxy (port 3001)
npm run server            # Fuji
npm run server:mainnet    # Mainnet

# Terminal 2 — Vite dev server (port 5174, proxies /api → 3001)
npm run dev               # Fuji
npm run dev:mainnet       # Mainnet
```

Run the server and the dev server for the same network.

Open [http://localhost:5174](http://localhost:5174) in your browser.

The Vite config (`vite.config.js`) forwards all `/api/*` requests to `http://localhost:3001`, so CORS is not an issue in development.

## Production Build

```bash
# 1. Install dependencies
npm install

# 2. Build the static assets into dist/<network>/
npm run build:fuji        # or: npm run build:mainnet

# 3. Start the Express server (serves dist/<network>/ + API routes)
npm run server            # or: npm run server:mainnet
```

The server listens on `PORT` (default **3001**) and serves `dist/$NETWORK/` as static files.

```bash
PORT=8080 npm run server   # custom port
```

## Running with PM2

PM2 keeps the server alive across crashes and reboots.

```bash
# Install PM2 globally (once)
npm install -g pm2

# Start the server (Fuji)
NETWORK=fuji pm2 start server.cjs --name redbridge-oracle

# Mainnet, on its own port
NETWORK=mainnet PORT=8082 pm2 start server.cjs --name redbridge-oracle-mainnet

# Save the process list so it survives a reboot
pm2 save
pm2 startup   # follow the printed command to register the init hook
```

Useful PM2 commands:

```bash
pm2 status          # list running processes
pm2 logs redbridge-oracle   # tail logs
pm2 restart redbridge-oracle
pm2 stop redbridge-oracle
pm2 delete redbridge-oracle
```

## Networks (Fuji / Mainnet)

Each network has its own config under `config/<network>/`:

| File | Used by | Contents |
|---|---|---|
| `config/<network>/client.json` | Vite build (bundled into the page — public) | C-Chain ID, chain name, RPC, explorer, RBR token + gateway proxy addresses, VM chain ID |
| `config/<network>/server.json` | `server.cjs` only (never sent to the browser) | P-Chain RPC, subnet ID, validator nodes (`nodeID`, `label`, read-proxy `rpcUrl`) |

- The build picks the network with `vite build --mode fuji|mainnet` and writes to `dist/<network>/`; only that network's `client.json` is bundled.
- The server picks the network with `NETWORK=fuji|mainnet` (default `fuji`) and serves `dist/$NETWORK/`.
- Both refuse to start if a required field is empty, so an unfilled mainnet config fails loudly instead of shipping a broken UI.
- `PCHAIN_RPC` and `SUBNET_ID` env vars still override `server.json`.

CI has a manual `deploy_ui_fuji` and `deploy_ui_mainnet` job, each with its own deploy path, pm2 process and port.

## Key Source Files

| File | Description |
|---|---|
| `src/App.jsx` | Main React component — wallet, fee approval, height query, polling |
| `src/lib/abis.js` | Contract ABIs + the selected network's config |
| `src/lib/vm.js` | Client-side helpers that call the Express proxy routes |
| `server.cjs` | Express proxy server with VM node RPC forwarding |
| `vite.config.js` | Vite config — network selection, React plugin, dev proxy to port 3001 |
| `config/<network>/` | Per-network client + server config (see above) |

## Usage

1. Open the app and click **Connect Wallet** — MetaMask will prompt to connect and switch to the build's network (Fuji or Avalanche C-Chain).
2. If the RBR allowance is insufficient, click **Approve RBR** to approve the gateway contract.
3. Select a redbridge node (Node 1 / 2 / 3), enter a Zcash block height, and click **Submit to red·bridge**.
4. The app waits for the ICM relayer to deliver the warp message and polls the VM (up to 12 × 5 s intervals).
5. The retrieved block data appears in the **Response** panel below.
6. Use **Recheck All Blocks** to trigger a full reconciliation of stored blocks on the selected node.

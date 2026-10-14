import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'node:fs'
import path from 'node:path'

const serverPort = process.env.PORT || 3001

const NETWORKS = ['fuji', 'mainnet']
const REQUIRED = ['chainId', 'chainName', 'cChainRPC', 'blockExplorerUrl', 'feeTokenAddr', 'gatewayAddr', 'vmChainID']

// Network is picked with `--mode fuji|mainnet` (or NETWORK=...), default fuji.
// Only that network's config/<network>/client.json is bundled, and the build
// lands in dist/<network> so server.cjs can never serve the wrong one.
export default defineConfig(({ mode }) => {
  const network = NETWORKS.includes(mode) ? mode : (process.env.NETWORK || 'fuji')
  if (!NETWORKS.includes(network)) throw new Error(`Unknown network "${network}" (expected ${NETWORKS.join(' | ')})`)

  const configPath = path.resolve(__dirname, 'config', network, 'client.json')
  const cfg = JSON.parse(fs.readFileSync(configPath, 'utf8'))
  const missing = REQUIRED.filter(k => cfg[k] === undefined || cfg[k] === '')
  if (missing.length) throw new Error(`${configPath} is missing: ${missing.join(', ')}`)

  return {
    plugins: [react()],
    resolve: {
      alias: { '@network-config': configPath },
    },
    build: {
      outDir: `dist/${network}`,
      emptyOutDir: true,
    },
    server: {
      port: 5174,
      proxy: {
        '/api': `http://localhost:${serverPort}`,
      },
    },
  }
})

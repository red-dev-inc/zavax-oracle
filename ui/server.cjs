const express = require('express')
const https   = require('https')
const axios   = require('axios')
const cors    = require('cors')
const path    = require('path')
const crypto  = require('crypto')
const fs      = require('fs')

// ── network selection ─────────────────────────────────────────────────────────
// NETWORK=fuji|mainnet (default fuji) picks config/<network>/server.json and
// the matching Vite build in dist/<network> (npm run build:<network>).
const NETWORKS = ['fuji', 'mainnet']
const NETWORK  = process.env.NETWORK || 'fuji'
if (!NETWORKS.includes(NETWORK)) {
  console.error(`Unknown NETWORK "${NETWORK}" (expected ${NETWORKS.join(' | ')})`)
  process.exit(1)
}
const SERVER_CONFIG_PATH = path.join(__dirname, 'config', NETWORK, 'server.json')
const serverConfig = JSON.parse(fs.readFileSync(SERVER_CONFIG_PATH, 'utf8'))
const DIST_DIR = path.join(__dirname, 'dist', NETWORK)

// ── node registry (server-side only, never exposed to client) ─────────────────
// The only piece that still has to be told by an operator: which real
// Avalanche NodeID corresponds to which of our own read-side RPC proxies.
// P-Chain has no notion of "node1/node2/node3" or our proxy hostnames, so
// this mapping can't be derived automatically — it's infrastructure
// ownership, not chain state. Everything else about a node (whether it's
// still an active validator, its live validationID, its stake weight) is
// fetched fresh from P-Chain in /api/validators below, never hand-copied.
const NODE_URLS = {}
const NODE_LABELS = {}
for (const n of serverConfig.nodes || []) {
  NODE_URLS[n.id] = n.rpcUrl
  NODE_LABELS[n.nodeID] = { id: n.id, label: n.label }
}

const PCHAIN_RPC = process.env.PCHAIN_RPC || serverConfig.pchainRpc
const SUBNET_ID  = process.env.SUBNET_ID  || serverConfig.subnetId

const missing = [
  !PCHAIN_RPC && 'pchainRpc',
  !SUBNET_ID && 'subnetId',
  !Object.keys(NODE_URLS).length && 'nodes',
].filter(Boolean)
if (missing.length) {
  console.error(`${SERVER_CONFIG_PATH} is missing: ${missing.join(', ')}`)
  process.exit(1)
}
// Read proxy used for validators that have none of their own (e.g. a node an
// outside operator just registered): the last node listed in server.json.
// Every validator holds the same accepted chain, so any of our nodes can
// answer reads for requests routed to it.
const DEFAULT_READ_ID = serverConfig.nodes[serverConfig.nodes.length - 1].id
const VALIDATORS_CACHE_TTL_MS = 30_000

const httpsAgent = new https.Agent({ rejectUnauthorized: false })

const DEBUG = process.env.DEBUG === '1' || process.env.DEBUG === 'true'

function log(...args) {
  if (DEBUG) console.debug('[debug]', new Date().toISOString(), ...args)
}

function nodeUrl(nodeId) {
  const url = NODE_URLS[Number(nodeId)]
  if (!url) throw new Error('Invalid node ID')
  return url
}

async function rpc(nodeId, method, params = {}) {
  const url = nodeUrl(nodeId)
  const body = { jsonrpc: '2.0', id: 1, method, params }

  log('→ request', {
    nodeId,
    url,
    method,
    params,
    tlsSkipVerify: true,
  })

  const t0 = Date.now()
  let response
  try {
    response = await axios.post(url, body, { httpsAgent })
  } catch (e) {
    console.error('[error]', new Date().toISOString(), '✗', method, `(node ${nodeId})`, e.message, {
      url,
      code: e.code,
      status: e.response?.status,
      data: e.response?.data,
    })
    throw e
  }

  log('← response', {
    nodeId,
    url,
    method,
    status: response.status,
    latencyMs: Date.now() - t0,
    data: response.data,
  })

  return response.data
}

// ── CB58 (Avalanche's base58+checksum encoding for IDs like validationID) ──────
const B58_ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'

function base58Decode(str) {
  let num = 0n
  for (const char of str) {
    const idx = B58_ALPHABET.indexOf(char)
    if (idx === -1) throw new Error(`invalid base58 character: ${char}`)
    num = num * 58n + BigInt(idx)
  }
  let hex = num.toString(16)
  if (hex.length % 2) hex = '0' + hex
  let bytes = hex === '0' ? Buffer.alloc(0) : Buffer.from(hex, 'hex')
  let leadingZeros = 0
  for (const char of str) {
    if (char !== '1') break
    leadingZeros++
  }
  if (leadingZeros > 0) bytes = Buffer.concat([Buffer.alloc(leadingZeros), bytes])
  return bytes
}

// cb58Decode mirrors avalanchego's ids.FromString: base58-decode, split off the
// trailing 4-byte checksum (= last 4 bytes of SHA256(payload)), verify it, and
// return the payload (32 bytes for a validationID).
function cb58Decode(str) {
  const decoded = base58Decode(str)
  if (decoded.length < 5) throw new Error('cb58: too short')
  const payload = decoded.subarray(0, decoded.length - 4)
  const checksum = decoded.subarray(decoded.length - 4)
  const expected = crypto.createHash('sha256').update(payload).digest().subarray(-4)
  if (!checksum.equals(expected)) throw new Error('cb58: checksum mismatch')
  return payload
}

// ── P-Chain validator lookup ────────────────────────────────────────────────
let validatorsCache = { at: 0, data: null }

async function fetchActiveValidators() {
  const now = Date.now()
  if (validatorsCache.data && now - validatorsCache.at < VALIDATORS_CACHE_TTL_MS) {
    return validatorsCache.data
  }

  const { data } = await axios.post(PCHAIN_RPC, {
    jsonrpc: '2.0',
    id: 1,
    method: 'platform.getCurrentValidators',
    params: { subnetID: SUBNET_ID },
  })
  if (data.error) throw new Error(data.error.message || 'platform.getCurrentValidators failed')

  const validators = (data.result?.validators || [])
    // Skip validators whose continuous-fee balance is exhausted — they're
    // registered but not actually active/serving right now.
    .filter(v => v.balance !== '0')
    .map(v => {
      const known = NODE_URLS[NODE_LABELS[v.nodeID]?.id] ? NODE_LABELS[v.nodeID] : null
      return {
        nodeID: v.nodeID,
        validationId: '0x' + cb58Decode(v.validationID).toString('hex'),
        weight: v.weight,
        // Validators without their own read proxy still get listed; their
        // results are read from DEFAULT_READ_ID instead.
        id: known ? known.id : DEFAULT_READ_ID,
        label: known?.label || v.nodeID,
        known: !!known,
      }
    })
    // Our labelled nodes first (by id), then everyone else by NodeID.
    .sort((a, b) =>
      (b.known - a.known) || (a.known ? a.id - b.id : a.nodeID.localeCompare(b.nodeID)))
    .map(({ known, ...v }) => v)

  validatorsCache = { at: now, data: validators }
  return validators
}

// ── app ───────────────────────────────────────────────────────────────────────
const app = express()
app.use(cors())
app.use(express.json())

// live active-validator list for this subnet, with real validationIDs
app.get('/api/validators', async (_req, res) => {
  try {
    res.json({ validators: await fetchActiveValidators() })
  } catch (e) {
    console.error('/api/validators', e.message)
    res.status(500).json({ error: e.message })
  }
})

// query a block by Zcash height
app.post('/api/block', async (req, res) => {
  const { nodeId, height } = req.body
  try {
    const data = await rpc(nodeId, 'zavax.getBlockByHeight', { height: Number(height) })
    res.json(data)
  } catch (e) {
    console.error('/api/block', e.message)
    res.status(500).json({ error: e.message })
  }
})

// latest VM block height
app.post('/api/height', async (req, res) => {
  const { nodeId } = req.body
  try {
    const data = await rpc(nodeId, 'zavax.getBlock', {})
    res.json(data)
  } catch (e) {
    console.error('/api/height', e.message)
    res.status(500).json({ error: e.message })
  }
})

// reconcile / recheck all blocks
app.post('/api/reconcile', async (req, res) => {
  const { nodeId } = req.body
  try {
    const data = await rpc(nodeId, 'zavax.reconcileBlocks', {})
    res.json(data)
  } catch (e) {
    console.error('/api/reconcile', e.message)
    res.status(500).json({ error: e.message })
  }
})

// serve Vite production build
app.use(express.static(DIST_DIR))
app.get('/{*path}', (_req, res) =>
  res.sendFile(path.join(DIST_DIR, 'index.html')),
)

const port = process.env.PORT || 3001
app.listen(port, () => console.log(`Redbridge Oracle server (${NETWORK}) running on port ${port}`))

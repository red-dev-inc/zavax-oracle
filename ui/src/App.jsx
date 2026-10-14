import { useState, useEffect, useCallback, useRef } from 'react'
import { ethers } from 'ethers'
import { FEE_TOKEN_ABI, GATEWAY_ABI, DEFAULT_CONFIG } from './lib/abis'
import { queryBlockByHeight, reconcileBlocks, getLatestHeight, fetchValidators } from './lib/vm'
import logo from './assets/logo.png'
import logoDark from './assets/logo_dark.png'
import logoWhite from './assets/logo_white.png'
import shape38Light from './assets/themes/light/parth_2_light.svg'
import shape32Light from './assets/themes/light/path_1_light.svg'
import shape38Dark from './assets/themes/dark/path_1_dark.svg'
import shape32Dark from './assets/themes/dark/path_2_dark.svg'
import shape38Red from './assets/themes/red/path_1_red.svg'
import shape32Red from './assets/themes/red/path_2_red.svg'
import shape38Zebra from './assets/themes/zebra/path_1_zebra.svg'
import shape32Zebra from './assets/themes/zebra/path_2_zebra.svg'
import circleLight from './assets/themes/light/circle_light.svg'
import circleDark from './assets/themes/dark/circle_dark.svg'
import circleRed from './assets/themes/red/circle_red.svg'
import circleZebra from './assets/themes/zebra/circle_zebra.svg'
import tooltipLight from './assets/themes/light/tooltip_light.svg'
import tooltipDark from './assets/themes/dark/tooltip_dark.svg'
import tooltipRed from './assets/themes/red/tooltip_red.svg'
import tooltipZebra from './assets/themes/zebra/tooltip_zebra.svg'
import './themes/light.css'
import './themes/dark.css'
import './themes/red.css'
import './themes/zebra.css'

const THEME_ORDER = ['zebra', 'light', 'dark', 'red']

const THEME_ICONS = {
  light: circleLight,
  dark: circleDark,
  zebra: circleZebra,
  red: circleRed,
}

const THEME_TOOLTIPS = {
  light: tooltipLight,
  dark: tooltipDark,
  zebra: tooltipZebra,
  red: tooltipRed,
}

const THEME_NEXT_LABELS = {
  light: 'Dark Mode',
  dark: 'red Mode',
  red: 'Zebra Mode',
  zebra: 'Light Mode',
}

const THEME_TEXT_COLORS = {
  light: '#EC3C39',
  dark: '#FFFFFF',
  red: '#FFFFFF',
  zebra: '#C89F2D',
}

const THEME_LOGOS = {
  light: logo,
  red: logoWhite,
  dark: logoDark,
  zebra: logoDark,
}

const THEME_SHAPES = {
  light: { shape38: shape38Light, shape32: shape32Light },
  dark: { shape38: shape38Dark, shape32: shape32Dark },
  red: { shape38: shape38Red, shape32: shape32Red },
  zebra: { shape38: shape38Zebra, shape32: shape32Zebra },
}

const short = (addr) => addr ? `${addr.slice(0, 6)}…${addr.slice(-4)}` : ''

function parseError(e) {
  const msg = e?.message ?? ''
  if (e?.code === 4001 || e?.code === 'ACTION_REJECTED' || e?.info?.error?.code === 4001)
    return 'Transaction cancelled.'
  if (msg.toLowerCase().includes('insufficient funds'))
    return 'Insufficient AVAX balance to cover gas fees. Contact administrator for RBR token.'
  if (e?.reason)
    return `Contract error: ${e.reason}`
  if (msg.toLowerCase().includes('execution reverted'))
    return 'Transaction reverted. Check your RBR balance and approval.'
  return msg || 'Unknown error.'
}

function StatusBadge({ status }) {
  const label = status === 'not_found' ? 'Not Found' : status
  return <span className={`status-badge ${status}`}>{label}</span>
}

function HeightBlock({ entry }) {
  const { zcashHeight, result } = entry
  const status = result?.status || (result?.id ? 'accepted' : 'not_found')

  return (
    <div className="height-block">
      <div className="height-block-header">
        <span className="height-label">Zcash #{zcashHeight.toLocaleString()}</span>
        <StatusBadge status={status} />
      </div>

      {status === 'accepted' && (
        <div className="height-block-fields">
          {result.timestamp && (
            <div className="field-row">
              <span className="field-key">Timestamp</span>
              <span className="field-val">{new Date(Number(result.timestamp) * 1000).toUTCString()}</span>
            </div>
          )}
          {result.height && (
            <div className="field-row">
              <span className="field-key">Avalanche height</span>
              <span className="field-val">{result.height}</span>
            </div>
          )}
          {result.id && (
            <div className="field-row">
              <span className="field-key">Block ID</span>
              <span className="field-val">{result.id}</span>
            </div>
          )}
          {result.parentID && (
            <div className="field-row">
              <span className="field-key">Parent ID</span>
              <span className="field-val">{result.parentID}</span>
            </div>
          )}
          {result.data && (
            <>
              <div className="data-section-label">Zcash block data</div>
              <pre className="json-block">{JSON.stringify(result.data, null, 2)}</pre>
            </>
          )}
        </div>
      )}
    </div>
  )
}

export default function App() {
  const [theme, setTheme] = useState('light')
  const shapes = THEME_SHAPES[theme]
  const nextTheme = THEME_ORDER[(THEME_ORDER.indexOf(theme) + 1) % THEME_ORDER.length]

  const [wallet, setWallet] = useState(null)
  const cfg = DEFAULT_CONFIG

  const [wftBalance, setWftBalance] = useState(null)
  const [allowance, setAllowance] = useState(null)
  const [feeAmount, setFeeAmount] = useState(null)

  const [height, setHeight] = useState('')

  const [txStatus, setTxStatus] = useState(null)
  const [txHash, setTxHash] = useState(null)
  const [txError, setTxError] = useState(null)
  const [pollAttempt, setPollAttempt] = useState(0)

  // selectedNode holds a nodeID (unique, always present) rather than the
  // small-int read-proxy id, since a live-fetched validator may not have one.
  const [selectedNode, setSelectedNode] = useState(null)
  const [nodeMenuOpen, setNodeMenuOpen] = useState(false)
  const nodeMenuRef = useRef(null)

  // Live active-validator list for this subnet, fetched from P-Chain via the
  // server (see fetchValidators()) — never hardcoded.
  const [nodes, setNodes] = useState([])
  const [nodesError, setNodesError] = useState(null)

  useEffect(() => {
    fetchValidators()
      .then(setNodes)
      .catch(e => setNodesError(e.message))
  }, [])

  const selectedNodeInfo = nodes.find(n => n.nodeID === selectedNode) || null

  useEffect(() => {
    if (!nodeMenuOpen) return
    const onClickOutside = (e) => {
      if (nodeMenuRef.current && !nodeMenuRef.current.contains(e.target)) setNodeMenuOpen(false)
    }
    document.addEventListener('mousedown', onClickOutside)
    return () => document.removeEventListener('mousedown', onClickOutside)
  }, [nodeMenuOpen])

  const [results, setResults] = useState([])
  const [dataBlockHeight, setDataBlockHeight] = useState('')
  const [latestVMBlock, setLatestVMBlock] = useState('')
  const pollRef = useRef(null)

  const [recheckOpen, setRecheckOpen] = useState(false)
  const [recheckLoading, setRecheckLoading] = useState(false)
  const [recheckResult, setRecheckResult] = useState('')
  const recheckController = useRef(null)

  useEffect(() => { document.documentElement.dataset.theme = theme }, [theme])

  /* ── wallet ─────────────────────────────────────────────────────────────── */
  const connect = useCallback(async ({ silent = false } = {}) => {
    if (!window.ethereum) { if (!silent) alert('MetaMask not found.'); return }
    try {
      const provider = new ethers.BrowserProvider(window.ethereum)
      await provider.send('eth_requestAccounts', [])
      try {
        await provider.send('wallet_switchEthereumChain', [
          { chainId: ethers.toBeHex(cfg.chainId) },
        ])
      } catch (e) {
        if (e.code === 4902) {
          await provider.send('wallet_addEthereumChain', [{
            chainId: ethers.toBeHex(cfg.chainId),
            chainName: cfg.chainName,
            nativeCurrency: { name: 'AVAX', symbol: 'AVAX', decimals: 18 },
            rpcUrls: [cfg.cChainRPC],
            blockExplorerUrls: [cfg.blockExplorerUrl],
          }])
        } else throw e
      }
      const signer = await provider.getSigner()
      const address = await signer.getAddress()
      setWallet({ address, signer, provider })
    } catch (e) { if (!silent) alert(`Connect failed: ${e.message}`) }
  }, [cfg.chainId, cfg.chainName, cfg.cChainRPC, cfg.blockExplorerUrl])

  // Auto-connect on page load — shows wallet popup; silently ignores dismissal
  useEffect(() => { connect({ silent: true }) }, [connect])

  // Detect MetaMask account / network changes
  useEffect(() => {
    if (!window.ethereum) return
    const onAccountsChanged = (accounts) => {
      if (accounts.length === 0) setWallet(null)
      else connect({ silent: true })
    }
    const onChainChanged = () => connect({ silent: true })
    window.ethereum.on('accountsChanged', onAccountsChanged)
    window.ethereum.on('chainChanged', onChainChanged)
    return () => {
      window.ethereum.removeListener('accountsChanged', onAccountsChanged)
      window.ethereum.removeListener('chainChanged', onChainChanged)
    }
  }, [connect])

  const disconnect = () => setWallet(null)

  /* ── on-chain state ──────────────────────────────────────────────────────── */
  const refresh = useCallback(async () => {
    if (!wallet || !cfg.feeTokenAddr || !cfg.gatewayAddr) return
    try {
      const token = new ethers.Contract(cfg.feeTokenAddr, FEE_TOKEN_ABI, wallet.provider)
      const gateway = new ethers.Contract(cfg.gatewayAddr, GATEWAY_ABI, wallet.provider)
      const [bal, alw, fee] = await Promise.all([
        token.balanceOf(wallet.address),
        token.allowance(wallet.address, cfg.gatewayAddr),
        gateway.feeAmount(),
      ])
      setWftBalance(bal); setAllowance(alw); setFeeAmount(fee)
    } catch (e) { console.warn('refresh error', e) }
  }, [wallet, cfg.feeTokenAddr, cfg.gatewayAddr])

  useEffect(() => { refresh() }, [refresh])
  useEffect(() => { setTxError(null); setTxStatus(null) }, [wallet])
  useEffect(() => () => clearTimeout(pollRef.current), [])

  /* ── add / update result ─────────────────────────────────────────────────── */
  const addResult = useCallback((h, r) => {
    setResults(prev => {
      const idx = prev.findIndex(e => e.zcashHeight === h)
      const entry = { zcashHeight: h, result: r }
      if (idx >= 0) { const next = [...prev]; next[idx] = entry; return next }
      return [entry, ...prev]
    })
  }, [])

  /* ── poll VM ─────────────────────────────────────────────────────────────── */
  const startPolling = useCallback((h) => {
    let attempts = 0
    setPollAttempt(0)

    // Read-side queries need the small-int read-proxy id, not the nodeID —
    // some active validators have no read proxy configured (id === null),
    // in which case we still show the pending tx but can't poll VM state.
    const readNodeId = selectedNodeInfo?.id
    if (readNodeId == null) {
      setTxStatus('done')
      return
    }

    const poll = async () => {
      try {
        const r = await queryBlockByHeight(readNodeId, h)
        addResult(h, r)
        if (r.status === 'accepted' || r.id) {
          setTxStatus('done')
          if (r.height) setDataBlockHeight(String(r.height))
          getLatestHeight(readNodeId).then(h => { if (h) setLatestVMBlock(String(h)) }).catch(() => { })
          return
        }
      } catch { /* keep polling */ }

      attempts++
      setPollAttempt(attempts)
      if (attempts < 12) {
        pollRef.current = setTimeout(poll, 5000)
      } else {
        setTxStatus('done')
      }
    }

    pollRef.current = setTimeout(poll, 6000)
  }, [cfg, addResult, selectedNodeInfo])

  /* ── approve RBR ─────────────────────────────────────────────────────────── */
  const approve = async () => {
    if (!wallet || !cfg.feeTokenAddr || !cfg.gatewayAddr) return
    setTxStatus('approving'); setTxError(null); setTxHash(null)
    try {
      const token = new ethers.Contract(cfg.feeTokenAddr, FEE_TOKEN_ABI, wallet.signer)
      const tx = await token.approve(cfg.gatewayAddr, feeAmount)
      setTxHash(tx.hash)
      await tx.wait()
      await refresh()
      setTxStatus(null)
    } catch (e) { setTxError(parseError(e)); setTxStatus('error') }
  }

  /* ── get height ──────────────────────────────────────────────────────────── */
  const getHeight = async () => {
    const h = Number(height)
    if (!wallet || !cfg.gatewayAddr || !h || !selectedNode) return

    const validationId = selectedNodeInfo?.validationId
    if (!validationId) {
      setTxError('Unknown node selected.')
      setTxStatus('error')
      return
    }

    if (feeAmount !== null && wftBalance !== null && wftBalance < feeAmount) {
      setTxError(
        `Insufficient RBR balance. You have ${ethers.formatEther(wftBalance)} RBR but this request requires ${ethers.formatEther(feeAmount)} RBR.`
      )
      setTxStatus('error')
      return
    }

    setTxStatus('sending'); setTxError(null); setTxHash(null)
    try {
      const gateway = new ethers.Contract(cfg.gatewayAddr, GATEWAY_ABI, wallet.signer)
      // validationId is the selected node's real ACP-99 validationID: the
      // contract resolves that validator's own payout wallet via
      // StakingManager, and the relayer routes to its RPC endpoint.
      const tx = await gateway.requestHeight(h, validationId)
      setTxHash(tx.hash)
      await tx.wait()
      await refresh()
      addResult(h, { status: 'pending' })
      setTxStatus('waiting')
      startPolling(h)
    } catch (e) { setTxError(parseError(e)); setTxStatus('error') }
  }

  /* ── recheck all blocks ──────────────────────────────────────────────────── */
  const recheckAllBlocks = async () => {
    const readNodeId = selectedNodeInfo?.id
    if (readNodeId == null) {
      setRecheckResult('This node has no configured read endpoint.')
      setRecheckOpen(true)
      return
    }
    recheckController.current = new AbortController()
    setRecheckLoading(true)
    setRecheckResult('Rechecking…')
    setRecheckOpen(true)
    try {
      const mismatched = await reconcileBlocks(readNodeId)

      if (mismatched.length === 0) {
        setRecheckResult('All blocks have been verified and exactly match.')
      } else {
        setRecheckResult(`All blocks have been verified. Below are mismatched blocks: ${mismatched.join(', ')}.`)
      }
    } catch (e) {
      setRecheckResult(`Error: ${e.message}`)
    } finally {
      setRecheckLoading(false)
    }
  }

  const needsApprove = feeAmount !== null && allowance !== null && (
    feeAmount > 0n ? allowance < feeAmount : allowance === 0n
  )
  const busy = txStatus === 'approving' || txStatus === 'sending' || txStatus === 'waiting'
  /* ── render ──────────────────────────────────────────────────────────────── */
  return (
    <div className="app">

      {/* ── background SVG shapes ── */}
      <div className="bg-shapes">
        <img className="shape-38" src={shapes.shape38} alt="" aria-hidden="true" />
        <img className="shape-32" src={shapes.shape32} alt="" aria-hidden="true" />
      </div>

      {/* ── header ── */}
      <header>
        {/* row 0: logo, on its own */}
        <div className="header-row0">
          <img src={THEME_LOGOS[theme]} alt="red·bridge Oracle Logo" className="logo-img" />
        </div>

        {/* row 1: title + wallet */}
        <div className="header-row1">
          <h1 className="title-text">red·bridge Oracle</h1>
          <div className="header-row1-actions">
            {wallet
              ? <button className="wallet-btn connected" onClick={disconnect}>
                <span className="dot green" />{short(wallet.address)}
              </button>
              : <button className="wallet-btn" onClick={connect}>Connect Wallet</button>
            }
            <div className="theme-toggle-wrap">
              <div className="theme-tooltip">
                <img className="theme-tooltip-border" src={THEME_TOOLTIPS[theme]} alt="" aria-hidden="true" />
                <span className="theme-tooltip-text" style={{ color: THEME_TEXT_COLORS[theme] }}>
                  {THEME_NEXT_LABELS[theme]}
                </span>
              </div>
              <button
                className="theme-toggle"
                onClick={() => setTheme(nextTheme)}
                aria-label={`Switch to ${nextTheme} theme`}
              >
                <img src={THEME_ICONS[nextTheme]} alt="" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>

        {/* row 2: description */}
        <div className="header-description">
          <p>Pick which node you want to use to initiate the request and the block height of the block you want to see.</p>
        </div>
      </header>

      <main>
        {/* ── get height ── */}
        <div className="send-card">
          <div className="height-input-row">
            <div className="node-dropdown" ref={nodeMenuRef}>
              <button
                type="button"
                className={`node-select node-dropdown-toggle${!selectedNode ? ' placeholder' : ''}`}
                onClick={() => setNodeMenuOpen(o => !o)}
                disabled={busy || (!nodes.length && !nodesError)}
                aria-haspopup="listbox"
                aria-expanded={nodeMenuOpen}
              >
                {selectedNodeInfo?.label
                  || (nodesError && 'Failed to load nodes')
                  || (!nodes.length && 'Loading active validators…')
                  || 'Select a node to query'}
              </button>
              {nodeMenuOpen && (
                <ul className="node-dropdown-list" role="listbox">
                  {nodes.map(n => (
                    <li key={n.nodeID} role="option" aria-selected={n.nodeID === selectedNode}>
                      <button
                        type="button"
                        className={`node-dropdown-option${n.nodeID === selectedNode ? ' selected' : ''}`}
                        onClick={() => { setSelectedNode(n.nodeID); setNodeMenuOpen(false) }}
                      >
                        {n.label}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
          {nodesError && (
            <div className="status-banner error">Couldn't load active validators from P-Chain: {nodesError}</div>
          )}

          <div className="height-input-row">
            <input
              type="number"
              className="height-input"
              placeholder="Block Height to query"
              value={height}
              min={1}
              onChange={e => { setHeight(e.target.value); setTxStatus(null); setTxError(null) }}
              disabled={busy}
            />
          </div>

          {wallet && feeAmount !== null && wftBalance !== null && wftBalance < feeAmount && (
            <div className={`balance-info${wftBalance < feeAmount ? ' insufficient' : ''}`}>
              <span>RBR balance: <strong>{ethers.formatEther(wftBalance)} RBR</strong></span>
              <span>Fee required: <strong>{ethers.formatEther(feeAmount)} RBR</strong></span>              
            </div>
          )}

          <div className="action-row">
            {needsApprove && (
              <button className="btn-secondary" onClick={approve} disabled={busy}>
                {txStatus === 'approving' ? 'Approving…' : 'Approve RBR'}
              </button>
            )}
            <button className="btn-primary" style={{ width: 'auto' }} onClick={getHeight}
              disabled={busy || !height || !cfg.gatewayAddr || !selectedNode || needsApprove}>
              {txStatus === 'sending' ? 'Sending…'
                : txStatus === 'waiting' ? `Waiting… (${pollAttempt}/12)`
                  : 'Submit to red·bridge'}
            </button>
          </div>

          {txHash && (txStatus === 'sending' || txStatus === 'waiting' || txStatus === 'approving') && (
            <div className="tx-feedback">
              <span className="label">{txStatus === 'approving' ? 'APPROVE TX' : 'TX'}</span>
              <span className="mono small">{txHash.slice(0, 20)}…</span>
            </div>
          )}

          {txStatus === 'waiting' && (
            <div className="status-banner warn">
              Waiting for ICM relayer to deliver warp message to the VM…
            </div>
          )}

          {txStatus === 'done' && (
            <div className="status-banner ok">
              Done — see Results for the block data.
            </div>
          )}

          {txStatus === 'error' && txError && (
            <div className="status-banner error">
              {txError}
            </div>
          )}
        </div>

        {/* ── results: full width below ── */}
        <div className="card results-card">
          <div className="results-header">
            <h2>Response</h2>
            {results.length > 0 && (
              <button className="btn-ghost small" onClick={() => setResults([])}>Clear</button>
            )}
          </div>

          <div className="result-list">
            {results.map(entry => (
              <HeightBlock key={entry.zcashHeight} entry={entry} />
            ))}
          </div>
        </div>

        {/* ── block data labels ── */}
        <div className="block-data-labels">
          <p>Data retrieved from red·bridge Subnet block: <strong>{dataBlockHeight}</strong></p>
          <p>New red·bridge Oracle Subnet block height: <strong>{latestVMBlock}</strong></p>
        </div>

        {/* ── recheck all blocks ── */}
        <div>
          <button
            className="btn-primary"
            style={{ width: 'auto', flex: 'none' }}
            onClick={recheckAllBlocks}
            disabled={!selectedNode || (recheckOpen && recheckLoading)}
          >
            Recheck All Blocks
          </button>
        </div>
      </main>

      {/* ── footer ── */}
      <footer className="app-footer">
        <a
          href="https://github.com/red-dev-inc/redbridge-oracle"
          target="_blank"
          rel="noopener noreferrer"
          className="footer-link"
        >
          red·bridge Oracle on GitHub
        </a>
      </footer>

      {/* ── recheck modal ── */}
      {recheckOpen && (
        <div className="modal-backdrop" onClick={() => { if (!recheckLoading) setRecheckOpen(false) }}>
          <div className="modal-box" onClick={e => e.stopPropagation()}>
            <span
              className="modal-close"
              onClick={() => { recheckController.current?.abort(); setRecheckOpen(false) }}
            >✕</span>
            <div className="modal-body">
              {recheckLoading && <span className="modal-spinner" />}
              <span>{recheckResult}</span>
            </div>
            {!recheckLoading && (
              <div className="modal-footer">
                <button className="btn-primary" style={{ flex: 'none' }} onClick={() => setRecheckOpen(false)}>
                  Close
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

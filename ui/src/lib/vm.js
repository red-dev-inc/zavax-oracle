async function post(endpoint, body) {
  const res = await fetch(endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json()
  if (data.error) throw new Error(data.error.message)
  return data
}

export async function queryBlockByHeight(nodeId, height) {
  const data = await post('/api/block', { nodeId, height })
  return data.result || {}
}

export async function getLatestHeight(nodeId) {
  const data = await post('/api/height', { nodeId })
  return data.result?.height ?? ''
}

export async function reconcileBlocks(nodeId) {
  const data = await post('/api/reconcile', { nodeId })
  const result = data.result
  return result?.mismatched === 0 ? [] : (result ? [result] : [])
}

// fetchValidators returns the subnet's currently active validators, fetched
// live from P-Chain by the server (never hand-copied): each entry carries the
// real validationId to pass to requestHeight(), plus the ops-assigned id/label
// for whichever ones also have a configured read-side RPC proxy.
export async function fetchValidators() {
  const res = await fetch('/api/validators')
  const data = await res.json()
  if (data.error) throw new Error(data.error)
  return data.validators || []
}

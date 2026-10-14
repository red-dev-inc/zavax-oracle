package vm

import "sync"

// Mempool manages pending Zcash height requests that have not yet been
// included in a finalized block.  It is safe for concurrent use.
//
// Invariants:
//   - Each Zcash height appears at most once in the queue.
//   - Heights that are already accepted in chain state must not be added.
type Mempool struct {
	mu    sync.Mutex
	queue []uint64        // FIFO of pending heights
	seen  map[uint64]bool // dedup set: height present in queue OR being built
}

// NewMempool creates an empty Mempool.
func NewMempool() *Mempool {
	return &Mempool{
		seen: make(map[uint64]bool),
	}
}

// Push adds a Zcash height to the queue if not already present.
// Returns true if the height was newly added.
func (m *Mempool) Push(height uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.seen[height] {
		return false
	}
	m.seen[height] = true
	m.queue = append(m.queue, height)
	return true
}

// Pop removes and returns the next pending Zcash height.
// Returns (0, false) when the queue is empty.
func (m *Mempool) Pop() (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for len(m.queue) > 0 {
		h := m.queue[0]
		m.queue = m.queue[1:]

		// A height may have been removed while still in the queue
		// (e.g., accepted via a peer's block).  Skip stale entries.
		if !m.seen[h] {
			continue
		}
		return h, true
	}
	return 0, false
}

// Has checks whether a height is in the mempool (queued or being built).
func (m *Mempool) Has(height uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen[height]
}

// Remove deletes a height from the seen set.  The corresponding queue
// entry (if any) will be lazily skipped on the next Pop.
func (m *Mempool) Remove(height uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.seen, height)
}

// Requeue re-adds a height to the queue, even if it is already in the
// seen set (e.g., after a failed BuildBlock).  This is necessary because
// Pop does not remove the height from seen, so a normal Push would be a
// no-op for heights that were popped but need to be retried.
func (m *Mempool) Requeue(height uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen[height] = true
	m.queue = append(m.queue, height)
}

// Len returns the number of heights in the seen set.
func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.seen)
}

// Peek returns the next height without removing it.
func (m *Mempool) Peek() (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for len(m.queue) > 0 {
		h := m.queue[0]
		if m.seen[h] {
			return h, true
		}
		// lazy cleanup
		m.queue = m.queue[1:]
	}
	return 0, false
}

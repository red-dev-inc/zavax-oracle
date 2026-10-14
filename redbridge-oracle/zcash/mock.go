package zcash

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// MockClient implements Client with configurable in-memory data for testing.
// It is safe for concurrent use.
type MockClient struct {
	mu           sync.RWMutex
	blocks       map[uint64]*BlockInfo
	latestHeight uint64
}

// NewMockClient creates a MockClient pre-populated with block data for the
// given height range [1, latestHeight]. Each mock block gets a deterministic
// hash derived from its height.
func NewMockClient(latestHeight uint64) *MockClient {
	m := &MockClient{
		blocks:       make(map[uint64]*BlockInfo),
		latestHeight: latestHeight,
	}
	for h := uint64(1); h <= latestHeight; h++ {
		m.blocks[h] = &BlockInfo{
			Height: h,
			Hash:   fmt.Sprintf("%064x", h), // deterministic mock hash
			Time:   int64(h) * 60,           // deterministic mock timestamp
		}
	}
	return m
}

// AddBlock inserts or replaces a block at the given height.
func (m *MockClient) AddBlock(info *BlockInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks[info.Height] = info
	if info.Height > m.latestHeight {
		m.latestHeight = info.Height
	}
}

// SetLatestHeight overrides the reported latest height (useful for testing
// the 24-block finality rule).
func (m *MockClient) SetLatestHeight(h uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latestHeight = h
}

// GetBlockByHeight returns mock block data for the requested height.
func (m *MockClient) GetBlockByHeight(_ context.Context, height uint64) (*BlockInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.blocks[height]
	if !ok {
		return nil, fmt.Errorf("mock: block at height %d not found", height)
	}
	// Return a copy to prevent mutation.
	cp := *info
	// Generate RawData on the fly if not already set.
	if cp.RawData == nil {
		cp.RawData = mockBlockJSON(cp.Height, cp.Hash, cp.Time)
	}
	return &cp, nil
}

// GetLatestHeight returns the configured latest Zcash height.
func (m *MockClient) GetLatestHeight(_ context.Context) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latestHeight, nil
}

// mockBlockJSON generates a full Zcash block JSON blob for testing.
func mockBlockJSON(height uint64, hash string, timestamp int64) json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"hash":             hash,
		"confirmations":    int64(100),
		"size":             0,
		"height":           height,
		"version":          4,
		"merkleroot":       "",
		"blockcommitments": "",
		"authdataroot":     "",
		"finalsaplingroot": "",
		"chainhistoryroot": "",
		"tx":               []string{},
		"time":             timestamp,
		"nonce":            "",
		"solution":         "",
		"bits":             "",
		"difficulty":       0.0,
		"chainwork":        "",
		"anchor":           "",
		"chainSupply": map[string]interface{}{
			"monitored":     false,
			"chainValue":    0,
			"chainValueZat": 0,
			"valueDelta":    0,
			"valueDeltaZat": 0,
		},
		"valuePools":        nil,
		"previousblockhash": "",
		"nextblockhash":     "",
	})
	return json.RawMessage(data)
}

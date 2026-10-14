// Package zcash provides a client interface for querying Zcash blockchain data.
package zcash

import "encoding/json"

// BlockInfo contains data fetched from a Zcash block, used to construct
// deterministic Avalanche blocks.
type BlockInfo struct {
	// Height is the Zcash block height.
	Height uint64 `json:"height"`
	// Hash is the hex-encoded Zcash block hash (deterministic, immutable for confirmed blocks).
	Hash string `json:"hash"`
	// Time is the Zcash block timestamp in Unix seconds.
	Time int64 `json:"time"`
	// RawData is the full JSON-encoded Zcash block as returned by the RPC node.
	// It is not included in JSON serialization of BlockInfo itself.
	RawData json.RawMessage `json:"-"`
}

package vm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// This file serves the validators API that signature aggregators and the
// Avalanche Builder Console read to discover a validator's uptime before
// asking the L1 to sign a proof of it. It is mounted at
//
//	/ext/bc/<blockchainID>/validators
//
// and speaks the same JSON-RPC method and field names as Subnet-EVM's
// validators API, so existing tooling works against this VM unmodified.

// ValidatorReply describes one L1 validator as seen by THIS node.
//
// uptimeSeconds is this node's own measurement, not a network-wide consensus
// value. Every validator measures independently and each will only sign a
// proof up to its own figure, so a proof aggregated from the single highest
// reading can fail to reach quorum. Tooling that wants the most reliable
// number should poll several validators and use the lowest.
type ValidatorReply struct {
	ValidationID     string  `json:"validationID"`
	ValidationIDHex  string  `json:"validationIDHex"`
	NodeID           string  `json:"nodeID"`
	Weight           uint64  `json:"weight"`
	StartTimestamp   int64   `json:"startTimestamp"`
	IsActive         bool    `json:"isActive"`
	IsL1Validator    bool    `json:"isL1Validator"`
	IsConnected      bool    `json:"isConnected"`
	UptimeSeconds    uint64  `json:"uptimeSeconds"`
	UptimePercentage float64 `json:"uptimePercentage"`
}

// GetCurrentValidatorsReply is the response for validators.getCurrentValidators.
type GetCurrentValidatorsReply struct {
	Validators []ValidatorReply `json:"validators"`
}

// newValidatorsHTTPHandler returns the handler mounted at /validators.
func newValidatorsHTTPHandler(vm *VM) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, nil, -32700, "parse error: "+err.Error())
			return
		}
		switch req.Method {
		case "validators.getCurrentValidators":
			handleGetCurrentValidators(vm, w, req)
		default:
			writeJSONError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	})
}

func handleGetCurrentValidators(vm *VM, w http.ResponseWriter, req JSONRPCRequest) {
	if vm.uptimeTracker == nil {
		writeJSONError(w, req.ID, -32603, "uptime tracking is not enabled on this node")
		return
	}

	snapshot := vm.uptimeTracker.validatorSnapshot()

	out := make([]ValidatorReply, 0, len(snapshot))
	for _, v := range snapshot {
		out = append(out, ValidatorReply{
			ValidationID:     v.validationID.String(),
			ValidationIDHex:  "0x" + hex.EncodeToString(v.validationID[:]),
			NodeID:           v.nodeID.String(),
			Weight:           v.weight,
			StartTimestamp:   v.startTime.Unix(),
			IsActive:         v.isActive,
			IsL1Validator:    v.isL1Validator,
			IsConnected:      v.isConnected,
			UptimeSeconds:    v.uptimeSeconds,
			UptimePercentage: v.uptimePercentage,
		})
	}

	// Stable ordering keeps responses diffable and tests deterministic; the
	// underlying data comes from a map, which Go iterates in random order.
	sort.Slice(out, func(i, j int) bool {
		return out[i].ValidationID < out[j].ValidationID
	})

	writeJSONResult(w, req.ID, GetCurrentValidatorsReply{Validators: out})
}

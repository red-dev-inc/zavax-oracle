package vm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// -------------------- JSON-RPC types --------------------

// JSONRPCRequest is a standard JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// JSONRPCResponse is a standard JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// -------------------- Request/response payloads --------------------

// GetZcashHeightArgs are the parameters for the getZcashHeight method.
type GetZcashHeightArgs struct {
	Height uint64 `json:"height"`
}

// GetZcashHeightReply is the response for the getBlockByHeight method.
// For accepted blocks, Data contains the full Zcash block JSON.
// For pending/unknown, only Status is set.
type GetZcashHeightReply struct {
	Timestamp string          `json:"timestamp,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Height    string          `json:"height,omitempty"`
	ID        string          `json:"id,omitempty"`
	ParentID  string          `json:"parentID,omitempty"`
	Status    string          `json:"status,omitempty"`
}

// GetBlockArgs are the parameters for the zavax.getBlock method.
type GetBlockArgs struct {
	BlockID string `json:"blockId"`
}

// GetBlockReply is the response for the zavax.getBlock method.
type GetBlockReply struct {
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
	Height    string          `json:"height"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parentID"`
}

// ReconcileBlocksReply is the response for the zavax.reconcileBlocks method.
type ReconcileBlocksReply struct {
	Height     int               `json:"height"`
	Matched    int               `json:"matched"`
	Mismatched int               `json:"mismatched"`
	Errors     int               `json:"errors"`
	Details    []ReconcileDetail `json:"details,omitempty"`
}

// PostWarpMessageArgs are the parameters for the zavax.postWarpMessage method.
type PostWarpMessageArgs struct {
	WarpMessage string `json:"warpMessage"`
}

// PostWarpMessageReply is the response for the zavax.postWarpMessage method.
type PostWarpMessageReply struct {
	Status string `json:"status"`
	Height uint64 `json:"height"`
}

// ReconcileDetail reports the result for a single Zcash height.
type ReconcileDetail struct {
	ZcashHeight uint64 `json:"zcashHeight"`
	StoredHash  string `json:"storedHash"`
	NodeHash    string `json:"nodeHash,omitempty"`
	Status      string `json:"status"` // "match", "mismatch", "error"
	Error       string `json:"error,omitempty"`
}

// -------------------- HTTP handler --------------------

// Service holds the API handler state.
type Service struct {
	vm *VM
}

// NewHTTPHandler creates an http.Handler that serves the VM's JSON-RPC API.
func NewHTTPHandler(vm *VM) http.Handler {
	vm.log.Info("NewHTTPHandler: creating JSON-RPC handler for zcash-bridge-vm")
	svc := &Service{vm: vm}
	return http.HandlerFunc(svc.handleRPC)
}

// handleRPC is the single JSON-RPC endpoint that dispatches by method name.
func (s *Service) handleRPC(w http.ResponseWriter, r *http.Request) {
	s.vm.log.Debug("handleRPC: incoming request",
		zap.String("httpMethod", r.Method),
		zap.String("url", r.URL.String()),
		zap.String("remoteAddr", r.RemoteAddr),
		zap.String("contentType", r.Header.Get("Content-Type")),
	)

	if r.Method != http.MethodPost {
		s.vm.log.Warn("handleRPC: rejected non-POST request",
			zap.String("httpMethod", r.Method),
		)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.vm.log.Error("handleRPC: failed to decode request body",
			zap.Error(err),
		)
		writeJSONError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	s.vm.log.Info("handleRPC: dispatching",
		zap.String("method", req.Method),
		zap.Any("id", req.ID),
	)
	s.vm.log.Debug("handleRPC: params", zap.String("params", string(req.Params)))

	switch req.Method {
	case "zavax.getBlockByHeight":
		s.getZcashHeight(w, r, req)
	case "zavax.getBlock":
		s.getBlock(w, r, req)
	case "zavax.reconcileBlocks":
		s.reconcileBlocks(w, r, req)
	case "zavax.postWarpMessage":
		s.postWarpMessage(w, r, req)
	case "zavax.getOutboundWarpMessages":
		s.getOutboundWarpMessages(w, r, req)
	case "zavax.deleteOutboundWarpMessage":
		s.deleteOutboundWarpMessage(w, r, req)
	default:
		s.vm.log.Warn("handleRPC: unknown method",
			zap.String("method", req.Method),
		)
		writeJSONError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// getZcashHeight handles zavax.getBlockByHeight — a public read-only query.
//
// Returns accepted block data if the height exists in the chain, "pending" if
// it is queued for consensus, or "not_found" if it has never been requested.
// This method never triggers a Zcash RPC fetch or pushes anything to the
// mempool. Bridging a new height is only possible via zavax.postWarpMessage
// (ICM relayer path).
func (s *Service) getZcashHeight(w http.ResponseWriter, _ *http.Request, req JSONRPCRequest) {
	s.vm.log.Debug("getZcashHeight: entry", zap.String("rawParams", string(req.Params)))

	var args GetZcashHeightArgs
	if err := json.Unmarshal(req.Params, &args); err != nil {
		s.vm.log.Error("getZcashHeight: invalid params", zap.Error(err))
		writeJSONError(w, req.ID, -32602, "invalid params: "+err.Error())
		if s.vm.metrics != nil {
			s.vm.metrics.apiRequests.WithLabelValues("getZcashHeight", "error").Inc()
		}
		return
	}

	height := args.Height

	s.vm.log.Info("getZcashHeight: querying", zap.Uint64("height", height))

	if height == 0 {
		writeJSONError(w, req.ID, -32602, "height must be greater than 0")
		if s.vm.metrics != nil {
			s.vm.metrics.apiRequests.WithLabelValues("getZcashHeight", "invalid").Inc()
		}
		return
	}

	// Accepted path: height exists in the chain — return full block data.
	if blk, err := s.vm.state.GetBlockByZcashHeight(height); err == nil {
		s.vm.log.Info("getZcashHeight: found accepted block",
			zap.Uint64("zcashHeight", height),
			zap.Stringer("blockID", blk.id),
		)
		if s.vm.metrics != nil {
			s.vm.metrics.apiRequests.WithLabelValues("getZcashHeight", "hit").Inc()
			s.vm.metrics.duplicateRequests.Inc()
		}
		var zcashData json.RawMessage
		if data, dataErr := s.vm.state.GetZcashData(height); dataErr == nil {
			zcashData = json.RawMessage(data)
		}
		writeJSONResult(w, req.ID, GetZcashHeightReply{
			Timestamp: fmt.Sprintf("%d", blk.Tmstmp),
			Data:      zcashData,
			Height:    fmt.Sprintf("%d", blk.Hght),
			ID:        blk.id.String(),
			ParentID:  blk.PrntID.String(),
		})
		return
	}

	// Pending path: height was submitted via postWarpMessage and is in the mempool.
	if s.vm.mempool.Has(height) {
		s.vm.log.Info("getZcashHeight: height is pending in mempool",
			zap.Uint64("zcashHeight", height),
		)
		if s.vm.metrics != nil {
			s.vm.metrics.apiRequests.WithLabelValues("getZcashHeight", "pending").Inc()
		}
		writeJSONResult(w, req.ID, GetZcashHeightReply{Status: "pending"})
		return
	}

	// Not found — this height has not been submitted for bridging.
	s.vm.log.Info("getZcashHeight: height not found",
		zap.Uint64("zcashHeight", height),
	)
	if s.vm.metrics != nil {
		s.vm.metrics.apiRequests.WithLabelValues("getZcashHeight", "not_found").Inc()
	}
	writeJSONResult(w, req.ID, GetZcashHeightReply{Status: "not_found"})
}

// -------------------- JSON helpers --------------------

// getBlock returns a block by its Avalanche block ID.
func (s *Service) getBlock(w http.ResponseWriter, _ *http.Request, req JSONRPCRequest) {
	s.vm.log.Debug("getBlock: entry", zap.String("rawParams", string(req.Params)))

	var args GetBlockArgs
	if err := json.Unmarshal(req.Params, &args); err != nil {
		writeJSONError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	var blkID ids.ID
	if args.BlockID == "" {
		// No blockId provided — return the latest (last accepted) block.
		s.vm.mu.RLock()
		blkID = s.vm.lastAccepted
		s.vm.mu.RUnlock()
		s.vm.log.Info("getBlock: no blockId provided, using lastAccepted",
			zap.Stringer("lastAccepted", blkID),
		)
	} else {
		var err error
		blkID, err = ids.FromString(args.BlockID)
		if err != nil {
			writeJSONError(w, req.ID, -32602, fmt.Sprintf("invalid blockId: %v", err))
			return
		}
	}

	blk, err := s.vm.state.GetBlock(blkID)
	if err != nil {
		s.vm.log.Warn("getBlock: block not found",
			zap.String("blockId", args.BlockID),
			zap.Error(err),
		)
		writeJSONError(w, req.ID, -32603, fmt.Sprintf("block not found: %v", err))
		return
	}
	blk.vm = s.vm

	s.vm.log.Info("getBlock: found block",
		zap.String("blockId", args.BlockID),
		zap.Uint64("avaxHeight", blk.Hght),
		zap.Uint64("zcashHeight", blk.ZcashHeight),
		zap.String("zcashHash", blk.ZcashHash),
	)

	// Retrieve the full Zcash block data if available.
	var zcashData json.RawMessage
	if blk.ZcashHeight > 0 {
		if data, dataErr := s.vm.state.GetZcashData(blk.ZcashHeight); dataErr == nil {
			zcashData = json.RawMessage(data)
		}
	}

	writeJSONResult(w, req.ID, GetBlockReply{
		Timestamp: fmt.Sprintf("%d", blk.Tmstmp),
		Data:      zcashData,
		Height:    fmt.Sprintf("%d", blk.Hght),
		ID:        blk.id.String(),
		ParentID:  blk.PrntID.String(),
	})
}

// reconcileBlocks verifies all stored Zcash block hashes against the live
// Zcash node. It iterates every accepted height and compares the stored hash
// with what the Zcash RPC returns.
func (s *Service) reconcileBlocks(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	s.vm.log.Info("reconcileBlocks: starting reconciliation")
	ctx := r.Context()

	entries, err := s.vm.state.GetAllZcashHeights()
	if err != nil {
		writeJSONError(w, req.ID, -32603, fmt.Sprintf("failed to read state: %v", err))
		return
	}

	reply := ReconcileBlocksReply{
		Height: len(entries),
	}

	for _, entry := range entries {
		blk, blkErr := s.vm.state.GetBlock(entry.BlockID)
		if blkErr != nil {
			reply.Errors++
			reply.Details = append(reply.Details, ReconcileDetail{
				ZcashHeight: entry.ZcashHeight,
				Status:      "error",
				Error:       fmt.Sprintf("load block: %v", blkErr),
			})
			continue
		}

		info, fetchErr := s.vm.zcash.GetBlockByHeight(ctx, entry.ZcashHeight)
		if fetchErr != nil {
			reply.Errors++
			reply.Details = append(reply.Details, ReconcileDetail{
				ZcashHeight: entry.ZcashHeight,
				StoredHash:  blk.ZcashHash,
				Status:      "error",
				Error:       fmt.Sprintf("zcash rpc: %v", fetchErr),
			})
			continue
		}

		if blk.ZcashHash == info.Hash {
			reply.Matched++
			reply.Details = append(reply.Details, ReconcileDetail{
				ZcashHeight: entry.ZcashHeight,
				StoredHash:  blk.ZcashHash,
				NodeHash:    info.Hash,
				Status:      "match",
			})
		} else {
			reply.Mismatched++
			reply.Details = append(reply.Details, ReconcileDetail{
				ZcashHeight: entry.ZcashHeight,
				StoredHash:  blk.ZcashHash,
				NodeHash:    info.Hash,
				Status:      "mismatch",
			})
		}
	}

	s.vm.log.Info("reconcile completed",
		zap.Int("height", reply.Height),
		zap.Int("matched", reply.Matched),
		zap.Int("mismatched", reply.Mismatched),
		zap.Int("errors", reply.Errors),
	)

	writeJSONResult(w, req.ID, reply)
}

// postWarpMessage handles the zavax.postWarpMessage method.
//
// Called by the ICM relayer after it collects BLS signatures for a warp
// message emitted by ZcashHeightGateway on C-Chain.
//
// Flow:
//  1. Decode and BLS-verify the warp message.
//  2. Extract the Zcash block height from the payload.
//  3. No-op if already accepted or pending.
//  4. Validate confirmation depth, then enqueue in mempool.
func (s *Service) postWarpMessage(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	s.vm.log.Debug("postWarpMessage: entry")

	var args PostWarpMessageArgs
	if err := json.Unmarshal(req.Params, &args); err != nil {
		writeJSONError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	if args.WarpMessage == "" {
		writeJSONError(w, req.ID, -32602, "warpMessage is required")
		return
	}

	// Decode warp message bytes and verify BLS aggregate signature.
	height, wallet, validationID, err := s.vm.decodeWarpMessage(r.Context(), args.WarpMessage)
	if err != nil {
		s.vm.log.Warn("postWarpMessage: warp decode/verify failed", zap.Error(err))
		writeJSONError(w, req.ID, -32603, fmt.Sprintf("warp verification failed: %v", err))
		return
	}

	// validationID is informational here: the fee for a validator-targeted
	// request was already paid to that validator's StakingManager reward
	// wallet on C-Chain by ZcashHeightGateway before this message was even sent.
	s.vm.log.Info("postWarpMessage: warp verified",
		zap.Uint64("zcashHeight", height),
		zap.Stringer("validationId", validationID),
	)

	// Already accepted — no-op.  Check before storing anything so that a
	// second user querying the same height never overwrites the first wallet.
	if s.vm.state.HasZcashHeight(height) {
		s.vm.log.Info("postWarpMessage: height already accepted, skipping",
			zap.Uint64("zcashHeight", height),
		)
		writeJSONResult(w, req.ID, PostWarpMessageReply{Status: "already_accepted", Height: height})
		return
	}

	// Already queued by a previous caller — no-op, preserve existing wallet.
	if s.vm.mempool.Has(height) {
		s.vm.log.Info("postWarpMessage: height already pending",
			zap.Uint64("zcashHeight", height),
		)
		writeJSONResult(w, req.ID, PostWarpMessageReply{Status: "pending", Height: height})
		return
	}

	// Validate the 24-block finality rule before enqueueing.
	ctx := r.Context()
	latestHeight, err := s.vm.zcash.GetLatestHeight(ctx)
	if err != nil {
		s.vm.log.Warn("postWarpMessage: failed to query zcash latest height", zap.Error(err))
		writeJSONError(w, req.ID, -32603,
			fmt.Sprintf("failed to query zcash latest height: %v", err))
		return
	}
	minAllowed := uint64(0)
	if latestHeight > s.vm.config.ConfirmationDepth {
		minAllowed = latestHeight - s.vm.config.ConfirmationDepth
	}
	if height > minAllowed {
		s.vm.log.Info("postWarpMessage: height too recent, rejecting",
			zap.Uint64("requestedHeight", height),
			zap.Uint64("latestHeight", latestHeight),
			zap.Uint64("minAllowed", minAllowed),
		)
		writeJSONError(w, req.ID, -32604,
			fmt.Sprintf("height %d too recent: latest=%d, max allowed=%d (need %d confirmations)",
				height, latestHeight, minAllowed, s.vm.config.ConfirmationDepth))
		return
	}

	// This is a genuinely new height.  Now store the wallet so Accept() can
	// emit the outbound warp message.  Gossip it to peer validators
	// so all 5 nodes have it before the block is accepted.
	var zeroWallet [20]byte
	if wallet != zeroWallet {
		if putErr := s.vm.state.PutWalletAddr(height, wallet); putErr != nil {
			s.vm.log.Warn("postWarpMessage: failed to store wallet addr", zap.Error(putErr))
		}
		s.vm.gossipWallet(ctx, height, wallet)
	}

	// Enqueue and notify the consensus engine.
	if s.vm.mempool.Push(height) {
		s.vm.notifyEngine()
		s.vm.log.Info("postWarpMessage: height enqueued in mempool",
			zap.Uint64("zcashHeight", height),
			zap.Int("mempoolSize", s.vm.mempool.Len()),
		)
	}

	writeJSONResult(w, req.ID, PostWarpMessageReply{Status: "pending", Height: height})
}

// OutboundWarpMessage is a pending unsigned warp message ready for BLS signing.
type OutboundWarpMessage struct {
	MessageID       string `json:"messageId"`       // hex-encoded 32-byte ID
	UnsignedMessage string `json:"unsignedMessage"` // hex-encoded serialised bytes
	BlockHeight     uint64 `json:"blockHeight"`     // Avalanche chain height — relayer cursor
}

// GetOutboundWarpMessagesArgs are the parameters for getOutboundWarpMessages.
type GetOutboundWarpMessagesArgs struct {
	FromHeight uint64 `json:"fromHeight"` // only return messages with blockHeight >= fromHeight
}

// getOutboundWarpMessages returns pending outbound warp messages at or above
// the given Avalanche block height cursor (default 0 = all messages).
func (s *Service) getOutboundWarpMessages(w http.ResponseWriter, _ *http.Request, req JSONRPCRequest) {
	var args GetOutboundWarpMessagesArgs
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &args)
	}

	s.vm.outboundWarpMsgsMu.RLock()
	msgs := make([]OutboundWarpMessage, 0, len(s.vm.outboundWarpMsgs))
	for msgID, entry := range s.vm.outboundWarpMsgs {
		if entry.avaxHeight < args.FromHeight {
			continue
		}
		id := msgID
		msgs = append(msgs, OutboundWarpMessage{
			MessageID:       hex.EncodeToString(id[:]),
			UnsignedMessage: hex.EncodeToString(entry.msg.Bytes()),
			BlockHeight:     entry.avaxHeight,
		})
	}
	s.vm.outboundWarpMsgsMu.RUnlock()

	writeJSONResult(w, req.ID, msgs)
}

// DeleteOutboundWarpMessageArgs are the parameters for deleteOutboundWarpMessage.
type DeleteOutboundWarpMessageArgs struct {
	MessageID   string `json:"messageId"`   // hex-encoded 32-byte message ID
	BlockHeight uint64 `json:"blockHeight"` // Avalanche chain height (needed for LevelDB key)
}

// deleteOutboundWarpMessage removes a delivered message from the in-memory
// outbox and from LevelDB so it is not returned on future polls or after restart.
func (s *Service) deleteOutboundWarpMessage(w http.ResponseWriter, _ *http.Request, req JSONRPCRequest) {
	var args DeleteOutboundWarpMessageArgs
	if err := json.Unmarshal(req.Params, &args); err != nil {
		writeJSONError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	msgIDBytes, err := hex.DecodeString(strings.TrimPrefix(args.MessageID, "0x"))
	if err != nil || len(msgIDBytes) != ids.IDLen {
		writeJSONError(w, req.ID, -32602, "invalid messageId")
		return
	}
	var msgID ids.ID
	copy(msgID[:], msgIDBytes)

	s.vm.outboundWarpMsgsMu.Lock()
	delete(s.vm.outboundWarpMsgs, msgID)
	s.vm.outboundWarpMsgsMu.Unlock()

	if err := s.vm.state.DeleteOutboundWarpMsg(args.BlockHeight, msgID); err != nil {
		s.vm.log.Warn("deleteOutboundWarpMessage: LevelDB delete failed", zap.Error(err))
	}

	writeJSONResult(w, req.ID, true)
}

// -------------------- /warp endpoint (BLS signing) --------------------

// WarpSignatureArgs are the params for warp_getMessageSignature.
type WarpSignatureArgs struct {
	MessageID       string `json:"messageID"`       // hex-encoded 32-byte message ID
	UnsignedMessage string `json:"unsignedMessage"` // full hex bytes of the UnsignedMessage
}

// WarpSignatureReply is the response for warp_getMessageSignature.
type WarpSignatureReply struct {
	Signature string `json:"signature"` // "0x"-prefixed hex BLS signature
	NodeID    string `json:"nodeId"`    // NodeID of the signing validator
}

// newWarpHTTPHandler returns the HTTP handler mounted at /warp.
// It implements warp_getMessageSignature so the ICM signature aggregator can
// collect each validator's BLS signature for outbound warp messages.
func newWarpHTTPHandler(vm *VM) http.Handler {
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
		case "warp_getMessageSignature":
			handleWarpGetMessageSignature(vm, w, req)
		default:
			writeJSONError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	})
}

func handleWarpGetMessageSignature(vm *VM, w http.ResponseWriter, req JSONRPCRequest) {
	var args WarpSignatureArgs
	if err := json.Unmarshal(req.Params, &args); err != nil {
		writeJSONError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	var msg *warp.UnsignedMessage

	if args.UnsignedMessage != "" {
		// Preferred path: caller supplies the full unsigned message bytes.
		// This lets all validators sign without needing in-memory gossip.
		rawMsg, err := hex.DecodeString(strings.TrimPrefix(args.UnsignedMessage, "0x"))
		if err != nil {
			writeJSONError(w, req.ID, -32602, "unsignedMessage: invalid hex: "+err.Error())
			return
		}
		parsed, err := warp.ParseUnsignedMessage(rawMsg)
		if err != nil {
			writeJSONError(w, req.ID, -32602, "unsignedMessage: parse error: "+err.Error())
			return
		}
		// Refuse to sign messages not originating from this chain.
		if parsed.SourceChainID != vm.ctx.ChainID {
			writeJSONError(w, req.ID, -32602,
				fmt.Sprintf("unsignedMessage: sourceChainID mismatch (got %s, want %s)",
					parsed.SourceChainID, vm.ctx.ChainID))
			return
		}
		msg = parsed
	} else {
		// Fallback: look up by message ID from the in-memory outbox.
		rawID, err := hex.DecodeString(strings.TrimPrefix(args.MessageID, "0x"))
		if err != nil || len(rawID) != ids.IDLen {
			writeJSONError(w, req.ID, -32602, fmt.Sprintf("messageID must be %d hex bytes", ids.IDLen))
			return
		}
		var msgID ids.ID
		copy(msgID[:], rawID)

		vm.outboundWarpMsgsMu.RLock()
		entry, ok := vm.outboundWarpMsgs[msgID]
		vm.outboundWarpMsgsMu.RUnlock()

		if !ok {
			writeJSONError(w, req.ID, -32603, "message not found")
			return
		}
		msg = entry.msg
	}

	// An uptime proof must never be obtainable here. This endpoint signs any
	// message attributed to this chain, which the ICM relayer relies on for
	// oracle messages, but that latitude would let anyone mint staking rewards
	// against a fabricated uptime. Uptime proofs are only ever signed after
	// being checked against this node's own measurement.
	if err := vm.checkNotForgedUptime(msg); err != nil {
		vm.log.Warn("warp_getMessageSignature: refusing to sign uptime proof",
			zap.Error(err),
		)
		writeJSONError(w, req.ID, -32602, "refusing to sign: "+err.Error())
		return
	}

	sig, err := vm.ctx.WarpSigner.Sign(msg)
	if err != nil {
		vm.log.Error("warp_getMessageSignature: signing failed",
			zap.String("messageID", args.MessageID),
			zap.Error(err),
		)
		writeJSONError(w, req.ID, -32603, "signing failed: "+err.Error())
		return
	}

	writeJSONResult(w, req.ID, WarpSignatureReply{
		Signature: "0x" + hex.EncodeToString(sig),
		NodeID:    vm.ctx.NodeID.String(),
	})
}

func writeJSONResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Can't do much here — response already partially written.
		_ = err
	}
}

func writeJSONError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		_ = err
	}
}

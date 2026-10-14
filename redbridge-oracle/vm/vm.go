// Package vm implements the Zcash Bridge VM – a minimal, production-grade
// Avalanche custom VM that bridges Zcash block data onto an Avalanche subnet
// using Snowman (linear chain) consensus.
//
// # Architecture
//
//   - vm.go      – VM lifecycle, consensus hooks (Initialize, BuildBlock, …)
//   - block.go   – Block structure, serialization, Verify/Accept/Reject
//   - state.go   – Persistent storage (blocks, zcash height index)
//   - mempool.go – Thread-safe pending-height queue
//   - api.go     – HTTP JSON-RPC endpoint (getZcashHeight)
//   - metrics.go – Prometheus metrics
//   - config.go  – Runtime configuration
//
// # Validator uptime (staking rewards)
//
// A PoS StakingManager pays staking rewards only against a ValidationUptimeMessage
// signed by this L1's own validators. The uptime_*.go files provide that:
//
//   - uptime_tracker.go  – measures per-validator uptime from peer connectivity
//   - uptime_state.go    – persists uptime across restarts
//   - uptime_message.go  – the 46-byte wire format the contract requires
//   - uptime_verifier.go – refuses to sign any uptime this node did not observe
//   - uptime_api.go      – serves /validators for signature aggregators
//
// # Determinism guarantees
//
// All block fields are derived from deterministic inputs:
//
//	parentID     = lastAccepted block ID (chain state)
//	height       = parent.height + 1
//	timestamp    = max(parent.timestamp+1, zcashBlock.time)
//	zcashHeight  = requested height
//	zcashHash    = fetched from Zcash RPC (immutable for confirmed blocks)
//
// Because zcash blocks must have ≥24 confirmations, their data is immutable
// across all validators. This guarantees that every validator produces
// byte-identical blocks for the same input, enabling trivial Snowman
// convergence.
package vm

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	"github.com/ava-labs/zcash-bridge-vm/zcash"
)

const (
	// Name is the human-readable name of this VM.
	Name = "zcash-bridge-vm"
	// VMVersion is the semantic version string reported by Version().
	VMVersion = "0.1.0"
	// signatureRequestsPerMinute caps how many ACP-118 signature requests a
	// single peer may make per minute.
	signatureRequestsPerMinute = 60
	// p2pMetricsNamespace is the metrics namespace for the ACP-118 p2p
	// network. Deliberately NOT prefixed with the VM's own "zcash_bridge"
	// namespace: avalanchego refuses to register two namespaces where either
	// is a prefix of the other.
	p2pMetricsNamespace = "p2p"
)

var (
	errNoPendingBlocks = errors.New("no pending blocks to build")
	errAlreadyAccepted = errors.New("zcash height already accepted")
)

// Compile-time interface check.
var _ block.ChainVM = (*VM)(nil)

// outboundMsgEntry pairs an unsigned warp message with the Avalanche chain
// height at which it was emitted, so the relayer can use it as a cursor.
type outboundMsgEntry struct {
	msg        *warp.UnsignedMessage
	avaxHeight uint64
}

// VM is the Zcash Bridge VM.  It embeds Zcash block data into an Avalanche
// linear chain via Snowman consensus.
type VM struct {
	ctx     *snow.Context
	db      database.Database
	state   *State
	mempool *Mempool
	zcash   zcash.Client
	log     logging.Logger

	// Internal notification channel — WaitForEvent blocks on this.
	pendingMsgs chan common.Message

	// Consensus bookkeeping.
	preferred    ids.ID
	lastAccepted ids.ID

	// Configuration.
	config Config

	// Prometheus metrics.
	metrics *Metrics

	// stopCh signals background goroutines to exit on Shutdown.
	stopCh chan struct{}

	// appSender is used to gossip (height, wallet) pairs to peer validators so
	// that all validators can sign the outbound warp message in Accept().
	appSender common.AppSender

	// outboundWarpMsgs holds unsigned warp messages that this node must sign
	// when queried by the ICM signature aggregator.
	outboundWarpMsgs   map[ids.ID]outboundMsgEntry
	outboundWarpMsgsMu sync.RWMutex

	// uptimeTracker measures L1 validator uptime so this node can sign the
	// ValidationUptimeMessage proofs a PoS StakingManager needs to pay staking
	// rewards. Nil when the chain context provides no Warp signer (unit tests).
	uptimeTracker *uptimeTracker

	// p2pNetwork routes ACP-118 signature requests to the uptime verifier.
	// Nil when there is no AppSender (unit tests).
	p2pNetwork *p2p.Network

	mu sync.RWMutex
}

// -------------------- VM Lifecycle --------------------

// Initialize sets up the VM with the provided context, database, and
// configuration.  It creates the genesis block if this is a fresh chain.
//
// AvalancheGo v1.15.0 common.VM interface.
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	vm.ctx = chainCtx
	vm.db = db
	vm.log = chainCtx.Log
	vm.mempool = NewMempool()
	vm.state = NewState(db, vm.log)
	vm.pendingMsgs = make(chan common.Message, 64)
	vm.appSender = appSender
	vm.outboundWarpMsgs = make(map[ids.ID]outboundMsgEntry)

	// Parse configuration.
	vm.config = DefaultConfig()
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &vm.config); err != nil {
			return fmt.Errorf("parse VM config: %w", err)
		}
	}
	vm.config.applyDefaults()

	// Initialize Zcash client (can be overridden in tests).
	if vm.zcash == nil {
		vm.zcash = zcash.NewRPCClient(
			vm.config.ZcashRPCEndpoint,
			vm.config.ZcashRPCUser,
			vm.config.ZcashRPCPassword,
		)
	}

	// Initialize Prometheus metrics.
	if chainCtx.Metrics != nil {
		reg, err := metrics.MakeAndRegister(chainCtx.Metrics, "zcash_bridge")
		if err != nil {
			vm.log.Warn("failed to register metrics gatherer, continuing without",
				zap.Error(err))
			vm.metrics, _ = NewMetrics(nil)
		} else {
			vm.metrics, err = NewMetrics(reg)
			if err != nil {
				vm.log.Warn("failed to register metrics, continuing without",
					zap.Error(err))
				vm.metrics, _ = NewMetrics(nil)
			}
		}
	} else {
		vm.metrics, _ = NewMetrics(nil)
	}

	// Bootstrap: load or create genesis.
	lastAcceptedID, err := vm.state.GetLastAccepted()
	if errors.Is(err, database.ErrNotFound) {
		if err := vm.createGenesis(); err != nil {
			return fmt.Errorf("create genesis: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("load last accepted: %w", err)
	} else {
		vm.lastAccepted = lastAcceptedID
		vm.preferred = lastAcceptedID
	}

	vm.stopCh = make(chan struct{})

	// Restore any outbound warp messages that were persisted before shutdown.
	vm.loadOutboundWarpMsgs()

	// Set up validator uptime tracking and ACP-118 warp signing. A failure
	// here is logged and tolerated: the oracle's core job (bridging Zcash
	// heights) does not depend on it, and refusing to boot the chain over a
	// rewards feature would be a far worse outcome.
	if err := vm.initUptimeTracking(chainCtx, appSender); err != nil {
		vm.log.Warn("uptime tracking disabled", zap.Error(err))
	}

	// Start a background watchdog that re-notifies the engine if the
	// mempool has pending items that haven't been picked up.  This
	// protects against lost notifications (channel full) or transient
	// consensus stalls.
	go vm.mempoolWatchdog()

	vm.log.Info("VM initialized",
		zap.String("name", Name),
		zap.String("version", VMVersion),
		zap.Stringer("lastAccepted", vm.lastAccepted),
		zap.Uint64("confirmationDepth", vm.config.ConfirmationDepth),
		zap.String("zcashRPCEndpoint", vm.config.ZcashRPCEndpoint),
		zap.String("chainID", vm.ctx.ChainID.String()),
	)
	return nil
}

// createGenesis builds and persists the genesis block (height 0).
func (vm *VM) createGenesis() error {
	genesis := &Block{
		PrntID:      ids.Empty,
		Hght:        0,
		Tmstmp:      0,
		ZcashHeight: 0,
		ZcashHash:   "",
		status:      choices.Accepted,
		vm:          vm,
	}
	if err := initBlock(genesis); err != nil {
		return err
	}

	if err := vm.state.PutBlock(genesis); err != nil {
		return err
	}
	if err := vm.state.SetLastAccepted(genesis.id); err != nil {
		return err
	}

	vm.lastAccepted = genesis.id
	vm.preferred = genesis.id
	vm.log.Info("genesis block created", zap.Stringer("id", genesis.id))
	return nil
}

// SetState is called when the engine transitions between states (e.g.,
// Bootstrapping → NormalOp).
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	vm.log.Info("VM state changed", zap.Stringer("state", state))

	// Only start accruing uptime once bootstrapping is done. Before that the
	// P-Chain is not reliably queryable and peer connections churn, so any
	// uptime measured would be meaningless.
	if state == snow.NormalOp {
		if err := vm.uptimeTracker.start(ctx); err != nil {
			vm.log.Warn("failed to start uptime tracking", zap.Error(err))
		}
	}
	return nil
}

// Shutdown cleanly stops the VM and closes its database.
func (vm *VM) Shutdown(_ context.Context) error {
	vm.log.Info("VM shutting down")
	if vm.stopCh != nil {
		close(vm.stopCh)
	}
	// Flush accrued uptime before the database closes, or validators lose
	// credit for the interval since the last periodic flush.
	vm.uptimeTracker.stop()
	if vm.db != nil {
		return vm.db.Close()
	}
	return nil
}

// Version returns the VM's semantic version.
func (vm *VM) Version(_ context.Context) (string, error) {
	return VMVersion, nil
}

// CreateHandlers returns the HTTP handlers mounted at the VM's API endpoint.
func (vm *VM) CreateHandlers(_ context.Context) (map[string]http.Handler, error) {
	vm.log.Info("CreateHandlers called — registering /rpc handler",
		zap.String("chainID", vm.ctx.ChainID.String()),
	)
	handler := NewHTTPHandler(vm)
	vm.log.Debug("CreateHandlers returning handler",
		zap.String("path", "/rpc"),
		zap.String("handlerType", fmt.Sprintf("%T", handler)),
	)
	return map[string]http.Handler{
		"/rpc":  handler,
		"/warp": newWarpHTTPHandler(vm),
		// Mounted at /ext/bc/<blockchainID>/validators. Signature aggregators
		// and the Builder Console read validator uptime from here before
		// requesting an uptime proof.
		"/validators": newValidatorsHTTPHandler(vm),
	}, nil
}

// NewHTTPHandler returns a combined HTTP handler for the VM (v1.15.0 interface).
func (vm *VM) NewHTTPHandler(_ context.Context) (http.Handler, error) {
	vm.log.Info("NewHTTPHandler called (v1.15.0 interface)",
		zap.String("chainID", vm.ctx.ChainID.String()),
	)
	return NewHTTPHandler(vm), nil
}

// WaitForEvent blocks until a message is available or the context is cancelled.
// This replaces the old toEngine channel pattern in AvalancheGo v1.15.0.
func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	vm.log.Debug("WaitForEvent: waiting for engine message",
		zap.Int("pendingChanLen", len(vm.pendingMsgs)),
	)
	select {
	case msg := <-vm.pendingMsgs:
		vm.log.Debug("WaitForEvent: received message",
			zap.Stringer("message", msg),
			zap.Int("remainingChanLen", len(vm.pendingMsgs)),
		)
		return msg, nil
	case <-ctx.Done():
		vm.log.Debug("WaitForEvent: context cancelled",
			zap.Error(ctx.Err()),
		)
		return 0, ctx.Err()
	}
}

// HealthCheck reports whether the VM is healthy.
func (vm *VM) HealthCheck(_ context.Context) (interface{}, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return map[string]interface{}{
		"lastAccepted": vm.lastAccepted.String(),
		"mempoolSize":  vm.mempool.Len(),
	}, nil
}

// -------------------- Snowman Consensus --------------------

// BuildBlock constructs the next block from the mempool.  Called by the
// engine after receiving a PendingTxs notification.
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vm.log.Info("BuildBlock: called by engine",
		zap.Int("mempoolSize", vm.mempool.Len()),
		zap.Stringer("preferred", vm.preferred),
		zap.Stringer("lastAccepted", vm.lastAccepted),
	)

	// Pop the next pending Zcash height.
	height, ok := vm.mempool.Pop()
	if !ok {
		vm.log.Debug("BuildBlock: mempool empty, nothing to build")
		return nil, errNoPendingBlocks
	}

	vm.log.Info("BuildBlock: popped height from mempool",
		zap.Uint64("zcashHeight", height),
		zap.Int("mempoolRemaining", vm.mempool.Len()),
	)

	// Double-check: height may have been accepted while queued.
	if vm.state.HasZcashHeight(height) {
		vm.mempool.Remove(height)
		vm.log.Info("BuildBlock: height already accepted, skipping",
			zap.Uint64("zcashHeight", height),
		)
		if vm.metrics != nil {
			vm.metrics.duplicateRequests.Inc()
		}
		// If there are more items in the mempool, re-notify the engine
		// so it calls BuildBlock again to drain the queue.
		if vm.mempool.Len() > 0 {
			vm.notifyEngine()
		}
		return nil, fmt.Errorf("%w: %d", errAlreadyAccepted, height)
	}

	// Fetch Zcash block data.
	vm.log.Debug("BuildBlock: fetching zcash block data",
		zap.Uint64("zcashHeight", height),
	)
	fetchStart := time.Now()
	info, err := vm.zcash.GetBlockByHeight(ctx, height)
	if err != nil {
		// Re-enqueue on transient failure so a future BuildBlock can retry.
		// Must use Requeue (not Push) because Pop leaves the height in the
		// seen set, which would cause Push to silently discard it.
		vm.mempool.Requeue(height)
		vm.notifyEngine()
		vm.log.Warn("BuildBlock: zcash fetch failed, re-enqueued for retry",
			zap.Uint64("zcashHeight", height),
			zap.Int("mempoolSize", vm.mempool.Len()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("fetch zcash block %d: %w", height, err)
	}
	fetchDuration := time.Since(fetchStart)
	vm.log.Debug("BuildBlock: zcash block fetched successfully",
		zap.Uint64("zcashHeight", height),
		zap.String("zcashHash", info.Hash),
		zap.Int64("zcashTime", info.Time),
		zap.Int("rawDataLen", len(info.RawData)),
		zap.Duration("fetchDuration", fetchDuration),
	)
	if vm.metrics != nil {
		vm.metrics.zcashFetchDuration.Observe(fetchDuration.Seconds())
	}

	// Load parent block.
	vm.log.Debug("BuildBlock: loading parent block",
		zap.Stringer("preferred", vm.preferred),
	)
	parent, err := vm.getBlockLocked(vm.preferred)
	if err != nil {
		vm.mempool.Requeue(height)
		vm.notifyEngine()
		vm.log.Warn("BuildBlock: parent load failed, re-enqueued for retry",
			zap.Uint64("zcashHeight", height),
			zap.Stringer("preferred", vm.preferred),
			zap.Int("mempoolSize", vm.mempool.Len()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("load parent %s: %w", vm.preferred, err)
	}

	vm.log.Debug("BuildBlock: parent loaded",
		zap.Stringer("parentID", parent.id),
		zap.Uint64("parentHeight", parent.Hght),
		zap.Int64("parentTimestamp", parent.Tmstmp),
	)

	// Construct deterministic block.
	blk := &Block{
		PrntID:      vm.preferred,
		Hght:        parent.Hght + 1,
		Tmstmp:      deterministicTimestamp(parent.Tmstmp, info.Time),
		ZcashHeight: height,
		ZcashHash:   info.Hash,
		ZcashData:   info.RawData,
		status:      choices.Processing,
		vm:          vm,
	}
	if err := initBlock(blk); err != nil {
		vm.mempool.Requeue(height)
		vm.notifyEngine()
		vm.log.Error("BuildBlock: initBlock failed, re-enqueued for retry",
			zap.Uint64("zcashHeight", height),
			zap.Error(err),
		)
		return nil, fmt.Errorf("init block: %w", err)
	}

	// Cache for later GetBlock / ParseBlock calls.
	vm.state.CacheBlock(blk)

	vm.log.Info("BuildBlock: block built successfully",
		zap.Stringer("blockID", blk.id),
		zap.Uint64("avaxHeight", blk.Hght),
		zap.Uint64("zcashHeight", blk.ZcashHeight),
		zap.String("zcashHash", blk.ZcashHash),
		zap.Int64("timestamp", blk.Tmstmp),
		zap.Int("blockBytes", len(blk.bytes)),
		zap.Int("mempoolRemaining", vm.mempool.Len()),
	)
	if vm.metrics != nil {
		vm.metrics.blocksBuilt.Inc()
	}

	// Re-notify the engine so it advances consensus on the just-built block
	// (and picks up any remaining mempool items). Without this the engine
	// blocks on WaitForEvent after BuildBlock returns and Accept is never
	// called until the next external API request fires a notification.
	vm.notifyEngine()

	return blk, nil
}

// GetBlock retrieves a block by its Avalanche ID.
func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.getBlockLocked(blkID)
}

func (vm *VM) getBlockLocked(blkID ids.ID) (*Block, error) {
	blk, err := vm.state.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	blk.vm = vm
	return blk, nil
}

// getBlock is used from outside the lock (block.go Verify).
func (vm *VM) getBlock(blkID ids.ID) (*Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.getBlockLocked(blkID)
}

// GetBlockIDAtHeight returns the block ID accepted at the given Avalanche
// chain height.  Required by block.ChainVM in AvalancheGo v1.15.0.
func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	blk, err := vm.getBlockLocked(vm.lastAccepted)
	if err != nil {
		return ids.Empty, err
	}

	for blk.Hght > height {
		blk, err = vm.getBlockLocked(blk.PrntID)
		if err != nil {
			return ids.Empty, database.ErrNotFound
		}
	}

	if blk.Hght == height {
		return blk.id, nil
	}
	return ids.Empty, database.ErrNotFound
}

// ParseBlock deserializes a block received from another node.
func (vm *VM) ParseBlock(_ context.Context, blockBytes []byte) (snowman.Block, error) {
	vm.log.Debug("ParseBlock: called",
		zap.Int("bytesLen", len(blockBytes)),
	)

	blk, err := UnmarshalBlock(blockBytes)
	if err != nil {
		vm.log.Error("ParseBlock: unmarshal failed",
			zap.Int("bytesLen", len(blockBytes)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("parse block: %w", err)
	}
	blk.vm = vm
	blk.status = choices.Processing

	// If we already have this block, use the existing status.
	if existing, existErr := vm.state.GetBlock(blk.id); existErr == nil {
		vm.log.Debug("ParseBlock: found existing block in state",
			zap.Stringer("blockID", blk.id),
			zap.Stringer("existingStatus", existing.status),
		)
		blk.status = existing.status
	}

	vm.log.Info("ParseBlock: block parsed",
		zap.Stringer("blockID", blk.id),
		zap.Uint64("avaxHeight", blk.Hght),
		zap.Uint64("zcashHeight", blk.ZcashHeight),
		zap.String("zcashHash", blk.ZcashHash),
		zap.Stringer("status", blk.status),
	)

	// Cache for subsequent operations.
	vm.state.CacheBlock(blk)
	return blk, nil
}

// SetPreference is called by the engine to set the preferred block tip.
func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.mu.Lock()
	oldPreferred := vm.preferred
	vm.preferred = blkID
	vm.mu.Unlock()
	vm.log.Info("SetPreference: preference updated",
		zap.Stringer("oldPreferred", oldPreferred),
		zap.Stringer("newPreferred", blkID),
	)
	return nil
}

// LastAccepted returns the ID of the last accepted block.
func (vm *VM) LastAccepted(_ context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.lastAccepted, nil
}

// -------------------- Networking --------------------
// AppRequest/AppResponse carry ACP-118 signature requests, which is how a
// signature aggregator asks this node to sign a warp message. AppGossip stays
// on the VM's own (unprefixed) wallet-gossip format and is deliberately NOT
// routed through the p2p router, which would not understand it.

// initUptimeTracking wires up validator uptime measurement and the ACP-118
// signature handler. Both are optional: if the chain context has no Warp
// signer or no AppSender (as in unit tests), the VM runs without them.
func (vm *VM) initUptimeTracking(chainCtx *snow.Context, appSender common.AppSender) error {
	tracker, err := newUptimeTracker(
		prefixdb.New(uptimePrefix, vm.db),
		vm.log,
		chainCtx.NodeID,
		chainCtx.SubnetID,
		chainCtx.ValidatorState,
	)
	if err != nil {
		return fmt.Errorf("create uptime tracker: %w", err)
	}
	vm.uptimeTracker = tracker

	if appSender == nil || chainCtx.WarpSigner == nil {
		// Uptime is still measured and served over the validators API; only
		// p2p signature serving is unavailable.
		vm.log.Info("ACP-118 signature handler not registered (no appSender or warp signer)")
		return nil
	}

	// p2p metrics get their own namespace on the chain gatherer. It must not
	// be a prefix of, nor be prefixed by, any other namespace registered there
	// — avalanchego rejects overlapping namespaces outright, so "zcash_bridge"
	// (registered in Initialize) rules out anything starting with it.
	// A registration failure is not worth losing signature serving over, so
	// fall back to a throwaway registry and carry on without the metrics.
	registerer, err := metrics.MakeAndRegister(chainCtx.Metrics, p2pMetricsNamespace)
	if err != nil {
		vm.log.Warn("failed to register p2p metrics, continuing without", zap.Error(err))
		registerer = prometheus.NewRegistry()
	}

	network, err := p2p.NewNetwork(vm.log, appSender, registerer, p2pMetricsNamespace)
	if err != nil {
		return fmt.Errorf("create p2p network: %w", err)
	}

	// Signature requests come from unauthenticated peers — a signature
	// aggregator need not be a validator — and each one costs a BLS signature.
	// Throttle per peer so this cannot be turned into a cheap CPU drain. The
	// limit is far above what honest aggregation needs: a removal collects one
	// signature per validator, not hundreds per minute.
	handler := p2p.NewThrottlerHandler(
		acp118.NewHandler(&uptimeVerifier{vm: vm}, chainCtx.WarpSigner),
		p2p.NewSlidingWindowThrottler(time.Minute, signatureRequestsPerMinute),
		vm.log,
	)
	if err := network.AddHandler(acp118.HandlerID, handler); err != nil {
		return fmt.Errorf("register ACP-118 handler: %w", err)
	}
	vm.p2pNetwork = network

	vm.log.Info("ACP-118 signature handler registered",
		zap.Uint64("handlerID", acp118.HandlerID),
	)
	return nil
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, appVersion *version.Application) error {
	vm.uptimeTracker.connect(nodeID)
	if vm.p2pNetwork != nil {
		return vm.p2pNetwork.Connected(ctx, nodeID, appVersion)
	}
	return nil
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.uptimeTracker.disconnect(nodeID)
	if vm.p2pNetwork != nil {
		return vm.p2pNetwork.Disconnected(ctx, nodeID)
	}
	return nil
}

func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	if vm.p2pNetwork == nil {
		return nil
	}
	return vm.p2pNetwork.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	if vm.p2pNetwork == nil {
		return nil
	}
	return vm.p2pNetwork.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if vm.p2pNetwork == nil {
		return nil
	}
	return vm.p2pNetwork.AppResponse(ctx, nodeID, requestID, response)
}

// AppGossip receives a (zcashHeight, wallet) pair gossiped by the validator
// that processed the inbound warp message.  Storing the wallet here ensures
// every validator can emit the outbound warp message in Accept().
func (vm *VM) AppGossip(_ context.Context, _ ids.NodeID, msgBytes []byte) error {
	height, wallet, ok := parseWalletGossip(msgBytes)
	if !ok {
		return nil // ignore unrecognised gossip
	}
	if err := vm.state.PutWalletAddr(height, wallet); err != nil {
		vm.log.Warn("AppGossip: failed to store wallet addr",
			zap.Uint64("zcashHeight", height),
			zap.Error(err),
		)
	}
	return nil
}

// -------------------- Outbound warp helpers --------------------

// emitOutboundWarpMessage creates an unsigned warp message carrying
// abi.encode(address wallet, uint256 zcashHeight), persists it to LevelDB
// (so it survives VM restarts), and stores it in the in-memory outbox.
// avaxHeight is the Avalanche chain height of the accepted block — it becomes
// the cursor the ICM relayer uses to skip already-delivered messages.
func (vm *VM) emitOutboundWarpMessage(zcashHeight uint64, wallet [20]byte, avaxHeight uint64) {
	abiData := OutboundPayload{Wallet: wallet, ZcashHeight: zcashHeight}.Encode()

	// C-Chain warp precompile expects AddressedCall codec format (type ID 1).
	// Without this wrapping the precompile misidentifies the payload as type 0
	// (Hash) and rejects the transaction with "trailing buffer space".
	ac, err := payload.NewAddressedCall(nil, abiData)
	if err != nil {
		vm.log.Error("emitOutboundWarpMessage: failed to create AddressedCall payload",
			zap.Uint64("zcashHeight", zcashHeight),
			zap.Error(err),
		)
		return
	}

	msg, err := warp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.ChainID, ac.Bytes())
	if err != nil {
		vm.log.Error("emitOutboundWarpMessage: failed to create unsigned warp message",
			zap.Uint64("zcashHeight", zcashHeight),
			zap.Error(err),
		)
		return
	}

	msgID := msg.ID()

	if err := vm.state.PutOutboundWarpMsg(avaxHeight, msgID, msg.Bytes()); err != nil {
		vm.log.Warn("emitOutboundWarpMessage: failed to persist to LevelDB",
			zap.Uint64("avaxHeight", avaxHeight),
			zap.Error(err),
		)
	}

	vm.outboundWarpMsgsMu.Lock()
	vm.outboundWarpMsgs[msgID] = outboundMsgEntry{msg: msg, avaxHeight: avaxHeight}
	vm.outboundWarpMsgsMu.Unlock()

	vm.log.Info("emitOutboundWarpMessage: outbound warp message queued for signing",
		zap.String("messageID", hex.EncodeToString(msgID[:])),
		zap.Uint64("zcashHeight", zcashHeight),
		zap.Uint64("avaxHeight", avaxHeight),
	)
}

// loadOutboundWarpMsgs restores persisted outbound warp messages from LevelDB
// into the in-memory outbox on VM startup.
func (vm *VM) loadOutboundWarpMsgs() {
	records, err := vm.state.GetOutboundWarpMsgsSince(0)
	if err != nil {
		vm.log.Warn("loadOutboundWarpMsgs: failed to read from LevelDB", zap.Error(err))
		return
	}
	vm.outboundWarpMsgsMu.Lock()
	defer vm.outboundWarpMsgsMu.Unlock()
	for _, rec := range records {
		msg, err := warp.ParseUnsignedMessage(rec.MsgBytes)
		if err != nil {
			vm.log.Warn("loadOutboundWarpMsgs: skip unparseable record",
				zap.Uint64("avaxHeight", rec.AvaxHeight),
				zap.Error(err),
			)
			continue
		}
		vm.outboundWarpMsgs[rec.MsgID] = outboundMsgEntry{msg: msg, avaxHeight: rec.AvaxHeight}
	}
	vm.log.Info("loadOutboundWarpMsgs: restored outbound messages",
		zap.Int("count", len(records)),
	)
}

// gossipWallet broadcasts a (zcashHeight, wallet) pair to all connected
// validators so they can store the wallet before Accept() fires.
func (vm *VM) gossipWallet(ctx context.Context, zcashHeight uint64, wallet [20]byte) {
	if vm.appSender == nil {
		return // unit tests inject nil appSender
	}
	gossipBytes := makeWalletGossip(zcashHeight, wallet)
	if err := vm.appSender.SendAppGossip(ctx, common.SendConfig{Validators: math.MaxInt}, gossipBytes); err != nil {
		vm.log.Warn("gossipWallet: SendAppGossip failed",
			zap.Uint64("zcashHeight", zcashHeight),
			zap.Error(err),
		)
	}
}

// walletGossipLen is the fixed byte length of a wallet gossip message.
const walletGossipLen = 28 // 8 (height) + 20 (wallet)

// makeWalletGossip serialises a (zcashHeight, wallet) pair for AppGossip.
func makeWalletGossip(zcashHeight uint64, wallet [20]byte) []byte {
	buf := make([]byte, walletGossipLen)
	binary.BigEndian.PutUint64(buf[0:8], zcashHeight)
	copy(buf[8:28], wallet[:])
	return buf
}

// parseWalletGossip deserialises a wallet gossip message.  Returns false if
// the bytes don't match the expected format.
func parseWalletGossip(data []byte) (uint64, [20]byte, bool) {
	if len(data) != walletGossipLen {
		return 0, [20]byte{}, false
	}
	height := binary.BigEndian.Uint64(data[0:8])
	var wallet [20]byte
	copy(wallet[:], data[8:28])
	return height, wallet, true
}

// -------------------- Helpers --------------------

// deterministicTimestamp computes a block timestamp that is:
//  1. Strictly greater than the parent's timestamp (monotonicity).
//  2. At least as large as the Zcash block's timestamp (meaningful).
//  3. Fully deterministic – no time.Now() calls.
func deterministicTimestamp(parentTimestamp, zcashTimestamp int64) int64 {
	next := parentTimestamp + 1
	if zcashTimestamp > next {
		return zcashTimestamp
	}
	return next
}

// mempoolWatchdog runs in the background and periodically checks if the
// mempool has pending items.  If it does, it re-sends a PendingTxs
// notification to the engine.  This is the safety net that prevents items
// from being stuck forever when a notification is dropped or BuildBlock
// fails without requeuing.
func (vm *VM) mempoolWatchdog() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-vm.stopCh:
			vm.log.Info("mempoolWatchdog: stopping")
			return
		case <-ticker.C:
			if vm.mempool.Len() > 0 {
				vm.log.Info("mempoolWatchdog: mempool non-empty, re-notifying engine",
					zap.Int("mempoolSize", vm.mempool.Len()),
				)
				vm.notifyEngine()
			}
		}
	}
}

// notifyEngine sends a PendingTxs message to the internal channel, which
// WaitForEvent will deliver to the consensus engine.  Non-blocking.
func (vm *VM) notifyEngine() {
	select {
	case vm.pendingMsgs <- common.PendingTxs:
		vm.log.Debug("notifyEngine: PendingTxs sent to engine channel")
	default:
		// Channel full – engine will call BuildBlock eventually.
		vm.log.Debug("notifyEngine: channel full, engine will pick up pending work later",
			zap.Int("channelCap", cap(vm.pendingMsgs)),
		)
	}
}

package vm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// uptimeSyncInterval is how often the tracker refreshes the L1 validator set
// from the P-Chain and flushes accrued uptime to disk. The P-Chain caches
// validator-set lookups, and a flush is a handful of small writes, so this is
// cheap enough to run indefinitely while still bounding how much uptime is
// lost if the process is killed without a clean shutdown.
const uptimeSyncInterval = time.Minute

// validatorInfo is the tracker's view of one L1 validator.
type validatorInfo struct {
	validationID  ids.ID
	nodeID        ids.NodeID
	weight        uint64
	startTime     time.Time
	isActive      bool
	isL1Validator bool
}

// uptimeTracker measures how long each L1 validator has been reachable from
// this node, and answers "has nodeID really been up for N seconds?" when the
// ACP-118 signature handler is asked to sign a ValidationUptimeMessage.
//
// # Why this exists
//
// A PoS StakingManager pays staking rewards only against an uptime proof
// signed by the L1's own validators. Without this tracker the VM has no uptime
// to attest to, every proof request is refused, and every validator's rewards
// are unclaimable.
//
// # Concurrency
//
// avalanchego's uptime.Manager is not thread-safe, and neither is uptimeState.
// Every exported method here takes mu for the whole call, which makes this
// type the single serialisation point for both. Keep it that way: never call
// into mgr or state from outside a locked method.
//
// All methods tolerate a nil receiver so callers on the VM's hot paths
// (Connected/Disconnected) need no nil checks. Unit tests construct a VM
// without a validator state, and those paths must stay working.
type uptimeTracker struct {
	log        logging.Logger
	selfNodeID ids.NodeID
	subnetID   ids.ID

	// validatorState reads the current L1 validator set from the P-Chain. It
	// is nil in unit tests, in which case the tracker records connectivity but
	// never learns of any validators.
	validatorState validators.State

	mu    sync.Mutex
	clock mockable.Clock
	mgr   uptime.Manager
	state *uptimeState

	// byNodeID / byValidationID are the two views of the current validator set.
	byNodeID       map[ids.NodeID]validatorInfo
	byValidationID map[ids.ID]ids.NodeID

	// connected is raw network connectivity as reported by the consensus
	// engine, before any validator filtering.
	connected map[ids.NodeID]struct{}

	// counting is the set of nodes uptime.Manager currently treats as up. It
	// mirrors the manager's internal connection set, which the Tracker
	// interface does not expose.
	counting map[ids.NodeID]struct{}

	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// newUptimeTracker builds a tracker over the VM's database. The returned
// tracker records connectivity immediately but does not accrue uptime until
// start is called, which happens when consensus reaches normal operation.
func newUptimeTracker(
	db database.Database,
	log logging.Logger,
	selfNodeID ids.NodeID,
	subnetID ids.ID,
	validatorState validators.State,
) (*uptimeTracker, error) {
	state, err := newUptimeState(db)
	if err != nil {
		return nil, fmt.Errorf("open uptime state: %w", err)
	}

	t := &uptimeTracker{
		log:            log,
		selfNodeID:     selfNodeID,
		subnetID:       subnetID,
		validatorState: validatorState,
		state:          state,
		byNodeID:       make(map[ids.NodeID]validatorInfo),
		byValidationID: make(map[ids.ID]ids.NodeID),
		connected:      make(map[ids.NodeID]struct{}),
		counting:       make(map[ids.NodeID]struct{}),
		stopCh:         make(chan struct{}),
	}
	t.mgr = uptime.NewManager(state, &t.clock)

	// This node is trivially reachable from itself: if it were not running it
	// could not be signing anything. The consensus engine never reports a
	// connection to the local node, so seed it here or this node would report
	// its own uptime as zero and refuse to sign its own proof.
	t.connected[selfNodeID] = struct{}{}

	return t, nil
}

// start begins accruing uptime and launches the background refresh loop. It is
// called on the transition to normal operation, once the P-Chain is queryable.
func (t *uptimeTracker) start(ctx context.Context) error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	// Learn the validator set before we start counting, so validators are
	// marked up from the moment tracking begins rather than from the first
	// refresh a minute later.
	if err := t.refresh(ctx); err != nil {
		// A P-Chain hiccup at startup must not stop the VM from booting. The
		// background loop retries every uptimeSyncInterval.
		t.log.Warn("uptime: initial validator refresh failed, will retry",
			zap.Error(err),
		)
	}

	t.mu.Lock()

	// Refuse to credit the window this node was not running. Without this,
	// StartTracking would hand every validator the full span since the last
	// flush — so a one-minute restart would silently add a minute of uptime
	// that nobody observed.
	if err := t.state.discardUnobservedGap(t.clock.Time()); err != nil {
		t.log.Warn("uptime: failed to discard unobserved gap", zap.Error(err))
	}

	if err := t.mgr.StartTracking(t.state.trackedNodeIDs()); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("start uptime tracking: %w", err)
	}
	t.started = true
	// Re-apply connectivity so counting reflects who is actually reachable
	// right now rather than who was reachable before the restart.
	for nodeID := range t.byNodeID {
		t.applyConnectivity(nodeID)
	}
	t.mu.Unlock()

	t.wg.Add(1)
	go t.refreshLoop()

	t.log.Info("uptime: tracking started",
		zap.Int("validators", len(t.byNodeID)),
	)
	return nil
}

// stop halts the refresh loop and flushes accrued uptime to disk. Safe to call
// even if start was never reached.
func (t *uptimeTracker) stop() {
	if t == nil {
		return
	}

	select {
	case <-t.stopCh:
		// Already stopped.
	default:
		close(t.stopCh)
	}
	t.wg.Wait()

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return
	}
	if err := t.mgr.StopTracking(t.state.trackedNodeIDs()); err != nil {
		t.log.Warn("uptime: failed to flush on shutdown", zap.Error(err))
	}
	t.started = false
}

// connect records that the consensus engine has connected to nodeID.
func (t *uptimeTracker) connect(nodeID ids.NodeID) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected[nodeID] = struct{}{}
	t.applyConnectivity(nodeID)
}

// disconnect records that the consensus engine has lost nodeID.
func (t *uptimeTracker) disconnect(nodeID ids.NodeID) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.connected, nodeID)
	t.applyConnectivity(nodeID)
}

// uptimeSeconds returns this node's measured uptime for a validation, in whole
// seconds, along with the validator it belongs to.
func (t *uptimeTracker) uptimeSeconds(validationID ids.ID) (uint64, validatorInfo, error) {
	if t == nil {
		return 0, validatorInfo{}, fmt.Errorf("uptime tracking is not enabled")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	nodeID, ok := t.byValidationID[validationID]
	if !ok {
		return 0, validatorInfo{}, fmt.Errorf("unknown validationID %s", validationID)
	}

	// Defence in depth: only ever answer with uptime that is stored against
	// the exact validation being asked about. refresh keeps the record and the
	// index in step, so a mismatch means something is wrong — and the safe
	// response to "something is wrong" is to refuse to attest.
	if storedID, ok := t.state.validationIDOf(nodeID); !ok || storedID != validationID {
		return 0, validatorInfo{}, fmt.Errorf(
			"no uptime recorded for validationID %s on %s", validationID, nodeID,
		)
	}

	seconds, err := t.calculateSeconds(nodeID)
	if err != nil {
		return 0, validatorInfo{}, err
	}
	return seconds, t.byNodeID[nodeID], nil
}

// validatorSnapshot returns every tracked validator together with this node's
// current view of its uptime. Used by the validators API.
type validatorSnapshot struct {
	validatorInfo
	uptimeSeconds    uint64
	uptimePercentage float64
	isConnected      bool
}

func (t *uptimeTracker) validatorSnapshot() []validatorSnapshot {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]validatorSnapshot, 0, len(t.byNodeID))
	for nodeID, info := range t.byNodeID {
		seconds, err := t.calculateSeconds(nodeID)
		if err != nil {
			t.log.Debug("uptime: skipping validator with no record",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		percentage := 100.0
		if elapsed := t.clock.Time().Sub(info.startTime).Seconds(); elapsed > 0 {
			percentage = float64(seconds) / elapsed * 100
			if percentage > 100 {
				percentage = 100
			}
		}

		_, connected := t.connected[nodeID]
		out = append(out, validatorSnapshot{
			validatorInfo:    info,
			uptimeSeconds:    seconds,
			uptimePercentage: percentage,
			isConnected:      connected,
		})
	}
	return out
}

// -------------------- internals (caller must hold mu) --------------------

// calculateSeconds returns the tracked uptime for nodeID, truncated down to
// whole seconds. Truncating down keeps this node from ever attesting to more
// uptime than it actually observed.
func (t *uptimeTracker) calculateSeconds(nodeID ids.NodeID) (uint64, error) {
	duration, _, err := t.mgr.CalculateUptime(nodeID)
	if err != nil {
		return 0, err
	}
	if duration < 0 {
		return 0, nil
	}
	return uint64(duration / time.Second), nil
}

// applyConnectivity reconciles uptime.Manager's notion of who is up with ours.
//
// A validator counts as up only when it is BOTH reachable on the network AND
// active on the P-Chain. An L1 validator goes inactive when its continuous-fee
// balance runs out; it is still a peer, but it is no longer validating, so
// crediting it uptime would overstate the rewards it earned.
func (t *uptimeTracker) applyConnectivity(nodeID ids.NodeID) {
	info, isValidator := t.byNodeID[nodeID]
	_, isConnected := t.connected[nodeID]
	_, isCounting := t.counting[nodeID]

	shouldCount := isValidator && isConnected && info.isActive

	switch {
	case shouldCount && !isCounting:
		if err := t.mgr.Connect(nodeID); err != nil {
			t.log.Warn("uptime: failed to mark node up",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			return
		}
		t.counting[nodeID] = struct{}{}

	case !shouldCount && isCounting:
		// Disconnect flushes the accrued interval into the stored uptime.
		if err := t.mgr.Disconnect(nodeID); err != nil {
			t.log.Warn("uptime: failed to mark node down",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			return
		}
		delete(t.counting, nodeID)
	}
}

// refreshLoop periodically re-reads the validator set and flushes uptime.
func (t *uptimeTracker) refreshLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(uptimeSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), uptimeSyncInterval)
			if err := t.refresh(ctx); err != nil {
				t.log.Warn("uptime: validator refresh failed", zap.Error(err))
			}
			cancel()
			t.flush()
		}
	}
}

// refresh reloads the L1 validator set from the P-Chain and reconciles it with
// what the tracker is counting.
func (t *uptimeTracker) refresh(ctx context.Context) error {
	if t.validatorState == nil {
		return nil // unit tests, or a chain context without P-Chain access
	}

	current, _, err := t.validatorState.GetCurrentValidatorSet(ctx, t.subnetID)
	if err != nil {
		return fmt.Errorf("get current validator set: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock.Time()
	byNodeID := make(map[ids.NodeID]validatorInfo, len(current))
	byValidationID := make(map[ids.ID]ids.NodeID, len(current))

	for validationID, v := range current {
		info := validatorInfo{
			validationID:  validationID,
			nodeID:        v.NodeID,
			weight:        v.Weight,
			startTime:     time.Unix(int64(v.StartTime), 0),
			isActive:      v.IsActive,
			isL1Validator: v.IsL1Validator,
		}
		byNodeID[v.NodeID] = info
		byValidationID[validationID] = v.NodeID

		// Passing the validationID lets the state reset uptime when a node
		// re-registers under a new validation, rather than carrying the old
		// validation's hours forward.
		if err := t.state.ensureValidator(v.NodeID, validationID, info.startTime, now); err != nil {
			t.log.Warn("uptime: failed to create record",
				zap.Stringer("nodeID", v.NodeID),
				zap.Error(err),
			)
		}
	}

	// Nodes that dropped out of the set must stop accruing uptime. Their
	// records are kept so a final proof can still be produced mid-removal.
	departed := make([]ids.NodeID, 0)
	for nodeID := range t.byNodeID {
		if _, stillValidating := byNodeID[nodeID]; !stillValidating {
			departed = append(departed, nodeID)
		}
	}

	t.byNodeID = byNodeID
	t.byValidationID = byValidationID

	if !t.started {
		return nil
	}
	for nodeID := range byNodeID {
		t.applyConnectivity(nodeID)
	}
	for _, nodeID := range departed {
		t.applyConnectivity(nodeID)
	}
	return nil
}

// flush writes each tracked validator's accrued uptime to disk so it survives
// an unclean shutdown.
func (t *uptimeTracker) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return
	}
	for _, nodeID := range t.state.trackedNodeIDs() {
		duration, lastUpdated, err := t.mgr.CalculateUptime(nodeID)
		if err != nil {
			continue
		}
		if err := t.state.SetUptime(nodeID, duration, lastUpdated); err != nil {
			t.log.Warn("uptime: failed to persist",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
		}
	}
}

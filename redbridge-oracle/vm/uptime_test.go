package vm

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// ---------- message codec ----------

// TestUptimeMessageMatchesContractLayout locks the wire format to the exact
// 46-byte layout StakingManager.unpackValidationUptimeMessage requires.
func TestUptimeMessageMatchesContractLayout(t *testing.T) {
	validationID := ids.GenerateTestID()
	raw := uptimeMessage{ValidationID: validationID, UptimeSeconds: 0x0102030405060708}.marshal()

	if len(raw) != 46 {
		t.Fatalf("payload must be 46 bytes, got %d", len(raw))
	}
	for i, want := range []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} {
		if raw[i] != want {
			t.Fatalf("byte %d: codecID/typeID must be zero, got %#x", i, raw[i])
		}
	}
	if got := raw[6:38]; string(got) != string(validationID[:]) {
		t.Fatal("validationID not at offset 6")
	}
	for i, want := range []byte{1, 2, 3, 4, 5, 6, 7, 8} {
		if raw[38+i] != want {
			t.Fatalf("uptime must be big-endian at offset 38, byte %d = %#x", i, raw[38+i])
		}
	}
}

func TestParseUptimeMessageRoundTrip(t *testing.T) {
	want := uptimeMessage{ValidationID: ids.GenerateTestID(), UptimeSeconds: 12345}
	got, err := parseUptimeMessage(want.marshal())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestParseUptimeMessageRejectsOtherPayloads(t *testing.T) {
	for name, in := range map[string][]byte{
		"too short":    make([]byte, 45),
		"too long":     make([]byte, 47),
		"bad codecID":  append([]byte{0x00, 0x01}, make([]byte, 44)...),
		"bad typeID":   append([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x09}, make([]byte, 40)...),
		"oracle bytes": OutboundPayload{Wallet: [20]byte{1}, ZcashHeight: 7}.Encode(),
	} {
		if _, err := parseUptimeMessage(in); err != errNotUptimeMessage {
			t.Fatalf("%s: want errNotUptimeMessage, got %v", name, err)
		}
	}
}

// TestParseUptimeAddressedCallRejectsSourceAddress guards the rule that makes
// a proof trustworthy: StakingManager only accepts proofs with an empty origin
// sender, proving a node signed directly rather than an on-chain caller.
func TestParseUptimeAddressedCallRejectsSourceAddress(t *testing.T) {
	inner := uptimeMessage{ValidationID: ids.GenerateTestID(), UptimeSeconds: 1}.marshal()
	ac, err := payload.NewAddressedCall([]byte{0xde, 0xad}, inner)
	if err != nil {
		t.Fatalf("build addressed call: %v", err)
	}

	if _, err := parseUptimeAddressedCall(ac.Bytes()); err == nil {
		t.Fatal("expected rejection of non-empty source address")
	} else if err == errNotUptimeMessage {
		t.Fatal("non-empty source address must be an explicit error, not a skip")
	}
}

func TestNewUptimeAddressedCallRoundTrip(t *testing.T) {
	validationID := ids.GenerateTestID()
	acBytes, err := newUptimeAddressedCall(validationID, 999)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := parseUptimeAddressedCall(acBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ValidationID != validationID || got.UptimeSeconds != 999 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

// ---------- state ----------

func TestUptimeStatePersistsAcrossRestart(t *testing.T) {
	db := memdb.New()
	nodeID := ids.GenerateTestNodeID()
	start := time.Unix(1700000000, 0)

	state, err := newUptimeState(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := state.ensureValidator(nodeID, ids.GenerateTestID(), start, start); err != nil {
		t.Fatalf("ensureValidator: %v", err)
	}
	if err := state.SetUptime(nodeID, 90*time.Second, start.Add(100*time.Second)); err != nil {
		t.Fatalf("SetUptime: %v", err)
	}

	reopened, err := newUptimeState(db)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	duration, lastUpdated, err := reopened.GetUptime(nodeID)
	if err != nil {
		t.Fatalf("GetUptime: %v", err)
	}
	if duration != 90*time.Second {
		t.Fatalf("upDuration: got %v want 90s", duration)
	}
	if !lastUpdated.Equal(start.Add(100 * time.Second)) {
		t.Fatalf("lastUpdated: got %v", lastUpdated)
	}
	if got, err := reopened.GetStartTime(nodeID); err != nil || !got.Equal(start) {
		t.Fatalf("GetStartTime: got %v err %v", got, err)
	}
}

func TestUptimeStateNeverGoesBackwards(t *testing.T) {
	state, err := newUptimeState(memdb.New())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	nodeID := ids.GenerateTestNodeID()
	now := time.Unix(1700000000, 0)

	if err := state.ensureValidator(nodeID, ids.GenerateTestID(), now, now); err != nil {
		t.Fatalf("ensureValidator: %v", err)
	}
	if err := state.SetUptime(nodeID, 500*time.Second, now); err != nil {
		t.Fatalf("SetUptime: %v", err)
	}
	// A backwards clock must not erase earned uptime.
	if err := state.SetUptime(nodeID, 10*time.Second, now); err != nil {
		t.Fatalf("SetUptime: %v", err)
	}
	if duration, _, _ := state.GetUptime(nodeID); duration != 500*time.Second {
		t.Fatalf("uptime regressed to %v", duration)
	}
}

func TestUptimeStateEnsureValidatorIsIdempotent(t *testing.T) {
	state, err := newUptimeState(memdb.New())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	nodeID := ids.GenerateTestNodeID()
	now := time.Unix(1700000000, 0)

	validationID := ids.GenerateTestID()
	_ = state.ensureValidator(nodeID, validationID, now, now)
	_ = state.SetUptime(nodeID, 42*time.Second, now)
	// A routine validator-set refresh must not reset accrued uptime.
	_ = state.ensureValidator(nodeID, validationID, now, now)

	if duration, _, _ := state.GetUptime(nodeID); duration != 42*time.Second {
		t.Fatalf("uptime reset to %v by refresh", duration)
	}
}

// ---------- tracker ----------

func newTestTracker(t *testing.T, selfNodeID ids.NodeID) *uptimeTracker {
	t.Helper()
	tracker, err := newUptimeTracker(memdb.New(), logging.NoLog{}, selfNodeID, ids.GenerateTestID(), nil)
	if err != nil {
		t.Fatalf("newUptimeTracker: %v", err)
	}
	return tracker
}

// addValidator injects a validator directly, standing in for the P-Chain
// refresh that is unavailable in unit tests.
func addValidator(t *testing.T, tracker *uptimeTracker, info validatorInfo, startTime time.Time) {
	t.Helper()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if err := tracker.state.ensureValidator(info.nodeID, info.validationID, startTime, startTime); err != nil {
		t.Fatalf("ensureValidator: %v", err)
	}
	tracker.byNodeID[info.nodeID] = info
	tracker.byValidationID[info.validationID] = info.nodeID
	tracker.applyConnectivity(info.nodeID)
}

func TestTrackerAccruesUptimeForConnectedValidator(t *testing.T) {
	self := ids.GenerateTestNodeID()
	tracker := newTestTracker(t, self)

	now := time.Unix(1700000000, 0)
	tracker.clock.Set(now)

	peer := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: validationID,
		nodeID:       peer,
		isActive:     true,
		startTime:    now,
	}, now)

	tracker.mu.Lock()
	if err := tracker.mgr.StartTracking(tracker.state.trackedNodeIDs()); err != nil {
		t.Fatalf("StartTracking: %v", err)
	}
	tracker.started = true
	tracker.mu.Unlock()

	tracker.connect(peer)
	tracker.clock.Set(now.Add(10 * time.Minute))

	seconds, info, err := tracker.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("uptimeSeconds: %v", err)
	}
	if seconds != 600 {
		t.Fatalf("uptime: got %ds want 600s", seconds)
	}
	if info.nodeID != peer {
		t.Fatalf("wrong validator returned: %s", info.nodeID)
	}
}

// TestTrackerDoesNotAccrueWhileDisconnected is the check that stops a node
// from vouching for a peer that was actually offline.
func TestTrackerDoesNotAccrueWhileDisconnected(t *testing.T) {
	tracker := newTestTracker(t, ids.GenerateTestNodeID())

	now := time.Unix(1700000000, 0)
	tracker.clock.Set(now)

	peer := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: validationID,
		nodeID:       peer,
		isActive:     true,
		startTime:    now,
	}, now)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.mu.Unlock()

	tracker.connect(peer)
	tracker.clock.Set(now.Add(5 * time.Minute))
	tracker.disconnect(peer)

	// Five more minutes pass while the peer is offline.
	tracker.clock.Set(now.Add(10 * time.Minute))

	seconds, _, err := tracker.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("uptimeSeconds: %v", err)
	}
	if seconds != 300 {
		t.Fatalf("offline time was credited: got %ds want 300s", seconds)
	}
}

// TestTrackerDoesNotAccrueWhileInactive covers an L1 validator whose
// continuous-fee balance ran out: still a peer, but no longer validating.
func TestTrackerDoesNotAccrueWhileInactive(t *testing.T) {
	tracker := newTestTracker(t, ids.GenerateTestNodeID())

	now := time.Unix(1700000000, 0)
	tracker.clock.Set(now)

	peer := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	info := validatorInfo{validationID: validationID, nodeID: peer, isActive: false, startTime: now}
	addValidator(t, tracker, info, now)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.mu.Unlock()

	tracker.connect(peer)
	tracker.clock.Set(now.Add(10 * time.Minute))

	seconds, _, err := tracker.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("uptimeSeconds: %v", err)
	}
	if seconds != 0 {
		t.Fatalf("inactive validator accrued %ds", seconds)
	}
}

func TestTrackerCountsSelfAsConnected(t *testing.T) {
	self := ids.GenerateTestNodeID()
	tracker := newTestTracker(t, self)

	now := time.Unix(1700000000, 0)
	tracker.clock.Set(now)

	validationID := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: validationID,
		nodeID:       self,
		isActive:     true,
		startTime:    now,
	}, now)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.applyConnectivity(self)
	tracker.mu.Unlock()

	tracker.clock.Set(now.Add(time.Hour))

	seconds, _, err := tracker.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("uptimeSeconds: %v", err)
	}
	if seconds != 3600 {
		t.Fatalf("self uptime: got %ds want 3600s", seconds)
	}
}

func TestTrackerUnknownValidationID(t *testing.T) {
	tracker := newTestTracker(t, ids.GenerateTestNodeID())
	if _, _, err := tracker.uptimeSeconds(ids.GenerateTestID()); err == nil {
		t.Fatal("expected error for unknown validationID")
	}
}

// TestNilTrackerIsSafe covers the unit-test / no-P-Chain configuration, where
// the VM runs with tracking disabled.
func TestNilTrackerIsSafe(t *testing.T) {
	var tracker *uptimeTracker

	tracker.connect(ids.GenerateTestNodeID())
	tracker.disconnect(ids.GenerateTestNodeID())
	tracker.stop()

	if err := tracker.start(context.Background()); err != nil {
		t.Fatalf("start on nil tracker: %v", err)
	}
	if snapshot := tracker.validatorSnapshot(); snapshot != nil {
		t.Fatal("expected nil snapshot")
	}
	if _, _, err := tracker.uptimeSeconds(ids.GenerateTestID()); err == nil {
		t.Fatal("expected error from nil tracker")
	}
}

// ---------- signing policy ----------

// signableTestVM builds a VM whose chain context matches the messages under
// test, with a tracker holding one validator at a known uptime.
func signableTestVM(t *testing.T) (*VM, ids.ID) {
	t.Helper()

	v := newTestVM(t)
	v.ctx.ChainID = ids.GenerateTestID()
	v.ctx.NetworkID = 5

	tracker := newTestTracker(t, ids.GenerateTestNodeID())
	now := time.Unix(1700000000, 0)
	tracker.clock.Set(now)

	peer := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: validationID,
		nodeID:       peer,
		isActive:     true,
		startTime:    now,
	}, now)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.mu.Unlock()

	tracker.connect(peer)
	tracker.clock.Set(now.Add(1000 * time.Second))

	v.uptimeTracker = tracker
	return v, validationID
}

func uptimeWarpMessage(t *testing.T, v *VM, validationID ids.ID, seconds uint64) *warp.UnsignedMessage {
	t.Helper()
	acBytes, err := newUptimeAddressedCall(validationID, seconds)
	if err != nil {
		t.Fatalf("build addressed call: %v", err)
	}
	msg, err := warp.NewUnsignedMessage(v.ctx.NetworkID, v.ctx.ChainID, acBytes)
	if err != nil {
		t.Fatalf("build warp message: %v", err)
	}
	return msg
}

func TestCheckSignableAcceptsHonestUptimeClaim(t *testing.T) {
	v, validationID := signableTestVM(t)

	for _, seconds := range []uint64{0, 500, 1000} {
		msg := uptimeWarpMessage(t, v, validationID, seconds)
		if err := v.checkSignable(msg); err != nil {
			t.Fatalf("claim of %ds (measured 1000s) rejected: %v", seconds, err)
		}
	}
}

// TestCheckSignableRejectsInflatedUptimeClaim is the core security test: a
// node must never attest to more uptime than it observed.
func TestCheckSignableRejectsInflatedUptimeClaim(t *testing.T) {
	v, validationID := signableTestVM(t)

	msg := uptimeWarpMessage(t, v, validationID, 1001)
	if err := v.checkSignable(msg); err == nil {
		t.Fatal("node signed an uptime claim above its own measurement")
	}
}

func TestCheckSignableRejectsUnknownValidation(t *testing.T) {
	v, _ := signableTestVM(t)

	msg := uptimeWarpMessage(t, v, ids.GenerateTestID(), 1)
	if err := v.checkSignable(msg); err == nil {
		t.Fatal("node signed a proof for a validation it does not track")
	}
}

func TestCheckSignableRejectsForeignChainAndNetwork(t *testing.T) {
	v, validationID := signableTestVM(t)

	wrongChain, err := warp.NewUnsignedMessage(v.ctx.NetworkID, ids.GenerateTestID(),
		uptimeWarpMessage(t, v, validationID, 1).Payload)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := v.checkSignable(wrongChain); err == nil {
		t.Fatal("node signed a message attributed to another chain")
	}

	wrongNetwork, err := warp.NewUnsignedMessage(v.ctx.NetworkID+1, v.ctx.ChainID,
		uptimeWarpMessage(t, v, validationID, 1).Payload)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := v.checkSignable(wrongNetwork); err == nil {
		t.Fatal("node signed a message attributed to another network")
	}
}

// TestCheckSignableAcceptsKnownOutboundMessage confirms the ACP-118 path still
// serves this VM's own oracle messages.
func TestCheckSignableAcceptsKnownOutboundMessage(t *testing.T) {
	v, _ := signableTestVM(t)

	abiData := OutboundPayload{Wallet: [20]byte{0xab}, ZcashHeight: 42}.Encode()
	ac, err := payload.NewAddressedCall(nil, abiData)
	if err != nil {
		t.Fatalf("build addressed call: %v", err)
	}
	msg, err := warp.NewUnsignedMessage(v.ctx.NetworkID, v.ctx.ChainID, ac.Bytes())
	if err != nil {
		t.Fatalf("build warp message: %v", err)
	}

	if err := v.checkSignable(msg); err == nil {
		t.Fatal("unknown outbound message must not be signable")
	}

	v.outboundWarpMsgs[msg.ID()] = outboundMsgEntry{msg: msg, avaxHeight: 1}
	if err := v.checkSignable(msg); err != nil {
		t.Fatalf("known outbound message rejected: %v", err)
	}
}

// TestCheckNotForgedUptimeLeavesOracleMessagesAlone verifies the /warp HTTP
// endpoint keeps its existing permissive behaviour for oracle messages while
// still blocking forged uptime proofs.
func TestCheckNotForgedUptimeLeavesOracleMessagesAlone(t *testing.T) {
	v, validationID := signableTestVM(t)

	abiData := OutboundPayload{Wallet: [20]byte{0xcd}, ZcashHeight: 7}.Encode()
	ac, err := payload.NewAddressedCall(nil, abiData)
	if err != nil {
		t.Fatalf("build addressed call: %v", err)
	}
	oracleMsg, err := warp.NewUnsignedMessage(v.ctx.NetworkID, v.ctx.ChainID, ac.Bytes())
	if err != nil {
		t.Fatalf("build warp message: %v", err)
	}

	// Not in the outbox, and still allowed — the endpoint's pre-existing
	// contract with the ICM relayer is unchanged.
	if err := v.checkNotForgedUptime(oracleMsg); err != nil {
		t.Fatalf("oracle message blocked by uptime guard: %v", err)
	}

	if err := v.checkNotForgedUptime(uptimeWarpMessage(t, v, validationID, 1001)); err == nil {
		t.Fatal("forged uptime proof accepted by the HTTP signing endpoint")
	}
	if err := v.checkNotForgedUptime(uptimeWarpMessage(t, v, validationID, 999)); err != nil {
		t.Fatalf("honest uptime proof blocked: %v", err)
	}
}

// ---------- re-registration ----------

// TestUptimeResetsOnReRegistration is the regression test for the case where a
// node validates, is removed, and later registers again.
//
// Day 1: the node validates for 2h and is removed.
// Day 2: the same node registers again — a NEW validationID — and validates 1h.
//
// The second validation must report 1h, not 3h. Reporting 3h would let it
// clear the reward calculator's uptime threshold without having been up.
func TestUptimeResetsOnReRegistration(t *testing.T) {
	tracker := newTestTracker(t, ids.GenerateTestNodeID())

	day1 := time.Unix(1700000000, 0)
	tracker.clock.Set(day1)

	peer := ids.GenerateTestNodeID()
	firstValidation := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: firstValidation,
		nodeID:       peer,
		isActive:     true,
		startTime:    day1,
	}, day1)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.mu.Unlock()

	// Day 1: two hours of validating.
	tracker.connect(peer)
	tracker.clock.Set(day1.Add(2 * time.Hour))

	seconds, _, err := tracker.uptimeSeconds(firstValidation)
	if err != nil {
		t.Fatalf("first validation uptime: %v", err)
	}
	if seconds != 7200 {
		t.Fatalf("first validation: got %ds want 7200s", seconds)
	}

	// Removed from the set, then a full day passes.
	tracker.disconnect(peer)
	day2 := day1.Add(24 * time.Hour)
	tracker.clock.Set(day2)

	// Re-registered: same node, brand new validationID.
	secondValidation := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: secondValidation,
		nodeID:       peer,
		isActive:     true,
		startTime:    day2,
	}, day2)

	tracker.connect(peer)
	tracker.clock.Set(day2.Add(time.Hour))

	seconds, _, err = tracker.uptimeSeconds(secondValidation)
	if err != nil {
		t.Fatalf("second validation uptime: %v", err)
	}
	if seconds != 3600 {
		t.Fatalf("second validation inherited earlier uptime: got %ds want 3600s", seconds)
	}

	// The retired validationID must no longer be answerable at all.
	if _, _, err := tracker.uptimeSeconds(firstValidation); err == nil {
		t.Fatal("retired validationID still reports uptime")
	}
}

// TestSigningRefusesInflatedClaimAfterReRegistration is the same scenario seen
// from the signing path: the node must refuse to sign the carried-over figure.
func TestSigningRefusesInflatedClaimAfterReRegistration(t *testing.T) {
	v := newTestVM(t)
	v.ctx.ChainID = ids.GenerateTestID()
	v.ctx.NetworkID = 5

	tracker := newTestTracker(t, ids.GenerateTestNodeID())
	day1 := time.Unix(1700000000, 0)
	tracker.clock.Set(day1)

	peer := ids.GenerateTestNodeID()
	addValidator(t, tracker, validatorInfo{
		validationID: ids.GenerateTestID(),
		nodeID:       peer,
		isActive:     true,
		startTime:    day1,
	}, day1)

	tracker.mu.Lock()
	_ = tracker.mgr.StartTracking(tracker.state.trackedNodeIDs())
	tracker.started = true
	tracker.mu.Unlock()

	tracker.connect(peer)
	tracker.clock.Set(day1.Add(2 * time.Hour))
	tracker.disconnect(peer)

	// Re-register the next day under a new validation, run for one hour.
	day2 := day1.Add(24 * time.Hour)
	tracker.clock.Set(day2)
	secondValidation := ids.GenerateTestID()
	addValidator(t, tracker, validatorInfo{
		validationID: secondValidation,
		nodeID:       peer,
		isActive:     true,
		startTime:    day2,
	}, day2)
	tracker.connect(peer)
	tracker.clock.Set(day2.Add(time.Hour))

	v.uptimeTracker = tracker

	// 1h is honest and must be signable.
	if err := v.checkSignable(uptimeWarpMessage(t, v, secondValidation, 3600)); err != nil {
		t.Fatalf("honest 1h claim rejected: %v", err)
	}
	// 3h is the carried-over figure and must be refused.
	if err := v.checkSignable(uptimeWarpMessage(t, v, secondValidation, 10800)); err == nil {
		t.Fatal("node signed 3h for a validation that has only run 1h")
	}
}

// TestUptimeStateResetsWhenValidationIDChanges pins the state-level rule.
func TestUptimeStateResetsWhenValidationIDChanges(t *testing.T) {
	state, err := newUptimeState(memdb.New())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	nodeID := ids.GenerateTestNodeID()
	now := time.Unix(1700000000, 0)

	first := ids.GenerateTestID()
	_ = state.ensureValidator(nodeID, first, now, now)
	_ = state.SetUptime(nodeID, 2*time.Hour, now)

	later := now.Add(24 * time.Hour)
	second := ids.GenerateTestID()
	if err := state.ensureValidator(nodeID, second, later, later); err != nil {
		t.Fatalf("ensureValidator: %v", err)
	}

	duration, lastUpdated, err := state.GetUptime(nodeID)
	if err != nil {
		t.Fatalf("GetUptime: %v", err)
	}
	if duration != 0 {
		t.Fatalf("uptime carried across validations: %v", duration)
	}
	if !lastUpdated.Equal(later) {
		t.Fatalf("lastUpdated not reseeded: %v", lastUpdated)
	}
	if got, _ := state.validationIDOf(nodeID); got != second {
		t.Fatalf("validationID not rebound: %s", got)
	}
}

// TestLegacyUptimeRecordIsResetNotTrusted covers records written before uptime
// was bound to a validationID: they cannot be attributed, so they must not be
// credited to whichever validation is running now.
func TestLegacyUptimeRecordIsResetNotTrusted(t *testing.T) {
	db := memdb.New()
	nodeID := ids.GenerateTestNodeID()
	now := time.Unix(1700000000, 0)

	// Hand-write a 24-byte legacy record claiming 5h of uptime.
	legacy := make([]byte, legacyUptimeRecordLen)
	binary.BigEndian.PutUint64(legacy[0:8], uint64((5 * time.Hour).Seconds()))
	binary.BigEndian.PutUint64(legacy[8:16], uint64(now.Unix()))
	binary.BigEndian.PutUint64(legacy[16:24], uint64(now.Unix()))
	if err := db.Put(nodeID[:], legacy); err != nil {
		t.Fatalf("seed legacy record: %v", err)
	}

	state, err := newUptimeState(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if duration, _, err := state.GetUptime(nodeID); err != nil || duration != 5*time.Hour {
		t.Fatalf("legacy record not loaded: %v %v", duration, err)
	}
	if got, _ := state.validationIDOf(nodeID); got != ids.Empty {
		t.Fatalf("legacy record must load with an empty validationID, got %s", got)
	}

	// First refresh binds it to a real validation and must zero it.
	if err := state.ensureValidator(nodeID, ids.GenerateTestID(), now, now); err != nil {
		t.Fatalf("ensureValidator: %v", err)
	}
	if duration, _, _ := state.GetUptime(nodeID); duration != 0 {
		t.Fatalf("legacy uptime was credited to a new validation: %v", duration)
	}
}

// TestRestartDoesNotCreditDowntime reproduces the observed behaviour: stop the
// observing node for a minute, restart it, and the minute must NOT appear as
// uptime. uptime.Manager would otherwise credit the whole span since the last
// flush, because before StartTracking it assumes nodes were online.
func TestRestartDoesNotCreditDowntime(t *testing.T) {
	db := memdb.New()
	self := ids.GenerateTestNodeID()
	peer := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	start := time.Unix(1700000000, 0)

	// --- first run: observe the peer for 10 minutes, then shut down ---
	first, err := newUptimeTracker(db, logging.NoLog{}, self, ids.GenerateTestID(), nil)
	if err != nil {
		t.Fatalf("newUptimeTracker: %v", err)
	}
	first.clock.Set(start)
	addValidator(t, first, validatorInfo{
		validationID: validationID,
		nodeID:       peer,
		isActive:     true,
		startTime:    start,
	}, start)

	first.mu.Lock()
	_ = first.mgr.StartTracking(first.state.trackedNodeIDs())
	first.started = true
	first.mu.Unlock()

	first.connect(peer)
	first.clock.Set(start.Add(10 * time.Minute))
	first.stop() // clean shutdown flushes 600s

	if seconds, _, err := first.uptimeSeconds(validationID); err != nil || seconds != 600 {
		t.Fatalf("before restart: got %ds err %v, want 600s", seconds, err)
	}

	// --- one minute of downtime, then restart against the same database ---
	restartAt := start.Add(11 * time.Minute)

	second, err := newUptimeTracker(db, logging.NoLog{}, self, ids.GenerateTestID(), nil)
	if err != nil {
		t.Fatalf("reopen tracker: %v", err)
	}
	second.clock.Set(restartAt)
	second.byNodeID[peer] = validatorInfo{
		validationID: validationID,
		nodeID:       peer,
		isActive:     true,
		startTime:    start,
	}
	second.byValidationID[validationID] = peer

	if err := second.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer second.stop()

	// The 60s the node was down must not have been credited.
	seconds, _, err := second.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("after restart: %v", err)
	}
	if seconds != 600 {
		t.Fatalf("restart credited downtime: got %ds, want 600s", seconds)
	}

	// Accrual resumes normally once the peer reconnects.
	second.connect(peer)
	second.clock.Set(restartAt.Add(5 * time.Minute))
	seconds, _, err = second.uptimeSeconds(validationID)
	if err != nil {
		t.Fatalf("after reconnect: %v", err)
	}
	if seconds != 900 {
		t.Fatalf("accrual did not resume: got %ds, want 900s", seconds)
	}
}

// TestDiscardUnobservedGapKeepsEarnedUptime pins that sealing the gap never
// destroys uptime that was genuinely earned before shutdown.
func TestDiscardUnobservedGapKeepsEarnedUptime(t *testing.T) {
	state, err := newUptimeState(memdb.New())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	nodeID := ids.GenerateTestNodeID()
	now := time.Unix(1700000000, 0)

	_ = state.ensureValidator(nodeID, ids.GenerateTestID(), now, now)
	_ = state.SetUptime(nodeID, 42*time.Minute, now)

	later := now.Add(time.Hour)
	if err := state.discardUnobservedGap(later); err != nil {
		t.Fatalf("discardUnobservedGap: %v", err)
	}

	duration, lastUpdated, err := state.GetUptime(nodeID)
	if err != nil {
		t.Fatalf("GetUptime: %v", err)
	}
	if duration != 42*time.Minute {
		t.Fatalf("earned uptime lost: got %v want 42m", duration)
	}
	if !lastUpdated.Equal(later) {
		t.Fatalf("lastUpdated not advanced: %v", lastUpdated)
	}
}

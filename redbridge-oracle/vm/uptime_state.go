package vm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
)

// uptimePrefix namespaces uptime records inside the VM's LevelDB. It is
// distinct from every prefix in state.go, so enabling uptime tracking cannot
// disturb existing block, index, wallet or outbound-message data.
var uptimePrefix = []byte("uptime")

// uptimeRecordLen is the fixed on-disk size of one validator's uptime record:
//
//	Offset  Size  Field
//	0       8     upDuration   (seconds, big-endian uint64)
//	8       8     lastUpdated  (unix seconds, big-endian int64)
//	16      8     startTime    (unix seconds, big-endian int64)
//	24      32    validationID (the validation this uptime belongs to)
const uptimeRecordLen = 56

// legacyUptimeRecordLen is the original record size, written before records
// were bound to a validationID. Such a record cannot be attributed to any
// particular validation, so it is loaded with an empty validationID, which
// forces a reset on the next refresh. Losing a little test uptime is the
// correct trade against attributing it to the wrong validation.
const legacyUptimeRecordLen = 24

// uptimeState persists per-validator uptime across restarts. It implements
// avalanchego's uptime.State so the stock uptime.Manager can drive it.
//
// Durations are stored truncated to whole seconds. Truncation always rounds
// down, so a node can only ever under-report its own uptime — never
// over-report it. That bias is deliberate: an over-report would let a
// validator claim rewards it did not earn.
//
// uptimeState is NOT internally synchronised. It is only ever touched by
// uptimeTracker, which holds its own mutex across every access, matching the
// fact that uptime.Manager is likewise not thread-safe.
type uptimeState struct {
	db database.Database

	// records caches every tracked validator so reads never hit disk. Writes
	// are write-through, so the cache and the database never diverge.
	records map[ids.NodeID]*uptimeRecord
}

type uptimeRecord struct {
	upDuration  time.Duration
	lastUpdated time.Time
	startTime   time.Time

	// validationID binds this uptime to one specific validation period.
	// Uptime is a property of a VALIDATION, not of a node: when a node is
	// removed and later re-registers, the P-Chain issues a new validationID
	// and its uptime must start again from zero. Without this binding, the
	// second validation would inherit the first one's uptime and could clear
	// the reward calculator's uptime threshold without having been up.
	validationID ids.ID
}

// newUptimeState opens the uptime namespace and loads any existing records.
func newUptimeState(db database.Database) (*uptimeState, error) {
	s := &uptimeState{
		db:      db,
		records: make(map[ids.NodeID]*uptimeRecord),
	}

	it := s.db.NewIterator()
	defer it.Release()

	for it.Next() {
		nodeID, err := ids.ToNodeID(it.Key())
		if err != nil {
			// A key that is not a node ID cannot have been written by this
			// VM. Skip it rather than abort startup.
			continue
		}
		rec, err := parseUptimeRecord(it.Value())
		if err != nil {
			continue
		}
		s.records[nodeID] = rec
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("iterate uptime records: %w", err)
	}

	return s, nil
}

// GetUptime implements uptime.State.
func (s *uptimeState) GetUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	rec, ok := s.records[nodeID]
	if !ok {
		// uptime.Manager treats ErrNotFound as "not a validator we track" and
		// skips the node, which is exactly the behaviour we want.
		return 0, time.Time{}, database.ErrNotFound
	}
	return rec.upDuration, rec.lastUpdated, nil
}

// SetUptime implements uptime.State.
func (s *uptimeState) SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) error {
	rec, ok := s.records[nodeID]
	if !ok {
		return database.ErrNotFound
	}

	// Never let a stored uptime move backwards. CalculateUptime is monotonic
	// under normal operation, but a clock adjustment must not silently erase
	// uptime a validator has already earned.
	if upDuration < rec.upDuration {
		upDuration = rec.upDuration
	}

	rec.upDuration = upDuration
	rec.lastUpdated = lastUpdated.Truncate(time.Second)
	return s.persist(nodeID, rec)
}

// GetStartTime implements uptime.State.
func (s *uptimeState) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	rec, ok := s.records[nodeID]
	if !ok {
		return time.Time{}, database.ErrNotFound
	}
	return rec.startTime, nil
}

// ensureValidator makes sure nodeID has a record for exactly the validation
// it is currently serving.
//
// If the node already has a record for this same validationID, the call is a
// no-op, so a routine validator-set refresh never disturbs accrued uptime.
// If the stored record belongs to a DIFFERENT validation — the node was
// removed and re-registered, so the P-Chain issued a new validationID — the
// record is reset to zero. Carrying uptime across validations would let a
// validator that ran two hours yesterday claim those hours against a fresh
// one-hour validation today.
//
// [now] is when this node first observed the validation. lastUpdated is seeded
// from it rather than from the validation's start time, because uptime.Manager
// treats the span between lastUpdated and the start of tracking as time the
// validator was UP. Seeding with the validation start time would therefore
// credit the whole pre-observation window — potentially weeks on a freshly
// synced node — as uptime this node never actually witnessed.
func (s *uptimeState) ensureValidator(
	nodeID ids.NodeID,
	validationID ids.ID,
	startTime time.Time,
	now time.Time,
) error {
	if rec, ok := s.records[nodeID]; ok && rec.validationID == validationID {
		return nil
	}

	rec := &uptimeRecord{
		upDuration:   0,
		lastUpdated:  now.Truncate(time.Second),
		startTime:    startTime.Truncate(time.Second),
		validationID: validationID,
	}
	s.records[nodeID] = rec
	return s.persist(nodeID, rec)
}

// discardUnobservedGap advances every record's lastUpdated to [now] without
// touching the accrued duration.
//
// It must be called immediately before uptime.Manager.StartTracking. That
// method walks every tracked node while its internal startedTracking flag is
// still false, and CalculateUptime takes this branch in that state:
//
//	// If we haven't started tracking, then we assume that the node has been
//	// online since their last update.
//
// In other words the manager credits the whole span since lastUpdated as time
// the validator was UP. For platformvm that is a deliberate kindness — a node
// should not penalise validators for a window it could not observe. For a
// rewards oracle it is the opposite of what we want: this node is about to put
// its BLS signature on a claim, and it must never attest to time it did not
// witness. Moving lastUpdated to now makes that span zero, so a restart costs
// the restarted node a little credit rather than inventing it.
func (s *uptimeState) discardUnobservedGap(now time.Time) error {
	now = now.Truncate(time.Second)
	for nodeID, rec := range s.records {
		if !rec.lastUpdated.Before(now) {
			continue
		}
		rec.lastUpdated = now
		if err := s.persist(nodeID, rec); err != nil {
			return err
		}
	}
	return nil
}

// validationIDOf returns the validation a node's stored uptime belongs to.
func (s *uptimeState) validationIDOf(nodeID ids.NodeID) (ids.ID, bool) {
	rec, ok := s.records[nodeID]
	if !ok {
		return ids.Empty, false
	}
	return rec.validationID, true
}

// Records are deliberately never deleted. When a validator leaves the set it
// simply stops being counted (see uptimeTracker.applyConnectivity), freezing
// its uptime. Keeping the record means a validator that briefly vanishes from
// this node's view — a P-Chain query hiccup, a restart mid-refresh — resumes
// with its earned uptime intact instead of silently restarting from zero.
// A record is 24 bytes, so retention costs nothing at any realistic L1 size.

// trackedNodeIDs returns every node that currently has an uptime record.
func (s *uptimeState) trackedNodeIDs() []ids.NodeID {
	nodeIDs := make([]ids.NodeID, 0, len(s.records))
	for nodeID := range s.records {
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs
}

func (s *uptimeState) persist(nodeID ids.NodeID, rec *uptimeRecord) error {
	buf := make([]byte, uptimeRecordLen)
	binary.BigEndian.PutUint64(buf[0:8], uint64(rec.upDuration/time.Second))
	binary.BigEndian.PutUint64(buf[8:16], uint64(rec.lastUpdated.Unix()))
	binary.BigEndian.PutUint64(buf[16:24], uint64(rec.startTime.Unix()))
	copy(buf[24:56], rec.validationID[:])

	if err := s.db.Put(nodeID[:], buf); err != nil {
		return fmt.Errorf("persist uptime for %s: %w", nodeID, err)
	}
	return nil
}

func parseUptimeRecord(b []byte) (*uptimeRecord, error) {
	if len(b) != uptimeRecordLen && len(b) != legacyUptimeRecordLen {
		return nil, fmt.Errorf("uptime record must be %d bytes, got %d", uptimeRecordLen, len(b))
	}

	rec := &uptimeRecord{
		upDuration:  time.Duration(binary.BigEndian.Uint64(b[0:8])) * time.Second,
		lastUpdated: time.Unix(int64(binary.BigEndian.Uint64(b[8:16])), 0),
		startTime:   time.Unix(int64(binary.BigEndian.Uint64(b[16:24])), 0),
	}
	// A legacy record leaves validationID empty, which never matches a real
	// validation, so the next refresh resets it rather than crediting its
	// uptime to whichever validation happens to be running now.
	if len(b) == uptimeRecordLen {
		copy(rec.validationID[:], b[24:56])
	}
	return rec, nil
}

// Compile-time check that we satisfy the interface uptime.Manager drives.
var _ uptime.State = (*uptimeState)(nil)

package vm

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	blockPrefix       = []byte("blk")
	zcashHeightPrefix = []byte("zhgt")
	zcashDataPrefix   = []byte("zdat")
	walletPrefix      = []byte("wall")
	metaPrefix        = []byte("meta")
	outboundPrefix    = []byte("outmsg")
	lastAcceptedKey   = []byte("lastAccepted")
)

// State manages persistent storage for the VM.  All public methods are
// safe for concurrent use.
type State struct {
	blockDB       database.Database
	zcashHeightDB database.Database
	zcashDataDB   database.Database
	walletDB      database.Database
	metaDB        database.Database
	outboundDB    database.Database // persists outbound warp messages across restarts

	// In-memory block cache (block ID -> *Block).
	cache map[ids.ID]*Block

	log logging.Logger
	mu  sync.RWMutex
}

// NewState creates a State backed by the given database.
func NewState(db database.Database, log logging.Logger) *State {
	return &State{
		blockDB:       prefixdb.New(blockPrefix, db),
		zcashHeightDB: prefixdb.New(zcashHeightPrefix, db),
		zcashDataDB:   prefixdb.New(zcashDataPrefix, db),
		walletDB:      prefixdb.New(walletPrefix, db),
		metaDB:        prefixdb.New(metaPrefix, db),
		outboundDB:    prefixdb.New(outboundPrefix, db),
		cache:         make(map[ids.ID]*Block),
		log:           log,
	}
}

// ---------- Block storage ----------

// GetBlock returns a block by its Avalanche block ID.
func (s *State) GetBlock(blkID ids.ID) (*Block, error) {
	s.mu.RLock()
	if blk, ok := s.cache[blkID]; ok {
		s.mu.RUnlock()
		return blk, nil
	}
	s.mu.RUnlock()

	data, err := s.blockDB.Get(blkID[:])
	if err != nil {
		return nil, fmt.Errorf("get block %s: %w", blkID, err)
	}

	blk, err := UnmarshalBlock(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal block %s: %w", blkID, err)
	}
	blk.status = choices.Accepted // persisted blocks are always accepted

	s.mu.Lock()
	s.cache[blkID] = blk
	s.mu.Unlock()

	return blk, nil
}

// PutBlock persists a block and its zcash-height index entry atomically.
func (s *State) PutBlock(blk *Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store block bytes keyed by block ID.
	if err := s.blockDB.Put(blk.id[:], blk.bytes); err != nil {
		return fmt.Errorf("put block: %w", err)
	}

	// Store zcash height -> block ID mapping (skip genesis height 0).
	if blk.ZcashHeight > 0 {
		key := zcashHeightKey(blk.ZcashHeight)
		if err := s.zcashHeightDB.Put(key, blk.id[:]); err != nil {
			return fmt.Errorf("put zcash height index: %w", err)
		}
	}

	s.cache[blk.id] = blk
	return nil
}

// CacheBlock stores a block in the in-memory cache without persisting.
// Used for blocks in Processing state during consensus.
func (s *State) CacheBlock(blk *Block) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[blk.id] = blk
}

// ---------- Zcash height index ----------

// HasZcashHeight returns true if the given Zcash height has been accepted.
func (s *State) HasZcashHeight(height uint64) bool {
	key := zcashHeightKey(height)
	s.mu.RLock()
	defer s.mu.RUnlock()
	has, _ := s.zcashHeightDB.Has(key)
	return has
}

// GetBlockByZcashHeight looks up the accepted Avalanche block that embedded
// the given Zcash height.
func (s *State) GetBlockByZcashHeight(height uint64) (*Block, error) {
	key := zcashHeightKey(height)
	s.mu.RLock()
	blockIDBytes, err := s.zcashHeightDB.Get(key)
	s.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("zcash height %d not found: %w", height, err)
	}

	var blockID ids.ID
	copy(blockID[:], blockIDBytes)
	return s.GetBlock(blockID)
}

// ---------- Zcash block data ----------

// PutZcashData stores the full Zcash block JSON for a given height.
func (s *State) PutZcashData(height uint64, data []byte) error {
	key := zcashHeightKey(height)
	return s.zcashDataDB.Put(key, data)
}

// GetZcashData retrieves the full Zcash block JSON for a given height.
func (s *State) GetZcashData(height uint64) ([]byte, error) {
	key := zcashHeightKey(height)
	return s.zcashDataDB.Get(key)
}

// GetAllZcashHeights returns all accepted Zcash heights and their block IDs
// by iterating the zcash height index. Results are in ascending key order.
func (s *State) GetAllZcashHeights() ([]ZcashHeightEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	iter := s.zcashHeightDB.NewIterator()
	defer iter.Release()

	var entries []ZcashHeightEntry
	for iter.Next() {
		key := iter.Key()
		val := iter.Value()
		if len(key) != 8 || len(val) != ids.IDLen {
			continue
		}
		height := binary.BigEndian.Uint64(key)
		var blockID ids.ID
		copy(blockID[:], val)
		entries = append(entries, ZcashHeightEntry{
			ZcashHeight: height,
			BlockID:     blockID,
		})
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate zcash heights: %w", err)
	}
	return entries, nil
}

// ZcashHeightEntry holds a zcash height and its corresponding Avalanche block ID.
type ZcashHeightEntry struct {
	ZcashHeight uint64
	BlockID     ids.ID
}

// ---------- Wallet address ----------

// PutWalletAddr stores the originating wallet address for a Zcash height.
// Used to emit the outbound warp message in Accept().
func (s *State) PutWalletAddr(zcashHeight uint64, wallet [20]byte) error {
	key := zcashHeightKey(zcashHeight)
	return s.walletDB.Put(key, wallet[:])
}

// GetWalletAddr retrieves the wallet address stored for a Zcash height.
// Returns false if no address was stored (e.g., legacy 32-byte payload).
func (s *State) GetWalletAddr(zcashHeight uint64) ([20]byte, bool) {
	key := zcashHeightKey(zcashHeight)
	data, err := s.walletDB.Get(key)
	if err != nil || len(data) != 20 {
		return [20]byte{}, false
	}
	var wallet [20]byte
	copy(wallet[:], data)
	return wallet, true
}

// ---------- Metadata ----------

// GetLastAccepted returns the ID of the last accepted block.
func (s *State) GetLastAccepted() (ids.ID, error) {
	data, err := s.metaDB.Get(lastAcceptedKey)
	if err != nil {
		return ids.Empty, err
	}
	if len(data) != ids.IDLen {
		return ids.Empty, fmt.Errorf("invalid last accepted ID length: %d", len(data))
	}
	var id ids.ID
	copy(id[:], data)
	return id, nil
}

// SetLastAccepted persists the last accepted block ID.
func (s *State) SetLastAccepted(id ids.ID) error {
	return s.metaDB.Put(lastAcceptedKey, id[:])
}

// ---------- Helpers ----------

func zcashHeightKey(height uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, height)
	return key
}

// ---------- Outbound warp message persistence ----------
//
// Key layout: [avaxHeight 8-bytes big-endian][msgID 32-bytes]
// Big-endian height ensures LevelDB iteration is in ascending height order,
// matching the block-cursor approach used by the ICM relayer.

// OutboundMsgRecord is a single persisted outbound warp message with its
// Avalanche chain height so the relayer can use it as a cursor.
type OutboundMsgRecord struct {
	AvaxHeight uint64
	MsgID      ids.ID
	MsgBytes   []byte
}

func outboundKey(avaxHeight uint64, msgID ids.ID) []byte {
	key := make([]byte, 8+ids.IDLen)
	binary.BigEndian.PutUint64(key[:8], avaxHeight)
	copy(key[8:], msgID[:])
	return key
}

// PutOutboundWarpMsg persists an outbound warp message to LevelDB.
func (s *State) PutOutboundWarpMsg(avaxHeight uint64, msgID ids.ID, msgBytes []byte) error {
	return s.outboundDB.Put(outboundKey(avaxHeight, msgID), msgBytes)
}

// GetOutboundWarpMsgsSince returns all persisted outbound messages with
// avaxHeight >= fromHeight, in ascending height order.
func (s *State) GetOutboundWarpMsgsSince(fromHeight uint64) ([]OutboundMsgRecord, error) {
	iter := s.outboundDB.NewIteratorWithStart(outboundKey(fromHeight, ids.Empty))
	defer iter.Release()

	var records []OutboundMsgRecord
	for iter.Next() {
		key := iter.Key()
		if len(key) != 8+ids.IDLen {
			continue
		}
		var rec OutboundMsgRecord
		rec.AvaxHeight = binary.BigEndian.Uint64(key[:8])
		copy(rec.MsgID[:], key[8:])
		rec.MsgBytes = append([]byte(nil), iter.Value()...)
		records = append(records, rec)
	}
	return records, iter.Error()
}

// DeleteOutboundWarpMsg removes a delivered outbound warp message from LevelDB.
func (s *State) DeleteOutboundWarpMsg(avaxHeight uint64, msgID ids.ID) error {
	return s.outboundDB.Delete(outboundKey(avaxHeight, msgID))
}

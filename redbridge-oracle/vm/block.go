// Package vm implements the Zcash Bridge VM for AvalancheGo.
//
// Block binary format (deterministic, version-tagged):
//
//	[1  byte ] version         – always 0x01
//	[32 bytes] parentID        – parent Avalanche block ID
//	[8  bytes] height          – Avalanche chain height (big-endian uint64)
//	[8  bytes] timestamp       – Unix seconds (big-endian int64)
//	[8  bytes] zcashHeight     – Zcash block height (big-endian uint64)
//	[2  bytes] zcashHashLen    – length of zcash hash string (big-endian uint16)
//	[N  bytes] zcashHash       – Zcash block hash as hex string
package vm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

const (
	// blockVersion is the serialization format version tag.
	blockVersion byte = 0x01
	// blockFixedSize = version(1) + parentID(32) + height(8) + timestamp(8) + zcashHeight(8) + hashLen(2)
	blockFixedSize = 1 + ids.IDLen + 8 + 8 + 8 + 2
)

var (
	errInvalidBlockVersion = errors.New("invalid block version")
	errInvalidBlockFormat  = errors.New("invalid block format: too short")
	errHashTooLong         = errors.New("zcash hash exceeds maximum length")
)

// Compile-time check: Block must satisfy snowman.Block.
var _ snowman.Block = (*Block)(nil)

// Block is a single block in the Zcash Bridge VM linear chain.
// Each block (except genesis) embeds data from exactly one Zcash block.
type Block struct {
	// --- Serialized fields ---
	PrntID      ids.ID // parent Avalanche block ID
	Hght        uint64 // Avalanche chain height
	Tmstmp      int64  // deterministic timestamp (Unix seconds)
	ZcashHeight uint64 // embedded Zcash block height (0 for genesis)
	ZcashHash   string // embedded Zcash block hash hex (empty for genesis)

	// --- Runtime fields (not serialized) ---
	ZcashData []byte // full Zcash block JSON (not part of consensus bytes)
	id        ids.ID
	bytes     []byte
	status    choices.Status
	vm        *VM // back-pointer for state access during consensus
}

// ---------- snowman.Block interface ----------

func (b *Block) ID() ids.ID             { return b.id }
func (b *Block) Parent() ids.ID         { return b.PrntID }
func (b *Block) Height() uint64         { return b.Hght }
func (b *Block) Bytes() []byte          { return b.bytes }
func (b *Block) Status() choices.Status { return b.status }
func (b *Block) Timestamp() time.Time   { return time.Unix(b.Tmstmp, 0) }

// Verify checks that this block is valid according to the chain rules.
// Called by other validators when they receive a proposed block.
func (b *Block) Verify(ctx context.Context) error {
	if b.vm == nil {
		return errors.New("block has no VM reference")
	}

	b.vm.log.Info("Verify: starting block verification",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("zcashHash", b.ZcashHash),
		zap.Stringer("parentID", b.PrntID),
		zap.Int64("timestamp", b.Tmstmp),
	)

	// 1. Parent must exist.
	parent, err := b.vm.getBlock(b.PrntID)
	if err != nil {
		b.vm.log.Warn("Verify: parent block not found",
			zap.Stringer("blockID", b.id),
			zap.Stringer("parentID", b.PrntID),
			zap.Error(err),
		)
		return fmt.Errorf("parent block %s not found: %w", b.PrntID, err)
	}
	b.vm.log.Debug("Verify: parent found",
		zap.Stringer("parentID", parent.id),
		zap.Uint64("parentHeight", parent.Hght),
		zap.Int64("parentTimestamp", parent.Tmstmp),
	)

	// 2. Height must be parent + 1 (linear chain).
	if b.Hght != parent.Hght+1 {
		b.vm.log.Warn("Verify: height mismatch",
			zap.Stringer("blockID", b.id),
			zap.Uint64("expected", parent.Hght+1),
			zap.Uint64("got", b.Hght),
		)
		return fmt.Errorf("expected height %d, got %d", parent.Hght+1, b.Hght)
	}

	// 3. Timestamp monotonicity: must be >= parent timestamp.
	if b.Tmstmp < parent.Tmstmp {
		b.vm.log.Warn("Verify: timestamp regression",
			zap.Stringer("blockID", b.id),
			zap.Int64("blockTimestamp", b.Tmstmp),
			zap.Int64("parentTimestamp", parent.Tmstmp),
		)
		return fmt.Errorf("timestamp %d < parent timestamp %d", b.Tmstmp, parent.Tmstmp)
	}

	// 4. Zcash height must not be zero (reserved for genesis).
	if b.ZcashHeight == 0 {
		b.vm.log.Warn("Verify: zcash height is 0 (genesis reserved)",
			zap.Stringer("blockID", b.id),
		)
		return errors.New("zcash height 0 is reserved for genesis")
	}

	// 5. No duplicate Zcash height in accepted state.
	if b.vm.state.HasZcashHeight(b.ZcashHeight) {
		b.vm.log.Warn("Verify: duplicate zcash height already accepted",
			zap.Stringer("blockID", b.id),
			zap.Uint64("zcashHeight", b.ZcashHeight),
		)
		return fmt.Errorf("zcash height %d already accepted", b.ZcashHeight)
	}

	// 6. Cross-validate Zcash block hash against our own RPC node.
	b.vm.log.Debug("Verify: cross-validating zcash hash via RPC",
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("expectedHash", b.ZcashHash),
	)
	info, err := b.vm.zcash.GetBlockByHeight(ctx, b.ZcashHeight)
	if err != nil {
		b.vm.log.Warn("Verify: failed to fetch zcash block for cross-validation",
			zap.Stringer("blockID", b.id),
			zap.Uint64("zcashHeight", b.ZcashHeight),
			zap.Error(err),
		)
		return fmt.Errorf("failed to fetch zcash block %d for verification: %w", b.ZcashHeight, err)
	}
	if info.Hash != b.ZcashHash {
		b.vm.log.Warn("Verify: zcash hash mismatch",
			zap.Stringer("blockID", b.id),
			zap.Uint64("zcashHeight", b.ZcashHeight),
			zap.String("blockHash", b.ZcashHash),
			zap.String("nodeHash", info.Hash),
		)
		return fmt.Errorf("zcash hash mismatch at height %d: block has %s, local node has %s",
			b.ZcashHeight, b.ZcashHash, info.Hash)
	}

	// Store the full Zcash block data for persistence in Accept.
	b.ZcashData = info.RawData

	b.vm.log.Info("Verify: block verified successfully",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("zcashHash", b.ZcashHash),
	)
	return nil
}

// Accept is called when consensus finalizes this block.
// NOTE: The Snowman engine serializes calls to Accept, BuildBlock, etc.,
// so there is no concurrent lock contention with BuildBlock.
func (b *Block) Accept(ctx context.Context) error {
	b.vm.log.Info("Accept: starting block acceptance",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("zcashHash", b.ZcashHash),
	)

	b.status = choices.Accepted

	// Update in-memory VM state first (consistent lock ordering: vm.mu
	// is always acquired before state.mu when both are needed).
	b.vm.mu.Lock()
	oldLastAccepted := b.vm.lastAccepted
	b.vm.lastAccepted = b.id
	b.vm.preferred = b.id
	b.vm.mu.Unlock()

	b.vm.log.Debug("Accept: VM state updated in memory",
		zap.Stringer("oldLastAccepted", oldLastAccepted),
		zap.Stringer("newLastAccepted", b.id),
	)

	// Persist block and update indices (acquires state.mu internally).
	if err := b.vm.state.PutBlock(b); err != nil {
		b.vm.log.Error("Accept: failed to persist block",
			zap.Stringer("blockID", b.id),
			zap.Error(err),
		)
		return fmt.Errorf("persist block: %w", err)
	}

	// Ensure we have the full Zcash data. It may be nil if the engine
	// re-parsed the block bytes (ParseBlock loses runtime-only fields).
	if len(b.ZcashData) == 0 && b.ZcashHeight > 0 {
		b.vm.log.Debug("Accept: ZcashData missing, re-fetching from RPC",
			zap.Uint64("zcashHeight", b.ZcashHeight),
		)
		if info, fetchErr := b.vm.zcash.GetBlockByHeight(ctx, b.ZcashHeight); fetchErr == nil {
			b.ZcashData = info.RawData
			b.vm.log.Debug("Accept: re-fetched ZcashData successfully",
				zap.Uint64("zcashHeight", b.ZcashHeight),
				zap.Int("dataLen", len(b.ZcashData)),
			)
		} else {
			b.vm.log.Warn("Accept: failed to fetch zcash data (will be missing from API)",
				zap.Uint64("zcashHeight", b.ZcashHeight),
				zap.Error(fetchErr),
			)
		}
	}

	// Persist full Zcash block data (stored separately from consensus bytes).
	if len(b.ZcashData) > 0 {
		if err := b.vm.state.PutZcashData(b.ZcashHeight, b.ZcashData); err != nil {
			b.vm.log.Error("Accept: failed to persist zcash data",
				zap.Uint64("zcashHeight", b.ZcashHeight),
				zap.Error(err),
			)
			return fmt.Errorf("persist zcash data: %w", err)
		}
	} else {
		b.vm.log.Warn("Accept: no ZcashData to persist",
			zap.Uint64("zcashHeight", b.ZcashHeight),
		)
	}

	if err := b.vm.state.SetLastAccepted(b.id); err != nil {
		b.vm.log.Error("Accept: failed to set last accepted",
			zap.Stringer("blockID", b.id),
			zap.Error(err),
		)
		return fmt.Errorf("set last accepted: %w", err)
	}

	// Remove height from mempool seen-set so future duplicate API calls
	// are handled by the state lookup path instead.
	b.vm.mempool.Remove(b.ZcashHeight)

	// If a wallet address was stored for this height (v2 payload from
	// ZcashHeightGateway), emit an outbound warp message so the ICM relayer
	// can deliver the message to VMWarpReceiver on C-Chain.
	if b.ZcashHeight > 0 {
		if wallet, ok := b.vm.state.GetWalletAddr(b.ZcashHeight); ok {
			var zeroWallet [20]byte
			if wallet != zeroWallet {
				b.vm.emitOutboundWarpMessage(b.ZcashHeight, wallet, b.Hght)
			}
		}
	}

	b.vm.log.Info("Accept: block accepted successfully",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("zcashHash", b.ZcashHash),
		zap.Int("mempoolRemaining", b.vm.mempool.Len()),
	)

	if b.vm.metrics != nil {
		b.vm.metrics.blocksAccepted.Inc()
	}
	return nil
}

// Reject is called when consensus rejects this block.
func (b *Block) Reject(ctx context.Context) error {
	b.vm.log.Info("Reject: starting block rejection",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.String("zcashHash", b.ZcashHash),
	)

	b.status = choices.Rejected

	// Remove height from mempool seen-set so the API no longer reports
	// "pending" and the height can be re-requested by a future API call.
	b.vm.mempool.Remove(b.ZcashHeight)

	b.vm.log.Info("Reject: block rejected, height removed from mempool",
		zap.Stringer("blockID", b.id),
		zap.Uint64("avaxHeight", b.Hght),
		zap.Uint64("zcashHeight", b.ZcashHeight),
		zap.Int("mempoolRemaining", b.vm.mempool.Len()),
	)

	if b.vm.metrics != nil {
		b.vm.metrics.blocksRejected.Inc()
	}
	return nil
}

// ---------- Serialization ----------

// MarshalBlock serializes the block into a deterministic byte representation.
func MarshalBlock(b *Block) ([]byte, error) {
	hashBytes := []byte(b.ZcashHash)
	if len(hashBytes) > 65535 {
		return nil, errHashTooLong
	}

	size := blockFixedSize + len(hashBytes)
	buf := make([]byte, size)

	offset := 0

	// Version tag
	buf[offset] = blockVersion
	offset++

	// Parent ID (32 bytes)
	copy(buf[offset:], b.PrntID[:])
	offset += ids.IDLen

	// Height (8 bytes, big-endian)
	binary.BigEndian.PutUint64(buf[offset:], b.Hght)
	offset += 8

	// Timestamp (8 bytes, big-endian, stored as uint64 bit pattern)
	binary.BigEndian.PutUint64(buf[offset:], uint64(b.Tmstmp))
	offset += 8

	// Zcash height (8 bytes, big-endian)
	binary.BigEndian.PutUint64(buf[offset:], b.ZcashHeight)
	offset += 8

	// Zcash hash length + data
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(hashBytes)))
	offset += 2
	copy(buf[offset:], hashBytes)

	return buf, nil
}

// UnmarshalBlock deserializes block bytes into a Block struct.
// The returned block has id and bytes set but vm and status are unset.
func UnmarshalBlock(data []byte) (*Block, error) {
	if len(data) < blockFixedSize {
		return nil, errInvalidBlockFormat
	}

	offset := 0

	ver := data[offset]
	if ver != blockVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", errInvalidBlockVersion, ver, blockVersion)
	}
	offset++

	var parentID ids.ID
	copy(parentID[:], data[offset:offset+ids.IDLen])
	offset += ids.IDLen

	height := binary.BigEndian.Uint64(data[offset:])
	offset += 8

	timestamp := int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	zcashHeight := binary.BigEndian.Uint64(data[offset:])
	offset += 8

	hashLen := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2

	if len(data) < offset+hashLen {
		return nil, fmt.Errorf("%w: need %d more bytes for hash", errInvalidBlockFormat, offset+hashLen-len(data))
	}
	zcashHash := string(data[offset : offset+hashLen])

	blk := &Block{
		PrntID:      parentID,
		Hght:        height,
		Tmstmp:      timestamp,
		ZcashHeight: zcashHeight,
		ZcashHash:   zcashHash,
	}
	blk.bytes = make([]byte, len(data))
	copy(blk.bytes, data)
	blk.id = computeBlockID(blk.bytes)

	return blk, nil
}

// computeBlockID derives a deterministic block ID from its serialized bytes.
func computeBlockID(data []byte) ids.ID {
	return ids.ID(sha256.Sum256(data))
}

// initBlock sets the computed fields (bytes, id) on a Block.  Used by
// BuildBlock and genesis creation.
func initBlock(b *Block) error {
	raw, err := MarshalBlock(b)
	if err != nil {
		return err
	}
	b.bytes = raw
	b.id = computeBlockID(raw)
	return nil
}

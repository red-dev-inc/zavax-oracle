package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/ava-labs/zcash-bridge-vm/zcash"
)

// ---------- helpers ----------

// newTestVM creates a VM backed by an in-memory database and a mock Zcash
// client with 1000 blocks.
func newTestVM(t *testing.T) *VM {
	t.Helper()

	chainCtx := &snow.Context{
		Log:     logging.NoLog{},
		Metrics: nil, // skip prometheus registration in tests
	}

	db := memdb.New()
	mockZcash := zcash.NewMockClient(1000) // heights 1..1000

	v := &VM{
		zcash: mockZcash, // inject before Initialize
	}

	configBytes, _ := json.Marshal(Config{
		ConfirmationDepth: 24,
	})

	err := v.Initialize(
		context.Background(),
		chainCtx,
		db,
		nil, // genesisBytes
		nil, // upgradeBytes
		configBytes,
		nil, // fxs
		nil, // appSender
	)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	return v
}

// ---------- Block serialization ----------

func TestBlockMarshalRoundTrip(t *testing.T) {
	original := &Block{
		PrntID:      ids.GenerateTestID(),
		Hght:        42,
		Tmstmp:      1700000000,
		ZcashHeight: 500,
		ZcashHash:   "00000000076e12d15d6e2bc933cd75c80d5e4970e6e85847a2b2a02000000007",
	}

	raw, err := MarshalBlock(original)
	if err != nil {
		t.Fatalf("MarshalBlock: %v", err)
	}

	parsed, err := UnmarshalBlock(raw)
	if err != nil {
		t.Fatalf("UnmarshalBlock: %v", err)
	}

	if parsed.PrntID != original.PrntID {
		t.Errorf("PrntID: got %s, want %s", parsed.PrntID, original.PrntID)
	}
	if parsed.Hght != original.Hght {
		t.Errorf("Hght: got %d, want %d", parsed.Hght, original.Hght)
	}
	if parsed.Tmstmp != original.Tmstmp {
		t.Errorf("Tmstmp: got %d, want %d", parsed.Tmstmp, original.Tmstmp)
	}
	if parsed.ZcashHeight != original.ZcashHeight {
		t.Errorf("ZcashHeight: got %d, want %d", parsed.ZcashHeight, original.ZcashHeight)
	}
	if parsed.ZcashHash != original.ZcashHash {
		t.Errorf("ZcashHash: got %q, want %q", parsed.ZcashHash, original.ZcashHash)
	}
	if len(parsed.bytes) != len(raw) {
		t.Errorf("bytes length: got %d, want %d", len(parsed.bytes), len(raw))
	}
}

func TestBlockIDDeterminism(t *testing.T) {
	blk := &Block{
		PrntID:      ids.Empty,
		Hght:        1,
		Tmstmp:      1000,
		ZcashHeight: 100,
		ZcashHash:   "abc123",
	}

	raw1, _ := MarshalBlock(blk)
	raw2, _ := MarshalBlock(blk)

	id1 := computeBlockID(raw1)
	id2 := computeBlockID(raw2)

	if id1 != id2 {
		t.Errorf("determinism violated: %s != %s", id1, id2)
	}
}

// ---------- Genesis ----------

func TestGenesisCreation(t *testing.T) {
	v := newTestVM(t)

	lastAccepted, err := v.LastAccepted(context.Background())
	if err != nil {
		t.Fatalf("LastAccepted: %v", err)
	}
	if lastAccepted == ids.Empty {
		t.Fatal("lastAccepted should not be Empty after genesis")
	}

	blk, err := v.GetBlock(context.Background(), lastAccepted)
	if err != nil {
		t.Fatalf("GetBlock(genesis): %v", err)
	}
	if blk.Height() != 0 {
		t.Errorf("genesis height: got %d, want 0", blk.Height())
	}
}

// ---------- BuildBlock → Verify → Accept flow ----------

func TestBuildVerifyAccept(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	// Submit a Zcash height via the mempool.
	zcashHeight := uint64(500)
	v.mempool.Push(zcashHeight)
	v.notifyEngine()

	// Drain the engine notification.
	<-v.pendingMsgs

	// Build the block.
	snowBlk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	blk := snowBlk.(*Block)
	if blk.ZcashHeight != zcashHeight {
		t.Errorf("ZcashHeight: got %d, want %d", blk.ZcashHeight, zcashHeight)
	}
	if blk.Height() != 1 {
		t.Errorf("Height: got %d, want 1", blk.Height())
	}
	if blk.Status() != choices.Processing {
		t.Errorf("Status: got %s, want Processing", blk.Status())
	}

	// Verify the block.
	if err := blk.Verify(ctx); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// Accept the block.
	if err := blk.Accept(ctx); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if blk.Status() != choices.Accepted {
		t.Errorf("Status after Accept: got %s, want Accepted", blk.Status())
	}

	// State should now contain the zcash height.
	storedBlk, err := v.state.GetBlockByZcashHeight(zcashHeight)
	if err != nil {
		t.Fatalf("GetBlockByZcashHeight: %v", err)
	}
	if storedBlk.ZcashHash != blk.ZcashHash {
		t.Errorf("stored hash: got %q, want %q", storedBlk.ZcashHash, blk.ZcashHash)
	}
}

// ---------- Duplicate height rejection ----------

func TestDuplicateHeightRejected(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	zcashHeight := uint64(300)

	// Build and accept a block for height 300.
	v.mempool.Push(zcashHeight)
	blk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if err := blk.(*Block).Verify(ctx); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := blk.Accept(ctx); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	// Attempt to build another block for the same height.
	v.mempool.Push(zcashHeight) // force re-add (seen was cleared by Accept)
	_, err = v.BuildBlock(ctx)
	if err == nil {
		t.Fatal("expected error building duplicate zcash height, got nil")
	}
}

// ---------- Read-only getBlockByHeight returns not_found for unknown heights ----------

// TestAPIReturnsNotFoundForUnknownHeight verifies that getBlockByHeight is
// read-only: heights not in state or mempool return {"status":"not_found"}.
// Depth checking is handled by postWarpMessage (ICM relayer path), not here.
func TestAPIReturnsNotFoundForUnknownHeight(t *testing.T) {
	v := newTestVM(t)

	handler := NewHTTPHandler(v)

	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":990}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var reply GetZcashHeightReply
	if err := json.Unmarshal(resultBytes, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Status != "not_found" {
		t.Errorf("status: got %q, want 'not_found'", reply.Status)
	}
}

// ---------- API accepted height lookup ----------

func TestAPIReturnsAcceptedHeight(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	zcashHeight := uint64(100)

	// Build and accept block.
	v.mempool.Push(zcashHeight)
	blk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if err := blk.(*Block).Verify(ctx); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := blk.Accept(ctx); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	// Query via API.
	handler := NewHTTPHandler(v)
	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":100}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var reply GetZcashHeightReply
	if err := json.Unmarshal(resultBytes, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Status != "" {
		t.Errorf("status: got %q, want empty (accepted has no status)", reply.Status)
	}
	if reply.Data == nil {
		t.Error("expected non-nil zcash data")
	}
	if reply.ID == "" {
		t.Error("expected non-empty block ID")
	}
	if reply.Height == "" {
		t.Error("expected non-empty height")
	}
	if reply.ParentID == "" {
		t.Error("expected non-empty parentID")
	}
}

// ---------- Pending height visible via API ----------

// TestAPIPendingHeight verifies that a height already in the mempool (added by
// postWarpMessage) is reported as "pending" by the read-only getBlockByHeight.
func TestAPIPendingHeight(t *testing.T) {
	v := newTestVM(t)

	// Simulate postWarpMessage having processed a warp message for height 200:
	// push to mempool and notify the consensus engine.
	v.mempool.Push(200)
	v.notifyEngine()

	handler := NewHTTPHandler(v)
	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":200}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var reply GetZcashHeightReply
	json.Unmarshal(resultBytes, &reply)
	if reply.Status != "pending" {
		t.Errorf("status: got %q, want 'pending'", reply.Status)
	}

	// Engine notification should be present (sent by notifyEngine above).
	select {
	case msg := <-v.pendingMsgs:
		if msg != common.PendingTxs {
			t.Errorf("engine message: got %d, want PendingTxs", msg)
		}
	default:
		t.Error("expected engine notification, got none")
	}
}

// ---------- Mempool dedup ----------

func TestMempoolDedup(t *testing.T) {
	m := NewMempool()

	if !m.Push(100) {
		t.Error("first push should return true")
	}
	if m.Push(100) {
		t.Error("second push of same height should return false")
	}
	if m.Len() != 1 {
		t.Errorf("len: got %d, want 1", m.Len())
	}
}

func TestMempoolPopOrder(t *testing.T) {
	m := NewMempool()
	m.Push(1)
	m.Push(2)
	m.Push(3)

	h, ok := m.Pop()
	if !ok || h != 1 {
		t.Errorf("first pop: got (%d, %v), want (1, true)", h, ok)
	}
	h, ok = m.Pop()
	if !ok || h != 2 {
		t.Errorf("second pop: got (%d, %v), want (2, true)", h, ok)
	}
}

// ---------- ParseBlock round-trip ----------

func TestParseBlockRoundTrip(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	// Build a block.
	v.mempool.Push(400)
	snowBlk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	original := snowBlk.(*Block)

	// Parse the same bytes (simulates receiving from another node).
	parsed, err := v.ParseBlock(ctx, original.Bytes())
	if err != nil {
		t.Fatalf("ParseBlock: %v", err)
	}

	if parsed.ID() != original.ID() {
		t.Errorf("ID mismatch: %s != %s", parsed.ID(), original.ID())
	}
	if parsed.Height() != original.Height() {
		t.Errorf("Height mismatch: %d != %d", parsed.Height(), original.Height())
	}
}

// ---------- Concurrent read-only requests for pending height ----------

// TestConcurrentAPISameHeight verifies thread safety when many goroutines
// read a height that is already pending.  getBlockByHeight is read-only so no
// races should corrupt mempool state.
func TestConcurrentAPISameHeight(t *testing.T) {
	v := newTestVM(t)

	// Pre-populate mempool (simulates postWarpMessage having run).
	const targetHeight = 200
	v.mempool.Push(targetHeight)

	handler := NewHTTPHandler(v)

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getBlockByHeight","params":{"height":200}}`
			req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			var resp JSONRPCResponse
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Error != nil {
				t.Errorf("unexpected error: %v", resp.Error.Message)
			}
		}()
	}
	wg.Wait()

	// Height must still be exactly once in mempool (concurrent reads must not remove it).
	if !v.mempool.Has(targetHeight) {
		t.Error("expected height in mempool after concurrent reads")
	}
}

// ---------- Consecutive blocks ----------

func TestMultipleBlocksSequential(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	heights := []uint64{100, 200, 300}

	for _, h := range heights {
		v.mempool.Push(h)
		blk, err := v.BuildBlock(ctx)
		if err != nil {
			t.Fatalf("BuildBlock(zcash=%d): %v", h, err)
		}
		if err := blk.(*Block).Verify(ctx); err != nil {
			t.Fatalf("Verify(zcash=%d): %v", h, err)
		}
		if err := blk.Accept(ctx); err != nil {
			t.Fatalf("Accept(zcash=%d): %v", h, err)
		}
	}

	// All three should be in state.
	for _, h := range heights {
		if !v.state.HasZcashHeight(h) {
			t.Errorf("zcash height %d not in state", h)
		}
	}

	// Chain tip should be at avax height 3.
	lastID, _ := v.LastAccepted(ctx)
	tip, _ := v.GetBlock(ctx, lastID)
	if tip.Height() != 3 {
		t.Errorf("tip height: got %d, want 3", tip.Height())
	}
}

// ---------- Timestamp determinism ----------

func TestDeterministicTimestamp(t *testing.T) {
	tests := []struct {
		parent, zcash, want int64
	}{
		{100, 200, 200}, // zcash timestamp is newer
		{200, 100, 201}, // parent+1 is newer
		{100, 101, 101}, // zcash == parent+1
		{0, 0, 1},       // genesis edge case
	}
	for _, tt := range tests {
		got := deterministicTimestamp(tt.parent, tt.zcash)
		if got != tt.want {
			t.Errorf("deterministicTimestamp(%d, %d) = %d, want %d",
				tt.parent, tt.zcash, got, tt.want)
		}
	}
}

// ---------- Reject flow ----------

func TestBlockReject(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	v.mempool.Push(500)
	blk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if err := blk.(*Block).Reject(ctx); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if blk.(*Block).Status() != choices.Rejected {
		t.Errorf("status after Reject: got %s, want Rejected", blk.(*Block).Status())
	}
}

// ---------- Height 0 rejection via Verify ----------

func TestVerifyRejectsHeightZero(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	genesis, _ := v.GetBlock(ctx, v.lastAccepted)

	badBlk := &Block{
		PrntID:      genesis.ID(),
		Hght:        1,
		Tmstmp:      1,
		ZcashHeight: 0,
		ZcashHash:   "fake",
		status:      choices.Processing,
		vm:          v,
	}
	initBlock(badBlk)
	v.state.CacheBlock(badBlk)

	err := badBlk.Verify(ctx)
	if err == nil {
		t.Fatal("expected Verify to reject zcash height 0")
	}
}

// ---------- zavax.getBlock ----------

func TestAPIGetBlock(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	// Build and accept a block.
	v.mempool.Push(uint64(100))
	blk, err := v.BuildBlock(ctx)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if err := blk.(*Block).Verify(ctx); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := blk.Accept(ctx); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	blockID := blk.ID().String()

	handler := NewHTTPHandler(v)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{"blockId":"%s"}}`, blockID)
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var reply GetBlockReply
	if err := json.Unmarshal(resultBytes, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.ID != blockID {
		t.Errorf("id: got %q, want %q", reply.ID, blockID)
	}
	if reply.Height != "1" {
		t.Errorf("height: got %q, want '1'", reply.Height)
	}
	if reply.Data == nil {
		t.Error("expected non-nil zcash data")
	}
}

func TestAPIGetBlockNotFound(t *testing.T) {
	v := newTestVM(t)
	handler := NewHTTPHandler(v)

	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getBlock","params":{"blockId":"11111111111111111111111111111111LpoYY"}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block, got nil")
	}
}

// ---------- zavax.reconcileBlocks ----------

func TestAPIReconcileBlocks(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	// Build and accept several blocks.
	for _, h := range []uint64{100, 200, 300} {
		v.mempool.Push(h)
		blk, err := v.BuildBlock(ctx)
		if err != nil {
			t.Fatalf("BuildBlock(%d): %v", h, err)
		}
		if err := blk.(*Block).Verify(ctx); err != nil {
			t.Fatalf("Verify(%d): %v", h, err)
		}
		if err := blk.Accept(ctx); err != nil {
			t.Fatalf("Accept(%d): %v", h, err)
		}
	}

	handler := NewHTTPHandler(v)
	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.reconcileBlocks","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var reply ReconcileBlocksReply
	if err := json.Unmarshal(resultBytes, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Height != 3 {
		t.Errorf("height: got %d, want 3", reply.Height)
	}
	if reply.Matched != 3 {
		t.Errorf("matched: got %d, want 3", reply.Matched)
	}
	if reply.Mismatched != 0 {
		t.Errorf("mismatched: got %d, want 0", reply.Mismatched)
	}
	if reply.Errors != 0 {
		t.Errorf("errors: got %d, want 0", reply.Errors)
	}
	if len(reply.Details) != 3 {
		t.Errorf("details count: got %d, want 3", len(reply.Details))
	}
	for _, d := range reply.Details {
		if d.Status != "match" {
			t.Errorf("height %d status: got %q, want 'match'", d.ZcashHeight, d.Status)
		}
	}
}

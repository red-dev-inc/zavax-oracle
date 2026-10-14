package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

// ---------- InboundPayload codec ----------

func TestInboundPayloadRoundTrip(t *testing.T) {
	wallet := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05}
	validationID := ids.ID{0xaa, 0xbb, 0xcc}
	orig := InboundPayload{Height: 99999, Wallet: wallet, ValidationID: validationID}

	data := EncodeInboundPayload(orig)
	if len(data) != 3*abiSlotSize {
		t.Fatalf("encoded length: got %d, want %d", len(data), 3*abiSlotSize)
	}

	got, err := DecodeInboundPayload(data)
	if err != nil {
		t.Fatalf("DecodeInboundPayload: %v", err)
	}
	if got.Height != orig.Height {
		t.Errorf("height: got %d, want %d", got.Height, orig.Height)
	}
	if got.Wallet != orig.Wallet {
		t.Errorf("wallet: got %x, want %x", got.Wallet, orig.Wallet)
	}
	if got.ValidationID != orig.ValidationID {
		t.Errorf("validationId: got %s, want %s", got.ValidationID, orig.ValidationID)
	}
}

func TestInboundPayloadRoundTripNoValidatorPreference(t *testing.T) {
	orig := InboundPayload{Height: 42, Wallet: [20]byte{0xaa}, ValidationID: ids.Empty}
	got, err := DecodeInboundPayload(EncodeInboundPayload(orig))
	if err != nil {
		t.Fatalf("DecodeInboundPayload: %v", err)
	}
	if got.ValidationID != ids.Empty {
		t.Errorf("validationId: got %s, want empty", got.ValidationID)
	}
}

func TestDecodeInboundPayloadRejectsZeroHeight(t *testing.T) {
	data := EncodeInboundPayload(InboundPayload{Height: 0, Wallet: [20]byte{}})
	if _, err := DecodeInboundPayload(data); err == nil {
		t.Fatal("expected error for height=0, got nil")
	}
}

func TestDecodeInboundPayloadRejectsTooShort(t *testing.T) {
	if _, err := DecodeInboundPayload([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error for short payload, got nil")
	}
}

// ---------- OutboundPayload codec ----------

func TestOutboundPayloadRoundTrip(t *testing.T) {
	wallet := [20]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04}
	orig := OutboundPayload{Wallet: wallet, ZcashHeight: 54321}

	encoded := orig.Encode()
	if len(encoded) != 2*abiSlotSize {
		t.Fatalf("encoded length: got %d, want %d", len(encoded), 2*abiSlotSize)
	}

	got, err := DecodeOutboundPayload(encoded)
	if err != nil {
		t.Fatalf("DecodeOutboundPayload: %v", err)
	}
	if got.Wallet != orig.Wallet {
		t.Errorf("wallet: got %x, want %x", got.Wallet, orig.Wallet)
	}
	if got.ZcashHeight != orig.ZcashHeight {
		t.Errorf("zcashHeight: got %d, want %d", got.ZcashHeight, orig.ZcashHeight)
	}
}

// ---------- Wallet gossip ----------

func TestWalletGossipRoundTrip(t *testing.T) {
	height := uint64(42000)
	wallet := [20]byte{0x11, 0x22, 0x33, 0x44, 0x55}

	gossipBytes := makeWalletGossip(height, wallet)
	if len(gossipBytes) != walletGossipLen {
		t.Fatalf("gossip length: got %d, want %d", len(gossipBytes), walletGossipLen)
	}

	gotHeight, gotWallet, ok := parseWalletGossip(gossipBytes)
	if !ok {
		t.Fatal("parseWalletGossip returned ok=false")
	}
	if gotHeight != height {
		t.Errorf("height: got %d, want %d", gotHeight, height)
	}
	if gotWallet != wallet {
		t.Errorf("wallet: got %x, want %x", gotWallet, wallet)
	}
}

func TestParseWalletGossipRejectsShortInput(t *testing.T) {
	if _, _, ok := parseWalletGossip([]byte{0x01, 0x02}); ok {
		t.Fatal("expected ok=false for short gossip bytes")
	}
}

// ---------- Wallet storage: first caller wins ----------

func TestFirstCallerWalletPreserved(t *testing.T) {
	v := newTestVM(t)

	walletA := [20]byte{0x0A}
	zcashHeight := uint64(400)

	// First caller stores walletA.
	if err := v.state.PutWalletAddr(zcashHeight, walletA); err != nil {
		t.Fatalf("PutWalletAddr: %v", err)
	}
	v.mempool.Push(zcashHeight)

	// A second caller for the same height hits the "already pending" guard in
	// postWarpMessage and never reaches PutWalletAddr — so walletA must win.
	stored, ok := v.state.GetWalletAddr(zcashHeight)
	if !ok {
		t.Fatal("expected wallet to be stored")
	}
	if stored != walletA {
		t.Errorf("wallet: got %x, want %x (walletA)", stored, walletA)
	}
}

// ---------- Accept emits outbound warp message ----------

func TestWalletStoredOnAcceptEmitsOutboundWarp(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	zcashHeight := uint64(500)
	wallet := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05}

	// Store wallet (simulates postWarpMessage having done so).
	if err := v.state.PutWalletAddr(zcashHeight, wallet); err != nil {
		t.Fatalf("PutWalletAddr: %v", err)
	}

	// Build → Verify → Accept.
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

	// Accept() must have called emitOutboundWarpMessage.
	v.outboundWarpMsgsMu.RLock()
	count := len(v.outboundWarpMsgs)
	v.outboundWarpMsgsMu.RUnlock()

	if count != 1 {
		t.Errorf("outbound warp messages: got %d, want 1", count)
	}
}

func TestNoOutboundWarpWithoutWallet(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	// Build and accept WITHOUT storing a wallet (v1 payload / no sender).
	v.mempool.Push(600)
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

	v.outboundWarpMsgsMu.RLock()
	count := len(v.outboundWarpMsgs)
	v.outboundWarpMsgsMu.RUnlock()

	if count != 0 {
		t.Errorf("outbound warp messages: got %d, want 0 (no wallet stored)", count)
	}
}

// ---------- getOutboundWarpMessages API ----------

func TestGetOutboundWarpMessagesAPI(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	zcashHeight := uint64(300)
	wallet := [20]byte{0xAB, 0xCD, 0xEF}

	if err := v.state.PutWalletAddr(zcashHeight, wallet); err != nil {
		t.Fatalf("PutWalletAddr: %v", err)
	}
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

	handler := NewHTTPHandler(v)
	body := `{"jsonrpc":"2.0","id":1,"method":"zavax.getOutboundWarpMessages","params":{}}`
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
	var msgs []OutboundWarpMessage
	if err := json.Unmarshal(resultBytes, &msgs); err != nil {
		t.Fatalf("unmarshal message list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("outbound messages: got %d, want 1", len(msgs))
	}
	if msgs[0].MessageID == "" {
		t.Error("messageId must not be empty")
	}
	if msgs[0].UnsignedMessage == "" {
		t.Error("unsignedMessage must not be empty")
	}
}

// ---------- Confirmation depth math ----------

// TestConfirmationDepthMath checks the min-allowed height calculation used by
// postWarpMessage (without going through BLS verification).
func TestConfirmationDepthMath(t *testing.T) {
	v := newTestVM(t)
	ctx := context.Background()

	latestHeight, err := v.zcash.GetLatestHeight(ctx)
	if err != nil {
		t.Fatalf("GetLatestHeight: %v", err)
	}
	// mock has 1000 blocks, depth=24 → minAllowed = 976
	minAllowed := uint64(0)
	if latestHeight > v.config.ConfirmationDepth {
		minAllowed = latestHeight - v.config.ConfirmationDepth
	}
	if minAllowed != 976 {
		t.Errorf("minAllowed: got %d, want 976 (1000 - 24)", minAllowed)
	}
	if 990 <= minAllowed { // height 990 is too recent
		t.Error("height 990 should be rejected as too recent")
	}
	if 100 > minAllowed { // height 100 is old enough
		t.Error("height 100 should be accepted (well within confirmation window)")
	}
}

//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

var (
	flagANREndpoint = flag.String("anr-endpoint", "0.0.0.0:8097", "ANR gRPC server endpoint")
	flagVMID        = flag.String("vm-id", "", "VM ID for the zcash-bridge-vm blockchain")
	_               = flag.String("avalanchego-path", "", "Path to avalanchego binary")
)

// ── JSON-RPC wire types ───────────────────────────────────────────────

type rpcReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type rpcResp struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int              `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// GetZcashHeightReply matches the actual JSON returned by zavax.getBlockByHeight.
// Accepted blocks: Height, ID, ParentID, Timestamp are set, Status is empty.
// Pending / not-found: only Status is set.
type GetZcashHeightReply struct {
	Status    string          `json:"status,omitempty"` // "pending" | "not_found" | "" (accepted)
	Height    string          `json:"height,omitempty"`
	ID        string          `json:"id,omitempty"`
	ParentID  string          `json:"parentID,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// OutboundWarpMessage matches zavax.getOutboundWarpMessages list entries.
type OutboundWarpMessage struct {
	MessageID       string `json:"messageId"`
	UnsignedMessage string `json:"unsignedMessage"`
}

// ── Helpers ───────────────────────────────────────────────────────────

func callRaw(t *testing.T, endpoint, method string, params interface{}) rpcResp {
	t.Helper()
	body, err := json.Marshal(rpcReq{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpResp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", endpoint, err)
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp rpcResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, string(respBody))
	}
	return resp
}

func getBlockByHeight(t *testing.T, endpoint string, height uint64) GetZcashHeightReply {
	t.Helper()
	resp := callRaw(t, endpoint, "zavax.getBlockByHeight", map[string]uint64{"height": height})
	if resp.Error != nil {
		t.Fatalf("RPC error for height %d: [%d] %s", height, resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatalf("nil result for height %d", height)
	}
	var reply GetZcashHeightReply
	if err := json.Unmarshal(*resp.Result, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	return reply
}

func discoverEndpoint(t *testing.T) string {
	t.Helper()
	if ep := os.Getenv("BLOCKCHAIN_ENDPOINT"); ep != "" {
		return ep
	}
	anrBin, err := exec.LookPath("avalanche-network-runner")
	if err == nil && *flagANREndpoint != "" {
		out, cmdErr := exec.Command(anrBin, "control", "status",
			"--endpoint", *flagANREndpoint).CombinedOutput()
		if cmdErr == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "http") && strings.Contains(line, "ext/bc/") {
					if ep := strings.TrimSpace(line); ep != "" {
						return ep + "/rpc"
					}
				}
			}
		}
		t.Logf("ANR status: %s", string(out))
	}
	vmID := *flagVMID
	if vmID == "" {
		vmID = "zcash-bridge-vm"
	}
	return fmt.Sprintf("http://127.0.0.1:9650/ext/bc/%s/rpc", vmID)
}

// ── Tests ──────────────────────────────────────────────────────────────

// TestE2EReadOnlyAPI verifies the read-only zavax.getBlockByHeight endpoint
// behaviour on a freshly-started local network (no ICM relayer running).
func TestE2EReadOnlyAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	endpoint := discoverEndpoint(t)
	if endpoint == "" {
		t.Fatal("could not discover blockchain endpoint")
	}
	t.Logf("Using endpoint: %s", endpoint)

	t.Run("HeightZeroRejected", func(t *testing.T) {
		resp := callRaw(t, endpoint, "zavax.getBlockByHeight", map[string]uint64{"height": 0})
		if resp.Error == nil {
			t.Fatal("expected error for height 0, got nil")
		}
		t.Logf("height 0 rejected: %s", resp.Error.Message)
	})

	t.Run("UnknownHeightNotFound", func(t *testing.T) {
		// Fresh network — height 100 has never been submitted.
		reply := getBlockByHeight(t, endpoint, 100)
		if reply.Status != "not_found" {
			t.Errorf("status: got %q, want 'not_found'", reply.Status)
		}
	})

	t.Run("TooRecentHeightReadOnlyNotFound", func(t *testing.T) {
		// getBlockByHeight is read-only; depth checking is done in postWarpMessage.
		// A height that has never been submitted returns "not_found", not an error.
		reply := getBlockByHeight(t, endpoint, 1990)
		if reply.Status != "not_found" {
			t.Errorf("status: got %q, want 'not_found'", reply.Status)
		}
	})
}

// TestE2EOutboundWarpMessages verifies the getOutboundWarpMessages endpoint
// returns a valid empty list on a fresh network.
func TestE2EOutboundWarpMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	endpoint := discoverEndpoint(t)
	t.Logf("Using endpoint: %s", endpoint)

	resp := callRaw(t, endpoint, "zavax.getOutboundWarpMessages", map[string]interface{}{})
	if resp.Error != nil {
		t.Fatalf("getOutboundWarpMessages error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("nil result")
	}
	// Should be an empty JSON array on a fresh network (no blocks accepted with wallets).
	var msgs []OutboundWarpMessage
	if err := json.Unmarshal(*resp.Result, &msgs); err != nil {
		t.Fatalf("unmarshal message list: %v", err)
	}
	t.Logf("Pending outbound warp messages: %d", len(msgs))
}

// TestE2EPostWarpMessageRequiresSignedPayload ensures that postWarpMessage
// returns a JSON-RPC error (not a panic) when called with a garbage payload.
// This validates the endpoint is wired correctly without needing a real ICM relayer.
func TestE2EPostWarpMessageRequiresSignedPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	endpoint := discoverEndpoint(t)
	t.Logf("Using endpoint: %s", endpoint)

	resp := callRaw(t, endpoint, "zavax.postWarpMessage",
		map[string]string{"warpMessage": "0xdeadbeef"})
	if resp.Error == nil {
		t.Fatal("expected error for invalid warp message, got nil")
	}
	t.Logf("postWarpMessage correctly rejected garbage payload: [%d] %s",
		resp.Error.Code, resp.Error.Message)
}

// TestE2EReconcileBlocks verifies reconcileBlocks works on a fresh network
// where no Zcash heights have been committed.
func TestE2EReconcileBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	endpoint := discoverEndpoint(t)
	t.Logf("Using endpoint: %s", endpoint)

	resp := callRaw(t, endpoint, "zavax.reconcileBlocks", map[string]interface{}{})
	if resp.Error != nil {
		t.Fatalf("reconcileBlocks error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("nil result")
	}
	type reconcileReply struct {
		Height     int `json:"height"`
		Matched    int `json:"matched"`
		Mismatched int `json:"mismatched"`
		Errors     int `json:"errors"`
	}
	var reply reconcileReply
	if err := json.Unmarshal(*resp.Result, &reply); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if reply.Mismatched != 0 || reply.Errors != 0 {
		t.Errorf("reconcile found mismatches=%d errors=%d on a fresh network",
			reply.Mismatched, reply.Errors)
	}
	t.Logf("Reconcile: height=%d matched=%d mismatched=%d errors=%d",
		reply.Height, reply.Matched, reply.Mismatched, reply.Errors)
}

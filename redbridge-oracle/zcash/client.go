package zcash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

var (
	// ErrZcashRPC is returned when the Zcash RPC returns an error.
	ErrZcashRPC = errors.New("zcash rpc error")
	// ErrTimeout is returned when the Zcash RPC request times out.
	ErrTimeout = errors.New("zcash rpc timeout")
)

// Client defines the interface for querying Zcash blockchain data.
// Implementations must be safe for concurrent use.
type Client interface {
	// GetBlockByHeight returns the block info for a given Zcash block height.
	GetBlockByHeight(ctx context.Context, height uint64) (*BlockInfo, error)
	// GetLatestHeight returns the current tip height of the Zcash blockchain.
	GetLatestHeight(ctx context.Context) (uint64, error)
}

// RPCClient implements Client by connecting to a Zcash JSON-RPC endpoint.
type RPCClient struct {
	endpoint string
	user     string
	password string
	client   *http.Client
	reqID    atomic.Int64

	// Retry configuration
	MaxRetries    int
	RetryInterval time.Duration
}

// NewRPCClient creates a new Zcash RPC client with sensible defaults.
func NewRPCClient(endpoint, user, password string) *RPCClient {
	return &RPCClient{
		endpoint: endpoint,
		user:     user,
		password: password,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		MaxRetries:    3,
		RetryInterval: 2 * time.Second,
	}
}

// rpcRequest is the JSON-RPC request envelope.
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// rpcResponse is the JSON-RPC response envelope.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("zcash rpc error %d: %s", e.Code, e.Message)
}

// call performs a JSON-RPC call with retry logic.
func (c *RPCClient) call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
	if params == nil {
		params = []interface{}{}
	}

	reqBody := rpcRequest{
		JSONRPC: "2.0",
		ID:      c.reqID.Add(1),
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v (after %d attempts, last error: %v)",
					ErrTimeout, ctx.Err(), attempt, lastErr)
			case <-time.After(c.RetryInterval):
			}
		}

		result, err := c.doRequest(ctx, body)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("%w: %v (exhausted %d retries)", ErrZcashRPC, lastErr, c.MaxRetries)
}

func (c *RPCClient) doRequest(ctx context.Context, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	return rpcResp.Result, nil
}

// GetBlockByHeight fetches block info for the given Zcash height.
func (c *RPCClient) GetBlockByHeight(ctx context.Context, height uint64) (*BlockInfo, error) {
	// Step 1: Get block hash by height
	hashResult, err := c.call(ctx, "getblockhash", height)
	if err != nil {
		return nil, fmt.Errorf("getblockhash(%d): %w", height, err)
	}

	var hash string
	if err := json.Unmarshal(hashResult, &hash); err != nil {
		return nil, fmt.Errorf("unmarshal hash: %w", err)
	}

	// Step 2: Get block details (verbosity=1 for JSON without txs)
	blockResult, err := c.call(ctx, "getblock", hash, 1)
	if err != nil {
		return nil, fmt.Errorf("getblock(%s): %w", hash, err)
	}

	var block struct {
		Hash   string `json:"hash"`
		Height uint64 `json:"height"`
		Time   int64  `json:"time"`
	}
	if err := json.Unmarshal(blockResult, &block); err != nil {
		return nil, fmt.Errorf("unmarshal block: %w", err)
	}

	return &BlockInfo{
		Height:  block.Height,
		Hash:    block.Hash,
		Time:    block.Time,
		RawData: blockResult,
	}, nil
}

// GetLatestHeight returns the current Zcash chain tip height.
func (c *RPCClient) GetLatestHeight(ctx context.Context) (uint64, error) {
	result, err := c.call(ctx, "getblockcount")
	if err != nil {
		return 0, fmt.Errorf("getblockcount: %w", err)
	}

	var height uint64
	if err := json.Unmarshal(result, &height); err != nil {
		return 0, fmt.Errorf("unmarshal height: %w", err)
	}
	return height, nil
}

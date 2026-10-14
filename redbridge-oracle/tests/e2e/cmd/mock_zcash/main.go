package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

const latestHeight uint64 = 2000

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func deterministicHash(height uint64) string {
	return fmt.Sprintf("%064x", height)
}

func hashToHeight(hash string) uint64 {
	h := strings.TrimLeft(hash, "0")
	if h == "" {
		return 0
	}
	n, _ := strconv.ParseUint(h, 16, 64)
	return n
}

func toUint64(v interface{}) (uint64, error) {
	switch n := v.(type) {
	case float64:
		return uint64(n), nil
	case json.Number:
		i, err := n.Int64()
		return uint64(i), err
	case string:
		return strconv.ParseUint(n, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to uint64", v)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "parse json", http.StatusBadRequest)
		return
	}

	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "getblockcount":
		resp.Result = latestHeight

	case "getblockhash":
		if len(req.Params) < 1 {
			resp.Error = map[string]interface{}{"code": -1, "message": "missing height param"}
		} else {
			height, err := toUint64(req.Params[0])
			if err != nil {
				resp.Error = map[string]interface{}{"code": -1, "message": err.Error()}
			} else if height > latestHeight {
				resp.Error = map[string]interface{}{"code": -8, "message": "Block height out of range"}
			} else {
				resp.Result = deterministicHash(height)
			}
		}

	case "getblock":
		if len(req.Params) < 1 {
			resp.Error = map[string]interface{}{"code": -1, "message": "missing hash param"}
		} else {
			hash, ok := req.Params[0].(string)
			if !ok {
				resp.Error = map[string]interface{}{"code": -1, "message": "hash must be string"}
			} else {
				height := hashToHeight(hash)
				prevHash := ""
				if height > 0 {
					prevHash = deterministicHash(height - 1)
				}
				nextHash := ""
				if height < latestHeight {
					nextHash = deterministicHash(height + 1)
				}
				resp.Result = map[string]interface{}{
					"hash":             hash,
					"confirmations":    int64(latestHeight - height),
					"size":             0,
					"height":           height,
					"version":          4,
					"merkleroot":       "",
					"blockcommitments": "",
					"authdataroot":     "",
					"finalsaplingroot": "",
					"chainhistoryroot": "",
					"tx":               []string{},
					"time":             int64(height) * 60,
					"nonce":            "",
					"solution":         "",
					"bits":             "",
					"difficulty":       0.0,
					"chainwork":        "",
					"anchor":           "",
					"chainSupply": map[string]interface{}{
						"monitored":     false,
						"chainValue":    0,
						"chainValueZat": 0,
						"valueDelta":    0,
						"valueDeltaZat": 0,
					},
					"valuePools":        nil,
					"previousblockhash": prevHash,
					"nextblockhash":     nextHash,
				}
			}
		}

	default:
		resp.Error = map[string]interface{}{"code": -32601, "message": "method not found: " + req.Method}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	addr := ":18232"
	log.Printf("Mock Zcash RPC server listening on %s (latest height: %d)\n", addr, latestHeight)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(addr, nil))
}

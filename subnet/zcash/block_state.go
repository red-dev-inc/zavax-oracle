// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zcash

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"	
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"fmt"
)

const (
	lastAcceptedByte byte = iota
)

const (
	// maximum block capacity of the cache
	blockCacheSize = 8192
)

// persists lastAccepted block IDs with this key
var lastAcceptedKey = []byte{lastAcceptedByte}

var _ BlockState = &blockState{}

type ChainSupply struct {
	Monitored       bool    `json:"monitored"`
	ChainValue      float64 `json:"chainValue"`
	ChainValueZat   int64   `json:"chainValueZat"`
	ValueDelta      float64 `json:"valueDelta"`
	ValueDeltaZat   int64   `json:"valueDeltaZat"`
}

type ValuePool struct {
	ID             string  `json:"id"`
	Monitored      bool    `json:"monitored"`
	ChainValue     float64 `json:"chainValue"`
	ChainValueZat  int64   `json:"chainValueZat"`
	ValueDelta     float64 `json:"valueDelta"`
	ValueDeltaZat  int64   `json:"valueDeltaZat"`
}

type ZcashBlock struct {
	Hash               string        `json:"hash"`
	Confirmations      int           `json:"confirmations"`
	Size               int           `json:"size"`
	Height             int           `json:"height"`
	Version            int           `json:"version"`
	MerkleRoot         string        `json:"merkleroot"`
	BlockCommitments   string        `json:"blockcommitments"`
	AuthDataRoot       string        `json:"authdataroot"`
	FinalSaplingRoot   string        `json:"finalsaplingroot"`
	ChainHistoryRoot   string        `json:"chainhistoryroot"`
	Tx                 []string      `json:"tx"`
	Time               int           `json:"time"`
	Nonce              string        `json:"nonce"`
	Solution           string        `json:"solution"`
	Bits               string        `json:"bits"`
	Difficulty         float64       `json:"difficulty"`
	ChainWork          string        `json:"chainwork"`
	Anchor             string        `json:"anchor"`
	ChainSupply        ChainSupply   `json:"chainSupply"`
	ValuePools         []ValuePool   `json:"valuePools"`
	PreviousBlockHash  string        `json:"previousblockhash"`
	NextBlockHash      string        `json:"nextblockhash"`
}


// BlockState defines methods to manage state with Blocks and LastAcceptedIDs.
type BlockState interface {
	GetBlock(blkID ids.ID) (*Block, error)
	GetBlockByHeight(ID uint64)(ZcashBlock)
	PutBlock(blk *Block) error
	GetLastAccepted() (ids.ID, error)
	SetLastAccepted(ids.ID) error
	QueryData(ID uint64) (ZcashBlock, error)
}

// blockState implements BlocksState interface with database and cache.
type blockState struct {
	// cache to store blocks
	blkCache cache.Cacher[ids.ID, *Block]
	// block database
	blockDB      database.Database
	lastAccepted ids.ID

	// vm reference
	vm *VM
}

// blkWrapper wraps the actual blk bytes and status to persist them together
type blkWrapper struct {
	Blk    []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

// NewBlockState returns BlockState with a new cache and given db
func NewBlockState(db database.Database, vm *VM) BlockState {
	return &blockState{
		blkCache: &cache.LRU[ids.ID, *Block]{Size: blockCacheSize},
		blockDB:  db,
		vm:       vm,
	}
}

// GetBlock gets Block from either cache or database
func (s *blockState) GetBlock(blkID ids.ID) (*Block, error) {
	// Check if cache has this blkID
	if blk, cached := s.blkCache.Get(blkID); cached {
		// there is a key but value is nil, so return an error
		if blk == nil {
			return nil, database.ErrNotFound
		}
		// We found it return the block in cache
		return blk, nil
	}

	// get block bytes from db with the blkID key
	wrappedBytes, err := s.blockDB.Get(blkID[:])
	if err != nil {
		// we could not find it in the db, let's cache this blkID with nil value
		// so next time we try to fetch the same key we can return error
		// without hitting the database
		if err == database.ErrNotFound {
			s.blkCache.Put(blkID, nil)
		}
		// could not find the block, return error
		return nil, err
	}

	// first decode/unmarshal the block wrapper so we can have status and block bytes
	blkw := blkWrapper{}
	if _, err := Codec.Unmarshal(wrappedBytes, &blkw); err != nil {
		return nil, err
	}

	// now decode/unmarshal the actual block bytes to block
	blk := &Block{}
	if _, err := Codec.Unmarshal(blkw.Blk, blk); err != nil {
		return nil, err
	}

	// initialize block with block bytes, status and vm
	blk.Initialize(blkw.Blk, blkw.Status, s.vm)

	// put block into cache
	s.blkCache.Put(blkID, blk)

	return blk, nil
}

// PutBlock puts block into both database and cache
func (s *blockState) PutBlock(blk *Block) error {
	// create block wrapper with block bytes and status
	blkw := blkWrapper{
		Blk:    blk.Bytes(),
		Status: blk.Status(),
	}

	// encode block wrapper to its byte representation
	wrappedBytes, err := Codec.Marshal(CodecVersion, &blkw)
	if err != nil {
		return err
	}

	blkID := blk.ID()
	// put actual block to cache, so we can directly fetch it from cache
	s.blkCache.Put(blkID, blk)

	// put wrapped block bytes into database
	return s.blockDB.Put(blkID[:], wrappedBytes)
}

// DeleteBlock deletes block from both cache and database
func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.blkCache.Put(blkID, nil)
	return s.blockDB.Delete(blkID[:])
}

// GetLastAccepted returns last accepted block ID
func (s *blockState) GetLastAccepted() (ids.ID, error) {
	// check if we already have lastAccepted ID in state memory
	if s.lastAccepted != ids.Empty {
		return s.lastAccepted, nil
	}

	// get lastAccepted bytes from database with the fixed lastAcceptedKey
	lastAcceptedBytes, err := s.blockDB.Get(lastAcceptedKey)
	if err != nil {
		return ids.ID{}, err
	}
	// parse bytes to ID
	lastAccepted, err := ids.ToID(lastAcceptedBytes)
	if err != nil {
		return ids.ID{}, err
	}
	// put lastAccepted ID into memory
	s.lastAccepted = lastAccepted
	return lastAccepted, nil
}

// SetLastAccepted persists lastAccepted ID into both cache and database
func (s *blockState) SetLastAccepted(lastAccepted ids.ID) error {
	// if the ID in memory and the given memory are same don't do anything
	if s.lastAccepted == lastAccepted {
		return nil
	}
	// put lastAccepted ID to memory
	s.lastAccepted = lastAccepted
	// persist lastAccepted ID to database with fixed lastAcceptedKey
	return s.blockDB.Put(lastAcceptedKey, lastAccepted[:])
}



func (s *blockState) GetBlockByHeight(hgt uint64) (ZcashBlock) {	

	expectedHeight := hgt
	result := 0

	fmt.Printf("expectedHeight: %+v\n",hgt)

	id, err := s.vm.state.GetLastAccepted()
	fmt.Printf("GetLastAccepted: %+v\n",id)
	zblock := ZcashBlock{}
	if err != nil {
		return zblock
	}

	for int(expectedHeight) != result {
		// Get the block from the database
		fmt.Printf("fetching last id: %+v\n",id)
		block, err := s.vm.getBlock(id)
		if err != nil {
			return ZcashBlock{}
		}
		// Convert JSON to struct
		err = json.Unmarshal(block.Dt, &zblock)
		if err != nil {
			return ZcashBlock{}
		}

		id = block.PrntID
		result = zblock.Height
		fmt.Printf("id: %+v\n",id)
		fmt.Printf("result: %+v\n",result)
		if block.Hght == 0 {
			fmt.Printf("Block height is 0 hence break loop %+v\n",block.Hght)
			break
		}
	}
	fmt.Printf("final zblock Hght: %+v\n",zblock.Height)
	fmt.Printf("final expected: %+v\n",int(expectedHeight))
	fmt.Printf("final result: %+v\n",result)
	if int(expectedHeight) != result {
		fmt.Printf("final not match: %+v\n",result)
		return  ZcashBlock{}
	}

	return zblock
}

// GetBlock gets Block from either cache or database
func (s *blockState) QueryData(ID uint64) (ZcashBlock, error) {	

	url := "http://127.0.0.1:8232/"
	
	hash := getZcashHash(ID)
	payload := map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      "curltest",
		"method":  "getblock",
		"params":  []interface{}{hash},
	}

	if hash == "" {
		return ZcashBlock{}, nil
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return ZcashBlock{}, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return ZcashBlock{}, err
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("zcash-user:Hw9!6an0i7c&")))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ZcashBlock{}, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ZcashBlock{}, err
	}

	var responseData struct {
		Result ZcashBlock `json:"result"`
	}
	err = json.Unmarshal(respBody, &responseData)
	if err != nil {
		return ZcashBlock{}, err
	}

	block := responseData.Result

	return block, nil
}


func getZcashHash(hgt uint64) (string){

	url := "http://127.0.0.1:8232/"

	payload := map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      "curltest",
		"method":  "getblockhash",
		"params":  []uint64{hgt},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "" 
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return ""
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("zcash-user:Hw9!6an0i7c&")))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var responseData struct {
		Result string `json:"result"`
	}
	err = json.Unmarshal(respBody, &responseData)
	if err != nil {
		return ""
	}

	hash := responseData.Result

	return hash

}


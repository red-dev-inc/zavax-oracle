// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zavax

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"
	log "github.com/inconshreveable/log15"
	"go.uber.org/zap"

	ejson "encoding/json"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/version"
)

const (
	DataLen        = 32
	Name           = "zavax"
	MaxMempoolSize = 4096
)

var (
	errNoPendingBlocks = errors.New("there is no block to propose")
	errBadGenesisBytes = errors.New("genesis data should be bytes (max length 32)")
	Version            = &version.Semantic{
		Major: 1,
		Minor: 3,
		Patch: 3,
	}

	_ block.ChainVM = &VM{}
)

// VM implements the snowman.VM interface
// Each block in this chain contains a Unix timestamp
// and a piece of data (a string)
type VM struct {
	// The context of this vm
	snowCtx   *snow.Context
	dbManager database.Database
	config    Config

	// State of this VM
	state State

	// ID of the preferred block
	preferred ids.ID

	// channel to send messages to the consensus engine
	toEngine chan<- common.Message

	// Proposed pieces of data that haven't been put into a block and proposed yet
	mempool [][]byte

	// Block ID --> Block
	// Each element is a block that passed verification but
	// hasn't yet been accepted/rejected
	verifiedBlocks map[ids.ID]*Block

	// Indicates that this VM has finised bootstrapping for the chain
	bootstrapped utils.Atomic[bool]
}

// GetBlockIDAtHeight implements block.ChainVM.
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

// VerifyHeightIndex implements block.ChainVM.
func (*VM) VerifyHeightIndex(context.Context) error {
	return nil
}

// Initialize this vm
// [ctx] is this vm's context
// [dbManager] is the manager of this vm's database
// [toEngine] is used to notify the consensus engine that new blocks are
//
//	ready to be added to consensus
//
// The data in the genesis block is [genesisData]
func (vm *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	dbManager database.Database,
	genesisData []byte,
	_ []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	_ []*common.Fx,
	_ common.AppSender,
) error {
	version, err := vm.Version(ctx)
	if err != nil {
		log.Error("error initializing ZavaX VM: %v", err)
		return err
	}
	log.Info("Initializing ZavaX VM", "Version", version)
	log.Info("Chain config", "config", string(configBytes))
	// Load config
	vm.config.SetDefaults()
	log.Info("Default Config", "Zcash URL", vm.config.Url)
	if len(configBytes) > 0 {
		if err := ejson.Unmarshal(configBytes, &vm.config); err != nil {
			return fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
		log.Info("Override Default Config", "Zcash URL from config file", vm.config.Url)
	}

	vm.dbManager = dbManager
	vm.snowCtx = snowCtx
	vm.toEngine = toEngine
	vm.verifiedBlocks = make(map[ids.ID]*Block)

	// Create new state
	vm.state = NewState(vm.dbManager, vm)

	// Initialize genesis
	if err := vm.initGenesis(genesisData); err != nil {
		return err
	}

	// Get last accepted
	lastAccepted, err := vm.state.GetLastAccepted()
	if err != nil {
		return err
	}

	snowCtx.Log.Info("initializing last accepted block",
		zap.Any("id", lastAccepted),
	)

	// Build off the most recently accepted block
	return vm.SetPreference(ctx, lastAccepted)
}

// Initializes Genesis if required
func (vm *VM) initGenesis(genesisData []byte) error {
	stateInitialized, err := vm.state.IsInitialized()
	if err != nil {
		return err
	}

	// if state is already initialized, skip init genesis.
	if stateInitialized {
		return nil
	}

	if len(genesisData) > DataLen {
		return errBadGenesisBytes
	}

	// genesisData is a byte slice but each block contains an byte array
	// Take the first [DataLen] bytes from genesisData and put them in an array
	genesisDataArr := genesisData
	log.Debug("genesis", "data", genesisDataArr)

	// Create the genesis block
	// ZavaX of genesis block is 0. It has no parent.
	genesisBlock, err := vm.NewBlock(ids.Empty, 0, genesisDataArr, time.Unix(0, 0))
	if err != nil {
		log.Error("error while creating genesis block: %v", err)
		return err
	}

	// Put genesis block to state
	if err := vm.state.PutBlock(genesisBlock); err != nil {
		log.Error("error while saving genesis block: %v", err)
		return err
	}

	// Accept the genesis block
	// Sets [vm.lastAccepted] and [vm.preferred]
	if err := genesisBlock.Accept(context.TODO()); err != nil {
		return fmt.Errorf("error accepting genesis block: %w", err)
	}

	// Mark this vm's state as initialized, so we can skip initGenesis in further restarts
	if err := vm.state.SetInitialized(); err != nil {
		return fmt.Errorf("error while setting db to initialized: %w", err)
	}

	// Flush VM's database to underlying db
	return vm.state.Commit()
}

// CreateHandlers returns a map where:
// Keys: The path extension for this VM's API (empty in this case)
// Values: The handler for the API
func (vm *VM) CreateHandlers(_ context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	requestTracker := NewRequestTracker()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(&Service{vm: vm, tracker: requestTracker}, Name); err != nil {
		return nil, err
	}

	return map[string]http.Handler{
		"": server,
	}, nil
}

// Health implements the common.VM interface
func (*VM) HealthCheck(_ context.Context) (interface{}, error) { return nil, nil }

// BuildBlock returns a block that this vm wants to add to consensus
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	fmt.Printf("BuildBlock : \n")
	if len(vm.mempool) == 0 { // There is no block to be built
		return nil, errNoPendingBlocks
	}

	// Get the value to put in the new block
	value := vm.mempool[0]
	vm.mempool = vm.mempool[1:]

	// Notify consensus engine that there are more pending data for blocks
	// (if that is the case) when done building this block
	if len(vm.mempool) > 0 {
		defer vm.NotifyBlockReady()
	}

	// Gets Preferred Block
	preferredBlock, err := vm.getBlock(vm.preferred)
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block: %w", err)
	}
	preferredHeight := preferredBlock.Height()

	// Build the block with preferred height
	newBlock, err := vm.NewBlock(vm.preferred, preferredHeight+1, value, time.Now())
	if err != nil {
		return nil, fmt.Errorf("couldn't build block: %w", err)
	}

	// Verifies block
	if err := newBlock.Verify(ctx); err != nil {
		return nil, err
	}
	return newBlock, nil
}

// NotifyBlockReady tells the consensus engine that a new block
// is ready to be created
func (vm *VM) NotifyBlockReady() {
	select {
	case vm.toEngine <- common.PendingTxs:
	default:
		vm.snowCtx.Log.Debug("dropping message to consensus engine")
	}
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.getBlock(blkID)
}

func (vm *VM) getBlock(blkID ids.ID) (*Block, error) {
	// If block is in memory, return it.
	if blk, exists := vm.verifiedBlocks[blkID]; exists {
		return blk, nil
	}

	return vm.state.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted(_ context.Context) (ids.ID, error) { return vm.state.GetLastAccepted() }

func (vm *VM) addZcashBlock(block []byte) bool {
	vm.mempool = append(vm.mempool, block)
	vm.NotifyBlockReady()
	return true
}

// ParseBlock parses [bytes] to a snowman.Block
// This function is used by the vm's state to unmarshal blocks saved in state
// and by the consensus layer when it receives the byte representation of a block
// from another node
func (vm *VM) ParseBlock(_ context.Context, bytes []byte) (snowman.Block, error) {
	fmt.Printf("Parse Block :\n")
	// A new empty block
	block := &Block{}

	// Unmarshal the byte repr. of the block into our empty block
	_, err := Codec.Unmarshal(bytes, block)
	if err != nil {
		return nil, err
	}

	// Initialize the block
	block.Initialize(bytes, choices.Processing, vm)

	if blk, err := vm.getBlock(block.ID()); err == nil {
		// If we have seen this block before, return it with the most up-to-date
		// info
		return blk, nil
	}

	// Return the block
	return block, nil
}

// NewBlock returns a new Block where:
// - the block's parent is [parentID]
// - the block's data is [data]
// - the block's timestamp is [timestamp]
func (vm *VM) NewBlock(parentID ids.ID, height uint64, data []byte, timestamp time.Time) (*Block, error) {
	fmt.Printf("NewBlock : \n")
	block := &Block{
		PrntID: parentID,
		Hght:   height,
		Tmstmp: timestamp.Unix(),
		Dt:     data,
	}

	// Get the byte representation of the block
	blockBytes, err := Codec.Marshal(CodecVersion, block)
	if err != nil {
		return nil, err
	}

	// Initialize the block by providing it with its byte representation
	// and a reference to this VM
	block.Initialize(blockBytes, choices.Processing, vm)
	return block, nil
}

// Shutdown this vm
func (vm *VM) Shutdown(_ context.Context) error {
	if vm.state == nil {
		return nil
	}

	return vm.state.Close() // close versionDB
}

// SetPreference sets the block with ID [ID] as the preferred block
func (vm *VM) SetPreference(_ context.Context, id ids.ID) error {
	vm.preferred = id
	return nil
}

// SetState sets this VM state according to given snow.State
func (vm *VM) SetState(_ context.Context, state snow.State) error {
	switch state {
	// Engine reports it's bootstrapping
	case snow.Bootstrapping:
		return vm.onBootstrapStarted()
	case snow.NormalOp:
		// Engine reports it can start normal operations
		return vm.onNormalOperationsStarted()
	default:
		return snow.ErrUnknownState
	}
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	return nil
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	// No need to set it again
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)
	return nil
}

// Returns this VM's version
func (*VM) Version(_ context.Context) (string, error) {
	return Version.String(), nil
}

func (*VM) Connected(_ context.Context, _ ids.NodeID, _ *version.Application) error {
	return nil // noop
}

func (*VM) Disconnected(_ context.Context, _ ids.NodeID) error {
	return nil // noop
}

// This VM doesn't (currently) have any app-specific messages
func (*VM) AppGossip(_ context.Context, _ ids.NodeID, _ []byte) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (*VM) AppRequest(_ context.Context, _ ids.NodeID, _ uint32, _ time.Time, _ []byte) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (*VM) AppResponse(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (*VM) AppRequestFailed(_ context.Context, _ ids.NodeID, _ uint32, _ *common.AppError) error {
	return nil
}

func (*VM) CrossChainAppRequest(_ context.Context, _ ids.ID, _ uint32, _ time.Time, _ []byte) error {
	return nil
}

func (*VM) CrossChainAppRequestFailed(_ context.Context, _ ids.ID, _ uint32, _ *common.AppError) error {
	return nil
}

func (*VM) CrossChainAppResponse(_ context.Context, _ ids.ID, _ uint32, _ []byte) error {
	return nil
}

func (vm *VM) queryZcashBlock(ID uint64, validateConfirm bool) (*ZcashBlock, error) {
	return vm.state.QueryZcashBlock(ID, validateConfirm)
}

func (vm *VM) getBlockByHeight(ID uint64) (*Block, error) {
	return vm.state.GetBlockByHeight(ID)
}

func (vm *VM) reconcileBlocks() ([]int, error) {
	return vm.state.ReconcileBlocks()
}

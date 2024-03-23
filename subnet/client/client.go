// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/red-dev-inc/zavax-oracle/tree/main/subnet/zavax"
)

// Client defines zavax client operations.
type Client interface {
	// GetBlock fetches the contents of a block
	GetBlock(ctx context.Context, blockID *ids.ID) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error)

	GetBlockByHeight(ctx context.Context, blockID uint64) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error)

	ReconcileBlocks(ctx context.Context) ([]uint64, error)

}

// New creates a new client object.
func New(uri string, tracker *zavax.RequestTracker) Client {
	req := rpc.NewEndpointRequester(uri)
	return &client{req: req, tracker: tracker}
}

type client struct {
	req rpc.EndpointRequester
	tracker *zavax.RequestTracker
}


func (cli *client) GetBlock(ctx context.Context, blockID *ids.ID) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error) {
	resp := new(zavax.GetBlockReply)
	err := cli.req.SendRequest(ctx,
		"zavax.getBlock",
		&zavax.GetBlockArgs{ID: blockID},
		resp,
	)

	if err != nil {
		
	}
	return uint64(resp.Timestamp), resp.Data, uint64(resp.Height), resp.ID, resp.ParentID, nil
}

func (cli *client) GetBlockByHeight(ctx context.Context, id uint64) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error) {	
	resp := new(zavax.GetBlockReply)
	err := cli.req.SendRequest(ctx,
		"zavax.getBlockByHeight",
		&zavax.QueryDataArgs{ID: id},
		resp,
	)
	if err != nil {		
	}

	return uint64(resp.Timestamp), resp.Data, uint64(resp.Height), resp.ID, resp.ParentID, nil
}

func (cli *client) ReconcileBlocks(ctx context.Context) ([]uint64, error) {
	resp := new(zavax.GetReconcileReply)
	err := cli.req.SendRequest(ctx,
		"zavax.reconcileBlocks",
		nil,
		resp,
	)
	if err != nil {		
	}

	return resp.Height, nil
}


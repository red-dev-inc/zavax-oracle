// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/red-dev-inc/zavax-oracle/tree/main/subnet/zavax"
)

// Client defines zavax client operations.
type Client interface {
	// GetBlock fetches the contents of a block
	GetBlock(ctx context.Context, blockID *ids.ID) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error)

	GetBlockByHeight(ctx context.Context, blockID uint64) (uint64, zavax.ZcashBlock, uint64, ids.ID, ids.ID, error)

}


// New creates a new client object.
func New(uri string) Client {
	req := NewEndpointRequester(uri, "zavax")
	return &client{req: req}
}

type client struct {
	req *EndpointRequester
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


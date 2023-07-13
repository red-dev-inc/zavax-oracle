// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/tamil-reddev/zcash-oracle/zcash"
)

// Client defines zcash client operations.
type Client interface {
	// GetBlock fetches the contents of a block
	GetBlock(ctx context.Context, blockID *ids.ID) (uint64, zcash.ZcashBlock, uint64, ids.ID, ids.ID, error)

	GetBlockByHeight(ctx context.Context, blockID uint64) (string, uint64, uint64, uint64, error)

}

// New creates a new client object.
func New(uri string) Client {
	req := rpc.NewEndpointRequester(uri)
	return &client{req: req}
}

type client struct {
	req rpc.EndpointRequester
}


func (cli *client) GetBlock(ctx context.Context, blockID *ids.ID) (uint64, zcash.ZcashBlock, uint64, ids.ID, ids.ID, error) {
	resp := new(zcash.GetBlockReply)
	err := cli.req.SendRequest(ctx,
		"zcash.getBlock",
		&zcash.GetBlockArgs{ID: blockID},
		resp,
	)

	if err != nil {
		
	}
	return uint64(resp.Timestamp), resp.Data, uint64(resp.Height), resp.ID, resp.ParentID, nil
}

func (cli *client) GetBlockByHeight(ctx context.Context, id uint64) (string, uint64, uint64,  uint64, error) {
	resp := new(zcash.QueryZcashBlockReply)
	err := cli.req.SendRequest(ctx,
		"zcash.getBlockByHeight",
		&zcash.QueryDataArgs{ID: id},
		resp,
	)
	if err != nil {
		return  "", 0, 0, 0, err
	}

	return resp.Hash, uint64(resp.Confirmations), uint64(resp.Size),  uint64(resp.Height), nil
}

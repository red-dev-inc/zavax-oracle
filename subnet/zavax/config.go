// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zavax

import (
	log "github.com/inconshreveable/log15"
)

type Config struct {
	BlockConfirmHeight       int `serialize:"true" json:"blockConfirmHeight"`
	Url string `serialize:"true" json:"url"`
	SkipUntilHeight  uint64 `serialize:"true" json:"skipUntilHeight"` 
}

func (c *Config) SetDefaults() {
	log.Info("Load default", "Version", "inside default")
	c.BlockConfirmHeight = 24
	c.Url = "http://127.0.0.1:8232/"
	c.SkipUntilHeight = 0
}

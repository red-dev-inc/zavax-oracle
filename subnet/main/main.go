// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"

	log "github.com/inconshreveable/log15"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/red-dev-inc/zavax-oracle/tree/main/subnet/zavax"
)

func main() {
	version, err := PrintVersion()
	if err != nil {
		fmt.Printf("couldn't get config: %s", err)
		os.Exit(1)
	}
	// Print VM ID and exit
	if version {
		fmt.Printf("%s@%s\n", zavax.Name, zavax.Version)
		os.Exit(0)
	}

	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		fmt.Printf("failed to set fd limit correctly due to: %s", err)
		os.Exit(1)
	}

	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat())))

	err = rpcchainvm.Serve(context.Background(), &zavax.VM{})
	if err != nil {
		fmt.Printf("failed to serve due to: %s", err)
		os.Exit(1)
	}
}

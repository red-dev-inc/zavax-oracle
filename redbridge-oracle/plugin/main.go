// Command zcash-bridge-vm is the standalone binary that AvalancheGo launches
// as a subprocess to run the Zcash Bridge VM via the rpcchainvm protocol.
//
// Build:
//
//	go build -o zcash-bridge-vm ./plugin
//
// Then register the binary path in the AvalancheGo VM alias file.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm"

	"github.com/ava-labs/zcash-bridge-vm/vm"
)

func main() {
	fmt.Fprintf(os.Stderr, "[zcash-bridge-vm] plugin process starting (pid=%d)\n", os.Getpid())
	if err := rpcchainvm.Serve(context.Background(), &vm.VM{}); err != nil {
		fmt.Fprintf(os.Stderr, "zcash-bridge-vm: %v\n", err)
		os.Exit(1)
	}
}

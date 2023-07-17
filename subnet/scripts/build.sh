#!/usr/bin/env bash
# (c) 2019-2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

# Load the constants
# Set the PATHS
GOPATH="$(go env GOPATH)"

# zavax root directory
ZAVAX_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Set default binary directory location
binary_directory="$GOPATH/src/github.com/ava-labs/avalanchego/build/plugins"

if [[ $# -eq 1 ]]; then
    binary_directory=$1
elif [[ $# -eq 0 ]]; then
    binary_directory="$GOPATH/src/github.com/ava-labs/avalanchego/build/vu3xjfNfwJcNq1c4yFzvjF2hz6t2HZ4uHaWWQJvo27oyF6czX"
else
    echo "Invalid arguments to build zavax. Requires either no arguments (default) or one arguments to specify binary location."
    exit 1
fi

# Build zavax, which is run as a subprocess
echo "Building zavax in $binary_directory"
go build -o "$binary_directory" "main/"*.go

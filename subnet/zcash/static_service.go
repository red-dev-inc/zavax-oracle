// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zcash

import (
	"errors"
)

var errArgumentDataEmpty = errors.New("argument Data cannot be empty")

// StaticService defines the base service for the timestamp vm
type StaticService struct{}

// CreateStaticService ...
func CreateStaticService() *StaticService {
	return &StaticService{}
}

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"github.com/clastix/capsule/pkg/indexer/namespace"
)

func init() {
	AddToIndexerFuncs = append(AddToIndexerFuncs, namespace.OwnerReference{})
}

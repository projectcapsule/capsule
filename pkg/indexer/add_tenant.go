// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import "github.com/clastix/capsule/pkg/indexer/tenant"

func init() {
	AddToIndexerFuncs = append(AddToIndexerFuncs, tenant.IngressHostnames{})
	AddToIndexerFuncs = append(AddToIndexerFuncs, tenant.NamespacesReference{})
	AddToIndexerFuncs = append(AddToIndexerFuncs, tenant.OwnerReference{})
}

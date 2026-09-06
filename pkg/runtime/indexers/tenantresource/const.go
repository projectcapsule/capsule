// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"

const (
	ServiceAccountIndexerFieldName string = serviceaccount.ReferenceFieldName
	ProcessedIndexerFieldName      string = "status.items"
	CreatedIndexerFieldName        string = "status.items.created"
	NamespaceIndexerFieldName      string = "metadata.namespace"
)

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import "github.com/projectcapsule/capsule/pkg/api/meta"

func processedItemKey(item meta.ObjectReferenceStatus) string {
	ref := item.ResourceID
	if item.ClusterScoped {
		ref.Namespace = ""
	}

	return ref.GetGVKKey("")
}

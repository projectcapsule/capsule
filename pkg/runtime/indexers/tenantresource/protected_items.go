// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// ProtectedItems selects replication parents protecting a managed resource.
type ProtectedItems struct {
	Obj client.Object
}

func (p ProtectedItems) Object() client.Object { return p.Obj }

func (p ProtectedItems) Field() string { return ProtectedIndexerFieldName }

func (p ProtectedItems) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		var items meta.ProcessedItems

		switch resource := object.(type) {
		case *capsulev1beta2.TenantResource:
			items = resource.Status.ProcessedItems
		case *capsulev1beta2.GlobalTenantResource:
			items = resource.Status.ProcessedItems
		default:
			return nil
		}

		keys := make([]string, 0, len(items))

		for _, item := range items {
			protected := item.Created
			if item.Policy != nil {
				protected = item.Policy.IsProtected()
			}

			if protected {
				keys = append(keys, processedItemKey(item))
			}
		}

		return keys
	}
}

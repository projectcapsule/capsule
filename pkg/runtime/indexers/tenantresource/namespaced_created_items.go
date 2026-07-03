// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type NamespacedCreatedItems struct{}

func (g NamespacedCreatedItems) Object() client.Object {
	return &capsulev1beta2.TenantResource{}
}

func (g NamespacedCreatedItems) Field() string {
	return CreatedIndexerFieldName
}

func (g NamespacedCreatedItems) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.TenantResource) //nolint:forcetypeassert

		out := make([]string, 0, len(tgr.Status.ProcessedItems))

		for _, pi := range tgr.Status.ProcessedItems {
			if pi.Created {
				out = append(out, processedItemKey(pi))
			}
		}

		return out
	}
}

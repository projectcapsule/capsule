// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type GlobalCreatedItems struct{}

func (g GlobalCreatedItems) Object() client.Object {
	return &capsulev1beta2.GlobalTenantResource{}
}

func (g GlobalCreatedItems) Field() string {
	return CreatedIndexerFieldName
}

func (g GlobalCreatedItems) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.GlobalTenantResource) //nolint:forcetypeassert

		out := make([]string, 0, len(tgr.Status.ProcessedItems))

		for _, pi := range tgr.Status.ProcessedItems {
			if pi.Created {
				out = append(out, processedItemKey(pi))
			}
		}

		return out
	}
}

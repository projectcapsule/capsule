// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type LocalProcessedItems struct{}

func (g LocalProcessedItems) Object() client.Object {
	return &capsulev1beta2.TenantResource{}
}

func (g LocalProcessedItems) Field() string {
	return IndexerFieldName
}

func (g LocalProcessedItems) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.TenantResource) //nolint:forcetypeassert

		out := make([]string, 0, len(tgr.Status.ProcessedItems))
		for _, pi := range tgr.Status.ProcessedItems {
			out = append(out, pi.String())
		}

		return out
	}
}

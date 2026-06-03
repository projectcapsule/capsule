// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package tenantowners

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	indexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantowner"
)

func (r *TenantOwnerManager) enqueueTenantOwnerRequests(
	ctx context.Context,
	q workqueue.TypedRateLimitingInterface[ctrl.Request],
	owners rbac.OwnerStatusListSpec,
) {
	seen := make(map[types.NamespacedName]struct{}, len(owners))

	for _, owner := range owners {
		if owner.Name == "" {
			continue
		}

		tenantOwnerList := &capsulev1beta2.TenantOwnerList{}
		if err := r.List(
			ctx,
			tenantOwnerList,
			client.MatchingFields{
				indexer.NameIndexerFieldName: owner.Name,
			},
		); err != nil {
			r.Log.Error(
				err,
				"Failed to list TenantOwners by spec.name",
				"ownerName", owner.Name,
				"ownerKind", owner.Kind,
			)

			continue
		}

		for _, tenantOwner := range tenantOwnerList.Items {
			if tenantOwner.Spec.Kind != owner.Kind {
				continue
			}

			key := types.NamespacedName{
				Name: tenantOwner.Name,
			}

			if _, ok := seen[key]; ok {
				continue
			}

			seen[key] = struct{}{}

			q.Add(ctrl.Request{
				NamespacedName: key,
			})
		}
	}
}

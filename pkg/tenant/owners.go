// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func CollectOwners(
	ctx context.Context,
	c client.Client,
	tnt *capsulev1beta2.Tenant,
	cfg configuration.Configuration,
) (api.OwnerStatusListSpec, error) {
	owners := tnt.Spec.Owners.ToStatusOwners()

	// Promoted ServiceAccounts
	if cfg.AllowServiceAccountPromotion() && len(tnt.Status.Namespaces) > 0 {
		saList := &corev1.ServiceAccountList{}
		if err := c.List(ctx, saList,
			client.MatchingLabels{
				meta.OwnerPromotionLabel: meta.ValueTrue,
			},
		); err != nil {
			return nil, err
		}

		for _, sa := range saList.Items {
			for _, ns := range tnt.Status.Namespaces {
				if sa.GetNamespace() != ns {
					continue
				}

				owners.Upsert(api.CoreOwnerSpec{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: serviceaccount.ServiceAccountUsernamePrefix + sa.Namespace + ":" + sa.Name,
					},
					ClusterRoles: cfg.RBAC().PromotionClusterRoles,
				})
			}
		}
	}

	// Administrators
	for _, a := range cfg.Administrators() {
		owners.Upsert(api.CoreOwnerSpec{
			UserSpec:     a,
			ClusterRoles: cfg.RBAC().AdministrationClusterRoles,
		})
	}

	// Dedicated Owner Objects
	listed, err := tnt.Spec.Permissions.ListMatchingOwners(ctx, c, tnt.GetName())
	if err != nil {
		return nil, err
	}

	for _, o := range listed {
		owners.Upsert(o.Spec.CoreOwnerSpec)
	}

	return owners, nil
}

func GetOwnersWithKinds(tenant *capsulev1beta2.Tenant) (owners []string) {
	for _, owner := range tenant.Status.Owners {
		owners = append(owners, fmt.Sprintf("%s:%s", owner.Kind.String(), owner.Name))
	}

	return owners
}

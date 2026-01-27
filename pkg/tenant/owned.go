// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func NamespaceIsOwned(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	userInfo authenticationv1.UserInfo,
) bool {
	for _, ownerRef := range ns.OwnerReferences {
		if !IsTenantOwnerReferenceForTenant(ownerRef, tnt) {
			continue
		}

		return users.IsTenantOwnerByStatus(ctx, c, cfg, tnt, userInfo)
	}

	return false
}

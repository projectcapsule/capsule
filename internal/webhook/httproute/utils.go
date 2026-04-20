// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package httproute

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// TenantFromHTTPRoute returns the Tenant owning the namespace of the given HTTPRoute,
// or nil if the namespace does not belong to any Tenant.
func TenantFromHTTPRoute(ctx context.Context, c client.Client, route *gatewayv1.HTTPRoute) (*capsulev1beta2.Tenant, error) {
	tenantList := &capsulev1beta2.TenantList{}
	if err := c.List(ctx, tenantList, client.MatchingFields{".status.namespaces": route.Namespace}); err != nil {
		return nil, err
	}

	if len(tenantList.Items) == 0 {
		return nil, nil //nolint:nilnil
	}

	return &tenantList.Items[0], nil
}

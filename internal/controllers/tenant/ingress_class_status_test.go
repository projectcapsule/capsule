// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func TestTenantStatusRetiresGatewayClassSnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{capsulev1beta2.AddToScheme, nodev1.AddToScheme, schedulingv1.AddToScheme, storagev1.AddToScheme, gatewayv1.Install} {
		require.NoError(t, add(scheme))
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&schedulingv1.PriorityClass{Name: "priority"}).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, gateway := list.(*gatewayv1.GatewayClassList); gateway {
				return errors.New("must not list GatewayClasses for tenant status")
			}
			return c.List(ctx, list, opts...)
		},
	}).Build()
	tenant := &capsulev1beta2.Tenant{Name: "tenant", Spec: capsulev1beta2.TenantSpec{GatewayOptions: capsulev1beta2.GatewayOptions{AllowedClasses: &api.DefaultAllowedListSpec{Exact: []string{"legacy"}}}},
		Status: capsulev1beta2.TenantStatus{Classes: capsulev1beta2.TenantAvailableClassesStatus{GatewayClasses: []string{"legacy"}}}}
	before := tenant.Spec.DeepCopy()
	m := Manager{reader: c}
	require.NoError(t, m.collectAvailableResources(t.Context(), logr.Discard(), tenant))
	require.Nil(t, tenant.Status.Classes.GatewayClasses)
	require.Equal(t, []string{"priority"}, tenant.Status.Classes.PriorityClasses)
	require.Equal(t, before, &tenant.Spec, "legacy policy remains supported")
}

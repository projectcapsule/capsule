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

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func TestTenantStatusRetiresPriorityClassSnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{capsulev1beta2.AddToScheme, nodev1.AddToScheme, schedulingv1.AddToScheme, storagev1.AddToScheme} {
		require.NoError(t, add(scheme))
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&schedulingv1.PriorityClass{Name: "legacy"},
		&storagev1.StorageClass{Name: "fast"},
	).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, priority := list.(*schedulingv1.PriorityClassList); priority {
				return errors.New("must not list PriorityClasses for tenant status")
			}

			return c.List(ctx, list, opts...)
		},
	}).Build()

	tenant := &capsulev1beta2.Tenant{
		Name: "tenant",
		Spec: capsulev1beta2.TenantSpec{
			PriorityClasses: &api.DefaultAllowedListSpec{Default: "legacy", Exact: []string{"legacy"}},
			StorageClasses:  &api.DefaultAllowedListSpec{Exact: []string{"fast"}},
		},
		Status: capsulev1beta2.TenantStatus{TenantAvailableStatus: capsulev1beta2.TenantAvailableStatus{Classes: capsulev1beta2.TenantAvailableClassesStatus{PriorityClasses: []string{"legacy"}}}},
	}
	before := tenant.Spec.DeepCopy()

	m := Manager{reader: c}
	require.NoError(t, m.collectAvailableResources(t.Context(), logr.Discard(), tenant))
	require.Nil(t, tenant.Status.Classes.PriorityClasses)
	require.Equal(t, []string{"fast"}, tenant.Status.Classes.StorageClasses, "other class snapshots are still collected")
	require.Equal(t, before, &tenant.Spec, "legacy policy remains supported")
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func newResourcePermitCleanupTenant(namespaceName string) *capsulev1beta2.Tenant {
	// The shared namespace fixture can already own a Tenant named namespaceName.
	// Keep this scenario's ownership and cleanup independent of that fixture.
	return &capsulev1beta2.Tenant{
		Name: namespaceName + "-cleanup", Labels: map[string]string{"env": "e2e"},
		Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: resourcePermitLifecycleReviewer, Kind: rbac.UserOwner}}},
	}
}

func TestResourcePermitCleanupTenantDoesNotReuseNamespaceTenant(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Match the tenant-backed BeforeEach fixture on main: both objects share
	// a name, and its owner is different from the lifecycle reviewer.
	existingTenant := &capsulev1beta2.Tenant{
		Name: "e2e-resourcepermit-fixture", Labels: map[string]string{"env": "e2e"},
		Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: "fixture-owner", Kind: rbac.UserOwner}}},
	}
	require.NoError(t, c.Create(ctx, existingTenant))
	existingNamespace := NewNamespace(existingTenant.Name, map[string]string{meta.TenantLabel: existingTenant.Name})
	require.NoError(t, c.Create(ctx, existingNamespace))

	cleanupTenant := newResourcePermitCleanupTenant(existingNamespace.Name)
	require.NoError(t, c.Create(ctx, cleanupTenant), "the cleanup scenario must coexist with the BeforeEach Tenant")
	persisted := &capsulev1beta2.Tenant{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cleanupTenant), persisted))
	require.Equal(t, rbac.OwnerListSpec{{Name: resourcePermitLifecycleReviewer, Kind: rbac.UserOwner}}, persisted.Spec.Owners)

	require.NoError(t, c.Delete(ctx, cleanupTenant))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(existingTenant), persisted))
	require.Equal(t, existingTenant, persisted, "scenario cleanup must preserve the fixture Tenant and its owner")
	currentNamespace := &corev1.Namespace{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(existingNamespace), currentNamespace))
	require.Equal(t, existingNamespace, currentNamespace, "scenario cleanup must preserve the fixture namespace")
}

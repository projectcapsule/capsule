// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestOwnerReferenceHandlerAllowsAdminTenantMigration(t *testing.T) {
	t.Parallel()

	oldTenant := testTenant("green", "green-uid")
	newTenant := testTenant("blue", "blue-uid")
	oldNs := testTenantNamespace("workloads", oldTenant)
	newNs := testTenantNamespace("workloads", newTenant)
	client := testClient(t, oldTenant, newTenant)

	response := (&ownerReferenceHandler{}).OnUpdate(
		client,
		client,
		users.AdmissionUser{Type: users.AdmissionUserAdmin},
		newNs,
		oldNs,
		nil,
		nil,
	)(context.Background(), admission.Request{})

	if response != nil {
		t.Fatalf("expected migration to proceed, got response %#v", response)
	}
	if got := tenant.TenanLabelValue(newNs); got != newTenant.GetName() {
		t.Fatalf("tenant label = %q, want %q", got, newTenant.GetName())
	}
	refs := tenant.TenantOwnerReferences(newNs)
	if len(refs) != 1 || !tenant.IsTenantOwnerReferenceForTenant(refs[0], newTenant) {
		t.Fatalf("tenant ownerReferences = %#v, want only tenant %q", refs, newTenant.GetName())
	}
}

func TestOwnerReferenceHandlerAllowsAdminTenantDetachment(t *testing.T) {
	t.Parallel()

	oldTenant := testTenant("green", "green-uid")
	oldNs := testTenantNamespace("workloads", oldTenant)
	newNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: oldNs.GetName()}}
	client := testClient(t, oldTenant)

	response := (&ownerReferenceHandler{}).OnUpdate(
		client,
		client,
		users.AdmissionUser{Type: users.AdmissionUserAdmin},
		newNs,
		oldNs,
		nil,
		nil,
	)(context.Background(), admission.Request{})

	if response != nil {
		t.Fatalf("expected detachment to proceed, got response %#v", response)
	}
	if tenant.HasTenantReference(newNs) {
		t.Fatalf("detached namespace still has tenant ownership: %#v", newNs.ObjectMeta)
	}
}

func TestOwnerReferenceHandlerDeniesIncompleteAdminTenantReference(t *testing.T) {
	t.Parallel()

	oldTenant := testTenant("green", "green-uid")
	oldNs := testTenantNamespace("workloads", oldTenant)
	newNs := oldNs.DeepCopy()
	newNs.OwnerReferences = nil
	client := testClient(t, oldTenant)

	response := (&ownerReferenceHandler{}).OnUpdate(
		client,
		client,
		users.AdmissionUser{Type: users.AdmissionUserAdmin},
		newNs,
		oldNs,
		nil,
		nil,
	)(context.Background(), admission.Request{})

	if response == nil || response.Allowed {
		t.Fatalf("expected incomplete tenant ownership to be denied, got %#v", response)
	}
}

func testTenant(name string, uid types.UID) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid}}
}

func testTenantNamespace(name string, tnt *capsulev1beta2.Tenant) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{meta.TenantLabel: tnt.GetName()},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: capsulev1beta2.GroupVersion.String(),
			Kind:       tenant.ObjectReferenceTenantKind,
			Name:       tnt.GetName(),
			UID:        tnt.GetUID(),
		}},
	}}
}

func testClient(t *testing.T, tenants ...*capsulev1beta2.Tenant) client.Client {
	t.Helper()

	objects := make([]client.Object, 0, len(tenants))
	for _, tnt := range tenants {
		objects = append(objects, tnt)
	}

	return fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objects...).
		Build()
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule API to scheme: %v", err)
	}

	return scheme
}

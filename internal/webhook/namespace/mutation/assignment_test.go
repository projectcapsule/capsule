// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	capevents "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestOwnerReferenceHandlerRevertsTenantOwnerAssignmentChanges(t *testing.T) {
	t.Parallel()

	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	green := testTenant("green", "green-uid")
	green.Status.Owners = rbac.OwnerStatusListSpec{owner}
	blue := testTenant("blue", "blue-uid")
	oldNs := testTenantNamespace("workloads", green)
	recorder := capevents.NewEventRecorder(nil, logr.Discard(), nil, nil)

	tests := []struct {
		name string
		new  *corev1.Namespace
	}{
		{name: "migration", new: testTenantNamespace("workloads", blue)},
		{
			name: "label migration with empty ownerReferences",
			new: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   "workloads",
				Labels: map[string]string{meta.TenantLabel: blue.Name},
			}},
		},
		{name: "detachment", new: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workloads"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, green, blue)
			newNs := tt.new.DeepCopy()

			response := (&ownerReferenceHandler{}).OnUpdate(
				c,
				c,
				users.AdmissionUser{Type: users.AdmissionUserCapsule, Username: owner.Name},
				newNs,
				oldNs.DeepCopy(),
				nil,
				recorder,
			)(context.Background(), admission.Request{})

			if response != nil {
				t.Fatalf("expected patch to be accepted and reverted, got %#v", response)
			}
			assertTenantAssignment(t, newNs, green)
		})
	}
}

func TestOwnerReferenceHandlerAllowsAdministratorAssignmentChanges(t *testing.T) {
	t.Parallel()

	green := testTenant("green", "green-uid")
	blue := testTenant("blue", "blue-uid")
	oldNs := testTenantNamespace("workloads", green)
	c := testClient(t, green, blue)

	newNs := testTenantNamespace("workloads", blue)
	response := (&ownerReferenceHandler{}).OnUpdate(
		c,
		c,
		users.AdmissionUser{Type: users.AdmissionUserAdmin},
		newNs,
		oldNs,
		nil,
		nil,
	)(context.Background(), admission.Request{})

	if response != nil {
		t.Fatalf("expected administrator migration to proceed, got %#v", response)
	}
	assertTenantAssignment(t, newNs, blue)
}

func TestOwnerReferenceHandlerRejectsNonOwnerJoin(t *testing.T) {
	t.Parallel()

	green := testTenant("green", "green-uid")
	c := testClient(t, green)
	recorder := capevents.NewEventRecorder(nil, logr.Discard(), nil, nil)

	response := (&ownerReferenceHandler{}).OnUpdate(
		c,
		c,
		users.AdmissionUser{Type: users.AdmissionUserCapsule, Username: "alice"},
		testTenantNamespace("workloads", green),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workloads"}},
		nil,
		recorder,
	)(context.Background(), admission.Request{})

	if response == nil || response.Allowed {
		t.Fatalf("expected unmanaged namespace join to be denied, got %#v", response)
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

func assertTenantAssignment(t *testing.T, ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) {
	t.Helper()

	if got := tenant.TenanLabelValue(ns); got != tnt.GetName() {
		t.Fatalf("tenant label = %q, want %q", got, tnt.GetName())
	}

	refs := tenant.TenantOwnerReferences(ns)
	if len(refs) != 1 || !tenant.IsTenantOwnerReferenceForTenant(refs[0], tnt) {
		t.Fatalf("tenant ownerReferences = %#v, want only tenant %q", refs, tnt.GetName())
	}
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

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestNamespaceTerminationHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "busy", Name: "pod-a"}},
	)

	pending, err := tenant.NamespaceIsPendingPodTerminating(ctx, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "busy"}})
	if err != nil {
		t.Fatalf("NamespaceIsPendingPodTerminating() unexpected error: %v", err)
	}
	if !pending {
		t.Fatalf("NamespaceIsPendingPodTerminating() = false, want true")
	}

	pending, err = tenant.NamespaceIsPendingPodTerminating(ctx, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "empty"}})
	if err != nil {
		t.Fatalf("NamespaceIsPendingPodTerminating(empty) unexpected error: %v", err)
	}
	if pending {
		t.Fatalf("NamespaceIsPendingPodTerminating(empty) = true, want false")
	}
}

func TestNamespaceIsPendingUnmanagedTerminationByStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnt := tenantObject("tenant-a", withUID(types.UID("tenant-uid")))
	tnt.Status.UpdateInstance(&capsulev1beta2.TenantStatusNamespaceItem{
		Name: "tenant-a-ns",
		Conditions: meta.ConditionList{{
			Type:   meta.TerminatingCondition,
			Status: metav1.ConditionTrue,
		}},
	})
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:            "tenant-a-ns",
		OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", types.UID("tenant-uid"))},
	}}
	cl := tenantFakeClient(t, tnt, ns)

	pending, err := tenant.NamespaceIsPendingUnmanagedTerminationByStatus(ctx, cl, ns)
	if err != nil {
		t.Fatalf("NamespaceIsPendingUnmanagedTerminationByStatus() unexpected error: %v", err)
	}
	if !pending {
		t.Fatalf("NamespaceIsPendingUnmanagedTerminationByStatus() = false, want true")
	}

	pending, err = tenant.NamespaceIsPendingUnmanagedTerminationByStatus(ctx, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unowned"}})
	if err != nil {
		t.Fatalf("NamespaceIsPendingUnmanagedTerminationByStatus(unowned) unexpected error: %v", err)
	}
	if pending {
		t.Fatalf("NamespaceIsPendingUnmanagedTerminationByStatus(unowned) = true, want false")
	}
}

func TestResolveNamespaceTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnt := tenantObject("tenant-a", withUID(types.UID("tenant-uid")))
	cl := tenantFakeClient(t, tnt)

	valid := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "tenant-a-ns",
		Labels: map[string]string{meta.TenantLabel: "tenant-a"},
		OwnerReferences: []metav1.OwnerReference{
			tenantOwnerReference("tenant-a", types.UID("tenant-uid")),
		},
	}}
	got, err := tenant.ResolveNamespaceTenant(ctx, cl, valid)
	if err != nil {
		t.Fatalf("ResolveNamespaceTenant() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("ResolveNamespaceTenant() = %#v, want tenant-a", got)
	}

	if got, err := tenant.ResolveNamespaceTenant(ctx, cl, nil); err != nil || got != nil {
		t.Fatalf("ResolveNamespaceTenant(nil) = %#v, %v, want nil nil", got, err)
	}

	tests := []struct {
		name string
		ns   *corev1.Namespace
		want string
	}{
		{
			name: "label without owner reference",
			ns: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{meta.TenantLabel: "tenant-a"},
			}},
			want: "no Tenant ownerReference",
		},
		{
			name: "owner reference without label",
			ns: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", types.UID("tenant-uid"))},
			}},
			want: "but no tenant label",
		},
		{
			name: "mismatched label",
			ns: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Labels:          map[string]string{meta.TenantLabel: "other"},
				OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", types.UID("tenant-uid"))},
			}},
			want: "does not match owner reference",
		},
	}

	for _, tt := range tests {
		_, err := tenant.ResolveNamespaceTenant(ctx, cl, tt.ns)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%s error = %v, want substring %q", tt.name, err, tt.want)
		}
	}
}

func TestCollectTenantNamespaceByLabel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{meta.TenantLabel: "tenant-a", "env": "prod"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{meta.TenantLabel: "tenant-a", "env": "dev"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "c", Labels: map[string]string{meta.TenantLabel: "tenant-b", "env": "prod"}}},
	)

	namespaces, err := tenant.CollectTenantNamespaceByLabel(ctx, cl, *tenantObject("tenant-a"), &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("CollectTenantNamespaceByLabel() unexpected error: %v", err)
	}
	if names := collectedNamespaceNames(namespaces); !reflect.DeepEqual(names, []string{"a"}) {
		t.Fatalf("CollectTenantNamespaceByLabel() = %#v, want namespace a", names)
	}
}

func TestNamespaceOwnershipAndContexts(t *testing.T) {
	t.Parallel()

	tnt := tenantObject("tenant-a", withUID(types.UID("tenant-uid")), withStatusOwner(rbac.UserOwner, "alice"))
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:            "tenant-a-ns",
		OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", types.UID("tenant-uid"))},
	}}

	if !tenant.NamespaceIsOwned(context.Background(), nil, nil, ns, tnt, users.AdmissionUser{Username: "alice"}) {
		t.Fatalf("NamespaceIsOwned() = false, want true for status owner")
	}
	if tenant.NamespaceIsOwned(context.Background(), nil, nil, ns, tnt, users.AdmissionUser{Username: "bob"}) {
		t.Fatalf("NamespaceIsOwned() = true, want false for non-owner")
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	ctx, err := tenant.NewTenantNamespaceContext(tnt, ns, scheme, sanitize.DefaultSanitizeOptions())
	if err != nil {
		t.Fatalf("NewTenantNamespaceContext() unexpected error: %v", err)
	}
	if ctx["tenant"] == nil || ctx["namespace"] == nil {
		t.Fatalf("NewTenantNamespaceContext() = %#v, want tenant and namespace contexts", ctx)
	}

	fast := tenant.FastContextForTenantAndNamespace(tnt, ns)
	if !reflect.DeepEqual(fast, map[string]string{"tenant.name": "tenant-a", "namespace": "tenant-a-ns"}) {
		t.Fatalf("FastContextForTenantAndNamespace() = %#v", fast)
	}
}

func TestTenantNamespaceContextIncludesStatusWhenRetained(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding Capsule scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		Status:     capsulev1beta2.TenantStatus{State: capsulev1beta2.TenantStateActive},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-ns"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}

	opts := sanitize.DefaultSanitizeOptions()
	opts.StripStatus = false

	ctx, err := tenant.NewTenantNamespaceContext(tnt, ns, scheme, opts)
	if err != nil {
		t.Fatalf("NewTenantNamespaceContext() unexpected error: %v", err)
	}

	tenantContext := ctx["tenant"].(map[string]any)
	tenantStatus := tenantContext["status"].(map[string]any)
	if tenantStatus["state"] != string(capsulev1beta2.TenantStateActive) {
		t.Fatalf("tenant status state = %#v, want Active", tenantStatus["state"])
	}

	namespaceContext := ctx["namespace"].(map[string]any)
	namespaceStatus := namespaceContext["status"].(map[string]any)
	if namespaceStatus["phase"] != string(corev1.NamespaceActive) {
		t.Fatalf("namespace status phase = %#v, want Active", namespaceStatus["phase"])
	}
}

func collectedNamespaceNames(namespaces []corev1.Namespace) []string {
	names := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		names = append(names, ns.Name)
	}

	return names
}

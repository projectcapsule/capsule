// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTenantByStatusNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t,
		tenantObject("tenant-a", withStatusNamespaces("ns-a")),
	)

	got, err := tenant.TenantByStatusNamespace(ctx, cl, "ns-a")
	if err != nil {
		t.Fatalf("TenantByStatusNamespace() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("TenantByStatusNamespace() = %#v, want tenant-a", got)
	}

	name, err := tenant.GetTenantNameByStatusNamespace(ctx, cl, "ns-a")
	if err != nil {
		t.Fatalf("GetTenantNameByStatusNamespace() unexpected error: %v", err)
	}
	if name != "tenant-a" {
		t.Fatalf("GetTenantNameByStatusNamespace() = %q, want tenant-a", name)
	}

	ok, err := tenant.IsNamespaceInTenant(ctx, cl, "ns-a")
	if err != nil {
		t.Fatalf("IsNamespaceInTenant() unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("IsNamespaceInTenant() = false, want true")
	}

	got, err = tenant.TenantByStatusNamespace(ctx, cl, "missing")
	if err != nil {
		t.Fatalf("TenantByStatusNamespace() unexpected error for missing namespace: %v", err)
	}
	if got != nil {
		t.Fatalf("TenantByStatusNamespace() = %#v, want nil", got)
	}
}

func TestGetTenantByNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnt := tenantObject("tenant-a", withUID("tenant-uid"))
	cl := tenantFakeClient(t,
		tnt,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:            "ns-a",
			OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", "tenant-uid")},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:            "ns-mismatch",
			OwnerReferences: []metav1.OwnerReference{tenantOwnerReference("tenant-a", "other-uid")},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-unowned"}},
	)

	name, err := tenant.GetTenantNameByNamespace(ctx, cl, "ns-a")
	if err != nil {
		t.Fatalf("GetTenantNameByNamespace() unexpected error: %v", err)
	}
	if name != "tenant-a" {
		t.Fatalf("GetTenantNameByNamespace() = %q, want tenant-a", name)
	}

	got, err := tenant.GetTenantByNamespace(ctx, cl, "ns-a")
	if err != nil {
		t.Fatalf("GetTenantByNamespace() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("GetTenantByNamespace() = %#v, want tenant-a", got)
	}

	got, err = tenant.GetTenantByNamespace(ctx, cl, "ns-unowned")
	if err != nil {
		t.Fatalf("GetTenantByNamespace() unexpected error for unowned namespace: %v", err)
	}
	if got != nil {
		t.Fatalf("GetTenantByNamespace() = %#v, want nil for unowned namespace", got)
	}

	got, err = tenant.GetTenantByNamespace(ctx, cl, "missing")
	if err != nil {
		t.Fatalf("GetTenantByNamespace() unexpected error for missing namespace: %v", err)
	}
	if got != nil {
		t.Fatalf("GetTenantByNamespace() = %#v, want nil for missing namespace", got)
	}

	if _, err = tenant.GetTenantByNamespace(ctx, cl, "ns-mismatch"); err == nil {
		t.Fatalf("GetTenantByNamespace() expected UID mismatch error")
	}
}

func TestGetTenantByOwnerreferences(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t, tenantObject("tenant-a"))
	refs := []metav1.OwnerReference{
		{APIVersion: "v1", Kind: "ConfigMap", Name: "ignored"},
		tenantOwnerReference("tenant-a", ""),
	}

	name, ok := tenant.GetTenantNameByOwnerreferences(refs)
	if !ok || name != "tenant-a" {
		t.Fatalf("GetTenantNameByOwnerreferences() = %q, %v, want tenant-a true", name, ok)
	}

	got, err := tenant.GetTenantByOwnerreferences(ctx, cl, refs)
	if err != nil {
		t.Fatalf("GetTenantByOwnerreferences() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("GetTenantByOwnerreferences() = %#v, want tenant-a", got)
	}

	got, err = tenant.GetTenantByOwnerreferences(ctx, cl, nil)
	if err != nil {
		t.Fatalf("GetTenantByOwnerreferences() unexpected error for nil refs: %v", err)
	}
	if got != nil {
		t.Fatalf("GetTenantByOwnerreferences() = %#v, want nil", got)
	}
}

func TestGetTenantByUserInfo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t,
		tenantObject("short", withSpecOwner(rbac.UserOwner, "alice")),
		tenantObject("very-long-name", withSpecOwner(rbac.GroupOwner, "developers")),
		tenantObject("service", withSpecOwner(rbac.ServiceAccountOwner, users.ServiceAccountUsername("tenant-a", "builder"))),
	)

	got, err := tenant.GetTenantByUserInfo(ctx, cl, nil, nil, users.AdmissionUser{
		Username: users.ServiceAccountUsername("tenant-a", "builder"),
		Groups:   []string{"developers"},
	})
	if err != nil {
		t.Fatalf("GetTenantByUserInfo() unexpected error: %v", err)
	}

	names := make([]string, 0, len(got))
	for _, tnt := range got {
		names = append(names, tnt.Name)
	}

	if !reflect.DeepEqual(names, []string{"very-long-name", "service"}) {
		t.Fatalf("GetTenantByUserInfo() names = %#v, want sorted matching tenants", names)
	}
}

func TestGetTenantByLabelsAndUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := tenantFakeClient(t,
		tenantObject("tenant-a", withStatusOwner(rbac.UserOwner, "alice")),
	)

	got, err := tenant.GetTenantByLabels(ctx, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{meta.TenantLabel: "tenant-a"},
	}})
	if err != nil {
		t.Fatalf("GetTenantByLabels() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("GetTenantByLabels() = %#v, want tenant-a", got)
	}

	got, err = tenant.GetTenantByLabelsAndUser(ctx, cl, nil, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{meta.TenantLabel: "tenant-a"},
	}}, users.NewAdmissionUser(users.AdmissionUserUnknown, authenticationv1.UserInfo{Username: "alice"}))
	if err != nil {
		t.Fatalf("GetTenantByLabelsAndUser() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("GetTenantByLabelsAndUser() = %#v, want tenant-a", got)
	}

	if _, err = tenant.GetTenantByLabelsAndUser(ctx, cl, nil, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{meta.TenantLabel: "tenant-a"},
	}}, users.NewAdmissionUser(users.AdmissionUserUnknown, authenticationv1.UserInfo{Username: "bob"})); err == nil {
		t.Fatalf("GetTenantByLabelsAndUser() expected non-owner error")
	}

	got, err = tenant.GetTenantByLabels(ctx, cl, &corev1.Namespace{})
	if err != nil {
		t.Fatalf("GetTenantByLabels() unexpected error without label: %v", err)
	}
	if got != nil {
		t.Fatalf("GetTenantByLabels() = %#v, want nil without label", got)
	}
}

func tenantFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&capsulev1beta2.Tenant{}, ".status.namespaces", func(obj client.Object) []string {
			return obj.(*capsulev1beta2.Tenant).Status.Namespaces
		}).
		WithIndex(&capsulev1beta2.Tenant{}, ".spec.owner.ownerkind", func(obj client.Object) []string {
			tnt := obj.(*capsulev1beta2.Tenant)
			values := make([]string, 0, len(tnt.Spec.Owners))
			for _, owner := range tnt.Spec.Owners {
				values = append(values, owner.Kind.String()+":"+owner.Name)
			}

			return values
		}).
		Build()
}

type tenantOption func(*capsulev1beta2.Tenant)

func tenantObject(name string, opts ...tenantOption) *capsulev1beta2.Tenant {
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name}}
	for _, opt := range opts {
		opt(tnt)
	}

	return tnt
}

func withUID(uid types.UID) tenantOption {
	return func(tnt *capsulev1beta2.Tenant) {
		tnt.UID = uid
	}
}

func withStatusNamespaces(namespaces ...string) tenantOption {
	return func(tnt *capsulev1beta2.Tenant) {
		tnt.Status.Namespaces = namespaces
	}
}

func withSpecOwner(kind rbac.OwnerKind, name string) tenantOption {
	return func(tnt *capsulev1beta2.Tenant) {
		tnt.Spec.Owners = append(tnt.Spec.Owners, rbac.OwnerSpec{
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{Kind: kind, Name: name},
			},
		})
	}
}

func withStatusOwner(kind rbac.OwnerKind, name string) tenantOption {
	return func(tnt *capsulev1beta2.Tenant) {
		tnt.Status.Owners = append(tnt.Status.Owners, rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{Kind: kind, Name: name},
		})
	}
}

func tenantOwnerReference(name string, uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: capsulev1beta2.GroupVersion.String(),
		Kind:       tenant.ObjectReferenceTenantKind,
		Name:       name,
		UID:        uid,
	}
}

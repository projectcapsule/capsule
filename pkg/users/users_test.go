// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users_test

import (
	"context"
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestAdmissionUser(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	info := users.ServiceAccountUserInfo("capsule-system", "capsule-controller")
	admissionUser := users.NewAdmissionUser(users.AdmissionUserAdmin, info)

	if !admissionUser.IsAdmin() {
		t.Fatalf("IsAdmin() = false, want true")
	}
	if admissionUser.IsCapsule() {
		t.Fatalf("IsCapsule() = true, want false")
	}
	if admissionUser.IsUnknown() {
		t.Fatalf("IsUnknown() = true, want false")
	}
	if !admissionUser.IsControllerServiceAccount() {
		t.Fatalf("IsControllerServiceAccount() = false, want true")
	}
	if got := admissionUser.UserInfo(); !reflect.DeepEqual(got, info) {
		t.Fatalf("UserInfo() = %#v, want %#v", got, info)
	}

	if got := users.ToServiceAccount("regular-user"); got != nil {
		t.Fatalf("ToServiceAccount() = %#v, want nil for regular user", got)
	}
	if got := users.ServiceAccountUsername("team-a", "builder"); got != "system:serviceaccount:team-a:builder" {
		t.Fatalf("ServiceAccountUsername() = %q, want service account username", got)
	}
}

func TestServiceAccountGroups(t *testing.T) {
	t.Parallel()

	want := []string{
		"system:serviceaccounts:team-a",
		"system:serviceaccounts",
		user.AllAuthenticated,
	}
	if got := users.ServiceAccountGroups("team-a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ServiceAccountGroups() = %#v, want %#v", got, want)
	}

	ref := meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Namespace: meta.RFC1123SubdomainName("team-a"),
		Name:      meta.RFC1123Name("builder"),
	}
	if got := users.GetServiceAccountFullName(ref); got != "system:serviceaccount:team-a:builder" {
		t.Fatalf("GetServiceAccountFullName() = %q", got)
	}
}

func TestHasIgnoredGroup(t *testing.T) {
	t.Parallel()

	if !users.HasIgnoredGroup([]string{"developers", "ignored"}, []string{"ignored"}) {
		t.Fatalf("HasIgnoredGroup() = false, want true")
	}
	if users.HasIgnoredGroup([]string{"developers"}, []string{"ignored"}) {
		t.Fatalf("HasIgnoredGroup() = true, want false")
	}
	if users.HasIgnoredGroup(nil, []string{"ignored"}) {
		t.Fatalf("HasIgnoredGroup() = true, want false for nil user groups")
	}
	if users.HasIgnoredGroup([]string{"developers"}, nil) {
		t.Fatalf("HasIgnoredGroup() = true, want false for nil ignored groups")
	}
}

func TestIsAdminUser(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	if !users.IsAdminUser(admission.Request{
		AdmissionRequest: admissionv1Request("system:serviceaccount:capsule-system:capsule-controller", nil),
	}, nil) {
		t.Fatalf("IsAdminUser() = false, want true for controller service account")
	}

	admins := rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "capsule-admins"}}
	if !users.IsAdminUser(admission.Request{
		AdmissionRequest: admissionv1Request("alice", []string{"capsule-admins"}),
	}, admins) {
		t.Fatalf("IsAdminUser() = false, want true for configured admin group")
	}

	if users.IsAdminUser(admission.Request{
		AdmissionRequest: admissionv1Request("alice", []string{"developers"}),
	}, admins) {
		t.Fatalf("IsAdminUser() = true, want false")
	}
}

func TestResolveServiceAccountActor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := usersFakeClient(t,
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "builder"}},
		&capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
			Status:     capsulev1beta2.TenantStatus{Namespaces: []string{"tenant-a"}},
		},
	)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")

	got, err := users.ResolveServiceAccountActor(
		ctx,
		cl,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target"}},
		users.ServiceAccountUsername("tenant-a", "builder"),
		cfg,
	)
	if err != nil {
		t.Fatalf("ResolveServiceAccountActor() unexpected error: %v", err)
	}
	if got == nil || got.Name != "tenant-a" {
		t.Fatalf("ResolveServiceAccountActor() = %#v, want tenant-a", got)
	}

	got, err = users.ResolveServiceAccountActor(
		ctx,
		cl,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   "target",
			Labels: map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue},
		}},
		users.ServiceAccountUsername("tenant-a", "builder"),
		cfg,
	)
	if err != nil {
		t.Fatalf("ResolveServiceAccountActor() unexpected error for promoted namespace: %v", err)
	}
	if got != nil {
		t.Fatalf("ResolveServiceAccountActor() = %#v, want nil when promotion label is present", got)
	}

	got, err = users.ResolveServiceAccountActor(
		ctx,
		cl,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target"}},
		users.ServiceAccountUsername("tenant-a", "missing"),
		cfg,
	)
	if err != nil {
		t.Fatalf("ResolveServiceAccountActor() unexpected error for missing service account: %v", err)
	}
	if got != nil {
		t.Fatalf("ResolveServiceAccountActor() = %#v, want nil for missing service account", got)
	}

	if _, err = users.ResolveServiceAccountActor(ctx, cl, nil, "regular-user", cfg); err == nil {
		t.Fatalf("ResolveServiceAccountActor() expected error for non service-account username")
	}
}

func TestIsTenantOwnerByStatus(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Owners: rbac.OwnerStatusListSpec{{
				UserSpec: rbac.UserSpec{
					Name: "alice",
					Kind: rbac.UserOwner,
				},
			}},
		},
	}

	if !users.IsTenantOwnerByStatus(tnt, users.AdmissionUser{Type: users.AdmissionUserAdmin, Username: "admin"}) {
		t.Fatalf("IsTenantOwnerByStatus() = false, want true for admin")
	}
	if !users.IsTenantOwnerByStatus(tnt, users.AdmissionUser{Type: users.AdmissionUserUnknown, Username: "alice"}) {
		t.Fatalf("IsTenantOwnerByStatus() = false, want true for status owner")
	}
	if users.IsTenantOwnerByStatus(tnt, users.AdmissionUser{Type: users.AdmissionUserUnknown, Username: "bob"}) {
		t.Fatalf("IsTenantOwnerByStatus() = true, want false")
	}
}

func usersFakeClient(t *testing.T, objects ...client.Object) client.Client {
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
		Build()
}

func admissionv1Request(username string, groups []string) admissionv1.AdmissionRequest {
	return admissionv1.AdmissionRequest{
		UserInfo: authenticationv1.UserInfo{
			Username: username,
			Groups:   groups,
		},
	}
}

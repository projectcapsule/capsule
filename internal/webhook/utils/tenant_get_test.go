// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestGetNamespaceTenantResolvesMatchingPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		tenants   []*capsulev1beta2.Tenant
		user      users.AdmissionUser
		want      string
	}{
		{
			name:      "issue 2058 group owner",
			namespace: "dummy-namespace",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("dummy", tenantGetTestOwner(rbac.GroupOwner, "dummy-team")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
				Groups:   []string{"dummy-team"},
			},
			want: "dummy",
		},
		{
			name:      "multiple available tenants",
			namespace: "dummy-namespace",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("dummy", tenantGetTestOwner(rbac.GroupOwner, "developer")),
				tenantGetTestTenant("another", tenantGetTestOwner(rbac.GroupOwner, "developer")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
				Groups:   []string{"developer"},
			},
			want: "dummy",
		},
		{
			name:      "same tenant matched by repeated group",
			namespace: "dummy-namespace",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("dummy", tenantGetTestOwner(rbac.GroupOwner, "dummy-team")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
				Groups:   []string{"dummy-team", "dummy-team"},
			},
			want: "dummy",
		},
		{
			name:      "closest overlapping prefix",
			namespace: "team-platform-namespace",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("team", tenantGetTestOwner(rbac.UserOwner, "alice")),
				tenantGetTestTenant("team-platform", tenantGetTestOwner(rbac.UserOwner, "alice")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			want: "team-platform",
		},
		{
			name:      "same tenant matched by user and group",
			namespace: "dummy-namespace",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant(
					"dummy",
					tenantGetTestOwner(rbac.UserOwner, "alice"),
					tenantGetTestOwner(rbac.GroupOwner, "dummy-team"),
				),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
				Groups:   []string{"dummy-team"},
			},
			want: "dummy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			cl, cfg := tenantGetTestClient(t, true, tt.tenants...)

			got, response := GetNamespaceTenant(
				ctx,
				cl,
				cl,
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tt.namespace}},
				tt.user,
				cfg,
				nil,
			)
			if response != nil {
				t.Fatalf("GetNamespaceTenant() unexpected response: %#v", response.Result)
			}
			if got == nil || got.Name != tt.want {
				t.Fatalf("GetNamespaceTenant() = %#v, want Tenant %q", got, tt.want)
			}
		})
	}
}

func TestGetNamespaceTenantResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		forcePrefix bool
		namespace   string
		tenants     []*capsulev1beta2.Tenant
		user        users.AdmissionUser
		wantTenant  string
		wantMessage string
	}{
		{
			name:        "single tenant requires configured prefix",
			forcePrefix: true,
			namespace:   "workloads",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("dummy", tenantGetTestOwner(rbac.UserOwner, "alice")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			wantMessage: "The Namespace name must start with 'dummy-' when ForceTenantPrefix is enabled in the Tenant.",
		},
		{
			name:        "multiple tenants require a matching prefix",
			forcePrefix: true,
			namespace:   "workloads",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant("dummy", tenantGetTestOwner(rbac.UserOwner, "alice")),
				tenantGetTestTenant("another", tenantGetTestOwner(rbac.UserOwner, "alice")),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			wantMessage: "The Namespace prefix used doesn't match any available Tenant",
		},
		{
			name:        "tenant override disables configured prefix",
			forcePrefix: true,
			namespace:   "workloads",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant(
					"dummy",
					tenantGetTestOwner(rbac.UserOwner, "alice"),
					tenantGetTestPrefixOverride(false),
				),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			wantTenant: "dummy",
		},
		{
			name:        "tenant override enables prefix",
			forcePrefix: false,
			namespace:   "workloads",
			tenants: []*capsulev1beta2.Tenant{
				tenantGetTestTenant(
					"dummy",
					tenantGetTestOwner(rbac.UserOwner, "alice"),
					tenantGetTestPrefixOverride(true),
				),
			},
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			wantMessage: "The Namespace name must start with 'dummy-' when ForceTenantPrefix is enabled in the Tenant.",
		},
		{
			name:        "capsule user without tenant is denied",
			forcePrefix: true,
			namespace:   "workloads",
			user: users.AdmissionUser{
				Type:     users.AdmissionUserCapsule,
				Username: "alice",
			},
			wantMessage: "You do not have any Tenant assigned: please, reach out to the system administrators",
		},
		{
			name:        "administrator without tenant is not assigned",
			forcePrefix: true,
			namespace:   "workloads",
			user: users.AdmissionUser{
				Type:     users.AdmissionUserAdmin,
				Username: "admin",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			cl, cfg := tenantGetTestClient(t, tt.forcePrefix, tt.tenants...)

			got, response := GetNamespaceTenant(
				ctx,
				cl,
				cl,
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tt.namespace}},
				tt.user,
				cfg,
				nil,
			)

			if tt.wantMessage != "" {
				if response == nil {
					t.Fatal("GetNamespaceTenant() response = nil, want denial")
				}
				if response.Result.Message != tt.wantMessage {
					t.Fatalf("GetNamespaceTenant() message = %q, want %q", response.Result.Message, tt.wantMessage)
				}
				if got != nil {
					t.Fatalf("GetNamespaceTenant() Tenant = %#v, want nil", got)
				}

				return
			}

			if response != nil {
				t.Fatalf("GetNamespaceTenant() unexpected response: %#v", response.Result)
			}
			if tt.wantTenant == "" {
				if got != nil {
					t.Fatalf("GetNamespaceTenant() Tenant = %#v, want nil", got)
				}

				return
			}
			if got == nil || got.Name != tt.wantTenant {
				t.Fatalf("GetNamespaceTenant() = %#v, want Tenant %q", got, tt.wantTenant)
			}
		})
	}
}

func TestResolveTenantByClosestNamespacePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		namespace     string
		tenantNames   []string
		want          string
		wantAmbiguous bool
	}{
		{
			name:        "no match",
			namespace:   "workloads",
			tenantNames: []string{"team", "platform"},
		},
		{
			name:        "prefix requires delimiter",
			namespace:   "teamworkloads",
			tenantNames: []string{"team"},
		},
		{
			name:        "one match",
			namespace:   "team-workloads",
			tenantNames: []string{"team", "platform"},
			want:        "team",
		},
		{
			name:        "closest match independent of order",
			namespace:   "team-platform-workloads",
			tenantNames: []string{"team", "team-platform"},
			want:        "team-platform",
		},
		{
			name:        "duplicate candidate is not ambiguous",
			namespace:   "team-workloads",
			tenantNames: []string{"team", "team"},
			want:        "team",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tenants := make([]capsulev1beta2.Tenant, 0, len(tt.tenantNames))
			for _, name := range tt.tenantNames {
				tenants = append(tenants, capsulev1beta2.Tenant{
					ObjectMeta: metav1.ObjectMeta{Name: name},
				})
			}

			got, ambiguous := resolveTenantByClosestNamespacePrefix(tt.namespace, tenants)
			if ambiguous != tt.wantAmbiguous {
				t.Fatalf("resolveTenantByClosestNamespacePrefix() ambiguous = %t, want %t", ambiguous, tt.wantAmbiguous)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("resolveTenantByClosestNamespacePrefix() = %#v, want nil", got)
				}

				return
			}
			if got == nil || got.Name != tt.want {
				t.Fatalf("resolveTenantByClosestNamespacePrefix() = %#v, want Tenant %q", got, tt.want)
			}
		})
	}
}

func TestValidateNamespacePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		forcePrefix bool
		override    *bool
		namespace   string
		want        bool
	}{
		{
			name:      "disabled",
			namespace: "workloads",
			want:      true,
		},
		{
			name:        "configured and matching",
			forcePrefix: true,
			namespace:   "team-workloads",
			want:        true,
		},
		{
			name:        "configured and not matching",
			forcePrefix: true,
			namespace:   "workloads",
			want:        false,
		},
		{
			name:        "tenant disables configuration",
			forcePrefix: true,
			override:    boolPointer(false),
			namespace:   "workloads",
			want:        true,
		},
		{
			name:      "tenant enables prefix",
			override:  boolPointer(true),
			namespace: "workloads",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, cfg := tenantGetTestClient(t, tt.forcePrefix)
			tenant := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "team"},
				Spec: capsulev1beta2.TenantSpec{
					ForceTenantPrefix: tt.override,
				},
			}

			got := validateNamespacePrefix(
				cfg,
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tt.namespace}},
				tenant,
			)
			if got != tt.want {
				t.Fatalf("validateNamespacePrefix() = %t, want %t", got, tt.want)
			}
		})
	}
}

func tenantGetTestClient(
	t *testing.T,
	forcePrefix bool,
	tenants ...*capsulev1beta2.Tenant,
) (client.Client, configuration.Configuration) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const configurationName = "capsule"

	objects := make([]client.Object, 0, len(tenants)+1)
	objects = append(objects, &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: configurationName},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			ForceTenantPrefix: forcePrefix,
		},
	})
	for _, tenant := range tenants {
		objects = append(objects, tenant)
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&capsulev1beta2.Tenant{}, ".spec.owner.ownerkind", func(obj client.Object) []string {
			return tenantGetTestOwnerKeys(obj.(*capsulev1beta2.Tenant).Status.Owners)
		}).
		Build()

	return cl, configuration.NewCapsuleConfiguration(
		context.Background(),
		cl,
		cl,
		nil,
		configurationName,
	)
}

func tenantGetTestTenant(name string, opts ...func(*capsulev1beta2.Tenant)) *capsulev1beta2.Tenant {
	tenant := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name}}
	for _, opt := range opts {
		opt(tenant)
	}

	return tenant
}

func tenantGetTestOwner(kind rbac.OwnerKind, name string) func(*capsulev1beta2.Tenant) {
	return func(tenant *capsulev1beta2.Tenant) {
		owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: kind, Name: name}}
		tenant.Status.Owners = append(tenant.Status.Owners, owner)
	}
}

func tenantGetTestPrefixOverride(value bool) func(*capsulev1beta2.Tenant) {
	return func(tenant *capsulev1beta2.Tenant) {
		tenant.Spec.ForceTenantPrefix = boolPointer(value)
	}
}

func tenantGetTestOwnerKeys(owners rbac.OwnerStatusListSpec) []string {
	keys := make([]string, 0, len(owners))
	for _, owner := range owners {
		keys = append(keys, owner.Kind.String()+":"+owner.Name)
	}

	return keys
}

func boolPointer(value bool) *bool {
	return &value
}

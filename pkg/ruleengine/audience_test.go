// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"slices"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestMatchesAudience(t *testing.T) {
	t.Parallel()

	cfg, cl := audienceConfiguration(t, nil)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UserInfo: authenticationv1.UserInfo{Username: "alice", Groups: []string{"developers"}}}}

	tests := []struct {
		name     string
		tnt      *capsulev1beta2.Tenant
		audience []rules.Audience
		want     bool
	}{
		{name: "user", audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}, want: true},
		{name: "group", audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: "developers"}}, want: true},
		{name: "no match", audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "bob"}}},
		{name: "tenant owner", tnt: &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"}}}}}}, audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := matchesAudience(t.Context(), cl, cfg, tt.tnt, req, tt.audience)
			if err != nil {
				t.Fatalf("matchesAudience() error = %v", err)
			}
			if matched != tt.want {
				t.Fatalf("matchesAudience() = %v, want %v", matched, tt.want)
			}
		})
	}
}

func TestFilterNamespaceRulesUsesRootAudience(t *testing.T) {
	t.Parallel()

	cfg, cl := audienceConfiguration(t, nil)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UserInfo: authenticationv1.UserInfo{Username: "alice", Groups: []string{"developers"}},
	}}
	matching := &rules.NamespaceRuleBodyNamespace{
		Audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: "developers"}},
		Enforce:  &rules.NamespaceRuleEnforceBody{},
	}
	nonMatching := &rules.NamespaceRuleBodyNamespace{
		Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "bob"}},
		Enforce:  &rules.NamespaceRuleEnforceBody{},
	}
	unscoped := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{}}

	got, err := FilterNamespaceRulesByAudience(t.Context(), cl, cfg, nil, req, []*rules.NamespaceRuleBodyNamespace{matching, nonMatching, unscoped})
	if err != nil {
		t.Fatalf("FilterNamespaceRulesByAudience() error = %v", err)
	}
	if len(got) != 2 || got[0] != matching || got[1] != unscoped {
		t.Fatalf("unexpected filtered rules: %#v", got)
	}
}

func TestCustomAudiencesIncludeTenantServiceAccounts(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	promoted := users.ServiceAccountUserInfo("team-a", "promoted")
	config := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			Users: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.GroupOwner, Name: "developers"},
				{Kind: rbac.ServiceAccountOwner, Name: users.ServiceAccountUsername("external", "configured")},
			},
			Administrators:       rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "admin"}},
			IgnoreUserWithGroups: []string{"ignored"},
		},
	}
	config.Status.Users = append(append(rbac.UserListSpec{}, config.Spec.Users...),
		rbac.UserSpec{Kind: rbac.UserOwner, Name: "aggregated-user"},
		rbac.UserSpec{Kind: rbac.GroupOwner, Name: "aggregated-group"},
	)
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"team-a", "capsule-system", "kube-system"},
			Owners: rbac.OwnerStatusListSpec{
				{UserSpec: rbac.UserSpec{Kind: rbac.ServiceAccountOwner, Name: promoted.Username}},
				{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "admin"}},
			},
		},
	}
	otherTenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"},
		Status:     capsulev1beta2.TenantStatus{Namespaces: []string{"team-b"}},
	}
	cfg, cl := audienceConfiguration(t, config, tnt, otherTenant)
	capsuleRule := &rules.NamespaceRuleBodyNamespace{
		Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceCapsuleUser)}},
		Enforce:  &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny},
	}
	ownerRule := &rules.NamespaceRuleBodyNamespace{
		Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}},
		Enforce:  &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny},
	}
	unscoped := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}}

	tests := []struct {
		name        string
		user        authenticationv1.UserInfo
		capsuleUser bool
		tenantOwner bool
	}{
		{name: "configured user", user: authenticationv1.UserInfo{Username: "alice"}, capsuleUser: true},
		{name: "configured group", user: authenticationv1.UserInfo{Username: "bob", Groups: []string{"developers"}}, capsuleUser: true},
		{name: "aggregated user", user: authenticationv1.UserInfo{Username: "aggregated-user"}, capsuleUser: true},
		{name: "aggregated group", user: authenticationv1.UserInfo{Username: "bob", Groups: []string{"aggregated-group"}}, capsuleUser: true},
		{name: "promoted service account", user: promoted, capsuleUser: true, tenantOwner: true},
		{name: "unpromoted service account", user: users.ServiceAccountUserInfo("team-a", "unpromoted"), capsuleUser: true},
		{name: "service account from another tenant", user: users.ServiceAccountUserInfo("team-b", "builder"), capsuleUser: true},
		{name: "configured external service account", user: users.ServiceAccountUserInfo("external", "configured"), capsuleUser: true},
		{name: "unrelated service account", user: users.ServiceAccountUserInfo("external", "unrelated")},
		{name: "unrelated user", user: authenticationv1.UserInfo{Username: "unrelated"}},
		{name: "administrator in status owners", user: authenticationv1.UserInfo{Username: "admin"}, tenantOwner: true},
		{name: "ignored group", user: authenticationv1.UserInfo{Username: "bob", Groups: []string{"developers", "ignored"}}},
		{name: "kube-system service account", user: users.ServiceAccountUserInfo("kube-system", "system-controller")},
		{name: "capsule controller", user: users.ServiceAccountUserInfo("capsule-system", "capsule-controller")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UserInfo: tt.user}}
			got, err := FilterNamespaceRulesByAudience(t.Context(), cl, cfg, tnt, req, []*rules.NamespaceRuleBodyNamespace{capsuleRule, ownerRule, unscoped})
			if err != nil {
				t.Fatalf("FilterNamespaceRulesByAudience() error = %v", err)
			}
			var want []*rules.NamespaceRuleBodyNamespace
			if tt.capsuleUser {
				want = append(want, capsuleRule)
			}
			if tt.tenantOwner {
				want = append(want, ownerRule)
			}
			want = append(want, unscoped)
			if !slices.Equal(got, want) {
				t.Fatalf("filtered rules = %v, want %v", got, want)
			}
		})
	}
}

func audienceConfiguration(t *testing.T, config *capsulev1beta2.CapsuleConfiguration, objects ...client.Object) (configuration.Configuration, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	if config == nil {
		config = &capsulev1beta2.CapsuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
			Spec:       configuration.DefaultCapsuleConfiguration(),
		}
		config.Status.Users = config.Spec.Users
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(append(objects, config)...).
		WithIndex(&capsulev1beta2.Tenant{}, ".status.namespaces", func(obj client.Object) []string {
			return obj.(*capsulev1beta2.Tenant).Status.Namespaces
		}).Build()

	return configuration.NewCapsuleConfiguration(t.Context(), cl, cl, &rest.Config{}, config.Name), cl
}

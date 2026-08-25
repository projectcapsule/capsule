// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestMatchesAudience(t *testing.T) {
	t.Parallel()

	cfg := audienceConfiguration(t)
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
			matched, err := matchesAudience(cfg, tt.tnt, req, tt.audience)
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

	cfg := audienceConfiguration(t)
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

	got, err := FilterNamespaceRulesByAudience(cfg, nil, req, []*rules.NamespaceRuleBodyNamespace{matching, nonMatching, unscoped})
	if err != nil {
		t.Fatalf("FilterNamespaceRulesByAudience() error = %v", err)
	}
	if len(got) != 2 || got[0] != matching || got[1] != unscoped {
		t.Fatalf("unexpected filtered rules: %#v", got)
	}
}

func audienceConfiguration(t *testing.T) configuration.Configuration {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	config := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec:       configuration.DefaultCapsuleConfiguration(),
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config).Build()

	return configuration.NewCapsuleConfiguration(t.Context(), cl, cl, &rest.Config{}, config.Name)
}

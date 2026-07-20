// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestMatchesAudience(t *testing.T) {
	t.Parallel()
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
			matched, err := matchesAudience(nil, tt.tnt, req, tt.audience)
			if err != nil {
				t.Fatalf("matchesAudience() error = %v", err)
			}
			if matched != tt.want {
				t.Fatalf("matchesAudience() = %v, want %v", matched, tt.want)
			}
		})
	}
}

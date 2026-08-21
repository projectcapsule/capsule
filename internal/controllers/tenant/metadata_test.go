// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestEnsureMetadataKeepsFinalizerForRuleGlobalResourceQuotas(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{Name: "compute"}},
			},
		}}},
	}
	manager := &Manager{}

	if err := manager.ensureMetadata(context.Background(), tnt); err != nil {
		t.Fatalf("ensureMetadata() error = %v", err)
	}
	if !controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) {
		t.Fatal("Tenant with rule-generated GlobalResourceQuota is missing the controller finalizer")
	}

	now := metav1.Now()
	tnt.DeletionTimestamp = &now
	if err := manager.ensureMetadata(context.Background(), tnt); err != nil {
		t.Fatalf("ensureMetadata() while deleting error = %v", err)
	}
	if controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) {
		t.Fatal("Tenant controller finalizer was retained after managed child cleanup")
	}
}

func TestHasRuleGlobalResourceQuotas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules []*rules.NamespaceRuleBodyTenant
		want  bool
	}{
		{name: "no rules"},
		{name: "nil rule", rules: []*rules.NamespaceRuleBodyTenant{nil}},
		{name: "rule without namespace body", rules: []*rules.NamespaceRuleBodyTenant{{}}},
		{
			name: "rule without quota",
			rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{},
			}},
		},
		{
			name: "rule with quota",
			rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Quota: []rules.ResourceQuotaRule{{Name: "compute"}},
				},
			}},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: test.rules}}
			if got := hasRuleGlobalResourceQuotas(tnt); got != test.want {
				t.Fatalf("hasRuleGlobalResourceQuotas() = %t, want %t", got, test.want)
			}
		})
	}
}

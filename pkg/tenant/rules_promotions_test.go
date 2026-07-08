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
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGetManagedRuleStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rs := &capsulev1beta2.RuleStatus{
		ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: "tenant-a"},
		Status: capsulev1beta2.RuleStatusStatus{
			Rules: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAudit}}},
		},
	}
	cl := tenantFakeClient(t, rs)

	got, err := tenant.GetManagedRuleStatus(ctx, cl, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}})
	if err != nil {
		t.Fatalf("GetManagedRuleStatus() unexpected error: %v", err)
	}
	if got.Name != meta.NameForManagedRuleStatus() || len(got.Status.Rules) != 1 {
		t.Fatalf("GetManagedRuleStatus() = %#v", got)
	}
}

func TestBuildNamespaceRuleBodyStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	tnt := tenantObject("tenant-a")
	matching := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: rules.ActionTypeAudit,
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"prod-scheduler"}}},
		},
	}}
	unmatched := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}}
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
		{
			NamespaceRuleBodyNamespace: matching,
			NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
		},
		{
			NamespaceRuleBodyNamespace: unmatched,
			NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"env": "dev"}},
		},
		nil,
		{},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-prod", Labels: map[string]string{"env": "prod"}}}

	got, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
	if err != nil {
		t.Fatalf("BuildNamespaceRuleBodyStatus() unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Enforce.Action != rules.ActionTypeAudit {
		t.Fatalf("BuildNamespaceRuleBodyStatus() = %#v, want one audit rule", got)
	}
	if got[0].Enforce.Workloads.Schedulers[0].Exact[0] != "prod-scheduler" {
		t.Fatalf("BuildNamespaceRuleBodyStatus() scheduler = %#v", got[0].Enforce.Workloads.Schedulers)
	}

	got[0].Enforce.Action = rules.ActionTypeDeny
	if matching.Enforce.Action != rules.ActionTypeAudit {
		t.Fatalf("BuildNamespaceRuleBodyStatus() returned shared rule body")
	}

	got, err = tenant.BuildNamespaceRuleBodyStatus(scheme, nil, tnt)
	if err != nil || got != nil {
		t.Fatalf("BuildNamespaceRuleBodyStatus(nil namespace) = %#v, %v, want nil nil", got, err)
	}
}

func TestCollectPromotions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnt := tenantObject("tenant-a", withStatusNamespaces("ns-a", "ns-b"))
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
		Permissions: rules.NamespaceRulePermissionBody{
			Promotions: []*rules.NamespaceRulePromotionRule{{
				ClusterRoles: []string{"admin"},
				Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}},
			}},
		},
	}}

	cl := tenantFakeClient(t,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-a", Labels: map[string]string{
			corev1.LabelMetadataName: "ns-a",
			"env":                    "prod",
		}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b", Labels: map[string]string{
			corev1.LabelMetadataName: "ns-b",
			"env":                    "dev",
		}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "builder", Labels: map[string]string{
			meta.ServiceAccountPromotionLabel: meta.ValueTrue,
			"team":                            "platform",
		}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "ignored", Labels: map[string]string{
			meta.ServiceAccountPromotionLabel: meta.ValueTrue,
			"team":                            "other",
		}}},
	)

	got, err := tenant.CollectPromotions(ctx, cl, tnt, nil)
	if err != nil {
		t.Fatalf("CollectPromotions() unexpected error: %v", err)
	}
	want := rbac.PromotionStatusListSpec{{
		UserSpec: rbac.UserSpec{
			Kind: rbac.ServiceAccountOwner,
			Name: users.ServiceAccountUsername("ns-a", "builder"),
		},
		ClusterRoles: []string{"admin"},
		Targets:      []string{"ns-a"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectPromotions() = %#v, want %#v", got, want)
	}

	emptyTenant := tenantObject("empty")
	got, err = tenant.CollectPromotions(ctx, cl, emptyTenant, nil)
	if err != nil {
		t.Fatalf("CollectPromotions(empty) unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("CollectPromotions(empty) = %#v, want nil", got)
	}
}

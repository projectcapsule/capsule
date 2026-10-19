// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestNamespaceRoleBindingPlanPreservesProfilesAndOrdering(t *testing.T) {
	binding := func(role, source string) rbac.AdditionalRoleBindingsSpec {
		return rbac.AdditionalRoleBindingsSpec{
			ClusterRoleName: role,
			Subjects:        []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "reader"}},
			Labels:          map[string]string{"source": source},
		}
	}
	selected := &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}}
	tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{
		AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{binding("view", "legacy")},
		Rules: []*rules.NamespaceRuleBodyTenant{
			nil,
			{NamespaceSelector: selected, Permissions: rules.NamespaceRulePermissionBody{
				Bindings: []rbac.AdditionalRoleBindingsSpec{binding("view", "selected"), binding("edit", "selected")},
			}},
			{Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{binding("view", "last")}}},
		},
	}, Status: capsulev1beta2.TenantStatus{
		Promotions: rbac.PromotionStatusListSpec{{Kind: rbac.UserOwner, Name: "promoted", ClusterRoles: []string{"admin"}, Targets: []string{"selected"}}},
	}}
	before := tnt.DeepCopy()
	plan := prepareNamespaceRoleBindings(tnt)
	for _, tc := range []struct {
		name, profile string
		count         int
	}{
		{"selected", "reader", 3},
		{"another-selected", "reader", 2},
		{"plain", "plain", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ns := &corev1.Namespace{Name: tc.name, Labels: map[string]string{"profile": tc.profile}}
			bindings, err := plan.forNamespace(ns)
			if err != nil {
				t.Fatal(err)
			}
			if len(bindings) != tc.count {
				t.Fatalf("got %d bindings, want %d: %+v", len(bindings), tc.count, bindings)
			}
			key := utils.RoleBindingHashFunc(binding("view", "ignored"))
			if got := bindings[key].Labels["source"]; got != "last" {
				t.Fatalf("later unselected rule lost precedence: got %q", got)
			}
			delete(bindings, key)
			again, err := plan.forNamespace(ns)
			if err != nil || len(again) != tc.count {
				t.Fatalf("namespace binding maps share mutations: %v, %+v", err, again)
			}
		})
	}
	if !reflect.DeepEqual(tnt, before) {
		t.Fatal("preparing or selecting bindings mutated the Tenant")
	}
	// A later selected rule must also override a preceding unselected rule.
	tnt.Spec.Rules[1], tnt.Spec.Rules[2] = tnt.Spec.Rules[2], tnt.Spec.Rules[1]
	bindings, err := prepareNamespaceRoleBindings(tnt).forNamespace(&corev1.Namespace{Name: "selected", Labels: map[string]string{"profile": "reader"}})
	if err != nil || bindings[utils.RoleBindingHashFunc(binding("view", "ignored"))].Labels["source"] != "selected" {
		t.Fatalf("selected rule lost precedence: %v, %+v", err, bindings)
	}
}

func TestNamespaceRoleBindingPlanEmptyAndInvalidSelectors(t *testing.T) {
	ns := &corev1.Namespace{Name: "empty"}
	bindings, err := prepareNamespaceRoleBindings(&capsulev1beta2.Tenant{}).forNamespace(ns)
	if err != nil || len(bindings) != 0 {
		t.Fatalf("empty Tenant produced bindings: %v, %+v", err, bindings)
	}
	tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{nil, {
		NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "profile", Operator: "invalid"}}},
		Permissions:       rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view"}}},
	}}}}
	bindings, err = prepareNamespaceRoleBindings(tnt).forNamespace(ns)
	if err == nil || !strings.Contains(err.Error(), "rules[1]") || bindings != nil {
		t.Fatalf("invalid selector must fail without granting permissions: %v, %+v", err, bindings)
	}
}

func TestSyncAdditionalRoleBindingDoesNotMutateSpecMetadata(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	labels := map[string]string{"example.com/label": "value"}
	annotations := map[string]string{"example.com/annotation": "value"}
	originalLabels := maps.Clone(labels)
	originalAnnotations := maps.Clone(annotations)
	binding := rbac.AdditionalRoleBindingsSpec{
		ClusterRoleName: "custom:pod-viewer",
		Subjects:        []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "alice"}},
		Labels:          labels,
		Annotations:     annotations,
	}

	manager := &Manager{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
	tenant := &capsulev1beta2.Tenant{Name: "green"}

	if err := manager.syncAdditionalRoleBinding(
		context.Background(),
		logr.Discard(),
		tenant,
		"green-app",
		map[string]rbac.AdditionalRoleBindingsSpec{"hash": binding},
		true,
	); err != nil {
		t.Fatal(err)
	}

	if !maps.Equal(labels, originalLabels) {
		t.Fatalf("binding labels were mutated: got %v, want %v", labels, originalLabels)
	}

	if !maps.Equal(annotations, originalAnnotations) {
		t.Fatalf("binding annotations were mutated: got %v, want %v", annotations, originalAnnotations)
	}
}

func TestRoleBindingClientResponsesCannotMutateSharedSubjects(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid"}
	r, _ := namespaceCleanupFixture(t, tnt)
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if err := c.Create(ctx, obj, opts...); err != nil {
			return err
		}
		if binding, ok := obj.(*rbacv1.RoleBinding); ok {
			binding.Subjects[0].Name = "API-response-subject"
		}
		return nil
	}})
	desired := rbac.AdditionalRoleBindingsSpec{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "alice", APIGroup: rbacv1.GroupName}}}
	bindings := map[string]rbac.AdditionalRoleBindingsSpec{"hash": desired}
	for _, namespace := range []string{"namespace-a", "namespace-b"} {
		if err := r.syncAdditionalRoleBinding(t.Context(), logr.Discard(), tnt, namespace, bindings, true); err != nil {
			t.Fatal(err)
		}
		if desired.Subjects[0].Name != "alice" || bindings["hash"].Subjects[0].Name != "alice" {
			t.Fatal("client response mutated subjects used by another namespace")
		}
	}
}

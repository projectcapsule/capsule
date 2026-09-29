// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestConditionCacheRebuildRetainsSharedProfilesAndQuotaExpressions(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	expressions := []string{`request.operation == 'CREATE'`, `has(object.spec.containers)`, `object.spec.type == 'ClusterIP'`}
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Conditions: []rules.AdmissionCondition{{Expression: expressions[0]}}, Workloads: rules.WorkloadMutation{}}}, Enforce: &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: expressions[1]}, {Expression: expressions[2]}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{}, Services: rules.NamespaceRuleEnforceServicesBody{}}}
	a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: body}}}}
	b := a.DeepCopy()
	b.Name = "tenant-b"
	status := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: "profile", Namespace: "ns-a"}, Spec: []*rules.NamespaceRuleBodyNamespace{body.DeepCopy()}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b, status).Build()
	r := &CacheInvalidator{Client: cl, CELCache: c}
	if _, err = c.GetOrCompileCondition(`false`, environment.StoredExpressions); err != nil {
		t.Fatal(err)
	}
	quota, err := c.GetOrCompileBoolean(`true`, environment.StoredExpressions)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []any{nil, a, b, status} {
		switch o := object.(type) {
		case *capsulev1beta2.Tenant:
			if err := cl.Delete(context.Background(), o); err != nil {
				t.Fatal(err)
			}
		case *capsulev1beta2.RuleStatus:
			if err := cl.Delete(context.Background(), o); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.rebuildConditionCache(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := 7
		if object == status {
			want = 1
		}
		if c.Stats() != want {
			t.Fatalf("cache entries=%d want=%d after %T", c.Stats(), want, object)
		}
		again, err := c.GetOrCompileBoolean(`true`, environment.StoredExpressions)
		if err != nil || again != quota {
			t.Fatal("condition invalidation altered quota cache")
		}
	}
}

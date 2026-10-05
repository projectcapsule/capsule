// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestPlacementRegexCacheRebuild(t *testing.T) {
	t.Parallel()
	var expressions []runtime.ExpressionRegex
	expression := func() *rules.PlacementExpressionMatch {
		expr := runtime.ExpressionRegex{Expression: fmt.Sprintf("^placement-%d$", len(expressions))}
		expressions = append(expressions, expr)
		return &rules.PlacementExpressionMatch{ExpressionRegex: expr}
	}
	pair := func() rules.WorkloadNodeSelectorMatch {
		return rules.WorkloadNodeSelectorMatch{Key: expression(), Values: expression()}
	}
	requirement := func() []rules.PlacementRequirementMatch {
		return []rules.PlacementRequirementMatch{{WorkloadNodeSelectorMatch: pair()}}
	}
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{NodeSelector: []rules.WorkloadNodeSelectorMatch{pair()},
		Tolerations:               []rules.WorkloadTolerationMatch{{WorkloadNodeSelectorMatch: pair()}},
		TopologySpreadConstraints: []rules.WorkloadTopologySpreadMatch{{TopologyKey: expression(), LabelSelector: &rules.PlacementLabelSelectorMatch{Requirements: requirement()}}},
		Affinity: []rules.WorkloadAffinityMatch{{
			TopologyKey: expression(), Namespaces: expression(), Requirements: requirement(), FieldRequirements: requirement(),
			LabelSelector: &rules.PlacementLabelSelectorMatch{Requirements: requirement()}, NamespaceSelector: &rules.PlacementLabelSelectorMatch{Requirements: requirement()},
		}}},
	}}}
	scheme := k8sruntime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	status := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: "rules", Namespace: "tenant-a"}, Spec: []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: []rules.NamespaceRuleMutation{{}}}, body}}
	status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body.DeepCopy()}
	other := status.DeepCopy()
	other.Namespace = "tenant-b"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(status, other).Build()
	r := &CacheInvalidator{Client: c, RegexCache: cache.NewRegexCache()}
	stale := runtime.ExpressionRegex{Expression: "^removed$"}
	if _, _, err := r.RegexCache.GetOrCompile(stale); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if err := r.rebuildRegexCache(context.Background(), logr.Discard()); err != nil {
			t.Fatal(err)
		}
		if got := r.RegexCache.Stats(); got != len(expressions) {
			t.Fatalf("cache contains %d entries, want %d distinct placement expressions", got, len(expressions))
		}
		if r.RegexCache.Has(cache.HashRegex(stale)) {
			t.Fatal("stale expression survived rebuild")
		}
		for _, expr := range expressions {
			if _, hit, err := r.RegexCache.GetOrCompile(expr); err != nil || !hit {
				t.Fatalf("placement expression %s was not warmed: hit=%t err=%v", expr.Expression, hit, err)
			}
		}
	}
	if err := c.Delete(context.Background(), status); err != nil {
		t.Fatal(err)
	}
	if err := r.rebuildRegexCache(context.Background(), logr.Discard()); err != nil {
		t.Fatal(err)
	}
	if r.RegexCache.Stats() != len(expressions) {
		t.Fatal("deleting tenant A retired expressions still used by tenant B")
	}
	if err := c.Delete(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if err := r.rebuildRegexCache(context.Background(), logr.Discard()); err != nil {
		t.Fatal(err)
	}
	if r.RegexCache.Stats() != 0 {
		t.Fatal("unused placement expressions survived deletion")
	}
}

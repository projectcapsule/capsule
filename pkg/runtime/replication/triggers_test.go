// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func BenchmarkPreparedWatchMatching(b *testing.B) {
	for _, size := range []struct {
		name                        string
		triggers, conditions, bytes int
	}{{"small", 1, 0, 1024}, {"typical", 8, 4, 4096}, {"large", 32, 8, 16384}} {
		for _, parallel := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/parallel=%t", size.name, parallel), func(b *testing.B) {
				compiler, err := cache.NewCELCache()
				if err != nil {
					b.Fatal(err)
				}
				spec := &capsulev1beta2.TenantResourceCommonSpec{}
				for i := range size.triggers {
					trigger := watchTrigger()
					trigger.Name = fmt.Sprintf("source-%d", i)
					trigger.ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"source": trigger.Name}}
					for condition := range size.conditions {
						trigger.MatchConditions = append(trigger.MatchConditions, capsulev1beta2.TenantResourceTriggerCondition{Name: fmt.Sprintf("changed-%d", condition), Expression: "operation == 'UPDATE' && object.data != oldObject.data"})
					}
					spec.Triggers = append(spec.Triggers, trigger)
				}
				prepared, err := replication.PrepareTriggers(spec, true, cache.NewLabelSelectorCache(), compiler)
				if err != nil {
					b.Fatal(err)
				}
				object := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"labels": map[string]any{"source": fmt.Sprintf("source-%d", size.triggers-1)}}, "data": map[string]any{"value": strings.Repeat("x", size.bytes)}}}
				oldObject := object.DeepCopy()
				oldObject.Object["data"] = map[string]any{"value": strings.Repeat("y", size.bytes)}
				key := replication.ResourceKey(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, admissionv1.Update)
				match := func() {
					matched, err := prepared.Matches(b.Context(), key, admissionv1.Update, nil, oldObject, object)
					if err != nil || !matched {
						b.Errorf("matching failed: %v", err)
					}
				}
				b.ReportAllocs()
				if parallel {
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							match()
						}
					})
					return
				}
				for b.Loop() {
					match()
				}
			})
		}
	}
}

func watchTrigger() capsulev1beta2.TenantResourceTrigger {
	return capsulev1beta2.TenantResourceTrigger{Name: "changes", Operations: []admissionv1.OperationType{admissionv1.OperationAll}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
}

func TestPreparedMatchingSkipsOnlyNamespaceRestrictedAlternatives(t *testing.T) {
	compiler, _ := cache.NewCELCache()
	restricted, unrestricted := watchTrigger(), watchTrigger()
	restricted.Name = "restricted"
	restricted.NamespaceSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "blocked", Operator: metav1.LabelSelectorOpDoesNotExist}}}
	unrestricted.Name, unrestricted.Resources = "unrestricted", []string{"secrets"}
	prepared, err := replication.PrepareTriggers(&capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{restricted, unrestricted}}, false, cache.NewLabelSelectorCache(), compiler)
	if err != nil {
		t.Fatal(err)
	}
	object := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"labels": map[string]any{}}}}
	for _, available := range []bool{false, true} {
		for _, resource := range []string{"configmaps", "secrets"} {
			key := replication.ResourceKey(schema.GroupVersionResource{Version: "v1", Resource: resource}, admissionv1.Create)
			got, err := prepared.MatchesWithNamespace(t.Context(), key, admissionv1.Create, nil, available, nil, object)
			want := resource == "secrets" || available
			if err != nil || got != want {
				t.Fatalf("resource=%s available=%t matched=%t err=%v", resource, available, got, err)
			}
		}
	}
}

func TestWatchTriggerValidation(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*capsulev1beta2.TenantResourceTrigger)
		want string
	}{
		{"valid", func(*capsulev1beta2.TenantResourceTrigger) {}, ""},
		{"namespaced-scope", func(s *capsulev1beta2.TenantResourceTrigger) { s.Scope = new(admissionv1.NamespacedScope) }, ""},
		{"cluster-scope", func(s *capsulev1beta2.TenantResourceTrigger) { s.Scope = new(admissionv1.ClusterScope) }, "Namespaced"},
		{"all-scopes", func(s *capsulev1beta2.TenantResourceTrigger) { s.Scope = new(admissionv1.AllScopes) }, "Namespaced"},
		{"all-observed-operations", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.Operations = []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete}
		}, ""},
		{"wildcard-resource", func(s *capsulev1beta2.TenantResourceTrigger) { s.Resources = []string{"*"} }, "top-level"},
		{"subresource", func(s *capsulev1beta2.TenantResourceTrigger) { s.Resources = []string{"deployments/scale"} }, "top-level"},
		{"wildcard-group", func(s *capsulev1beta2.TenantResourceTrigger) { s.APIGroups = []string{"*"} }, "explicit"},
		{"wildcard-version", func(s *capsulev1beta2.TenantResourceTrigger) { s.APIVersions = []string{"*"} }, "explicit"},
		{"empty-version", func(s *capsulev1beta2.TenantResourceTrigger) { s.APIVersions = []string{""} }, "explicit"},
		{"too-many-operations", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.Operations = []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete, admissionv1.Create}
		}, "at most three"},
		{"connect", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.Operations = []admissionv1.OperationType{admissionv1.Connect}
		}, "unsupported"},
		{"audience", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
		}, "request identities"},
		{"request-cel", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "request", Expression: "request.operation == 'CREATE'"}}
		}, "undeclared"},
		{"authorizer-cel", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "auth", Expression: "authorizer.group('apps').resource('deployments').check('create').allowed()"}}
		}, "undeclared"},
		{"state-cel", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "state", Expression: "operation != 'UPDATE' || object.data != oldObject.data"}}
		}, ""},
		{"invalid-cel", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "bad", Expression: "object["}}
		}, "compile"},
		{"non-boolean", func(s *capsulev1beta2.TenantResourceTrigger) {
			s.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "bad", Expression: "object.metadata.name"}}
		}, "bool"},
		{"namespace-selector-local", func(s *capsulev1beta2.TenantResourceTrigger) { s.NamespaceSelector = &metav1.LabelSelector{} }, "only supported"},
		{"empty-operations", func(s *capsulev1beta2.TenantResourceTrigger) { s.Operations = nil }, "requires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := watchTrigger()
			tc.edit(&s)
			err := replication.ValidateTriggers(&capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{s}}, true, compiler)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
	if err := replication.ValidateTriggers(nil, true, compiler); err == nil {
		t.Fatal("nil spec accepted")
	}
}

func TestPreparedWatchConditionsAndSelectors(t *testing.T) {
	compiler, _ := cache.NewCELCache()
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}
	trigger := watchTrigger()
	trigger.NamespaceSelector = selector
	trigger.ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"source": "yes"}}
	trigger.MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "state", Expression: "operation != 'UPDATE' || object.data != oldObject.data"}}
	spec := &capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}
	prepared, err := replication.PrepareTriggers(spec, false, cache.NewLabelSelectorCache(), compiler)
	if err != nil {
		t.Fatal(err)
	}
	// Input changes and returned slices cannot mutate a prepared subscription.
	selector.MatchLabels["profile"] = "other"
	resources := prepared.Resources()
	resources[0].Resource = "secrets"
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	object := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"labels": map[string]any{"source": "yes"}}, "data": map[string]any{"value": "old"}}}
	changed := object.DeepCopy()
	changed.Object["data"] = map[string]any{"value": "new"}
	for _, tc := range []struct {
		name         string
		operation    admissionv1.OperationType
		old, current *unstructured.Unstructured
		ns           map[string]string
		want         bool
	}{
		{"create", admissionv1.Create, nil, object, map[string]string{"profile": "selected"}, true},
		{"update-same", admissionv1.Update, object, object, map[string]string{"profile": "selected"}, false},
		{"update-changed", admissionv1.Update, object, changed, map[string]string{"profile": "selected"}, true},
		{"delete", admissionv1.Delete, object, nil, map[string]string{"profile": "selected"}, true},
		{"namespace-mismatch", admissionv1.Create, nil, object, map[string]string{"profile": "other"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prepared.Matches(t.Context(), replication.ResourceKey(gvr, tc.operation), tc.operation, tc.ns, tc.old, tc.current)
			if err != nil || got != tc.want {
				t.Fatalf("match=%t err=%v", got, err)
			}
		})
	}
	// An error in one alternative must not suppress another matching trigger.
	spec.Triggers[0].NamespaceSelector = nil
	spec.Triggers[0].MatchConditions[0].Expression = "object.missing == 'yes'"
	fallback := watchTrigger()
	fallback.Name = "fallback"
	spec.Triggers = append(spec.Triggers, fallback)
	prepared, err = replication.PrepareTriggers(spec, false, cache.NewLabelSelectorCache(), compiler)
	if err != nil {
		t.Fatal(err)
	}
	if match, err := prepared.Matches(context.Background(), replication.ResourceKey(gvr, admissionv1.Create), admissionv1.Create, nil, nil, object); !match || err != nil {
		t.Fatalf("fallback match=%t err=%v", match, err)
	}
}

func TestManagedEligibilityPreservesLegacyTenantAssignment(t *testing.T) {
	for _, name := range []string{"controller", "resources", "replications"} {
		obj := &metav1.PartialObjectMetadata{Labels: map[string]string{meta.ManagedByCapsuleLabel: name}}
		if replication.Managed(obj) {
			t.Fatal("tenant name treated as management")
		}
		obj.Labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
		if !replication.Managed(obj) {
			t.Fatal("actual management not detected")
		}
	}
}

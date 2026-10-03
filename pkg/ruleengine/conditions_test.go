// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func conditionCache(t testing.TB) *cache.CELCache {
	t.Helper()
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAdmissionConditionGates(t *testing.T) {
	c := conditionCache(t)
	for _, tc := range []struct {
		name        string
		expressions []string
		want        bool
		failure     string
	}{
		{"unconditional", nil, true, ""},
		{"object and request", []string{`!has(object.spec.nodeSelector)`, `request.operation == 'CREATE' && request.namespace == 'tenant-a' && request.userInfo.username == 'alice'`}, true, ""},
		{"all required", []string{"true", "false"}, false, ""},
		{"any within expression", []string{"false || true"}, true, ""},
		{"error", []string{`object.spec.missing == 'x'`}, false, "gate"},
		{"false beats previous error", []string{`object.spec.missing == 'x'`, "false"}, false, ""},
		{"false beats later error", []string{"false", `object.spec.missing == 'x'`}, false, ""},
		{"request cannot expose old object", []string{`request.oldObject != null`}, false, "oldObject"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evaluator := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Operation: admissionv1.Create, Namespace: "tenant-a"})
			request := admissionv1.AdmissionRequest{Operation: admissionv1.Create, Namespace: "tenant-a"}
			request.UserInfo.Username = "alice"
			evaluator = ruleengine.NewConditionEvaluator(c, request)
			conditions := make([]rules.AdmissionCondition, len(tc.expressions))
			for i, e := range tc.expressions {
				conditions[i] = rules.AdmissionCondition{Name: "gate", Expression: e}
			}
			got, err := evaluator.Matches(context.Background(), &corev1.Pod{}, conditions)
			if got != tc.want || (tc.failure == "" && err != nil) || (tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure))) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
	if _, err := c.GetOrCompileBoolean(`request.operation == 'CREATE'`, environment.NewExpressions); err == nil {
		t.Fatal("admission request leaked into quota CEL environment")
	}
	if _, err := c.GetOrCompileCondition(`'not boolean'`, environment.NewExpressions); err == nil {
		t.Fatal("non-Boolean condition accepted")
	}
}

func TestConditionCacheHasNoTenantResults(t *testing.T) {
	c := conditionCache(t)
	conditions := []rules.AdmissionCondition{{Expression: `object.metadata.namespace == request.namespace && request.namespace == 'tenant-a'`}}
	for _, namespace := range []string{"tenant-a", "tenant-b", "tenant-a"} {
		evaluator := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Namespace: namespace})
		ok, err := evaluator.Matches(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace}}, conditions)
		if err != nil || ok != (namespace == "tenant-a") {
			t.Fatalf("namespace=%s result=%v error=%v", namespace, ok, err)
		}
	}
	if c.Stats() != 1 {
		t.Fatalf("shared expression compiled %d times", c.Stats())
	}
}

func TestConditionCostLimit(t *testing.T) {
	c := conditionCache(t)
	expression := `object.spec.containers.all(a, object.spec.containers.all(b, a.name != b.name || a.name == b.name))`
	pod := &corev1.Pod{}
	for i := 0; i < 600; i++ {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: fmt.Sprint(i)})
	}
	_, err := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{}).Matches(context.Background(), pod, []rules.AdmissionCondition{{Name: "expensive", Expression: expression}})
	if err == nil || !strings.Contains(err.Error(), "cost limit") {
		t.Fatalf("expected bounded evaluation, got %v", err)
	}
}

func TestValidateMutationAndConditions(t *testing.T) {
	c := conditionCache(t)
	for _, tc := range []struct{ name, yaml, want string }{
		{"empty mutation", `mutate: [{}]`, "workload mutation property"},
		{"invalid action", `mutate: [{action: append, workloads: {"placement": {tolerations: []}}}]`, "action"},
		{"clear all", `mutate: [{action: replace, workloads: {"placement": {nodeSelector: {}, tolerations: [], topologySpreadConstraints: [], affinity: {}}}}]`, ""},
		{"conditional mutation", `mutate: [{conditions: [{name: create, expression: "request.operation == 'CREATE'"}], workloads: {"placement": {nodeSelector: {pool: shared}}}}]`, ""},
		{"invalid mutation expression", `mutate: [{conditions: [{expression: "object.spec."}], workloads: {"placement": {tolerations: []}}}]`, "mutate[0].conditions[0]"},
		{"Boolean required", `enforce: {conditions: [{expression: "'x'"}]}`, "must evaluate to bool"},
		{"invalid enforcement expression", `enforce: {conditions: [{expression: "1"}]}`, "enforce.conditions[0]"},
		{"duplicate names", `enforce: {conditions: [{name: same, expression: "true"}, {name: same, expression: "false"}]}`, "duplicate condition name"},
		{"invalid name", `enforce: {conditions: [{name: 'not valid', expression: "true"}]}`, ".name"},
		{"empty expression", `enforce: {conditions: [{expression: ""}]}`, "must not be empty"},
		{"shared gate", `enforce: {conditions: [{expression: "has(object.metadata.labels)"}], workloads: {"placement": {tolerations: [{}]}}, services: {types: [NodePort]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body rules.NamespaceRuleBodyNamespace
			if err := yaml.UnmarshalStrict([]byte(tc.yaml), &body); err != nil {
				t.Fatal(err)
			}
			err := ruleengine.ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{&body}, c)
			if (tc.want == "" && err != nil) || (tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want))) {
				t.Fatalf("wanted %q got %v", tc.want, err)
			}
		})
	}
}

func TestMutationJSONPreservesExplicitEmptyProperties(t *testing.T) {
	input := []byte(`{"mutate": [{"action": "replace", "workloads": {"placement": {"scheduler": "tenant-scheduler", "nodeSelector": {}, "tolerations": [], "topologySpreadConstraints": [], "affinity": {}}, "security": {"hostUsers": false}}}, {"workloads": {"placement": {"nodeSelector": {"pool": "shared"}}}}]}`)
	var body rules.NamespaceRuleBodyNamespace
	if err := json.Unmarshal(input, &body); err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(body.DeepCopy())
	if err != nil {
		t.Fatal(err)
	}
	var again rules.NamespaceRuleBodyNamespace
	if err = json.Unmarshal(output, &again); err != nil {
		t.Fatal(err)
	}
	first := again.Mutate[0].Workloads
	if first.Placement.Scheduler != "tenant-scheduler" || first.Security.HostUsers == nil || *first.Security.HostUsers || first.Placement.NodeSelector == nil || first.Placement.Tolerations == nil || first.Placement.TopologySpreadConstraints == nil || first.Placement.Affinity == nil {
		t.Fatalf("explicit empty value lost: %s", output)
	}
	second := again.Mutate[1].Workloads
	if second.Placement.Scheduler != "" || second.Security.HostUsers != nil || second.Placement.Tolerations != nil || second.Placement.TopologySpreadConstraints != nil || second.Placement.Affinity != nil {
		t.Fatalf("omitted values became explicit: %s", output)
	}
}

func BenchmarkAdmissionConditions(b *testing.B) {
	for _, count := range []int{0, 1, 20} {
		for _, mode := range []string{"allow", "skip", "error"} {
			b.Run(fmt.Sprintf("rules=%d/%s", count, mode), func(b *testing.B) {
				c := conditionCache(b)
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"profile": "shared"}}}
				expression := `object.metadata.labels['profile'] == 'shared' && request.namespace == 'tenant-a'`
				if mode == "skip" {
					expression = `object.metadata.labels['profile'] == 'dedicated'`
				}
				if mode == "error" {
					expression = `object.spec.missing == 'x'`
				}
				bodies := make([]*rules.NamespaceRuleEnforceBody, count)
				for i := range bodies {
					bodies[i] = &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: expression}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{}}
				}
				_, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					evaluator := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Namespace: "tenant-a"})
					result, err := ruleengine.FilterEnforcementConditions(context.Background(), evaluator, pod, bodies, func(*rules.NamespaceRuleEnforceBody) bool { return true })
					if mode == "error" && count > 0 {
						if err == nil {
							b.Fatal("missing error")
						}
						continue
					}
					want := count
					if mode == "skip" {
						want = 0
					}
					if err != nil || len(result) != want {
						b.Fatalf("result=%d error=%v", len(result), err)
					}
				}
			})
		}
	}
	for _, mode := range []string{"cold", "warm", "invalidated", "quota-reset", "parallel"} {
		b.Run("cache/"+mode, func(b *testing.B) {
			c := conditionCache(b)
			expression := `request.namespace == 'tenant-a' && !has(object.spec.nodeSelector)`
			pod := &corev1.Pod{}
			conditions := []rules.AdmissionCondition{{Expression: expression}}
			run := func() {
				e := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Namespace: "tenant-a"})
				ok, err := e.Matches(context.Background(), pod, conditions)
				if err != nil || !ok {
					b.Errorf("result=%v error=%v", ok, err)
				}
			}
			run()
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "parallel" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						run()
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c = conditionCache(b)
				}
				if mode == "invalidated" {
					c.PruneConditions(nil)
				}
				if mode == "quota-reset" {
					c.ResetQuotaExpressions()
				}
				run()
			}
		})
	}
}

func TestConditionsOnlyAtActionLevel(t *testing.T) {
	for _, input := range []string{
		`mutate: [{workloads: {conditions: [{expression: "false"}], "placement": {nodeSelector: {pool: shared}}}}]`,
		`enforce: {workloads: {conditions: [{expression: "false"}]}}`,
		`enforce: {services: {conditions: [{expression: "false"}]}}`,
	} {
		var body rules.NamespaceRuleBodyNamespace
		if err := yaml.UnmarshalStrict([]byte(input), &body); err == nil {
			t.Fatalf("obsolete nested conditions accepted: %s", input)
		}
	}
}

func TestConditionRawObjectDecoding(t *testing.T) {
	c := conditionCache(t)
	for _, tc := range []struct {
		raw  string
		fail bool
	}{
		{`{"spec":{"enabled":true}}`, false}, {`null`, true}, {`[]`, true}, {`{`, true}, {``, true},
	} {
		req := admissionv1.AdmissionRequest{}
		req.Object.Raw = []byte(tc.raw)
		evaluator := ruleengine.NewConditionEvaluator(c, req)
		got, err := evaluator.Matches(t.Context(), nil, []rules.AdmissionCondition{{Expression: "object.spec.enabled == true"}})
		if (err != nil) != tc.fail || got == tc.fail {
			t.Fatalf("raw=%q result=%v error=%v", tc.raw, got, err)
		}
		if got, err := evaluator.Matches(t.Context(), nil, nil); err != nil || !got {
			t.Fatalf("ungated rules attempted decoding: %v", err)
		}
	}
}

func TestNamespaceEnforcementGatePreservesMutationAndOrder(t *testing.T) {
	c := conditionCache(t)
	body := &rules.NamespaceRuleBodyNamespace{
		Mutate:  []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"pool": "shared"}}}}},
		Enforce: &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: "false"}}},
	}
	original := body.DeepCopy()
	bodies := []*rules.NamespaceRuleBodyNamespace{nil, body, {}, body}
	filtered, err := ruleengine.FilterNamespaceEnforcementConditions(t.Context(), ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{}), &corev1.Pod{}, bodies, func(*rules.NamespaceRuleEnforceBody) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 4 || filtered[0] != nil || filtered[1].Enforce != nil || filtered[2] != bodies[2] || filtered[3].Enforce != nil {
		t.Fatalf("incorrect filtered order: %#v", filtered)
	}
	if filtered[1].Mutate[0].Workloads.Placement.NodeSelector["pool"] != "shared" || body.Enforce.Conditions[0] != original.Enforce.Conditions[0] {
		t.Fatal("lost independent mutation or modified cached input")
	}
}

func TestRawConditionsPreserveIntegerValues(t *testing.T) {
	request := admissionv1.AdmissionRequest{}
	request.Object.Raw = []byte(`{"spec":{"replicas":3,"large":9007199254740993}}`)
	matched, err := ruleengine.NewConditionEvaluator(conditionCache(t), request).Matches(t.Context(), nil, []rules.AdmissionCondition{{Expression: "object.spec.replicas % 2 == 1 && object.spec.large == 9007199254740993"}})
	if err != nil || !matched {
		t.Fatalf("numeric values changed during request decoding: %v %v", matched, err)
	}
}

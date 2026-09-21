// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func mutationConditions(t testing.TB) *ruleengine.ConditionEvaluator {
	t.Helper()
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	return ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Operation: admissionv1.Create})
}

func TestOrderedConditionalMutations(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		{Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"pool": "shared"}}},
		{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Name: "shared", Expression: `object.spec.nodeSelector['pool'] == 'shared' && request.operation == 'CREATE'`}}, Tolerations: []corev1.Toleration{{Key: "shared", Operator: corev1.TolerationOpExists}}}},
		{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Expression: `!has(object.spec.nodeSelector)`}}, NodeSelector: map[string]string{"wrong": "yes"}}},
		{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"pool": "dedicated"}}},
		{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Expression: `object.spec.nodeSelector['pool'] == 'dedicated'`}}, NodeSelector: map[string]string{"seen": "yes"}}},
	}}}
	before := bodies[0].DeepCopy()
	pod := &corev1.Pod{}
	for pass := 0; pass < 2; pass++ {
		changed, err := MutatePodPlacement(context.Background(), pod, bodies, mutationConditions(t))
		if err != nil || changed != (pass == 0) {
			t.Fatalf("pass=%d changed=%v error=%v", pass, changed, err)
		}
	}
	if pod.Spec.NodeSelector["pool"] != "dedicated" || pod.Spec.NodeSelector["seen"] != "yes" || len(pod.Spec.NodeSelector) != 2 || len(pod.Spec.Tolerations) != 1 {
		t.Fatalf("wrong mutation: %+v", pod.Spec)
	}
	if !equality.Semantic.DeepEqual(before, bodies[0]) {
		t.Fatal("modified rule inputs")
	}
}

func TestReplacePlacementPreservesOmittedAndClearsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		clear      bool
	}{
		{"replace supplied selectors", `{"nodeSelector":{"new":"value"}}`, false},
		{"clear explicit properties", `{"nodeSelector":{},"tolerations":[],"topologySpreadConstraints":[],"affinity":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			if _, err := MutatePodPlacement(context.Background(), pod, placementBaseline(), nil); err != nil {
				t.Fatal(err)
			}
			original := pod.DeepCopy()
			var workload rules.WorkloadMutation
			if err := json.Unmarshal([]byte(tc.rule), &workload); err != nil {
				t.Fatal(err)
			}
			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: workload}}}}
			before := bodies[0].DeepCopy()
			for pass := 0; pass < 2; pass++ {
				changed, err := MutatePodPlacement(context.Background(), pod, bodies, nil)
				if err != nil || changed != (pass == 0) {
					t.Fatalf("pass=%d changed=%v err=%v", pass, changed, err)
				}
			}
			if _, ok := pod.Spec.NodeSelector["pool"]; ok {
				t.Fatal("replace retained prior selector")
			}
			if tc.clear {
				if len(pod.Spec.NodeSelector) != 0 || len(pod.Spec.Tolerations) != 0 || len(pod.Spec.TopologySpreadConstraints) != 0 || !equality.Semantic.DeepEqual(pod.Spec.Affinity, &corev1.Affinity{}) {
					t.Fatalf("failed to clear: %+v", pod.Spec)
				}
			} else {
				if pod.Spec.NodeSelector["new"] != "value" || !equality.Semantic.DeepEqual(original.Spec.Tolerations, pod.Spec.Tolerations) || !equality.Semantic.DeepEqual(original.Spec.TopologySpreadConstraints, pod.Spec.TopologySpreadConstraints) || !equality.Semantic.DeepEqual(original.Spec.Affinity, pod.Spec.Affinity) {
					t.Fatal("replace modified omitted fields")
				}
			}
			pod.Spec.NodeSelector["other"] = "local"
			if !equality.Semantic.DeepEqual(before, bodies[0]) {
				t.Fatal("Pod aliases rule")
			}
		})
	}
}

func TestWorkloadConditionErrorDoesNotCommitPartialMutation(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		{Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"first": "yes"}}},
		{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Name: "broken", Expression: `object.spec.missing == 'x'`}}, Tolerations: []corev1.Toleration{}}},
	}}}
	values, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&corev1.Pod{})
	if err != nil {
		t.Fatal(err)
	}
	obj := &unstructured.Unstructured{Object: values}
	before := obj.DeepCopy()
	changed, err := MutateWorkloadResources(context.Background(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{}))
	if err == nil || !strings.Contains(err.Error(), `rules[0].mutate[1].workloads: conditions[0] ("broken")`) || changed || !equality.Semantic.DeepEqual(before, obj) {
		t.Fatalf("changed=%v error=%v object=%v", changed, err, obj)
	}
	// Resource routing happens before conditions; a Service must not evaluate this Pod expression.
	changed, err = MutateWorkloadResources(context.Background(), obj, corev1.SchemeGroupVersion.WithKind("Service"), bodies, nil)
	if err != nil || changed {
		t.Fatalf("Service evaluated Pod conditions: %v", err)
	}
}

func TestMutationHandlerSkipsPlacementOutsidePodCreate(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	h := MetadataRules(c).(*metadataRules)
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Expression: `object.spec.missing == 'x'`}}, NodeSelector: map[string]string{"pool": "shared"}}}}}}
	for _, req := range []admissionv1.AdmissionRequest{{Operation: admissionv1.Update}, {Operation: admissionv1.Create, SubResource: "status"}} {
		req.Kind.Group = ""
		req.Kind.Version = "v1"
		req.Kind.Kind = "Pod"
		obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{}}}
		if response := h.mutate(obj, bodies)(context.Background(), admission.Request{AdmissionRequest: req}); response != nil {
			t.Fatalf("unexpected placement response: %+v", response)
		}
	}
}

func BenchmarkConditionalPodMutation(b *testing.B) {
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
		b.Run(string(action), func(b *testing.B) {
			c, err := cache.NewCELCache()
			if err != nil {
				b.Fatal(err)
			}
			bodies := placementBaseline()
			bodies[0].Mutate[0].Action = action
			bodies[0].Mutate[0].Workloads.Conditions = []rules.AdmissionCondition{{Expression: `request.operation == 'CREATE' && !has(object.spec.nodeSelector)`}}
			b.ReportAllocs()
			for b.Loop() {
				pod := &corev1.Pod{}
				e := ruleengine.NewConditionEvaluator(c, admissionv1.AdmissionRequest{Operation: admissionv1.Create})
				changed, err := MutatePodPlacement(context.Background(), pod, bodies, e)
				if err != nil || !changed {
					b.Fatalf("changed=%v err=%v", changed, err)
				}
			}
		})
	}
}

func TestWorkloadResourceMutationConditionsAndPlacementOrdering(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		expression := "false"
		if enabled {
			expression = "true"
		}
		quantity := resource.MustParse("1")
		body := &rules.NamespaceRuleBodyNamespace{
			Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
				Conditions: []rules.AdmissionCondition{{Expression: expression}}, Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
				Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceCPU: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: &quantity}}},
			}},
			Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Conditions: []rules.AdmissionCondition{{Expression: `has(object.spec.containers[0].resources.requests) && 'cpu' in object.spec.containers[0].resources.requests && object.spec.containers[0].resources.requests['cpu'] == '1'`}}, NodeSelector: map[string]string{"resources-applied": "yes"}}}},
		}
		pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app"}}}}
		values, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
		if err != nil {
			t.Fatal(err)
		}
		obj := &unstructured.Unstructured{Object: values}
		changed, err := MutateWorkloadResources(context.Background(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
		if err != nil || changed != enabled {
			t.Fatalf("enabled=%v changed=%v err=%v", enabled, changed, err)
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod); err != nil {
			t.Fatal(err)
		}
		_, hasCPU := pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
		if hasCPU != enabled || (pod.Spec.NodeSelector["resources-applied"] == "yes") != enabled {
			t.Fatalf("resource gate or snapshot stale: %+v", pod.Spec)
		}
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func TestSchedulerMutationActions(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, input := range []string{"", corev1.DefaultSchedulerName, "custom-scheduler", "tenant-scheduler"} {
			for _, configured := range []string{"", corev1.DefaultSchedulerName, "tenant-scheduler"} {
				t.Run(fmt.Sprintf("%s/%s-to-%s", action, input, configured), func(t *testing.T) {
					pod := &corev1.Pod{Spec: corev1.PodSpec{SchedulerName: input, NodeSelector: map[string]string{"keep": "yes"}}}
					bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Scheduler: configured}}}}}
					before := bodies[0].DeepCopy()
					want := input
					if configured != "" && (action == rules.MutationActionReplace || input == "") {
						want = configured
					}
					for pass := range 2 {
						changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && want != input, changed)
						require.Equal(t, want, pod.Spec.SchedulerName)
					}
					require.Equal(t, map[string]string{"keep": "yes"}, pod.Spec.NodeSelector)
					require.Equal(t, before, bodies[0])
				})
			}
		}
	}
}

func TestSchedulerMutationOrderAndConditions(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: []rules.NamespaceRuleMutation{
		{Workloads: rules.WorkloadMutation{Scheduler: "first"}},
		{Workloads: rules.WorkloadMutation{Scheduler: "second"}},
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `object.spec.schedulerName == 'first'`}}, Workloads: rules.WorkloadMutation{Scheduler: "replaced"}},
		{Conditions: []rules.AdmissionCondition{{Expression: `object.spec.schedulerName == 'replaced'`}}, Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"observed": "yes"}}},
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{Scheduler: "skipped"}},
	}}}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"schedulerName": ""}}}
	for pass := range 2 {
		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t))
		require.NoError(t, err)
		require.Equal(t, pass == 0, changed)
	}
	name, found, err := unstructured.NestedString(obj.Object, "spec", "schedulerName")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "replaced", name)
	observed, _, err := unstructured.NestedString(obj.Object, "spec", "nodeSelector", "observed")
	require.NoError(t, err)
	require.Equal(t, "yes", observed)
}

func TestSchedulerConditionalDefault(t *testing.T) {
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{
		Action:     rules.MutationActionReplace,
		Conditions: []rules.AdmissionCondition{{Name: "default-scheduler", Expression: `!has(object.spec.schedulerName) || object.spec.schedulerName in ['', 'default-scheduler']`}},
		Workloads:  rules.WorkloadMutation{Scheduler: "tenant-scheduler"},
	}}}
	for _, input := range []string{"omitted", "", corev1.DefaultSchedulerName, "custom-scheduler"} {
		t.Run(input, func(t *testing.T) {
			spec := map[string]any{}
			if input != "omitted" {
				spec["schedulerName"] = input
			}
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": spec}}
			want := "tenant-scheduler"
			if input == "custom-scheduler" {
				want = input
			}
			for pass := range 2 {
				changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
				require.NoError(t, err)
				require.Equal(t, pass == 0 && input != "custom-scheduler", changed)
				got, found, err := unstructured.NestedString(obj.Object, "spec", "schedulerName")
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, want, got)
			}
		})
	}
}

func TestSchedulerMutationHandlerScope(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Scheduler: "tenant-scheduler"}}}}}
	h := MetadataRules(nil)
	for _, tc := range []struct {
		name, kind, subresource string
		operation               admissionv1.Operation
		want                    bool
	}{
		{"pod create", "Pod", "", admissionv1.Create, true},
		{"pod update", "Pod", "", admissionv1.Update, false},
		{"pod delete", "Pod", "", admissionv1.Delete, false},
		{"pod subresource", "Pod", "status", admissionv1.Create, false},
		{"service create", "Service", "", admissionv1.Create, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": tc.kind, "spec": map[string]any{"schedulerName": "custom"}}}
			before := obj.DeepCopy()
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Version: "v1", Kind: tc.kind}, Operation: tc.operation, SubResource: tc.subresource, Object: runtime.RawExtension{Raw: raw}}}
			var response *admission.Response
			switch tc.operation {
			case admissionv1.Create:
				response = h.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Update:
				response = h.OnUpdate(nil, nil, before, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Delete:
				response = h.OnDelete(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req)
			}
			if tc.want {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
				name, _, err := unstructured.NestedString(obj.Object, "spec", "schedulerName")
				require.NoError(t, err)
				require.Equal(t, "tenant-scheduler", name)
			} else {
				require.Nil(t, response)
				require.Equal(t, before, obj)
			}
		})
	}
}

func BenchmarkSchedulerMutation(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"merge", "preserve", "preserve-default", "replace", "conditional-default", "conditional-preserve", "skip"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					conditional := mode == "conditional-default" || mode == "conditional-preserve"
					const expression = `!has(object.spec.schedulerName) || object.spec.schedulerName in ['', 'default-scheduler']`
					var compiler ruleengine.ConditionCompiler
					if conditional {
						c, err := cache.NewCELCache()
						require.NoError(b, err)
						_, err = c.GetOrCompileCondition(expression, environment.StoredExpressions)
						require.NoError(b, err)
						compiler = c
					}
					bodies := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					objects := make([]*unstructured.Unstructured, tenants)
					scheduler := corev1.DefaultSchedulerName
					if mode == "merge" {
						scheduler = ""
					}
					if mode == "preserve" || mode == "conditional-preserve" {
						scheduler = "custom"
					}
					schedulerValue := any(scheduler)
					for tenant := range tenants {
						pod := &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, Spec: corev1.PodSpec{SchedulerName: scheduler, Containers: []corev1.Container{{Name: "app", Image: "example/app:v1"}}}}
						object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
						if err != nil {
							b.Fatal(err)
						}
						objects[tenant] = &unstructured.Unstructured{Object: object}
						for rule := range count {
							action := rules.MutationActionMerge
							if mode == "replace" || conditional {
								action = rules.MutationActionReplace
							}
							body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Scheduler: fmt.Sprintf("tenant-%d-scheduler-%d", tenant, rule)}}}}
							if conditional {
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Name: "default-scheduler", Expression: expression}}
							}
							if mode == "skip" {
								body.Mutate = nil
							}
							bodies[tenant] = append(bodies[tenant], body)
						}
					}
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						obj := objects[i%tenants]
						// Reset the input so each iteration performs a fresh admission mutation.
						obj.Object["spec"].(map[string]any)["schedulerName"] = schedulerValue
						var conditions *ruleengine.ConditionEvaluator
						if conditional {
							conditions = ruleengine.NewConditionEvaluator(compiler, admissionv1.AdmissionRequest{Operation: admissionv1.Create})
						}
						changed, err := MutateWorkloadResources(b.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies[i%tenants], conditions)
						if err != nil || changed != (mode == "merge" || mode == "replace" || mode == "conditional-default") {
							b.Fatalf("changed=%v error=%v", changed, err)
						}
						want := scheduler
						if changed {
							index := 0
							if mode == "replace" {
								index = count - 1
							}
							want = bodies[i%tenants][index].Mutate[0].Workloads.Scheduler
						}
						got, _, err := unstructured.NestedString(obj.Object, "spec", "schedulerName")
						if err != nil || got != want {
							b.Fatalf("scheduler=%q want=%q error=%v", got, want, err)
						}
					}
				})
			}
		}
	}
}

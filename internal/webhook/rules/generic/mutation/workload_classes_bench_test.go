// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func BenchmarkPriorityClassMutation(b *testing.B) {
	const expression = `!has(object.spec.priorityClassName) || object.spec.priorityClassName in ['', 'cluster-default']`

	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"merge", "merge-global-default", "preserve", "unchanged", "replace", "conditional-default", "conditional-preserve", "skip"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					conditional := mode == "conditional-default" || mode == "conditional-preserve"

					var compiler ruleengine.ConditionCompiler

					if conditional {
						c, err := cache.NewCELCache()
						if err != nil {
							b.Fatal(err)
						}

						if _, err = c.GetOrCompileCondition(expression, environment.StoredExpressions); err != nil {
							b.Fatal(err)
						}

						compiler = c
					}

					scheme := runtime.NewScheme()
					if err := schedulingv1.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}

					classes := []client.Object{
						&schedulingv1.PriorityClass{Name: classGlobalDefault, Value: 10, GlobalDefault: true, PreemptionPolicy: new(corev1.PreemptLowerPriority)},
						&schedulingv1.PriorityClass{Name: classCustom, Value: 500, PreemptionPolicy: new(corev1.PreemptLowerPriority)},
					}

					bodies := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					objects := make([]*unstructured.Unstructured, tenants)
					defaults := make([]string, tenants)

					for tenant := range tenants {
						defaults[tenant] = fmt.Sprintf("tenant-%d-default", tenant)
						classes = append(classes, &schedulingv1.PriorityClass{Name: defaults[tenant], Value: 1000, PreemptionPolicy: new(corev1.PreemptNever)})

						pod := &corev1.Pod{APIVersion: "v1", Kind: "Pod", Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/app:v1"}}}}

						object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
						if err != nil {
							b.Fatal(err)
						}

						objects[tenant] = &unstructured.Unstructured{Object: object}

						for range count {
							action := rules.MutationActionMerge
							if mode == "replace" || conditional {
								action = rules.MutationActionReplace
							}

							body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: defaults[tenant]}}}}}
							if conditional {
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Name: "no-class", Expression: expression}}
							}

							if mode == "skip" {
								body.Mutate = nil
							}

							bodies[tenant] = append(bodies[tenant], body)
						}
					}

					// The cache and the API server hold the same classes; reads are counted apart.
					cached := &priorityClasses{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(classes...).Build()}
					api := &priorityClasses{Client: cached.Client}

					// The Pod as the built-in Priority plugin hands it to webhooks.
					input := func(tenant int) (string, int64, string) {
						switch mode {
						case "merge-global-default":
							return classGlobalDefault, 10, string(corev1.PreemptLowerPriority)
						case "preserve", "replace", "conditional-preserve":
							return classCustom, 500, string(corev1.PreemptLowerPriority)
						case "unchanged":
							return defaults[tenant], 1000, string(corev1.PreemptNever)
						default:
							return "", 0, string(corev1.PreemptLowerPriority)
						}
					}

					wantChanged := mode == "merge" || mode == "merge-global-default" || mode == "replace" || mode == "conditional-default"

					b.ReportAllocs()

					for i := 0; b.Loop(); i++ {
						tenant := i % tenants
						obj := objects[tenant]
						spec := obj.Object["spec"].(map[string]any)
						// Reset the input so each iteration performs a fresh admission mutation.
						class, priority, policy := input(tenant)
						spec["priorityClassName"], spec["priority"], spec["preemptionPolicy"] = class, priority, policy

						var conditions *ruleengine.ConditionEvaluator
						if conditional {
							conditions = ruleengine.NewConditionEvaluator(compiler, admissionv1.AdmissionRequest{Operation: admissionv1.Create})
						}

						changed, err := MutateWorkloadResources(b.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies[tenant], conditions, cached, api)
						if err != nil || changed != wantChanged {
							b.Fatalf("changed=%v error=%v", changed, err)
						}

						want := class
						if changed {
							want = defaults[tenant]
						}

						got, _, err := unstructured.NestedString(obj.Object, "spec", "priorityClassName")
						if err != nil || got != want {
							b.Fatalf("priority class=%q want=%q error=%v", got, want, err)
						}
					}

					b.ReportMetric(float64(cached.gets.Load())/float64(b.N), "cache-reads/op")
					b.ReportMetric(float64(api.gets.Load())/float64(b.N), "api-reads/op")
				})
			}
		}
	}
}

func BenchmarkRuntimeClassMutation(b *testing.B) {
	const expression = `!has(object.spec.runtimeClassName) || object.spec.runtimeClassName == ''`

	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"merge", "preserve", "unchanged", "replace", "replace-overhead", "conditional-default", "conditional-preserve", "skip"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					conditional := mode == "conditional-default" || mode == "conditional-preserve"

					var compiler ruleengine.ConditionCompiler

					if conditional {
						c, err := cache.NewCELCache()
						if err != nil {
							b.Fatal(err)
						}

						if _, err = c.GetOrCompileCondition(expression, environment.StoredExpressions); err != nil {
							b.Fatal(err)
						}

						compiler = c
					}

					bodies := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					objects := make([]*unstructured.Unstructured, tenants)
					defaults := make([]string, tenants)

					for tenant := range tenants {
						defaults[tenant] = fmt.Sprintf("tenant-%d-default", tenant)

						pod := &corev1.Pod{APIVersion: "v1", Kind: "Pod", Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/app:v1"}}}}

						object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
						if err != nil {
							b.Fatal(err)
						}

						objects[tenant] = &unstructured.Unstructured{Object: object}

						for range count {
							action := rules.MutationActionMerge
							if mode == "replace" || mode == "replace-overhead" || conditional {
								action = rules.MutationActionReplace
							}

							body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: defaults[tenant]}}}}}
							if conditional {
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Name: "no-class", Expression: expression}}
							}

							if mode == "skip" {
								body.Mutate = nil
							}

							bodies[tenant] = append(bodies[tenant], body)
						}
					}

					// A runtime class is assigned without reading any class; both readers must stay idle.
					readers := &priorityClasses{Client: fake.NewClientBuilder().Build()}

					// The Pod as the built-in RuntimeClass plugin hands it to webhooks.
					input := func(tenant int) (class string, overhead bool) {
						switch mode {
						case "preserve", "replace", "conditional-preserve":
							return classCustom, false
						case "replace-overhead":
							return classCustom, true
						case "unchanged":
							return defaults[tenant], false
						default:
							return "", false
						}
					}

					wantChanged := mode == "merge" || mode == "replace" || mode == "replace-overhead" || mode == "conditional-default"

					b.ReportAllocs()

					for i := 0; b.Loop(); i++ {
						tenant := i % tenants
						obj := objects[tenant]
						spec := obj.Object["spec"].(map[string]any)
						// Reset the input so each iteration performs a fresh admission mutation.
						class, overhead := input(tenant)

						delete(spec, "runtimeClassName")
						delete(spec, "overhead")

						if class != "" {
							spec["runtimeClassName"] = class
						}

						if overhead {
							spec["overhead"] = map[string]any{"cpu": "250m", "memory": "64Mi"}
						}

						var conditions *ruleengine.ConditionEvaluator
						if conditional {
							conditions = ruleengine.NewConditionEvaluator(compiler, admissionv1.AdmissionRequest{Operation: admissionv1.Create})
						}

						changed, err := MutateWorkloadResources(b.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies[tenant], conditions, readers, readers)
						if err != nil || changed != wantChanged {
							b.Fatalf("changed=%v error=%v", changed, err)
						}

						want := class
						if changed {
							want = defaults[tenant]
						}

						got, _, err := unstructured.NestedString(obj.Object, "spec", "runtimeClassName")
						if err != nil || got != want {
							b.Fatalf("runtime class=%q want=%q error=%v", got, want, err)
						}

						if _, found := obj.Object["spec"].(map[string]any)["overhead"]; found && changed {
							b.Fatal("the overhead of the replaced class was kept")
						}
					}

					b.ReportMetric(float64(readers.gets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}

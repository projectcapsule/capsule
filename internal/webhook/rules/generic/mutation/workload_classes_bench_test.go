// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
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

					scheme := runtime.NewScheme()
					if err := nodev1.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}

					// The defaults are scheduling-only classes: assigning one merges its
					// constraints and leaves no overhead.
					classes := []client.Object{runtimeClassObject(classCustom)}

					bodies := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					objects := make([]*unstructured.Unstructured, tenants)
					defaults := make([]string, tenants)

					for tenant := range tenants {
						defaults[tenant] = fmt.Sprintf("tenant-%d-default", tenant)
						classes = append(classes, &nodev1.RuntimeClass{
							Name:    defaults[tenant],
							Handler: "runc",
							Scheduling: &nodev1.Scheduling{
								NodeSelector: map[string]string{"runtime-" + defaults[tenant]: "true"},
								Tolerations:  []corev1.Toleration{{Key: "runtime-" + defaults[tenant], Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule}},
							},
						})

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

					// Only an assignment reads the class, once and authoritatively; the
					// cached reader serves only callers without an API reader. Reads are
					// counted apart.
					cached := &priorityClasses{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(classes...).Build()}
					api := &priorityClasses{Client: cached.Client}

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
						delete(spec, "nodeSelector")
						delete(spec, "tolerations")

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

						changed, err := MutateWorkloadResources(b.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies[tenant], conditions, cached, api)
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

						if changed {
							selected, _, err := unstructured.NestedString(obj.Object, "spec", "nodeSelector", "runtime-"+defaults[tenant])
							if err != nil || selected != "true" {
								b.Fatalf("node selector of the assigned class=%q error=%v", selected, err)
							}
						}
					}

					b.ReportMetric(float64(cached.gets.Load())/float64(b.N), "cache-reads/op")
					b.ReportMetric(float64(api.gets.Load())/float64(b.N), "api-reads/op")
				})
			}
		}
	}
}

// BenchmarkRuntimeClassSchedulingRestore measures the restoration of the
// scheduling of a kept class after a placement entry disturbed it, and the
// comparison that keeps an undisturbed Pod read-free.
func BenchmarkRuntimeClassSchedulingRestore(b *testing.B) {
	for _, mode := range []string{"restore", "untouched"} {
		b.Run(mode, func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := nodev1.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}

			// A scheduling-only class: nothing downstream would reject a Pod
			// whose selector a replacement dropped.
			class := runtimeClassObject(classSecondDefault)
			cached := &priorityClasses{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()}
			api := &priorityClasses{Client: cached.Client}

			mutation := rules.NamespaceRuleMutation{Action: rules.MutationActionReplace,
				Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"pool": "dedicated"}}}}
			if mode == "untouched" {
				mutation = rules.NamespaceRuleMutation{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "profile-scheduler"}}}
			}

			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}

			pod := &corev1.Pod{APIVersion: "v1", Kind: "Pod", Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/app:v1"}}}}

			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
			if err != nil {
				b.Fatal(err)
			}

			obj := &unstructured.Unstructured{Object: object}

			b.ReportAllocs()

			for b.Loop() {
				// Reset the Pod as the built-in RuntimeClass plugin hands it to
				// webhooks: the class it names and that class's scheduling.
				spec := obj.Object["spec"].(map[string]any)
				spec["runtimeClassName"] = classSecondDefault
				spec["nodeSelector"] = map[string]any{"keep": "yes", "runtime-" + classSecondDefault: "true"}
				spec["tolerations"] = []any{map[string]any{"key": "runtime-" + classSecondDefault, "operator": "Equal", "value": "true", "effect": "NoSchedule"}}
				delete(spec, "schedulerName")

				changed, err := MutateWorkloadResources(b.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, nil, cached, api)
				if err != nil || !changed {
					b.Fatalf("changed=%v error=%v", changed, err)
				}

				selected, _, err := unstructured.NestedString(obj.Object, "spec", "nodeSelector", "runtime-"+classSecondDefault)
				if err != nil || selected != "true" {
					b.Fatalf("node selector of the kept class=%q error=%v", selected, err)
				}
			}

			b.ReportMetric(float64(cached.gets.Load())/float64(b.N), "cache-reads/op")
			b.ReportMetric(float64(api.gets.Load())/float64(b.N), "api-reads/op")
		})
	}
}

// BenchmarkRuntimeClassTolerationMerge verifies the merge scales with what the
// class adds, not with the square of the Pod's own toleration list.
func BenchmarkRuntimeClassTolerationMerge(b *testing.B) {
	toleration := func(scope string, i int) corev1.Toleration {
		return corev1.Toleration{Key: fmt.Sprintf("%s-%d", scope, i), Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule}
	}

	for _, pods := range []int{8, 128, 1024} {
		for _, classes := range []int{1, 8} {
			b.Run(fmt.Sprintf("pod=%d/class=%d", pods, classes), func(b *testing.B) {
				pod := make([]corev1.Toleration, pods)
				for i := range pod {
					pod[i] = toleration("pod", i)
				}

				class := make([]corev1.Toleration, classes)
				for i := range class {
					class[i] = toleration("class", i)
				}

				b.ReportAllocs()

				for b.Loop() {
					if merged := mergeRuntimeClassTolerations(pod, class); len(merged) != pods+classes {
						b.Fatalf("merged %d tolerations, want %d", len(merged), pods+classes)
					}
				}
			})
		}
	}
}

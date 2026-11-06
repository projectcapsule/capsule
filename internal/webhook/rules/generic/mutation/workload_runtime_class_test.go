// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

const classSecondDefault = "second-default"

// noReads fails the test on any read: a runtime class is assigned without one.
type noReads struct {
	client.Client
	tb testing.TB
}

func (r noReads) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	r.tb.Helper()
	r.tb.Error("a runtime class mutation must not read any object")

	return errors.New("unexpected read")
}

func (r noReads) List(context.Context, client.ObjectList, ...client.ListOption) error {
	r.tb.Helper()
	r.tb.Error("a runtime class mutation must not list any object")

	return errors.New("unexpected list")
}

// admittedRuntimePod is a Pod as the built-in RuntimeClass plugin hands it to
// webhooks: the overhead and the scheduling of the class it names are set.
func admittedRuntimePod(class string) *corev1.Pod {
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		NodeSelector: map[string]string{"keep": "yes"},
		Containers:   []corev1.Container{{Name: "app", Image: "example.com/app:v1"}},
	}}

	if class != "" {
		pod.Spec.RuntimeClassName = new(class)
		pod.Spec.Overhead = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}
		pod.Spec.NodeSelector["runtime"] = class
		pod.Spec.Tolerations = []corev1.Toleration{{Key: "runtime", Operator: corev1.TolerationOpEqual, Value: class, Effect: corev1.TaintEffectNoSchedule}}
	}

	return pod
}

func runtimeClassMutation(action rules.MutationAction, class string) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: class}}}
}

func runtimeClassOf(pod *corev1.Pod) string {
	if pod.Spec.RuntimeClassName == nil {
		return ""
	}

	return *pod.Spec.RuntimeClassName
}

func TestRuntimeClassMutationActions(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, input := range []string{"", classCustom, classTenantDefault} {
			for _, configured := range []string{"", classTenantDefault} {
				t.Run(fmt.Sprintf("%s/%s-to-%s", action, input, configured), func(t *testing.T) {
					pod := admittedRuntimePod(input)
					original := pod.DeepCopy()

					mutation := runtimeClassMutation(action, configured)
					if configured == "" {
						// A mutation needs a property; this one must leave the class alone.
						mutation.Workloads.Placement.NodeSelector = maps.Clone(pod.Spec.NodeSelector)
					}

					bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}
					before := bodies[0].DeepCopy()

					assigned := configured != "" && input != configured && (action == rules.MutationActionReplace || input == "")

					for pass := range 2 {
						// No reader is passed: a runtime class is assigned without one.
						changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && assigned, changed)

						if !assigned {
							require.Equal(t, original.Spec, pod.Spec)

							continue
						}

						require.Equal(t, configured, runtimeClassOf(pod))
						require.Nil(t, pod.Spec.Overhead, "the API server derives the overhead of the assigned class")
						// What the API server merged for the replaced class stays on the Pod.
						require.Equal(t, original.Spec.NodeSelector, pod.Spec.NodeSelector)
						require.Equal(t, original.Spec.Tolerations, pod.Spec.Tolerations)
						require.Equal(t, original.Spec.Containers, pod.Spec.Containers)
					}

					require.Equal(t, before, bodies[0])
				})
			}
		}
	}
}

func TestRuntimeClassMutationOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     *string
		mutations []rules.NamespaceRuleMutation
		want      string
	}{
		{name: "first applicable merge wins", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault), runtimeClassMutation("", classSecondDefault)}, want: classTenantDefault},
		{name: "merge does not undo a replace", input: new(classCustom), mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault), runtimeClassMutation("", classSecondDefault)}, want: classTenantDefault},
		{name: "replace overrides a merge", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault), runtimeClassMutation(rules.MutationActionReplace, classSecondDefault)}, want: classSecondDefault},
		{name: "last replace wins", input: new(classCustom), mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault), runtimeClassMutation(rules.MutationActionReplace, classSecondDefault)}, want: classSecondDefault},
		{name: "explicit class survives every merge", input: new(classCustom), mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault), runtimeClassMutation("", classSecondDefault)}, want: classCustom},
		{name: "an empty name is an omitted class", input: new(""), mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault), runtimeClassMutation("", classSecondDefault)}, want: classTenantDefault},
		{name: "container targets do not select the pod class", mutations: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{
			Targets:    []rules.WorkloadValidationTarget{rules.ValidateContainers},
			Classes:    rules.WorkloadClassMutation{Runtime: classTenantDefault},
			Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullAlways},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := admittedRuntimePod("")
			pod.Spec.RuntimeClassName = tc.input
			bodies := []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: tc.mutations[:1]}, {Mutate: tc.mutations[1:]}}

			_, err := MutatePodPlacement(t.Context(), pod, bodies, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, runtimeClassOf(pod))
		})
	}
}

// The API server sets spec.overhead from the class a Pod names and rejects one
// that differs from it, so the overhead must not outlive the class it belongs to.
func TestRuntimeClassMutationOverhead(t *testing.T) {
	supplied := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}

	for _, tc := range []struct {
		name     string
		input    string
		supplied bool
		action   rules.MutationAction
		cleared  bool
	}{
		{name: "replace clears the overhead of the replaced class", input: classCustom, action: rules.MutationActionReplace, cleared: true},
		{name: "replace keeps the overhead of the class it confirms", input: classTenantDefault, action: rules.MutationActionReplace},
		{name: "merge keeps the overhead of the class the Pod names", input: classCustom, action: rules.MutationActionMerge},
		{name: "merge fills a Pod that has no overhead", action: rules.MutationActionMerge, cleared: true},
		{name: "merge clears an overhead supplied without a class", supplied: true, action: rules.MutationActionMerge, cleared: true},
		{name: "replace clears an overhead supplied without a class", supplied: true, action: rules.MutationActionReplace, cleared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := admittedRuntimePod(tc.input)
			if tc.supplied {
				pod.Spec.Overhead = supplied.DeepCopy()
			}

			before := pod.Spec.Overhead.DeepCopy()

			_, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(tc.action, classTenantDefault)}}}, nil)
			require.NoError(t, err)

			if tc.cleared {
				require.Equal(t, classTenantDefault, runtimeClassOf(pod))
				// A nil map is omitted from the patch; an empty one would not be.
				require.Nil(t, pod.Spec.Overhead)
			} else {
				require.Equal(t, before, pod.Spec.Overhead)
			}
		})
	}
}

// The API server calls the webhook again once its RuntimeClass plugin has set
// the overhead and the scheduling of the class Capsule assigned. Nothing derives
// the overhead after that call, so it must leave the Pod as it is: also when
// entries replace the class in turn, and when another entry changes the Pod.
func TestRuntimeClassMutationReinvocation(t *testing.T) {
	dedicated := rules.NamespaceRuleMutation{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{
		Tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}},
	}}}
	// What the RuntimeClass plugin merges into a Pod that names the assigned class.
	scheduled := corev1.Toleration{Key: "runtime", Operator: corev1.TolerationOpEqual, Value: classTenantDefault, Effect: corev1.TaintEffectNoSchedule}

	for _, tc := range []struct {
		name      string
		mutations []rules.NamespaceRuleMutation
		// changed tells whether the second call still changes the Pod.
		changed bool
	}{
		{name: "merge", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault)}},
		{name: "replace", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}},
		{name: "replaces in turn", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}},
		{name: "merge before a replace", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation("", classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}},
		// Replace swaps the whole toleration list, the entry the API server added for
		// the class included: a rule has to repeat it to keep it.
		{name: "replaces in turn next to another change", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault), dedicated}, changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: tc.mutations}}
			pod := admittedRuntimePod("")

			changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, classTenantDefault, runtimeClassOf(pod))
			require.Nil(t, pod.Spec.Overhead)

			// What the RuntimeClass plugin derives from the assigned class.
			derived := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}
			pod.Spec.Overhead = derived.DeepCopy()
			pod.Spec.NodeSelector["runtime"] = classTenantDefault
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, scheduled)

			changed, err = MutatePodPlacement(t.Context(), pod, bodies, nil)
			require.NoError(t, err)
			require.Equal(t, tc.changed, changed)
			require.Equal(t, classTenantDefault, runtimeClassOf(pod))
			require.Equal(t, derived, pod.Spec.Overhead, "the overhead of the assigned class must survive the second call")
			require.Equal(t, classTenantDefault, pod.Spec.NodeSelector["runtime"])

			if tc.changed {
				require.Equal(t, dedicated.Workloads.Placement.Tolerations, pod.Spec.Tolerations)
			} else {
				require.Equal(t, []corev1.Toleration{scheduled}, pod.Spec.Tolerations)
			}
		})
	}

	// Entries that end at the class a Pod arrived with leave its overhead alone too.
	pod := admittedRuntimePod(classTenantDefault)
	original := pod.DeepCopy()

	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		runtimeClassMutation(rules.MutationActionReplace, classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault),
	}}}, nil)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, original.Spec, pod.Spec)
}

// Nothing follows the second call. A class that a condition selects only then,
// from what the API server added for the class of the first call, is assigned
// by name alone. The overhead of the previous class must not stay on the Pod:
// the API server validates what is left, and rejects the Pod if the assigned
// class defines an overhead of its own.
func TestRuntimeClassMutationAssignedOnReinvocation(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		runtimeClassMutation("", classTenantDefault),
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `'runtime' in object.spec.nodeSelector`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: classSecondDefault}}},
	}}}
	mutate := func(pod *corev1.Pod) (*corev1.Pod, bool) {
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
		require.NoError(t, err)

		obj := &unstructured.Unstructured{Object: object}
		obj.SetAPIVersion("v1")
		obj.SetKind("Pod")

		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t))
		require.NoError(t, err)

		mutated := &corev1.Pod{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, mutated))

		return mutated, changed
	}

	// First call: nothing on the Pod matches the condition yet.
	first, changed := mutate(admittedRuntimePod(""))
	require.True(t, changed)
	require.Equal(t, classTenantDefault, runtimeClassOf(first))

	// The RuntimeClass plugin derives the overhead and scheduling of that class.
	first.Spec.Overhead = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}
	first.Spec.NodeSelector["runtime"] = classTenantDefault

	second, changed := mutate(first)
	require.True(t, changed)
	require.Equal(t, classSecondDefault, runtimeClassOf(second))
	require.Empty(t, second.Spec.Overhead, "the overhead of the first class must not be kept for another one")
	require.Equal(t, map[string]string{"keep": "yes", "runtime": classTenantDefault}, second.Spec.NodeSelector)
}

func TestRuntimeClassMutationDoesNotAliasTheRule(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}}}
	first, second := admittedRuntimePod(""), admittedRuntimePod(classCustom)

	for _, pod := range []*corev1.Pod{first, second} {
		changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil)
		require.NoError(t, err)
		require.True(t, changed)
	}

	require.NotSame(t, first.Spec.RuntimeClassName, second.Spec.RuntimeClassName)

	*first.Spec.RuntimeClassName = "changed"

	require.Equal(t, classTenantDefault, *second.Spec.RuntimeClassName)
	require.Equal(t, classTenantDefault, bodies[0].Mutate[0].Workloads.Classes.Runtime, "rules are cache-owned and must not change")
}

func TestRuntimeClassMutationConditions(t *testing.T) {
	pod := func(class string) *unstructured.Unstructured {
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(admittedRuntimePod(class))
		require.NoError(t, err)

		obj := &unstructured.Unstructured{Object: object}
		obj.SetAPIVersion("v1")
		obj.SetKind("Pod")

		return obj
	}
	mutate := func(obj *unstructured.Unstructured, mutations ...rules.NamespaceRuleMutation) bool {
		bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: mutations}}

		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t))
		require.NoError(t, err)

		// A second admission of the result changes nothing.
		again, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t))
		require.NoError(t, err)
		require.False(t, again)

		return changed
	}
	observe := func(expression string) rules.NamespaceRuleMutation {
		return rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: expression}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"observed": "yes"}}}}
	}
	chain := []rules.NamespaceRuleMutation{
		runtimeClassMutation("", classTenantDefault),
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `has(object.spec.runtimeClassName) && object.spec.runtimeClassName == 'tenant-default'`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: classSecondDefault}}},
		observe(`has(object.spec.runtimeClassName) && object.spec.runtimeClassName == 'second-default'`),
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: "skipped"}}},
	}

	// Later entries see the class an earlier entry assigned.
	classless := pod("")
	require.True(t, mutate(classless, chain...))

	name, _, err := unstructured.NestedString(classless.Object, "spec", "runtimeClassName")
	require.NoError(t, err)
	require.Equal(t, classSecondDefault, name)

	observed, _, err := unstructured.NestedString(classless.Object, "spec", "nodeSelector", "observed")
	require.NoError(t, err)
	require.Equal(t, "yes", observed)

	// A Pod that names a class is left alone by the merge and by its conditions.
	explicit := pod(classCustom)
	before := explicit.DeepCopy()
	require.False(t, mutate(explicit, chain...))
	require.Equal(t, before, explicit)

	// A condition can single out the Pods without a class. The overhead of the
	// replaced class is dropped once every entry has run.
	replaced := pod(classCustom)
	require.True(t, mutate(replaced,
		rules.NamespaceRuleMutation{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `!has(object.spec.runtimeClassName)`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: "skipped"}}},
		runtimeClassMutation(rules.MutationActionReplace, classTenantDefault),
		observe(`object.spec.runtimeClassName == 'tenant-default' && has(object.spec.overhead)`),
	))

	name, _, err = unstructured.NestedString(replaced.Object, "spec", "runtimeClassName")
	require.NoError(t, err)
	require.Equal(t, classTenantDefault, name)

	_, found, err := unstructured.NestedMap(replaced.Object, "spec", "overhead")
	require.NoError(t, err)
	require.False(t, found)

	selector, _, err := unstructured.NestedStringMap(replaced.Object, "spec", "nodeSelector")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", "runtime": classCustom, "observed": "yes"}, selector)
}

// One entry can assign both classes. Only the priority class is read.
func TestRuntimeAndPriorityClassMutationInOneEntry(t *testing.T) {
	mutation := func(priority string) []*rules.NamespaceRuleBodyNamespace {
		return []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: priority, Runtime: "sandboxed"}}}}}}
	}

	classes := priorityClassFixture(t)
	pod := admittedPod("")

	changed, err := MutatePodPlacement(t.Context(), pod, mutation(classTenantDefault), nil, classes)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, classTenantDefault, pod.Spec.PriorityClassName)
	require.Equal(t, "sandboxed", runtimeClassOf(pod))
	require.EqualValues(t, 1, classes.gets.Load(), "only the priority class is read")

	// A priority class that cannot be resolved fails the entry before its runtime class is assigned.
	pod = admittedPod("")
	original := pod.DeepCopy()

	changed, err = MutatePodPlacement(t.Context(), pod, mutation("absent"), nil, priorityClassFixture(t))
	require.ErrorContains(t, err, `rules[0].mutate[0].workloads.classes.priority: PriorityClass "absent" does not exist`)
	require.False(t, changed)
	require.Equal(t, original, pod)
}

func TestRuntimeClassMutationHandlerScope(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}}}
	h := MetadataRules(nil)

	for _, tc := range []struct {
		name, group, kind, subresource string
		operation                      admissionv1.Operation
		want                           bool
	}{
		{name: "pod create", kind: "Pod", operation: admissionv1.Create, want: true},
		{name: "pod update", kind: "Pod", operation: admissionv1.Update},
		{name: "pod delete", kind: "Pod", operation: admissionv1.Delete},
		{name: "pod subresource", kind: "Pod", subresource: "status", operation: admissionv1.Create},
		{name: "ephemeral containers", kind: "Pod", subresource: "ephemeralcontainers", operation: admissionv1.Update},
		{name: "deployment create", group: "apps", kind: "Deployment", operation: admissionv1.Create},
		{name: "service create", kind: "Service", operation: admissionv1.Create},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readers := noReads{tb: t}

			spec := map[string]any{"runtimeClassName": classCustom, "overhead": map[string]any{"cpu": "250m"}, "containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}
			if tc.kind == "Deployment" {
				spec = map[string]any{"template": map[string]any{"spec": spec}}
			}

			apiVersion := "v1"
			if tc.group != "" {
				apiVersion = tc.group + "/v1"
			}

			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": tc.kind, "spec": spec}}
			before := obj.DeepCopy()
			raw, err := json.Marshal(obj)
			require.NoError(t, err)

			req := admission.Request{Kind: metav1.GroupVersionKind{Group: tc.group, Version: "v1", Kind: tc.kind}, Operation: tc.operation, SubResource: tc.subresource, Object: runtime.RawExtension{Raw: raw}}

			var response *admission.Response

			switch tc.operation {
			case admissionv1.Create:
				response = h.OnCreate(readers, readers, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Update:
				response = h.OnUpdate(readers, readers, before, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Delete:
				response = h.OnDelete(readers, readers, obj, nil, nil, nil, bodies)(t.Context(), req)
			}

			if !tc.want {
				require.Nil(t, response)
				require.Equal(t, before, obj)

				return
			}

			require.NotNil(t, response)
			require.True(t, response.Allowed)

			patched := map[string]string{}
			for _, patch := range response.Patches {
				patched[patch.Path] = fmt.Sprintf("%s %v", patch.Operation, patch.Value)
			}

			require.Equal(t, "replace "+classTenantDefault, patched["/spec/runtimeClassName"])
			require.Equal(t, "remove <nil>", patched["/spec/overhead"])
		})
	}

	// A Pod without a class gets one, and nothing else changes.
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)

	readers := noReads{tb: t}
	req := admission.Request{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}

	response := h.OnCreate(readers, readers, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault)}}})(t.Context(), req)
	require.NotNil(t, response)
	require.True(t, response.Allowed)

	patched := map[string]string{}
	for _, patch := range response.Patches {
		patched[patch.Path] = fmt.Sprintf("%s %v", patch.Operation, patch.Value)
	}

	require.Equal(t, "add "+classTenantDefault, patched["/spec/runtimeClassName"])
	require.NotContains(t, patched, "/spec/overhead", "a Pod without an overhead needs no patch for it")
	require.NotContains(t, patched, "/spec/nodeSelector")
	require.NotContains(t, patched, "/spec/tolerations")
}

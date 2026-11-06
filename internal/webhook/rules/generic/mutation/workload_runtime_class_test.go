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
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

const classSecondDefault = "second-default"

// noReads fails the test on any read: paths that assign no runtime class must
// stay read-free.
type noReads struct {
	client.Client
	tb testing.TB
}

func (r noReads) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	r.tb.Helper()
	r.tb.Error("this runtime class mutation must not read any object")

	return errors.New("unexpected read")
}

func (r noReads) List(context.Context, client.ObjectList, ...client.ListOption) error {
	r.tb.Helper()
	r.tb.Error("this runtime class mutation must not list any object")

	return errors.New("unexpected list")
}

// runtimeClassObject is a fixture class. Every class schedules through a key
// of its own, so one never conflicts with another; second-default carries
// scheduling constraints and no overhead, the class the reconciliation exists
// for, and sandboxed carries neither.
func runtimeClassObject(name string) *nodev1.RuntimeClass {
	class := &nodev1.RuntimeClass{Name: name, Handler: "runc"}
	if name == "sandboxed" {
		return class
	}

	class.Scheduling = &nodev1.Scheduling{
		NodeSelector: map[string]string{"runtime-" + name: "true"},
		Tolerations:  []corev1.Toleration{{Key: "runtime-" + name, Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule}},
	}

	switch name {
	case classSecondDefault:
	case classCustom:
		class.Overhead = &nodev1.Overhead{PodFixed: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}}
	default:
		class.Overhead = &nodev1.Overhead{PodFixed: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}}
	}

	return class
}

func runtimeClassFixture(tb testing.TB) *priorityClasses {
	tb.Helper()

	return priorityClassFixtureOf(tb,
		runtimeClassObject(classTenantDefault),
		runtimeClassObject(classSecondDefault),
		runtimeClassObject(classCustom),
		runtimeClassObject("sandboxed"),
	)
}

// admitRuntimeClass merges the overhead and the scheduling of the class, as
// the built-in RuntimeClass plugin does before webhooks run and as the webhook
// itself does for the class it assigns.
func admitRuntimeClass(pod *corev1.Pod, class *nodev1.RuntimeClass) {
	if class.Overhead != nil {
		pod.Spec.Overhead = class.Overhead.PodFixed.DeepCopy()
	}

	if class.Scheduling == nil {
		return
	}

	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = map[string]string{}
	}

	maps.Copy(pod.Spec.NodeSelector, class.Scheduling.NodeSelector)
	pod.Spec.Tolerations = append(pod.Spec.Tolerations, class.Scheduling.Tolerations...)
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
		admitRuntimeClass(pod, runtimeClassObject(class))
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
					classes := runtimeClassFixture(t)
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
						changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, classes)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && assigned, changed)

						if !assigned {
							require.Equal(t, original.Spec, pod.Spec)

							continue
						}

						// The webhook carries the overhead and the scheduling of the class it
						// assigns; what was merged for the replaced class stays on the Pod.
						expected := original.DeepCopy()
						expected.Spec.RuntimeClassName = new(configured)
						admitRuntimeClass(expected, runtimeClassObject(configured))

						require.Equal(t, expected.Spec, pod.Spec)
					}

					var reads int64
					if assigned {
						reads = 1 // the assigned class, on the call that changes it
					}

					require.EqualValues(t, reads, classes.gets.Load())
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

			_, err := MutatePodPlacement(t.Context(), pod, bodies, nil, runtimeClassFixture(t))
			require.NoError(t, err)
			require.Equal(t, tc.want, runtimeClassOf(pod))
		})
	}
}

// The webhook applies the overhead and the scheduling of the class the entries
// end at, as the built-in RuntimeClass plugin does for the class a Pod names.
// The plugin does not run after a class assigned during the webhook's own
// reinvocation, so the webhook cannot leave that work to it.
func TestRuntimeClassMutationDerivesClass(t *testing.T) {
	supplied := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}

	for _, tc := range []struct {
		name     string
		input    string
		supplied bool
		action   rules.MutationAction
		class    string
		assigned bool
	}{
		{name: "replace derives the overhead and scheduling of the replacement", input: classCustom, action: rules.MutationActionReplace, class: classTenantDefault, assigned: true},
		{name: "replace with a scheduling-only class carries its constraints and clears the overhead", input: classCustom, action: rules.MutationActionReplace, class: classSecondDefault, assigned: true},
		{name: "replace with a bare class clears the overhead and merges nothing", input: classCustom, action: rules.MutationActionReplace, class: "sandboxed", assigned: true},
		{name: "replace leaves the class it confirms alone", input: classTenantDefault, action: rules.MutationActionReplace, class: classTenantDefault},
		{name: "merge fills an omitted class and derives it", action: rules.MutationActionMerge, class: classTenantDefault, assigned: true},
		{name: "merge preserves the class the Pod names", input: classCustom, action: rules.MutationActionMerge, class: classTenantDefault},
		{name: "merge replaces an overhead supplied without a class", supplied: true, action: rules.MutationActionMerge, class: classTenantDefault, assigned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			classes := runtimeClassFixture(t)
			pod := admittedRuntimePod(tc.input)
			if tc.supplied {
				pod.Spec.Overhead = supplied.DeepCopy()
			}

			original := pod.DeepCopy()

			changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(tc.action, tc.class)}}}, nil, classes)
			require.NoError(t, err)
			require.Equal(t, tc.assigned, changed)

			if !tc.assigned {
				require.Equal(t, original.Spec, pod.Spec)
				require.Zero(t, classes.gets.Load(), "an unchanged class is not read")

				return
			}

			expected := original.DeepCopy()
			expected.Spec.RuntimeClassName = new(tc.class)
			expected.Spec.Overhead = nil
			admitRuntimeClass(expected, runtimeClassObject(tc.class))

			require.Equal(t, expected.Spec, pod.Spec)
			require.EqualValues(t, 1, classes.gets.Load())
		})
	}
}

func TestRuntimeClassMutationReconcileErrors(t *testing.T) {
	conflicting := &nodev1.RuntimeClass{Name: "conflicting", Handler: "runc",
		Scheduling: &nodev1.Scheduling{NodeSelector: map[string]string{"keep": "no"}}}
	replace := func(class string) []*rules.NamespaceRuleBodyNamespace {
		return []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, class)}}}
	}

	for _, tc := range []struct {
		name    string
		class   string
		readers []client.Reader
		message string
	}{
		{name: "a class that does not exist rejects the Pod", class: "absent", readers: []client.Reader{runtimeClassFixture(t)},
			message: `workloads.classes.runtime: RuntimeClass "absent" not found`},
		{name: "a node selector that conflicts with the Pod rejects it", class: "conflicting", readers: []client.Reader{priorityClassFixtureOf(t, conflicting)},
			message: "workloads.classes.runtime: conflict: runtimeClass.scheduling.nodeSelector[keep] = no; pod.spec.nodeSelector[keep] = yes"},
		{name: "an assignment without a reader is a server error", class: classTenantDefault,
			message: "workloads.classes.runtime: RuntimeClass lookup is unavailable"},
		{name: "a failed read is a server error", class: classTenantDefault, readers: []client.Reader{&priorityClasses{err: errors.New("cache is gone")}},
			message: `workloads.classes.runtime: get RuntimeClass "tenant-default": cache is gone`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := admittedRuntimePod(classCustom)

			changed, err := MutatePodPlacement(t.Context(), pod, replace(tc.class), nil, tc.readers...)
			require.ErrorContains(t, err, tc.message)
			require.False(t, changed)
		})
	}
}

// A class the cache does not know yet, such as one created moments ago, is
// read from the API server.
func TestRuntimeClassMutationFreshFallback(t *testing.T) {
	cached := priorityClassFixtureOf(t)
	fresh := runtimeClassFixture(t)
	pod := admittedRuntimePod("")

	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault)}}}, nil, cached, fresh)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, classTenantDefault, runtimeClassOf(pod))
	require.Equal(t, runtimeClassObject(classTenantDefault).Overhead.PodFixed, pod.Spec.Overhead)
	require.EqualValues(t, 1, cached.gets.Load())
	require.EqualValues(t, 1, fresh.gets.Load())
}

// The first call settles the class, its overhead, and its scheduling, so the
// built-in RuntimeClass plugin finds nothing left to merge. A second call must
// leave the Pod as it is: also when entries replace the class in turn, and
// when another entry changes the Pod.
func TestRuntimeClassMutationReinvocation(t *testing.T) {
	dedicated := rules.NamespaceRuleMutation{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{
		Tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}},
	}}}
	class := runtimeClassObject(classTenantDefault)

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
		// Replace swaps the whole toleration list. On the first call the toleration
		// of the class is merged back for the class the entries assigned; a second
		// call ends at the class the Pod arrived with, so the replace has the last
		// word and a rule has to repeat the toleration to keep it.
		{name: "replaces in turn next to another change", mutations: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault), dedicated}, changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			classes := runtimeClassFixture(t)
			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: tc.mutations}}
			pod := admittedRuntimePod("")

			changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, classes)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, classTenantDefault, runtimeClassOf(pod))
			require.Equal(t, class.Overhead.PodFixed, pod.Spec.Overhead)
			require.Equal(t, "true", pod.Spec.NodeSelector["runtime-"+classTenantDefault])

			changed, err = MutatePodPlacement(t.Context(), pod, bodies, nil, classes)
			require.NoError(t, err)
			require.Equal(t, tc.changed, changed)
			require.Equal(t, classTenantDefault, runtimeClassOf(pod))
			require.Equal(t, class.Overhead.PodFixed, pod.Spec.Overhead, "the overhead of the assigned class must survive the second call")
			require.Equal(t, "true", pod.Spec.NodeSelector["runtime-"+classTenantDefault])

			if tc.changed {
				require.Equal(t, dedicated.Workloads.Placement.Tolerations, pod.Spec.Tolerations)
			} else {
				require.Equal(t, class.Scheduling.Tolerations, pod.Spec.Tolerations)
			}

			require.EqualValues(t, 1, classes.gets.Load(), "the class is read once, on the call that changes it")
		})
	}

	// Entries that end at the class a Pod arrived with leave it alone, and read nothing.
	classes := runtimeClassFixture(t)
	pod := admittedRuntimePod(classTenantDefault)
	original := pod.DeepCopy()

	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		runtimeClassMutation(rules.MutationActionReplace, classSecondDefault), runtimeClassMutation(rules.MutationActionReplace, classTenantDefault),
	}}}, nil, classes)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, original.Spec, pod.Spec)
	require.Zero(t, classes.gets.Load())
}

// Nothing follows the second call. A class that a condition selects only then,
// from what the first call merged into the Pod, is assigned after the built-in
// RuntimeClass plugin's last pass: the webhook itself carries the overhead and
// the scheduling of that class, or a scheduling-only replacement would pass
// validation without its node selector and tolerations and the Pod could be
// scheduled onto incompatible nodes.
func TestRuntimeClassMutationAssignedOnReinvocation(t *testing.T) {
	classes := runtimeClassFixture(t)
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		runtimeClassMutation("", classTenantDefault),
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `'runtime-tenant-default' in object.spec.nodeSelector`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: classSecondDefault}}},
	}}}
	mutate := func(pod *corev1.Pod) (*corev1.Pod, bool) {
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
		require.NoError(t, err)

		obj := &unstructured.Unstructured{Object: object}
		obj.SetAPIVersion("v1")
		obj.SetKind("Pod")

		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t), classes)
		require.NoError(t, err)

		mutated := &corev1.Pod{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, mutated))

		return mutated, changed
	}

	// First call: the scheduling of the assigned class is merged only after the
	// entries have run, so nothing on the Pod matches the condition yet.
	first, changed := mutate(admittedRuntimePod(""))
	require.True(t, changed)
	require.Equal(t, classTenantDefault, runtimeClassOf(first))
	require.Equal(t, runtimeClassObject(classTenantDefault).Overhead.PodFixed, first.Spec.Overhead)

	// The built-in plugin finds the Pod consistent and adds nothing; another
	// webhook's change still reinvokes this one, and the condition now matches.
	second, changed := mutate(first)
	require.True(t, changed)
	require.Equal(t, classSecondDefault, runtimeClassOf(second))

	scheduling := runtimeClassObject(classSecondDefault).Scheduling
	require.Nil(t, second.Spec.Overhead, "a scheduling-only class defines no overhead")
	require.Equal(t, "true", second.Spec.NodeSelector["runtime-"+classSecondDefault], "the node selector of a class assigned on reinvocation must not be lost")
	require.Subset(t, second.Spec.Tolerations, scheduling.Tolerations, "the tolerations of a class assigned on reinvocation must not be lost")
	// What was merged for the replaced class stays on the Pod.
	require.Equal(t, "true", second.Spec.NodeSelector["runtime-"+classTenantDefault])
	require.EqualValues(t, 2, classes.gets.Load(), "each call reads the class it assigns")
}

// The toleration merge keeps the superset, as the plugin does: the class the
// webhook assigns must never narrow a broader toleration the Pod carries, such
// as an unlimited NoExecute toleration against one the class limits in time,
// which would let the Pod be evicted earlier.
func TestRuntimeClassMutationPreservesBroaderTolerations(t *testing.T) {
	limited := func(seconds int64) corev1.Toleration {
		return corev1.Toleration{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: &seconds}
	}
	unlimited := corev1.Toleration{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}
	exists := corev1.Toleration{Key: "dedicated", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}
	other := corev1.Toleration{Key: "other", Operator: corev1.TolerationOpExists}

	for _, tc := range []struct {
		name   string
		pod    []corev1.Toleration
		class  []corev1.Toleration
		merged []corev1.Toleration
	}{
		{name: "an unlimited toleration of the Pod is not limited in time", pod: []corev1.Toleration{unlimited}, class: []corev1.Toleration{limited(60)}, merged: []corev1.Toleration{unlimited}},
		{name: "a longer toleration of the Pod is kept over a shorter one", pod: []corev1.Toleration{limited(600)}, class: []corev1.Toleration{limited(60)}, merged: []corev1.Toleration{limited(600)}},
		{name: "a broader toleration of the class subsumes the Pod's", pod: []corev1.Toleration{limited(60)}, class: []corev1.Toleration{unlimited}, merged: []corev1.Toleration{unlimited}},
		{name: "an identical toleration is kept once", pod: []corev1.Toleration{unlimited}, class: []corev1.Toleration{unlimited}, merged: []corev1.Toleration{unlimited}},
		{name: "an Exists toleration of the Pod covers the class's", pod: []corev1.Toleration{exists}, class: []corev1.Toleration{limited(60)}, merged: []corev1.Toleration{exists}},
		{name: "a class toleration for every effect subsumes the Pod's", pod: []corev1.Toleration{exists}, class: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}, merged: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}},
		{name: "unrelated tolerations stay next to the merged one", pod: []corev1.Toleration{other}, class: []corev1.Toleration{limited(60)}, merged: []corev1.Toleration{other, limited(60)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			classes := priorityClassFixtureOf(t, &nodev1.RuntimeClass{Name: "evicting", Handler: "runc",
				Scheduling: &nodev1.Scheduling{Tolerations: tc.class}})
			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, "evicting")}}}

			pod := admittedRuntimePod("")
			pod.Spec.Tolerations = tc.pod

			changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, classes)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "evicting", runtimeClassOf(pod))
			require.Equal(t, tc.merged, pod.Spec.Tolerations)

			// The settled Pod gives a reinvocation nothing to change or read.
			changed, err = MutatePodPlacement(t.Context(), pod, bodies, nil, noReads{tb: t})
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, tc.merged, pod.Spec.Tolerations)
		})
	}
}

func TestRuntimeClassMutationDoesNotAliasTheRule(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classTenantDefault)}}}
	first, second := admittedRuntimePod(""), admittedRuntimePod(classCustom)

	for _, pod := range []*corev1.Pod{first, second} {
		changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, runtimeClassFixture(t))
		require.NoError(t, err)
		require.True(t, changed)
	}

	require.NotSame(t, first.Spec.RuntimeClassName, second.Spec.RuntimeClassName)

	*first.Spec.RuntimeClassName = "changed"
	first.Spec.Overhead[corev1.ResourceCPU] = resource.MustParse("1")

	require.Equal(t, classTenantDefault, *second.Spec.RuntimeClassName)
	require.Equal(t, runtimeClassObject(classTenantDefault).Overhead.PodFixed, second.Spec.Overhead)
	require.Equal(t, classTenantDefault, bodies[0].Mutate[0].Workloads.Classes.Runtime, "rules are cache-owned and must not change")
}

func TestRuntimeClassMutationConditions(t *testing.T) {
	classes := runtimeClassFixture(t)
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

		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t), classes)
		require.NoError(t, err)

		// A second admission of the result changes nothing.
		again, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t), classes)
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

	// Later entries see the class an earlier entry assigned. Only the class the
	// entries end at is derived: its scheduling is merged, and it has no overhead.
	classless := pod("")
	require.True(t, mutate(classless, chain...))

	name, _, err := unstructured.NestedString(classless.Object, "spec", "runtimeClassName")
	require.NoError(t, err)
	require.Equal(t, classSecondDefault, name)

	observed, _, err := unstructured.NestedString(classless.Object, "spec", "nodeSelector", "observed")
	require.NoError(t, err)
	require.Equal(t, "yes", observed)

	_, found, err := unstructured.NestedMap(classless.Object, "spec", "overhead")
	require.NoError(t, err)
	require.False(t, found, "a scheduling-only class defines no overhead")

	selector, _, err := unstructured.NestedStringMap(classless.Object, "spec", "nodeSelector")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", "observed": "yes", "runtime-" + classSecondDefault: "true"}, selector)

	// A Pod that names a class is left alone by the merge and by its conditions.
	explicit := pod(classCustom)
	before := explicit.DeepCopy()
	require.False(t, mutate(explicit, chain...))
	require.Equal(t, before, explicit)

	// A condition can single out the Pods without a class. The overhead of the
	// replaced class makes way for the one of the assigned class.
	replaced := pod(classCustom)
	require.True(t, mutate(replaced,
		rules.NamespaceRuleMutation{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `!has(object.spec.runtimeClassName)`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: "skipped"}}},
		runtimeClassMutation(rules.MutationActionReplace, classTenantDefault),
		observe(`object.spec.runtimeClassName == 'tenant-default' && has(object.spec.overhead)`),
	))

	name, _, err = unstructured.NestedString(replaced.Object, "spec", "runtimeClassName")
	require.NoError(t, err)
	require.Equal(t, classTenantDefault, name)

	overhead, found, err := unstructured.NestedMap(replaced.Object, "spec", "overhead")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, map[string]any{"cpu": "500m"}, overhead)

	selector, _, err = unstructured.NestedStringMap(replaced.Object, "spec", "nodeSelector")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", "runtime-" + classCustom: "true", "observed": "yes", "runtime-" + classTenantDefault: "true"}, selector)
}

// One entry can assign both classes; each is read once.
func TestRuntimeAndPriorityClassMutationInOneEntry(t *testing.T) {
	mutation := func(priority string) []*rules.NamespaceRuleBodyNamespace {
		return []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: priority, Runtime: "sandboxed"}}}}}}
	}
	fixture := func() *priorityClasses {
		return priorityClassFixtureOf(t,
			&schedulingv1.PriorityClass{Name: classTenantDefault, Value: 1000, PreemptionPolicy: new(corev1.PreemptNever)},
			runtimeClassObject("sandboxed"),
		)
	}

	classes := fixture()
	pod := admittedPod("")

	changed, err := MutatePodPlacement(t.Context(), pod, mutation(classTenantDefault), nil, classes)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, classTenantDefault, pod.Spec.PriorityClassName)
	require.Equal(t, "sandboxed", runtimeClassOf(pod))
	require.EqualValues(t, 2, classes.gets.Load(), "each class is read once")

	// A priority class that cannot be resolved fails the entry before its runtime class is assigned.
	pod = admittedPod("")
	original := pod.DeepCopy()

	changed, err = MutatePodPlacement(t.Context(), pod, mutation("absent"), nil, fixture())
	require.ErrorContains(t, err, `rules[0].mutate[0].workloads.classes.priority: PriorityClass "absent" does not exist`)
	require.False(t, changed)
	require.Equal(t, original, pod)
}

func TestRuntimeClassMutationHandlerScope(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation(rules.MutationActionReplace, classSecondDefault)}}}
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
			// Only the create that assigns the class reads it; every other request
			// must stay read-free.
			var readers client.Client = noReads{tb: t}
			if tc.want {
				readers = runtimeClassFixture(t)
			}

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

			require.Equal(t, "replace "+classSecondDefault, patched["/spec/runtimeClassName"])
			require.Equal(t, "remove <nil>", patched["/spec/overhead"], "a scheduling-only class defines no overhead")
			require.Contains(t, patched, "/spec/nodeSelector", "the node selector of the assigned class is carried by the webhook")
			require.Contains(t, patched, "/spec/tolerations", "the tolerations of the assigned class are carried by the webhook")
		})
	}

	// A Pod without a class gets one, together with what the class defines.
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)

	readers := runtimeClassFixture(t)
	req := admission.Request{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}

	response := h.OnCreate(readers, readers, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{runtimeClassMutation("", classTenantDefault)}}})(t.Context(), req)
	require.NotNil(t, response)
	require.True(t, response.Allowed)

	patched := map[string]string{}
	for _, patch := range response.Patches {
		patched[patch.Path] = fmt.Sprintf("%s %v", patch.Operation, patch.Value)
	}

	require.Equal(t, "add "+classTenantDefault, patched["/spec/runtimeClassName"])
	require.Contains(t, patched, "/spec/overhead", "the webhook carries the overhead of the class it assigns")
	require.Contains(t, patched, "/spec/nodeSelector")
	require.Contains(t, patched, "/spec/tolerations")
	require.EqualValues(t, 1, readers.gets.Load())
}

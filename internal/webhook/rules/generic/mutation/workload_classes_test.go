// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

const (
	classTenantDefault = "tenant-default"
	classGlobalDefault = "cluster-default"
	classCustom        = "custom"
)

// priorityClasses counts the PriorityClass reads of a mutation.
type priorityClasses struct {
	client.Client
	gets atomic.Int64
	err  error
}

func (c *priorityClasses) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)

	if c.err != nil {
		return c.err
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

func priorityClassFixtureOf(tb testing.TB, classes ...client.Object) *priorityClasses {
	tb.Helper()

	scheme := runtime.NewScheme()
	require.NoError(tb, schedulingv1.AddToScheme(scheme))

	return &priorityClasses{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(classes...).Build()}
}

func priorityClassFixture(tb testing.TB) *priorityClasses {
	tb.Helper()

	return priorityClassFixtureOf(tb,
		&schedulingv1.PriorityClass{Name: classTenantDefault, Value: 1000, PreemptionPolicy: new(corev1.PreemptNever)},
		&schedulingv1.PriorityClass{Name: classGlobalDefault, Value: 10, GlobalDefault: true, PreemptionPolicy: new(corev1.PreemptLowerPriority)},
		&schedulingv1.PriorityClass{Name: classCustom, Value: 500, PreemptionPolicy: new(corev1.PreemptLowerPriority)},
		&schedulingv1.PriorityClass{Name: "second-default", Value: 2000},
	)
}

// admittedPod is a Pod as the built-in Priority plugin hands it to webhooks.
func admittedPod(class string) *corev1.Pod {
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		PriorityClassName: class,
		Priority:          new(int32(0)),
		PreemptionPolicy:  new(corev1.PreemptLowerPriority),
		NodeSelector:      map[string]string{"keep": "yes"},
		Containers:        []corev1.Container{{Name: "app", Image: "example.com/app:v1"}},
	}}

	switch class {
	case classGlobalDefault:
		pod.Spec.Priority = new(int32(10))
	case classCustom:
		pod.Spec.Priority = new(int32(500))
	case classTenantDefault:
		pod.Spec.Priority, pod.Spec.PreemptionPolicy = new(int32(1000)), new(corev1.PreemptNever)
	}

	return pod
}

func priorityClassMutation(action rules.MutationAction, class string) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: class}}}
}

func TestPriorityClassMutationActions(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, input := range []string{"", classGlobalDefault, classCustom, classTenantDefault} {
			for _, configured := range []string{"", classTenantDefault} {
				t.Run(fmt.Sprintf("%s/%s-to-%s", action, input, configured), func(t *testing.T) {
					classes := priorityClassFixture(t)
					pod := admittedPod(input)
					original := pod.DeepCopy()

					mutation := priorityClassMutation(action, configured)
					if configured == "" {
						// A mutation needs a property; this one must leave the class alone.
						mutation.Workloads.Placement.NodeSelector = map[string]string{"keep": "yes"}
					}

					bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}
					before := bodies[0].DeepCopy()

					assigned := configured != "" && input != configured && (action == rules.MutationActionReplace || input == "" || input == classGlobalDefault)

					var reads int64

					switch {
					case configured == "" || input == configured:
					case action == rules.MutationActionReplace || input == "":
						reads = 1 // the configured class
					case input == classGlobalDefault:
						reads = 2 // the Pod's class and the configured class
					default:
						reads = 1 // the Pod's class, which is explicit
					}

					for pass := range 2 {
						changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, classes)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && assigned, changed)

						if assigned {
							require.Equal(t, classTenantDefault, pod.Spec.PriorityClassName)
							require.EqualValues(t, 1000, *pod.Spec.Priority)
							require.Equal(t, corev1.PreemptNever, *pod.Spec.PreemptionPolicy)
						} else {
							require.Equal(t, original.Spec, pod.Spec)
						}

						if pass == 0 {
							require.Equal(t, reads, classes.gets.Load(), "PriorityClass reads")
						}
					}

					require.Equal(t, map[string]string{"keep": "yes"}, pod.Spec.NodeSelector)
					require.Equal(t, before, bodies[0])
				})
			}
		}
	}
}

func TestPriorityClassMutationOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		mutations []rules.NamespaceRuleMutation
		want      string
	}{
		{name: "first applicable merge wins", mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classTenantDefault), priorityClassMutation("", "second-default")}, want: classTenantDefault},
		{name: "a merge that picks the global default still wins", mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classGlobalDefault), priorityClassMutation("", classTenantDefault)}, want: classGlobalDefault},
		{name: "a confirmed global default is not omitted for later merges", input: classGlobalDefault, mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classGlobalDefault), priorityClassMutation("", classTenantDefault)}, want: classGlobalDefault},
		{name: "merge does not undo a replace", input: classCustom, mutations: []rules.NamespaceRuleMutation{priorityClassMutation(rules.MutationActionReplace, classGlobalDefault), priorityClassMutation("", classTenantDefault)}, want: classGlobalDefault},
		{name: "replace overrides a merge", mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classTenantDefault), priorityClassMutation(rules.MutationActionReplace, "second-default")}, want: "second-default"},
		{name: "last replace wins", input: classCustom, mutations: []rules.NamespaceRuleMutation{priorityClassMutation(rules.MutationActionReplace, classTenantDefault), priorityClassMutation(rules.MutationActionReplace, "second-default")}, want: "second-default"},
		{name: "explicit class survives every merge", input: classCustom, mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classTenantDefault), priorityClassMutation("", "second-default")}, want: classCustom},
		{name: "unknown class is explicit", input: "not-cached-yet", mutations: []rules.NamespaceRuleMutation{priorityClassMutation("", classTenantDefault)}, want: "not-cached-yet"},
		{name: "container targets do not select the pod class", mutations: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{
			Targets:    []rules.WorkloadValidationTarget{rules.ValidateContainers},
			Classes:    rules.WorkloadClassMutation{Priority: classTenantDefault},
			Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullAlways},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := admittedPod(tc.input)
			bodies := []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: tc.mutations[:1]}, {Mutate: tc.mutations[1:]}}

			_, err := MutatePodPlacement(t.Context(), pod, bodies, nil, priorityClassFixture(t))
			require.NoError(t, err)
			require.Equal(t, tc.want, pod.Spec.PriorityClassName)
		})
	}

	// A class without a preemption policy clears the one resolved for the old class.
	pod := admittedPod("")
	_, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{priorityClassMutation("", "second-default")}}}, nil, priorityClassFixture(t))
	require.NoError(t, err)
	require.EqualValues(t, 2000, *pod.Spec.Priority)
	require.Nil(t, pod.Spec.PreemptionPolicy)
}

func TestPriorityClassMutationConditions(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{
		priorityClassMutation("", classTenantDefault),
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `object.spec.priorityClassName == 'tenant-default' && object.spec.priority == 1000`}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: "second-default"}}},
		{Conditions: []rules.AdmissionCondition{{Expression: `object.spec.priorityClassName == 'second-default'`}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"observed": "yes"}}}},
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: classCustom}}},
	}}}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(admittedPod(""))
	require.NoError(t, err)

	obj := &unstructured.Unstructured{Object: object}
	obj.SetAPIVersion("v1")
	obj.SetKind("Pod")

	classes := priorityClassFixture(t)

	for pass := range 2 {
		changed, err := MutateWorkloadResources(t.Context(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), bodies, mutationConditions(t), classes)
		require.NoError(t, err)
		require.Equal(t, pass == 0, changed)
	}

	name, _, err := unstructured.NestedString(obj.Object, "spec", "priorityClassName")
	require.NoError(t, err)
	require.Equal(t, "second-default", name)

	priority, _, err := unstructured.NestedInt64(obj.Object, "spec", "priority")
	require.NoError(t, err)
	require.EqualValues(t, 2000, priority)

	_, found, err := unstructured.NestedString(obj.Object, "spec", "preemptionPolicy")
	require.NoError(t, err)
	require.False(t, found)

	observed, _, err := unstructured.NestedString(obj.Object, "spec", "nodeSelector", "observed")
	require.NoError(t, err)
	require.Equal(t, "yes", observed)
}

func TestPriorityClassMutationFailures(t *testing.T) {
	unavailable := priorityClassFixture(t)
	unavailable.err = errors.New("cache unavailable")

	for _, tc := range []struct {
		name     string
		input    string
		mutation rules.NamespaceRuleMutation
		classes  []client.Reader
		want     string
	}{
		{name: "missing class", mutation: priorityClassMutation("", "absent"), classes: []client.Reader{priorityClassFixture(t)}, want: `rules[1].mutate[0].workloads.classes.priority: PriorityClass "absent" does not exist`},
		{name: "missing class on replace", input: classCustom, mutation: priorityClassMutation(rules.MutationActionReplace, "absent"), classes: []client.Reader{priorityClassFixture(t)}, want: `PriorityClass "absent" does not exist`},
		{name: "failed read of the configured class", mutation: priorityClassMutation("", classTenantDefault), classes: []client.Reader{unavailable}, want: `get PriorityClass "tenant-default": cache unavailable`},
		{name: "failed read of the pod class", input: classCustom, mutation: priorityClassMutation("", classTenantDefault), classes: []client.Reader{unavailable}, want: `get PriorityClass "custom": cache unavailable`},
		{name: "no reader", mutation: priorityClassMutation("", classTenantDefault), want: "PriorityClass lookup is unavailable"},
		{name: "nil reader", mutation: priorityClassMutation(rules.MutationActionReplace, classTenantDefault), classes: []client.Reader{nil}, want: "PriorityClass lookup is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := admittedPod(tc.input)
			original := pod.DeepCopy()

			changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: []rules.NamespaceRuleMutation{tc.mutation}}}, nil, tc.classes...)
			require.ErrorContains(t, err, tc.want)
			require.False(t, changed)
			require.Equal(t, original, pod, "a failed mutation must not leave a partial class")
		})
	}

	// Mutations without a class never need the reader.
	pod := admittedPod("")
	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"pool": "shared"}}}}}}}, nil)
	require.NoError(t, err)
	require.True(t, changed)
}

// The cache answers every lookup it can. Only a Pod class it does not know yet,
// which the API server has already resolved, is read from the API server.
func TestPriorityClassMutationCacheStates(t *testing.T) {
	tenantDefault := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: classTenantDefault}, Value: 1000}
	global := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: classGlobalDefault}, Value: 10, GlobalDefault: true}
	demoted := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: classGlobalDefault}, Value: 10}

	for _, tc := range []struct {
		name          string
		action        rules.MutationAction
		cached, fresh []client.Object
		noFresh       bool
		freshErr      error
		want          string
		wantErr       string
		freshReads    int64
	}{
		{name: "cache knows the global default", cached: []client.Object{tenantDefault, global}, fresh: []client.Object{tenantDefault, global}, want: classTenantDefault},
		{name: "cache is behind a new global default", cached: []client.Object{tenantDefault}, fresh: []client.Object{tenantDefault, global}, want: classTenantDefault, freshReads: 1},
		{name: "cache is behind an explicit class", cached: []client.Object{tenantDefault}, fresh: []client.Object{tenantDefault, demoted}, want: classGlobalDefault, freshReads: 1},
		{name: "class unknown everywhere is explicit", cached: []client.Object{tenantDefault}, fresh: []client.Object{tenantDefault}, want: classGlobalDefault, freshReads: 1},
		{name: "cache hit is not read again", cached: []client.Object{tenantDefault, demoted}, fresh: []client.Object{tenantDefault, global}, want: classGlobalDefault},
		{name: "no API reader keeps an unknown class", cached: []client.Object{tenantDefault}, noFresh: true, want: classGlobalDefault},
		{name: "failed API read", cached: []client.Object{tenantDefault}, freshErr: errors.New("apiserver unavailable"), wantErr: `get PriorityClass "cluster-default": apiserver unavailable`, want: classGlobalDefault, freshReads: 1},
		{name: "configured class is only read from the cache", cached: []client.Object{global}, fresh: []client.Object{tenantDefault, global}, wantErr: `PriorityClass "tenant-default" does not exist`, want: classGlobalDefault},
		{name: "replace never reads the Pod class", action: rules.MutationActionReplace, cached: []client.Object{tenantDefault}, fresh: []client.Object{tenantDefault, global}, want: classTenantDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cached, fresh := priorityClassFixtureOf(t, tc.cached...), priorityClassFixtureOf(t, tc.fresh...)
			fresh.err = tc.freshErr

			readers := []client.Reader{cached, fresh}
			if tc.noFresh {
				readers = readers[:1]
			}

			pod := admittedPod(classGlobalDefault)
			bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{priorityClassMutation(tc.action, classTenantDefault)}}}

			changed, err := MutatePodPlacement(t.Context(), pod, bodies, nil, readers...)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}

			require.Equal(t, tc.want != classGlobalDefault, changed)
			require.Equal(t, tc.want, pod.Spec.PriorityClassName)
			require.Equal(t, tc.freshReads, fresh.gets.Load(), "API reads")
		})
	}
}

// The handler hands the cache and the API reader of the request to the mutation.
func TestPriorityClassMutationHandlerUsesTheAPIReaderOnCacheMiss(t *testing.T) {
	tenantDefault := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: classTenantDefault}, Value: 1000}
	global := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: classGlobalDefault}, Value: 10, GlobalDefault: true}
	cached, fresh := priorityClassFixtureOf(t, tenantDefault), priorityClassFixtureOf(t, tenantDefault, global)

	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{priorityClassMutation("", classTenantDefault)}}}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"priorityClassName": classGlobalDefault, "priority": int64(10), "containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)

	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}}
	response := MetadataRules(nil).OnCreate(cached, fresh, obj, nil, nil, nil, bodies)(t.Context(), req)
	require.NotNil(t, response)
	require.True(t, response.Allowed)
	require.EqualValues(t, 1, fresh.gets.Load())

	name, _, err := unstructured.NestedString(obj.Object, "spec", "priorityClassName")
	require.NoError(t, err)
	require.Equal(t, classTenantDefault, name)
}

func TestPriorityClassMutationDoesNotAliasTheClass(t *testing.T) {
	class := &schedulingv1.PriorityClass{Name: classTenantDefault, Value: 1000, PreemptionPolicy: new(corev1.PreemptNever)}
	pod := admittedPod("")
	state := &podPriorityClass{reader: sharedClass{class: class}}

	require.NoError(t, state.apply(t.Context(), pod, classTenantDefault, false))

	*pod.Spec.Priority = 1
	*pod.Spec.PreemptionPolicy = corev1.PreemptLowerPriority

	require.EqualValues(t, 1000, class.Value)
	require.Equal(t, corev1.PreemptNever, *class.PreemptionPolicy)
}

// sharedClass hands out one object, like a reader that does not copy.
type sharedClass struct {
	client.Reader
	class *schedulingv1.PriorityClass
}

func (s sharedClass) Get(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	*obj.(*schedulingv1.PriorityClass) = *s.class

	return nil
}

func TestPriorityClassMutationHandlerScope(t *testing.T) {
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{priorityClassMutation(rules.MutationActionReplace, classTenantDefault)}}}
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
			classes := priorityClassFixture(t)

			spec := map[string]any{"priorityClassName": classCustom, "priority": int64(500), "containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}
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
				response = h.OnCreate(classes, nil, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Update:
				response = h.OnUpdate(classes, nil, before, obj, nil, nil, nil, bodies)(t.Context(), req)
			case admissionv1.Delete:
				response = h.OnDelete(classes, nil, obj, nil, nil, nil, bodies)(t.Context(), req)
			}

			if !tc.want {
				require.Nil(t, response)
				require.Equal(t, before, obj)
				require.Zero(t, classes.gets.Load())

				return
			}

			require.NotNil(t, response)
			require.True(t, response.Allowed)
			require.EqualValues(t, 1, classes.gets.Load())

			patched := map[string]any{}
			for _, patch := range response.Patches {
				patched[patch.Path] = patch.Value
			}

			require.Equal(t, classTenantDefault, patched["/spec/priorityClassName"])
			require.Equal(t, "1000", fmt.Sprint(patched["/spec/priority"]))
			require.Equal(t, string(corev1.PreemptNever), patched["/spec/preemptionPolicy"])
		})
	}

	// The handler reports a missing class as an error and returns no patch.
	missing := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{priorityClassMutation("", "absent")}}}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)

	req := admission.Request{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}
	response := h.OnCreate(priorityClassFixture(t), nil, obj, nil, nil, nil, missing)(t.Context(), req)
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
	require.Contains(t, response.Result.Message, `PriorityClass "absent" does not exist`)
	require.Empty(t, response.Patches)
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

// classReads counts the class reads of an admission.
type classReads struct {
	client.Reader
	gets atomic.Int64
	err  error
}

func (c *classReads) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)

	if c.err != nil {
		return c.err
	}

	return c.Reader.Get(ctx, key, obj, opts...)
}

type classEvents struct {
	events.EventRecorder
	reasons []string
}

func (r *classEvents) LabeledEvent(obj runtime.Object, typ, reason, action, note string) events.LabeledEvent {
	r.reasons = append(r.reasons, reason)

	return r.EventRecorder.LabeledEvent(obj, typ, reason, action, note)
}

// classKind runs the shared class tests against one kind of class a Pod
// references. Every kind is matched alike; an omitted class is not.
type classKind struct {
	name string
	// set, path and allowed are the terms of admission messages.
	set, path, allowed string
	// object is the kind of the class object, field the Pod spec field.
	object, field string
	forbidden     string
	// missing is the event reason of a controller template without a class. It
	// is empty when an omitted class is never matched.
	missing string
	class   func(name string, labels map[string]string) client.Object
	rule    func(*rules.WorkloadClassEnforcement, []rules.WorkloadClassMatch)
	assign  func(*corev1.Pod, string)
}

var classKinds = []classKind{
	{
		name: "priority", set: "priority class", path: "spec.priorityClassName", allowed: "Allowed priority classes",
		object: "PriorityClass", field: "priorityClassName",
		forbidden: events.ReasonForbiddenPriorityClass, missing: events.ReasonMissingPriorityClass,
		class: func(name string, labels map[string]string) client.Object {
			return &schedulingv1.PriorityClass{Name: name, Labels: labels, Value: 1000}
		},
		rule:   func(c *rules.WorkloadClassEnforcement, matches []rules.WorkloadClassMatch) { c.Priority = matches },
		assign: func(pod *corev1.Pod, class string) { pod.Spec.PriorityClassName = class },
	},
	{
		name: "runtime", set: "runtime class", path: "spec.runtimeClassName", allowed: "Allowed runtime classes",
		object: "RuntimeClass", field: "runtimeClassName",
		forbidden: events.ReasonForbiddenRuntimeClass,
		class: func(name string, labels map[string]string) client.Object {
			return &nodev1.RuntimeClass{Name: name, Labels: labels, Handler: "runc"}
		},
		rule: func(c *rules.WorkloadClassEnforcement, matches []rules.WorkloadClassMatch) { c.Runtime = matches },
		assign: func(pod *corev1.Pod, class string) {
			pod.Spec.RuntimeClassName = nil
			if class != "" {
				pod.Spec.RuntimeClassName = &class
			}
		},
	},
}

func classScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()

	scheme := runtime.NewScheme()
	require.NoError(tb, schedulingv1.AddToScheme(scheme))
	require.NoError(tb, nodev1.AddToScheme(scheme))

	return scheme
}

// fixture holds the classes gold, silver and bronze of this kind.
func (k classKind) fixture(tb testing.TB) *classReads {
	tb.Helper()

	return &classReads{Reader: fake.NewClientBuilder().WithScheme(classScheme(tb)).WithObjects(
		k.class("gold", map[string]string{"env": "customer", "tier": "gold"}),
		k.class("silver", nil),
		k.class("bronze", map[string]string{"env": "internal"}),
	).Build()}
}

func (k classKind) body(action rules.ActionType, classes ...rules.WorkloadClassMatch) *rules.NamespaceRuleBodyNamespace {
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action}}
	k.rule(&body.Enforce.Workloads.Classes, classes)

	return body
}

func (k classKind) pod(class string) *corev1.Pod {
	pod := &corev1.Pod{
		Name: "pod", Namespace: "tenant-a", Labels: map[string]string{"app": "example"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example.com/app:v1"}}},
	}
	k.assign(pod, class)

	return pod
}

func (k classKind) template(t testing.TB, target rules.WorkloadValidationTarget, class string) (*unstructured.Unstructured, admission.Request) {
	t.Helper()

	obj, req := templateFixture(t, target)
	path := append(workloads.PodTemplatePath(obj.GroupVersionKind()), "spec", k.field)

	if class == "" {
		unstructured.RemoveNestedField(obj.Object, path...)
	} else {
		require.NoError(t, unstructured.SetNestedField(obj.Object, class, path...))
	}

	return obj, req
}

// rejected is the message for a class no allow rule lists.
func (k classKind) rejected(class string) string {
	return fmt.Sprintf("%s %q at %s is not allowed by namespace rule", k.set, class, k.path)
}

// denied is the message for a class a deny rule matches.
func (k classKind) denied(class string) string {
	return fmt.Sprintf("%s %q at %s is denied by namespace rule", k.set, class, k.path)
}

func classNames(names ...string) rules.WorkloadClassMatch {
	return rules.WorkloadClassMatch{Exact: names}
}

func classLabels(key, value string) rules.WorkloadClassMatch {
	return rules.WorkloadClassMatch{MatchLabels: map[string]string{key: value}}
}

func TestClassRules(t *testing.T) {
	customer, internal := classLabels("env", "customer"), classLabels("env", "internal")

	for _, kind := range classKinds {
		targeted := func(targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
			body := kind.body(rules.ActionTypeAllow, classNames("gold"))
			body.Enforce.Workloads.Targets = targets

			return body
		}

		for _, tc := range []struct {
			name   string
			class  string
			bodies []*rules.NamespaceRuleBodyNamespace
			denied string
			reads  int64
			events []string
		}{
			{name: "no rules", class: "gold"},
			{name: "unrelated rule", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{QoSClasses: []corev1.PodQOSClass{corev1.PodQOSBestEffort}}}}}},
			{name: "pod without a class is not matched", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, classNames("gold"), customer)}},
			{name: "allow exact", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, classNames("silver", "gold"))}},
			{name: "allow expression", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "^(gold|silver)$"})}},
			{name: "allow selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer)}, reads: 1},
			{name: "allow selector expression", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, rules.WorkloadClassMatch{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"internal"}}}})}, reads: 1},
			{name: "allow list rejects another name", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, classNames("gold"), rules.WorkloadClassMatch{Expression: "^silver$"})},
				denied: kind.rejected("bronze") + ". " + kind.allowed + ": exact: gold, exp: ^silver$", events: []string{kind.forbidden}},
			{name: "allow list rejects other labels", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer)},
				denied: kind.rejected("silver") + ". " + kind.allowed + ": selector: env=customer", reads: 1, events: []string{kind.forbidden}},
			{name: "missing class never matches a selector", class: "ghost", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer)},
				denied: kind.rejected("ghost"), reads: 1, events: []string{kind.forbidden}},
			{name: "name decides before any selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, internal, customer, classNames("gold"))}},
			{name: "criteria within an entry are ORed", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, rules.WorkloadClassMatch{Exact: []string{"gold"}, LabelSelector: internal.LabelSelector})}, reads: 1},
			{name: "several selectors read the class once", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer, internal), kind.body(rules.ActionTypeDeny, classLabels("tier", "gold"))},
				denied: "is not allowed by namespace rule", reads: 1, events: []string{kind.forbidden}},
			{name: "deny exact", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeDeny, classNames("gold"))},
				denied: kind.denied("gold"), events: []string{kind.forbidden}},
			{name: "default action denies", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body("", classNames("gold"))},
				denied: "is denied by namespace rule", events: []string{kind.forbidden}},
			{name: "deny selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeDeny, customer)},
				denied: kind.denied("gold"), reads: 1, events: []string{kind.forbidden}},
			{name: "deny list admits another class", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeDeny, classNames("gold"), customer)}, reads: 1},
			{name: "last matching rule wins: deny", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, classNames("gold")), kind.body(rules.ActionTypeDeny, classNames("gold"))},
				denied: "is denied by namespace rule", events: []string{kind.forbidden}},
			{name: "last matching rule wins: allow", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeDeny, classNames("gold")), kind.body(rules.ActionTypeAllow, classNames("gold"))}},
			{name: "audit records every matching entry and never decides", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAudit, classNames("gold"), customer, internal)}, reads: 1,
				events: []string{events.ReasonNamespaceRuleAudit, events.ReasonNamespaceRuleAudit}},
			{name: "audit does not grant what an allow list rejects", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAudit, classNames("bronze")), kind.body(rules.ActionTypeAllow, classNames("gold"))},
				denied: "is not allowed by namespace rule", events: []string{events.ReasonNamespaceRuleAudit, kind.forbidden}},
			{name: "container targets do not select the pod class", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ValidateContainers)}},
			{name: "controller targets do not select pods", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ValidateDeployment)}},
			{name: "pod target selects the pod class", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ValidateDeployment, rules.ValidatePod)},
				denied: "is not allowed by namespace rule", events: []string{kind.forbidden}},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				reader := kind.fixture(t)
				recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}

				var before []*rules.NamespaceRuleBodyNamespace
				for _, body := range tc.bodies {
					before = append(before, body.DeepCopy())
				}

				pod := kind.pod(tc.class)
				original := pod.DeepCopy()

				response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, pod, nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), admission.Request{})
				if tc.denied == "" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.EqualValues(t, http.StatusForbidden, response.Result.Code)
					require.Contains(t, response.Result.Message, tc.denied)
				}

				require.Equal(t, tc.reads, reader.gets.Load(), "%s reads", kind.object)
				require.Equal(t, tc.events, recorder.reasons)
				require.Equal(t, before, tc.bodies, "rules are cache-owned and must not change")
				require.Equal(t, original, pod)
			})
		}
	}
}

// One rule can carry several kinds of class. Each list judges its own Pod
// field and reads its own class object; neither grants or restricts the other.
func TestClassRulesKeepKindsApart(t *testing.T) {
	selected := rules.WorkloadClassEnforcement{
		Priority: []rules.WorkloadClassMatch{classLabels("env", "customer")},
		Runtime:  []rules.WorkloadClassMatch{classLabels("env", "internal")},
	}

	for _, tc := range []struct {
		name              string
		classes           rules.WorkloadClassEnforcement
		priority, runtime string
		denied            string
		reads             []string
	}{
		{name: "both allowed", classes: selected, priority: "gold", runtime: "bronze", reads: []string{"*v1.PriorityClass", "*v1.RuntimeClass"}},
		// The first rejected class ends the admission, so the other is not read.
		{name: "priority rejected", classes: selected, priority: "bronze", runtime: "bronze", denied: `priority class "bronze" at spec.priorityClassName is not allowed`, reads: []string{"*v1.PriorityClass"}},
		{name: "runtime rejected", classes: selected, priority: "gold", runtime: "gold", denied: `runtime class "gold" at spec.runtimeClassName is not allowed`, reads: []string{"*v1.PriorityClass", "*v1.RuntimeClass"}},
		{name: "omitted classes are not matched", classes: selected},
		{name: "omitted runtime class next to an allowed priority class", classes: selected, priority: "gold", reads: []string{"*v1.PriorityClass"}},
		{name: "omitted priority class next to a rejected runtime class", classes: selected, runtime: "gold", denied: "runtime class", reads: []string{"*v1.RuntimeClass"}},
		{name: "a priority list does not restrict the runtime class", classes: rules.WorkloadClassEnforcement{Priority: []rules.WorkloadClassMatch{classNames("gold")}}, priority: "gold", runtime: "bronze"},
		{name: "a runtime list does not restrict the priority class", classes: rules.WorkloadClassEnforcement{Runtime: []rules.WorkloadClassMatch{classNames("gold")}}, priority: "bronze", runtime: "gold"},
		{name: "a priority list does not allow a runtime class of the same name", classes: rules.WorkloadClassEnforcement{Priority: []rules.WorkloadClassMatch{classNames("gold")}, Runtime: []rules.WorkloadClassMatch{classNames("silver")}}, priority: "gold", runtime: "gold", denied: `runtime class "gold"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object

			for _, kind := range classKinds {
				objects = append(objects, kind.class("gold", map[string]string{"env": "customer"}), kind.class("bronze", map[string]string{"env": "internal"}))
			}

			reader := &typedReads{Reader: fake.NewClientBuilder().WithScheme(classScheme(t)).WithObjects(objects...).Build()}
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: tc.classes}}}

			pod := classKinds[0].pod(tc.priority)
			classKinds[1].assign(pod, tc.runtime)

			response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), admission.Request{})
			if tc.denied == "" {
				require.Nil(t, response)
			} else {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, tc.denied)
			}

			require.Equal(t, tc.reads, reader.types)
		})
	}
}

// typedReads records the type of every object read.
type typedReads struct {
	client.Reader
	types []string
}

func (r *typedReads) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.types = append(r.types, fmt.Sprintf("%T", obj))

	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestClassRulesFailedRead(t *testing.T) {
	customer := classLabels("env", "customer")

	for _, kind := range classKinds {
		for _, tc := range []struct {
			name    string
			bodies  []*rules.NamespaceRuleBodyNamespace
			errored bool
			reads   int64
		}{
			{name: "selector needs the class", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer)}, errored: true, reads: 1},
			{name: "deny selector needs the class", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeDeny, customer)}, errored: true, reads: 1},
			{name: "a failed read is not retried", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer, classLabels("env", "internal")), kind.body(rules.ActionTypeAudit, customer)}, errored: true, reads: 1},
			{name: "a name match needs no read", bodies: []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, customer, classNames("gold"))}},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				reader := kind.fixture(t)
				reader.err = errors.New("apiserver unavailable")
				recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}

				response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, kind.pod("gold"), nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), admission.Request{})
				if tc.errored {
					require.NotNil(t, response)
					require.False(t, response.Allowed, "a failed read must never admit the Pod")
					require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
					require.Contains(t, response.Result.Message, `get `+kind.object+` "gold": apiserver unavailable`)
				} else {
					require.Nil(t, response)
				}

				require.Equal(t, tc.reads, reader.gets.Load())
				require.Empty(t, recorder.reasons, "a server error is not a rule decision")
			})
		}
	}
}

func TestClassRulesInvalidExpression(t *testing.T) {
	for _, kind := range classKinds {
		t.Run(kind.name, func(t *testing.T) {
			reader := kind.fixture(t)
			bodies := []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "("}, classNames("gold"))}

			response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, kind.pod("gold"), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Contains(t, response.Result.Message, kind.set+": invalid rule")
			require.Zero(t, reader.gets.Load())
		})
	}
}

func TestClassRulesConditionsUpdatesAndSubresources(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	h := PodRules(nil, nil, nil, compiler)

	for _, kind := range classKinds {
		for _, tc := range []struct {
			name        string
			condition   string
			operation   admissionv1.Operation
			subresource string
			denied      string
			reads       int64
		}{
			{name: "create", operation: admissionv1.Create, denied: "is not allowed by namespace rule", reads: 1},
			{name: "create with a matching condition", condition: "object.metadata.labels['app'] == 'example'", operation: admissionv1.Create, denied: "is not allowed by namespace rule", reads: 1},
			{name: "create with another condition", condition: "object.metadata.labels['app'] == 'other'", operation: admissionv1.Create},
			{name: "create with a failing condition", condition: "object.spec.missing == 'x'", operation: admissionv1.Create, denied: "conditions"},
			{name: "update keeps the class", operation: admissionv1.Update},
			{name: "update is checked by a conditional rule", condition: "object.metadata.labels['app'] == 'example'", operation: admissionv1.Update, denied: "is not allowed by namespace rule", reads: 1},
			{name: "ephemeral containers", operation: admissionv1.Update, subresource: "ephemeralcontainers"},
			{name: "ephemeral containers skip the condition", condition: "object.spec.missing == 'x'", operation: admissionv1.Update, subresource: "ephemeralcontainers"},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				reader := kind.fixture(t)
				body := kind.body(rules.ActionTypeAllow, classLabels("env", "customer"))

				if tc.condition != "" {
					body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: tc.condition}}
				}

				pod := kind.pod("silver")
				req := admission.Request{Operation: tc.operation, SubResource: tc.subresource}

				var response *admission.Response
				if tc.operation == admissionv1.Create {
					response = h.OnCreate(nil, reader, pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
				} else {
					response = h.OnUpdate(nil, reader, pod.DeepCopy(), pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
				}

				if tc.denied == "" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, tc.denied)
				}

				require.Equal(t, tc.reads, reader.gets.Load())
			})
		}
	}
}

// An update that keeps the class takes the decision again only when an allow or
// deny class rule has conditions: one of them may have started or stopped
// matching. Unrelated conditional rules and audit rules never cause that.
func TestClassRulesUnchangedClassUpdates(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	unrelated := func(expression string) *rules.NamespaceRuleBodyNamespace {
		return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action:     rules.ActionTypeDeny,
			Conditions: []rules.AdmissionCondition{{Expression: expression}},
			Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{QoSClasses: []corev1.PodQOSClass{corev1.PodQOSGuaranteed}},
		}}
	}

	const tierSilver = "has(object.metadata.labels) && 'tier' in object.metadata.labels && object.metadata.labels['tier'] == 'silver'"

	for _, kind := range classKinds {
		conditionalClass := func(expression string, action rules.ActionType, names ...string) *rules.NamespaceRuleBodyNamespace {
			body := kind.body(action, classNames(names...))
			body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: expression}}

			return body
		}
		allowGold := kind.body(rules.ActionTypeAllow, classNames("gold"))

		for _, tc := range []struct {
			name   string
			bodies []*rules.NamespaceRuleBodyNamespace
			// denied is the outcome of an update that keeps the class "silver".
			denied bool
			audits int
		}{
			{name: "unconditional class rule", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold}},
			{name: "unrelated matching conditional rule", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, unrelated("true")}},
			{name: "unrelated conditional rule gated by false", bodies: []*rules.NamespaceRuleBodyNamespace{unrelated("false"), allowGold}},
			{name: "conditional deny that matches", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("true", rules.ActionTypeDeny, "silver")}, denied: true},
			{name: "conditional allow list that matches", bodies: []*rules.NamespaceRuleBodyNamespace{conditionalClass("true", rules.ActionTypeAllow, "gold"), unrelated("false")}, denied: true},
			// The Pod was admitted while it carried tier=silver; the update drops the label.
			{name: "conditional allow that stopped matching", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass(tierSilver, rules.ActionTypeAllow, "silver")}, denied: true},
			{name: "conditional deny that does not match", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("false", rules.ActionTypeDeny, "silver"), unrelated("true")}, denied: true},
			{name: "conditional audit that matches is recorded without deciding", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("true", rules.ActionTypeAudit, "silver"), kind.body(rules.ActionTypeAudit, classNames("silver"))}, audits: 1},
			{name: "conditional audit that does not match", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("false", rules.ActionTypeAudit, "silver"), unrelated("true")}},
			{name: "conditional audit next to a conditional allow", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("true", rules.ActionTypeAudit, "silver"), conditionalClass(tierSilver, rules.ActionTypeAllow, "silver")}, denied: true, audits: 1},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				pods, templates := PodRules(nil, nil, nil, compiler), TemplateRules(nil, nil, nil, compiler)
				tenant := &capsulev1beta2.Tenant{}

				pod := kind.pod("silver")
				old := pod.DeepCopy()
				old.Labels["tier"] = "silver"

				req := admission.Request{Operation: admissionv1.Update}
				recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
				require.Equal(t, tc.denied, pods.OnUpdate(nil, kind.fixture(t), old, pod, nil, recorder, tenant, tc.bodies)(t.Context(), req) != nil, "Pod update that keeps the class")

				audits := 0

				for _, reason := range recorder.reasons {
					if reason == events.ReasonNamespaceRuleAudit {
						audits++
					}
				}

				require.Equal(t, tc.audits, audits, "audit events")

				// Creating the Pod, or moving it to the forbidden class, is always checked.
				require.NotNil(t, pods.OnCreate(nil, kind.fixture(t), pod, nil, events.NewDiscardRecorder(), tenant, tc.bodies)(t.Context(), admission.Request{}))

				changed := pod.DeepCopy()
				kind.assign(changed, "gold")
				require.NotNil(t, pods.OnUpdate(nil, kind.fixture(t), changed, pod, nil, events.NewDiscardRecorder(), tenant, tc.bodies)(t.Context(), req))

				targeted := make([]*rules.NamespaceRuleBodyNamespace, len(tc.bodies))
				for i, body := range tc.bodies {
					targeted[i] = body.DeepCopy()
					targeted[i].Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
				}

				obj, templateReq := kind.template(t, rules.ValidateDeployment, "silver")
				templateReq.Operation = admissionv1.Update
				previous := obj.DeepCopy()
				previous.SetLabels(map[string]string{"tier": "silver"})
				require.Equal(t, tc.denied, templates.OnUpdate(nil, kind.fixture(t), previous, obj, nil, events.NewDiscardRecorder(), tenant, targeted)(t.Context(), templateReq) != nil, "template update that keeps the class")
			})
		}
	}
}

// Each class kind follows its own conditional rules: one of another kind must
// not make an update match a class it keeps.
func TestClassRulesConditionalRulesAreScopedToTheirKind(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	for i, kind := range classKinds {
		other := classKinds[(i+1)%len(classKinds)]

		t.Run(kind.name, func(t *testing.T) {
			conditional := func(kind classKind) *rules.NamespaceRuleBodyNamespace {
				body := kind.body(rules.ActionTypeDeny, classNames("silver"))
				body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "true"}}

				return body
			}
			// The selector would read the class if the update matched it again.
			bodies := []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, classLabels("env", "customer")), conditional(other)}

			h := PodRules(nil, nil, nil, compiler)
			reader := kind.fixture(t)
			pod := kind.pod("silver")
			req := admission.Request{Operation: admissionv1.Update}

			require.Nil(t, h.OnUpdate(nil, reader, pod.DeepCopy(), pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), req))
			require.Zero(t, reader.gets.Load(), "the class the update keeps must not be matched again")

			// The same conditional rule of this kind does take the decision again.
			bodies[1] = conditional(kind)

			response := h.OnUpdate(nil, reader, pod.DeepCopy(), pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), req)
			require.NotNil(t, response)
			require.Contains(t, response.Result.Message, kind.denied("silver"))
		})
	}
}

// A class rule without the pod target never applies to the class of the Pod,
// so its conditions must not be evaluated for the class alone.
func TestClassRulesConditionsNeedThePodTarget(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	for _, kind := range classKinds {
		for _, tc := range []struct {
			name    string
			targets []rules.WorkloadValidationTarget
			failed  bool
		}{
			{name: "omitted targets", failed: true},
			{name: "pod target", targets: []rules.WorkloadValidationTarget{rules.ValidatePod}, failed: true},
			{name: "container target only", targets: []rules.WorkloadValidationTarget{rules.ValidateContainers}},
			{name: "controller target", targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, failed: true},
			{name: "controller container target only", targets: []rules.WorkloadValidationTarget{"deployment/containers"}},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				body := kind.body(rules.ActionTypeAllow, classNames("gold"))
				body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}
				body.Enforce.Workloads.Targets = tc.targets
				bodies := []*rules.NamespaceRuleBodyNamespace{body}

				var response *admission.Response

				if len(tc.targets) > 0 && strings.HasPrefix(string(tc.targets[0]), "deployment") {
					obj, req := kind.template(t, rules.ValidateDeployment, "silver")
					response = TemplateRules(nil, nil, nil, compiler).OnCreate(nil, kind.fixture(t), obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), req)
				} else {
					response = PodRules(nil, nil, nil, compiler).OnCreate(nil, kind.fixture(t), kind.pod("silver"), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
				}

				if tc.failed {
					require.NotNil(t, response)
					require.Contains(t, response.Result.Message, "conditions")
				} else {
					require.Nil(t, response, "a rule that cannot apply must not reject the request")
				}
			})
		}
	}
}

// omittedClassCase is a set of rules for a Deployment template. matched and
// audits tell whether an allow or deny rule, and how many audit rules, apply to
// its class. rejected is a class those rules do not admit, if there is one.
type omittedClassCase struct {
	name     string
	bodies   []*rules.NamespaceRuleBodyNamespace
	matched  bool
	audits   int
	rejected string
}

// omittedClassCases are the rules a controller template without a class is
// admitted against. The outcome depends on whether Kubernetes assigns a class
// to its Pods later.
func omittedClassCases(kind classKind) []omittedClassCase {
	// The rules match the class gold, and silver by neither name nor labels.
	targeted := func(action rules.ActionType, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
		body := kind.body(action, classNames("gold"), classLabels("env", "customer"))
		body.Enforce.Workloads.Targets = targets

		return body
	}

	return []omittedClassCase{
		{name: "allow list", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidateDeployment)}, matched: true, rejected: "silver"},
		{name: "deny list", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeDeny, rules.ValidateDeployment)}, matched: true, rejected: "gold"},
		{name: "default action", bodies: []*rules.NamespaceRuleBodyNamespace{targeted("", rules.ValidatePod, rules.ValidateDeployment)}, matched: true, rejected: "gold"},
		{name: "audit only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAudit, rules.ValidateDeployment)}, audits: 1},
		{name: "audit and allow", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAudit, rules.ValidateDeployment), targeted(rules.ActionTypeAllow, rules.ValidateDeployment)}, matched: true, audits: 1, rejected: "silver"},
		{name: "another controller", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidateStatefulSet)}},
		{name: "pods only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidatePod), targeted(rules.ActionTypeAllow)}},
		{name: "container target only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, "deployment/containers")}},
	}
}

// Kubernetes resolves the priority class of a controller template only when
// its Pods are created, so a rule that targets the controller cannot let it be
// omitted.
func TestPriorityClassRulesOmittedTemplateClass(t *testing.T) {
	kind := classKinds[0]
	require.NotEmpty(t, kind.missing)

	for _, tc := range omittedClassCases(kind) {
		t.Run(tc.name, func(t *testing.T) {
			reader := kind.fixture(t)
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
			h := TemplateRules(nil, nil, nil, nil)
			obj, req := kind.template(t, rules.ValidateDeployment, "")

			var wanted []string
			for range tc.audits {
				wanted = append(wanted, events.ReasonNamespaceRuleAudit)
			}

			response := h.OnCreate(nil, reader, obj, nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req)
			if tc.matched {
				wanted = append(wanted, events.ReasonMissingPriorityClass)

				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.EqualValues(t, http.StatusForbidden, response.Result.Code)
				require.Contains(t, response.Result.Message, "spec.template: priority class is required at spec.priorityClassName because class rules target deployment")
			} else {
				require.Nil(t, response)
			}

			require.Equal(t, wanted, recorder.reasons)
			require.Zero(t, reader.gets.Load(), "a missing class needs no read")

			// An update that keeps the class omitted is not checked again; dropping it is.
			req.Operation = admissionv1.Update
			require.Nil(t, h.OnUpdate(nil, reader, obj.DeepCopy(), obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req))

			named, _ := kind.template(t, rules.ValidateDeployment, "gold")
			require.Equal(t, tc.matched, h.OnUpdate(nil, reader, named, obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req) != nil)
		})
	}
}

// Kubernetes has no default RuntimeClass, so nothing assigns a class to the
// Pods of a controller template later: an omitted runtime class is never
// matched, whatever the rule targets or decides.
func TestRuntimeClassRulesOmittedTemplateClass(t *testing.T) {
	kind := classKinds[1]
	require.Empty(t, kind.missing)

	for _, tc := range omittedClassCases(kind) {
		t.Run(tc.name, func(t *testing.T) {
			reader := kind.fixture(t)
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
			h := TemplateRules(nil, nil, nil, nil)
			obj, req := kind.template(t, rules.ValidateDeployment, "")

			require.Nil(t, h.OnCreate(nil, reader, obj, nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req))
			require.Empty(t, recorder.reasons, "an omitted runtime class is neither rejected nor audited")

			// Dropping the class of a template is not checked either.
			req.Operation = admissionv1.Update
			named, _ := kind.template(t, rules.ValidateDeployment, "gold")
			require.Nil(t, h.OnUpdate(nil, reader, named, obj, nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req))
			require.Empty(t, recorder.reasons)
			require.Zero(t, reader.gets.Load(), "an omitted class needs no read")

			// The same rules still judge a class the template names: only the one they
			// do not admit is rejected, and none when they do not decide for the template.
			for _, name := range []string{"gold", "silver"} {
				template, createReq := kind.template(t, rules.ValidateDeployment, name)
				response := h.OnCreate(nil, reader, template, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), createReq)

				// Adding it to a template that had none is an update that changes the class.
				updated := h.OnUpdate(nil, reader, obj, template, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req)

				if name != tc.rejected {
					require.Nil(t, response, "class %s", name)
					require.Nil(t, updated, "class %s", name)

					continue
				}

				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, `spec.template: runtime class "`+name+`" at spec.runtimeClassName`)
				require.NotNil(t, updated)
			}
		})
	}
}

// A Pod without a class is never matched. A priority class has already been
// assigned by Kubernetes if the cluster has a global default; a runtime class
// has no default at all, also when the field is set to an empty name.
func TestClassRulesOmittedPodClass(t *testing.T) {
	for _, kind := range classKinds {
		for _, action := range []rules.ActionType{rules.ActionTypeAllow, rules.ActionTypeDeny, rules.ActionTypeAudit} {
			for _, targets := range [][]rules.WorkloadValidationTarget{nil, {rules.ValidatePod}, {rules.ValidatePod, rules.ValidateDeployment}} {
				body := kind.body(action, classNames("gold"), classLabels("env", "customer"))
				body.Enforce.Workloads.Targets = targets

				pods := []*corev1.Pod{kind.pod("")}
				if kind.name == "runtime" {
					empty := kind.pod("")
					empty.Spec.RuntimeClassName = new("")
					pods = append(pods, empty)
				}

				for _, pod := range pods {
					reader := kind.fixture(t)
					recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}

					require.Nil(t, PodRules(nil, nil, nil, nil).OnCreate(nil, reader, pod, nil, recorder, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), admission.Request{}), "%s %s %v", kind.name, action, targets)
					require.Empty(t, recorder.reasons)
					require.Zero(t, reader.gets.Load())
				}
			}
		}
	}
}

func TestClassRulesControllerTemplates(t *testing.T) {
	tenant := &capsulev1beta2.Tenant{Name: "tenant-a"}
	h := TemplateRules(nil, nil, nil, nil)

	for _, kind := range classKinds {
		for _, target := range []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
			t.Run(kind.name+"/"+string(target), func(t *testing.T) {
				recorder := &templateRecorder{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
				body := kind.body(rules.ActionTypeAllow, classNames("gold"), classLabels("env", "internal"))
				body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{target}
				bodies := []*rules.NamespaceRuleBodyNamespace{body}
				before := body.DeepCopy()

				for _, tc := range []struct {
					class  string
					denied bool
					reads  int64
				}{{class: "gold"}, {class: "bronze", reads: 1}, {class: "silver", denied: true, reads: 1}, {class: "ghost", denied: true, reads: 1}, {class: "", denied: kind.missing != ""}} {
					reader := kind.fixture(t)
					obj, req := kind.template(t, target, tc.class)
					original := obj.DeepCopy()
					path := strings.Join(workloads.PodTemplatePath(obj.GroupVersionKind()), ".")

					response := h.OnCreate(nil, reader, obj, nil, recorder, tenant, bodies)(t.Context(), req)
					if tc.denied {
						want := path + ": " + kind.rejected(tc.class)
						if tc.class == "" {
							want = path + ": " + kind.set + " is required at " + kind.path + " because class rules target " + string(target)
						}

						require.NotNil(t, response, tc.class)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, want)
						require.Same(t, obj, recorder.regarding, "events must concern the controller, not a synthetic Pod")
					} else {
						require.Nil(t, response, tc.class)
					}

					require.Equal(t, tc.reads, reader.gets.Load(), tc.class)
					require.Equal(t, original, obj)

					// An update that keeps the class is not checked again; changing it is.
					req.Operation = admissionv1.Update
					reader.gets.Store(0)
					require.Nil(t, h.OnUpdate(nil, reader, obj.DeepCopy(), obj, nil, recorder, tenant, bodies)(t.Context(), req), tc.class)
					require.Zero(t, reader.gets.Load())

					old, _ := kind.template(t, target, "gold")
					if tc.class == "gold" {
						old, _ = kind.template(t, target, "bronze")
					}

					response = h.OnUpdate(nil, reader, old, obj, nil, recorder, tenant, bodies)(t.Context(), req)
					require.Equal(t, tc.denied, response != nil, tc.class)
				}

				// Omitted targets and other kinds never opt this controller in.
				obj, req := kind.template(t, target, "silver")
				for _, targets := range [][]rules.WorkloadValidationTarget{nil, {rules.ValidatePod}} {
					scoped := body.DeepCopy()
					scoped.Enforce.Workloads.Targets = targets
					require.Nil(t, h.OnCreate(nil, kind.fixture(t), obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{scoped})(t.Context(), req))
				}

				// A part target scopes other policies, not the Pod-level class.
				part := body.DeepCopy()
				part.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{target + "/containers"}
				require.Nil(t, h.OnCreate(nil, kind.fixture(t), obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{part})(t.Context(), req))

				require.Equal(t, before, body)
			})
		}
	}
}

func TestClassRulesFailedReadOnTemplate(t *testing.T) {
	for _, kind := range classKinds {
		t.Run(kind.name, func(t *testing.T) {
			reader := kind.fixture(t)
			reader.err = errors.New("apiserver unavailable")

			body := kind.body(rules.ActionTypeDeny, classLabels("env", "customer"))
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
			obj, req := kind.template(t, rules.ValidateDeployment, "gold")

			response := TemplateRules(nil, nil, nil, nil).OnCreate(nil, reader, obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
			require.Contains(t, response.Result.Message, `get `+kind.object+` "gold": apiserver unavailable`)
		})
	}
}

// Compiled expressions and selectors are shared between requests; class
// snapshots and decisions are not.
func TestClassRulesConcurrentRequests(t *testing.T) {
	for _, kind := range classKinds {
		t.Run(kind.name, func(t *testing.T) {
			regex, selectors := cache.NewRegexCache(), cache.NewLabelSelectorCache()
			h := PodRules(regex, nil, selectors, nil)
			reader := kind.fixture(t)
			bodies := []*rules.NamespaceRuleBodyNamespace{kind.body(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "^gold$"}, classLabels("env", "internal"))}

			var wg sync.WaitGroup

			for _, tc := range []struct {
				class  string
				denied bool
			}{{class: "gold"}, {class: "bronze"}, {class: "silver", denied: true}} {
				for range 16 {
					wg.Go(func() {
						response := h.OnCreate(nil, reader, kind.pod(tc.class), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
						if tc.denied != (response != nil) {
							t.Errorf("class %s: denied=%v, want %v", tc.class, response != nil, tc.denied)
						}
					})
				}
			}

			wg.Wait()

			require.EqualValues(t, 32, reader.gets.Load(), "only bronze and silver need their labels")
			require.Equal(t, 1, regex.Stats())
		})
	}
}

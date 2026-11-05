// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
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

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

// classReads counts the PriorityClass reads of an admission.
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

func priorityClassFixture(tb testing.TB) *classReads {
	tb.Helper()

	scheme := runtime.NewScheme()
	require.NoError(tb, schedulingv1.AddToScheme(scheme))

	return &classReads{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&schedulingv1.PriorityClass{Name: "gold", Labels: map[string]string{"env": "customer", "tier": "gold"}, Value: 1000},
		&schedulingv1.PriorityClass{Name: "silver", Value: 500},
		&schedulingv1.PriorityClass{Name: "bronze", Labels: map[string]string{"env": "internal"}, Value: 100},
	).Build()}
}

func priorityClassBody(action rules.ActionType, classes ...rules.WorkloadClassMatch) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action:    action,
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: classes}},
	}}
}

func classNames(names ...string) rules.WorkloadClassMatch {
	return rules.WorkloadClassMatch{Exact: names}
}

func classLabels(key, value string) rules.WorkloadClassMatch {
	return rules.WorkloadClassMatch{MatchLabels: map[string]string{key: value}}
}

func priorityClassPod(class string) *corev1.Pod {
	return &corev1.Pod{
		Name: "pod", Namespace: "tenant-a", Labels: map[string]string{"app": "example"},
		Spec: corev1.PodSpec{PriorityClassName: class, Containers: []corev1.Container{{Name: "app", Image: "example.com/app:v1"}}},
	}
}

func TestPriorityClassRules(t *testing.T) {
	customer, internal := classLabels("env", "customer"), classLabels("env", "internal")

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
		{name: "pod without a class is not matched", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, classNames("gold"), customer)}},
		{name: "allow exact", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, classNames("silver", "gold"))}},
		{name: "allow expression", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "^(gold|silver)$"})}},
		{name: "allow selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer)}, reads: 1},
		{name: "allow selector expression", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, rules.WorkloadClassMatch{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"internal"}}}})}, reads: 1},
		{name: "allow list rejects another name", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, classNames("gold"), rules.WorkloadClassMatch{Expression: "^silver$"})},
			denied: `priority class "bronze" at spec.priorityClassName is not allowed by namespace rule. Allowed priority classes: exact: gold, exp: ^silver$`, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "allow list rejects other labels", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer)},
			denied: `priority class "silver" at spec.priorityClassName is not allowed by namespace rule. Allowed priority classes: selector: env=customer`, reads: 1, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "missing class never matches a selector", class: "ghost", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer)},
			denied: `priority class "ghost" at spec.priorityClassName is not allowed`, reads: 1, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "name decides before any selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, internal, customer, classNames("gold"))}},
		{name: "criteria within an entry are ORed", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, rules.WorkloadClassMatch{Exact: []string{"gold"}, LabelSelector: internal.LabelSelector})}, reads: 1},
		{name: "several selectors read the class once", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer, internal), priorityClassBody(rules.ActionTypeDeny, classLabels("tier", "gold"))},
			denied: "is not allowed by namespace rule", reads: 1, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "deny exact", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeDeny, classNames("gold"))},
			denied: `priority class "gold" at spec.priorityClassName is denied by namespace rule`, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "default action denies", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody("", classNames("gold"))},
			denied: "is denied by namespace rule", events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "deny selector", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeDeny, customer)},
			denied: `priority class "gold" at spec.priorityClassName is denied by namespace rule`, reads: 1, events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "deny list admits another class", class: "silver", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeDeny, classNames("gold"), customer)}, reads: 1},
		{name: "last matching rule wins: deny", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, classNames("gold")), priorityClassBody(rules.ActionTypeDeny, classNames("gold"))},
			denied: "is denied by namespace rule", events: []string{events.ReasonForbiddenPriorityClass}},
		{name: "last matching rule wins: allow", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeDeny, classNames("gold")), priorityClassBody(rules.ActionTypeAllow, classNames("gold"))}},
		{name: "audit records every matching entry and never decides", class: "gold", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAudit, classNames("gold"), customer, internal)}, reads: 1,
			events: []string{events.ReasonNamespaceRuleAudit, events.ReasonNamespaceRuleAudit}},
		{name: "audit does not grant what an allow list rejects", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAudit, classNames("bronze")), priorityClassBody(rules.ActionTypeAllow, classNames("gold"))},
			denied: "is not allowed by namespace rule", events: []string{events.ReasonNamespaceRuleAudit, events.ReasonForbiddenPriorityClass}},
		{name: "container targets do not select the pod class", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{func() *rules.NamespaceRuleBodyNamespace {
			body := priorityClassBody(rules.ActionTypeAllow, classNames("gold"))
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateContainers}

			return body
		}()}},
		{name: "controller targets do not select pods", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{func() *rules.NamespaceRuleBodyNamespace {
			body := priorityClassBody(rules.ActionTypeAllow, classNames("gold"))
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}

			return body
		}()}},
		{name: "pod target selects the pod class", class: "bronze", bodies: []*rules.NamespaceRuleBodyNamespace{func() *rules.NamespaceRuleBodyNamespace {
			body := priorityClassBody(rules.ActionTypeAllow, classNames("gold"))
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidatePod}

			return body
		}()}, denied: "is not allowed by namespace rule", events: []string{events.ReasonForbiddenPriorityClass}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := priorityClassFixture(t)
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}

			var before []*rules.NamespaceRuleBodyNamespace
			for _, body := range tc.bodies {
				before = append(before, body.DeepCopy())
			}

			pod := priorityClassPod(tc.class)
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

			require.Equal(t, tc.reads, reader.gets.Load(), "PriorityClass reads")
			require.Equal(t, tc.events, recorder.reasons)
			require.Equal(t, before, tc.bodies, "rules are cache-owned and must not change")
			require.Equal(t, original, pod)
		})
	}
}

func TestPriorityClassRulesFailedRead(t *testing.T) {
	customer := classLabels("env", "customer")

	for _, tc := range []struct {
		name    string
		bodies  []*rules.NamespaceRuleBodyNamespace
		errored bool
		reads   int64
	}{
		{name: "selector needs the class", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer)}, errored: true, reads: 1},
		{name: "deny selector needs the class", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeDeny, customer)}, errored: true, reads: 1},
		{name: "a failed read is not retried", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer, classLabels("env", "internal")), priorityClassBody(rules.ActionTypeAudit, customer)}, errored: true, reads: 1},
		{name: "a name match needs no read", bodies: []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, customer, classNames("gold"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := priorityClassFixture(t)
			reader.err = errors.New("apiserver unavailable")
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}

			response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, priorityClassPod("gold"), nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), admission.Request{})
			if tc.errored {
				require.NotNil(t, response)
				require.False(t, response.Allowed, "a failed read must never admit the Pod")
				require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
				require.Contains(t, response.Result.Message, `get PriorityClass "gold": apiserver unavailable`)
			} else {
				require.Nil(t, response)
			}

			require.Equal(t, tc.reads, reader.gets.Load())
			require.Empty(t, recorder.reasons, "a server error is not a rule decision")
		})
	}
}

func TestPriorityClassRulesInvalidExpression(t *testing.T) {
	reader := priorityClassFixture(t)
	bodies := []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "("}, classNames("gold"))}

	response := PodRules(nil, nil, nil, nil).OnCreate(nil, reader, priorityClassPod("gold"), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Contains(t, response.Result.Message, "priority class: invalid rule")
	require.Zero(t, reader.gets.Load())
}

func TestPriorityClassRulesConditionsUpdatesAndSubresources(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	h := PodRules(nil, nil, nil, compiler)

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
		t.Run(tc.name, func(t *testing.T) {
			reader := priorityClassFixture(t)
			body := priorityClassBody(rules.ActionTypeAllow, classLabels("env", "customer"))

			if tc.condition != "" {
				body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: tc.condition}}
			}

			pod := priorityClassPod("silver")
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

// An update that keeps the class takes the decision again only when an allow or
// deny class rule has conditions: one of them may have started or stopped
// matching. Unrelated conditional rules and audit rules never cause that.
func TestPriorityClassRulesUnchangedClassUpdates(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	unrelated := func(expression string) *rules.NamespaceRuleBodyNamespace {
		return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action:     rules.ActionTypeDeny,
			Conditions: []rules.AdmissionCondition{{Expression: expression}},
			Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{QoSClasses: []corev1.PodQOSClass{corev1.PodQOSGuaranteed}},
		}}
	}
	conditionalClass := func(expression string, action rules.ActionType, names ...string) *rules.NamespaceRuleBodyNamespace {
		body := priorityClassBody(action, classNames(names...))
		body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: expression}}

		return body
	}
	allowGold := priorityClassBody(rules.ActionTypeAllow, classNames("gold"))

	const tierSilver = "has(object.metadata.labels) && 'tier' in object.metadata.labels && object.metadata.labels['tier'] == 'silver'"

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
		{name: "conditional audit that matches is recorded without deciding", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("true", rules.ActionTypeAudit, "silver"), priorityClassBody(rules.ActionTypeAudit, classNames("silver"))}, audits: 1},
		{name: "conditional audit that does not match", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("false", rules.ActionTypeAudit, "silver"), unrelated("true")}},
		{name: "conditional audit next to a conditional allow", bodies: []*rules.NamespaceRuleBodyNamespace{allowGold, conditionalClass("true", rules.ActionTypeAudit, "silver"), conditionalClass(tierSilver, rules.ActionTypeAllow, "silver")}, denied: true, audits: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pods, templates := PodRules(nil, nil, nil, compiler), TemplateRules(nil, nil, nil, compiler)
			tenant := &capsulev1beta2.Tenant{}

			pod := priorityClassPod("silver")
			old := pod.DeepCopy()
			old.Labels["tier"] = "silver"

			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Update}}
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
			require.Equal(t, tc.denied, pods.OnUpdate(nil, priorityClassFixture(t), old, pod, nil, recorder, tenant, tc.bodies)(t.Context(), req) != nil, "Pod update that keeps the class")

			audits := 0

			for _, reason := range recorder.reasons {
				if reason == events.ReasonNamespaceRuleAudit {
					audits++
				}
			}

			require.Equal(t, tc.audits, audits, "audit events")

			// Creating the Pod, or moving it to the forbidden class, is always checked.
			require.NotNil(t, pods.OnCreate(nil, priorityClassFixture(t), pod, nil, events.NewDiscardRecorder(), tenant, tc.bodies)(t.Context(), admission.Request{}))

			changed := pod.DeepCopy()
			changed.Spec.PriorityClassName = "gold"
			require.NotNil(t, pods.OnUpdate(nil, priorityClassFixture(t), changed, pod, nil, events.NewDiscardRecorder(), tenant, tc.bodies)(t.Context(), req))

			targeted := make([]*rules.NamespaceRuleBodyNamespace, len(tc.bodies))
			for i, body := range tc.bodies {
				targeted[i] = body.DeepCopy()
				targeted[i].Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
			}

			obj, templateReq := priorityClassTemplate(t, rules.ValidateDeployment, "silver")
			templateReq.Operation = admissionv1.Update
			previous := obj.DeepCopy()
			previous.SetLabels(map[string]string{"tier": "silver"})
			require.Equal(t, tc.denied, templates.OnUpdate(nil, priorityClassFixture(t), previous, obj, nil, events.NewDiscardRecorder(), tenant, targeted)(t.Context(), templateReq) != nil, "template update that keeps the class")
		})
	}
}

// A class rule without the pod target never applies to priorityClassName, so
// its conditions must not be evaluated for the class alone.
func TestPriorityClassRulesConditionsNeedThePodTarget(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

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
		t.Run(tc.name, func(t *testing.T) {
			body := priorityClassBody(rules.ActionTypeAllow, classNames("gold"))
			body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}
			body.Enforce.Workloads.Targets = tc.targets
			bodies := []*rules.NamespaceRuleBodyNamespace{body}

			var response *admission.Response

			if len(tc.targets) > 0 && strings.HasPrefix(string(tc.targets[0]), "deployment") {
				obj, req := priorityClassTemplate(t, rules.ValidateDeployment, "silver")
				response = TemplateRules(nil, nil, nil, compiler).OnCreate(nil, priorityClassFixture(t), obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), req)
			} else {
				response = PodRules(nil, nil, nil, compiler).OnCreate(nil, priorityClassFixture(t), priorityClassPod("silver"), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
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

// Kubernetes resolves the class of a controller template only when its Pods
// are created, so a rule that targets the controller cannot let it be omitted.
func TestPriorityClassRulesOmittedTemplateClass(t *testing.T) {
	targeted := func(action rules.ActionType, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
		body := priorityClassBody(action, classNames("gold"), classLabels("env", "customer"))
		body.Enforce.Workloads.Targets = targets

		return body
	}

	for _, tc := range []struct {
		name   string
		bodies []*rules.NamespaceRuleBodyNamespace
		denied bool
		events []string
	}{
		{name: "allow list", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidateDeployment)}, denied: true, events: []string{events.ReasonMissingPriorityClass}},
		{name: "deny list", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeDeny, rules.ValidateDeployment)}, denied: true, events: []string{events.ReasonMissingPriorityClass}},
		{name: "default action", bodies: []*rules.NamespaceRuleBodyNamespace{targeted("", rules.ValidatePod, rules.ValidateDeployment)}, denied: true, events: []string{events.ReasonMissingPriorityClass}},
		{name: "audit only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAudit, rules.ValidateDeployment)}, events: []string{events.ReasonNamespaceRuleAudit}},
		{name: "audit and allow", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAudit, rules.ValidateDeployment), targeted(rules.ActionTypeAllow, rules.ValidateDeployment)}, denied: true, events: []string{events.ReasonNamespaceRuleAudit, events.ReasonMissingPriorityClass}},
		{name: "another controller", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidateStatefulSet)}},
		{name: "pods only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, rules.ValidatePod), targeted(rules.ActionTypeAllow)}},
		{name: "container target only", bodies: []*rules.NamespaceRuleBodyNamespace{targeted(rules.ActionTypeAllow, "deployment/containers")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := priorityClassFixture(t)
			recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
			h := TemplateRules(nil, nil, nil, nil)
			obj, req := priorityClassTemplate(t, rules.ValidateDeployment, "")

			response := h.OnCreate(nil, reader, obj, nil, recorder, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req)
			if tc.denied {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.EqualValues(t, http.StatusForbidden, response.Result.Code)
				require.Contains(t, response.Result.Message, "spec.template: priority class is required at spec.priorityClassName because class rules target deployment")
			} else {
				require.Nil(t, response)
			}

			require.Equal(t, tc.events, recorder.reasons)
			require.Zero(t, reader.gets.Load(), "a missing class needs no read")

			// An update that keeps the class omitted is not checked again; dropping it is.
			req.Operation = admissionv1.Update
			require.Nil(t, h.OnUpdate(nil, reader, obj.DeepCopy(), obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req))

			named, _ := priorityClassTemplate(t, rules.ValidateDeployment, "gold")
			require.Equal(t, tc.denied, h.OnUpdate(nil, reader, named, obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req) != nil)
		})
	}

	// A Pod has already been given the cluster's global default class, if any.
	for _, action := range []rules.ActionType{rules.ActionTypeAllow, rules.ActionTypeDeny, rules.ActionTypeAudit} {
		recorder := &classEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
		require.Nil(t, PodRules(nil, nil, nil, nil).OnCreate(nil, priorityClassFixture(t), priorityClassPod(""), nil, recorder, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{targeted(action)})(t.Context(), admission.Request{}))
		require.Empty(t, recorder.reasons)
	}
}

func priorityClassTemplate(t testing.TB, target rules.WorkloadValidationTarget, class string) (*unstructured.Unstructured, admission.Request) {
	t.Helper()

	obj, req := templateFixture(t, target)
	path := append(workloads.PodTemplatePath(obj.GroupVersionKind()), "spec", "priorityClassName")

	if class == "" {
		unstructured.RemoveNestedField(obj.Object, path...)
	} else {
		require.NoError(t, unstructured.SetNestedField(obj.Object, class, path...))
	}

	return obj, req
}

func TestPriorityClassRulesControllerTemplates(t *testing.T) {
	recorder := &templateRecorder{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
	tenant := &capsulev1beta2.Tenant{Name: "tenant-a"}
	h := TemplateRules(nil, nil, nil, nil)

	for _, target := range []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
		t.Run(string(target), func(t *testing.T) {
			body := priorityClassBody(rules.ActionTypeAllow, classNames("gold"), classLabels("env", "internal"))
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{target}
			bodies := []*rules.NamespaceRuleBodyNamespace{body}
			before := body.DeepCopy()

			for _, tc := range []struct {
				class  string
				denied bool
				reads  int64
			}{{class: "gold"}, {class: "bronze", reads: 1}, {class: "silver", denied: true, reads: 1}, {class: "ghost", denied: true, reads: 1}, {class: "", denied: true}} {
				reader := priorityClassFixture(t)
				obj, req := priorityClassTemplate(t, target, tc.class)
				original := obj.DeepCopy()
				path := strings.Join(workloads.PodTemplatePath(obj.GroupVersionKind()), ".")

				response := h.OnCreate(nil, reader, obj, nil, recorder, tenant, bodies)(t.Context(), req)
				if tc.denied {
					want := path + `: priority class "` + tc.class + `" at spec.priorityClassName is not allowed by namespace rule`
					if tc.class == "" {
						want = path + ": priority class is required at spec.priorityClassName because class rules target " + string(target)
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

				old, _ := priorityClassTemplate(t, target, "gold")
				if tc.class == "gold" {
					old, _ = priorityClassTemplate(t, target, "bronze")
				}

				response = h.OnUpdate(nil, reader, old, obj, nil, recorder, tenant, bodies)(t.Context(), req)
				require.Equal(t, tc.denied, response != nil, tc.class)
			}

			// Omitted targets and other kinds never opt this controller in.
			obj, req := priorityClassTemplate(t, target, "silver")
			for _, targets := range [][]rules.WorkloadValidationTarget{nil, {rules.ValidatePod}} {
				scoped := body.DeepCopy()
				scoped.Enforce.Workloads.Targets = targets
				require.Nil(t, h.OnCreate(nil, priorityClassFixture(t), obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{scoped})(t.Context(), req))
			}

			// A part target scopes other policies, not the Pod-level class.
			part := body.DeepCopy()
			part.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{target + "/containers"}
			require.Nil(t, h.OnCreate(nil, priorityClassFixture(t), obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{part})(t.Context(), req))

			require.Equal(t, before, body)
		})
	}
}

func TestPriorityClassRulesFailedReadOnTemplate(t *testing.T) {
	reader := priorityClassFixture(t)
	reader.err = errors.New("apiserver unavailable")

	body := priorityClassBody(rules.ActionTypeDeny, classLabels("env", "customer"))
	body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
	obj, req := priorityClassTemplate(t, rules.ValidateDeployment, "gold")

	response := TemplateRules(nil, nil, nil, nil).OnCreate(nil, reader, obj, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
}

// Compiled expressions and selectors are shared between requests; class
// snapshots and decisions are not.
func TestPriorityClassRulesConcurrentRequests(t *testing.T) {
	regex, selectors := cache.NewRegexCache(), cache.NewLabelSelectorCache()
	h := PodRules(regex, nil, selectors, nil)
	reader := priorityClassFixture(t)
	bodies := []*rules.NamespaceRuleBodyNamespace{priorityClassBody(rules.ActionTypeAllow, rules.WorkloadClassMatch{Expression: "^gold$"}, classLabels("env", "internal"))}

	var wg sync.WaitGroup

	for _, tc := range []struct {
		class  string
		denied bool
	}{{class: "gold"}, {class: "bronze"}, {class: "silver", denied: true}} {
		for range 16 {
			wg.Go(func() {
				response := h.OnCreate(nil, reader, priorityClassPod(tc.class), nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{}, bodies)(t.Context(), admission.Request{})
				if tc.denied != (response != nil) {
					t.Errorf("class %s: denied=%v, want %v", tc.class, response != nil, tc.denied)
				}
			})
		}
	}

	wg.Wait()

	require.EqualValues(t, 32, reader.gets.Load(), "only bronze and silver need their labels")
	require.Equal(t, 1, regex.Stats())
}

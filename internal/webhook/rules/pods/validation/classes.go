// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// classLookupError is a failed read of a referenced class: a server error,
// never a rule decision.
type classLookupError struct{ err error }

func (e *classLookupError) Error() string { return e.err.Error() }

func (e *classLookupError) Unwrap() error { return e.err }

// workloadClass describes a class a Pod references and the rules matching it.
// Every class is matched by name or by the labels of the class object; the
// kinds differ in the Pod field, the object, and what an omitted class means.
type workloadClass struct {
	// set and path identify the reference in admission messages and events.
	set, path string
	// kind is the kind of the class object.
	kind string
	// allowed labels the allow list in admission messages.
	allowed string
	// forbidden is the event reason of a rejected class.
	forbidden string
	// missing is the event reason of a controller template without a class. It
	// is set when Kubernetes assigns a default class as the Pods of a controller
	// are created, and empty when an omitted class stays unset.
	missing string

	// name returns the class a Pod names, empty when it names none.
	name func(*corev1.Pod) string
	// applies reports whether a rule matches this class of the Pod.
	applies func(*apirules.NamespaceRuleEnforceBody) bool
	// matches returns the class matchers of a rule.
	matches func(*apirules.NamespaceRuleEnforceBody) []apirules.WorkloadClassMatch
	// object returns an empty class object to read into.
	object func() client.Object
}

var (
	// The built-in Priority admission plugin assigns the cluster's global default
	// class before webhooks run, so an empty name means the Pod has no class at
	// all. It resolves the class of a controller template only when its Pods are
	// created, which a rule that only targets the controller never sees.
	priorityClasses = &workloadClass{
		set:       "priority class",
		path:      "spec.priorityClassName",
		kind:      "PriorityClass",
		allowed:   "Allowed priority classes",
		forbidden: events.ReasonForbiddenPriorityClass,
		missing:   events.ReasonMissingPriorityClass,
		name:      func(pod *corev1.Pod) string { return pod.Spec.PriorityClassName },
		applies:   appliesToPriorityClasses,
		matches: func(body *apirules.NamespaceRuleEnforceBody) []apirules.WorkloadClassMatch {
			return body.Workloads.Classes.Priority
		},
		object: func() client.Object { return &schedulingv1.PriorityClass{} },
	}

	// Kubernetes has no default RuntimeClass. A Pod or Pod template without a
	// class keeps the default runtime of its node, also after admission, so an
	// omitted class is never matched.
	runtimeClasses = &workloadClass{
		set:       "runtime class",
		path:      "spec.runtimeClassName",
		kind:      "RuntimeClass",
		allowed:   "Allowed runtime classes",
		forbidden: events.ReasonForbiddenRuntimeClass,
		name:      func(pod *corev1.Pod) string { return ptr.Deref(pod.Spec.RuntimeClassName, "") },
		applies:   appliesToRuntimeClasses,
		matches: func(body *apirules.NamespaceRuleEnforceBody) []apirules.WorkloadClassMatch {
			return body.Workloads.Classes.Runtime
		},
		object: func() client.Object { return &nodev1.RuntimeClass{} },
	}
)

func appliesToPriorityClasses(body *apirules.NamespaceRuleEnforceBody) bool {
	return body != nil && len(body.Workloads.Classes.Priority) > 0 && body.GetWorkloadTargets(apirules.ValidatePod)
}

func appliesToRuntimeClasses(body *apirules.NamespaceRuleEnforceBody) bool {
	return body != nil && len(body.Workloads.Classes.Runtime) > 0 && body.GetWorkloadTargets(apirules.ValidatePod)
}

// classReader reads the class a Pod names at most once, and only when a
// selector has to be matched. A missing class never matches a selector.
type classReader struct {
	reader client.Reader
	class  *workloadClass
	name   string

	read   bool
	object client.Object
	err    error
}

func (c *classReader) get(ctx context.Context) (client.Object, error) {
	if c.read {
		return c.object, c.err
	}

	c.read = true

	object := c.class.object()
	if err := c.reader.Get(ctx, client.ObjectKey{Name: c.name}, object); err != nil {
		if !apierrors.IsNotFound(err) {
			c.err = &classLookupError{err: fmt.Errorf("get %s %q: %w", c.class.kind, c.name, err)}
		}

		return nil, c.err
	}

	c.object = object

	return object, nil
}

// validatePriorityClasses and validateRuntimeClasses run on every Pod and Pod
// template admission. Each tests its own predicate before anything else, so
// the compiler can inline it into the scan of the rules.
func (h *podRules) validatePriorityClasses(
	ctx context.Context,
	reader client.Reader,
	object client.Object,
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if pod == nil || !slices.ContainsFunc(enforceBodies, appliesToPriorityClasses) {
		return nil, nil
	}

	return h.validateClasses(ctx, reader, object, pod, enforceBodies, priorityClasses)
}

func (h *podRules) validateRuntimeClasses(
	ctx context.Context,
	reader client.Reader,
	object client.Object,
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	// Most Pods name no runtime class and nothing assigns one later: no rule can
	// match them, so the rules are not even scanned.
	if pod == nil || ptr.Deref(pod.Spec.RuntimeClassName, "") == "" || !slices.ContainsFunc(enforceBodies, appliesToRuntimeClasses) {
		return nil, nil
	}

	return h.validateClasses(ctx, reader, object, pod, enforceBodies, runtimeClasses)
}

// validateClasses matches the class a Pod or Pod template names against the
// rules that apply to it.
func (h *podRules) validateClasses(
	ctx context.Context,
	reader client.Reader,
	object client.Object,
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	class *workloadClass,
) (*ruleengine.Evaluation, error) {
	name := class.name(pod)
	if name == "" {
		if _, isPod := object.(*corev1.Pod); isPod || class.missing == "" {
			return nil, nil
		}

		return class.missingClass(object, enforceBodies), nil
	}

	classes := &classReader{reader: reader, class: class, name: name}

	evaluation, err := evaluatePodRules(pod, enforceBodies, podRuleSet[apirules.WorkloadClassMatch]{
		Name:        class.set,
		EventReason: class.forbidden,
		Values: func(*corev1.Pod) []ruleengine.Value {
			return []ruleengine.Value{{Value: name, Path: class.path}}
		},
		Rules: func(body *apirules.NamespaceRuleEnforceBody) []apirules.WorkloadClassMatch {
			if !class.applies(body) {
				return nil
			}

			return h.classesToMatch(class.matches(body), body.Action.OrDefault(), name)
		},
		Matches: func(match apirules.WorkloadClassMatch, value ruleengine.Value) (ruleengine.Match, error) {
			if match.HasNameMatch() {
				matched, err := match.NameMatch().MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				if matched {
					return ruleengine.Match{Matched: true}, nil
				}
			}

			if !match.HasSelector() {
				return ruleengine.Match{}, nil
			}

			selector, err := h.selectors.GetOrCompile(&match.LabelSelector)
			if err != nil {
				return ruleengine.Match{}, err
			}

			stored, err := classes.get(ctx)
			if err != nil || stored == nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(stored.GetLabels()))}, nil
		},
		RuleDescription:    apirules.WorkloadClassMatch.Describe,
		AllowedDescription: class.allowed,
	})
	if classes.err != nil {
		return nil, classes.err
	}

	return evaluation, err
}

// missingClass handles a controller template without a class like a missing
// ingress hostname: allow and deny rules reject it, audit rules record it. Its
// Pods would get the cluster's default class when they are created, which a
// rule that only targets the controller never sees.
func (c *workloadClass) missingClass(object client.Object, bodies []*apirules.NamespaceRuleEnforceBody) *ruleengine.Evaluation {
	evaluation := &ruleengine.Evaluation{}
	value := ruleengine.Value{Path: c.path}
	audited, enforced := false, false

	for _, body := range bodies {
		if !c.applies(body) {
			continue
		}

		if body.Action.OrDefault() == apirules.ActionTypeAudit {
			audited = true
		} else {
			enforced = true
		}
	}

	if audited {
		evaluation.Audits = append(evaluation.Audits, &ruleengine.Decision{
			SetName:     c.set,
			EventReason: events.ReasonNamespaceRuleAudit,
			Action:      apirules.ActionTypeAudit,
			Value:       value,
			Message:     fmt.Sprintf("empty %s detected at %s by audit namespace rule", c.set, c.path),
		})
	}

	if enforced {
		target, _ := apirules.WorkloadTargetForGVK(object.GetObjectKind().GroupVersionKind())

		evaluation.Blocking = &ruleengine.Decision{
			SetName:     c.set,
			EventReason: c.missing,
			Action:      apirules.ActionTypeDeny,
			Value:       value,
			Message:     fmt.Sprintf("%s is required at %s because class rules target %s", c.set, c.path, target),
		}
	}

	return evaluation
}

// classesToMatch returns the entries of a rule to match against the class. The
// entries of an allow or deny rule are ORed, so the first one that matches the
// class by name decides the rule: matching the others could only read the class
// for nothing, and a failed read would turn the decision into a server error.
// Audit rules keep every entry, since each match is recorded.
func (h *podRules) classesToMatch(classes []apirules.WorkloadClassMatch, action apirules.ActionType, name string) []apirules.WorkloadClassMatch {
	// Without a selector no entry reads the class, so there is nothing to narrow.
	if action == apirules.ActionTypeAudit || !slices.ContainsFunc(classes, apirules.WorkloadClassMatch.HasSelector) {
		return classes
	}

	for i, match := range classes {
		if !match.HasNameMatch() {
			continue
		}

		matched, err := match.NameMatch().MatchesWithExpressionMatcher(h.regexCache, name)
		if err != nil {
			// Matching every entry reports the invalid expression.
			return classes
		}

		if matched {
			return classes[i : i+1]
		}
	}

	return classes
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

const (
	priorityClassSetName = "priority class"
	priorityClassPath    = "spec.priorityClassName"
)

// classLookupError is a failed read of a referenced class: a server error,
// never a rule decision.
type classLookupError struct{ err error }

func (e *classLookupError) Error() string { return e.err.Error() }

func (e *classLookupError) Unwrap() error { return e.err }

func appliesToPriorityClasses(body *apirules.NamespaceRuleEnforceBody) bool {
	return body != nil && len(body.Workloads.Classes.Priority) > 0 && body.GetWorkloadTargets(apirules.ValidatePod)
}

// priorityClassReader reads the Pod's PriorityClass at most once, and only when
// a selector has to be matched. A missing class never matches a selector.
type priorityClassReader struct {
	reader client.Reader
	name   string

	read  bool
	class *schedulingv1.PriorityClass
	err   error
}

func (c *priorityClassReader) get(ctx context.Context) (*schedulingv1.PriorityClass, error) {
	if c.read {
		return c.class, c.err
	}

	c.read = true

	class, err := utils.GetPriorityClassByName(ctx, c.reader, c.name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			c.err = &classLookupError{err: fmt.Errorf("get PriorityClass %q: %w", c.name, err)}
		}

		return nil, c.err
	}

	c.class = class

	return class, nil
}

// validatePriorityClasses matches the class a Pod or Pod template names. The
// built-in Priority admission plugin assigns the cluster's global default class
// before webhooks run, so an empty name means the Pod has no class at all. It
// resolves the class of a controller template only when its Pods are created.
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

	if pod.Spec.PriorityClassName == "" {
		if _, isPod := object.(*corev1.Pod); isPod {
			return nil, nil
		}

		return missingPriorityClass(object, enforceBodies), nil
	}

	name := pod.Spec.PriorityClassName
	classes := &priorityClassReader{reader: reader, name: name}

	evaluation, err := evaluatePodRules(pod, enforceBodies, podRuleSet[apirules.WorkloadClassMatch]{
		Name:        priorityClassSetName,
		EventReason: events.ReasonForbiddenPriorityClass,
		Values: func(*corev1.Pod) []ruleengine.Value {
			return []ruleengine.Value{{Value: name, Path: priorityClassPath}}
		},
		Rules: func(body *apirules.NamespaceRuleEnforceBody) []apirules.WorkloadClassMatch {
			if !appliesToPriorityClasses(body) {
				return nil
			}

			return h.priorityClassesToMatch(body, name)
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

			class, err := classes.get(ctx)
			if err != nil || class == nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(class.Labels))}, nil
		},
		RuleDescription:    apirules.WorkloadClassMatch.Describe,
		AllowedDescription: "Allowed priority classes",
	})
	if classes.err != nil {
		return nil, classes.err
	}

	return evaluation, err
}

// missingPriorityClass handles a controller template without a class like a
// missing ingress hostname: allow and deny rules reject it, audit rules record
// it. Its Pods would get the cluster's global default class when they are
// created, which a rule that only targets the controller never sees.
func missingPriorityClass(object client.Object, bodies []*apirules.NamespaceRuleEnforceBody) *ruleengine.Evaluation {
	evaluation := &ruleengine.Evaluation{}
	value := ruleengine.Value{Path: priorityClassPath}
	audited, enforced := false, false

	for _, body := range bodies {
		if !appliesToPriorityClasses(body) {
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
			SetName:     priorityClassSetName,
			EventReason: events.ReasonNamespaceRuleAudit,
			Action:      apirules.ActionTypeAudit,
			Value:       value,
			Message:     fmt.Sprintf("empty priority class detected at %s by audit namespace rule", priorityClassPath),
		})
	}

	if enforced {
		target, _ := apirules.WorkloadTargetForGVK(object.GetObjectKind().GroupVersionKind())

		evaluation.Blocking = &ruleengine.Decision{
			SetName:     priorityClassSetName,
			EventReason: events.ReasonMissingPriorityClass,
			Action:      apirules.ActionTypeDeny,
			Value:       value,
			Message:     fmt.Sprintf("priority class is required at %s because class rules target %s", priorityClassPath, target),
		}
	}

	return evaluation
}

// priorityClassesToMatch returns the entries of a rule to match against the
// class. The entries of an allow or deny rule are ORed, so the first one that
// matches the class by name decides the rule: matching the others could only
// read the class for nothing, and a failed read would turn the decision into a
// server error. Audit rules keep every entry, since each match is recorded.
func (h *podRules) priorityClassesToMatch(body *apirules.NamespaceRuleEnforceBody, name string) []apirules.WorkloadClassMatch {
	classes := body.Workloads.Classes.Priority
	// Without a selector no entry reads the class, so there is nothing to narrow.
	if body.Action.OrDefault() == apirules.ActionTypeAudit || !slices.ContainsFunc(classes, apirules.WorkloadClassMatch.HasSelector) {
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

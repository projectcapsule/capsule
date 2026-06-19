// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func (h *podRules) validateSchedulers(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return evaluatePodRules[api.ExpressionMatch](
		pod,
		enforceBodies,
		podRuleSet[api.ExpressionMatch]{
			Name:        "scheduler",
			EventReason: events.ReasonForbiddenPodScheduler,
			Values: func(pod *corev1.Pod) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: strings.TrimSpace(pod.Spec.SchedulerName),
						Path:  "spec.schedulerName",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []api.ExpressionMatch {
				if enforce == nil {
					return nil
				}

				return enforce.Workloads.Schedulers
			},
			Matches: func(match api.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
				matched, err := match.MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				return ruleengine.Match{
					Matched: matched,
				}, nil
			},
		},
	)
}

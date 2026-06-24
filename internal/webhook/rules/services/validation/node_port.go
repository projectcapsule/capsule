// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func (h *serviceRules) validateNodePorts(
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if svc == nil || !serviceTypeIsNodePort(svc) {
		return nil, nil
	}

	if requiresNodePortRanges(enforceBodies) && len(nodePortValues(svc)) == 0 {
		return &ruleengine.Evaluation{
			Blocking: &ruleengine.Decision{
				SetName:     "nodePort",
				EventReason: events.ReasonForbiddenNodePort,
				Action:      apirules.ActionTypeDeny,
				Value: ruleengine.Value{
					Value: string(svc.Spec.Type),
					Path:  "spec.type",
				},
				Message: "service requires explicit spec.ports[*].nodePort because nodePort ranges are enforced by namespace rule",
			},
		}, nil
	}

	values := nodePortValues(svc)
	if len(values) == 0 {
		return nil, nil
	}

	return evaluateServiceRules[apirules.ServiceNodePortRange](
		svc,
		enforceBodies,
		serviceRuleSet[apirules.ServiceNodePortRange]{
			Name:        "nodePort",
			EventReason: events.ReasonForbiddenNodePort,
			Values: func(_ *corev1.Service) []ruleengine.Value {
				return values
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []apirules.ServiceNodePortRange {
				if enforce == nil || enforce.Services.NodePorts == nil {
					return nil
				}

				return enforce.Services.NodePorts.Ports
			},
			Matches: func(r apirules.ServiceNodePortRange, value ruleengine.Value) (ruleengine.Match, error) {
				if r.From > r.To {
					return ruleengine.Match{}, fmt.Errorf(
						"invalid nodePort range: from %d must be lower than or equal to %d",
						r.From,
						r.To,
					)
				}

				port, err := portFromValue(value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				matched := port >= r.From && port <= r.To

				match := ruleengine.Match{
					Matched:      matched,
					MatchedValue: describeNodePortRange(r),
				}

				if matched {
					match.Detail = fmt.Sprintf("nodePort %d is within allowed range %s", port, describeNodePortRange(r))
				}

				return match, nil
			},
			RuleDescription:    describeNodePortRange,
			AllowedDescription: "Allowed ranges",
		},
	)
}

func describeNodePortRange(r apirules.ServiceNodePortRange) string {
	if r.From == r.To {
		return fmt.Sprintf("%d", r.From)
	}

	return fmt.Sprintf("%d-%d", r.From, r.To)
}

func nodePortValues(svc *corev1.Service) []ruleengine.Value {
	out := make([]ruleengine.Value, 0, len(svc.Spec.Ports))

	for i := range svc.Spec.Ports {
		port := svc.Spec.Ports[i]
		if port.NodePort == 0 {
			continue
		}

		out = append(out, ruleengine.Value{
			Value: fmt.Sprintf("%d", port.NodePort),
			Path:  fmt.Sprintf("spec.ports[%d].nodePort", i),
		})
	}

	return out
}

func requiresNodePortRanges(
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) bool {
	for _, enforce := range enforceBodies {
		if enforce == nil ||
			enforce.Services.NodePorts == nil {
			continue
		}

		if len(enforce.Services.NodePorts.Ports) > 0 {
			return true
		}
	}

	return false
}

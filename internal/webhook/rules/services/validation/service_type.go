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

func (h *serviceRules) validateServiceTypes(
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return evaluateServiceRules[apirules.ServiceType](
		svc,
		enforceBodies,
		serviceRuleSet[apirules.ServiceType]{
			Name:        "service type",
			EventReason: events.ReasonForbiddenServiceType,
			Values: func(svc *corev1.Service) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: string(serviceType(svc)),
						Path:  "spec.type",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []apirules.ServiceType {
				if enforce == nil {
					return nil
				}

				return enforce.Services.Types
			},
			Matches: func(rule apirules.ServiceType, value ruleengine.Value) (ruleengine.Match, error) {
				matched := string(rule) == value.Value

				match := ruleengine.Match{
					Matched:      matched,
					MatchedValue: rule,
				}

				if matched {
					match.Detail = fmt.Sprintf("service type %q matched %q", value.Value, rule)
				}

				return match, nil
			},
			RuleDescription: func(rule apirules.ServiceType) string {
				return string(rule)
			},
			AllowedDescription: "Allowed service types",
		},
	)
}

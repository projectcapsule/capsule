// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func (h *serviceRules) validateExternalNames(
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if svc == nil || svc.Spec.Type != corev1.ServiceTypeExternalName {
		return nil, nil
	}

	if strings.TrimSpace(svc.Spec.ExternalName) == "" {
		return nil, nil
	}

	return evaluateServiceRules[runtime.ExpressionMatch](
		svc,
		enforceBodies,
		serviceRuleSet[runtime.ExpressionMatch]{
			Name:        "externalName hostname",
			EventReason: events.ReasonForbiddenExternalName,
			Values: func(svc *corev1.Service) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: strings.TrimSpace(svc.Spec.ExternalName),
						Path:  "spec.externalName",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []runtime.ExpressionMatch {
				if enforce == nil || enforce.Services.ExternalNames == nil {
					return nil
				}

				return enforce.Services.ExternalNames.Hostnames
			},
			Matches: func(match runtime.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
				matched, err := match.MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				out := ruleengine.Match{
					Matched:      matched,
					MatchedValue: describeExpressionMatch(match),
				}

				if matched {
					out.Detail = fmt.Sprintf("%q matched hostname rule %s", value.Value, describeExpressionMatch(match))
				}

				return out, nil
			},
			RuleDescription:    describeExpressionMatch,
			AllowedDescription: "Allowed hostnames",
		},
	)
}

func describeExpressionMatch(match runtime.ExpressionMatch) string {
	parts := make([]string, 0, 2)

	if len(match.Exact) > 0 {
		parts = append(parts, fmt.Sprintf("exact: %s", strings.Join(match.Exact, ", ")))
	}

	if match.Expression != "" {
		parts = append(parts, fmt.Sprintf("exp: %s", match.Expression))
	}

	return strings.Join(parts, "; ")
}

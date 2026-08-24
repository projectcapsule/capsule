// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type externalIPCIDRRule struct {
	CIDRs    []string
	MatchAll bool
}

func (h *serviceRules) validateExternalIPs(
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return evaluateServiceRules[externalIPCIDRRule](
		svc,
		enforceBodies,
		serviceRuleSet[externalIPCIDRRule]{
			Name:        "external IP",
			EventReason: events.ReasonForbiddenExternalServiceIP,
			Values:      externalIPValues,
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []externalIPCIDRRule {
				if enforce == nil || enforce.Services.ExternalIPs == nil {
					return nil
				}

				if len(enforce.Services.ExternalIPs.CIDRs) == 0 {
					if enforce.Action.OrDefault() == apirules.ActionTypeDeny {
						return []externalIPCIDRRule{
							{
								MatchAll: true,
							},
						}
					}

					return nil
				}

				cidrs := make([]string, 0, len(enforce.Services.ExternalIPs.CIDRs))
				for _, cidr := range enforce.Services.ExternalIPs.CIDRs {
					cidr = strings.TrimSpace(cidr)
					if cidr == "" {
						continue
					}

					cidrs = append(cidrs, cidr)
				}

				if len(cidrs) == 0 {
					return nil
				}

				return []externalIPCIDRRule{
					{
						CIDRs: cidrs,
					},
				}
			},
			Matches: func(rule externalIPCIDRRule, value ruleengine.Value) (ruleengine.Match, error) {
				if rule.MatchAll {
					return ruleengine.Match{
						Matched:      true,
						MatchedValue: "all external IPs",
						Detail:       "deny rule applies to all external IPs",
					}, nil
				}

				ip := net.ParseIP(value.Value)
				if ip == nil {
					return ruleengine.Match{}, fmt.Errorf(
						"%s contains invalid IP %q",
						value.Path,
						value.Value,
					)
				}

				for _, rawCIDR := range rule.CIDRs {
					allowedCIDR, err := parseCIDR(rawCIDR)
					if err != nil {
						return ruleengine.Match{}, fmt.Errorf("invalid external IP CIDR %q: %w", rawCIDR, err)
					}

					if !cidrContainsIP(allowedCIDR, ip) {
						continue
					}

					return ruleengine.Match{
						Matched:      true,
						MatchedValue: rawCIDR,
						Detail:       fmt.Sprintf("%s is contained in %s", value.Value, rawCIDR),
					}, nil
				}

				return ruleengine.Match{}, nil
			},
			RuleDescription: func(rule externalIPCIDRRule) string {
				if rule.MatchAll {
					return "all external IPs"
				}

				return strings.Join(rule.CIDRs, ", ")
			},
			AllowedDescription: "Allowed CIDRs",
		},
	)
}

func externalIPValues(svc *corev1.Service) []ruleengine.Value {
	out := make([]ruleengine.Value, 0, len(svc.Spec.ExternalIPs))

	for i, externalIP := range svc.Spec.ExternalIPs {
		out = append(out, ruleengine.Value{
			Value: externalIP,
			Path:  fmt.Sprintf("spec.externalIPs[%d]", i),
		})
	}

	return out
}

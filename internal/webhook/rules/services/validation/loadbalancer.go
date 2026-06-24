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

type loadBalancerCIDRRule struct {
	CIDRs []string
}

func (h *serviceRules) validateLoadBalancers(
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if svc == nil || svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil, nil
	}

	if requiresLoadBalancerCIDRs(enforceBodies) && len(loadBalancerCIDRValues(svc)) == 0 {
		return &ruleengine.Evaluation{
			Blocking: &ruleengine.Decision{
				SetName:     "loadBalancer CIDR",
				EventReason: events.ReasonForbiddenLoadBalancerCIDR,
				Action:      apirules.ActionTypeDeny,
				Value: ruleengine.Value{
					Value: string(corev1.ServiceTypeLoadBalancer),
					Path:  "spec.type",
				},
				Message: "loadBalancer service requires spec.loadBalancerIP or spec.loadBalancerSourceRanges because loadBalancer CIDR constraints are enforced by namespace rule",
			},
		}, nil
	}

	values := loadBalancerCIDRValues(svc)
	if len(values) == 0 {
		return nil, nil
	}

	return evaluateServiceRules[loadBalancerCIDRRule](
		svc,
		enforceBodies,
		serviceRuleSet[loadBalancerCIDRRule]{
			Name:        "loadBalancer CIDR",
			EventReason: events.ReasonForbiddenLoadBalancerCIDR,
			Values: func(_ *corev1.Service) []ruleengine.Value {
				return values
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []loadBalancerCIDRRule {
				if enforce == nil ||
					enforce.Services.LoadBalancers == nil ||
					len(enforce.Services.LoadBalancers.CIDRs) == 0 {
					return nil
				}

				cidrs := make([]string, 0, len(enforce.Services.LoadBalancers.CIDRs))
				for _, cidr := range enforce.Services.LoadBalancers.CIDRs {
					cidr = strings.TrimSpace(cidr)
					if cidr == "" {
						continue
					}

					cidrs = append(cidrs, cidr)
				}

				if len(cidrs) == 0 {
					return nil
				}

				return []loadBalancerCIDRRule{
					{
						CIDRs: cidrs,
					},
				}
			},
			Matches: func(rule loadBalancerCIDRRule, value ruleengine.Value) (ruleengine.Match, error) {
				if len(rule.CIDRs) == 0 {
					return ruleengine.Match{}, nil
				}

				for _, rawCIDR := range rule.CIDRs {
					allowedCIDR, err := parseCIDR(rawCIDR)
					if err != nil {
						return ruleengine.Match{}, fmt.Errorf("invalid loadBalancer CIDR %q: %w", rawCIDR, err)
					}

					if ip := net.ParseIP(value.Value); ip != nil {
						if !cidrContainsIP(allowedCIDR, ip) {
							continue
						}

						return ruleengine.Match{
							Matched:      true,
							MatchedValue: rawCIDR,
							Detail:       fmt.Sprintf("%s is contained in %s", value.Value, rawCIDR),
						}, nil
					}

					_, requestedCIDR, err := net.ParseCIDR(value.Value)
					if err != nil {
						return ruleengine.Match{}, fmt.Errorf(
							"%s contains invalid IP or CIDR %q: %w",
							value.Path,
							value.Value,
							err,
						)
					}

					if !cidrContainsCIDR(allowedCIDR, requestedCIDR) {
						continue
					}

					return ruleengine.Match{
						Matched:      true,
						MatchedValue: rawCIDR,
						Detail:       fmt.Sprintf("%s is contained in %s", value.Value, rawCIDR),
					}, nil
				}

				return ruleengine.Match{
					Matched: false,
				}, nil
			},
			RuleDescription: func(rule loadBalancerCIDRRule) string {
				return strings.Join(rule.CIDRs, ", ")
			},
			AllowedDescription: "Allowed CIDRs",
		},
	)
}

func requiresLoadBalancerCIDRs(
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) bool {
	for _, enforce := range enforceBodies {
		if enforce == nil ||
			enforce.Services.LoadBalancers == nil {
			continue
		}

		if len(enforce.Services.LoadBalancers.CIDRs) > 0 {
			return true
		}
	}

	return false
}

func parseCIDR(raw string) (*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("CIDR is empty")
	}

	if !strings.Contains(raw, "/") {
		ip := net.ParseIP(raw)
		if ip == nil {
			return nil, fmt.Errorf("invalid CIDR %q", raw)
		}

		if ip.To4() != nil {
			raw += "/32"
		} else {
			raw += "/128"
		}
	}

	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		return nil, err
	}

	return network, nil
}

func loadBalancerCIDRValues(svc *corev1.Service) []ruleengine.Value {
	out := make([]ruleengine.Value, 0, 1+len(svc.Spec.LoadBalancerSourceRanges))

	if svc.Spec.LoadBalancerIP != "" {
		out = append(out, ruleengine.Value{
			Value: svc.Spec.LoadBalancerIP,
			Path:  "spec.loadBalancerIP",
		})
	}

	for i, sourceRange := range svc.Spec.LoadBalancerSourceRanges {
		out = append(out, ruleengine.Value{
			Value: sourceRange,
			Path:  fmt.Sprintf("spec.loadBalancerSourceRanges[%d]", i),
		})
	}

	return out
}

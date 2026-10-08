// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// Run the same grant fixtures in either direction without aliasing shared input.
func ingressFixtures(obj *networkingv1.NetworkPolicy, bodies []*rules.NamespaceRuleEnforceBody) (*networkingv1.NetworkPolicy, []*rules.NamespaceRuleEnforceBody) {
	obj = obj.DeepCopy()
	if obj != nil {
		for _, rule := range obj.Spec.Egress {
			obj.Spec.Ingress = append(obj.Spec.Ingress, networkingv1.NetworkPolicyIngressRule{From: rule.To, Ports: rule.Ports})
		}
		obj.Spec.Egress = nil
		for i, direction := range obj.Spec.PolicyTypes {
			if direction == networkingv1.PolicyTypeEgress {
				obj.Spec.PolicyTypes[i] = networkingv1.PolicyTypeIngress
			} else {
				obj.Spec.PolicyTypes[i] = networkingv1.PolicyTypeEgress
			}
		}
	}
	converted := make([]*rules.NamespaceRuleEnforceBody, len(bodies))
	for i, body := range bodies {
		if body != nil {
			converted[i] = body.DeepCopy()
			converted[i].Network.Policies.Ingress, converted[i].Network.Policies.Egress = converted[i].Network.Policies.Egress, nil
		}
	}
	return obj, converted
}

func TestNetworkPolicyDirectionsIndependent(t *testing.T) {
	obj := policy("10.20.0.0/16")
	obj.Spec.PolicyTypes = append(obj.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{From: obj.Spec.Egress[0].To}}
	for _, tc := range []struct {
		name            string
		egress, ingress rules.ActionType
		denied          string
	}{
		{"both allow", rules.ActionTypeAllow, rules.ActionTypeAllow, ""},
		{"ingress allow cannot override egress deny", rules.ActionTypeDeny, rules.ActionTypeAllow, "egress"},
		{"egress allow cannot override ingress deny", rules.ActionTypeAllow, rules.ActionTypeDeny, "ingress"},
		{"both audit", rules.ActionTypeAudit, rules.ActionTypeAudit, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ingress := ingressFixtures(nil, []*rules.NamespaceRuleEnforceBody{cidrRule(tc.ingress, "10.0.0.0/8")})
			bodies := append([]*rules.NamespaceRuleEnforceBody{cidrRule(tc.egress, "10.0.0.0/8")}, ingress...)
			original := obj.DeepCopy()
			result, err := evaluate(t.Context(), obj, bodies)
			require.NoError(t, err)
			if tc.denied == "" {
				require.NoError(t, result.BlockingError())
			} else {
				require.ErrorContains(t, result.BlockingError(), "networkPolicy "+tc.denied+" CIDR")
			}
			if tc.egress == rules.ActionTypeAudit {
				require.Len(t, result.Audits, 2)
				require.Contains(t, result.Audits[0].Message, "spec.egress[0].to[0]")
				require.Contains(t, result.Audits[1].Message, "spec.ingress[0].from[0]")
			}
			require.Equal(t, original, obj)
		})
	}

	// Only the direction with an allow list is restricted by its allow misses.
	for _, ingress := range []bool{false, true} {
		body := cidrRule(rules.ActionTypeAllow, "10.0.0.0/8")
		current := obj.DeepCopy()
		if ingress {
			_, converted := ingressFixtures(nil, []*rules.NamespaceRuleEnforceBody{body})
			body = converted[0]
			current.Spec.Egress[0].To[0].IPBlock.CIDR = "192.0.2.0/24"
		} else {
			current.Spec.Ingress[0].From[0].IPBlock.CIDR = "192.0.2.0/24"
		}
		result, err := evaluate(t.Context(), current, []*rules.NamespaceRuleEnforceBody{body})
		require.NoError(t, err)
		require.NoError(t, result.BlockingError())
	}
}

func TestIngressMultipleRulesAndCancellation(t *testing.T) {
	obj, bodies := ingressFixtures(policy("192.0.2.0/24"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "10.20.0.0/16")})
	obj.Spec.Ingress = append(obj.Spec.Ingress, networkingv1.NetworkPolicyIngressRule{From: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.20.0.0/16"}}}})
	result, err := evaluate(t.Context(), obj, bodies)
	require.NoError(t, err)
	require.ErrorContains(t, result.BlockingError(), "spec.ingress[1].from[0]")
	require.Equal(t, events.ReasonForbiddenNetworkPolicyIngressCIDR, result.Blocking.EventReason)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &cancelDuringEvaluation{Context: ctx, cancel: cancel, remaining: 50}
	large, _ := ingressFixtures(exceptionPolicy(8192), nil)
	_, err = evaluate(probe, large, bodies)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIngressAuditRequestsConcurrent(t *testing.T) {
	obj, bodies := auditPolicy(64, 8)
	obj, bodies = ingressFixtures(obj, bodies)
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for range 2 {
				result, err := evaluate(t.Context(), obj, bodies)
				require.NoError(t, err)
				require.NoError(t, result.BlockingError())
				require.Len(t, result.Audits, 64*8)
			}
		})
	}
}

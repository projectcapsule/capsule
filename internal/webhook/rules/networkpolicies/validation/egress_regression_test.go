// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func exceptionPolicy(count int) *networkingv1.NetworkPolicy {
	obj := policy("10.0.0.0/8")
	for i := range count {
		n := 2 * i
		obj.Spec.Egress[0].To[0].IPBlock.Except = append(obj.Spec.Egress[0].To[0].IPBlock.Except, fmt.Sprintf("10.%d.%d.%d/32", n>>16, (n>>8)&255, n&255))
	}
	return obj
}

func auditPolicy(count, peers int) (*networkingv1.NetworkPolicy, []*rules.NamespaceRuleEnforceBody) {
	obj := policy("::/0")
	peer := obj.Spec.Egress[0].To[0]
	obj.Spec.Egress[0].To = make([]networkingv1.NetworkPolicyPeer, peers)
	for i := range peers {
		obj.Spec.Egress[0].To[i] = peer
	}
	var cidrs []string
	for bits := range count {
		cidrs = append(cidrs, fmt.Sprintf("::/%d", bits))
	}
	return obj, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAudit, cidrs...)}
}

func TestEgressAuditsDeduplicatePartitions(t *testing.T) {
	for _, peers := range []int{1, 8, 32} {
		t.Run(fmt.Sprint(peers), func(t *testing.T) {
			obj, bodies := auditPolicy(64, peers)
			// An identical audit in another body has the same emitted message.
			bodies = append(bodies, cidrRule(rules.ActionTypeAudit, "::/0"))
			for range 2 { // Deduplication must remain request-local.
				result, err := evaluate(t.Context(), obj, bodies)
				require.NoError(t, err)
				require.NoError(t, result.BlockingError())
				require.Equal(t, 64*peers, len(result.Audits))
				seen := make(map[string]struct{})
				for _, audit := range result.Audits {
					require.NotContains(t, seen, audit.Message)
					seen[audit.Message] = struct{}{}
				}
			}
		})
	}
}

func TestEgressAuditDeduplicationPreservesDecisions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bodies []*rules.NamespaceRuleEnforceBody
		denied bool
	}{
		{"audit does not satisfy allow", []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAudit, "10.0.0.0/8"), cidrRule(rules.ActionTypeAllow, "10.20.0.0/16")}, true},
		{"same prefix deny then allow", []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAudit, "10.0.0.0/8"), cidrRule(rules.ActionTypeDeny, "10.0.0.0/8"), cidrRule(rules.ActionTypeAllow, "10.0.0.0/8")}, false},
		{"same prefix allow then deny", []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAudit, "10.0.0.0/8"), cidrRule(rules.ActionTypeAllow, "10.0.0.0/8"), cidrRule(rules.ActionTypeDeny, "10.0.0.0/8")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluate(t.Context(), policy("10.0.0.0/8"), tc.bodies)
			require.NoError(t, err)
			require.Equal(t, tc.denied, result.BlockingError() != nil)
			require.Len(t, result.Audits, 1)
		})
	}
}

func TestEgressExceptionSweep(t *testing.T) {
	for _, tc := range []struct {
		name, cidr, deny string
		except           []string
		denied           bool
	}{
		{"nested unsorted duplicates", "10.0.0.0/8", "10.20.0.0/16", []string{"10.20.128.0/17", "10.20.0.0/16", "10.20.0.0/17", "10.20.0.0/16"}, false},
		{"adjacent cover grant", "10.20.0.0/16", "0.0.0.0/0", []string{"10.20.128.0/17", "10.20.0.0/17"}, false},
		{"gap remains denied", "10.20.0.0/16", "0.0.0.0/0", []string{"10.20.0.0/18", "10.20.128.0/17"}, true},
		{"IPv6 nested unsorted", "::/0", "fd00::/8", []string{"fd80::/9", "fd00::/8", "fd00::/9"}, false},
		{"IPv6 maximum exception", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffc/126", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128", []string{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128"}, false},
		{"wrong family rejected", "10.0.0.0/8", "10.0.0.0/8", []string{"fd00::/16"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := policy(tc.cidr, tc.except...)
			original := obj.DeepCopy()
			result, err := evaluate(t.Context(), obj, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, tc.deny)})
			if tc.name == "wrong family rejected" {
				require.ErrorContains(t, err, "except[0]")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.denied, result.BlockingError() != nil)
			}
			require.Equal(t, original, obj)
		})
	}
	obj := exceptionPolicy(8192)
	result, err := evaluate(t.Context(), obj, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "10.0.0.0/32")})
	require.NoError(t, err)
	require.NoError(t, result.BlockingError())
	result, err = evaluate(t.Context(), obj, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "10.0.0.1/32")})
	require.NoError(t, err)
	require.Error(t, result.BlockingError())
}

// Cancel deterministically while work is underway, without wall-clock sleeps.
type cancelDuringEvaluation struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *cancelDuringEvaluation) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestEgressCancellationDuringPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &cancelDuringEvaluation{Context: ctx, cancel: cancel, remaining: 50}
	_, err := evaluate(probe, exceptionPolicy(1000), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "192.0.2.0/24")})
	require.ErrorIs(t, err, context.Canceled)
}

func TestEgressCancellationDuringPartitions(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &cancelDuringEvaluation{Context: ctx, cancel: cancel, remaining: 50}
	var boundaries []netip.Addr
	for i := range 1000 {
		boundaries = append(boundaries, netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}))
	}
	_, err := appendGrant(probe, nil, netip.MustParsePrefix("10.0.0.0/8"), nil, boundaries, "spec.egress[0].to")
	require.ErrorIs(t, err, context.Canceled)
}

func TestEgressCancellationDuringRuleEvaluation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &cancelDuringEvaluation{Context: ctx, cancel: cancel, remaining: 512}
	obj, bodies := auditPolicy(64, 1)
	_, err := evaluate(probe, obj, bodies)
	require.ErrorIs(t, err, context.Canceled)
}

func TestEgressAuditRequestsConcurrent(t *testing.T) {
	obj, bodies := auditPolicy(64, 8)
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			result, err := evaluate(t.Context(), obj, bodies)
			require.NoError(t, err)
			require.Equal(t, 64*8, len(result.Audits))
		})
	}
}

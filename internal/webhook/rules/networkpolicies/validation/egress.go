// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"net/netip"
	"slices"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func hasEgressCIDRs(body *rules.NamespaceRuleEnforceBody) bool {
	return body.Network.Policies.Egress != nil && len(body.Network.Policies.Egress.CIDRs) > 0
}

// evaluate partitions grants at rule and exception boundaries. Membership of
// every CIDR is constant inside each partition, so one address per partition
// proves coverage of the entire grant, including IPv6 /0, without enumerating
// addresses. The existing evaluator retains ordered decisions and allow misses.
func evaluate(ctx context.Context, obj *networkingv1.NetworkPolicy, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	if obj == nil || len(obj.Spec.Egress) == 0 ||
		(len(obj.Spec.PolicyTypes) > 0 && !slices.Contains(obj.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)) {
		return nil, nil
	}

	// Prefix parsing is cheap, allocation-free, and done once per rule per
	// request, never per peer or partition. No cluster lookup or mutable shared
	// state is needed; the supplied effective rules are the source of truth.
	prepared := make(map[*rules.NamespaceRuleEnforceBody][]netip.Prefix)

	var boundaries []netip.Addr

	for _, body := range bodies {
		if body == nil || !hasEgressCIDRs(body) {
			continue
		}

		prefixes := make([]netip.Prefix, 0, len(body.Network.Policies.Egress.CIDRs))

		for _, cidr := range body.Network.Policies.Egress.CIDRs {
			prefix, err := parsePrefix(cidr)
			if err != nil {
				return nil, fmt.Errorf("networkPolicy egress CIDR rule: %w", err)
			}

			prefixes = append(prefixes, prefix)
			boundaries = appendBoundaries(boundaries, prefix)
		}

		prepared[body] = prefixes
	}

	if len(prepared) == 0 {
		return nil, nil
	}

	slices.SortFunc(boundaries, netip.Addr.Compare)
	boundaries = slices.Compact(boundaries)

	set := ruleengine.Set[netip.Prefix, []ruleengine.Value]{
		Name:        "networkPolicy egress CIDR",
		EventReason: events.ReasonForbiddenNetworkPolicyEgressCIDR,
		Values:      func(values []ruleengine.Value) []ruleengine.Value { return values },
		Rules:       func(body *rules.NamespaceRuleEnforceBody) []netip.Prefix { return prepared[body] },
		Matches: func(prefix netip.Prefix, value ruleengine.Value) (ruleengine.Match, error) {
			address, ok := value.Data.(netip.Addr)
			if !ok || !prefix.Contains(address) {
				return ruleengine.Match{}, nil
			}

			return ruleengine.Match{Matched: true, MatchedValue: prefix.String()}, nil
		},
		RuleDescription:    netip.Prefix.String,
		AllowedDescription: "Allowed egress CIDRs",
		Message: func(action rules.ActionType, value ruleengine.Value, matched any) string {
			return fmt.Sprintf("networkPolicy egress CIDR %q at %s matches %s rule for CIDR %v", value.Value, value.Path, action, matched)
		},
	}
	result := &ruleengine.Evaluation{}
	err := walkEgressValues(ctx, obj, boundaries, func(values []ruleengine.Value) error {
		evaluation, err := ruleengine.EvaluateEnforce(values, bodies, set)
		if err != nil {
			return err
		}

		result.Append(evaluation)

		return evaluation.BlockingError()
	})

	if result.Blocking != nil {
		return result, nil
	}

	return result, err
}

// Process one peer at a time, bounding temporary storage by rule/exception
// boundaries rather than multiplying it by every peer in an untrusted object.
func walkEgressValues(ctx context.Context, obj *networkingv1.NetworkPolicy, boundaries []netip.Addr, visit func([]ruleengine.Value) error) error {
	for i, rule := range obj.Spec.Egress {
		if err := ctx.Err(); err != nil {
			return err
		}

		path := fmt.Sprintf("spec.egress[%d].to", i)
		if len(rule.To) == 0 {
			if err := visit(appendUnrestricted(nil, boundaries, path)); err != nil {
				return err
			}
		}

		for j, peer := range rule.To {
			if err := ctx.Err(); err != nil {
				return err
			}

			peerPath := fmt.Sprintf("%s[%d]", path, j)

			if peer.IPBlock == nil {
				if peer.PodSelector == nil && peer.NamespaceSelector == nil {
					if err := visit(appendUnrestricted(nil, boundaries, peerPath)); err != nil {
						return err
					}
				}

				continue
			}

			prefix, err := parsePrefix(peer.IPBlock.CIDR)
			if err != nil {
				return fmt.Errorf("%s.ipBlock.cidr: %w", peerPath, err)
			}

			exceptions := make([]netip.Prefix, 0, len(peer.IPBlock.Except))

			for k, cidr := range peer.IPBlock.Except {
				except, err := parsePrefix(cidr)
				if err != nil || except.Bits() <= prefix.Bits() || !prefix.Contains(except.Addr()) {
					return fmt.Errorf("%s.ipBlock.except[%d]: %q must be a CIDR strictly inside %s", peerPath, k, cidr, prefix)
				}

				exceptions = append(exceptions, except)
			}

			if err := visit(appendGrant(nil, prefix, exceptions, boundaries, peerPath+".ipBlock.cidr")); err != nil {
				return err
			}
		}
	}

	return nil
}

func appendUnrestricted(values []ruleengine.Value, boundaries []netip.Addr, path string) []ruleengine.Value {
	values = appendGrant(values, netip.PrefixFrom(netip.IPv4Unspecified(), 0), nil, boundaries, path)

	return appendGrant(values, netip.PrefixFrom(netip.IPv6Unspecified(), 0), nil, boundaries, path)
}

func appendGrant(values []ruleengine.Value, prefix netip.Prefix, exceptions []netip.Prefix, boundaries []netip.Addr, path string) []ruleengine.Value {
	points := []netip.Addr{prefix.Addr()}

	start, _ := slices.BinarySearchFunc(boundaries, prefix.Addr(), netip.Addr.Compare)
	for _, point := range boundaries[start:] {
		if !prefix.Contains(point) {
			break
		}

		points = append(points, point)
	}

	for _, except := range exceptions {
		points = appendBoundaries(points, except)
	}

	slices.SortFunc(points, netip.Addr.Compare)
	points = slices.Compact(points)
	value := prefix.String()

	for _, point := range points {
		if !prefix.Contains(point) || slices.ContainsFunc(exceptions, func(except netip.Prefix) bool { return except.Contains(point) }) {
			continue
		}

		values = append(values, ruleengine.Value{Value: value, Path: path, Data: point})
	}

	return values
}

func parsePrefix(raw string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("invalid IPv4 or IPv6 CIDR %q", raw)
	}

	return prefix.Masked(), nil
}

func appendBoundaries(points []netip.Addr, prefix netip.Prefix) []netip.Addr {
	points = append(points, prefix.Addr())
	last := prefix.Addr().As16()

	bits := prefix.Bits()
	if prefix.Addr().Is4() {
		bits += 96
	}

	for bit := bits; bit < 128; bit++ {
		last[bit/8] |= 1 << (7 - bit%8)
	}

	end := netip.AddrFrom16(last)
	if prefix.Addr().Is4() {
		end = end.Unmap()
	}

	if next := end.Next(); next.IsValid() {
		points = append(points, next)
	}

	return points
}

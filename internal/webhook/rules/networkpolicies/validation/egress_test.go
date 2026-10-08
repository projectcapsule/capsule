// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func cidrRule(action rules.ActionType, cidrs ...string) *rules.NamespaceRuleEnforceBody {
	return &rules.NamespaceRuleEnforceBody{Action: action, Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: cidrs}}}}
}

func policy(cidr string, except ...string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr, Except: except}}}}},
	}}
}

func TestEgressCIDRGrants(t *testing.T) {
	deny := cidrRule(rules.ActionTypeDeny, "10.20.0.0/16")
	allow := cidrRule(rules.ActionTypeAllow, "10.20.0.0/17", "10.20.128.0/17")
	all := policy("0.0.0.0/0")
	all.Spec.Egress[0].To = nil
	emptyPeer := policy("0.0.0.0/0")
	emptyPeer.Spec.Egress[0].To = []networkingv1.NetworkPolicyPeer{{}}
	selectors := policy("0.0.0.0/0")
	selectors.Spec.Egress[0].To = []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}, {NamespaceSelector: &metav1.LabelSelector{}}}
	ingress := policy("10.20.0.0/16")
	ingress.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	implicit := policy("10.20.0.0/16")
	implicit.Spec.PolicyTypes = nil
	lastIPv4 := cidrRule(rules.ActionTypeDeny, "255.255.255.255/32")
	cases := []struct {
		name   string
		obj    *networkingv1.NetworkPolicy
		bodies []*rules.NamespaceRuleEnforceBody
		denied bool
		err    string
		audit  bool
	}{
		{"subnet", policy("10.20.4.0/24"), []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"supernet", policy("10.0.0.0/8"), []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"universal", policy("0.0.0.0/0"), []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"excluded", policy("10.0.0.0/8", "10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"multiple exclusions cover deny", policy("10.0.0.0/8", "10.20.0.0/17", "10.20.128.0/17"), []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"partial exclusion", policy("10.0.0.0/8", "10.20.0.0/17"), []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"disjoint", policy("10.30.0.0/16"), []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"host bits normalized", policy("10.20.4.7/24"), []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"all peers omitted", all, []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"empty peer", emptyPeer, []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"selectors unaffected", selectors, []*rules.NamespaceRuleEnforceBody{deny, allow}, false, "", false},
		{"no grants", &networkingv1.NetworkPolicy{}, []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"ingress only", ingress, []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"default policy types", implicit, []*rules.NamespaceRuleEnforceBody{deny}, true, "", false},
		{"nil policy", nil, []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"no rules", all, nil, false, "", false},
		{"empty cidrs", all, []*rules.NamespaceRuleEnforceBody{nil, {}, cidrRule(rules.ActionTypeAllow)}, false, "", false},
		{"default action", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{cidrRule("", "10.20.0.0/16")}, true, "", false},
		{"audit", policy("0.0.0.0/0"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAudit, "10.20.0.0/16")}, false, "", true},
		{"allow union", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{allow}, false, "", false},
		{"allow miss", policy("10.0.0.0/8"), []*rules.NamespaceRuleEnforceBody{allow}, true, "", false},
		{"later allow", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{deny, allow}, false, "", false},
		{"later deny", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{allow, deny}, true, "", false},
		{"narrow allow cannot excuse broad grant", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{deny, cidrRule(rules.ActionTypeAllow, "10.20.0.0/17")}, true, "", false},
		{"union across rules", policy("10.20.0.0/16"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAllow, "10.20.0.0/17"), cidrRule(rules.ActionTypeAllow, "10.20.128.0/17")}, false, "", false},
		{"IPv6 overlap", policy("::/0"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "fd00:1234::/48")}, true, "", false},
		{"IPv6 excluded", policy("::/0", "fd00:1234::/48"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "fd00:1234::/48")}, false, "", false},
		{"separate address families", policy("::/0"), []*rules.NamespaceRuleEnforceBody{deny}, false, "", false},
		{"allow requires both families for unrestricted peer", all, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAllow, "0.0.0.0/0")}, true, "", false},
		{"unrestricted allowed both families", all, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeAllow, "0.0.0.0/0", "::/0")}, false, "", false},
		{"last IPv4", policy("0.0.0.0/0"), []*rules.NamespaceRuleEnforceBody{lastIPv4}, true, "", false},
		{"last IPv4 excluded", policy("0.0.0.0/0", "255.255.255.255/32"), []*rules.NamespaceRuleEnforceBody{lastIPv4}, false, "", false},
		{"last IPv6", policy("::/0"), []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128")}, true, "", false},
		{"invalid rule", all, []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "invalid")}, false, "CIDR rule", false},
		{"invalid action", all, []*rules.NamespaceRuleEnforceBody{cidrRule("invalid", "0.0.0.0/0")}, false, "unsupported rule action", false},
		{"invalid peer", policy("invalid"), []*rules.NamespaceRuleEnforceBody{deny}, false, "spec.egress[0].to[0].ipBlock.cidr", false},
		{"invalid except", policy("10.0.0.0/8", "invalid"), []*rules.NamespaceRuleEnforceBody{deny}, false, "except[0]", false},
		{"outside except", policy("10.0.0.0/8", "192.0.2.0/24"), []*rules.NamespaceRuleEnforceBody{deny}, false, "except[0]", false},
	}
	for _, direction := range []string{"egress", "ingress"} {
		for _, tc := range cases {
			t.Run(direction+"/"+tc.name, func(t *testing.T) {
				obj, bodies := tc.obj.DeepCopy(), tc.bodies
				wantError := tc.err
				path := "spec.egress[0].to"
				if direction == "ingress" {
					obj, bodies = ingressFixtures(obj, bodies)
					wantError = strings.ReplaceAll(wantError, "spec.egress[0].to", "spec.ingress[0].from")
					path = "spec.ingress[0].from"
				}
				original := obj.DeepCopy()
				result, err := evaluate(t.Context(), obj, bodies)
				if tc.err != "" {
					require.ErrorContains(t, err, wantError)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.denied, result.BlockingError() != nil)
				if tc.denied {
					require.Contains(t, result.BlockingError().Error(), path)
				}
				if tc.audit {
					require.NotEmpty(t, result.Audits)
				}
				require.Equal(t, original, obj, "admission must not mutate input")
			})
		}
	}
}

// Compare the partition algorithm with exhaustive address decisions in a small
// deterministic subnet, covering interacting ordered rules and exceptions.
func TestEgressPartitionCoverage(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 7))
	for range 200 {
		obj := policy("192.0.2.0/24")
		for range 1 + rng.IntN(12) {
			obj.Spec.Egress[0].To[0].IPBlock.Except = append(obj.Spec.Egress[0].To[0].IPBlock.Except, netip.MustParsePrefix(fmt.Sprintf("192.0.2.%d/%d", rng.IntN(256), 25+rng.IntN(8))).Masked().String())
		}
		var bodies []*rules.NamespaceRuleEnforceBody
		for range 8 {
			action := []rules.ActionType{rules.ActionTypeDeny, rules.ActionTypeAllow, rules.ActionTypeAudit}[rng.IntN(3)]
			bodies = append(bodies, cidrRule(action, fmt.Sprintf("192.0.2.%d/%d", rng.IntN(256), 24+rng.IntN(9))))
		}
		denied := false
		var exceptions []netip.Prefix
		for _, raw := range obj.Spec.Egress[0].To[0].IPBlock.Except {
			exceptions = append(exceptions, netip.MustParsePrefix(raw))
		}
		for address := 0; address < 256; address++ {
			ip := netip.AddrFrom4([4]byte{192, 0, 2, byte(address)})
			if slices.ContainsFunc(exceptions, func(except netip.Prefix) bool { return except.Contains(ip) }) {
				continue
			}
			hasAllow, last := false, rules.ActionType("")
			for _, body := range bodies {
				hasAllow = hasAllow || body.Action == rules.ActionTypeAllow
				if body.Action != rules.ActionTypeAudit && netip.MustParsePrefix(body.Network.Policies.Egress.CIDRs[0]).Contains(ip) {
					last = body.Action
				}
			}
			denied = denied || last == rules.ActionTypeDeny || (last == "" && hasAllow)
		}
		result, err := evaluate(t.Context(), obj, bodies)
		require.NoError(t, err)
		require.Equal(t, denied, result.BlockingError() != nil)
	}
}

func TestEgressMultiplePeersAndCancellation(t *testing.T) {
	obj := policy("192.0.2.0/24")
	obj.Spec.Egress[0].To = append(obj.Spec.Egress[0].To, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: "10.20.0.0/16"}})
	body := cidrRule(rules.ActionTypeDeny, "10.20.0.0/16")
	result, err := evaluate(t.Context(), obj, []*rules.NamespaceRuleEnforceBody{body})
	require.NoError(t, err)
	require.ErrorContains(t, result.BlockingError(), "spec.egress[0].to[1]")
	require.ErrorContains(t, result.BlockingError(), "deny rule for CIDR 10.20.0.0/16")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = evaluate(ctx, obj, []*rules.NamespaceRuleEnforceBody{body})
	require.ErrorIs(t, err, context.Canceled)
}

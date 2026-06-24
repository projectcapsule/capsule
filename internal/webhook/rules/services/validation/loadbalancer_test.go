// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"net"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestServiceRulesValidateLoadBalancers(t *testing.T) {
	tests := []struct {
		name          string
		svc           *corev1.Service
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantNil       bool
		wantBlocking  bool
		wantFinal     bool
		wantErr       string
		wantMessage   []string
	}{
		{
			name:    "nil service returns nil evaluation",
			svc:     nil,
			wantNil: true,
		},
		{
			name: "non LoadBalancer service returns nil evaluation",
			svc:  clusterIPServiceForLoadBalancerTest("cluster-ip"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeDeny, "10.0.0.0/8"),
			},
			wantNil: true,
		},
		{
			name: "LoadBalancer without values and without CIDR rules returns nil evaluation",
			svc:  loadBalancerServiceForTest("lb", "", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantNil: true,
		},
		{
			name: "requires loadBalancerIP or source ranges when CIDR rules are configured",
			svc:  loadBalancerServiceForTest("lb", "", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2/32"),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				"loadBalancer service requires spec.loadBalancerIP or spec.loadBalancerSourceRanges",
				"loadBalancer CIDR constraints are enforced by namespace rule",
			},
		},
		{
			name: "empty CIDR entry still triggers required value check",
			svc:  loadBalancerServiceForTest("lb", "", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, ""),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				"loadBalancer service requires spec.loadBalancerIP or spec.loadBalancerSourceRanges",
			},
		},
		{
			name: "allows exact IPv4 loadBalancerIP inside single IP CIDR",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2/32"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.0.2" at spec.loadBalancerIP is allowed by namespace rule`,
				"10.0.0.2 is contained in 10.0.0.2/32",
			},
		},
		{
			name: "allows IPv4 loadBalancerIP inside configured range",
			svc:  loadBalancerServiceForTest("lb", "10.0.1.44", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.1.0/24"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.1.44" at spec.loadBalancerIP is allowed by namespace rule`,
				"10.0.1.44 is contained in 10.0.1.0/24",
			},
		},
		{
			name: "allows plain IPv4 rule by normalizing to host CIDR",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				"10.0.0.2 is contained in 10.0.0.2",
			},
		},
		{
			name: "allows IPv6 loadBalancerIP inside configured range",
			svc:  loadBalancerServiceForTest("lb", "2001:db8::1", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "2001:db8::/32"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`loadBalancer CIDR "2001:db8::1" at spec.loadBalancerIP is allowed by namespace rule`,
				"2001:db8::1 is contained in 2001:db8::/32",
			},
		},
		{
			name: "allows plain IPv6 rule by normalizing to host CIDR",
			svc:  loadBalancerServiceForTest("lb", "2001:db8::2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "2001:db8::2"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				"2001:db8::2 is contained in 2001:db8::2",
			},
		},
		{
			name: "allow miss denies loadBalancerIP outside configured CIDRs",
			svc:  loadBalancerServiceForTest("lb", "10.0.171.239", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2/32", "10.0.1.0/24"),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP is not allowed by namespace rule`,
				"Allowed CIDRs",
				"10.0.0.2/32",
				"10.0.1.0/24",
			},
		},
		{
			name: "allows source range fully contained in configured CIDR",
			svc:  loadBalancerServiceForTest("lb", "", []string{"10.0.1.0/25"}),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.1.0/24"),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.1.0/25" at spec.loadBalancerSourceRanges[0] is allowed by namespace rule`,
				"10.0.1.0/25 is contained in 10.0.1.0/24",
			},
		},
		{
			name: "allow miss denies source range not fully contained in configured CIDR",
			svc:  loadBalancerServiceForTest("lb", "", []string{"10.0.1.0/23"}),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.1.0/24"),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.1.0/23" at spec.loadBalancerSourceRanges[0] is not allowed by namespace rule`,
				"Allowed CIDRs",
				"10.0.1.0/24",
			},
		},
		{
			name: "multiple values deny if any value misses allow list",
			svc:  loadBalancerServiceForTest("lb", "10.0.1.44", []string{"172.16.0.0/16"}),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.1.0/24"),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`loadBalancer CIDR "172.16.0.0/16" at spec.loadBalancerSourceRanges[0] is not allowed by namespace rule`,
				"Allowed CIDRs",
				"10.0.1.0/24",
			},
		},
		{
			name: "deny matching CIDR",
			svc:  loadBalancerServiceForTest("lb", "10.0.66.10", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeDeny, "10.0.66.0/24"),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.66.10" at spec.loadBalancerIP is denied by namespace rule`,
				"10.0.66.10 is contained in 10.0.66.0/24",
			},
		},
		{
			name: "later deny overrides earlier allow",
			svc:  loadBalancerServiceForTest("lb", "10.0.66.10", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.0/8"),
				loadBalancerEnforceForTest(apirules.ActionTypeDeny, "10.0.66.0/24"),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.66.10" at spec.loadBalancerIP is denied by namespace rule`,
				"10.0.66.10 is contained in 10.0.66.0/24",
			},
		},
		{
			name: "later allow overrides earlier deny",
			svc:  loadBalancerServiceForTest("lb", "10.0.171.239", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeDeny, "10.0.0.0/8"),
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.171.0/24"),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP is allowed by namespace rule`,
				"10.0.171.239 is contained in 10.0.171.0/24",
			},
		},
		{
			name: "audit match is observational",
			svc:  loadBalancerServiceForTest("lb", "10.0.171.239", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAudit, "10.0.171.0/24"),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP matched audit namespace rule`,
				"10.0.171.239 is contained in 10.0.171.0/24",
			},
		},
		{
			name: "audit does not satisfy allow list",
			svc:  loadBalancerServiceForTest("lb", "10.0.171.239", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAudit, "10.0.171.0/24"),
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2/32"),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP is not allowed by namespace rule`,
				"Allowed CIDRs",
				"10.0.0.2/32",
			},
		},
		{
			name: "invalid configured CIDR returns matcher error",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeDeny, "10.0.0.0/33"),
			},
			wantErr: `loadBalancer CIDR: invalid rule: invalid loadBalancer CIDR "10.0.0.0/33"`,
		},
		{
			name: "invalid requested source range returns matcher error",
			svc:  loadBalancerServiceForTest("lb", "", []string{"not-a-cidr"}),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.0/8"),
			},
			wantErr: `loadBalancer CIDR: invalid rule: spec.loadBalancerSourceRanges[0] contains invalid IP or CIDR "not-a-cidr"`,
		},
		{
			name: "nil enforce body is ignored",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "10.0.0.2/32"),
			},
			wantFinal:    true,
			wantBlocking: false,
		},
		{
			name: "enforce without loadBalancer rules is ignored",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantFinal:    false,
			wantBlocking: false,
		},
		{
			name: "empty configured CIDRs are ignored during rule extraction when values exist",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionTypeAllow, "", " "),
			},
			wantFinal:    false,
			wantBlocking: false,
		},
		{
			name: "unsupported action returns error",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				loadBalancerEnforceForTest(apirules.ActionType("invalid"), "10.0.0.2/32"),
			},
			wantErr: `loadBalancer CIDR: unsupported rule action "invalid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := serviceRulesForTest()

			evaluation, err := h.validateLoadBalancers(tt.svc, tt.enforceBodies)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantNil {
				if evaluation != nil {
					t.Fatalf("expected nil evaluation, got %#v", evaluation)
				}

				return
			}

			if evaluation == nil {
				t.Fatalf("expected evaluation, got nil")
			}

			if tt.wantBlocking && evaluation.Blocking == nil {
				t.Fatalf("expected blocking decision, got nil")
			}

			if !tt.wantBlocking && evaluation.Blocking != nil {
				t.Fatalf("expected no blocking decision, got %#v", evaluation.Blocking)
			}

			if tt.wantFinal && evaluation.Final == nil {
				t.Fatalf("expected final decision, got nil")
			}

			if !tt.wantFinal && evaluation.Final != nil {
				t.Fatalf("expected no final decision, got %#v", evaluation.Final)
			}

			if len(tt.wantMessage) > 0 {
				msg := decisionMessageForLoadBalancerTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenLoadBalancerCIDR {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenLoadBalancerCIDR)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenLoadBalancerCIDR {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenLoadBalancerCIDR)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenLoadBalancerCIDR {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenLoadBalancerCIDR)
				}
			}
		})
	}
}

func TestRequiresLoadBalancerCIDRs(t *testing.T) {
	tests := []struct {
		name          string
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		want          bool
	}{
		{
			name:          "nil bodies",
			enforceBodies: nil,
			want:          false,
		},
		{
			name: "nil enforce body",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
			},
			want: false,
		},
		{
			name: "missing loadBalancer rules",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			want: false,
		},
		{
			name: "empty cidr list",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Services: apirules.NamespaceRuleEnforceServicesBody{
						LoadBalancers: &apirules.ServiceLoadBalancerRule{},
					},
				},
			},
			want: false,
		},
		{
			name: "blank cidr entry still counts as configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Services: apirules.NamespaceRuleEnforceServicesBody{
						LoadBalancers: &apirules.ServiceLoadBalancerRule{
							CIDRs: []string{""},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "cidr configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Services: apirules.NamespaceRuleEnforceServicesBody{
						LoadBalancers: &apirules.ServiceLoadBalancerRule{
							CIDRs: []string{"10.0.0.0/8"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "later cidr configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				{},
				{
					Services: apirules.NamespaceRuleEnforceServicesBody{
						LoadBalancers: &apirules.ServiceLoadBalancerRule{
							CIDRs: []string{"10.0.0.0/8"},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiresLoadBalancerCIDRs(tt.enforceBodies)
			if got != tt.want {
				t.Fatalf("requiresLoadBalancerCIDRs() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestParseCIDR(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantNetwork string
		wantErr     string
	}{
		{
			name:        "IPv4 CIDR",
			raw:         "10.0.0.0/8",
			wantNetwork: "10.0.0.0/8",
		},
		{
			name:        "IPv4 host",
			raw:         "10.0.0.2",
			wantNetwork: "10.0.0.2/32",
		},
		{
			name:        "IPv4 host with whitespace",
			raw:         " 10.0.0.2 ",
			wantNetwork: "10.0.0.2/32",
		},
		{
			name:        "IPv6 CIDR",
			raw:         "2001:db8::/32",
			wantNetwork: "2001:db8::/32",
		},
		{
			name:        "IPv6 host",
			raw:         "2001:db8::2",
			wantNetwork: "2001:db8::2/128",
		},
		{
			name:    "empty",
			raw:     "",
			wantErr: "CIDR is empty",
		},
		{
			name:    "whitespace",
			raw:     " ",
			wantErr: "CIDR is empty",
		},
		{
			name:    "invalid IP without slash",
			raw:     "not-an-ip",
			wantErr: `invalid CIDR "not-an-ip"`,
		},
		{
			name:    "invalid CIDR",
			raw:     "10.0.0.0/33",
			wantErr: "invalid CIDR address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCIDR(tt.raw)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got == nil {
				t.Fatalf("expected network, got nil")
			}

			if got.String() != tt.wantNetwork {
				t.Fatalf("network = %q, want %q", got.String(), tt.wantNetwork)
			}
		})
	}
}

func TestLoadBalancerCIDRValues(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want []struct {
			value string
			path  string
		}
	}{
		{
			name: "no values",
			svc:  loadBalancerServiceForTest("lb", "", nil),
			want: nil,
		},
		{
			name: "loadBalancerIP only",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", nil),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "10.0.0.2",
					path:  "spec.loadBalancerIP",
				},
			},
		},
		{
			name: "source ranges only",
			svc:  loadBalancerServiceForTest("lb", "", []string{"10.0.1.0/25", "10.0.2.0/24"}),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "10.0.1.0/25",
					path:  "spec.loadBalancerSourceRanges[0]",
				},
				{
					value: "10.0.2.0/24",
					path:  "spec.loadBalancerSourceRanges[1]",
				},
			},
		},
		{
			name: "loadBalancerIP and source ranges",
			svc:  loadBalancerServiceForTest("lb", "10.0.0.2", []string{"10.0.1.0/25"}),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "10.0.0.2",
					path:  "spec.loadBalancerIP",
				},
				{
					value: "10.0.1.0/25",
					path:  "spec.loadBalancerSourceRanges[0]",
				},
			},
		},
		{
			name: "blank source range is preserved for matcher validation",
			svc:  loadBalancerServiceForTest("lb", "", []string{" "}),
			want: []struct {
				value string
				path  string
			}{
				{
					value: " ",
					path:  "spec.loadBalancerSourceRanges[0]",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadBalancerCIDRValues(tt.svc)

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d values, got %d: %#v", len(tt.want), len(got), got)
			}

			for i, want := range tt.want {
				if got[i].Value != want.value {
					t.Fatalf("value[%d] = %q, want %q", i, got[i].Value, want.value)
				}

				if got[i].Path != want.path {
					t.Fatalf("path[%d] = %q, want %q", i, got[i].Path, want.path)
				}
			}
		})
	}
}

func TestCIDRContainsHelpers(t *testing.T) {
	_, allowedIPv4, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, requestedInsideIPv4, err := net.ParseCIDR("10.0.1.0/24")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, requestedOutsideIPv4, err := net.ParseCIDR("10.1.0.0/7")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, allowedIPv6, err := net.ParseCIDR("2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, requestedInsideIPv6, err := net.ParseCIDR("2001:db8:1::/48")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, requestedOutsideIPv6, err := net.ParseCIDR("2001:db9::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	tests := []struct {
		name string
		fn   func() bool
		want bool
	}{
		{
			name: "IPv4 contains IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv4, net.ParseIP("10.0.0.2"))
			},
			want: true,
		},
		{
			name: "IPv4 does not contain IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv4, net.ParseIP("192.168.0.1"))
			},
			want: false,
		},
		{
			name: "nil IP is not contained",
			fn: func() bool {
				return cidrContainsIP(allowedIPv4, nil)
			},
			want: false,
		},
		{
			name: "nil CIDR does not contain IP",
			fn: func() bool {
				return cidrContainsIP(nil, net.ParseIP("10.0.0.2"))
			},
			want: false,
		},
		{
			name: "IPv4 contains requested CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv4, requestedInsideIPv4)
			},
			want: true,
		},
		{
			name: "IPv4 does not fully contain requested CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv4, requestedOutsideIPv4)
			},
			want: false,
		},
		{
			name: "nil allowed CIDR does not contain CIDR",
			fn: func() bool {
				return cidrContainsCIDR(nil, requestedInsideIPv4)
			},
			want: false,
		},
		{
			name: "nil requested CIDR is not contained",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv4, nil)
			},
			want: false,
		},
		{
			name: "IPv6 contains IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv6, net.ParseIP("2001:db8::1"))
			},
			want: true,
		},
		{
			name: "IPv6 does not contain IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv6, net.ParseIP("2001:db9::1"))
			},
			want: false,
		},
		{
			name: "IPv6 contains requested CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv6, requestedInsideIPv6)
			},
			want: true,
		},
		{
			name: "IPv6 does not contain requested CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv6, requestedOutsideIPv6)
			},
			want: false,
		},
		{
			name: "IPv4 CIDR does not contain IPv6 IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv4, net.ParseIP("2001:db8::1"))
			},
			want: false,
		},
		{
			name: "IPv4 CIDR does not contain IPv6 CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv4, requestedInsideIPv6)
			},
			want: false,
		},
		{
			name: "IPv6 CIDR does not contain IPv4 IP",
			fn: func() bool {
				return cidrContainsIP(allowedIPv6, net.ParseIP("10.0.0.2"))
			},
			want: false,
		},
		{
			name: "IPv6 CIDR does not contain IPv4 CIDR",
			fn: func() bool {
				return cidrContainsCIDR(allowedIPv6, requestedInsideIPv4)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got != tt.want {
				t.Fatalf("got %t, want %t", got, tt.want)
			}
		})
	}
}

func loadBalancerEnforceForTest(
	action apirules.ActionType,
	cidrs ...string,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Services: apirules.NamespaceRuleEnforceServicesBody{
			LoadBalancers: &apirules.ServiceLoadBalancerRule{
				CIDRs: cidrs,
			},
		},
	}
}

func loadBalancerServiceForTest(
	name string,
	loadBalancerIP string,
	sourceRanges []string,
) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerIP:           loadBalancerIP,
			LoadBalancerSourceRanges: sourceRanges,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
}

func clusterIPServiceForLoadBalancerTest(name string) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
}

func decisionMessageForLoadBalancerTest(evaluation interface {
}) string {
	e, ok := evaluation.(*ruleengine.Evaluation)
	if !ok || e == nil {
		return ""
	}

	switch {
	case e.Blocking != nil:
		return e.Blocking.Message
	case e.Final != nil:
		return e.Final.Message
	case len(e.Audits) > 0:
		return e.Audits[0].Message
	default:
		return ""
	}
}

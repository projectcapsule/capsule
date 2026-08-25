// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestServiceRulesValidateExternalIPs(t *testing.T) {
	tests := []struct {
		name          string
		svc           *corev1.Service
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantNil       bool
		wantBlocking  bool
		wantFinal     bool
		wantAudits    int
		wantErr       string
		wantMessage   []string
	}{
		{
			name:    "nil service returns nil evaluation",
			svc:     nil,
			wantNil: true,
		},
		{
			name: "service without external IPs remains unrestricted",
			svc:  externalIPServiceForTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.0.0.0/8"),
			},
		},
		{
			name: "external IP without external IP rules remains unrestricted",
			svc:  externalIPServiceForTest("10.0.0.2"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{Action: apirules.ActionTypeAllow},
			},
		},
		{
			name: "allows IPv4 address inside configured CIDR",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/16"),
			},
			wantFinal: true,
			wantMessage: []string{
				`external IP "10.20.1.44" at spec.externalIPs[0] is allowed by namespace rule`,
				"10.20.1.44 is contained in 10.20.0.0/16",
			},
		},
		{
			name: "allows plain IPv4 rule as a host CIDR",
			svc:  externalIPServiceForTest("192.168.1.2"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "192.168.1.2"),
			},
			wantFinal: true,
		},
		{
			name: "allows IPv6 address inside configured CIDR",
			svc:  externalIPServiceForTest("2001:db8::2"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "2001:db8::/32"),
			},
			wantFinal: true,
			wantMessage: []string{
				"2001:db8::2 is contained in 2001:db8::/32",
			},
		},
		{
			name: "allow miss denies address outside configured CIDRs",
			svc:  externalIPServiceForTest("8.8.8.8"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/16", "192.168.1.2"),
			},
			wantBlocking: true,
			wantMessage: []string{
				`external IP "8.8.8.8" at spec.externalIPs[0] is not allowed by namespace rule`,
				"Allowed CIDRs",
				"10.20.0.0/16",
				"192.168.1.2",
			},
		},
		{
			name: "all external IPs must match an allow rule",
			svc:  externalIPServiceForTest("10.20.1.44", "8.8.8.8"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/16"),
			},
			wantFinal:    true,
			wantBlocking: true,
			wantMessage: []string{
				`external IP "8.8.8.8" at spec.externalIPs[1] is not allowed by namespace rule`,
			},
		},
		{
			name: "matching deny rule blocks address",
			svc:  externalIPServiceForTest("10.20.66.4"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeDeny, "10.20.66.0/24"),
			},
			wantFinal:    true,
			wantBlocking: true,
			wantMessage: []string{
				`external IP "10.20.66.4" at spec.externalIPs[0] is denied by namespace rule`,
			},
		},
		{
			name: "deny with omitted CIDRs blocks every external IP",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeDeny),
			},
			wantFinal:    true,
			wantBlocking: true,
			wantMessage: []string{
				`external IP "10.20.1.44" at spec.externalIPs[0] is denied by namespace rule`,
				"all external IPs",
				"deny rule applies to all external IPs",
			},
		},
		{
			name: "deny with explicit empty CIDRs blocks every external IP",
			svc:  externalIPServiceForTest("2001:db8::2"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeDeny,
					Services: apirules.NamespaceRuleEnforceServicesBody{
						ExternalIPs: &apirules.ServiceExternalIPRule{
							CIDRs: []string{},
						},
					},
				},
			},
			wantFinal:    true,
			wantBlocking: true,
		},
		{
			name: "default deny action with empty CIDRs blocks every external IP",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(""),
			},
			wantFinal:    true,
			wantBlocking: true,
		},
		{
			name: "deny with empty CIDRs does not require an external IP",
			svc:  externalIPServiceForTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeDeny),
			},
		},
		{
			name: "later allow overrides earlier matching deny",
			svc:  externalIPServiceForTest("10.20.66.4"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeDeny, "10.20.0.0/16"),
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.66.0/24"),
			},
			wantFinal: true,
		},
		{
			name: "matching audit rule is observational",
			svc:  externalIPServiceForTest("10.20.66.4"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAudit, "10.20.66.0/24"),
			},
			wantAudits: 1,
			wantMessage: []string{
				`external IP "10.20.66.4" at spec.externalIPs[0] matched audit namespace rule`,
			},
		},
		{
			name: "audit does not satisfy a non-matching allow rule",
			svc:  externalIPServiceForTest("10.20.66.4"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAudit, "10.20.66.0/24"),
				externalIPEnforceForTest(apirules.ActionTypeAllow, "192.168.0.0/16"),
			},
			wantAudits:   1,
			wantBlocking: true,
		},
		{
			name: "blank configured CIDRs are ignored",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "", " "),
			},
		},
		{
			name: "invalid configured CIDR returns matcher error",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/33"),
			},
			wantErr: `external IP: invalid rule: invalid external IP CIDR "10.20.0.0/33"`,
		},
		{
			name: "invalid requested external IP returns matcher error",
			svc:  externalIPServiceForTest("not-an-ip"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/16"),
			},
			wantErr: `external IP: invalid rule: spec.externalIPs[0] contains invalid IP "not-an-ip"`,
		},
		{
			name: "nil enforce body is ignored",
			svc:  externalIPServiceForTest("10.20.1.44"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				externalIPEnforceForTest(apirules.ActionTypeAllow, "10.20.0.0/16"),
			},
			wantFinal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := serviceRulesForTest()

			evaluation, err := h.validateExternalIPs(tt.svc, tt.enforceBodies)
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

			if (evaluation.Blocking != nil) != tt.wantBlocking {
				t.Fatalf("blocking decision = %#v, want presence %t", evaluation.Blocking, tt.wantBlocking)
			}

			if (evaluation.Final != nil) != tt.wantFinal {
				t.Fatalf("final decision = %#v, want presence %t", evaluation.Final, tt.wantFinal)
			}

			if len(evaluation.Audits) != tt.wantAudits {
				t.Fatalf("audit decisions = %d, want %d", len(evaluation.Audits), tt.wantAudits)
			}

			message := externalIPDecisionMessagesForTest(evaluation)
			for _, expected := range tt.wantMessage {
				if !strings.Contains(message, expected) {
					t.Fatalf("expected message %q to contain %q", message, expected)
				}
			}

			if evaluation.Final != nil && evaluation.Final.EventReason != events.ReasonForbiddenExternalServiceIP {
				t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenExternalServiceIP)
			}

			if evaluation.Blocking != nil && evaluation.Blocking.EventReason != events.ReasonForbiddenExternalServiceIP {
				t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenExternalServiceIP)
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenExternalServiceIP {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenExternalServiceIP)
				}
			}
		})
	}
}

func TestExternalIPValues(t *testing.T) {
	values := externalIPValues(externalIPServiceForTest("10.20.1.44", "2001:db8::2"))
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0].Value != "10.20.1.44" || values[0].Path != "spec.externalIPs[0]" {
		t.Fatalf("unexpected first value: %#v", values[0])
	}

	if values[1].Value != "2001:db8::2" || values[1].Path != "spec.externalIPs[1]" {
		t.Fatalf("unexpected second value: %#v", values[1])
	}
}

func externalIPEnforceForTest(
	action apirules.ActionType,
	cidrs ...string,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Services: apirules.NamespaceRuleEnforceServicesBody{
			ExternalIPs: &apirules.ServiceExternalIPRule{
				CIDRs: cidrs,
			},
		},
	}
}

func externalIPServiceForTest(externalIPs ...string) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:        corev1.ServiceTypeClusterIP,
			ExternalIPs: externalIPs,
		},
	}
}

func externalIPDecisionMessagesForTest(evaluation *ruleengine.Evaluation) string {
	messages := make([]string, 0, len(evaluation.Audits)+2)

	for _, audit := range evaluation.Audits {
		messages = append(messages, audit.Message)
	}

	if evaluation.Final != nil {
		messages = append(messages, evaluation.Final.Message)
	}

	if evaluation.Blocking != nil && evaluation.Blocking != evaluation.Final {
		messages = append(messages, evaluation.Blocking.Message)
	}

	return strings.Join(messages, "\n")
}

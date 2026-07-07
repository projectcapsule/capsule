// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestServiceRulesValidateExternalNames(t *testing.T) {
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
			name: "non ExternalName service returns nil evaluation",
			svc:  clusterIPServiceForExternalNameTest("cluster-ip"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(apirules.ActionTypeDeny, expressionMatchForTest(".*")),
			},
			wantNil: true,
		},
		{
			name: "empty externalName returns nil evaluation",
			svc:  externalNameServiceForTest("empty", " "),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(apirules.ActionTypeDeny, expressionMatchForTest(".*")),
			},
			wantNil: true,
		},
		{
			name: "no rules allows externalName without final decision",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantFinal:    false,
			wantBlocking: false,
		},
		{
			name: "allow exact hostname",
			svc:  externalNameServiceForTest("external", "internal.git.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					exactMatchForTest("internal.git.com"),
				),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`externalName hostname "internal.git.com" at spec.externalName is allowed by namespace rule`,
				`"internal.git.com" matched hostname rule exact: internal.git.com`,
			},
		},
		{
			name: "allow regex hostname",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					expressionMatchForTest(".*\\.example\\.com"),
				),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`externalName hostname "api.example.com" at spec.externalName is allowed by namespace rule`,
				`"api.example.com" matched hostname rule exp: .*\.example\.com`,
			},
		},
		{
			name: "allow exact and regex in same matcher",
			svc:  externalNameServiceForTest("external", "combined.internal.git.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					runtime.ExpressionMatch{
						Exact: []string{
							"combined.internal.git.com",
						},
						ExpressionRegex: runtime.ExpressionRegex{
							Expression: "combined\\..*\\.example\\.com",
						},
					},
				),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`externalName hostname "combined.internal.git.com" at spec.externalName is allowed by namespace rule`,
				`exact: combined.internal.git.com; exp: combined\..*\.example\.com`,
			},
		},
		{
			name: "allow miss denies and reports allowed hostnames",
			svc:  externalNameServiceForTest("external", "api.bad.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					exactMatchForTest("internal.git.com"),
					expressionMatchForTest(".*\\.example\\.com"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`externalName hostname "api.bad.com" at spec.externalName is not allowed by namespace rule`,
				`Allowed hostnames`,
				`exact: internal.git.com`,
				`exp: .*\.example\.com`,
			},
		},
		{
			name: "deny matching exact hostname",
			svc:  externalNameServiceForTest("external", "blocked.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					exactMatchForTest("blocked.example.com"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`externalName hostname "blocked.example.com" at spec.externalName is denied by namespace rule`,
				`"blocked.example.com" matched hostname rule exact: blocked.example.com`,
			},
		},
		{
			name: "later deny overrides earlier allow",
			svc:  externalNameServiceForTest("external", "blocked.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					expressionMatchForTest(".*\\.example\\.com"),
				),
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					exactMatchForTest("blocked.example.com"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`externalName hostname "blocked.example.com" at spec.externalName is denied by namespace rule`,
				`"blocked.example.com" matched hostname rule exact: blocked.example.com`,
			},
		},
		{
			name: "later allow overrides earlier deny",
			svc:  externalNameServiceForTest("external", "trusted.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					expressionMatchForTest(".*\\.example\\.com"),
				),
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					exactMatchForTest("trusted.example.com"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`externalName hostname "trusted.example.com" at spec.externalName is allowed by namespace rule`,
				`"trusted.example.com" matched hostname rule exact: trusted.example.com`,
			},
		},
		{
			name: "audit match is observational",
			svc:  externalNameServiceForTest("external", "audit.internal"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAudit,
					expressionMatchForTest("audit\\..*"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`externalName hostname "audit.internal" at spec.externalName matched audit namespace rule`,
				`"audit.internal" matched hostname rule exp: audit\..*`,
			},
		},
		{
			name: "audit does not satisfy allow list",
			svc:  externalNameServiceForTest("external", "audit.internal"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeAudit,
					expressionMatchForTest("audit\\..*"),
				),
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					expressionMatchForTest("allowed\\..*"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`externalName hostname "audit.internal" at spec.externalName is not allowed by namespace rule`,
				`Allowed hostnames`,
				`exp: allowed\..*`,
			},
		},
		{
			name: "negated regex deny matches untrusted hostname",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					negatedExpressionMatchForTest("trusted\\..*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`externalName hostname "api.example.com" at spec.externalName is denied by namespace rule`,
				`"api.example.com" matched hostname rule exp: trusted\..*`,
			},
		},
		{
			name: "negated regex deny does not match trusted hostname",
			svc:  externalNameServiceForTest("external", "trusted.api"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					negatedExpressionMatchForTest("trusted\\..*"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
		},
		{
			name: "invalid regex returns matcher error",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				externalNameEnforceForTest(
					apirules.ActionTypeDeny,
					expressionMatchForTest("["),
				),
			},
			wantErr: `externalName hostname: invalid rule`,
		},
		{
			name: "nil enforce body is ignored",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				externalNameEnforceForTest(
					apirules.ActionTypeAllow,
					expressionMatchForTest(".*\\.example\\.com"),
				),
			},
			wantFinal:    true,
			wantBlocking: false,
		},
		{
			name: "enforce without externalName rules is ignored",
			svc:  externalNameServiceForTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantFinal:    false,
			wantBlocking: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := serviceRulesForTest()

			evaluation, err := h.validateExternalNames(tt.svc, tt.enforceBodies)

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

			if len(evaluation.Audits) != tt.wantAudits {
				t.Fatalf("expected %d audit decisions, got %d", tt.wantAudits, len(evaluation.Audits))
			}

			if len(tt.wantMessage) > 0 {
				msg := decisionMessageForExternalNameTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenExternalName {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenExternalName)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenExternalName {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenExternalName)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenExternalName {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenExternalName)
				}
			}
		})
	}
}

func TestDescribeExpressionMatch(t *testing.T) {
	tests := []struct {
		name  string
		match runtime.ExpressionMatch
		want  string
	}{
		{
			name:  "empty matcher",
			match: runtime.ExpressionMatch{},
			want:  "",
		},
		{
			name: "exact only",
			match: runtime.ExpressionMatch{
				Exact: []string{
					"internal.git.com",
					"api.example.com",
				},
			},
			want: "exact: internal.git.com, api.example.com",
		},
		{
			name: "expression only",
			match: runtime.ExpressionMatch{
				ExpressionRegex: runtime.ExpressionRegex{
					Expression: ".*\\.example\\.com",
				},
			},
			want: "exp: .*\\.example\\.com",
		},
		{
			name: "exact and expression",
			match: runtime.ExpressionMatch{
				Exact: []string{
					"internal.git.com",
				},
				ExpressionRegex: runtime.ExpressionRegex{
					Expression: ".*\\.example\\.com",
				},
			},
			want: "exact: internal.git.com; exp: .*\\.example\\.com",
		},
		{
			name: "negate is currently not included in description",
			match: runtime.ExpressionMatch{
				ExpressionRegex: runtime.ExpressionRegex{
					Expression: "trusted\\..*",
					Negate:     true,
				},
			},
			want: "exp: trusted\\..*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeExpressionMatch(tt.match)
			if got != tt.want {
				t.Fatalf("describeExpressionMatch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func externalNameEnforceForTest(
	action apirules.ActionType,
	hostnames ...runtime.ExpressionMatch,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Services: apirules.NamespaceRuleEnforceServicesBody{
			ExternalNames: &apirules.ServiceExternalNameRule{
				Hostnames: hostnames,
			},
		},
	}
}

func externalNameServiceForTest(name string, externalName string) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: externalName,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(443),
				},
			},
		},
	}
}

func clusterIPServiceForExternalNameTest(name string) *corev1.Service {
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

func exactMatchForTest(values ...string) runtime.ExpressionMatch {
	return runtime.ExpressionMatch{
		Exact: values,
	}
}

func expressionMatchForTest(expression string) runtime.ExpressionMatch {
	return runtime.ExpressionMatch{
		ExpressionRegex: runtime.ExpressionRegex{
			Expression: expression,
		},
	}
}

func negatedExpressionMatchForTest(expression string) runtime.ExpressionMatch {
	return runtime.ExpressionMatch{
		ExpressionRegex: runtime.ExpressionRegex{
			Expression: expression,
			Negate:     true,
		},
	}
}

func decisionMessageForExternalNameTest(evaluation interface {
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

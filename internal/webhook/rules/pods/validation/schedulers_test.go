// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestPodRulesValidateSchedulers(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantBlocking  bool
		wantFinal     bool
		wantAudits    int
		wantErr       string
		wantMessage   []string
	}{
		{
			name: "pod without schedulerName returns empty evaluation",
			pod:  schedulerPodForTest(""),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExpressionForTest(".*"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
		},
		{
			name: "blank schedulerName is trimmed and skipped",
			pod:  schedulerPodForTest("   "),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExpressionForTest(".*"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
		},
		{
			name: "no scheduler rules returns empty evaluation",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantBlocking: false,
			wantFinal:    false,
		},
		{
			name: "nil enforce body is ignored",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-scheduler" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exact: tenant-scheduler`,
			},
		},
		{
			name: "allow exact scheduler",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-scheduler" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exact: tenant-scheduler`,
			},
		},
		{
			name: "allow regex scheduler",
			pod:  schedulerPodForTest("tenant-batch"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExpressionForTest("tenant-[a-z0-9-]+"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-batch" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exp: tenant-[a-z0-9-]+`,
			},
		},
		{
			name: "allow exact and regex in same matcher",
			pod:  schedulerPodForTest("tenant-special"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					api.ExpressionMatch{
						Exact: []string{
							"default-scheduler",
						},
						ExpressionRegex: api.ExpressionRegex{
							Expression: "tenant-[a-z0-9-]+",
						},
					},
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-special" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exact: default-scheduler; exp: tenant-[a-z0-9-]+`,
			},
		},
		{
			name: "allow miss denies scheduler missing from allowed list",
			pod:  schedulerPodForTest("other-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
					schedulerExpressionForTest("batch-[a-z0-9-]+"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`scheduler "other-scheduler" at spec.schedulerName is not allowed by namespace rule`,
				`Allowed schedulers`,
				`exact: tenant-scheduler`,
				`exp: batch-[a-z0-9-]+`,
			},
		},
		{
			name: "deny matching exact scheduler",
			pod:  schedulerPodForTest("unsafe-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExactForTest("unsafe-scheduler"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "unsafe-scheduler" at spec.schedulerName is denied by namespace rule`,
				`matched denied rule exact: unsafe-scheduler`,
			},
		},
		{
			name: "default action is deny",
			pod:  schedulerPodForTest("unsafe-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					"",
					schedulerExactForTest("unsafe-scheduler"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "unsafe-scheduler" at spec.schedulerName is denied by namespace rule`,
				`matched denied rule exact: unsafe-scheduler`,
			},
		},
		{
			name: "later deny overrides earlier allow",
			pod:  schedulerPodForTest("unsafe-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExpressionForTest(".*-scheduler"),
				),
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExactForTest("unsafe-scheduler"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "unsafe-scheduler" at spec.schedulerName is denied by namespace rule`,
				`matched denied rule exact: unsafe-scheduler`,
			},
		},
		{
			name: "later allow overrides earlier deny",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExpressionForTest(".*-scheduler"),
				),
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-scheduler" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exact: tenant-scheduler`,
			},
		},
		{
			name: "non matching later deny does not override earlier allow",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExactForTest("unsafe-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-scheduler" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule exact: tenant-scheduler`,
			},
		},
		{
			name: "audit match is observational",
			pod:  schedulerPodForTest("custom-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAudit,
					schedulerExactForTest("custom-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`scheduler "custom-scheduler" at spec.schedulerName matched audit namespace rule`,
				`matched audit rule exact: custom-scheduler`,
			},
		},
		{
			name: "audit does not satisfy allow list",
			pod:  schedulerPodForTest("custom-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAudit,
					schedulerExactForTest("custom-scheduler"),
				),
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`scheduler "custom-scheduler" at spec.schedulerName is not allowed by namespace rule`,
				`Allowed schedulers`,
				`exact: tenant-scheduler`,
			},
		},
		{
			name: "negated exact deny matches every other scheduler",
			pod:  schedulerPodForTest("other-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerNegatedExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "other-scheduler" at spec.schedulerName is denied by namespace rule`,
				`matched denied rule not exact: tenant-scheduler`,
			},
		},
		{
			name: "negated exact deny does not match excluded scheduler",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerNegatedExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
		},
		{
			name: "negated regex allow matches scheduler outside regex",
			pod:  schedulerPodForTest("external-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerNegatedExpressionForTest("tenant-[a-z0-9-]+"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "external-scheduler" at spec.schedulerName is allowed by namespace rule`,
				`matched allowed rule not exp: tenant-[a-z0-9-]+`,
			},
		},
		{
			name: "invalid regex returns matcher error",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeDeny,
					schedulerExpressionForTest("["),
				),
			},
			wantErr: `scheduler: invalid rule`,
		},
		{
			name: "schedulerName is trimmed before evaluation",
			pod:  schedulerPodForTest(" tenant-scheduler "),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionTypeAllow,
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`scheduler "tenant-scheduler" at spec.schedulerName is allowed by namespace rule`,
			},
		},
		{
			name: "unsupported action returns error",
			pod:  schedulerPodForTest("tenant-scheduler"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				schedulerEnforceForTest(
					apirules.ActionType("invalid"),
					schedulerExactForTest("tenant-scheduler"),
				),
			},
			wantErr: `scheduler: unsupported rule action "invalid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := podRulesForTest()

			evaluation, err := h.validateSchedulers(tt.pod, tt.enforceBodies)

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
				msg := decisionMessageForSchedulerTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenPodScheduler {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenPodScheduler)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenPodScheduler {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenPodScheduler)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenPodScheduler {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenPodScheduler)
				}
			}
		})
	}
}

func schedulerEnforceForTest(
	action apirules.ActionType,
	schedulers ...api.ExpressionMatch,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			Schedulers: schedulers,
		},
	}
}

func schedulerPodForTest(schedulerName string) *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			SchedulerName: schedulerName,
			Containers: []corev1.Container{
				{
					Name:  "shell",
					Image: "busybox",
				},
			},
		},
	}
}

func schedulerExactForTest(values ...string) api.ExpressionMatch {
	return api.ExpressionMatch{
		Exact: values,
	}
}

func schedulerExpressionForTest(expression string) api.ExpressionMatch {
	return api.ExpressionMatch{
		ExpressionRegex: api.ExpressionRegex{
			Expression: expression,
		},
	}
}

func schedulerNegatedExactForTest(values ...string) api.ExpressionMatch {
	return api.ExpressionMatch{
		Exact: values,
		ExpressionRegex: api.ExpressionRegex{
			Negate: true,
		},
	}
}

func schedulerNegatedExpressionForTest(expression string) api.ExpressionMatch {
	return api.ExpressionMatch{
		ExpressionRegex: api.ExpressionRegex{
			Expression: expression,
			Negate:     true,
		},
	}
}

func decisionMessageForSchedulerTest(evaluation interface {
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

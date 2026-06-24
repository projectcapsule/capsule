// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestPodRulesValidateQoSClasses(t *testing.T) {
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
			name: "BestEffort pod without QoS rules returns empty evaluation",
			pod:  bestEffortPodForQoSTest(),
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
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule BestEffort`,
			},
		},
		{
			name: "allow BestEffort QoS class",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule BestEffort`,
			},
		},
		{
			name: "allow Burstable QoS class",
			pod:  burstablePodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBurstable,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "Burstable" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule Burstable`,
			},
		},
		{
			name: "allow Guaranteed QoS class",
			pod:  guaranteedPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSGuaranteed,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "Guaranteed" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule Guaranteed`,
			},
		},
		{
			name: "allow miss denies QoS class missing from allowed list",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBurstable,
					corev1.PodQOSGuaranteed,
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is not allowed by namespace rule`,
				`Allowed QoS classes`,
				`Burstable`,
				`Guaranteed`,
			},
		},
		{
			name: "deny matching QoS class",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeDeny,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is denied by namespace rule`,
				`matched denied rule BestEffort`,
			},
		},
		{
			name: "default action is deny",
			pod:  burstablePodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					"",
					corev1.PodQOSBurstable,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "Burstable" at status.qosClass is denied by namespace rule`,
				`matched denied rule Burstable`,
			},
		},
		{
			name: "later deny overrides earlier allow",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBestEffort,
				),
				qosEnforceForTest(
					apirules.ActionTypeDeny,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is denied by namespace rule`,
				`matched denied rule BestEffort`,
			},
		},
		{
			name: "later allow overrides earlier deny",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeDeny,
					corev1.PodQOSBestEffort,
				),
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule BestEffort`,
			},
		},
		{
			name: "non matching later deny does not override earlier allow",
			pod:  guaranteedPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSGuaranteed,
				),
				qosEnforceForTest(
					apirules.ActionTypeDeny,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "Guaranteed" at status.qosClass is allowed by namespace rule`,
				`matched allowed rule Guaranteed`,
			},
		},
		{
			name: "audit match is observational",
			pod:  burstablePodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAudit,
					corev1.PodQOSBurstable,
				),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`QoS class "Burstable" at status.qosClass matched audit namespace rule`,
				`matched audit rule Burstable`,
			},
		},
		{
			name: "audit does not satisfy allow list",
			pod:  burstablePodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAudit,
					corev1.PodQOSBurstable,
				),
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSGuaranteed,
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`QoS class "Burstable" at status.qosClass is not allowed by namespace rule`,
				`Allowed QoS classes`,
				`Guaranteed`,
			},
		},
		{
			name: "unsupported action returns error",
			pod:  bestEffortPodForQoSTest(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionType("invalid"),
					corev1.PodQOSBestEffort,
				),
			},
			wantErr: `QoS class: unsupported rule action "invalid"`,
		},
		{
			name: "uses existing status qosClass when present",
			pod: func() *corev1.Pod {
				pod := bestEffortPodForQoSTest()
				pod.Status.QOSClass = corev1.PodQOSGuaranteed

				return pod
			}(),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSGuaranteed,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "Guaranteed" at status.qosClass is allowed by namespace rule`,
			},
		},
		{
			name: "empty pod still evaluates as BestEffort",
			pod:  &corev1.Pod{},
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				qosEnforceForTest(
					apirules.ActionTypeAllow,
					corev1.PodQOSBestEffort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`QoS class "BestEffort" at status.qosClass is allowed by namespace rule`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := podRulesForTest()

			evaluation, err := h.validateQoSClasses(tt.pod, tt.enforceBodies)

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
				msg := decisionMessageForQoSTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenPodQoSClass {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenPodQoSClass)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenPodQoSClass {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenPodQoSClass)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenPodQoSClass {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenPodQoSClass)
				}
			}
		})
	}
}

func qosEnforceForTest(
	action apirules.ActionType,
	classes ...corev1.PodQOSClass,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			QoSClasses: classes,
		},
	}
}

func bestEffortPodForQoSTest() *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "shell",
					Image: "busybox",
				},
			},
		},
	}
}

func burstablePodForQoSTest() *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "shell",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
		},
	}
}

func guaranteedPodForQoSTest() *corev1.Pod {
	cpu := resource.MustParse("100m")
	memory := resource.MustParse("128Mi")

	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "shell",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    cpu,
							corev1.ResourceMemory: memory,
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    cpu,
							corev1.ResourceMemory: memory,
						},
					},
				},
			},
		},
	}
}

func decisionMessageForQoSTest(evaluation interface {
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

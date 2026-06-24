// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestServiceRulesValidateServiceTypes(t *testing.T) {
	tests := []struct {
		name          string
		svc           *corev1.Service
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantBlocking  bool
		wantFinal     bool
		wantErr       string
		wantMessage   []string
	}{
		{
			name: "ClusterIP service without rules returns empty evaluation",
			svc:  serviceTypeServiceForTest("cluster-ip", corev1.ServiceTypeClusterIP),
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
			svc:  serviceTypeServiceForTest("cluster-ip", corev1.ServiceTypeClusterIP),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "ClusterIP" at spec.type is allowed by namespace rule`,
				`service type "ClusterIP" matched "ClusterIP"`,
			},
		},
		{
			name: "allow ClusterIP service type",
			svc:  serviceTypeServiceForTest("cluster-ip", corev1.ServiceTypeClusterIP),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "ClusterIP" at spec.type is allowed by namespace rule`,
				`service type "ClusterIP" matched "ClusterIP"`,
			},
		},
		{
			name: "allow NodePort service type",
			svc:  serviceTypeServiceForTest("node-port", corev1.ServiceTypeNodePort),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeNodePort,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "NodePort" at spec.type is allowed by namespace rule`,
				`service type "NodePort" matched "NodePort"`,
			},
		},
		{
			name: "allow LoadBalancer service type",
			svc:  serviceTypeServiceForTest("load-balancer", corev1.ServiceTypeLoadBalancer),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "LoadBalancer" at spec.type is allowed by namespace rule`,
				`service type "LoadBalancer" matched "LoadBalancer"`,
			},
		},
		{
			name: "allow ExternalName service type",
			svc:  serviceTypeServiceForTest("external-name", corev1.ServiceTypeExternalName),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeExternalName,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "ExternalName" at spec.type is allowed by namespace rule`,
				`service type "ExternalName" matched "ExternalName"`,
			},
		},
		{
			name: "empty Kubernetes service type is treated as ClusterIP",
			svc:  serviceTypeServiceForTest("default-cluster-ip", corev1.ServiceType("")),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "ClusterIP" at spec.type is allowed by namespace rule`,
				`service type "ClusterIP" matched "ClusterIP"`,
			},
		},
		{
			name: "allow miss denies service type missing from allowed list",
			svc:  serviceTypeServiceForTest("external-name", corev1.ServiceTypeExternalName),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`service type "ExternalName" at spec.type is not allowed by namespace rule`,
				"Allowed service types",
				"ClusterIP",
			},
		},
		{
			name: "allow miss reports multiple allowed service types",
			svc:  serviceTypeServiceForTest("external-name", corev1.ServiceTypeExternalName),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
					apirules.ServiceTypeNodePort,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`service type "ExternalName" at spec.type is not allowed by namespace rule`,
				"Allowed service types",
				"ClusterIP",
				"NodePort",
				"LoadBalancer",
			},
		},
		{
			name: "deny matching service type",
			svc:  serviceTypeServiceForTest("load-balancer", corev1.ServiceTypeLoadBalancer),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeDeny,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`service type "LoadBalancer" at spec.type is denied by namespace rule`,
				`service type "LoadBalancer" matched "LoadBalancer"`,
			},
		},
		{
			name: "default action is deny",
			svc:  serviceTypeServiceForTest("node-port", corev1.ServiceTypeNodePort),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					"",
					apirules.ServiceTypeNodePort,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`service type "NodePort" at spec.type is denied by namespace rule`,
				`service type "NodePort" matched "NodePort"`,
			},
		},
		{
			name: "later deny overrides earlier allow",
			svc:  serviceTypeServiceForTest("load-balancer", corev1.ServiceTypeLoadBalancer),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeLoadBalancer,
				),
				serviceTypeEnforceForTest(
					apirules.ActionTypeDeny,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`service type "LoadBalancer" at spec.type is denied by namespace rule`,
				`service type "LoadBalancer" matched "LoadBalancer"`,
			},
		},
		{
			name: "later allow overrides earlier deny",
			svc:  serviceTypeServiceForTest("load-balancer", corev1.ServiceTypeLoadBalancer),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeDeny,
					apirules.ServiceTypeLoadBalancer,
				),
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "LoadBalancer" at spec.type is allowed by namespace rule`,
				`service type "LoadBalancer" matched "LoadBalancer"`,
			},
		},
		{
			name: "non matching later deny does not override earlier allow",
			svc:  serviceTypeServiceForTest("cluster-ip", corev1.ServiceTypeClusterIP),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
				serviceTypeEnforceForTest(
					apirules.ActionTypeDeny,
					apirules.ServiceTypeLoadBalancer,
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`service type "ClusterIP" at spec.type is allowed by namespace rule`,
				`service type "ClusterIP" matched "ClusterIP"`,
			},
		},
		{
			name: "audit match is observational",
			svc:  serviceTypeServiceForTest("external-name", corev1.ServiceTypeExternalName),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAudit,
					apirules.ServiceTypeExternalName,
				),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantMessage: []string{
				`service type "ExternalName" at spec.type matched audit namespace rule`,
				`service type "ExternalName" matched "ExternalName"`,
			},
		},
		{
			name: "audit does not satisfy allow list",
			svc:  serviceTypeServiceForTest("external-name", corev1.ServiceTypeExternalName),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionTypeAudit,
					apirules.ServiceTypeExternalName,
				),
				serviceTypeEnforceForTest(
					apirules.ActionTypeAllow,
					apirules.ServiceTypeClusterIP,
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`service type "ExternalName" at spec.type is not allowed by namespace rule`,
				"Allowed service types",
				"ClusterIP",
			},
		},
		{
			name: "unsupported action returns error",
			svc:  serviceTypeServiceForTest("cluster-ip", corev1.ServiceTypeClusterIP),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				serviceTypeEnforceForTest(
					apirules.ActionType("invalid"),
					apirules.ServiceTypeClusterIP,
				),
			},
			wantErr: `service type: unsupported rule action "invalid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := serviceRulesForTest()

			evaluation, err := h.validateServiceTypes(tt.svc, tt.enforceBodies)

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

			if len(tt.wantMessage) > 0 {
				msg := decisionMessageForServiceTypeTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenServiceType {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenServiceType)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenServiceType {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenServiceType)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenServiceType {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenServiceType)
				}
			}
		})
	}
}

func serviceTypeEnforceForTest(
	action apirules.ActionType,
	types ...apirules.ServiceType,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Services: apirules.NamespaceRuleEnforceServicesBody{
			Types: types,
		},
	}
}

func serviceTypeServiceForTest(
	name string,
	serviceType corev1.ServiceType,
) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: serviceType,
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

func decisionMessageForServiceTypeTest(evaluation interface {
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

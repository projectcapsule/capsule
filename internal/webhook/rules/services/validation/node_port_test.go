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

func TestServiceRulesValidateNodePorts(t *testing.T) {
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
			name: "ClusterIP service returns nil evaluation",
			svc:  clusterIPServiceForNodePortTest("cluster-ip"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30000, 32767)),
			},
			wantNil: true,
		},
		{
			name: "ExternalName service returns nil evaluation",
			svc:  externalNameServiceForNodePortTest("external", "api.example.com"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30000, 32767)),
			},
			wantNil: true,
		},
		{
			name: "LoadBalancer with nodePort allocation disabled returns nil evaluation",
			svc:  loadBalancerServiceForNodePortTest("lb", false, 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30000, 32767)),
			},
			wantNil: true,
		},
		{
			name: "NodePort without values and without nodePort rules returns nil evaluation",
			svc:  nodePortServiceForTest("node-port", 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantNil: true,
		},
		{
			name: "requires explicit nodePort when ranges are configured",
			svc:  nodePortServiceForTest("node-port", 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				"service requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
		},
		{
			name: "requires explicit LoadBalancer nodePort when allocation is enabled and ranges are configured",
			svc:  loadBalancerServiceForNodePortTest("lb", true, 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				"service requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
		},
		{
			name: "requires explicit LoadBalancer nodePort when allocation default is enabled and ranges are configured",
			svc:  loadBalancerServiceForNodePortTestWithDefaultAllocation("lb", 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				"service requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
		},
		{
			name: "allows explicit nodePort inside range",
			svc:  nodePortServiceForTest("node-port", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`nodePort "30080" at spec.ports[0].nodePort is allowed by namespace rule`,
				"nodePort 30080 is within allowed range 30000-30100",
			},
		},
		{
			name: "allows explicit nodePort equal to single-port range",
			svc:  nodePortServiceForTest("node-port", 30500),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30500, 30500)),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`nodePort "30500" at spec.ports[0].nodePort is allowed by namespace rule`,
				"nodePort 30500 is within allowed range 30500",
			},
		},
		{
			name: "allow miss denies nodePort outside configured ranges",
			svc:  nodePortServiceForTest("node-port", 32080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(
					apirules.ActionTypeAllow,
					nodePortRangeForTest(30000, 30100),
					nodePortRangeForTest(30500, 30500),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`nodePort "32080" at spec.ports[0].nodePort is not allowed by namespace rule`,
				"Allowed ranges",
				"30000-30100",
				"30500",
			},
		},
		{
			name: "deny matching nodePort",
			svc:  nodePortServiceForTest("node-port", 30090),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30090, 30090)),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`nodePort "30090" at spec.ports[0].nodePort is denied by namespace rule`,
				"nodePort 30090 is within allowed range 30090",
			},
		},
		{
			name: "later deny overrides earlier allow",
			svc:  nodePortServiceForTest("node-port", 30090),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30090, 30090)),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`nodePort "30090" at spec.ports[0].nodePort is denied by namespace rule`,
				"nodePort 30090 is within allowed range 30090",
			},
		},
		{
			name: "later allow overrides earlier deny",
			svc:  nodePortServiceForTest("node-port", 30090),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30000, 32767)),
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30090, 30090)),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`nodePort "30090" at spec.ports[0].nodePort is allowed by namespace rule`,
				"nodePort 30090 is within allowed range 30090",
			},
		},
		{
			name: "audit match is observational",
			svc:  nodePortServiceForTest("node-port", 30090),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAudit, nodePortRangeForTest(30090, 30090)),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantMessage: []string{
				`nodePort "30090" at spec.ports[0].nodePort matched audit namespace rule`,
				"nodePort 30090 is within allowed range 30090",
			},
		},
		{
			name: "audit does not satisfy allow list",
			svc:  nodePortServiceForTest("node-port", 30090),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAudit, nodePortRangeForTest(30090, 30090)),
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30500, 30500)),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`nodePort "30090" at spec.ports[0].nodePort is not allowed by namespace rule`,
				"Allowed ranges",
				"30500",
			},
		},
		{
			name: "LoadBalancer with allocation enabled validates nodePort",
			svc:  loadBalancerServiceForNodePortTest("lb", true, 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`nodePort "30080" at spec.ports[0].nodePort is allowed by namespace rule`,
			},
		},
		{
			name: "LoadBalancer with default allocation validates nodePort",
			svc:  loadBalancerServiceForNodePortTestWithDefaultAllocation("lb", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantFinal:    true,
			wantBlocking: false,
			wantMessage: []string{
				`nodePort "30080" at spec.ports[0].nodePort is allowed by namespace rule`,
			},
		},
		{
			name: "LoadBalancer with allocation enabled denies nodePort outside range",
			svc:  loadBalancerServiceForNodePortTest("lb", true, 32080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`nodePort "32080" at spec.ports[0].nodePort is not allowed by namespace rule`,
				"Allowed ranges",
				"30000-30100",
			},
		},
		{
			name: "multiple ports deny if one nodePort misses allow list",
			svc: nodePortServiceWithPortsForTest(
				"node-port",
				[]int32{30080, 32080},
			),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`nodePort "32080" at spec.ports[1].nodePort is not allowed by namespace rule`,
				"Allowed ranges",
				"30000-30100",
			},
		},
		{
			name: "multiple ports with zero nodePort skips zero value",
			svc: nodePortServiceWithPortsForTest(
				"node-port",
				[]int32{0, 30080},
			),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`nodePort "30080" at spec.ports[1].nodePort is allowed by namespace rule`,
			},
		},
		{
			name: "invalid configured range returns matcher error",
			svc:  nodePortServiceForTest("node-port", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeDeny, nodePortRangeForTest(30100, 30000)),
			},
			wantErr: `nodePort: invalid rule: invalid nodePort range: from 30100 must be lower than or equal to 30000`,
		},
		{
			name: "unsupported action returns error",
			svc:  nodePortServiceForTest("node-port", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionType("invalid"), nodePortRangeForTest(30000, 30100)),
			},
			wantErr: `nodePort: unsupported rule action "invalid"`,
		},
		{
			name: "nil enforce body is ignored",
			svc:  nodePortServiceForTest("node-port", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			wantFinal:    true,
			wantBlocking: false,
		},
		{
			name: "enforce without nodePort rules is ignored",
			svc:  nodePortServiceForTest("node-port", 30080),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			wantFinal:    false,
			wantBlocking: false,
		},
		{
			name: "empty nodePort range list does not require explicit nodePort",
			svc:  nodePortServiceForTest("node-port", 0),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Services: apirules.NamespaceRuleEnforceServicesBody{
						NodePorts: &apirules.ServiceNodePortRule{},
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := serviceRulesForTest()

			evaluation, err := h.validateNodePorts(tt.svc, tt.enforceBodies)

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
				msg := decisionMessageForNodePortTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenNodePort {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenNodePort)
				}
			}

			if evaluation.Blocking != nil {
				if evaluation.Blocking.EventReason != events.ReasonForbiddenNodePort {
					t.Fatalf("blocking event reason = %q, want %q", evaluation.Blocking.EventReason, events.ReasonForbiddenNodePort)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenNodePort {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenNodePort)
				}
			}
		})
	}
}

func TestDescribeNodePortRange(t *testing.T) {
	tests := []struct {
		name string
		in   apirules.ServiceNodePortRange
		want string
	}{
		{
			name: "range",
			in:   nodePortRangeForTest(30000, 30100),
			want: "30000-30100",
		},
		{
			name: "single port",
			in:   nodePortRangeForTest(30500, 30500),
			want: "30500",
		},
		{
			name: "invalid range still describes values",
			in:   nodePortRangeForTest(30100, 30000),
			want: "30100-30000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeNodePortRange(tt.in)
			if got != tt.want {
				t.Fatalf("describeNodePortRange() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodePortValues(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want []struct {
			value string
			path  string
		}
	}{
		{
			name: "no ports",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: nil,
				},
			},
			want: nil,
		},
		{
			name: "zero nodePort is skipped",
			svc:  nodePortServiceForTest("node-port", 0),
			want: nil,
		},
		{
			name: "single nodePort",
			svc:  nodePortServiceForTest("node-port", 30080),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "30080",
					path:  "spec.ports[0].nodePort",
				},
			},
		},
		{
			name: "multiple nodePorts preserve index path and skip zero",
			svc: nodePortServiceWithPortsForTest(
				"node-port",
				[]int32{30080, 0, 30500},
			),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "30080",
					path:  "spec.ports[0].nodePort",
				},
				{
					value: "30500",
					path:  "spec.ports[2].nodePort",
				},
			},
		},
		{
			name: "LoadBalancer nodePorts are extracted too",
			svc:  loadBalancerServiceForNodePortTest("lb", true, 30080),
			want: []struct {
				value string
				path  string
			}{
				{
					value: "30080",
					path:  "spec.ports[0].nodePort",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodePortValues(tt.svc)

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

func TestRequiresNodePortRanges(t *testing.T) {
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
			name: "missing nodePort rules",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			want: false,
		},
		{
			name: "empty ports list",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				{
					Services: apirules.NamespaceRuleEnforceServicesBody{
						NodePorts: &apirules.ServiceNodePortRule{},
					},
				},
			},
			want: false,
		},
		{
			name: "range configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			want: true,
		},
		{
			name: "later range configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nil,
				{},
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30000, 30100)),
			},
			want: true,
		},
		{
			name: "invalid range still counts as configured",
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				nodePortEnforceForTest(apirules.ActionTypeAllow, nodePortRangeForTest(30100, 30000)),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiresNodePortRanges(tt.enforceBodies)
			if got != tt.want {
				t.Fatalf("requiresNodePortRanges() = %t, want %t", got, tt.want)
			}
		})
	}
}

func nodePortEnforceForTest(
	action apirules.ActionType,
	ports ...apirules.ServiceNodePortRange,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Services: apirules.NamespaceRuleEnforceServicesBody{
			NodePorts: &apirules.ServiceNodePortRule{
				Ports: ports,
			},
		},
	}
}

func nodePortRangeForTest(from int32, to int32) apirules.ServiceNodePortRange {
	return apirules.ServiceNodePortRange{
		From: from,
		To:   to,
	}
}

func nodePortServiceForTest(name string, nodePort int32) *corev1.Service {
	return nodePortServiceWithPortsForTest(name, []int32{nodePort})
}

func nodePortServiceWithPortsForTest(name string, nodePorts []int32) *corev1.Service {
	ports := make([]corev1.ServicePort, 0, len(nodePorts))

	for i, nodePort := range nodePorts {
		ports = append(ports, corev1.ServicePort{
			Name:       "port-" + string(rune('a'+i)),
			Port:       int32(8080 + i),
			TargetPort: intstr.FromInt(8080 + i),
			NodePort:   nodePort,
		})
	}

	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeNodePort,
			Ports: ports,
		},
	}
}

func loadBalancerServiceForNodePortTest(
	name string,
	allocate bool,
	nodePort int32,
) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                          corev1.ServiceTypeLoadBalancer,
			AllocateLoadBalancerNodePorts: &allocate,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   nodePort,
				},
			},
		},
	}
}

func loadBalancerServiceForNodePortTestWithDefaultAllocation(
	name string,
	nodePort int32,
) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   nodePort,
				},
			},
		},
	}
}

func clusterIPServiceForNodePortTest(name string) *corev1.Service {
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

func externalNameServiceForNodePortTest(name string, externalName string) *corev1.Service {
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

func decisionMessageForNodePortTest(evaluation interface {
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

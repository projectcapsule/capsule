// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func TestPodConditionsScopeAndUpdateReevaluation(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, expression string
		want             int
		failure          string
	}{
		{"false skips only gated body", "false", 1, ""},
		{"true includes gated body", "true", 2, ""},
		{"runtime error fails", "object.spec.missing == 'x'", 0, `enforce: enforcement rule[0]: conditions[0] ("condition")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evaluated := false
			h := &podRules{compiler: c, rules: []podRuleValidator{{changed: func(*corev1.Pod, *corev1.Pod) bool { return false }, evaluate: func(_ *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
				evaluated = true
				if len(bodies) != tc.want {
					return nil, fmt.Errorf("got %d bodies, want %d", len(bodies), tc.want)
				}
				return nil, nil
			}}}}
			bodies := []*rules.NamespaceRuleEnforceBody{
				{Conditions: []rules.AdmissionCondition{{Name: "condition", Expression: tc.expression}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Tolerations: []rules.WorkloadTolerationMatch{{}}}},
				{},
			}
			original := bodies[0].DeepCopy()
			pod := &corev1.Pod{}
			err := h.validatePodRules(context.Background(), admission.Request{}, pod, nil, nil, bodies, pod.DeepCopy())
			if tc.failure == "" {
				if err != nil || !evaluated {
					t.Fatalf("evaluation=%v err=%v", evaluated, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("error=%v", err)
			}
			if bodies[0].Conditions[0] != original.Conditions[0] {
				t.Fatal("mutated rule")
			}
		})
	}
}

func TestPlacementOnlyConditionsSkipSubresources(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	h := PodRules(nil, nil, c).(*podRules)
	bodies := []*rules.NamespaceRuleEnforceBody{{
		Conditions: []rules.AdmissionCondition{{Expression: `object.spec.missing == 'x'`}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{

			Tolerations: []rules.WorkloadTolerationMatch{{}},
		}}}
	for _, subresource := range []string{"status", "ephemeralcontainers"} {
		req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: subresource}}
		if err := h.validatePodRules(context.Background(), req, &corev1.Pod{}, nil, nil, bodies); err != nil {
			t.Fatalf("%s evaluated placement condition: %v", subresource, err)
		}
	}
	if c.Stats() != 0 {
		t.Fatal("irrelevant condition was compiled")
	}
}

func TestPodSkipsConditionsForServiceOnlyRule(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	body := &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.type == 'NodePort'"}}, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}}}
	err = PodRules(nil, nil, compiler).(*podRules).validatePodRules(t.Context(), admission.Request{}, &corev1.Pod{}, nil, nil, []*rules.NamespaceRuleEnforceBody{body})
	if err != nil || compiler.Stats() != 0 {
		t.Fatalf("unrelated gate evaluated: err=%v compiled=%d", err, compiler.Stats())
	}
}

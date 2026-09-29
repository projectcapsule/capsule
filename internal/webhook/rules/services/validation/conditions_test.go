// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func TestServiceConditionsGateEnforcement(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{1, 2} {
		expression := "false"
		if want == 2 {
			expression = `object.spec.type == 'ClusterIP'`
		}
		called := false
		h := &serviceRules{compiler: c, rules: []serviceRuleValidator{func(_ *corev1.Service, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
			called = true
			if len(bodies) != want {
				return nil, fmt.Errorf("got %d rules, want %d", len(bodies), want)
			}
			return nil, nil
		}}}
		bodies := []*rules.NamespaceRuleEnforceBody{{Conditions: []rules.AdmissionCondition{{Expression: expression}}, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}}}, {}}
		err := h.validateServiceRules(context.Background(), admission.Request{}, &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}}, nil, nil, bodies)
		if err != nil || !called {
			t.Fatalf("called=%v err=%v", called, err)
		}
	}
}

func TestServiceConditionErrorLocation(t *testing.T) {
	c, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	h := &serviceRules{compiler: c}
	bodies := []*rules.NamespaceRuleEnforceBody{{Conditions: []rules.AdmissionCondition{{Name: "service-gate", Expression: "object.spec.missing == 'x'"}}, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}}}}
	err = h.validateServiceRules(context.Background(), admission.Request{}, &corev1.Service{}, nil, nil, bodies)
	if err == nil || !strings.Contains(err.Error(), `enforce: enforcement rule[0]: conditions[0] ("service-gate")`) {
		t.Fatalf("missing condition location: %v", err)
	}
}

func TestServiceSkipsConditionsForWorkloadOnlyRule(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	body := &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.containers.size() > 0"}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Tolerations: []rules.WorkloadTolerationMatch{{}}}}
	err = ServiceRules(nil, compiler).(*serviceRules).validateServiceRules(t.Context(), admission.Request{}, &corev1.Service{}, nil, nil, []*rules.NamespaceRuleEnforceBody{body})
	if err != nil || compiler.Stats() != 0 {
		t.Fatalf("unrelated gate evaluated: err=%v compiled=%d", err, compiler.Stats())
	}
}

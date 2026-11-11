// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type recordingConditions struct {
	seen   map[string]int
	reject string
}

func (c *recordingConditions) ValidateTriggerCondition(expression string) error {
	c.seen[expression]++
	if expression == c.reject {
		return fmt.Errorf("invalid condition")
	}
	return nil
}

func TestTriggerBudgetPrecedesCELValidation(t *testing.T) {
	r, cfg, c := registrationWorkload(t, 5)
	counter := &recordingConditions{seen: map[string]int{}}
	r.conditions = counter
	// Candidate gathering performs cheap structure checks, even for more than
	// the native cache's working set of 8192 distinct expressions.
	candidates, err := r.triggerWebhooks(t.Context(), cfg.Client)
	if err != nil || len(candidates) != 160 || len(counter.seen) != 0 {
		t.Fatalf("candidate count=%d validations=%d err=%v", len(candidates), len(counter.seen), err)
	}
	if err := r.reconcileValidatingConfiguration(t.Context(), cfg, false); err != nil {
		t.Fatal(err)
	}
	result := &admissionv1.ValidatingWebhookConfiguration{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: string(cfg.Name)}, result); err != nil {
		t.Fatal(err)
	}
	published := 0
	enforcement := false
	expressions := map[string]bool{}
	for _, hook := range result.Webhooks {
		if !isTriggerRegistration(hook) {
			enforcement = true
			continue
		}
		published++
		for _, condition := range hook.MatchConditions {
			if !strings.HasPrefix(condition.Name, "capsule-trigger-") {
				expressions[condition.Expression] = true
			}
		}
	}
	if !enforcement || published == 0 || published > maxTriggerGroups || len(counter.seen) != published*60 {
		t.Fatalf("published=%d validations=%d enforcement=%t", published, len(counter.seen), enforcement)
	}
	for expression := range counter.seen {
		if !expressions[expression] {
			t.Fatal("compiled an omitted group")
		}
	}
	for range 2 {
		if err := r.reconcileValidatingConfiguration(t.Context(), cfg, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(counter.seen) != published*60 {
		t.Fatal("repeated rebuild compiled new omitted expressions")
	}
	// Updating enforcement must still work while trigger inputs exceed budget.
	obj := &capsulev1beta2.CapsuleConfiguration{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: "default"}, obj); err != nil {
		t.Fatal(err)
	}
	obj.Spec.Admission.Validating.Webhooks[0].Name = "updated-enforcement.projectcapsule.dev"
	if err := c.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileValidatingConfiguration(t.Context(), obj.Spec.Admission.Validating, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Name: string(cfg.Name)}, result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hook := range result.Webhooks {
		found = found || hook.Name == "updated-enforcement.projectcapsule.dev"
	}
	if !found {
		t.Fatal("over-budget trigger inputs blocked enforcement refresh")
	}
}

func TestInvalidSelectedCELDoesNotBlockEnforcement(t *testing.T) {
	r, cfg, c := registrationWorkload(t, 1)
	source := &capsulev1beta2.TenantResource{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: "source-0", Namespace: "tenant-0"}, source); err != nil {
		t.Fatal(err)
	}
	expression := source.Spec.Triggers[0].MatchConditions[0].Expression
	r.conditions = &recordingConditions{seen: map[string]int{}, reject: expression}
	if err := r.reconcileValidatingConfiguration(t.Context(), cfg, false); err != nil {
		t.Fatal(err)
	}
	result := &admissionv1.ValidatingWebhookConfiguration{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: string(cfg.Name)}, result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hook := range result.Webhooks {
		found = found || !isTriggerRegistration(hook)
		for _, condition := range hook.MatchConditions {
			if condition.Expression == expression {
				t.Fatal("published invalid CEL")
			}
		}
	}
	if !found {
		t.Fatal("invalid trigger blocked enforcement")
	}
}

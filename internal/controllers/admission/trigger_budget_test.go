// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestTriggerBudgetPreservesEnforcement(t *testing.T) {
	legacy := admissionv1.ValidatingWebhook{Name: "enforcement.example.com", FailurePolicy: ptr.To(admissionv1.Fail)}
	var hooks []admissionv1.ValidatingWebhook
	for i := range maxTriggerGroups + 2 {
		path := replication.TriggerPath + fmt.Sprint(i)
		hooks = append(hooks, admissionv1.ValidatingWebhook{Name: fmt.Sprint(i), ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{Path: &path}}})
	}
	hooks = append(hooks, legacy)
	original := make([]admissionv1.ValidatingWebhook, len(hooks))
	copy(original, hooks)
	bounded, omitted, err := boundTriggerRegistrations(hooks)
	if err != nil || omitted != 2 || len(bounded) != maxTriggerGroups+1 {
		t.Fatalf("unexpected budget result: count=%d omitted=%d err=%v", len(bounded), omitted, err)
	}
	if !reflect.DeepEqual(bounded[len(bounded)-1], legacy) || !reflect.DeepEqual(hooks, original) {
		t.Fatal("enforcement changed or cache input was mutated")
	}
	large := legacy
	large.ClientConfig.CABundle = []byte(strings.Repeat("x", maxCombinedWebhookBytes))
	bounded, omitted, err = boundTriggerRegistrations(append([]admissionv1.ValidatingWebhook{large}, hooks[:2]...))
	if err != nil || omitted != 2 || len(bounded) != 1 || !reflect.DeepEqual(bounded[0], large) {
		t.Fatal("trigger budget crowded an existing enforcement configuration out")
	}
}

func BenchmarkTriggerRegistrationBudget(b *testing.B) {
	for _, count := range []int{0, 128, 256} {
		b.Run(fmt.Sprintf("groups=%d", count), func(b *testing.B) {
			hooks := []admissionv1.ValidatingWebhook{{Name: "enforcement.example.com", FailurePolicy: ptr.To(admissionv1.Fail)}}
			for i := range count {
				path := replication.TriggerPath + fmt.Sprint(i)
				hooks = append(hooks, admissionv1.ValidatingWebhook{Name: fmt.Sprint(i), ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{Path: &path}}})
			}
			b.ReportAllocs()
			for b.Loop() {
				bounded, omitted, err := boundTriggerRegistrations(hooks)
				if err != nil || len(bounded) != 1+min(count, maxTriggerGroups) || omitted != max(0, count-maxTriggerGroups) {
					b.Fatalf("unexpected budget result: count=%d omitted=%d err=%v", len(bounded), omitted, err)
				}
				if bounded[0].FailurePolicy == nil || *bounded[0].FailurePolicy != admissionv1.Fail {
					b.Fatal("enforcement was lost")
				}
			}
		})
	}
}

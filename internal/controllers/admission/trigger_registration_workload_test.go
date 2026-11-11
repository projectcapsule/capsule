// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func registrationWorkload(tb testing.TB, count int) (*validatingReconciler, *capsulev1beta2.DynamicValidatingAdmissionConfig, client.Client) {
	tb.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{capsulev1beta2.AddToScheme, admissionv1.AddToScheme} {
		if err := add(scheme); err != nil {
			tb.Fatal(err)
		}
	}
	url := "https://capsule.example.com"
	side := admissionv1.SideEffectClassNone
	cfg := &capsulev1beta2.CapsuleConfiguration{Name: "default", UID: "cfg", Spec: capsulev1beta2.CapsuleConfigurationSpec{Admission: capsulev1beta2.DynamicAdmission{Validating: &capsulev1beta2.DynamicValidatingAdmissionConfig{Name: "registration", Client: &admissionv1.WebhookClientConfig{URL: &url}, Webhooks: []*admissionapi.ValidatingWebhook{{Name: "enforcement.projectcapsule.dev", SideEffects: &side, AdmissionReviewVersions: []string{"v1"}}}}}}}
	objects := []client.Object{cfg}
	for i := range count {
		spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{}}
		for j := range 32 {
			trigger := eventTrigger()
			trigger.Name = fmt.Sprintf("t%d", j)
			for k := range 60 {
				trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("c%d", k), Expression: fmt.Sprintf("request.name != '%d-%d-%d'", i, j, k)})
			}
			spec.Triggers = append(spec.Triggers, trigger)
		}
		objects = append(objects, &capsulev1beta2.TenantResource{Name: fmt.Sprintf("source-%d", i), Namespace: fmt.Sprintf("tenant-%d", i), Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&capsulev1beta2.TenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.GlobalTenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).Build()
	conditions, err := cache.NewCELCache()
	if err != nil {
		tb.Fatal(err)
	}
	r := &validatingReconciler{client: c, configuration: configuration.NewCapsuleConfiguration(tb.Context(), c, c, nil, cfg.Name), conditions: conditions, delivery: testDeliveryKey(), log: logr.Discard()}
	return r, cfg.Spec.Admission.Validating, c
}

func BenchmarkTriggerRegistrationOverBudget(b *testing.B) {
	for _, count := range []int{1, 5, 10} {
		for _, mode := range []string{"warm", "cold"} {
			b.Run(fmt.Sprintf("parents=%d/%s", count, mode), func(b *testing.B) {
				r, cfg, c := registrationWorkload(b, count)
				if err := r.reconcileValidatingConfiguration(b.Context(), cfg, false); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if mode == "cold" {
						any(r.conditions).(*cache.CELCache).Reset()
					}
					if err := r.reconcileValidatingConfiguration(b.Context(), cfg, false); err != nil {
						b.Fatal(err)
					}
				}
				out := &admissionv1.ValidatingWebhookConfiguration{}
				if err := c.Get(b.Context(), client.ObjectKey{Name: string(cfg.Name)}, out); err != nil {
					b.Fatal(err)
				}
				if len(out.Webhooks) < 2 || len(out.Webhooks) > maxTriggerGroups+1 {
					b.Fatalf("published %d hooks", len(out.Webhooks))
				}
				b.ReportMetric(float64(len(out.Webhooks)-1), "published-groups")
			})
		}
	}
}

func eventTrigger() capsulev1beta2.TenantResourceTrigger {
	return capsulev1beta2.TenantResourceTrigger{Name: "changes", Operations: []admissionv1.OperationType{admissionv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func newAdmissionBenchmarkClient(tb testing.TB) client.Client {
	tb.Helper()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithIndex(&capsulev1beta2.TenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.GlobalTenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).Build()
}

func TestTriggerRegistrationsGroupNamespacesAndRemoveSubscriptions(t *testing.T) {
	f := newAdmissionConfigurationFixture(t, "validating", 1, false)
	r := f.reconciler.(*validatingReconciler)
	trigger := capsulev1beta2.TenantResourceTrigger{Name: "secret-events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}
	for _, ns := range []string{"team-a", "team-b"} {
		obj := &capsulev1beta2.TenantResource{Name: "subscriber", Namespace: ns, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}
		if err := f.client.Create(t.Context(), obj); err != nil {
			t.Fatal(err)
		}
	}
	hooks, err := r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || len(hooks[0].NamespaceSelector.MatchExpressions) != 1 || len(hooks[0].NamespaceSelector.MatchExpressions[0].Values) != 2 {
		t.Fatalf("registrations not consolidated: %+v", hooks)
	}
	global := &capsulev1beta2.GlobalTenantResource{Name: "global", Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}
	if err := f.client.Create(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	hooks, err = r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || hooks[0].NamespaceSelector != nil {
		t.Fatal("global subscription incorrectly restricted to local namespaces")
	}
	global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "production"}}
	if err := f.client.Update(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	hooks, err = r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 {
		t.Fatal("distinct namespace profiles combined")
	}
	for _, obj := range []client.Object{global, &capsulev1beta2.TenantResource{Name: "subscriber", Namespace: "team-a"}, &capsulev1beta2.TenantResource{Name: "subscriber", Namespace: "team-b"}} {
		if err := f.client.Delete(t.Context(), obj); err != nil {
			t.Fatal(err)
		}
	}
	hooks, err = r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
	if err != nil || len(hooks) != 0 {
		t.Fatalf("stale registrations after deletion: %+v %v", hooks, err)
	}
}

func BenchmarkTriggerRegistration(b *testing.B) {
	for _, count := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("identical-namespaces=%d", count), func(b *testing.B) {
			schemeClient := newAdmissionBenchmarkClient(b)
			trigger := capsulev1beta2.TenantResourceTrigger{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}
			for i := range count {
				if err := schemeClient.Create(b.Context(), &capsulev1beta2.TenantResource{Name: "source", Namespace: fmt.Sprintf("tenant-%d", i), Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}); err != nil {
					b.Fatal(err)
				}
			}
			r := &validatingReconciler{delivery: testDeliveryKey(), client: schemeClient}
			url := "https://capsule.example.com"
			config := &admissionv1.WebhookClientConfig{URL: &url}
			b.ReportAllocs()
			for b.Loop() {
				hooks, err := r.triggerWebhooks(b.Context(), config)
				if err != nil || len(hooks) != 1 {
					b.Fatalf("grouping failed: %v %+v", err, hooks)
				}
				if *hooks[0].ClientConfig.URL != url+testDeliveryKey().Path(replication.MatchID(&trigger)) {
					b.Fatal("incorrect route")
				}
			}
		})
	}
}

func testDeliveryKey() *replication.DeliveryKey {
	key, _ := replication.NewDeliveryKey(make([]byte, 32))
	return key
}

func TestTriggerRegistrationRequiresDeliveryAuthentication(t *testing.T) {
	c := newAdmissionBenchmarkClient(t)
	source := &capsulev1beta2.TenantResource{Name: "source", Namespace: "ns", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}}}}
	if err := c.Create(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	r := &validatingReconciler{client: c}
	if hooks, err := r.triggerWebhooks(t.Context(), nil); err != nil || len(hooks) != 0 {
		t.Fatal("published unauthenticated trigger")
	}

	f := newAdmissionConfigurationFixture(t, "validating", 1, false)
	r = f.reconciler.(*validatingReconciler)
	r.delivery = nil
	if err := f.client.Create(t.Context(), &capsulev1beta2.TenantResource{Name: "fallback", Namespace: source.Namespace, Spec: source.Spec}); err != nil {
		t.Fatal(err)
	}

	hooks, err := r.validatingWebhooks(t.Context(), r.configuration.Admission().Validating)
	if err != nil || len(hooks) == 0 {
		t.Fatalf("credential outage disabled enforcement: hooks=%d err=%v", len(hooks), err)
	}
	for _, hook := range hooks {
		if strings.HasPrefix(hook.Name, "trigger-") {
			t.Fatal("registered a trigger without delivery authentication")
		}
	}
}

func TestTriggerRegistrationDeletionStart(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			f := newAdmissionConfigurationFixture(t, "validating", 1, false)
			r := f.reconciler.(*validatingReconciler)
			trigger := capsulev1beta2.TenantResourceTrigger{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}
			spec := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}
			var source client.Object = &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", Finalizers: []string{"projectcapsule.dev/test-hold"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
			if global {
				source = &capsulev1beta2.GlobalTenantResource{Name: "source", Finalizers: []string{"projectcapsule.dev/test-hold"}, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: spec}}
			}
			if err := f.client.Create(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			hooks, err := r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
			if err != nil || len(hooks) != 1 {
				t.Fatalf("initial hooks=%d err=%v", len(hooks), err)
			}
			old := source.DeepCopyObject().(client.Object)
			if err := f.client.Delete(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			if err := f.client.Get(t.Context(), client.ObjectKeyFromObject(source), source); err != nil {
				t.Fatal(err)
			}
			if source.GetDeletionTimestamp().IsZero() || source.GetGeneration() != old.GetGeneration() {
				t.Fatal("fixture did not preserve generation at deletion start")
			}
			if !triggerSourceChanged().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: source}) {
				t.Fatal("watch discarded deletion start")
			}
			hooks, err = r.triggerWebhooks(t.Context(), r.configuration.Admission().Validating.Client)
			if err != nil || len(hooks) != 0 {
				t.Fatalf("retained deleting subscriptions: %d %v", len(hooks), err)
			}
			unchanged := old.DeepCopyObject().(client.Object)
			unchanged.SetResourceVersion("status-only")
			if triggerSourceChanged().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: unchanged}) {
				t.Fatal("watch admits status-only writes")
			}
		})
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestWatchIndexResourceOperationAndLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	trigger := capsulev1beta2.TenantResourceTrigger{Name: "source", Operations: []admissionv1.OperationType{admissionv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
	a := &capsulev1beta2.TenantResource{Name: "source", Namespace: "a", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}
	b := a.DeepCopy()
	b.Namespace = "b"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b).WithIndex(a, replication.TriggerIndexerField, (Triggers{}).Func()).Build()
	key := replication.ResourceKey(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, admissionv1.Create)
	count := func(ns, k string) int {
		t.Helper()
		list := &capsulev1beta2.TenantResourceList{}
		if err := c.List(t.Context(), list, client.InNamespace(ns), client.MatchingFields{replication.TriggerIndexerField: k}); err != nil {
			t.Fatal(err)
		}
		return len(list.Items)
	}
	if count("a", key) != 1 || count("b", key) != 1 || count("c", key) != 0 {
		t.Fatal("namespace scoping failed")
	}
	a.Spec.Triggers[0].Resources = []string{"secrets"}
	if err := c.Update(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if count("a", key) != 0 || count("b", key) != 1 {
		t.Fatal("resource relationship was not updated")
	}
	a.Spec.Cordoned = new(true)
	if err := c.Update(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if count("a", replication.TriggerEnabledKey) != 0 {
		t.Fatal("cordoned source indexed")
	}
	if err := c.Delete(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	if count("b", key) != 0 {
		t.Fatal("deleted source indexed")
	}
	a.Spec.Cordoned = new(false)
	a.Spec.Triggers[0].Resources = []string{"*"}
	if len((Triggers{}).Func()(a)) != 1 {
		t.Fatal("malformed stored input expanded a wildcard")
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestTriggerIndexTracksUpdatesAndNamespaceSeparation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	obj := &capsulev1beta2.TenantResource{Name: "subscriber", Namespace: "a", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "one"}}}}}
	other := obj.DeepCopy()
	other.Namespace = "b"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj, other).WithIndex(obj, replication.TriggerIndexerField, (Triggers{}).Func()).Build()
	id := replication.MatchID(&obj.Spec.Triggers[0])
	list := func(namespace string, key string) int {
		l := &capsulev1beta2.TenantResourceList{}
		if err := c.List(t.Context(), l, client.InNamespace(namespace), client.MatchingFields{replication.TriggerIndexerField: key}); err != nil {
			t.Fatal(err)
		}
		return len(l.Items)
	}
	if list("a", id) != 1 || list("b", id) != 1 || list("c", id) != 0 {
		t.Fatal("namespace separation lost")
	}
	obj.Spec.Triggers[0].ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "changed"}}
	if err := c.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	if list("a", id) != 0 || list("b", id) != 1 {
		t.Fatal("updated relationships leaked across namespaces")
	}
	obj.Spec.Cordoned = new(true)
	if err := c.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	if list("a", replication.TriggerEnabledKey) != 0 {
		t.Fatal("cordoned source remains enabled")
	}
	if err := c.Delete(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if list("b", id) != 0 {
		t.Fatal("deleted source still indexed")
	}
}

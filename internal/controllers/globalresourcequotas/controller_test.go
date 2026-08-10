// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequotas

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestObserveUsageTracksTotalAndNamespaces(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	hard := corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("8"),
		corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
	}
	quota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", UID: types.UID("quota-uid")},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			Quota: corev1.ResourceQuotaSpec{Hard: hard},
		},
	}
	namespaceA := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "a"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	namespaceB := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "b"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	resourceQuotaA := observedResourceQuota(quota, namespaceA.Name, corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("2"),
		corev1.ResourceRequestsMemory: resource.MustParse("3Gi"),
	})
	resourceQuotaB := observedResourceQuota(quota, namespaceB.Name, corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
	})

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(quota, resourceQuotaA, resourceQuotaB).
		Build()
	controller := &Controller{Client: cl, reader: cl}

	status, initialized, err := controller.observeUsage(
		context.Background(),
		quota,
		[]corev1.Namespace{namespaceB, namespaceA},
	)
	if err != nil {
		t.Fatalf("observeUsage() error = %v", err)
	}
	if !initialized {
		t.Fatal("observeUsage() initialized = false, want true")
	}
	if status.NamespaceSize != 2 || len(status.NamespaceUsage) != 2 {
		t.Fatalf("namespace status = size %d usage %#v", status.NamespaceSize, status.NamespaceUsage)
	}
	if status.Namespaces[0] != "a" || status.Namespaces[1] != "b" {
		t.Fatalf("ordered namespaces = %#v, want [a b]", status.Namespaces)
	}
	assertResource(t, status.Total.Used, corev1.ResourceRequestsCPU, "3")
	assertResource(t, status.Total.Used, corev1.ResourceRequestsMemory, "7Gi")
	assertResource(t, status.Total.Available, corev1.ResourceRequestsCPU, "5")
	assertResource(t, status.NamespaceUsage["b"].Used, corev1.ResourceRequestsMemory, "4Gi")
}

func TestReconcileLedgerStatusConsumesObservedUsage(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(time.Minute))
	current := &capsulev1beta2.QuantityLedgerResourceQuotaStatus{
		Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("0")},
		Reservations: []capsulev1beta2.QuantityLedgerResourceQuotaReservation{{
			ID:        "request",
			Delta:     corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("2")},
			ExpiresAt: &expires,
		}},
	}

	next := reconcileLedgerStatus(
		current,
		3,
		[]string{"a", "b"},
		corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")},
		true,
		now,
	)

	if len(next.Reservations) != 1 {
		t.Fatalf("reservations = %d, want 1", len(next.Reservations))
	}
	assertResource(t, next.Reservations[0].Delta, corev1.ResourceRequestsCPU, "1")
	assertResource(t, next.Used, corev1.ResourceRequestsCPU, "1")
	assertResource(t, next.Allocated, corev1.ResourceRequestsCPU, "2")
	if next.ObservedGeneration != 3 || len(next.Namespaces) != 2 {
		t.Fatalf("ledger snapshot = generation %d, namespaces %#v", next.ObservedGeneration, next.Namespaces)
	}
}

func TestMatchingNamespaceSelectorsUseOR(t *testing.T) {
	t.Parallel()

	quota := &capsulev1beta2.GlobalResourceQuota{
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{
				{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}}},
				{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "b"}}},
			},
		},
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "b", Labels: map[string]string{"team": "b"},
	}}

	cl := fake.NewClientBuilder().WithObjects(namespace).Build()
	matched, err := selectors.GetNamespacesMatchingSelectors(
		context.Background(),
		cl,
		quota.Spec.NamespaceSelectors,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].Name != namespace.Name {
		t.Fatalf("matched namespaces = %#v, want b", matched)
	}
}

func observedResourceQuota(
	quota *capsulev1beta2.GlobalResourceQuota,
	namespace string,
	used corev1.ResourceList,
) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      quota.GetResourceQuotaName(),
			Namespace: namespace,
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: quota.Spec.Quota.Hard.DeepCopy(),
			Used: used.DeepCopy(),
		},
	}
}

func assertResource(
	t *testing.T,
	resources corev1.ResourceList,
	name corev1.ResourceName,
	want string,
) {
	t.Helper()

	got := resources[name]
	if got.Cmp(resource.MustParse(want)) != 0 {
		t.Fatalf("%s = %s, want %s", name, got.String(), want)
	}
}

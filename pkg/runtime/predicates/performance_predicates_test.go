// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestObjectMetadataChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.ObjectMetadataChangedPredicate{}
	oldPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Labels: map[string]string{"a": "b"}}}
	statusOnly := oldPod.DeepCopy()
	statusOnly.Status.Phase = corev1.PodRunning

	if p.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: statusOnly}) {
		t.Fatal("status-only update must be filtered")
	}

	metadataChanged := oldPod.DeepCopy()
	metadataChanged.Labels["a"] = "c"
	if !p.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: metadataChanged}) {
		t.Fatal("metadata update must be admitted")
	}

	if !p.Create(event.CreateEvent{Object: oldPod}) {
		t.Fatal("create must be admitted")
	}

	if p.Delete(event.DeleteEvent{Object: oldPod}) {
		t.Fatal("delete must be filtered")
	}
}

func TestTenantManagedResourceChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.TenantManagedResourceChangedPredicate{}

	tests := []struct {
		name string
		old  client.Object
		new  client.Object
		want bool
	}{
		{
			name: "limitrange spec drift without generation change",
			old:  &corev1.LimitRange{},
			new: &corev1.LimitRange{Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer,
			}}}},
			want: true,
		},
		{
			name: "resourcequota spec drift without generation change",
			old:  &corev1.ResourceQuota{},
			new: &corev1.ResourceQuota{Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			}}},
			want: true,
		},
		{
			name: "resourcequota usage status is filtered",
			old:  &corev1.ResourceQuota{},
			new: &corev1.ResourceQuota{Status: corev1.ResourceQuotaStatus{Used: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			}}},
			want: false,
		},
		{
			name: "networkpolicy spec drift",
			old:  &networkingv1.NetworkPolicy{},
			new: &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			}},
			want: true,
		},
		{
			name: "rolebinding subjects drift",
			old:  &rbacv1.RoleBinding{},
			new: &rbacv1.RoleBinding{Subjects: []rbacv1.Subject{{
				Kind: rbacv1.UserKind,
				Name: "alice",
			}}},
			want: true,
		},
		{
			name: "managed owner reference drift",
			old: &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{
				Kind: "Tenant", Name: "tenant",
			}}}},
			new:  &corev1.LimitRange{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Update(event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}); got != tt.want {
				t.Fatalf("Update() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestValidatingAdmissionConfigurationChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.ValidatingAdmissionConfigurationChangedPredicate{}
	withoutHash := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Webhooks:   []admissionv1.ValidatingWebhook{{Name: "namespaces.projectcapsule.dev"}},
	}
	obj := withoutHash.DeepCopy()
	obj.Annotations = map[string]string{
		predicates.AdmissionStateHashAnnotation: predicates.ValidatingAdmissionStateHash(obj),
	}
	if p.Update(event.UpdateEvent{ObjectOld: withoutHash, ObjectNew: obj}) {
		t.Fatal("controller hash update must not enqueue itself")
	}

	unchanged := obj.DeepCopy()
	unchanged.ResourceVersion = "2"
	if p.Update(event.UpdateEvent{ObjectOld: obj, ObjectNew: unchanged}) {
		t.Fatal("controller-authored state must not enqueue itself")
	}

	drifted := unchanged.DeepCopy()
	drifted.Webhooks[0].Name = "changed.projectcapsule.dev"
	if !p.Update(event.UpdateEvent{ObjectOld: unchanged, ObjectNew: drifted}) {
		t.Fatal("webhook drift must be admitted")
	}

	caOnly := unchanged.DeepCopy()
	caOnly.Webhooks[0].ClientConfig.CABundle = []byte("rotated")
	if p.Update(event.UpdateEvent{ObjectOld: unchanged, ObjectNew: caOnly}) {
		t.Fatal("CA rotation is owned by the TLS controller and must be filtered")
	}
}

func TestQuantityLedgerWorkChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.QuantityLedgerWorkChangedPredicate{}
	oldLedger := &capsulev1beta2.QuantityLedger{}
	derived := oldLedger.DeepCopy()
	derived.Status.Allocated = resource.MustParse("1")
	if p.Update(event.UpdateEvent{ObjectOld: oldLedger, ObjectNew: derived}) {
		t.Fatal("derived allocation update must be filtered")
	}

	work := oldLedger.DeepCopy()
	work.Status.Reservations = append(work.Status.Reservations, capsulev1beta2.QuantityLedgerReservation{ID: "request"})
	if !p.Update(event.UpdateEvent{ObjectOld: oldLedger, ObjectNew: work}) {
		t.Fatal("reservation update must be admitted")
	}
}

func TestResourcePoolNamespacesChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.ResourcePoolNamespacesChangedPredicate{}
	oldPool := &capsulev1beta2.ResourcePool{}
	statusOnly := oldPool.DeepCopy()
	statusOnly.Status.ClaimSize = 1
	if p.Update(event.UpdateEvent{ObjectOld: oldPool, ObjectNew: statusOnly}) {
		t.Fatal("unrelated pool status update must be filtered")
	}

	namespacesChanged := oldPool.DeepCopy()
	namespacesChanged.Status.Namespaces = []string{"tenant-a"}
	if !p.Update(event.UpdateEvent{ObjectOld: oldPool, ObjectNew: namespacesChanged}) {
		t.Fatal("namespace allocation update must be admitted")
	}

	deleting := oldPool.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	if !p.Update(event.UpdateEvent{ObjectOld: oldPool, ObjectNew: deleting}) {
		t.Fatal("deletion transition must be admitted")
	}
}

func TestResourceQuotaUsageChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.ResourceQuotaUsageChangedPredicate{}
	oldQuota := &corev1.ResourceQuota{}

	hardOnly := oldQuota.DeepCopy()
	hardOnly.Status.Hard = corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("2"),
	}
	if p.Update(event.UpdateEvent{ObjectOld: oldQuota, ObjectNew: hardOnly}) {
		t.Fatal("status.hard-only update must be filtered")
	}

	used := hardOnly.DeepCopy()
	used.Status.Used = corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("1"),
	}
	if !p.Update(event.UpdateEvent{ObjectOld: hardOnly, ObjectNew: used}) {
		t.Fatal("status.used update must be admitted")
	}

	usageReleased := used.DeepCopy()
	usageReleased.Status.Used[corev1.ResourceLimitsCPU] = resource.MustParse("0")
	if !p.Update(event.UpdateEvent{ObjectOld: used, ObjectNew: usageReleased}) {
		t.Fatal("released usage update must be admitted")
	}

	if p.Update(event.UpdateEvent{ObjectOld: oldQuota, ObjectNew: &corev1.ConfigMap{}}) {
		t.Fatal("non-ResourceQuota update must be filtered")
	}
}

func TestDeletionChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.DeletionChangedPredicate{}
	oldObject := &corev1.ConfigMap{}
	statusOnly := oldObject.DeepCopy()
	if p.Update(event.UpdateEvent{ObjectOld: oldObject, ObjectNew: statusOnly}) {
		t.Fatal("ordinary update must be filtered")
	}

	deleting := oldObject.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	if !p.Update(event.UpdateEvent{ObjectOld: oldObject, ObjectNew: deleting}) {
		t.Fatal("deletion timestamp transition must be admitted")
	}
}

func TestClassChangedAdmitsDeletionTransition(t *testing.T) {
	t.Parallel()

	p := predicates.ClassChanged()
	oldTenant := &capsulev1beta2.Tenant{}
	deleting := oldTenant.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	if !p.Update(event.UpdateEvent{ObjectOld: oldTenant, ObjectNew: deleting}) {
		t.Fatal("controller primary predicate must admit deletion transition")
	}
}

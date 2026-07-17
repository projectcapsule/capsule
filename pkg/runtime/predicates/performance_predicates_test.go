// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

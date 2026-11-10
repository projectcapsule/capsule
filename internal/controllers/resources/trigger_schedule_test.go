// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestTriggerSchedulePreservesPendingAndLifecycle(t *testing.T) {
	now := time.Unix(100, 0)
	obj := &capsulev1beta2.TenantResource{Generation: 2, Annotations: map[string]string{replication.TriggerAnnotation: "first"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}, TriggerMinInterval: metav1.Duration{Duration: 10 * time.Second}}}}
	status := &obj.Status.TenantResourceCommonStatus
	status.ObservedGeneration = 2
	if delay, token := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now); delay != 0 || token != "first" {
		t.Fatal("first event not immediately eligible")
	}
	recordTrigger(status, "first", now)
	obj.Annotations[replication.TriggerAnnotation] = "during-reconcile"
	if delay, token := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(time.Second)); delay != 9*time.Second || token != "during-reconcile" {
		t.Fatal("new event was lost or interval bypassed")
	}
	deadline := metav1.NewMicroTime(now.Add(time.Second))
	status.NextResyncAt = &deadline
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(500*time.Millisecond)); delay != 500*time.Millisecond {
		t.Fatal("cooldown discarded an earlier periodic queue deadline")
	}
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(2*time.Second)); delay != 0 {
		t.Fatal("pending cooldown delayed periodic recovery")
	}
	status.NextResyncAt = nil
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(10*time.Second)); delay != 0 {
		t.Fatal("trailing reconcile was not scheduled")
	}
	obj.Generation++
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(time.Second)); delay != 0 {
		t.Fatal("spec update throttled")
	}
	obj.Generation--
	obj.Annotations[meta.ReconcileAnnotation] = "manual"
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(time.Second)); delay != 0 {
		t.Fatal("manual request throttled")
	}
	delete(obj.Annotations, meta.ReconcileAnnotation)
	if !finishTrigger(status, "retry-after-failure", now.Add(2*time.Second), errors.New("dependency unavailable")) || status.Trigger.ObservedRequest != "first" {
		t.Fatal("failed attempt consumed pending notification")
	}
	obj.DeletionTimestamp = &metav1.Time{Time: now}
	if delay, _ := triggerSchedule(obj, &obj.Spec.TenantResourceCommonSpec, status, now.Add(time.Second)); delay != 0 {
		t.Fatal("deletion throttled")
	}
}

func TestTriggerInitialApplyRetryPreservesLegacyResync(t *testing.T) {
	err := errors.New("protected target parent status is not indexed yet")
	legacy := &capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}}
	if got := triggerRetryAfter(legacy, 24*time.Hour, err); got != 24*time.Hour {
		t.Fatal("legacy reconciliation timing changed")
	}
	legacy.Triggers = []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}
	legacy.TriggerMinInterval.Duration = 4 * time.Second
	if got := triggerRetryAfter(legacy, 24*time.Hour, err); got != 4*time.Second {
		t.Fatal("initial apply failure waits for long resync")
	}
	if got := triggerRetryAfter(legacy, time.Second, err); got != time.Second {
		t.Fatal("shorter retry was delayed")
	}
	if got := triggerRetryAfter(legacy, 24*time.Hour, nil); got != 24*time.Hour {
		t.Fatal("successful reconciliation was accelerated")
	}
	legacy.TriggerMinInterval.Duration = 24 * time.Hour
	if got := triggerRetryAfter(legacy, 24*time.Hour, err); got != time.Minute {
		t.Fatal("initial bootstrap cannot wait for a day-long trigger interval")
	}
}

func TestTriggerDispatcherIncarnationAndPendingToken(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	obj := &capsulev1beta2.TenantResource{Name: "source", Namespace: "ns", UID: "current", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
	d := NewTriggerDispatcher(c, c, logr.Discard(), metrics.NewTriggerRecorder())
	if d.NeedLeaderElection() {
		t.Fatal("webhook replica cannot dispatch without leadership")
	}
	target := replication.Target{Name: obj.Name, Namespace: obj.Namespace, UID: "old"}
	if err := d.dispatch(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	got := &capsulev1beta2.TenantResource{}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[replication.TriggerAnnotation] != "" {
		t.Fatal("stale notification affected recreated object")
	}
	target.UID = obj.UID
	if err := d.dispatch(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[replication.TriggerAnnotation] == "" {
		t.Fatal("notification was not persisted")
	}
	got.Spec.Cordoned = new(true)
	if err := c.Update(t.Context(), got); err != nil {
		t.Fatal(err)
	}
	previous := got.Annotations[replication.TriggerAnnotation]
	if err := d.dispatch(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[replication.TriggerAnnotation] != previous {
		t.Fatal("cordoned resource notified")
	}
}

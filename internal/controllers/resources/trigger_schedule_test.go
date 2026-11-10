// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	resourceindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestPendingTriggerPreservesAcknowledgement(t *testing.T) {
	now := time.Unix(100, 0)
	obj := &capsulev1beta2.TenantResource{Annotations: map[string]string{replication.TriggerAnnotation: "first"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}}}}
	status := &obj.Status.TenantResourceCommonStatus
	if pendingTriggerRequest(obj, &obj.Spec.TenantResourceCommonSpec, status) != "first" {
		t.Fatal("pending notification missing")
	}
	recordTrigger(status, "first", now)
	if pendingTriggerRequest(obj, &obj.Spec.TenantResourceCommonSpec, status) != "" {
		t.Fatal("handled notification remained pending")
	}
	obj.Annotations[replication.TriggerAnnotation] = "second"
	if !finishTrigger(status, "second", now, errors.New("dependency unavailable")) || status.Trigger.ObservedRequest != "first" {
		t.Fatal("failed attempt consumed notification")
	}
	obj.Annotations[replication.TriggerAnnotation] = "during-reconcile"
	finishTrigger(status, "second", now, nil)
	if pendingTriggerRequest(obj, &obj.Spec.TenantResourceCommonSpec, status) != "during-reconcile" {
		t.Fatal("later notification was acknowledged by older work")
	}
}

func TestDependencyRecoveryIsNotBlockedByPendingTrigger(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			common := capsulev1beta2.TenantResourceCommonSpec{Settings: capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: new(false), Force: new(false)}, Cordoned: new(false), PruningOnDelete: new(true), ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, TriggerMinInterval: metav1.Duration{Duration: 24 * time.Hour}, DependsOn: []meta.LocalRFC1123ObjectReference{{Name: "dependency"}}, Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}}}
			var parent, dependency client.Object
			var status, depStatus *capsulev1beta2.TenantResourceCommonStatus
			if global {
				p := &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "source", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
				d := &capsulev1beta2.GlobalTenantResource{Name: "dependency"}
				parent, dependency, status, depStatus = p, d, &p.Status.TenantResourceCommonStatus, &d.Status.TenantResourceCommonStatus
			} else {
				p := &capsulev1beta2.TenantResource{Name: "source", Namespace: "ns", UID: "source", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
				d := &capsulev1beta2.TenantResource{Name: "dependency", Namespace: "ns"}
				parent, dependency, status, depStatus = p, d, &p.Status.TenantResourceCommonStatus, &d.Status.TenantResourceCommonStatus
			}
			parent.SetAnnotations(map[string]string{replication.TriggerAnnotation: "pending"})
			status.ObservedGeneration = 1
			recordTrigger(status, "older", time.Now())
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, dependency, &corev1.Namespace{Name: "ns"}, &capsulev1beta2.Tenant{Name: "tenant"}).WithStatusSubresource(parent, dependency).WithIndex(&capsulev1beta2.TenantResource{}, resourceindex.NamespacedDependenciesFieldName, (resourceindex.NamespacedDependencies{}).Func()).WithIndex(&capsulev1beta2.GlobalTenantResource{}, resourceindex.GlobalDependenciesFieldName, (resourceindex.GlobalDependencies{}).Func()).Build()
			var r reconcile.Reconciler
			var enqueue func() []reconcile.Request
			if global {
				controller := &globalResourceController{client: c, reader: c, log: logr.Discard(), metrics: metrics.NewGlobalTenantResourceRecorder(), processor: processor.Processor{}}
				controller.clients = impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: c, resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.GlobalTenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
					return nil
				}}
				r = controller
				enqueue = func() []reconcile.Request {
					return controller.enqueueDependentGlobalTenantResources(t.Context(), dependency)
				}
			} else {
				controller := &namespacedResourceController{client: c, reader: c, log: logr.Discard(), metrics: metrics.NewTenantResourceRecorder()}
				controller.clients = impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: c, resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.TenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
					return nil
				}}
				r = controller
				enqueue = func() []reconcile.Request { return controller.enqueueDependentTenantResources(t.Context(), dependency) }
			}
			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent); err != nil {
				t.Fatal(err)
			}
			condition := status.Conditions.GetConditionByType(meta.ReadyCondition)
			if condition == nil || condition.Status != metav1.ConditionFalse || status.Trigger.ObservedRequest != "older" {
				t.Fatal("dependency failure was delayed or pending work acknowledged")
			}
			depStatus.Conditions.UpdateConditionByType(meta.NewReadyCondition(dependency))
			if err := c.Status().Update(t.Context(), dependency); err != nil {
				t.Fatal(err)
			}
			requests := enqueue()
			if len(requests) != 1 {
				t.Fatalf("dependency recovery selected %d parents", len(requests))
			}
			if _, err := r.Reconcile(t.Context(), requests[0]); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent); err != nil {
				t.Fatal(err)
			}
			condition = status.Conditions.GetConditionByType(meta.ReadyCondition)
			if condition == nil || condition.Status != metav1.ConditionTrue || status.Trigger.ObservedRequest != "pending" {
				t.Fatalf("dependency recovery blocked by trigger cooldown: %+v", status)
			}
		})
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
	if _, err := d.dispatch(t.Context(), target, time.Now()); err != nil {
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
	if _, err := d.dispatch(t.Context(), target, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations["projectcapsule.dev/replication-trigger"] == "" {
		t.Fatal("notification was not persisted under the current metadata prefix")
	}
	got.Spec.Cordoned = new(true)
	if err := c.Update(t.Context(), got); err != nil {
		t.Fatal(err)
	}
	previous := got.Annotations[replication.TriggerAnnotation]
	if _, err := d.dispatch(t.Context(), target, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[replication.TriggerAnnotation] != previous {
		t.Fatal("cordoned resource notified")
	}
}

func TestDispatcherEnforcesPersistedCooldownWithoutReplacingPendingWork(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(100, 0)
			common := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}, TriggerMinInterval: metav1.Duration{Duration: 10 * time.Second}}
			var obj client.Object
			var status *capsulev1beta2.TenantResourceCommonStatus
			if global {
				r := &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "one", Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
				obj, status = r, &r.Status.TenantResourceCommonStatus
			} else {
				r := &capsulev1beta2.TenantResource{Name: "source", Namespace: "ns", UID: "one", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
				obj, status = r, &r.Status.TenantResourceCommonStatus
			}
			obj.SetAnnotations(map[string]string{replication.TriggerAnnotation: "handled"})
			recordTrigger(status, "handled", now)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
			d := NewTriggerDispatcher(c, c, logr.Discard(), metrics.NewTriggerRecorder())
			target := replication.Target{Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: obj.GetUID()}
			if delay, err := d.dispatch(t.Context(), target, now.Add(time.Second)); err != nil || delay != 9*time.Second {
				t.Fatalf("cooldown=%s err=%v", delay, err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj); err != nil {
				t.Fatal(err)
			}
			if obj.GetAnnotations()[replication.TriggerAnnotation] != "handled" {
				t.Fatal("cooldown wrote a new token")
			}
			if delay, err := d.dispatch(t.Context(), target, now.Add(10*time.Second)); err != nil || delay != 0 {
				t.Fatalf("eligible dispatch=%s err=%v", delay, err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj); err != nil {
				t.Fatal(err)
			}
			pending := obj.GetAnnotations()[replication.TriggerAnnotation]
			if pending == "handled" {
				t.Fatal("eligible notification missing")
			}
			if delay, err := d.dispatch(t.Context(), target, now.Add(20*time.Second)); err != nil || delay <= 0 {
				t.Fatalf("unacknowledged work replaced: delay=%s err=%v", delay, err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj); err != nil {
				t.Fatal(err)
			}
			if obj.GetAnnotations()[replication.TriggerAnnotation] != pending {
				t.Fatal("later event replaced outstanding token")
			}
		})
	}
}

func BenchmarkTriggerDispatch(b *testing.B) {
	for _, unrelated := range []int{0, 1000} {
		for _, state := range []string{"eligible", "cooldown", "pending"} {
			b.Run(fmt.Sprintf("unrelated=%d/%s", unrelated, state), func(b *testing.B) {
				scheme := runtime.NewScheme()
				if err := capsulev1beta2.AddToScheme(scheme); err != nil {
					b.Fatal(err)
				}
				now := time.Unix(100, 0)
				parent := &capsulev1beta2.TenantResource{Name: "source", Namespace: "ns", UID: "one", Annotations: map[string]string{replication.TriggerAnnotation: "handled"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{TriggerMinInterval: metav1.Duration{Duration: time.Second}, Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}}}}
				stamp := now.Add(-time.Second)
				if state == "cooldown" {
					stamp = now
				}
				recordTrigger(&parent.Status.TenantResourceCommonStatus, "handled", stamp)
				if state == "pending" {
					parent.Annotations[replication.TriggerAnnotation] = "pending"
				}
				objects := []client.Object{parent}
				for i := range unrelated {
					objects = append(objects, &capsulev1beta2.TenantResource{Name: fmt.Sprint(i), Namespace: "other"})
				}
				var reads, patches atomic.Int64
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads.Add(1)
						return c.Get(ctx, key, obj, opts...)
					},
					Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
						patches.Add(1)
						data, err := patch.Data(obj)
						if err == nil && len(data) == 0 {
							b.Fatal("empty notification patch")
						}
						return err
					},
				}).Build()
				d := NewTriggerDispatcher(c, c, logr.Discard(), metrics.NewTriggerRecorder())
				target := replication.Target{Name: parent.Name, Namespace: parent.Namespace, UID: parent.UID}
				b.ReportAllocs()
				for b.Loop() {
					delay, err := d.dispatch(b.Context(), target, now)
					if err != nil || (state == "eligible") != (delay == 0) {
						b.Fatalf("state=%s delay=%s err=%v", state, delay, err)
					}
				}
				b.ReportMetric(float64(reads.Load())/float64(b.N), "GET/op")
				b.ReportMetric(float64(patches.Load())/float64(b.N), "PATCH/op")
			})
		}
	}
}

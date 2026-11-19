// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

func TestReplicationRetainsInventoryAcrossConcurrentSpecChanges(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, change := range []string{"spec", "disable-health", "enable-health", "deletion", "replacement", "conflict"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, change), func(t *testing.T) {
				_, c, instance, _ := healthFixture(t, global)
				spec, applied := healthSource(instance)
				spec.Cordoned = new(false)
				originalFinalizers := instance.GetFinalizers()
				instance.SetFinalizers([]string{meta.ControllerFinalizer})
				// The old generation created a new target and pruned its former target.
				applied.ProcessedItems[0].Name = "new-target"
				fresh := instance.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(instance), fresh))
				freshSpec, freshStatus := healthSource(fresh)
				fresh.SetGeneration(instance.GetGeneration() + 1)
				fresh.SetFinalizers([]string{"other.example/finalizer"})
				fresh.SetAnnotations(map[string]string{meta.ReconcileAnnotation: "new-request"})
				freshSpec.Resources = nil
				switch change {
				case "disable-health":
					freshSpec.HealthChecks = nil
				case "enable-health":
					spec.HealthChecks = nil
				case "replacement":
					fresh.SetUID("replacement")
				}
				require.NoError(t, c.Update(t.Context(), fresh))
				freshStatus.ObservedGeneration = fresh.GetGeneration()
				freshStatus.Conditions = meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionFalse, Reason: "NewerApplication"}}
				freshStatus.Health = &capsulev1beta2.TenantResourceHealthStatus{Revision: "newer", Applying: true}
				require.NoError(t, c.Status().Update(t.Context(), fresh))
				if change == "deletion" {
					require.NoError(t, c.Delete(t.Context(), fresh))
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(instance), fresh))
				}
				before := freshStatus.DeepCopy()
				writes := 0
				writer := interceptor.NewClient(c, interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					if change == "conflict" && writes == 1 {
						return apierrors.NewConflict(schema.GroupResource{Group: capsulev1beta2.GroupVersion.Group, Resource: "replications"}, obj.GetName(), fmt.Errorf("concurrent writer"))
					}
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				}})
				var err error
				switch obj := instance.(type) {
				case *capsulev1beta2.TenantResource:
					err = (&namespacedResourceController{client: writer, reader: c}).updateStatus(t.Context(), obj, nil)
				case *capsulev1beta2.GlobalTenantResource:
					err = (&globalResourceController{client: writer, reader: c}).updateStatus(t.Context(), obj, nil)
				}
				if change == "replacement" {
					require.True(t, apierrors.IsNotFound(err))
					require.Zero(t, writes)
				} else {
					require.NoError(t, err)
				}
				require.NoError(t, patchReplicationMetadata(t.Context(), c, c, instance, originalFinalizers, "old-request"))
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(instance), fresh))
				_, persisted := healthSource(fresh)
				if change == "replacement" {
					require.Equal(t, before, persisted)
					require.Equal(t, []string{"other.example/finalizer"}, fresh.GetFinalizers())
					return
				}
				require.Equal(t, "new-target", persisted.ProcessedItems[0].Name)
				require.EqualValues(t, 1, persisted.Size)
				require.Equal(t, before.ObservedGeneration, persisted.ObservedGeneration)
				require.Equal(t, before.Health, persisted.Health, "stale application must not complete health")
				require.Equal(t, before.Conditions, persisted.Conditions)
				require.ElementsMatch(t, []string{"other.example/finalizer", meta.ControllerFinalizer}, fresh.GetFinalizers())
				require.Equal(t, "new-request", fresh.GetAnnotations()[meta.ReconcileAnnotation])
				if change == "conflict" {
					require.Equal(t, 2, writes)
				}
			})
		}
	}
}

func TestReplicationConditionEventsRestoreAndCleanMetrics(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			_, _, parent, _ := healthFixture(t, global)
			var h handler.EventHandler
			registry := prometheus.NewRegistry()
			if global {
				r := metrics.NewGlobalTenantResourceRecorder()
				registry.MustRegister(r.Collectors()...)
				h = replicationConditionHandler(r.RecordConditions, func(obj *capsulev1beta2.GlobalTenantResource) { r.DeleteMetrics(obj.Name) })
			} else {
				r := metrics.NewTenantResourceRecorder()
				registry.MustRegister(r.Collectors()...)
				h = replicationConditionHandler(r.RecordConditions, func(obj *capsulev1beta2.TenantResource) { r.DeleteMetrics(obj.Name, obj.Namespace) })
			}
			assertHealthy := func(want float64, present bool) {
				t.Helper()
				families, err := registry.Gather()
				require.NoError(t, err)
				found := false
				for _, family := range families {
					for _, sample := range family.Metric {
						for _, label := range sample.Label {
							if label.GetName() == "condition" && label.GetValue() == meta.HealthyCondition {
								found = true
								require.Equal(t, want, sample.GetGauge().GetValue())
							}
						}
					}
				}
				require.Equal(t, present, found)
			}
			h.Create(t.Context(), event.CreateEvent{Object: parent}, nil)
			assertHealthy(0, true)
			older := parent.DeepCopyObject().(client.Object)
			_, status := healthSource(parent)
			setHealthCondition(parent, status, metav1.ConditionTrue, meta.SucceededReason, "healthy")
			// Status events follow API commit order even when the old apply queue
			// returns after the health queue. Neither writer records local snapshots.
			h.Update(t.Context(), event.UpdateEvent{ObjectOld: older, ObjectNew: parent}, nil)
			assertHealthy(1, true)
			h.Delete(t.Context(), event.DeleteEvent{Object: parent}, nil)
			assertHealthy(0, false)
			parent.SetUID("recreated")
			h.Create(t.Context(), event.CreateEvent{Object: parent}, nil)
			assertHealthy(1, true)
			older = parent.DeepCopyObject().(client.Object)
			status.Conditions.RemoveConditionByType(meta.HealthyCondition)
			h.Update(t.Context(), event.UpdateEvent{ObjectOld: older, ObjectNew: parent}, nil)
			assertHealthy(0, false)
		})
	}
}

func TestHealthMetricFollowsCommitOrderWhenApplyReturnsLast(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			health, c, parent, _ := healthFixture(t, global)
			spec, status := healthSource(parent)
			spec.Cordoned = new(false)
			spec.PruningOnDelete = new(true)
			spec.Settings.Adopt, spec.Settings.Force = new(false), new(false)
			require.NoError(t, c.Update(t.Context(), parent))
			status.ProcessedItems = nil
			require.NoError(t, c.Status().Update(t.Context(), parent))
			tenant := &capsulev1beta2.Tenant{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tenant))
			tenant.Status.Namespaces = []string{"tenant-a"}
			require.NoError(t, c.Update(t.Context(), tenant))
			require.NoError(t, c.Create(t.Context(), &corev1.Namespace{Name: "tenant-a", Labels: map[string]string{meta.TenantLabel: tenant.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenant.Name, UID: tenant.UID}}}))
			registry := prometheus.NewRegistry()
			localMetrics := metrics.NewTenantResourceRecorder()
			globalMetrics := metrics.NewGlobalTenantResourceRecorder()
			var h handler.EventHandler
			if global {
				registry.MustRegister(globalMetrics.Collectors()...)
				h = replicationConditionHandler(globalMetrics.RecordConditions, func(obj *capsulev1beta2.GlobalTenantResource) { globalMetrics.DeleteMetrics(obj.Name) })
			} else {
				registry.MustRegister(localMetrics.Collectors()...)
				h = replicationConditionHandler(localMetrics.RecordConditions, func(obj *capsulev1beta2.TenantResource) { localMetrics.DeleteMetrics(obj.Name, obj.Namespace) })
			}
			health.client = interceptor.NewClient(c, interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, subresource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				if err := c.SubResource(subresource).Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				h.Update(ctx, event.UpdateEvent{ObjectNew: obj}, nil)
				return nil
			}})
			writer := interceptor.NewClient(c, interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if err := c.SubResource(subresource).Update(ctx, obj, opts...); err != nil {
					return err
				}
				h.Update(ctx, event.UpdateEvent{ObjectNew: obj}, nil)
				_, applied := healthSource(obj)
				if applied.Health.Applying || applied.Health.RequestedAt.IsZero() {
					return nil
				}

				// Publish the next health observation before the apply writer returns.
				// Its local snapshot is deliberately still Unknown/Ready=False.
				fresh := obj.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), fresh))
				_, status := healthSource(fresh)
				before, conditions := status.Health.DeepCopy(), slices.Clone(status.Conditions)
				setHealthCondition(fresh, status, metav1.ConditionTrue, meta.SucceededReason, "healthy")
				status.Conditions.UpdateConditionByType(meta.NewReadyCondition(fresh))
				published, err := health.publish(ctx, fresh, status, before, conditions)
				require.NoError(t, err)
				require.True(t, published)
				return nil
			}})
			var controller reconcile.Reconciler
			if global {
				controller = &globalResourceController{client: writer, reader: c, metrics: globalMetrics, conditions: health.conditions, clients: health.globals, processor: processor.Processor{}, collector: NewCollector(c, c.RESTMapper())}
			} else {
				controller = &namespacedResourceController{client: writer, reader: c, metrics: localMetrics, conditions: health.conditions, clients: health.locals, processor: processor.Processor{}, collector: NewCollector(c, c.RESTMapper())}
			}
			_, err := controller.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			_, persisted := healthSource(parent)
			require.True(t, meta.IsStatusConditionTrue(persisted.Conditions, meta.HealthyCondition))
			families, err := registry.Gather()
			require.NoError(t, err)
			require.Len(t, families, 1)
			for _, sample := range families[0].Metric {
				for _, label := range sample.Label {
					if label.GetName() == "condition" && label.GetValue() == meta.HealthyCondition {
						require.Equal(t, float64(1), sample.GetGauge().GetValue(), "the later-returning apply must not overwrite the committed healthy observation")
					}
				}
			}
		})
	}
}

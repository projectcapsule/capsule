// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

func TestLateScopedInventoryInvalidatesHealth(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, scenario := range []string{"late-disabled", "no-checkpoint", "conflict", "unchanged", "disabled", "replacement"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, scenario), func(t *testing.T) {
				_, c, parent, _ := healthFixture(t, global)
				old := parent.DeepCopyObject().(client.Object)
				oldSpec, _ := healthSource(old)
				oldSpec.HealthChecks = nil
				spec, status := healthSource(parent)
				parent.SetGeneration(2)
				if scenario == "disabled" {
					spec.HealthChecks = nil
				}
				if scenario == "replacement" {
					parent.SetUID("replacement")
				}
				require.NoError(t, c.Update(t.Context(), parent))
				other := status.ProcessedItems[0]
				other.Tenant, other.Namespace, other.Name = "tenant-b", "tenant-b", "private"
				status.ProcessedItems = append(status.ProcessedItems, other)
				completeHealthApplication(parent, spec, status, status.DeepCopy(), nil, time.Now())
				if scenario == "no-checkpoint" {
					status.Health = nil
				}
				if status.Health != nil {
					status.Health.Cursor = 1
					setHealthCondition(parent, status, metav1.ConditionTrue, meta.SucceededReason, "checked")
				}
				require.NoError(t, c.Status().Update(t.Context(), parent))
				before := parent.DeepCopyObject().(client.Object)
				beforeStatus := status.DeepCopy()
				items := meta.ProcessedItems{status.ProcessedItems[0]}
				if scenario != "unchanged" {
					items[0].Name, items[0].Health = "late-target", nil
				}
				writes := 0
				writer := interceptor.NewClient(c, interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					if scenario == "conflict" && writes == 1 {
						return apierrors.NewConflict(corev1.Resource("replications"), obj.GetName(), errors.New("concurrent observer"))
					}
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				}})
				scope := processor.Scope{Tenant: "tenant-a", Namespace: "tenant-a"}
				var err error
				switch old := old.(type) {
				case *capsulev1beta2.TenantResource:
					err = (scopedStatusPatcher[*capsulev1beta2.TenantResource]{client: writer, reader: c, factory: func() *capsulev1beta2.TenantResource { return &capsulev1beta2.TenantResource{} }, status: func(o *capsulev1beta2.TenantResource) *capsulev1beta2.TenantResourceCommonStatus {
						return &o.Status.TenantResourceCommonStatus
					}}).Patch(t.Context(), old, scope, items)
				case *capsulev1beta2.GlobalTenantResource:
					err = (scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]{client: writer, reader: c, factory: func() *capsulev1beta2.GlobalTenantResource { return &capsulev1beta2.GlobalTenantResource{} }, status: func(o *capsulev1beta2.GlobalTenantResource) *capsulev1beta2.TenantResourceCommonStatus {
						return &o.Status.TenantResourceCommonStatus
					}}).Patch(t.Context(), old, scope, items)
				}
				if scenario == "replacement" {
					require.True(t, apierrors.IsNotFound(err))
					require.Zero(t, writes)
					return
				}
				require.NoError(t, err)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				_, status = healthSource(parent)
				require.Len(t, status.ProcessedItems, 2)
				require.Contains(t, status.ProcessedItems, other, "another tenant's inventory must be preserved")
				if scenario == "unchanged" || scenario == "disabled" {
					require.Equal(t, beforeStatus.Health, status.Health)
					require.False(t, healthApplicationInvalidated().Update(event.UpdateEvent{ObjectOld: before, ObjectNew: parent}))
					return
				}
				require.Contains(t, status.ProcessedItems, items[0], "late targets must remain available for cleanup")
				require.Empty(t, status.Health.Revision)
				require.True(t, status.Health.Applying)
				require.False(t, status.Health.StartedAt.IsZero())
				require.False(t, status.Health.Deadline.IsZero())
				require.False(t, status.Health.RequestedAt.IsZero())
				require.Zero(t, status.Health.Cursor)
				require.False(t, healthRequested(parent))
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition))
				require.True(t, healthApplicationInvalidated().Update(event.UpdateEvent{ObjectOld: before, ObjectNew: parent}), "status publication must enqueue a full apply")
				require.False(t, healthApplicationInvalidated().Update(event.UpdateEvent{ObjectOld: parent, ObjectNew: parent}), "an unchanged checkpoint must not loop")
				if scenario == "conflict" {
					require.Equal(t, 2, writes)
				}
			})
		}
	}
}

func TestHealthObserverRejectsMismatchedInventory(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, change := range []string{"missing-reference", "different-revision", "different-target", "different-identity"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, change), func(t *testing.T) {
				r, c, parent, _ := healthFixture(t, global)
				_, status := healthSource(parent)
				switch change {
				case "missing-reference":
					status.ProcessedItems[0].Health = nil
				case "different-revision":
					status.ProcessedItems[0].Health.Digest = "newer"
				case "different-target":
					status.ProcessedItems[0].Name = "other"
				case "different-identity":
					status.ServiceAccount.Name = "other"
				}
				status.Health.NextCheckAt = metav1.NewTime(time.Now().Add(time.Hour))
				setHealthCondition(parent, status, metav1.ConditionTrue, meta.SucceededReason, "old inventory")
				require.NoError(t, c.Status().Update(t.Context(), parent))
				before := parent.DeepCopyObject().(client.Object)
				r.client = interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					_, target := obj.(*unstructured.Unstructured)
					require.False(t, target, "a mismatched checkpoint must not read targets")
					return c.Get(ctx, key, obj, opts...)
				}})
				_, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
				require.NoError(t, err)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				_, status = healthSource(parent)
				require.True(t, status.Health.Applying)
				require.Empty(t, status.Health.Revision)
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition))
				require.True(t, healthApplicationInvalidated().Update(event.UpdateEvent{ObjectOld: before, ObjectNew: parent}))
			})
		}
	}
}

func TestHealthObservationDeduplicatesTargetsAcrossBatches(t *testing.T) {
	for _, distinct := range []int{1, healthBatchSize + 1} {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("targets=%d/pending=%t", distinct, pending), func(t *testing.T) {
				r, c, parent, target := healthFixture(t, false)
				spec, status := healthSource(parent)
				prototype := status.ProcessedItems[0]
				status.ProcessedItems = nil
				for i := range distinct {
					copy := target.DeepCopy()
					copy.Name, copy.ResourceVersion = fmt.Sprintf("target-%d", i), ""
					copy.UID = target.UID + "-" + types.UID(fmt.Sprint(i))
					copy.Data["state"] = "healthy"
					if pending && i == distinct-1 {
						copy.Data["state"] = "pending"
					}
					require.NoError(t, c.Create(t.Context(), copy))
					item := prototype
					item.Name, item.Health = copy.Name, &meta.ResourceHealthReference{UID: copy.UID}
					for contribution := range 10 {
						item.Origin = fmt.Sprint(contribution)
						status.ProcessedItems = append(status.ProcessedItems, item)
					}
				}
				// Repeat the whole inventory after the batch boundary as well.
				status.ProcessedItems = append(status.ProcessedItems, status.ProcessedItems...)
				checks, err := compileHealthChecks(spec, r.conditions)
				require.NoError(t, err)
				reads := 0
				reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					return c.Get(ctx, key, obj, opts...)
				}})
				observeHealthBatch(t.Context(), reader, parent, status, checks, time.Now())
				require.Equal(t, min(distinct, healthBatchSize), reads)
				if distinct > healthBatchSize {
					require.NotZero(t, status.Health.Cursor)
					require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
					observeHealthBatch(t.Context(), reader, parent, status, checks, time.Now())
				}
				require.Equal(t, distinct, reads, "the budget and GET count must apply to distinct pinned targets")
				require.Zero(t, status.Health.Cursor)
				require.Equal(t, !pending, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
			})
		}
	}
}

func TestHealthObservationRetainsDistinctRevisions(t *testing.T) {
	for _, change := range []string{"origin", "uid", "generation", "digest", "empty-uid"} {
		t.Run(change, func(t *testing.T) {
			_, c, parent, target := healthFixture(t, false)
			_, status := healthSource(parent)
			target.Data["state"] = "healthy"
			require.NoError(t, c.Update(t.Context(), target))
			item := status.ProcessedItems[0]
			item.Health = new(*item.Health)
			item.Origin = "another-contribution"
			expectedReads := 2
			switch change {
			case "origin":
				expectedReads = 1
			case "uid":
				item.Health.UID = "replacement"
			case "generation":
				item.Health.Generation++
			case "digest":
				item.Health.Digest = "different"
			case "empty-uid":
				item.Health.UID = ""
				status.ProcessedItems[0].Health = new(*item.Health)
			}
			status.ProcessedItems = append(status.ProcessedItems, item)
			reads := 0
			reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				reads++
				return c.Get(ctx, key, obj, opts...)
			}})
			observeHealthBatch(t.Context(), reader, parent, status, nil, time.Now())
			require.Equal(t, expectedReads, reads)
			if change == "uid" || change == "generation" || change == "empty-uid" {
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
			} else {
				require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
			}
		})
	}
}

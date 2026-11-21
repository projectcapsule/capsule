// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

func TestFinalizerRemovalRetainsScopedCleanupInventory(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, publication := range []string{"before-status-noop", "after-status", "metadata-conflict", "empty-inventory"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, publication), func(t *testing.T) {
				_, c, parent, _ := healthFixture(t, global)
				spec, status := healthSource(parent)
				spec.HealthChecks, spec.Cordoned = nil, new(false)
				parent.SetFinalizers([]string{meta.ControllerFinalizer, "other.projectcapsule.dev/finalizer"})
				require.NoError(t, c.Update(t.Context(), parent))
				status.Health = nil
				status.Conditions = meta.ConditionList{meta.NewReadyCondition(parent), meta.NewCordonedCondition(parent)}
				require.NoError(t, c.Status().Update(t.Context(), parent))
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				originalFinalizers := slices.Clone(parent.GetFinalizers())
				checkpoint := &inventoryCheckpoint{}
				checkpoint.start(status.ProcessedItems)
				concurrent := *status.ProcessedItems[0].DeepCopy()
				concurrent.Name, concurrent.Health = "scoped-target", nil
				scopedParent := parent.DeepCopyObject().(client.Object)
				patcher := scopedStatusPatcher[client.Object]{
					client: c, reader: c,
					factory: func() client.Object { return scopedParent.DeepCopyObject().(client.Object) },
					status: func(obj client.Object) *capsulev1beta2.TenantResourceCommonStatus {
						_, status := healthSource(obj)
						return status
					},
				}
				publish := func() {
					require.NoError(t, patcher.Patch(t.Context(), scopedParent, processor.Scope{Tenant: concurrent.Tenant, Namespace: concurrent.Namespace}, meta.ProcessedItems{concurrent}))
				}
				// The full pass has pruned its snapshot and decided to retire cleanup.
				status.ProcessedItems = nil
				parent.SetFinalizers([]string{"other.projectcapsule.dev/finalizer"})
				if publication == "before-status-noop" {
					publish()
				}
				statusWrites := 0
				writer := interceptor.NewClient(c, interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					statusWrites++
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				}})
				switch parent := parent.(type) {
				case *capsulev1beta2.GlobalTenantResource:
					_, err := (&globalResourceController{client: writer, reader: c}).updateStatus(t.Context(), parent, nil, checkpoint)
					require.NoError(t, err)
				case *capsulev1beta2.TenantResource:
					_, err := (&namespacedResourceController{client: writer, reader: c}).updateStatus(t.Context(), parent, nil, checkpoint)
					require.NoError(t, err)
				}
				if publication == "before-status-noop" {
					require.Zero(t, statusWrites, "already-published merged status must not be rewritten")
					require.Equal(t, meta.ProcessedItems{concurrent}, status.ProcessedItems, "no-write publication must still synchronize the full writer")
					require.Contains(t, parent.GetFinalizers(), meta.ControllerFinalizer)
				} else if publication == "after-status" {
					publish()
				}
				metadataWrites := 0
				metadataClient := interceptor.NewClient(c, interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					metadataWrites++
					if publication == "metadata-conflict" && metadataWrites == 1 {
						publish()
					}
					return c.Patch(ctx, obj, patch, opts...)
				}})
				require.NoError(t, patchReplicationMetadata(t.Context(), metadataClient, c, parent, originalFinalizers, ""))
				fresh := parent.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
				_, persisted := healthSource(fresh)
				if publication == "empty-inventory" {
					require.Empty(t, persisted.ProcessedItems)
					require.Equal(t, []string{"other.projectcapsule.dev/finalizer"}, fresh.GetFinalizers(), "completed cleanup must still retire its own finalizer")
				} else {
					require.Equal(t, meta.ProcessedItems{concurrent}, persisted.ProcessedItems)
					require.ElementsMatch(t, originalFinalizers, fresh.GetFinalizers(), "persisted cleanup inventory must retain its finalizer")
				}
				if publication == "metadata-conflict" {
					require.Equal(t, 2, metadataWrites, "a conflict must retry against the new inventory")
				}
			})
		}
	}
}

func BenchmarkReplicationMetadataFinalizerGuard(b *testing.B) {
	for _, count := range []int{0, 128} {
		b.Run(fmt.Sprintf("inventory=%d", count), func(b *testing.B) {
			_, c, parent, _ := healthFixture(b, true)
			parent.SetFinalizers([]string{meta.ControllerFinalizer})
			require.NoError(b, c.Update(b.Context(), parent))
			_, status := healthSource(parent)
			row := status.ProcessedItems[0]
			status.ProcessedItems = make(meta.ProcessedItems, count)
			for i := range status.ProcessedItems {
				status.ProcessedItems[i] = row
				status.ProcessedItems[i].Name = fmt.Sprint(i)
			}
			require.NoError(b, c.Status().Update(b.Context(), parent))
			desired := parent.DeepCopyObject().(client.Object)
			desired.SetFinalizers(nil)
			gets, patches := 0, 0
			counted := interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					gets++
					return c.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patches++
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			b.ReportAllocs()
			for b.Loop() {
				if err := patchReplicationMetadata(b.Context(), counted, counted, desired, []string{meta.ControllerFinalizer}, ""); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(gets)/float64(b.N), "GET/op")
			b.ReportMetric(float64(patches)/float64(b.N), "PATCH/op")
		})
	}
}

func TestFullApplyRetainsConcurrentNamespaceInventory(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("global=%t/health=%t", global, enabled), func(t *testing.T) {
				_, c, parent, _ := healthFixture(t, global)
				spec, status := healthSource(parent)
				if !enabled {
					spec.HealthChecks = nil
				}
				spec.Cordoned = new(false)
				require.NoError(t, c.Update(t.Context(), parent))
				checkpoint := &inventoryCheckpoint{}
				checkpoint.start(status.ProcessedItems)
				fresh := parent.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
				_, persisted := healthSource(fresh)
				concurrent := *persisted.ProcessedItems[0].DeepCopy()
				concurrent.Namespace = "new-namespace"
				concurrent.Name = "new-target"
				concurrent.Health = nil
				persisted.ProcessedItems = append(persisted.ProcessedItems, concurrent)
				persisted.UpdateStats()
				require.NoError(t, c.Status().Update(t.Context(), fresh))
				var again bool
				var err error
				switch parent := parent.(type) {
				case *capsulev1beta2.GlobalTenantResource:
					again, err = (&globalResourceController{client: c, reader: c}).updateStatus(t.Context(), parent, nil, checkpoint)
				case *capsulev1beta2.TenantResource:
					again, err = (&namespacedResourceController{client: c, reader: c}).updateStatus(t.Context(), parent, nil, checkpoint)
				}
				require.NoError(t, err)
				require.True(t, again)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
				_, persisted = healthSource(fresh)
				require.Len(t, persisted.ProcessedItems, 2)
				require.Equal(t, uint(2), persisted.Size)
				require.Contains(t, parent.GetFinalizers(), meta.ControllerFinalizer)
				if enabled {
					require.True(t, persisted.Health.Applying)
					require.False(t, meta.IsStatusConditionTrue(persisted.Conditions, meta.ReadyCondition))
				}
			})
		}
	}
}

func TestInventoryCheckpointPreservesAuthoritativePruning(t *testing.T) {
	_, _, parent, _ := healthFixture(t, true)
	_, status := healthSource(parent)
	before := status.ProcessedItems
	for _, scenario := range []string{"unchanged", "new-scope", "changed-pruned-item", "empty", "not-started"} {
		t.Run(scenario, func(t *testing.T) {
			checkpoint := &inventoryCheckpoint{}
			checkpoint.start(before)
			latest := make(meta.ProcessedItems, len(before))
			for i := range before {
				before[i].DeepCopyInto(&latest[i])
			}
			want := 0
			again := false
			switch scenario {
			case "new-scope":
				row := *latest[0].DeepCopy()
				row.Namespace = "new"
				latest = append(latest, row)
				want = 1
				again = true
			case "changed-pruned-item":
				latest[0].Message = "reapplied after snapshot"
				want = 1
				again = true
			case "empty":
				checkpoint.start(nil)
				latest = meta.ProcessedItems{}
			case "not-started":
				checkpoint = &inventoryCheckpoint{}
				want = len(latest)
			}
			got, followup := checkpoint.merge(latest, nil)
			require.Len(t, got, want)
			require.Equal(t, again, followup)
		})
	}
}

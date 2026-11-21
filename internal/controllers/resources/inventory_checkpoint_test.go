// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

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

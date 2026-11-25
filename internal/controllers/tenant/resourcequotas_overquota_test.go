// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

// TestSyncResourceQuotasOverQuotaPreservesOtherResourceLimits reproduces the
// order-dependent Hard map replacement in the OverQuota branch of
// syncResourceQuotasLocked (resourcequotas.go, the `else` arm that assigns a
// fresh single-entry map to Spec.Hard).
//
// Setup: a Tenant-scoped quota declares CPU and memory. The tenant is exactly
// at its CPU budget (OverQuota branch), while memory is under quota. One
// namespace has not reported CPU usage yet (freshly joined, Status.Used only
// contains memory), which is the condition that triggers the map replacement.
//
// Expected behavior: the CPU hard limit of the usage-less namespace is pinned
// to zero, but its memory hard limit must survive the sync.
//
// Actual behavior: when the CPU iteration runs after the memory iteration,
// the whole Hard map is replaced with {cpu: 0} and the previously computed
// memory limit is lost. The per-resource persistence inside the loop writes
// that truncated map to the API.
//
// The defect is deterministic with two resources: whichever resource is
// processed before the over-quota one loses its limit, so at least one
// declared limit is always missing after a single pass. Which specific
// resource is lost depends on Go's randomized map iteration order; the
// 20 repetitions below make the CPU-last ordering (memory limit lost)
// virtually certain to occur at least once.
func TestSyncResourceQuotasOverQuotaPreservesOtherResourceLimits(t *testing.T) {
	t.Parallel()

	for i := 0; i < 20; i++ {
		tenant := quotaCleanupTenant([]corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{
			corev1.ResourceLimitsCPU:    resource.MustParse("1"),
			corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
		}}})
		tenant.Spec.ResourceQuota.Scope = api.ResourceQuotaScopeTenant
		tenant.Status.Size = 2
		tenant.Status.Spaces = append(tenant.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{
			Name: "team-b",
		})

		// team-a consumes the whole CPU budget and some memory.
		quotaA := managedResourceQuota(tenant.Name, "team-a", "0", corev1.ResourceList{
			corev1.ResourceLimitsCPU:    resource.MustParse("1"),
			corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
		})
		quotaA.Status.Used = corev1.ResourceList{
			corev1.ResourceLimitsCPU:    resource.MustParse("1"),
			corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
		}

		// team-b just joined: the quota controller created its ResourceQuota,
		// but usage reporting has only observed memory so far (no CPU entry).
		quotaB := managedResourceQuota(tenant.Name, "team-b", "0", corev1.ResourceList{
			corev1.ResourceLimitsCPU:    resource.MustParse("1"),
			corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
		})
		quotaB.Status.Used = corev1.ResourceList{
			corev1.ResourceLimitsMemory: resource.MustParse("256Mi"),
		}

		manager := quotaCleanupManager(t, tenant, quotaA, quotaB)

		if err := manager.syncResourceQuotas(context.Background(), logr.Discard(), tenant); err != nil {
			t.Fatalf("syncResourceQuotas() unexpected error: %v", err)
		}

		updated := &corev1.ResourceQuota{}
		if err := manager.Get(context.Background(), client.ObjectKeyFromObject(quotaB), updated); err != nil {
			t.Fatalf("get updated ResourceQuota: %v", err)
		}

		// The over-quota CPU pin itself is intended behavior.
		if got, ok := updated.Spec.Hard[corev1.ResourceLimitsCPU]; !ok || got.Cmp(resource.MustParse("0")) != 0 {
			t.Fatalf("iteration %d: team-b CPU hard = %v (present=%t), want 0", i, got.String(), ok)
		}

		// The memory limit must not be collateral damage of the CPU pin.
		if _, ok := updated.Spec.Hard[corev1.ResourceLimitsMemory]; !ok {
			t.Fatalf("iteration %d: team-b memory hard limit lost after OverQuota sync, Hard=%v", i, updated.Spec.Hard)
		}
	}
}

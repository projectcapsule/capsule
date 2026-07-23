// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestGetClaimFromStatus(t *testing.T) {
	ns := "test-namespace"
	testUID := types.UID("test-uid")
	otherUID := types.UID("wrong-uid")

	claim := &capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claim-a",
			Namespace: ns,
			UID:       testUID,
		},
	}

	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				ns: {
					&capsulev1beta2.ResourcePoolClaimsItem{
						NamespacedRFC1123ObjectReferenceWithNamespaceWithUID: meta.NamespacedRFC1123ObjectReferenceWithNamespaceWithUID{
							UID: testUID,
						},
						Claims: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
	}

	t.Run("returns matching claim", func(t *testing.T) {
		found := pool.GetClaimFromStatus(claim)
		assert.NotNil(t, found)
		assert.Equal(t, testUID, found.UID)
	})

	t.Run("returns nil if UID doesn't match", func(t *testing.T) {
		claimWrongUID := *claim
		claimWrongUID.UID = otherUID

		found := pool.GetClaimFromStatus(&claimWrongUID)
		assert.Nil(t, found)
	})

	t.Run("returns nil if namespace has no claims", func(t *testing.T) {
		claimWrongNS := *claim
		claimWrongNS.Namespace = "other-ns"

		found := pool.GetClaimFromStatus(&claimWrongNS)
		assert.Nil(t, found)
	})
}

func makeResourceList(cpu, memory string) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceLimitsCPU:    resource.MustParse(cpu),
		corev1.ResourceLimitsMemory: resource.MustParse(memory),
	}
}

func makeClaim(name, ns string, uid types.UID, res corev1.ResourceList) *capsulev1beta2.ResourcePoolClaim {
	return &capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
		Spec: capsulev1beta2.ResourcePoolClaimSpec{
			ResourceClaims: res,
		},
	}
}

func TestAssignNamespaces(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{}

	namespaces := []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "active-ns"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		{ObjectMeta: metav1.ObjectMeta{Name: "terminating-ns", DeletionTimestamp: &metav1.Time{}}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
	}

	pool.AssignNamespaces(namespaces)

	assert.Equal(t, uint(1), pool.Status.NamespaceSize)
	assert.Equal(t, []string{"active-ns"}, pool.Status.Namespaces)
}

func TestAssignClaims(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns": {
					&capsulev1beta2.ResourcePoolClaimsItem{},
					&capsulev1beta2.ResourcePoolClaimsItem{},
				},
			},
		},
	}
	pool.AssignClaims()

	assert.Equal(t, uint(2), pool.Status.ClaimSize)
}

func TestAddRemoveClaimToStatus(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{}

	claim := makeClaim("claim-1", "ns", "uid-1", makeResourceList("1", "1Gi"))
	pool.AddClaimToStatus(claim)

	stored := pool.GetClaimFromStatus(claim)

	assert.NotNil(t, stored)
	assert.Equal(t, meta.RFC1123Name("claim-1"), stored.Name)

	pool.RemoveClaimFromStatus(claim)
	assert.Nil(t, pool.GetClaimFromStatus(claim))
}

func TestCalculateResources(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Allocation: capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU: resource.MustParse("2"),
				},
			},
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns": {
					&capsulev1beta2.ResourcePoolClaimsItem{
						Claims: corev1.ResourceList{
							corev1.ResourceLimitsCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	pool.CalculateClaimedResources()

	actualClaimed := pool.Status.Allocation.Claimed[corev1.ResourceLimitsCPU]
	actualAvailable := pool.Status.Allocation.Available[corev1.ResourceLimitsCPU]

	assert.Equal(t, 0, (&actualClaimed).Cmp(resource.MustParse("1")))
	assert.Equal(t, 0, (&actualAvailable).Cmp(resource.MustParse("1")))
}

func TestCalculateResources_OverSubscriptionDoesNotGoNegative(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Allocation: capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU: resource.MustParse("10"),
				},
			},
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns": {
					&capsulev1beta2.ResourcePoolClaimsItem{
						Claims: corev1.ResourceList{
							corev1.ResourceLimitsCPU: resource.MustParse("55"),
						},
					},
				},
			},
		},
	}

	pool.CalculateClaimedResources()

	// Even when claims exceed the hard limit, available must clamp at zero
	// and Hard must stay intact (regression: subtracting in place corrupted
	// both Hard and Available, producing negative values — see issue #1977).
	actualAvailable := pool.Status.Allocation.Available[corev1.ResourceLimitsCPU]
	assert.Equal(t, 0, (&actualAvailable).Cmp(resource.MustParse("0")))

	actualHard := pool.Status.Allocation.Hard[corev1.ResourceLimitsCPU]
	assert.Equal(t, 0, (&actualHard).Cmp(resource.MustParse("10")))

	// With zero available, a new oversized claim must be rejected.
	errs := pool.CanClaimFromPool(corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("55"),
	})
	assert.Len(t, errs, 1)
}

func TestCanClaimFromPool(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Allocation: capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
				},
			},
		},
	}

	errs := pool.CanClaimFromPool(corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
	})
	assert.Len(t, errs, 1)

	errs = pool.CanClaimFromPool(corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("500Mi"),
	})
	assert.Len(t, errs, 0)
}

func TestGetResourceQuotaHardResources(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Spec: capsulev1beta2.ResourcePoolSpec{
			Defaults: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			},
		},
		Status: capsulev1beta2.ResourcePoolStatus{
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns": {
					&capsulev1beta2.ResourcePoolClaimsItem{
						Claims: corev1.ResourceList{
							corev1.ResourceLimitsCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	res := pool.GetResourceQuotaHardResources("ns")
	actual := res[corev1.ResourceLimitsCPU]
	assert.Equal(t, 0, (&actual).Cmp(resource.MustParse("2")))
}

func TestGetNamespaceClaims(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns": {
					&capsulev1beta2.ResourcePoolClaimsItem{
						NamespacedRFC1123ObjectReferenceWithNamespaceWithUID: meta.NamespacedRFC1123ObjectReferenceWithNamespaceWithUID{UID: "uid1"},
						Claims: corev1.ResourceList{
							corev1.ResourceLimitsCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	claims, res := pool.GetNamespaceClaims("ns")
	assert.Contains(t, claims, "uid1")
	actual := res[corev1.ResourceLimitsCPU]
	assert.Equal(t, 0, (&actual).Cmp(resource.MustParse("1")))
}

func TestGetClaimedByNamespaceClaims(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Claims: capsulev1beta2.ResourcePoolNamespaceClaimsStatus{
				"ns1": {
					&capsulev1beta2.ResourcePoolClaimsItem{
						Claims: makeResourceList("1", "1Gi"),
					},
				},
			},
		},
	}

	result := pool.GetClaimedByNamespaceClaims()
	actualCPU := result["ns1"][corev1.ResourceLimitsCPU]
	actualMem := result["ns1"][corev1.ResourceLimitsMemory]

	assert.Equal(t, 0, (&actualCPU).Cmp(resource.MustParse("1")))
	assert.Equal(t, 0, (&actualMem).Cmp(resource.MustParse("1Gi")))
}

func TestIsBoundToResourcePool_2(t *testing.T) {
	t.Run("bound to resource pool (Assigned=True)", func(t *testing.T) {
		claim := &capsulev1beta2.ResourcePoolClaim{
			Status: capsulev1beta2.ResourcePoolClaimStatus{
				Conditions: meta.ConditionList{},
			},
		}

		assert.Equal(t, false, claim.IsBoundInResourcePool())
	})

	t.Run("not bound - wrong condition type", func(t *testing.T) {
		claim := &capsulev1beta2.ResourcePoolClaim{
			Status: capsulev1beta2.ResourcePoolClaimStatus{
				Conditions: meta.ConditionList{
					meta.Condition{},
				},
			},
		}

		cond := meta.NewAssignedCondition(claim)
		cond.Status = metav1.ConditionFalse
		claim.Status.Conditions.UpdateConditionByType(cond)

		assert.Equal(t, false, claim.IsBoundInResourcePool())
	})

	t.Run("not bound - condition not true", func(t *testing.T) {
		claim := &capsulev1beta2.ResourcePoolClaim{
			Status: capsulev1beta2.ResourcePoolClaimStatus{
				Conditions: meta.ConditionList{
					meta.Condition{},
				},
			},
		}

		cond := meta.NewBoundCondition(claim)
		cond.Status = metav1.ConditionFalse
		claim.Status.Conditions.UpdateConditionByType(cond)

		assert.Equal(t, false, claim.IsBoundInResourcePool())
	})

	t.Run("not bound - condition not true", func(t *testing.T) {
		claim := &capsulev1beta2.ResourcePoolClaim{
			Status: capsulev1beta2.ResourcePoolClaimStatus{
				Conditions: meta.ConditionList{
					meta.Condition{},
				},
			},
		}

		cond := meta.NewBoundCondition(claim)
		cond.Status = metav1.ConditionTrue
		claim.Status.Conditions.UpdateConditionByType(cond)

		assert.Equal(t, true, claim.IsBoundInResourcePool())
	})

}

func TestGetAvailableClaimableResources_OverSubscriptionDoesNotGoNegative(t *testing.T) {
	pool := &capsulev1beta2.ResourcePool{
		Status: capsulev1beta2.ResourcePoolStatus{
			Allocation: capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU: resource.MustParse("10"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU: resource.MustParse("55"),
				},
			},
		},
	}

	// Claimed exceeds Hard, so Hard-Claimed is negative. The helper must clamp
	// at zero so negative available values never leak into PoolExhausted
	// condition messages.
	claimable := pool.GetAvailableClaimableResources()
	got := claimable[corev1.ResourceLimitsCPU]
	assert.Equal(t, 0, (&got).Cmp(resource.MustParse("0")))
	assert.False(t, (&got).Sign() < 0)
}

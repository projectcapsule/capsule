package v1beta2

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestGetClaimFromStatus(t *testing.T) {
	ns := "test-namespace"
	testUID := types.UID("test-uid")
	otherUID := types.UID("wrong-uid")

	claim := &ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claim-a",
			Namespace: ns,
			UID:       testUID,
		},
	}

	pool := &ResourcePool{
		Status: ResourcePoolStatus{
			Claims: ResourcePoolNamespaceClaimsStatus{
				ns: {
					&ResourcePoolClaimsItem{
						StatusNameUID: api.StatusNameUID{
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
		claimWrongUID := claim
		claimWrongUID.UID = otherUID

		found := pool.GetClaimFromStatus(claimWrongUID)
		assert.Nil(t, found)
	})

	t.Run("returns nil if namespace has no claims", func(t *testing.T) {
		claimWrongNS := claim
		claimWrongNS.Namespace = "other-ns"

		found := pool.GetClaimFromStatus(claimWrongNS)
		assert.Nil(t, found)
	})
}

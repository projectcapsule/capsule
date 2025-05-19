package meta

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stretchr/testify/assert"
)

func TestLooseOwnerReferenceHelpers(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "default",
			UID:       types.UID("owner-uid"),
		},
	}

	target := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "target",
			Namespace: "default",
		},
	}

	t.Run("SetLooseOwnerReference adds and clears controller fields", func(t *testing.T) {
		err := SetLooseOwnerReference(target, owner, scheme)
		assert.NoError(t, err)

		refs := target.GetOwnerReferences()
		assert.Len(t, refs, 1)
		ref := refs[0]
		assert.Equal(t, owner.UID, ref.UID)
		assert.Nil(t, ref.BlockOwnerDeletion)
		assert.Nil(t, ref.Controller)
	})

	t.Run("HasLooseOwnerReference returns true if present", func(t *testing.T) {
		result := HasLooseOwnerReference(target, owner)
		assert.True(t, result)
	})

	t.Run("RemoveLooseOwnerReference removes the reference", func(t *testing.T) {
		RemoveLooseOwnerReference(target, owner)
		refs := target.GetOwnerReferences()
		assert.Len(t, refs, 0)
	})

	t.Run("HasLooseOwnerReference returns false if not present", func(t *testing.T) {
		result := HasLooseOwnerReference(target, owner)
		assert.False(t, result)
	})
}

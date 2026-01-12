// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Adds an ownerreferences, which does not delete the object when the owner is deleted.
func SetLooseOwnerReference(obj client.Object, owner metav1.OwnerReference) error {
	if obj == nil {
		return nil
	}

	ownerRefs := obj.GetOwnerReferences()

	// Overwrite existing entry with same UID
	for i := range ownerRefs {
		if ownerRefs[i].UID == owner.UID {
			ownerRefs[i] = owner
			obj.SetOwnerReferences(ownerRefs)
			return nil
		}
	}

	ownerRefs = append(ownerRefs, owner)
	obj.SetOwnerReferences(ownerRefs)

	return nil
}

// Removes a Loose Ownerreference based on UID.
func RemoveLooseOwnerReference(
	obj client.Object,
	owner metav1.OwnerReference,
) {
	refs := []metav1.OwnerReference{}

	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.UID == owner.UID {
			continue
		}

		refs = append(refs, ownerRef)
	}

	obj.SetOwnerReferences(refs)
}

// If not returns false.
func HasLooseOwnerReference(
	obj client.Object,
	owner metav1.OwnerReference,
) bool {
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.UID == owner.UID {
			return true
		}
	}

	return false
}

func GetLooseOwnerReference(
	obj client.Object,
) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: obj.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:       obj.GetObjectKind().GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func LooseOwnerReferenceEqual(a, b metav1.OwnerReference) bool {
	return a.APIVersion == b.APIVersion &&
		a.Kind == b.Kind &&
		a.Name == b.Name &&
		a.UID == b.UID
}

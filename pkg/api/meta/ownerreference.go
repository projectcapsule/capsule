// Copyright 2020-2026 Project Capsule Authors
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

// RemoveLooseOwnerReferenceForKindExceptGiven removes all ownerReferences that have the same
// APIVersion and Kind as the given owner, except the given owner itself (matched by UID).
// OwnerReferences with different APIVersion/Kind are preserved.
//
// If the given owner is not present on the object, all ownerReferences with that APIVersion/Kind
// are removed.
func RemoveLooseOwnerReferenceForKindExceptGiven(
	obj client.Object,
	owner metav1.OwnerReference,
) {
	in := obj.GetOwnerReferences()
	out := make([]metav1.OwnerReference, 0, len(in))

	for _, ref := range in {
		sameKind := ref.Kind == owner.Kind
		sameAPIVersion := ref.APIVersion == owner.APIVersion

		if !sameKind || !sameAPIVersion {
			out = append(out, ref)

			continue
		}

		if ref.UID == owner.UID {
			out = append(out, ref)
		}
	}

	obj.SetOwnerReferences(out)
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

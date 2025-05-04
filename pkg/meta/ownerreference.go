// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Adds an ownerreferences, which does not delete the object when the owner is deleted.
func SetLooseOwnerReference(
	obj client.Object,
	owner client.Object,
	schema *runtime.Scheme,
) (err error) {
	err = controllerutil.SetOwnerReference(owner, obj, schema)
	if err != nil {
		return err
	}

	ownerRefs := obj.GetOwnerReferences()
	for i, ownerRef := range ownerRefs {
		if ownerRef.UID == owner.GetUID() {
			if ownerRef.BlockOwnerDeletion != nil || ownerRef.Controller != nil {
				ownerRefs[i].BlockOwnerDeletion = nil
				ownerRefs[i].Controller = nil
			}

			break
		}
	}

	return nil
}

// Removes a Loose Ownerreference based on UID
func RemoveLooseOwnerReference(
	obj client.Object,
	owner client.Object,
) (updated bool) {
	ownerRefs := obj.GetOwnerReferences()
	updated = false

	for i, ownerRef := range ownerRefs {
		if ownerRef.UID == owner.GetUID() {
			ownerRefs = append(ownerRefs[:i], ownerRefs[i+1:]...)

			updated = true

			break
		}
	}

	if updated {
		obj.SetOwnerReferences(ownerRefs)
	}

	return
}

// If not returns false.
func HasLooseOwnerReference(
	obj client.Object,
	owner client.Object,
) bool {
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.UID == obj.GetUID() {
			return true
		}
	}

	return false
}

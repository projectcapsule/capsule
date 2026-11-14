// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// StorageMutation supplies storage defaults on PersistentVolumeClaim creation.
// +kubebuilder:object:generate=true
type StorageMutation struct {
	// AttributeClass sets spec.volumeAttributesClassName on PersistentVolumeClaim
	// creation, including the claims the StatefulSet and ephemeral volume
	// controllers create from templates under their own identity. Merge fills an
	// omitted or null class and keeps an empty one, which chooses no class, as
	// an empty storageClassName opts out of the default StorageClass. The first
	// applicable merge wins. Replace always sets the class. Updates never change
	// the class, since a new class modifies the volume. Capsule neither reads nor
	// creates the VolumeAttributesClass; Kubernetes keeps a claim whose class
	// doesn't exist pending until it does. Omitted or null leaves the claim
	// unchanged. Tenant and Namespace templates are supported; rendered names
	// must satisfy native VolumeAttributesClass name validation.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	AttributeClass string `json:"attributeClass,omitempty"`
}

// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package v1beta1 contains API Schema definitions for the capsule v1beta1 API group
// +kubebuilder:object:generate=true
// +groupName=capsule.clastix.io
package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "capsule.clastix.io", Version: "v1beta1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

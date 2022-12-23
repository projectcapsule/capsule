// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"strings"

	"github.com/clastix/capsule/pkg/api"
)

type NamespaceOptions struct {
	// +kubebuilder:validation:Minimum=1
	// Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.
	Quota *int32 `json:"quota,omitempty"`
	// Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.
	AdditionalMetadata *api.AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
}

func (in *Tenant) hasForbiddenNamespaceLabelsAnnotations() bool {
	if _, ok := in.Annotations[api.ForbiddenNamespaceLabelsAnnotation]; ok {
		return true
	}

	if _, ok := in.Annotations[api.ForbiddenNamespaceLabelsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (in *Tenant) hasForbiddenNamespaceAnnotationsAnnotations() bool {
	if _, ok := in.Annotations[api.ForbiddenNamespaceAnnotationsAnnotation]; ok {
		return true
	}

	if _, ok := in.Annotations[api.ForbiddenNamespaceAnnotationsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (in *Tenant) ForbiddenUserNamespaceLabels() *api.ForbiddenListSpec {
	if !in.hasForbiddenNamespaceLabelsAnnotations() {
		return nil
	}

	return &api.ForbiddenListSpec{
		Exact: strings.Split(in.Annotations[api.ForbiddenNamespaceLabelsAnnotation], ","),
		Regex: in.Annotations[api.ForbiddenNamespaceLabelsRegexpAnnotation],
	}
}

func (in *Tenant) ForbiddenUserNamespaceAnnotations() *api.ForbiddenListSpec {
	if !in.hasForbiddenNamespaceAnnotationsAnnotations() {
		return nil
	}

	return &api.ForbiddenListSpec{
		Exact: strings.Split(in.Annotations[api.ForbiddenNamespaceAnnotationsAnnotation], ","),
		Regex: in.Annotations[api.ForbiddenNamespaceAnnotationsRegexpAnnotation],
	}
}

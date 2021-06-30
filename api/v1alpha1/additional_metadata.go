// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

type AdditionalMetadataSpec struct {
	AdditionalLabels      map[string]string `json:"additionalLabels,omitempty"`
	AdditionalAnnotations map[string]string `json:"additionalAnnotations,omitempty"`
}

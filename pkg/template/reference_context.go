// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

// +kubebuilder:object:generate=true
type TemplateResourceReference struct {
	ResourceReference `json:",inline"`

	// Index to mount the resource in the template context
	Index string `json:"index,omitempty"`
}

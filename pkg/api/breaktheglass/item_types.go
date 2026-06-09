// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:generate=true

// TemplateItem defines an item to be applied by a break request.
type TemplateItem struct {
	// +kubebuilder:validation:Required
	ManifestTemplate runtime.RawExtension `json:"manifestTemplate"`
	ParamSchema      runtime.RawExtension `json:"paramSchema,omitempty"`
}

type (
	// TemplateItems maps template items by name.
	TemplateItems map[string]TemplateItem

	// Items maps rendered items by name.
	Items map[string]*runtime.RawExtension
)

// TemplateParams defines a set of parameters to be used in a template item.
type TemplateParams map[string]runtime.RawExtension

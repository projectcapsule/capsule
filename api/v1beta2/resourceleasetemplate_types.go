// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// ResourceLeaseTemplateSpec defines the desired state of a namespaced ResourceLeaseTemplate.
type ResourceLeaseTemplateSpec struct {
	// Impersonation identifies the namespace-local ServiceAccount used for
	// context loading and every managed-resource action performed for requests
	// using this template. When omitted, the tenant default ServiceAccount from
	// CapsuleConfiguration is used. If neither is configured, Capsule uses its
	// controller identity.
	// +optional
	Impersonation *meta.LocalRFC1123ObjectReference `json:"impersonation,omitempty"`

	// Resources rendered and managed by this template.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Resources []apiruntime.ResourceTemplate `json:"resources"`

	// ParamSchema is the JSON Schema used to validate template parameters.
	// Properties may use the x-capsule-form vendor extension to select values
	// from arbitrary Kubernetes GVKs in compatible form clients. The schema may
	// use x-kubernetes-validations for Kubernetes-compatible CEL rules.
	ParamSchema *k8sruntime.RawExtension `json:"paramSchema,omitempty"`

	// Context loads additional Kubernetes resources for use by all resource targets and templates.
	// Resource reference fields may use parameters declared by ParamSchema.
	// +optional
	Context *tpl.TemplateContext `json:"context,omitempty"`

	// The default duration of a ResourceLease referencing this template.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The maximum allowed duration of a ResourceLease referencing this template.
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`

	// The duration a ResourceLease is retained after it expires for auditing.
	KeepFor *resourcelease.ExtendedDuration `json:"keepFor,omitempty"`

	// Approvals configures automatic and manual approval of requests using this template.
	// +optional
	Approvals resourcelease.ApprovalSpec `json:"approvals,omitempty"`
}

func (brt *ResourceLeaseTemplate) TemplateData() ResourceLeaseTemplateData {
	return ResourceLeaseTemplateData{
		Resources:       brt.Spec.Resources,
		ParamSchema:     brt.Spec.ParamSchema,
		Context:         brt.Spec.Context,
		DefaultDuration: brt.Spec.DefaultDuration,
		MaxDuration:     brt.Spec.MaxDuration,
		KeepFor:         brt.Spec.KeepFor,
		Approvals:       brt.Spec.Approvals,
	}
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=rlt
// +kubebuilder:printcolumn:name="AutoApprove",type=boolean,JSONPath=`.spec.approvals.auto`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// ResourceLeaseTemplate is the Schema for namespaced ResourceLease templates.
type ResourceLeaseTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ResourceLeaseTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceLeaseTemplateList contains a list of ResourceLeaseTemplate.
type ResourceLeaseTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ResourceLeaseTemplate `json:"items"`
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// ResourcePermitTemplateSpec defines the desired state of a namespaced ResourcePermitTemplate.
type ResourcePermitTemplateSpec struct {
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

	// The default duration of a ResourcePermit referencing this template.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The maximum allowed duration of a ResourcePermit referencing this template.
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`

	// The duration a ResourcePermit is retained after it expires for auditing.
	KeepFor *resourcepermit.ExtendedDuration `json:"keepFor,omitempty"`

	// Approvals configures automatic and manual approval of requests using this template.
	// +optional
	Approvals resourcepermit.ApprovalSpec `json:"approvals,omitempty"`
}

func (brt *ResourcePermitTemplate) TemplateData() ResourcePermitTemplateData {
	return ResourcePermitTemplateData{
		Resources:       brt.Spec.Resources,
		ParamSchema:     brt.Spec.ParamSchema,
		Context:         brt.Spec.Context,
		DefaultDuration: brt.Spec.DefaultDuration,
		MaxDuration:     brt.Spec.MaxDuration,
		KeepFor:         brt.Spec.KeepFor,
		Approvals:       brt.Spec.Approvals,
	}
}

// ResourcePermitTemplateStatus defines the observed state of ResourcePermitTemplate.
type ResourcePermitTemplateStatus struct {
	// ObservedGeneration is the most recent generation resolved by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contains the reconciliation conditions for this template.
	// +optional
	Conditions meta.ConditionList `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=rpt
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AutoApprove",type=boolean,JSONPath=`.spec.approvals.auto`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// ResourcePermitTemplate is the Schema for namespaced ResourcePermit templates.
type ResourcePermitTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePermitTemplateSpec   `json:"spec,omitempty"`
	Status ResourcePermitTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePermitTemplateList contains a list of ResourcePermitTemplate.
type ResourcePermitTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ResourcePermitTemplate `json:"items"`
}

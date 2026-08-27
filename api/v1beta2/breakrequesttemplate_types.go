// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// BreakRequestTemplateSpec defines the desired state of BreakRequestTemplate.
type BreakRequestTemplateSpec struct {
	// NamespaceSelectors limit the namespaces in which BreakRequests may reference this template.
	// Selectors are ORed. When omitted, the template is available in every namespace.
	// +optional
	NamespaceSelectors []selectors.NamespaceSelector `json:"namespaceSelectors,omitempty"`

	// Resources rendered and managed by this template.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Resources []apiruntime.ResourceTemplate `json:"resources"`

	// ParamSchema template parameter schema
	ParamSchema *k8sruntime.RawExtension `json:"paramSchema,omitempty"`

	// Context loads additional Kubernetes resources for use by all resource targets and templates.
	// Resource reference fields may use parameters declared by ParamSchema.
	// +optional
	Context *tpl.TemplateContext `json:"context,omitempty"`

	// The default duration of the BreakRequest referencing this template should be valid for. If not set,
	// the resource will be kept until the request is deleted.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The max allowed duration of the BreakRequest referencing this template should be valid for.
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`

	// The duration of this BreakRequest will be kept in the system after it has been expired (eg. auditing purposes)
	// If not set, the BreakRequest will be deleted after expiring.
	KeepFor *breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`

	// AutoApprove requests created by this template will be automatically approved.
	AutoApprove bool `json:"autoApprove,omitempty"`

	// ApprovalCondition an optional CEL expression that must be successful for the request to be approved.
	ApprovalCondition string `json:"approvalCondition,omitempty"`
}

// BreakRequestTemplateStatus defines the observed state of BreakRequestTemplate.
type BreakRequestTemplateStatus struct {
	// ObservedGeneration is the most recent generation resolved by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Namespaces contains the namespaces allowed to reference this template.
	// A single "*" entry means that the template is available in every namespace.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AutoApprove",type=boolean,JSONPath=`.spec.autoApprove`
// +kubebuilder:printcolumn:name="Condition",type=string,JSONPath=`.spec.approvalCondition`,priority=10

// BreakRequestTemplate is the Schema for the breakrequesttemplates API.
type BreakRequestTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BreakRequestTemplateSpec   `json:"spec,omitempty"`
	Status BreakRequestTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BreakRequestTemplateList contains a list of BreakRequestTemplate.
type BreakRequestTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BreakRequestTemplate `json:"items"`
}

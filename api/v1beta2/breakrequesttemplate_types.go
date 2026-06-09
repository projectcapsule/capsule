// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

// BreakRequestTemplateSpec defines the desired state of BreakRequestTemplate.
type BreakRequestTemplateSpec struct {
	// Actual Items being created by this template
	// +kubebuilder:validation:Required
	Items breaktheglass.TemplateItems `json:"items"`

	// The default duration of the BreakRequest referencing this template should be valid for. If not set,
	// the resource will be kept until the request is deleted.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The max allowed duration of the BreakRequest referencing this template should be valid for.
	MaxDuration metav1.Duration `json:"maxDuration,omitempty"`

	// The duration of this BreakRequest will be kept in the system after it has been expired (eg. auditing purposes)
	// If not set, the BreakRequest will be deleted after expiring.
	KeepFor breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`

	// AutoApprove requests created by this template will be automatically approved.
	AutoApprove bool `json:"autoApprove,omitempty"`

	// ApprovalCondition an optional CEL expression that must be successful for the request to be approved.
	ApprovalCondition string `json:"approvalCondition,omitempty"`
}

// BreakRequestTemplateStatus defines the observed state of BreakRequestTemplate.
type BreakRequestTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
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

func init() {
	SchemeBuilder.Register(&BreakRequestTemplate{}, &BreakRequestTemplateList{})
}

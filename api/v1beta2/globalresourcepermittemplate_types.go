// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// GlobalResourcePermitTemplateSpec defines the desired state of GlobalResourcePermitTemplate.
type GlobalResourcePermitTemplateSpec struct {
	// Impersonation identifies the ServiceAccount used for context loading and
	// every managed-resource action performed for requests using this template.
	// When omitted, the global default ServiceAccount from CapsuleConfiguration
	// is used. If neither is configured, Capsule uses its controller identity.
	// +optional
	Impersonation *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"impersonation,omitempty"`

	// NamespaceSelectors limit the namespaces in which ResourcePermits may reference this template.
	// Selectors are ORed. When omitted, the template is available in every namespace.
	// +optional
	NamespaceSelectors []selectors.NamespaceSelector `json:"namespaceSelectors,omitempty"`

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

	// The default duration of the ResourcePermit referencing this template should be valid for. If not set,
	// the resource will be kept until the request is deleted.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The max allowed duration of the ResourcePermit referencing this template should be valid for.
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`

	// The duration of this ResourcePermit will be kept in the system after it has been expired (eg. auditing purposes)
	// If not set, the ResourcePermit will be deleted after expiring.
	KeepFor *resourcepermit.ExtendedDuration `json:"keepFor,omitempty"`

	// Approvals configures automatic and manual approval of requests using this template.
	// +optional
	Approvals resourcepermit.ApprovalSpec `json:"approvals,omitempty"`
}

// GlobalResourcePermitTemplateStatus defines the observed state of GlobalResourcePermitTemplate.
type GlobalResourcePermitTemplateStatus struct {
	// ObservedGeneration is the most recent generation resolved by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Namespaces contains the namespaces allowed to reference this template.
	// A single "*" entry means that the template is available in every namespace.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// Conditions contains the reconciliation conditions for this template.
	// +optional
	Conditions meta.ConditionList `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=grpt
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AutoApprove",type=boolean,JSONPath=`.spec.approvals.auto`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// GlobalResourcePermitTemplate is the Schema for the globalresourcepermittemplates API.
type GlobalResourcePermitTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlobalResourcePermitTemplateSpec   `json:"spec,omitempty"`
	Status GlobalResourcePermitTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GlobalResourcePermitTemplateList contains a list of GlobalResourcePermitTemplate.
type GlobalResourcePermitTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GlobalResourcePermitTemplate `json:"items"`
}

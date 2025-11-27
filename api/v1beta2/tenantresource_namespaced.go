// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// TenantResourceSpec defines the desired state of TenantResource.
type TenantResourceSpec struct {
	misc.ReplicationSettings `json:",inline"`

	// Defines the rules to select targeting Namespace, along with the objects that must be replicated.
	Resources []ResourceSpec `json:"resources"`
}

type ResourceSpec struct {
	// Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated.
	// In case of nil value, all the Tenant Namespaces are targeted.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// List of the resources already existing in other Namespaces that must be replicated.
	NamespacedItems []misc.ResourceReference `json:"namespacedItems,omitempty"`
	// List of raw resources that must be replicated.
	RawItems []RawExtension `json:"rawItems,omitempty"`
	// Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be
	// added to the replicated resources.
	AdditionalMetadata *api.AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Templates for advanced use cases
	Templates []TemplateItemSpec `json:"templates,omitempty"`
	// Provide additional template context, which can be used throughout all
	// the declared items for the replication
	// +optional
	Context *tpl.TemplateContext `json:"context,omitempty"`
}

// +kubebuilder:validation:XEmbeddedResource
// +kubebuilder:validation:XPreserveUnknownFields
type RawExtension struct {
	runtime.RawExtension `json:",inline"`
}

// TenantResourceStatus defines the observed state of TenantResource.
type TenantResourceStatus struct {
	// List of the replicated resources for the given TenantResource.
	ProcessedItems ProcessedItems `json:"processedItems"`
	// Condition of the GlobalTenantResource.
	Conditions meta.ConditionList `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status for the tenant"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message for the tenant"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// TenantResource allows a Tenant Owner, if enabled with proper RBAC, to propagate resources in its Namespace.
// The object must be deployed in a Tenant Namespace, and cannot reference object living in non-Tenant namespaces.
// For such cases, the GlobalTenantResource must be used.
type TenantResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantResourceSpec   `json:"spec,omitempty"`
	Status TenantResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantResourceList contains a list of TenantResource.
type TenantResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TenantResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TenantResource{}, &TenantResourceList{})
}

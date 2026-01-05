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
	// Define the period of time upon a second reconciliation must be invoked.
	// Keep in mind that any change to the manifests will trigger a new reconciliation.
	// +kubebuilder:default="60s"
	ResyncPeriod metav1.Duration `json:"resyncPeriod"`
	// Deprecated: Each Resource now declares it's own Prune property
	//
	// When the replicated resource manifest is deleted, all the objects replicated so far will be automatically deleted.
	// Disable this to keep replicated resources although the deletion of the replication manifest.
	// +kubebuilder:default=true
	PruningOnDelete *bool `json:"pruningOnDelete,omitempty"`
	// When cordoning a replication it will no longer execute any applies or deletions (paused).
	// This is useful for maintenances
	// +kubebuilder:default=false
	Cordoned *bool `json:"cordoned,omitempty"`
	// Local ServiceAccount which will perform all the actions defined in the TenantResource
	// You must provide permissions accordingly to that ServiceAccount
	ServiceAccount *api.ServiceAccountReference `json:"serviceAccount,omitempty"`
	// Defines the rules to select targeting Namespace, along with the objects that must be replicated.
	Resources []ResourceSpec `json:"resources"`
}

type ResourceSpec struct {
	// +kubebuilder:default={}
	ResourceSpecSettings `json:"settings,omitzero"`

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

type ResourceSpecSettings struct {
	// When the replicated resource manifest is deleted, all the objects replicated so far will be automatically deleted.
	// Disable this to keep replicated resources although the deletion of the replication manifest.
	// +kubebuilder:default=true
	Prune *bool `json:"prune,omitempty"`
	// Enabling this allows TenanResources to interact with objects which were not created by a TenantResource. In this case on prune no deletion of the entire object is made.
	// +kubebuilder:default=false
	Adopt *bool `json:"adopt,omitempty"`
	// Force indicates that in case of conflicts with server-side apply, the client should acquire ownership of the conflicting field.
	// You may create collisions with this.
	// +kubebuilder:default=false
	Force *bool `json:"force,omitempty"`
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
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec TenantResourceSpec `json:"spec"`

	// +optional
	Status TenantResourceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TenantResourceList contains a list of TenantResource.
type TenantResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []TenantResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TenantResource{}, &TenantResourceList{})
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

type TenantResourceCommonStatus struct {
	// Condition of the GlobalTenantResource.
	Conditions meta.ConditionList `json:"conditions,omitempty"`

	// List of the replicated resources for the given TenantResource.
	//+optional
	ProcessedItems meta.ProcessedItems `json:"processedItems,omitzero"`

	// How many items are being replicated by the TenantResource.
	Size uint `json:"size"`

	// Serviceaccount used for impersonation
	//+optional
	ServiceAccount *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"serviceAccount,omitzero"`
}

func (s *TenantResourceCommonStatus) UpdateStats() {
	s.Size = uint(len(s.ProcessedItems))
}

type TenantResourceCommonSpec struct {
	// Provide additional settings
	// +kubebuilder:default={}
	Settings TenantResourceCommonSpecSettings `json:"settings,omitzero"`
	// DependsOn may contain a meta.NamespacedObjectReference slice
	// with references to TenantResource resources that must be ready before this
	// TenantResource can be reconciled.
	// +optional
	DependsOn []meta.LocalRFC1123ObjectReference `json:"dependsOn,omitempty"`
	// Define the period of time upon a second reconciliation must be invoked.
	// Keep in mind that any change to the manifests will trigger a new reconciliation.
	// +kubebuilder:default="60s"
	ResyncPeriod metav1.Duration `json:"resyncPeriod"`
	// When the replicated resource manifest is deleted, all the objects replicated so far will be automatically deleted.
	// Disable this to keep replicated resources although the deletion of the replication manifest.
	// +kubebuilder:default=true
	PruningOnDelete *bool `json:"pruningOnDelete,omitempty"`
	// When cordoning a replication it will no longer execute any applies or deletions (paused).
	// This is useful for maintenances
	// +kubebuilder:default=false
	Cordoned *bool `json:"cordoned,omitempty"`
	// Defines the rules to select targeting Namespace, along with the objects that must be replicated.
	Resources []ResourceSpec `json:"resources"`
}

type TenantResourceCommonSpecSettings struct {
	// Enabling this allows TenanResources to interact with objects which were not created by a TenantResource. In this case on prune no deletion of the entire object is made.
	// +kubebuilder:default=false
	Adopt *bool `json:"adopt,omitempty"`
	// Force indicates that in case of conflicts with server-side apply, the client should acquire ownership of the conflicting field.
	// You may create collisions with this.
	// +kubebuilder:default=false
	Force *bool `json:"force,omitempty"`
}

type ResourceSpec struct {
	// Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated.
	// In case of nil value, all the Tenant Namespaces are targeted.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// List of the resources already existing in other Namespaces that must be replicated.
	NamespacedItems []tpl.ResourceReference `json:"namespacedItems,omitempty"`
	// List of raw resources that must be replicated.
	RawItems []RawExtension `json:"rawItems,omitempty"`
	// Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be
	// added to the replicated resources.
	AdditionalMetadata *api.AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Templates for advanced use cases
	Generators []TemplateItemSpec `json:"generators,omitempty"`
	// Provide additional template context, which can be used throughout all
	// the declared items for the replication
	// +optional
	Context *tpl.TemplateContext `json:"context,omitempty"`
}

// +kubebuilder:validation:XPreserveUnknownFields
type RawExtension struct {
	runtime.RawExtension `json:",inline"`
}

type TemplateItemSpec struct {
	// Template contains any amount of yaml which is applied to Kubernetes.
	// This can be a single resource or multiple resources
	Template string `json:"template,omitempty"`
	// Missing Key Option for templating
	// +kubebuilder:default=zero
	MissingKey tpl.MissingKeyOption `json:"missingKey,omitempty"`
}

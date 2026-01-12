// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

type TenantResourceCommonStatus struct {
	// Condition of the GlobalTenantResource.
	Conditions meta.ConditionList `json:"conditions,omitempty"`

	// List of the replicated resources for the given TenantResource.
	//+optional
	ProcessedItems ProcessedItems `json:"processedItems,omitzero"`

	// How many items are being replicated by the TenantResource.
	Size uint `json:"size"`
}

func (s *TenantResourceCommonStatus) UpdateStats() {
	s.Size = uint(len(s.ProcessedItems))
}

type TenantResourceCommonSpec struct {
	// DependsOn may contain a meta.NamespacedObjectReference slice
	// with references to TenantResource resources that must be ready before this
	// TenantResource can be reconciled.
	// +optional
	DependsOn []meta.LocalRFC1123ObjectReference `json:"dependsOn,omitempty"`
	// Define the period of time upon a second reconciliation must be invoked.
	// Keep in mind that any change to the manifests will trigger a new reconciliation.
	// +kubebuilder:default="60s"
	ResyncPeriod metav1.Duration `json:"resyncPeriod"`
	// Enabling this allows TenanResources to interact with objects which were not created by a TenantResource. In this case on prune no deletion of the entire object is made.
	// +kubebuilder:default=false
	Adopt *bool `json:"adopt,omitempty"`
	// Force indicates that in case of conflicts with server-side apply, the client should acquire ownership of the conflicting field.
	// You may create collisions with this.
	// +kubebuilder:default=false
	Force *bool `json:"force,omitempty"`
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

// +kubebuilder:validation:XPreserveUnknownFields
type RawExtension struct {
	runtime.RawExtension `json:",inline"`
}

type ProcessedItems []ObjectReferenceStatus

// Adds a condition by type.
func (p *ProcessedItems) UpdateItem(item ObjectReferenceStatus) {
	for i, stat := range *p {
		if p.isEqual(stat, item) {
			(*p)[i].ObjectReferenceStatusCondition = item.ObjectReferenceStatusCondition

			return
		}
	}

	*p = append(*p, item)
}

// Removes a condition by type.
func (p *ProcessedItems) RemoveItem(item ObjectReferenceStatus) {
	filtered := make(ProcessedItems, 0, len(*p))

	for _, stat := range *p {
		if !p.isEqual(stat, item) {
			filtered = append(filtered, stat)
		}
	}

	*p = filtered
}

func (p *ProcessedItems) isEqual(a, b ObjectReferenceStatus) bool {
	return a.ResourceID == b.ResourceID
}

type ObjectReference struct {
	// Name of the referent.
	// +required
	Name string `json:"name"`

	// Namespace of the referent, when not specified it acts as LocalObjectReference.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

type TemplateItemSpec struct {
	// Template contains any amount of yaml which is applied to Kubernetes.
	// This can be a single resource or multiple resources
	Template string `json:"template,omitempty"`
	// Missing Key Option for templating
	// +kubebuilder:default=default
	MissingKey tpl.MissingKeyOption `json:"missingKey,omitempty"`
}

type ObjectReferenceAbstract struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	Namespace string `json:"namespace"`
	// API version of the referent.
	APIVersion string `json:"apiVersion,omitempty"`
}

type ObjectReferenceStatus struct {
	misc.ResourceID `json:",inline"`

	ObjectReferenceStatusCondition `json:"status,omitempty"`
}

type ObjectReferenceStatusCondition struct {
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`

	// An opaque value that represents the internal version of this object that can
	// be used by clients to determine when objects have changed. May be used for optimistic
	// concurrency, change detection, and the watch operation on a resource or set of resources.
	// Clients must treat these values as opaque and passed unmodified back to the server.
	// They may only be valid for a particular resource or set of resources.
	//
	// Populated by the system.
	// Read-only.
	// Value must be treated as opaque by clients and .
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#concurrency-control-and-consistency
	// +optional
	LastApply metav1.Time `json:"lastApply,omitempty,omitzero" protobuf:"bytes,8,opt,name=lastApply"`

	// Indicates wether the resource was created or adopted
	Created bool `json:"created,omitempty"`
}
type ObjectReferenceStatusOwner struct {
	// Name of the owning object.
	Name string `json:"name,omitempty"`
	// UID of the owning object.
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`
	// Scope of the owning object.
	Scope api.ResourceScope `json:"scope,omitempty"`
}

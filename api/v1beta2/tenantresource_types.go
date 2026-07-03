// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

type TenantResourceCommonStatus struct {
	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
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
	// Triggers re-render this resource (near-)immediately when matching cluster
	// objects change, instead of only waiting for resyncPeriod. This lets you keep
	// a high resyncPeriod while still reacting quickly to changes of the objects
	// the rendering depends on (e.g. Secrets or ServiceAccounts referenced through
	// a resource's context). Each trigger installs a metadata-only watch per
	// referenced kind; a watch is torn down automatically once no resource
	// references its kind anymore.
	// +optional
	// +kubebuilder:validation:MaxItems=100
	Triggers []TriggerSpec `json:"triggers,omitempty"`
}

// TriggerOperation is the object lifecycle event a trigger reacts to.
// +kubebuilder:validation:Enum=CREATE;UPDATE;DELETE
type TriggerOperation string

const (
	// TriggerOperationCreate reacts to creations of matching objects.
	TriggerOperationCreate TriggerOperation = "CREATE"
	// TriggerOperationUpdate reacts to updates of matching objects.
	TriggerOperationUpdate TriggerOperation = "UPDATE"
	// TriggerOperationDelete reacts to deletions of matching objects.
	TriggerOperationDelete TriggerOperation = "DELETE"
)

// TriggerSpec declares the cluster object kinds whose changes cause the owning
// TenantResource / GlobalTenantResource to be re-rendered.
//
// Wildcards are rejected: every kind selected by a trigger is armed as a
// dedicated watch, so the selection must be a bounded, concrete set.
//
// +kubebuilder:validation:XValidation:rule="!self.kinds.exists(k, k.contains('*'))",message="wildcard kinds are not supported in triggers"
// +kubebuilder:validation:XValidation:rule="!has(self.apiGroups) || !self.apiGroups.exists(g, g.contains('*'))",message="wildcard apiGroups are not supported in triggers"
type TriggerSpec struct {
	capruntime.VersionKinds `json:",inline"`

	// Operations that cause a re-render. When empty, all operations
	// (CREATE, UPDATE and DELETE) are considered.
	// +optional
	Operations []TriggerOperation `json:"operations,omitempty"`
	// Selector narrows the trigger to objects whose labels match. When omitted,
	// every object of the referenced kind matches.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// NamespaceSelector narrows the trigger to objects living in namespaces whose
	// labels match. It is only honored for the cluster-scoped GlobalTenantResource;
	// for the namespaced TenantResource it is ignored, as the trigger is always
	// scoped to the namespaces of the owning Tenant.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// MatchesOperation reports whether the trigger reacts to the given operation.
// An empty operation list matches every operation.
func (t TriggerSpec) MatchesOperation(op TriggerOperation) bool {
	if len(t.Operations) == 0 {
		return true
	}

	return slices.Contains(t.Operations, op)
}

// Matches reports whether the trigger reacts to a change of the given kind and
// operation whose object carries the given labels. Namespace scoping
// (NamespaceSelector, tenant scoping) is consumer policy and not evaluated here.
func (t TriggerSpec) Matches(gvk schema.GroupVersionKind, op TriggerOperation, lbls map[string]string) bool {
	if !t.MatchesGroupVersionKind(gvk) || !t.MatchesOperation(op) {
		return false
	}

	if t.Selector == nil {
		return true
	}

	ok, err := selectors.MatchesSelector(labels.Set(lbls), *t.Selector)

	return err == nil && ok
}

// TriggerVersionKinds returns the de-duplicated set of kind selectors
// referenced by the resource's triggers. Selectors without a concrete version
// (e.g. apiGroups: ["apps"]) are resolved to a watchable GroupVersionKind by
// the trigger watch manager via the REST mapper.
func (s *TenantResourceCommonSpec) TriggerVersionKinds() []capruntime.VersionKind {
	seen := make(map[capruntime.VersionKind]struct{}, len(s.Triggers))
	out := make([]capruntime.VersionKind, 0, len(s.Triggers))

	for _, t := range s.Triggers {
		for _, vk := range t.VersionKinds.VersionKinds() {
			if vk.Kind == "" {
				continue
			}

			if _, ok := seen[vk]; ok {
				continue
			}

			seen[vk] = struct{}{}

			out = append(out, vk)
		}
	}

	return out
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

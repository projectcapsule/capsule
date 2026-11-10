// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

type TenantResourceCommonStatus struct {
	meta.ManagedResourcesStatus `json:",inline"`

	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Condition of the GlobalTenantResource.
	Conditions meta.ConditionList `json:"conditions,omitempty"`

	// Serviceaccount used for impersonation
	//+optional
	ServiceAccount *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"serviceAccount,omitzero"`
	// Trigger records the last admission notification handled by this controller.
	// +optional
	Trigger *TenantResourceTriggerStatus `json:"trigger,omitempty"`
	// NextResyncAt is the next periodic resync deadline for trigger-enabled replications.
	// +optional
	NextResyncAt *metav1.MicroTime `json:"nextResyncAt,omitempty"`
}

type TenantResourceTriggerStatus struct {
	// ObservedRequest identifies the notification included in the last triggered reconcile.
	ObservedRequest string `json:"observedRequest"`
	// LastTriggeredAt is the start of the last triggered reconcile, including failed attempts.
	LastTriggeredAt metav1.MicroTime `json:"lastTriggeredAt"`
}

// TenantResourceTrigger requests best-effort reconciliation on matching admission attempts.
// Resource behavior and replication destinations remain defined by resources.
type TenantResourceTrigger struct {
	admissionv1.RuleWithOperations `json:",inline"`

	// Name uniquely identifies this trigger.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
	// Audience uses the same identity matching as namespace rules. Empty matches everyone.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	Audience []rules.Audience `json:"audience,omitempty"`
	// NamespaceSelector selects event source namespaces for GlobalTenantResources.
	// TenantResources always observe only their own namespace and cannot set this field.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// ObjectSelector matches the labels of either the old or new admission object.
	// +optional
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty"`
	// MatchConditions are native admission CEL conditions. All must match.
	// +optional
	// +kubebuilder:validation:MaxItems=60
	// +listType=map
	// +listMapKey=name
	MatchConditions []admissionv1.MatchCondition `json:"matchConditions,omitempty"`
}

type TenantResourceCommonSpec struct {
	// Triggers request reconciliation on matching namespaced admission attempts.
	// Capsule-managed resources never trigger reconciliation. Delivery is best effort;
	// resyncPeriod remains the recovery mechanism for missed or pre-persistence events.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	Triggers []TenantResourceTrigger `json:"triggers,omitempty"`
	// TriggerMinInterval bounds triggered reconciliation frequency across all triggers
	// on this resource. Events within the interval coalesce into one pending reconcile.
	// Spec changes, deletion, manual requests, and periodic resyncs retain their behavior.
	// +optional
	// +kubebuilder:default="10s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('24h')",message="triggerMinInterval must be between 1s and 24h"
	TriggerMinInterval metav1.Duration `json:"triggerMinInterval,omitzero"`
	// Deprecated: configure resources[].policy instead. Admission converts Settings
	// into policies for blocks without one. Retained as a compatibility fallback
	// for stored resources that have not passed through admission again.
	//
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
	//
	// Deprecated: use resources[].policy.deletion instead. Retained for blocks without a policy.
	//
	// +kubebuilder:default=true
	PruningOnDelete *bool `json:"pruningOnDelete,omitempty"`
	// When cordoning a replication it will no longer execute any applies or deletions (paused).
	// This is useful for maintenances
	// +kubebuilder:default=false
	Cordoned *bool `json:"cordoned,omitempty"`
	// Defines the rules to select targeting Namespace, along with the objects that must be replicated.
	Resources []ResourceSpec `json:"resources"`
}

// IsCordoned states whether the replication is paused, in which case no apply nor deletion
// must be performed. Being an optional field, an unset value is not cordoned.
func (s *TenantResourceCommonSpec) IsCordoned() bool {
	return s.Cordoned != nil && *s.Cordoned
}

type TenantResourceCommonSpecSettings struct {
	// Enabling this allows TenanResources to interact with objects which were not created by a TenantResource. In this case on prune no deletion of the entire object is made.
	//
	// Deprecated: use resources[].policy.creation: Merge instead.
	//
	// +kubebuilder:default=false
	Adopt *bool `json:"adopt,omitempty"`
	// Force indicates that in case of conflicts with server-side apply, the client should acquire ownership of the conflicting field.
	// You may create collisions with this.
	//
	// Deprecated: use resources[].policy.force instead.
	//
	// +kubebuilder:default=false
	Force *bool `json:"force,omitempty"`
}

type ResourceSpec struct {
	// Policy controls creation, admission protection, SSA conflict ownership,
	// and deletion for every raw item, namespaced item, and generator in this block.
	// An explicit policy replaces the deprecated spec.settings for this block.
	// When omitted, admission converts legacy settings and pruningOnDelete into a policy.
	// +optional
	Policy *apiruntime.ResourceReplicationPolicy `json:"policy,omitempty"`

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

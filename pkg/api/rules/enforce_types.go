// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

type AudienceKind string

const (
	AudienceKindUser           AudienceKind = "User"
	AudienceKindGroup          AudienceKind = "Group"
	AudienceKindServiceAccount AudienceKind = "ServiceAccount"
	AudienceKindCustom         AudienceKind = "Custom"
)

type CustomAudience string

const (
	CustomAudienceCapsuleUser   CustomAudience = "CapsuleUser"
	CustomAudienceAdministrator CustomAudience = "Administrator"
	CustomAudienceTenantOwner   CustomAudience = "TenantOwner"
	CustomAudienceController    CustomAudience = "Controller"
)

// +kubebuilder:object:generate=true
type Audience struct {
	// +kubebuilder:validation:Enum=User;Group;ServiceAccount;Custom
	Kind AudienceKind `json:"kind"`
	Name string       `json:"name"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceBody struct {
	// Conditions gate this entire enforcement rule, including workloads, services,
	// metadata, ingress and storage. All conditions must be true; empty means apply.
	// Conditions run during admission; managed metadata in a conditional rule
	// is applied on matching requests only and is not reconciled in the background.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Conditions []AdmissionCondition `json:"conditions,omitempty"`

	// Declare the action being performed on the enforcement rule:
	// deny: On match, deny admission request
	// allow: On match, allowed admission request
	// audit: On match, audit (post event) of admission request
	//+kubebuilder:default:=deny
	Action ActionType `json:"action,omitempty"`

	// Enforcement for native workload kinds and Pod properties.
	Workloads NamespaceRuleEnforceWorkloadsBody `json:"workloads,omitempty"`

	// Enforcement for Services.
	// +optional
	Services NamespaceRuleEnforceServicesBody `json:"services,omitempty"`

	// Additional access to PersistentVolumes without a tenant ownership label.
	// +optional
	Storage NamespaceRuleEnforceStorageBody `json:"storage,omitempty"`

	// Enforcement for object metadata on namespaced resources.
	//
	// +optional
	Metadata []MetadataRule `json:"metadata,omitempty"`

	// Enforcement for Ingress and Gateway API resource hostnames.
	// +optional
	Ingress NamespaceRuleEnforceIngressBody `json:"ingress,omitempty"`
}

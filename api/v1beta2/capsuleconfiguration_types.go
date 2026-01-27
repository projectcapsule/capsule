// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// CapsuleConfigurationSpec defines the Capsule configuration.
type CapsuleConfigurationSpec struct {
	// Define entities which are considered part of the Capsule construct
	// Users not mentioned here will be ignored by Capsule
	Users api.UserListSpec `json:"users,omitempty"`
	// Deprecated: use users property instead (https://projectcapsule.dev/docs/operating/setup/configuration/#users)
	//
	// Names of the users considered as Capsule users.
	UserNames []string `json:"userNames,omitempty"`
	// Deprecated: use users property instead (https://projectcapsule.dev/docs/operating/setup/configuration/#users)
	//
	// Names of the groups considered as Capsule users.
	UserGroups []string `json:"userGroups,omitempty"`
	// Define groups which when found in the request of a user will be ignored by the Capsule
	// this might be useful if you have one group where all the users are in, but you want to separate administrators from normal users with additional groups.
	IgnoreUserWithGroups []string `json:"ignoreUserWithGroups,omitempty"`
	// ServiceAccounts within tenant namespaces can be promoted to owners of the given tenant
	// this can be achieved by labeling the serviceaccount and then they are considered owners. This can only be done by other owners of the tenant.
	// However ServiceAccounts which have been promoted to owner can not promote further serviceAccounts.
	// +kubebuilder:default=false
	AllowServiceAccountPromotion bool `json:"allowServiceAccountPromotion,omitempty"`
	// Enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix,
	// separated by a dash. This is useful to avoid Namespace name collision in a public CaaS environment.
	// +kubebuilder:default=false
	ForceTenantPrefix bool `json:"forceTenantPrefix,omitempty"`
	// Disallow creation of namespaces, whose name matches this regexp
	ProtectedNamespaceRegexpString string `json:"protectedNamespaceRegex,omitempty"`
	// Allows to set different name rather than the canonical one for the Capsule configuration objects,
	// such as webhook secret or configurations.
	// +kubebuilder:default={TLSSecretName:"capsule-tls",mutatingWebhookConfigurationName:"capsule-mutating-webhook-configuration",validatingWebhookConfigurationName:"capsule-validating-webhook-configuration"}
	// +optional
	CapsuleResources CapsuleResources `json:"overrides,omitzero"`
	// Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant.
	// This applies only if the Tenant has an active NodeSelector, and the Owner have right to patch their nodes.
	NodeMetadata *NodeMetadata `json:"nodeMetadata,omitempty"`
	// Toggles the TLS reconciler, the controller that is able to generate CA and certificates for the webhooks
	// when not using an already provided CA and certificate, or when these are managed externally with Vault, or cert-manager.
	// +kubebuilder:default=false
	EnableTLSReconciler bool `json:"enableTLSReconciler"` //nolint:tagliatelle
	// Define entities which can act as Administrators in the capsule construct
	// These entities are automatically owners for all existing tenants. Meaning they can add namespaces to any tenant. However they must be specific by using the capsule label
	// for interacting with namespaces. Because if that label is not defined, it's assumed that namespace interaction was not targeted towards a tenant and will therefor
	// be ignored by capsule.
	Administrators api.UserListSpec `json:"administrators,omitempty"`
	// Configuration for dynamic Validating and Mutating Admission webhooks managed by Capsule.
	Admission DynamicAdmission `json:"admission,omitempty"`
	// Define Properties for managed ClusterRoles by Capsule
	// +kubebuilder:default={}
	RBAC *RBACConfiguration `json:"rbac"`
	// Define the period of time upon a cache invalidation is executed for all caches.
	// +kubebuilder:default="24h"
	CacheInvalidation metav1.Duration `json:"cacheInvalidation"`
}

type RBACConfiguration struct {
	// The ClusterRoles applied for Administrators
	// +kubebuilder:default={capsule-namespace-deleter}
	AdministrationClusterRoles []string `json:"administrationClusterRoles,omitempty"`
	// The ClusterRoles applied for ServiceAccounts which had owner Promotion
	// +kubebuilder:default={capsule-namespace-provisioner,capsule-namespace-deleter}
	PromotionClusterRoles []string `json:"promotionClusterRoles,omitempty"`
	// Name for the ClusterRole required to grant Namespace Deletion permissions.
	// +kubebuilder:default=capsule-namespace-deleter
	DeleterClusterRole string `json:"deleter,omitempty"`
	// Name for the ClusterRole required to grant Namespace Provision permissions.
	// +kubebuilder:default=capsule-namespace-provisioner
	ProvisionerClusterRole string `json:"provisioner,omitempty"`
}

type DynamicAdmission struct {
	// Configure dynamic Mutating Admission for Capsule
	Mutating DynamicAdmissionConfig `json:"mutating,omitempty"`

	// Configure dynamic Validating Admission for Capsule
	Validating DynamicAdmissionConfig `json:"validating,omitempty"`
}

type DynamicAdmissionConfig struct {
	// Name the Admission Webhook
	Name api.Name `json:"name,omitempty"`
	// Labels added to the Admission Webhook
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations added to the Admission Webhook
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// From the upstram struct
	Client admissionregistrationv1.WebhookClientConfig `json:"client"`
}

type NodeMetadata struct {
	// Define the labels that a Tenant Owner cannot set for their nodes.
	// +optional
	ForbiddenLabels api.ForbiddenListSpec `json:"forbiddenLabels,omitzero"`
	// Define the annotations that a Tenant Owner cannot set for their nodes.
	// +optional
	ForbiddenAnnotations api.ForbiddenListSpec `json:"forbiddenAnnotations,omitzero"`
}

type CapsuleResources struct {
	// Defines the Secret name used for the webhook server.
	// Must be in the same Namespace where the Capsule Deployment is deployed.
	// +kubebuilder:default=capsule-tls
	TLSSecretName string `json:"TLSSecretName"` //nolint:tagliatelle
	// Name of the MutatingWebhookConfiguration which contains the dynamic admission controller paths and resources.
	// +kubebuilder:default=capsule-mutating-webhook-configuration
	MutatingWebhookConfigurationName string `json:"mutatingWebhookConfigurationName"`
	// Name of the ValidatingWebhookConfiguration which contains the dynamic admission controller paths and resources.
	// +kubebuilder:default=capsule-validating-webhook-configuration
	ValidatingWebhookConfigurationName string `json:"validatingWebhookConfigurationName"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion

// CapsuleConfiguration is the Schema for the Capsule configuration API.
type CapsuleConfiguration struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec CapsuleConfigurationSpec `json:"spec"`

	// +optional
	Status CapsuleConfigurationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CapsuleConfigurationList contains a list of CapsuleConfiguration.
type CapsuleConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []CapsuleConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CapsuleConfiguration{}, &CapsuleConfigurationList{})
}

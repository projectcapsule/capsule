// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/pkg/api"
)

// CapsuleConfigurationSpec defines the Capsule configuration.
type CapsuleConfigurationSpec struct {
	// Names of the groups for Capsule users.
	// +kubebuilder:default={capsule.clastix.io}
	UserGroups []string `json:"userGroups,omitempty"`
	// Enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix,
	// separated by a dash. This is useful to avoid Namespace name collision in a public CaaS environment.
	// +kubebuilder:default=false
	ForceTenantPrefix bool `json:"forceTenantPrefix,omitempty"`
	// Disallow creation of namespaces, whose name matches this regexp
	ProtectedNamespaceRegexpString string `json:"protectedNamespaceRegex,omitempty"`
	// Allows to set different name rather than the canonical one for the Capsule configuration objects,
	// such as webhook secret or configurations.
	// +kubebuilder:default={TLSSecretName:"capsule-tls",mutatingWebhookConfigurationName:"capsule-mutating-webhook-configuration",validatingWebhookConfigurationName:"capsule-validating-webhook-configuration"}
	CapsuleResources CapsuleResources `json:"overrides,omitempty"`
	// Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant.
	// This applies only if the Tenant has an active NodeSelector, and the Owner have right to patch their nodes.
	NodeMetadata *NodeMetadata `json:"nodeMetadata,omitempty"`
	// Toggles the TLS reconciler, the controller that is able to generate CA and certificates for the webhooks
	// when not using an already provided CA and certificate, or when these are managed externally with Vault, or cert-manager.
	// +kubebuilder:default=true
	EnableTLSReconciler bool `json:"enableTLSReconciler"` //nolint:tagliatelle
}

type NodeMetadata struct {
	// Define the labels that a Tenant Owner cannot set for their nodes.
	ForbiddenLabels api.ForbiddenListSpec `json:"forbiddenLabels"`
	// Define the annotations that a Tenant Owner cannot set for their nodes.
	ForbiddenAnnotations api.ForbiddenListSpec `json:"forbiddenAnnotations"`
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
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion

// CapsuleConfiguration is the Schema for the Capsule configuration API.
type CapsuleConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CapsuleConfigurationSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CapsuleConfigurationList contains a list of CapsuleConfiguration.
type CapsuleConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CapsuleConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CapsuleConfiguration{}, &CapsuleConfigurationList{})
}

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/pkg/api"
)

// TenantSpec defines the desired state of Tenant.
type TenantSpec struct {
	Owner OwnerSpec `json:"owner"`

	// +kubebuilder:validation:Minimum=1
	NamespaceQuota         *int32                           `json:"namespaceQuota,omitempty"`
	NamespacesMetadata     *AdditionalMetadata              `json:"namespacesMetadata,omitempty"`
	ServicesMetadata       *AdditionalMetadata              `json:"servicesMetadata,omitempty"`
	StorageClasses         *api.AllowedListSpec             `json:"storageClasses,omitempty"`
	IngressClasses         *api.AllowedListSpec             `json:"ingressClasses,omitempty"`
	IngressHostnames       *api.AllowedListSpec             `json:"ingressHostnames,omitempty"`
	ContainerRegistries    *api.AllowedListSpec             `json:"containerRegistries,omitempty"`
	NodeSelector           map[string]string                `json:"nodeSelector,omitempty"`
	NetworkPolicies        []networkingv1.NetworkPolicySpec `json:"networkPolicies,omitempty"`
	LimitRanges            []corev1.LimitRangeSpec          `json:"limitRanges,omitempty"`
	ResourceQuota          []corev1.ResourceQuotaSpec       `json:"resourceQuotas,omitempty"`
	AdditionalRoleBindings []api.AdditionalRoleBindingsSpec `json:"additionalRoleBindings,omitempty"`
	ExternalServiceIPs     *api.ExternalServiceIPsSpec      `json:"externalServiceIPs,omitempty"`
}

// TenantStatus defines the observed state of Tenant.
type TenantStatus struct {
	Size       uint     `json:"size"`
	Namespaces []string `json:"namespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tnt
// +kubebuilder:printcolumn:name="Namespace quota",type="integer",JSONPath=".spec.namespaceQuota",description="The max amount of Namespaces can be created"
// +kubebuilder:printcolumn:name="Namespace count",type="integer",JSONPath=".status.size",description="The total amount of Namespaces in use"
// +kubebuilder:printcolumn:name="Owner name",type="string",JSONPath=".spec.owner.name",description="The assigned Tenant owner"
// +kubebuilder:printcolumn:name="Owner kind",type="string",JSONPath=".spec.owner.kind",description="The assigned Tenant owner kind"
// +kubebuilder:printcolumn:name="Node selector",type="string",JSONPath=".spec.nodeSelector",description="Node Selector applied to Pods"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"
// +kubebuilder:deprecatedversion:warning="This version is going to be dropped in the upcoming version of Capsule; please, migrate to v1beta2 version."

// Tenant is the Schema for the tenants API.
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec,omitempty"`
	Status TenantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Minimum=1
type NamespaceQuota uint

type AdditionalMetadata struct {
	// +nullable
	AdditionalLabels map[string]string `json:"additionalLabels"`
	// +nullable
	AdditionalAnnotations map[string]string `json:"additionalAnnotations"`
}

type StorageClassesSpec struct {
	// +nullable
	Allowed StorageClassList `json:"allowed"`
	// +nullable
	AllowedRegex string `json:"allowedRegex"`
}

type IngressClassesSpec struct {
	// +nullable
	Allowed IngressClassList `json:"allowed"`
	// +nullable
	AllowedRegex string `json:"allowedRegex"`
}

type ContainerRegistriesSpec struct {
	// +nullable
	Allowed RegistryList `json:"allowed"`
	// +nullable
	AllowedRegex string `json:"allowedRegex"`
}

// TenantSpec defines the desired state of Tenant
type TenantSpec struct {
	Owner OwnerSpec `json:"owner"`
	// +kubebuilder:validation:Optional
	NamespacesMetadata AdditionalMetadata `json:"namespacesMetadata"`
	// +kubebuilder:validation:Optional
	ServicesMetadata    AdditionalMetadata      `json:"servicesMetadata"`
	StorageClasses      StorageClassesSpec      `json:"storageClasses"`
	IngressClasses      IngressClassesSpec      `json:"ingressClasses"`
	ContainerRegistries *ContainerRegistriesSpec `json:"containerRegistries,omitempty"`
	// +kubebuilder:validation:Optional
	NodeSelector    map[string]string                `json:"nodeSelector"`
	NamespaceQuota  NamespaceQuota                   `json:"namespaceQuota"`
	NetworkPolicies []networkingv1.NetworkPolicySpec `json:"networkPolicies,omitempty"`
	LimitRanges     []corev1.LimitRangeSpec          `json:"limitRanges"`
	// +kubebuilder:validation:Optional
	ResourceQuota          []corev1.ResourceQuotaSpec `json:"resourceQuotas"`
	AdditionalRoleBindings []AdditionalRoleBindings   `json:"additionalRoleBindings,omitempty"`
}

type AdditionalRoleBindings struct {
	ClusterRoleName string `json:"clusterRoleName"`
	// kubebuilder:validation:Minimum=1
	Subjects []rbacv1.Subject `json:"subjects"`
}

// OwnerSpec defines tenant owner name and kind
type OwnerSpec struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
}

// +kubebuilder:validation:Enum=User;Group
type Kind string

func (k Kind) String() string {
	return string(k)
}

// TenantStatus defines the observed state of Tenant
type TenantStatus struct {
	Size       uint          `json:"size"`
	Namespaces NamespaceList `json:"namespaces,omitempty"`
	Users      []string      `json:"users,omitempty"`
	Groups     []string      `json:"groups,omitempty"`
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

// Tenant is the Schema for the tenants API
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec,omitempty"`
	Status TenantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}

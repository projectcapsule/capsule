// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// GlobalResourceQuotaSpec defines the desired state of GlobalResourceQuota
type GlobalResourceQuotaSpec struct {
	// When a quota is active it's checking for the resources in the cluster
	// If not active the resourcequotas are removed and the webhook no longer blocks updates
	// +kubebuilder:default=true
	Active bool `json:"active,omitempty"`

	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	Selectors []GlobalResourceQuotaSelector `json:"selectors,omitempty"`

	// Define resourcequotas for the namespaces
	Items map[api.Name]corev1.ResourceQuotaSpec `json:"quotas,omitempty"`
}

type GlobalResourceQuotaSelector struct {
	// Only considers namespaces which are part of a tenant, other namespaces which might match
	// the label, but do not have a tenant, are ignored.
	// +kubebuilder:default=true
	MustTenantNamespace bool `json:"tenant,omitempty"`

	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	api.NamespaceSelector `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=globalquota
// +kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Active status of the GlobalResourceQuota"
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.size",description="The total amount of Namespaces spanned across"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// GlobalResourceQuota is the Schema for the globalresourcequotas API
type GlobalResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlobalResourceQuotaSpec   `json:"spec,omitempty"`
	Status GlobalResourceQuotaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GlobalResourceQuotaList contains a list of GlobalResourceQuota
type GlobalResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalResourceQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlobalResourceQuota{}, &GlobalResourceQuotaList{})
}

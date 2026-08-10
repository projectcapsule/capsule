// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

// GlobalResourceQuotaSpec defines a native ResourceQuota shared by every
// namespace matched by any namespace selector.
type GlobalResourceQuotaSpec struct {
	// NamespaceSelectors select the namespaces that share this quota.
	// Selectors are ORed; requirements within one selector are ANDed. An empty
	// label selector matches all namespaces.
	NamespaceSelectors []selectors.NamespaceSelector `json:"namespaceSelectors,omitempty"`

	// Quota is the native Kubernetes ResourceQuota specification enforced
	// across the selected namespaces.
	Quota corev1.ResourceQuotaSpec `json:"quota"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=globalquota;grq
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.namespaceCount",description="Selected namespaces"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile message"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type GlobalResourceQuota struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec GlobalResourceQuotaSpec `json:"spec"`

	// +optional
	Status GlobalResourceQuotaStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true
type GlobalResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []GlobalResourceQuota `json:"items"`
}

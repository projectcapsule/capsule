// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// QuotaLedgerTargetRef identifies the quota object that owns this ledger.
// Namespace is optional for cluster-scoped targets such as GlobalCustomQuota.
type QuantityLedgerTargetRef struct {
	// APIGroup of the target quota resource, for example "capsule.clastix.io".
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Kind of the target quota resource, for example "CustomQuota" or "GlobalCustomQuota".
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Namespace of the target quota resource.
	// Must be empty for cluster-scoped targets.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the target quota resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// UID of the target quota resource.
	// Optional, but useful for stale reference detection.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}

// QuotaLedgerObjectRef identifies the object for which a reservation exists.
// UID may be empty for CREATE admission before the object is persisted.
type QuantityLedgerObjectRef struct {
	// APIGroup of the tracked object.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// APIVersion of the tracked object, for example "v1".
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`

	// Kind of the tracked object, for example "Pod".
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Namespace of the tracked object.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the tracked object.
	// +optional
	Name string `json:"name,omitempty"`

	// UID of the tracked object.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}

// QuotaLedgerSpec contains the immutable target reference.
type QuantityLedgerSpec struct {
	// TargetRef points to the quota object that this ledger belongs to.
	TargetRef QuantityLedgerTargetRef `json:"targetRef"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=quantityledgers,scope=Namespaced,shortName=ql
// +kubebuilder:printcolumn:name="TargetKind",type=string,JSONPath=`.spec.targetRef.kind`
// +kubebuilder:printcolumn:name="TargetNamespace",type=string,JSONPath=`.spec.targetRef.namespace`
// +kubebuilder:printcolumn:name="TargetName",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Reserved",type=string,JSONPath=`.status.reserved`
// +kubebuilder:printcolumn:name="Reservations",type=integer,JSONPath=`.status.reservations.size()`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type QuantityLedger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   QuantityLedgerSpec   `json:"spec,omitempty"`
	Status QuantityLedgerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type QuantityLedgerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []QuantityLedger `json:"items"`
}

func init() {
	SchemeBuilder.Register(&QuantityLedger{}, &QuantityLedgerList{})
}

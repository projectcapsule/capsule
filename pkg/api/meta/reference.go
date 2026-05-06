// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// NamespaceName must be a lowercase RFC1123 label.
// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
// +kubebuilder:validation:MaxLength=63
type RFC1123Name string

func (n RFC1123Name) String() string {
	return string(n)
}

// Name must be unique within a namespace.
// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
// +kubebuilder:validation:MaxLength=253
// +kubebuilder:object:generate=true
type RFC1123SubdomainName string

func (n RFC1123SubdomainName) String() string {
	return string(n)
}

// LocalObjectReference contains enough information to locate the referenced Kubernetes resource object.
// +kubebuilder:object:generate=true
type LocalRFC1123ObjectReference struct {
	// Name of the referent.
	// +required
	Name RFC1123Name `json:"name"`
}

// LocalObjectReference contains enough information to locate the referenced Kubernetes resource object.
// +kubebuilder:object:generate=true
type LocalObjectReference struct {
	// Name of the referent.
	// +required
	Name string `json:"name"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace.
// +kubebuilder:object:generate=true
type NamespacedRFC1123ObjectReference struct {
	// Name of the referent.
	// +required
	Name RFC1123Name `json:"name"`

	// Namespace of the referent, when not specified it acts as LocalObjectReference.
	// +optional
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace.
// +kubebuilder:object:generate=true
type NamespacedObjectReference struct {
	// Name of the referent.
	// +required
	Name string `json:"name"`

	// Namespace of the referent, when not specified it acts as LocalObjectReference.
	// +optional
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace.
// +kubebuilder:object:generate=true
type NamespacedObjectWithUIDReference struct {
	// UID of the tracked Tenant to pin point tracking
	// +required
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`

	// Name of the referent.
	// +required
	Name string `json:"name"`

	// Namespace of the referent, when not specified it acts as LocalObjectReference.
	// +optional
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace. But the namespace is required.
// +kubebuilder:object:generate=true
type NamespacedObjectReferenceWithNamespace struct {
	// Name of the referent.
	// +required
	Name string `json:"name"`

	// Namespace of the referent.
	// +required
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace. But the namespace is required.
// +kubebuilder:object:generate=true
type NamespacedRFC1123ObjectReferenceWithNamespace struct {
	// Name of the referent.
	// +required
	Name RFC1123Name `json:"name"`

	// Namespace of the referent.
	// +required
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

type LocalRFC1123ObjectReferenceWithUID struct {
	// UID of the tracked Tenant to pin point tracking
	// +required
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`

	// Name of the referent.
	// +required
	Name RFC1123Name `json:"name,omitempty"`
}

// NamespacedObjectReference contains enough information to locate the referenced Kubernetes resource object in any
// namespace. But the namespace is required.
// +kubebuilder:object:generate=true
type NamespacedRFC1123ObjectReferenceWithNamespaceWithUID struct {
	// UID of the tracked Tenant to pin point tracking
	// +required
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`

	// Name of the referent.
	// +required
	Name RFC1123Name `json:"name"`

	// Namespace of the referent.
	// +required
	Namespace RFC1123SubdomainName `json:"namespace,omitempty"`
}

// Advanced Status Item for pin pointing items in tenants/namespaces.
// +kubebuilder:object:generate=true
type ObjectReferenceStatus struct {
	gvk.ResourceID `json:",inline"`

	ObjectReferenceStatusCondition `json:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type ObjectReferenceStatusCondition struct {
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`

	// An opaque value that represents the internal version of this object that can
	// be used by clients to determine when objects have changed. May be used for optimistic
	// concurrency, change detection, and the watch operation on a resource or set of resources.
	// Clients must treat these values as opaque and passed unmodified back to the server.
	// They may only be valid for a particular resource or set of resources.
	//
	// Populated by the system.
	// Read-only.
	// Value must be treated as opaque by clients and .
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#concurrency-control-and-consistency
	// +optional
	LastApply metav1.Time `json:"lastApply,omitempty,omitzero" protobuf:"bytes,8,opt,name=lastApply"`

	// Indicates wether the resource was created or adopted
	Created bool `json:"created,omitempty"`
}

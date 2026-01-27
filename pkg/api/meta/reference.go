// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

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

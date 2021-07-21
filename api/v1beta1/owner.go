// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

type OwnerSpec struct {
	// Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"
	Kind OwnerKind `json:"kind"`
	// Name of tenant owner.
	Name string `json:"name"`
	// Proxy settings for tenant owner.
	ProxyOperations []ProxySettings `json:"proxySettings,omitempty"`
}

// +kubebuilder:validation:Enum=User;Group;ServiceAccount
type OwnerKind string

func (k OwnerKind) String() string {
	return string(k)
}

type ProxySettings struct {
	Kind       ProxyServiceKind `json:"kind"`
	Operations []ProxyOperation `json:"operations"`
}

// +kubebuilder:validation:Enum=List;Update;Delete
type ProxyOperation string

func (p ProxyOperation) String() string {
	return string(p)
}

// +kubebuilder:validation:Enum=Nodes;StorageClasses;IngressClasses;PriorityClasses
type ProxyServiceKind string

func (p ProxyServiceKind) String() string {
	return string(p)
}

const (
	NodesProxy           ProxyServiceKind = "Nodes"
	StorageClassesProxy  ProxyServiceKind = "StorageClasses"
	IngressClassesProxy  ProxyServiceKind = "IngressClasses"
	PriorityClassesProxy ProxyServiceKind = "PriorityClasses"

	ListOperation   ProxyOperation = "List"
	UpdateOperation ProxyOperation = "Update"
	DeleteOperation ProxyOperation = "Delete"

	UserOwner           OwnerKind = "User"
	GroupOwner          OwnerKind = "Group"
	ServiceAccountOwner OwnerKind = "ServiceAccount"
)

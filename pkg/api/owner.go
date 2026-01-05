// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

// +kubebuilder:object:generate=true

type OwnerSpec struct {
	CoreOwnerSpec `json:",inline"`

	// Proxy settings for tenant owner.
	ProxyOperations []ProxySettings `json:"proxySettings,omitempty"`
	// Additional Labels for the synchronized rolebindings
	Labels map[string]string `json:"labels,omitempty"`
	// Additional Annotations for the synchronized rolebindings
	Annotations map[string]string `json:"annotations,omitempty"`
}

// +kubebuilder:object:generate=true

type CoreOwnerSpec struct {
	UserSpec `json:",inline"`

	// Defines additional cluster-roles for the specific Owner.
	// +kubebuilder:default={admin,capsule-namespace-deleter}
	ClusterRoles []string `json:"clusterRoles,omitempty"`
}

func (o CoreOwnerSpec) ToAdditionalRolebindings() []AdditionalRoleBindingsSpec {
	bindings := make([]AdditionalRoleBindingsSpec, 0, len(o.ClusterRoles))

	for _, clusterRoleName := range o.ClusterRoles {
		bindings = append(bindings, AdditionalRoleBindingsSpec{
			ClusterRoleName: clusterRoleName,
			Subjects: []rbacv1.Subject{
				o.Subject(),
			},
		})
	}

	return bindings
}

// +kubebuilder:validation:Enum=User;Group;ServiceAccount
type OwnerKind string

func (k OwnerKind) String() string {
	return string(k)
}

// +kubebuilder:object:generate=true

type ProxySettings struct {
	Kind       ProxyServiceKind `json:"kind"`
	Operations []ProxyOperation `json:"operations"`
}

// +kubebuilder:validation:Enum=List;Update;Delete
type ProxyOperation string

func (p ProxyOperation) String() string {
	return string(p)
}

// +kubebuilder:validation:Enum=Nodes;StorageClasses;IngressClasses;PriorityClasses;RuntimeClasses;PersistentVolumes
type ProxyServiceKind string

func (p ProxyServiceKind) String() string {
	return string(p)
}

const (
	NodesProxy             ProxyServiceKind = "Nodes"
	StorageClassesProxy    ProxyServiceKind = "StorageClasses"
	IngressClassesProxy    ProxyServiceKind = "IngressClasses"
	PriorityClassesProxy   ProxyServiceKind = "PriorityClasses"
	RuntimeClassesProxy    ProxyServiceKind = "RuntimeClasses"
	PersistentVolumesProxy ProxyServiceKind = "PersistentVolumes"
	TenantProxy            ProxyServiceKind = "Tenant"

	ListOperation   ProxyOperation = "List"
	UpdateOperation ProxyOperation = "Update"
	DeleteOperation ProxyOperation = "Delete"

	UserOwner           OwnerKind = "User"
	GroupOwner          OwnerKind = "Group"
	ServiceAccountOwner OwnerKind = "ServiceAccount"
)

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import k8stypes "k8s.io/apimachinery/pkg/types"

// Name must be unique within a namespace. Is required when creating resources, although
// some resources may allow a client to request the generation of an appropriate name
// automatically. Name is primarily intended for creation idempotence and configuration
// definition.
// Cannot be updated.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names#names
// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
// +kubebuilder:validation:MaxLength=253
// +kubebuilder:object:generate=true
type Name string

func (n Name) String() string {
	return string(n)
}

type StatusNameUID struct {
	// UID of the tracked Tenant to pin point tracking
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`

	// Name
	Name Name `json:"name,omitempty"`
	// Namespace
	Namespace Name `json:"namespace,omitempty"`
}

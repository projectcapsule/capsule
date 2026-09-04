// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

type AccessEntityType string

const (
	AccessEntityTypeUser           AccessEntityType = "User"
	AccessEntityTypeGroup          AccessEntityType = "Group"
	AccessEntityTypeSystem         AccessEntityType = "System"
	AccessEntityTypeServiceAccount AccessEntityType = "ServiceAccount"
)

func (t AccessEntityType) String() string {
	return string(t)
}

// +kubebuilder:object:generate=true
type AccessEntity struct {
	// The name of the entity
	Name string `json:"name,omitempty"`
	// The type of the entity
	// +kubebuilder:validation:Enum=User;Group;System;ServiceAccount
	Type AccessEntityType `json:"type,omitempty"`
	// The groups the entity belongs to
	Groups []string `json:"groups,omitempty"`
}

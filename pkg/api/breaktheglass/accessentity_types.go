// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type AccessEntityType string

const (
	AccessEntityTypeUser   AccessEntityType = "User"
	AccessEntityTypeGroup  AccessEntityType = "Group"
	AccessEntityTypeSystem AccessEntityType = "System"
)

func (t AccessEntityType) String() string {
	return string(t)
}

type AccessEntity struct {
	// The name of the entity
	Name string `json:"name,omitempty"`
	// The type of the entity
	// +kubebuilder:validation:Enum=User;Group;System
	Type AccessEntityType `json:"type,omitempty"`
}

// BreakRequestStatus defines the observed state of BreakRequest.
type BreakRequestStatusConditionItem struct {
	metav1.Condition `json:",inline"`

	Reviewer AccessEntity `json:"reviewer,omitempty"`
}

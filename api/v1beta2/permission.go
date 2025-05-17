// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PermissionSpec struct {
	// Defines roleBindings between the ClusterRole and the Subjet in the Tenants namespaces.
	AllowedClusterBindings []metav1.LabelSelectorRequirement `json:"allowedClusterBindings,omitempty"`
}

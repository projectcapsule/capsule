// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type GlobalResourceQuotaStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// NamespaceSize is the number of selected namespaces.
	// +kubebuilder:default=0
	NamespaceSize uint `json:"namespaceCount,omitempty"`

	// Namespaces is the ordered set of selected namespace names.
	Namespaces []string `json:"namespaces,omitempty"`

	// Total contains aggregate quota usage across all selected namespaces.
	Total GlobalResourceQuotaUsage `json:"total,omitzero"`

	// NamespaceUsage contains observed quota usage per selected namespace.
	NamespaceUsage GlobalResourceQuotaNamespaceUsage `json:"namespaceUsage,omitempty"`

	// Conditions report reconciliation and admission readiness.
	Conditions meta.ConditionList `json:"conditions,omitzero"`
}

type GlobalResourceQuotaUsage struct {
	// Hard is the configured shared limit.
	Hard corev1.ResourceList `json:"hard,omitempty"`

	// Used is the usage observed across the relevant namespace set.
	Used corev1.ResourceList `json:"used,omitempty"`

	// Available is max(Hard-Used, 0).
	Available corev1.ResourceList `json:"available,omitempty"`
}

type GlobalResourceQuotaNamespaceUsage map[string]GlobalResourceQuotaNamespaceStatus

type GlobalResourceQuotaNamespaceStatus struct {
	// Used is the usage observed in this namespace.
	Used corev1.ResourceList `json:"used,omitempty"`
}

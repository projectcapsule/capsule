// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SchedulingOverwrite PodSchedulingAction = "overwrite"
	SchedulingValidate  PodSchedulingAction = "validate"
	SchedulingAggregate PodSchedulingAction = "aggregate"
)

// +kubebuilder:validation:Enum=overwrite;validate;aggregate
type PodSchedulingAction string

func (p PodSchedulingAction) String() string {
	return string(p)
}

// +kubebuilder:object:generate=true
type PodOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Pod resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Allow Specifying scheduling options for the pod
	Scheduling []PodSchedulingOptions `json:"scheduling,omitempty"`
}

// +kubebuilder:object:generate=true
type PodSchedulingOptions struct {
	// Specify Action for defined Scheduling options
	//+kubebuilder:default=overwrite
	Action PodSchedulingAction `json:"action"`
	// Specify Selector for selecting the pods
	Selector PodSchedulingSelector `json:"selector,omitempty"`
	// Allow Specifying Nodeselectors for the pod
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// Allow Specifying Tolerations for the pod
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Allow Specifying TopologySpreadConstraints for the pod
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// Allow Specifying NodeSelector for the pod (directly applied to the pod)
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// +kubebuilder:object:generate=true
type PodSchedulingSelector struct {
	// Specify Selector for selecting the pods
	PodSelector metav1.LabelSelector `json:"podSelector,omitempty"`
	// Specify NamespaceSelector for selecting the namespaces
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

func (p PodSchedulingOptions) IsSelected(target *corev1.Pod) bool {
	if p.Selector.PodSelector.Size() != 0 && target.Labels != nil {
		return false
	}

	return true
}

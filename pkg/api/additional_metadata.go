// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:generate=true

type AdditionalMetadataSpec struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// +kubebuilder:object:generate=true

type AdditionalMetadataSelectorSpec struct {
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	Labels            map[string]string     `json:"labels,omitempty"`
	Annotations       map[string]string     `json:"annotations,omitempty"`
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type TenantResourceHealthChecks struct {
	// Wait requires successfully applied targets to pass health checks before Ready.
	// Omitted or false preserves apply-only readiness and performs no health reads.
	// +optional
	Wait bool `json:"wait,omitempty"`
	// Timeout bounds an incomplete rollout across retries and leader changes.
	// Defaults to five minutes. A completed rollout can start a new assessment.
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('1h')",message="healthChecks.timeout must be positive and at most one hour"
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// Expressions override kstatus for the specified API groups and kinds.
	// Expressions receive object and its top-level fields (metadata, spec, status).
	// Evaluated in order: inProgress, failed, current. Requires wait: true.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	Expressions []TenantResourceHealthCheckExpression `json:"expressions,omitempty"`
}

// Enabled reports whether readiness waits for healthy applied targets.
func (h *TenantResourceHealthChecks) Enabled() bool {
	return h != nil && h.Wait
}

type TenantResourceHealthCheckExpression struct {
	// APIVersion identifies the API group; the served version is ignored when matching.
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`
	// Kind identifies the kind whose status is evaluated.
	// +kubebuilder:validation:Pattern=^[A-Za-z][A-Za-z0-9]*$
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`
	// Current returns true when the target is healthy.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Current string `json:"current"`
	// InProgress returns true while reconciliation is ongoing.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	InProgress string `json:"inProgress,omitempty"`
	// Failed returns true on a failed rollout.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	Failed string `json:"failed,omitempty"`
}

// TenantResourceHealthStatus is a checkpoint, not an additional target inventory.
// Target identities remain in processedItems. Only the first diagnostic is retained.
type TenantResourceHealthStatus struct {
	Revision           string           `json:"revision"`
	ObservedGeneration int64            `json:"observedGeneration"`
	StartedAt          metav1.Time      `json:"startedAt"`
	Deadline           metav1.Time      `json:"deadline"`
	RequestedAt        metav1.MicroTime `json:"requestedAt"`
	// +optional
	NextCheckAt metav1.Time `json:"nextCheckAt,omitzero"`
	// Applying prevents observations from completing while a full apply is in flight.
	// +optional
	Applying bool `json:"applying,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	Cursor int32 `json:"cursor,omitempty"`
	// +optional
	Pending bool `json:"pending,omitempty"`
	// +optional
	Failed bool `json:"failed,omitempty"`
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	Message string `json:"message,omitempty"`
}

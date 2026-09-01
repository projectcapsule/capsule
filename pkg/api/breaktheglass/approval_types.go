// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import "github.com/projectcapsule/capsule/pkg/api/rbac"

// +kubebuilder:object:generate=true
type ApprovalSpec struct {
	// Auto automatically approves matching requests. Approvers are ignored for
	// automatic approvals.
	// +optional
	Auto bool `json:"auto,omitempty"`

	// Approvers lists the subjects permitted to approve a request manually.
	// When omitted, any subject with permission to update the request status may
	// approve it.
	// +optional
	Approvers rbac.UserListSpec `json:"approvers,omitempty"`

	// Conditions contains CEL expressions evaluated as an OR list. At least one
	// expression must evaluate to true. When omitted, approval is unconditional.
	// +kubebuilder:validation:items:MinLength=1
	// +optional
	Conditions []string `json:"conditions,omitempty"`
}

// IsApprover reports whether an authenticated subject may manually approve a
// request. An empty approver list deliberately permits any authenticated
// subject that already has Kubernetes authorization to update the request.
func (a ApprovalSpec) IsApprover(name string, groups []string) bool {
	return len(a.Approvers) == 0 || a.Approvers.IsPresent(name, groups)
}

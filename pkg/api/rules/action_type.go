// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

const (
	ActionTypeAllow ActionType = "allow"
	ActionTypeDeny  ActionType = "deny"
	ActionTypeAudit ActionType = "audit"
)

// +kubebuilder:validation:Enum=allow;deny;audit
type ActionType string

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import "slices"

func (a ActionType) OrDefault() ActionType {
	if a == "" {
		return ActionTypeDeny
	}

	return a
}

func (e NamespaceRuleEnforceBody) GetWorkloadTargets(target WorkloadValidationTarget) bool {
	if len(e.Workloads.Targets) == 0 {
		return true
	}

	return slices.Contains(e.Workloads.Targets, target)
}

func (e NamespaceRuleEnforceBody) WorkloadTargetsAny(targets ...WorkloadValidationTarget) bool {
	if len(e.Workloads.Targets) == 0 {
		return true
	}

	return slices.ContainsFunc(targets, e.GetWorkloadTargets)
}

// GetStorageClassTargets reports whether the storage classes of this rule apply
// to target. Omitted targets select every supported kind.
func (e NamespaceRuleEnforceBody) GetStorageClassTargets(target StorageValidationTarget) bool {
	if len(e.Storage.Classes) == 0 {
		return false
	}

	if len(e.Storage.Targets) == 0 {
		return true
	}

	return slices.Contains(e.Storage.Targets, target)
}

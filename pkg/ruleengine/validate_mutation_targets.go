// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"slices"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateMutationTargets(path string, workload rules.WorkloadMutation, podProperties bool) error {
	if len(workload.Targets) > 4 {
		return fmt.Errorf("%s.targets: at most 4 targets are supported", path)
	}

	for i, target := range workload.Targets {
		switch target { //nolint:exhaustive // Mutations support only compatible Pod locations.
		case rules.ValidatePod, rules.ValidateContainers, rules.ValidateInitContainers, rules.ValidateEphemeralContainers:
		default:
			return fmt.Errorf("%s.targets[%d]: unsupported workload mutation target %q", path, i, target)
		}

		if slices.Contains(workload.Targets[:i], target) {
			return fmt.Errorf("%s.targets[%d]: duplicate target %q", path, i, target)
		}
	}

	if podProperties && !workload.GetWorkloadTargets(rules.ValidatePod) {
		return fmt.Errorf("%s.targets: Pod-level mutation properties require the pod target", path)
	}

	return nil
}

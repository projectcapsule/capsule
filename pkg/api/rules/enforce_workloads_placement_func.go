// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"fmt"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// VisitPlacementExpressions shares expression traversal between rule validation
// and the existing regex-cache invalidator. Paths are relative to workloads.placement.
func (w NamespaceRuleEnforceWorkloadsBody) VisitPlacementExpressions(visit func(string, *runtime.ExpressionMatch) error) error {
	pair := func(path string, match WorkloadNodeSelectorMatch) error {
		if err := visit(path+".key", (*runtime.ExpressionMatch)(match.Key)); err != nil {
			return err
		}

		return visit(path+".values", (*runtime.ExpressionMatch)(match.Values))
	}
	requirements := func(path string, values []PlacementRequirementMatch) error {
		for i, value := range values {
			if err := pair(fmt.Sprintf("%s[%d]", path, i), value.WorkloadNodeSelectorMatch); err != nil {
				return err
			}
		}

		return nil
	}

	selector := func(path string, value *PlacementLabelSelectorMatch) error {
		if value == nil {
			return nil
		}

		return requirements(path+".requirements", value.Requirements)
	}

	for i, value := range w.Placement.NodeSelector {
		if err := pair(fmt.Sprintf("nodeSelector[%d]", i), value); err != nil {
			return err
		}
	}

	for i, value := range w.Placement.Tolerations {
		if err := pair(fmt.Sprintf("tolerations[%d]", i), value.WorkloadNodeSelectorMatch); err != nil {
			return err
		}
	}

	for i, value := range w.Placement.TopologySpreadConstraints {
		path := fmt.Sprintf("topologySpreadConstraints[%d]", i)
		if err := visit(path+".topologyKey", (*runtime.ExpressionMatch)(value.TopologyKey)); err != nil {
			return err
		}

		if err := selector(path+".labelSelector", value.LabelSelector); err != nil {
			return err
		}
	}

	for i, value := range w.Placement.Affinity {
		path := fmt.Sprintf("affinity[%d]", i)
		if err := visit(path+".topologyKey", (*runtime.ExpressionMatch)(value.TopologyKey)); err != nil {
			return err
		}

		if err := visit(path+".namespaces", (*runtime.ExpressionMatch)(value.Namespaces)); err != nil {
			return err
		}

		if err := requirements(path+".requirements", value.Requirements); err != nil {
			return err
		}

		if err := requirements(path+".fieldRequirements", value.FieldRequirements); err != nil {
			return err
		}

		if err := selector(path+".labelSelector", value.LabelSelector); err != nil {
			return err
		}

		if err := selector(path+".namespaceSelector", value.NamespaceSelector); err != nil {
			return err
		}
	}

	return nil
}

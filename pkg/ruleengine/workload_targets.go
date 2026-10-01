// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"slices"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// WorkloadEnforcement selects property policies for the admitted kind. It
// preserves order and never changes rules owned by the cache. The ordinary
// Pod-only path returns the input without copying it.
func WorkloadEnforcement(bodies []*rules.NamespaceRuleEnforceBody, gvk schema.GroupVersionKind) []*rules.NamespaceRuleEnforceBody {
	var out []*rules.NamespaceRuleEnforceBody

	changed := false
	pod := gvk.Group == "" && gvk.Kind == "Pod"

	for i, body := range bodies {
		selected := body

		if body != nil && (!pod || len(body.Workloads.Targets) > 0) {
			targets, matches := body.Workloads.PodTargets(gvk)
			if !matches || body.Workloads.TargetsOnly() || (gvk.Kind != "Pod" && !body.Workloads.HasPolicies()) {
				selected = nil
			} else if !slices.Equal(targets, body.Workloads.Targets) {
				projected := *body
				projected.Workloads.Targets = targets
				selected = &projected
			}
		}

		if !changed && selected != body {
			out = make([]*rules.NamespaceRuleEnforceBody, 0, len(bodies))
			out = append(out, bodies[:i]...)
			changed = true
		}

		if changed && selected != nil {
			out = append(out, selected)
		}
	}

	if !changed {
		return bodies
	}

	return out
}

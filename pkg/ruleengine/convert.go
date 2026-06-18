// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import api "github.com/projectcapsule/capsule/pkg/api/rules"

func EnforceBodiesFromNamespaceRules(
	bodies []*api.NamespaceRuleBodyNamespace,
) []*api.NamespaceRuleEnforceBody {
	if len(bodies) == 0 {
		return nil
	}

	out := make([]*api.NamespaceRuleEnforceBody, 0, len(bodies))

	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}

		out = append(out, body.Enforce)
	}

	return out
}

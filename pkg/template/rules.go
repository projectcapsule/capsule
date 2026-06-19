// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"

	"go.yaml.in/yaml/v2"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func RenderNamespaceRuleBodies(
	context map[string]any,
	key MissingKeyOption,
	bodies []*rules.NamespaceRuleBodyNamespace,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	if len(bodies) == 0 {
		return nil, nil
	}

	raw, err := yaml.Marshal(bodies)
	if err != nil {
		return nil, fmt.Errorf("marshal namespace rule bodies: %w", err)
	}

	rendered, err := RenderTemplateBytes(context, key, raw)
	if err != nil {
		return nil, fmt.Errorf("render namespace rule bodies template: %w", err)
	}

	var out []*rules.NamespaceRuleBodyNamespace
	if err := yaml.Unmarshal(rendered, &out); err != nil {
		return nil, fmt.Errorf(
			"unmarshal rendered namespace rule bodies: %w\nrendered template:\n%s",
			err,
			withLineNumbers(string(rendered)),
		)
	}

	return out, nil
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apiserver/pkg/cel/environment"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateRuleConditions(index int, body *rules.NamespaceRuleBodyNamespace, compilers []ConditionCompiler) error {
	return body.VisitConditions(func(location string, conditions []rules.AdmissionCondition) error {
		path := fmt.Sprintf("rules[%d].%s", index, location)
		if len(conditions) > 64 {
			return fmt.Errorf("%s: at most 64 conditions are supported", path)
		}

		names := make(map[string]struct{}, len(conditions))

		for i, condition := range conditions {
			if condition.Name != "" {
				if errs := validation.IsDNS1123Label(condition.Name); len(errs) > 0 {
					return fmt.Errorf("%s[%d].name: %s", path, i, strings.Join(errs, "; "))
				}

				if _, found := names[condition.Name]; found {
					return fmt.Errorf("%s[%d].name: duplicate condition name %q", path, i, condition.Name)
				}

				names[condition.Name] = struct{}{}
			}
			// Templated expressions are checked again after namespace rendering,
			// when the effective RuleStatus is admitted.
			if strings.Contains(condition.Expression, "{{") {
				continue
			}

			if len(compilers) == 0 || compilers[0] == nil {
				return fmt.Errorf("%s[%d]: admission condition compiler is unavailable", path, i)
			}

			if _, err := compilers[0].GetOrCompileCondition(condition.Expression, environment.NewExpressions); err != nil {
				return fmt.Errorf("%s[%d] (%q): %w", path, i, condition.Name, err)
			}
		}

		return nil
	})
}

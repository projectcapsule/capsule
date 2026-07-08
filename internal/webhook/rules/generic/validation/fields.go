// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

const fieldSetName = "field"

func (h *genericRules) validateFields(
	obj genericObject,
	gvk schema.GroupVersionKind,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if obj == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	paths := fieldRulePaths(gvk, enforceBodies)
	if len(paths) == 0 {
		return nil, nil
	}

	out := &ruleengine.Evaluation{}

	for _, path := range paths {
		compiled, err := h.jsonPathCache.GetOrCompile(path)
		if err != nil {
			return out, fmt.Errorf("field rule path %q is invalid: %w", path, err)
		}

		// Rules constrain values that exist: paths resolving to nothing and
		// empty values are skipped by the engine, so they leave the object
		// untouched by this rule.
		//
		// Violations are reported against the configured path itself: array
		// expansions stay "[*]", they are not resolved to concrete indexes.
		scalars := compiled.FindScalars(obj.UnstructuredContent())

		values := make([]ruleengine.Value, 0, len(scalars))
		for _, scalar := range scalars {
			values = append(values, ruleengine.Value{Value: scalar, Path: path})
		}

		evaluation, err := evaluateGenericRules(
			obj,
			enforceBodies,
			h.fieldSet(gvk, path, values),
		)
		if err != nil {
			return out, err
		}

		out.Append(evaluation)

		//nolint:nilerr
		if evaluation != nil && evaluation.BlockingError() != nil {
			return out, nil
		}
	}

	return out, nil
}

func (h *genericRules) fieldSet(
	gvk schema.GroupVersionKind,
	path string,
	values []ruleengine.Value,
) genericRuleSet[runtime.ExpressionMatch] {
	return genericRuleSet[runtime.ExpressionMatch]{
		Name:        fieldSetName,
		EventReason: events.ReasonForbiddenField,

		Values: func(_ genericObject) []ruleengine.Value {
			return values
		},

		Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []runtime.ExpressionMatch {
			if enforce == nil || len(enforce.Fields) == 0 {
				return nil
			}

			var out []runtime.ExpressionMatch

			for i := range enforce.Fields {
				rule := enforce.Fields[i]
				if rule.Path != path || !rule.MatchesGroupVersionKind(gvk) {
					continue
				}

				out = append(out, rule.Match...)
			}

			return out
		},
		Matches: func(match runtime.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
			matched, err := match.MatchesWithExpressionMatcher(h.regexCache, value.Value)
			if err != nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{
				Matched: matched,
			}, nil
		},
		RuleDescription:    runtime.DescribeExpressionMatch,
		AllowedDescription: "Allowed field values",
	}
}

func fieldRulePaths(
	gvk schema.GroupVersionKind,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) []string {
	seen := make(map[string]struct{})

	for _, enforce := range enforceBodies {
		if enforce == nil {
			continue
		}

		for i := range enforce.Fields {
			rule := enforce.Fields[i]
			if !rule.MatchesGroupVersionKind(gvk) {
				continue
			}

			seen[rule.Path] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}

	sort.Strings(out)

	return out
}

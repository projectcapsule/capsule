// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

type Value struct {
	Value string
	Path  string
}

type Set[R any, T any] struct {
	Name string

	// Values extracts the values from the admitted object.
	Values func(T) []Value

	// Rules extracts rule entries from a Tenant rule.
	Rules func(*rules.NamespaceRuleBodyTenant) []R

	// Matches checks whether a rule entry matches an extracted value.
	Matches func(R, Value) (bool, error)
}

func Evaluate[R any, T any](
	obj T,
	tenantRules []*rules.NamespaceRuleBodyTenant,
	set Set[R, T],
) error {
	if set.Values == nil {
		return fmt.Errorf("%s: values extractor is nil", set.Name)
	}

	if set.Rules == nil {
		return fmt.Errorf("%s: rules extractor is nil", set.Name)
	}

	if set.Matches == nil {
		return fmt.Errorf("%s: matcher is nil", set.Name)
	}

	values := set.Values(obj)
	if len(values) == 0 {
		return nil
	}

	for _, value := range values {
		if value.Value == "" {
			continue
		}

		hasAllowRule := false
		allowedByRule := false

		for _, rule := range tenantRules {
			if rule == nil || rule.Enforce == nil {
				continue
			}

			items := set.Rules(rule)
			if len(items) == 0 {
				continue
			}

			switch rule.Enforce.Action {
			case rules.ActionTypeAllow:
				hasAllowRule = true

			case rules.ActionTypeDeny, rules.ActionTypeAudit:
				// Supported actions.

			default:
				return fmt.Errorf(
					"%s: unsupported rule action %q",
					set.Name,
					rule.Enforce.Action,
				)
			}

			for _, item := range items {
				matched, err := set.Matches(item, value)
				if err != nil {
					return fmt.Errorf("%s: invalid rule: %w", set.Name, err)
				}

				if !matched {
					continue
				}

				switch rule.Enforce.Action {
				case rules.ActionTypeDeny:
					return fmt.Errorf(
						"%s %q at %s is denied by tenant rule",
						set.Name,
						value.Value,
						value.Path,
					)

				case rules.ActionTypeAllow:
					allowedByRule = true

				case rules.ActionTypeAudit:
					// Non-blocking. Existing audit hooks can be added here later.
					continue
				}
			}
		}

		if hasAllowRule && !allowedByRule {
			return fmt.Errorf(
				"%s %q at %s is not allowed by tenant rule",
				set.Name,
				value.Value,
				value.Path,
			)
		}
	}

	return nil
}

func EvaluateTenantRules[R any, T any](
	obj T,
	tenant *capsulev1beta2.Tenant,
	set Set[R, T],
) error {
	if tenant == nil {
		return nil
	}

	return Evaluate[R, T](obj, tenant.Spec.Rules, set)
}

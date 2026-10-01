// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateSecurityProfileRules(index int, workloads rules.NamespaceRuleEnforceWorkloadsBody) error {
	for _, group := range []struct {
		name    string
		matches []rules.WorkloadSecurityProfileMatch
	}{
		{"seccompProfiles", workloads.SeccompProfiles}, {"appArmorProfiles", workloads.AppArmorProfiles},
	} {
		for i, match := range group.matches {
			fieldPath := fmt.Sprintf("rules[%d].enforce.workloads.%s[%d]", index, group.name, i)
			if len(match.Types) == 0 {
				return fmt.Errorf("%s.types: at least one type is required", fieldPath)
			}

			if err := placementEnum(fieldPath+".types", match.Types, rules.SecurityProfileRuntimeDefault, rules.SecurityProfileLocalhost, rules.SecurityProfileUnconfined); err != nil {
				return err
			}

			if len(match.LocalhostProfiles) > 0 && !slices.Contains(match.Types, rules.SecurityProfileLocalhost) {
				return fmt.Errorf("%s.localhostProfiles requires Localhost in types", fieldPath)
			}

			for j, expression := range match.LocalhostProfiles {
				p := fmt.Sprintf("%s.localhostProfiles[%d]", fieldPath, j)
				if len(expression.Exact) == 0 && strings.TrimSpace(expression.Expression) == "" {
					return fmt.Errorf("%s: exact or exp is required", p)
				}

				for _, name := range expression.Exact {
					if strings.TrimSpace(name) == "" {
						return fmt.Errorf("%s.exact: profile names must not be empty", p)
					}
				}

				if err := validateExpressionMatch(expression, p); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateSecurityProfileMutation(fieldPath string, workload rules.WorkloadMutation) error {
	if p := workload.SeccompProfile; p != nil {
		if err := validateNativeSecurityProfile(fieldPath+".seccompProfile", string(p.Type), p.LocalhostProfile, true); err != nil {
			return err
		}
	}

	if p := workload.AppArmorProfile; p != nil {
		if err := validateNativeSecurityProfile(fieldPath+".appArmorProfile", string(p.Type), p.LocalhostProfile, false); err != nil {
			return err
		}
	}

	return nil
}

func validateNativeSecurityProfile(fieldPath, kind string, localhost *string, seccomp bool) error {
	if err := placementEnum(fieldPath+".type", []rules.SecurityProfileType{rules.SecurityProfileType(kind)}, rules.SecurityProfileRuntimeDefault, rules.SecurityProfileLocalhost, rules.SecurityProfileUnconfined); err != nil {
		return err
	}

	if kind != string(rules.SecurityProfileLocalhost) {
		if localhost != nil {
			return fmt.Errorf("%s.localhostProfile requires type Localhost", fieldPath)
		}

		return nil
	}

	if localhost == nil || strings.TrimSpace(*localhost) == "" || strings.ContainsRune(*localhost, '\x00') {
		return fmt.Errorf("%s.localhostProfile: a non-empty profile is required", fieldPath)
	}
	// Validate templated paths after rendering as well as the original Tenant.
	if seccomp && !strings.Contains(*localhost, "{{") && (path.IsAbs(*localhost) || slices.Contains(strings.Split(*localhost, "/"), "..")) {
		return fmt.Errorf("%s.localhostProfile: path must be relative and must not contain '..'", fieldPath)
	}

	return nil
}

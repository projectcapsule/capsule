// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateDeviceRules(index int, devices rules.NamespaceRuleEnforceDevicesBody) error {
	if len(devices.Classes) > 64 {
		return fmt.Errorf("rules[%d].enforce.devices.classes: at most 64 matches are supported", index)
	}

	for i, match := range devices.Classes {
		path := fmt.Sprintf("rules[%d].enforce.devices.classes[%d]", index, i)

		hasName := len(match.Exact) > 0 || match.Expression != ""

		if !hasName && (match.Selector == nil || match.Negate) {
			return fmt.Errorf("%s: exact or exp is required unless a non-negated selector is supplied", path)
		}

		if err := validateExpressionMatch(match.NameMatch(), path); err != nil {
			return err
		}

		for _, name := range match.Exact {
			if strings.Contains(name, "{{") {
				continue
			}

			if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
				return fmt.Errorf("%s.exact: %s", path, strings.Join(errs, "; "))
			}
		}

		if match.Expression != "" && strings.TrimSpace(match.Expression) == "" {
			return fmt.Errorf("%s.exp must not be blank", path)
		}

		if match.Selector != nil {
			if err := validateNativePlacementSelector(path+".selector", match.Selector); err != nil {
				return err
			}
		}
	}

	return nil
}

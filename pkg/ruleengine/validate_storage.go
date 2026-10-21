// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateStorageRules(index int, storage rules.NamespaceRuleEnforceStorageBody) error {
	if len(storage.Volumes) > 64 {
		return fmt.Errorf("rules[%d].enforce.storage.volumes: at most 64 matches are supported", index)
	}

	names := make(map[string]struct{}, len(storage.Volumes))

	for i, volume := range storage.Volumes {
		path := fmt.Sprintf("rules[%d].enforce.storage.volumes[%d]", index, i)

		if volume.Name != "" {
			if errs := validation.IsDNS1123Label(volume.Name); len(errs) > 0 {
				return fmt.Errorf("%s.name: %s", path, strings.Join(errs, "; "))
			}

			if _, exists := names[volume.Name]; exists {
				return fmt.Errorf("%s.name: duplicate match name %q", path, volume.Name)
			}

			names[volume.Name] = struct{}{}
		}

		if volume.Selector == nil {
			return fmt.Errorf("%s.selector is required; use {} to explicitly match all volumes", path)
		}

		if err := validateNativePlacementSelector(path+".selector", volume.Selector); err != nil {
			return err
		}
	}

	return validateStorageClassRules(index, storage)
}

func validateStorageClassRules(index int, storage rules.NamespaceRuleEnforceStorageBody) error {
	path := fmt.Sprintf("rules[%d].enforce.storage", index)

	targets := make(map[rules.StorageValidationTarget]struct{}, len(storage.Targets))

	for i, target := range storage.Targets {
		switch target {
		case rules.StorageTargetVolumeSnapshot:
		default:
			return fmt.Errorf("%s.targets[%d] %q is invalid: unsupported storage target", path, i, target)
		}

		if _, exists := targets[target]; exists {
			return fmt.Errorf("%s.targets[%d] %q is invalid: duplicate target", path, i, target)
		}

		targets[target] = struct{}{}
	}

	if len(storage.Targets) > 0 && len(storage.Classes) == 0 {
		return fmt.Errorf("%s.classes is invalid: classes must be configured when targets are configured", path)
	}

	if len(storage.Classes) > 64 {
		return fmt.Errorf("%s.classes: at most 64 matches are supported", path)
	}

	for i, class := range storage.Classes {
		classPath := fmt.Sprintf("%s.classes[%d]", path, i)

		if !class.HasNameMatch() && !class.HasSelector() {
			return fmt.Errorf("%s is invalid: at least one of exact, exp, matchLabels or matchExpressions must be set", classPath)
		}

		if err := validateExpressionMatch(class.NameMatch(), classPath); err != nil {
			return err
		}

		if err := validateNativePlacementSelector(classPath, &class.LabelSelector); err != nil {
			return err
		}
	}

	return nil
}

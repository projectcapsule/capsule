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

	return nil
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateDeviceMutation(path string, mutation rules.NamespaceRuleMutation) error {
	name := mutation.Devices.Class
	if name == "" {
		return nil
	}

	if mutation.Action == rules.MutationActionReplace {
		return fmt.Errorf("%s: DeviceClass defaults support merge only; replacing class names can invalidate scheduler-selected allocations", path)
	}

	if utf8.RuneCountInString(name) > 4096 {
		return fmt.Errorf("%s: at most 4096 characters are supported", path)
	}

	if !strings.Contains(name, "{{") {
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("%s: %s", path, strings.Join(errs, "; "))
		}
	}

	return nil
}

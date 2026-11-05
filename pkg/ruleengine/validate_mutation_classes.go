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

func validateClassMutation(path string, classes rules.WorkloadClassMutation) error {
	name := classes.Priority
	if name == "" {
		return nil
	}

	if utf8.RuneCountInString(name) > 4096 {
		return fmt.Errorf("%s.priority: at most 4096 characters are supported", path)
	}

	if !strings.Contains(name, "{{") {
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("%s.priority: invalid priority class name %q: %s", path, name, strings.Join(errs, "; "))
		}
	}

	return nil
}

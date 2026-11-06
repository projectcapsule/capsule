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

// PriorityClass and RuntimeClass names are both DNS subdomains.
func validateClassMutation(path string, classes rules.WorkloadClassMutation) error {
	for _, class := range []struct{ field, name string }{
		{"priority", classes.Priority},
		{"runtime", classes.Runtime},
	} {
		if class.name == "" {
			continue
		}

		if utf8.RuneCountInString(class.name) > 4096 {
			return fmt.Errorf("%s.%s: at most 4096 characters are supported", path, class.field)
		}

		if !strings.Contains(class.name, "{{") {
			if errs := validation.IsDNS1123Subdomain(class.name); len(errs) > 0 {
				return fmt.Errorf("%s.%s: invalid %s class name %q: %s", path, class.field, class.field, class.name, strings.Join(errs, "; "))
			}
		}
	}

	return nil
}

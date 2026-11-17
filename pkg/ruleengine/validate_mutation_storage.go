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

// validateStorageMutation checks the class name a rule sets. Templates are
// checked once they are rendered.
func validateStorageMutation(path string, storage rules.StorageMutation) error {
	name := storage.AttributeClass
	if name == "" {
		return nil
	}

	if utf8.RuneCountInString(name) > 4096 {
		return fmt.Errorf("%s: at most 4096 characters are supported", path)
	}

	if !strings.Contains(name, "{{") {
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("%s: invalid volume attributes class name %q: %s", path, name, strings.Join(errs, "; "))
		}
	}

	return nil
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateIngressMutation(path string, ingress rules.IngressMutation) error {
	ref := ingress.ListenerSets.ParentRef
	if ref == nil {
		return nil
	}

	path += ".listenerSets.parentRef"

	for _, field := range []struct {
		name     string
		value    *string
		validate func(string) []string
	}{
		{"name", &ref.Name, validation.IsDNS1123Subdomain},
		{"namespace", ref.Namespace, validation.IsDNS1123Label},
		{"group", ref.Group, func(value string) []string {
			if value == "" {
				return nil
			}

			return validation.IsDNS1123Subdomain(value)
		}},
		{"kind", ref.Kind, func(value string) []string { return validation.IsDNS1035Label(strings.ToLower(value)) }},
	} {
		if field.value == nil || strings.Contains(*field.value, "{{") {
			continue
		}

		if errs := field.validate(*field.value); len(errs) > 0 {
			return fmt.Errorf("%s.%s: invalid value %q: %s", path, field.name, *field.value, strings.Join(errs, "; "))
		}
	}

	return nil
}

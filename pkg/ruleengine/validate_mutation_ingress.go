// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateIngressMutation(path string, ingress rules.IngressMutation) error {
	if err := validateIngressClassMutation(path, ingress); err != nil {
		return err
	}

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

func validateIngressClassMutation(path string, ingress rules.IngressMutation) error {
	if ingress.Class == "" && len(ingress.Types) == 0 {
		return nil
	}

	if ingress.Class == "" || len(ingress.Types) == 0 {
		return fmt.Errorf("%s: types and class must be configured together", path)
	}

	if len(ingress.Types) > 2 {
		return fmt.Errorf("%s.types: at most two types are supported", path)
	}

	for i, kind := range ingress.Types {
		if kind != rules.IngressTypeIngress && kind != rules.IngressTypeGateway {
			return fmt.Errorf("%s.types[%d]: unsupported class mutation type %q", path, i, kind)
		}

		if slices.Contains(ingress.Types[:i], kind) {
			return fmt.Errorf("%s.types[%d]: duplicate type %q", path, i, kind)
		}
	}

	if utf8.RuneCountInString(ingress.Class) > 4096 {
		return fmt.Errorf("%s.class: at most 4096 characters are supported", path)
	}

	if !strings.Contains(ingress.Class, "{{") {
		if errs := validation.IsDNS1123Subdomain(ingress.Class); len(errs) > 0 {
			return fmt.Errorf("%s.class: invalid class name %q: %s", path, ingress.Class, strings.Join(errs, "; "))
		}
	}

	return nil
}

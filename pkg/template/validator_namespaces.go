// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
)

type NamespaceValidator func(namespace string) error

func NewNamespaceValidator(allowCrossNamespaceSelection bool, allowed sets.Set[string]) NamespaceValidator {
	return func(namespace string) error {
		if namespace == "" {
			return nil
		}

		if allowCrossNamespaceSelection {
			return nil
		}

		if allowed.Has(namespace) {
			return nil
		}

		return fmt.Errorf(
			"cross-namespace selection is not allowed. Referring a Namespace (%s) that is not part of the allowed namespaces %v",
			namespace,
			allowed.UnsortedList(),
		)
	}
}

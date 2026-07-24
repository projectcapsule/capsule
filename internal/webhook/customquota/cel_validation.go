// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"fmt"

	"k8s.io/apiserver/pkg/cel/environment"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
)

func validateCELExpressions(
	celCache *cache.CELCache,
	sources []capsulev1beta2.CustomQuotaSpecSource,
) error {
	for sourceIndex, source := range sources {
		if source.CEL != "" {
			if _, err := celCache.GetOrCompileQuantity(source.CEL, environment.NewExpressions); err != nil {
				return fmt.Errorf("spec.sources[%d].cel: %w", sourceIndex, err)
			}
		}

		for selectorIndex, selector := range source.Selectors {
			for expressionIndex, expression := range selector.CELExpressions {
				if _, err := celCache.GetOrCompileBoolean(expression, environment.NewExpressions); err != nil {
					return fmt.Errorf(
						"spec.sources[%d].selectors[%d].celExpressions[%d]: %w",
						sourceIndex,
						selectorIndex,
						expressionIndex,
						err,
					)
				}
			}
		}
	}

	return nil
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func (r *CacheInvalidator) rebuildJSONPathCache(ctx context.Context, log logr.Logger) error {
	customQuotas := &capsulev1beta2.CustomQuotaList{}
	if err := r.List(ctx, customQuotas); err != nil {
		return err
	}

	globalCustomQuotas := &capsulev1beta2.GlobalCustomQuotaList{}
	if err := r.List(ctx, globalCustomQuotas); err != nil {
		return err
	}

	log.V(5).Info("rebuilding custom quota jsonpath cache",
		"jsonPathsBefore", r.JSONPathCache.Stats(),
		"customQuotas", len(customQuotas.Items),
		"globalCustomQuotas", len(globalCustomQuotas.Items),
	)

	r.JSONPathCache.Reset()

	expressions := make(map[string]struct{})

	for _, cq := range customQuotas.Items {
		collectJSONPaths(expressions, cq.Spec.Sources)
	}

	for _, gcq := range globalCustomQuotas.Items {
		collectJSONPaths(expressions, gcq.Spec.Sources)
	}

	for expr := range expressions {
		if expr == "" {
			continue
		}

		if _, err := r.JSONPathCache.GetOrCompile(expr); err != nil {
			return fmt.Errorf("build JSONPath cache entry %q: %w", expr, err)
		}
	}

	log.V(5).Info("rebuilt custom quota jsonpath cache",
		"uniqueExpressions", len(expressions),
		"jsonPathsAfter", r.JSONPathCache.Stats(),
	)

	return nil
}

func collectJSONPaths(
	set map[string]struct{},
	sources []capsulev1beta2.CustomQuotaSpecSource,
) {
	for _, source := range sources {
		if source.Path != "" {
			set[source.Path] = struct{}{}
		}

		for _, sel := range source.Selectors {
			for _, fs := range sel.FieldSelectors {
				if fs != "" {
					set[fs] = struct{}{}
				}
			}
		}
	}
}

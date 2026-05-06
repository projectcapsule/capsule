// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/customquotas"
)

func (r *CacheInvalidator) rebuildTargetsCache(ctx context.Context, log logr.Logger) error {
	customQuotas := &capsulev1beta2.CustomQuotaList{}
	if err := r.Client.List(ctx, customQuotas); err != nil {
		return err
	}

	globalCustomQuotas := &capsulev1beta2.GlobalCustomQuotaList{}
	if err := r.Client.List(ctx, globalCustomQuotas); err != nil {
		return err
	}

	log.V(5).Info("rebuilding custom quota targets cache",
		"targetsBefore", r.TargetsCache.Stats(),
		"customQuotas", len(customQuotas.Items),
		"globalCustomQuotas", len(globalCustomQuotas.Items),
	)

	r.TargetsCache.Reset()

	targetsByKey := make(map[string][]capsulev1beta2.CustomQuotaStatusTarget, len(customQuotas.Items)+len(globalCustomQuotas.Items))

	for _, cq := range customQuotas.Items {
		key := customquotas.MakeCustomQuotaCacheKey(cq.GetNamespace(), cq.GetName())
		targetsByKey[key] = customQuotaStatusTargetsFromSources(cq.Spec.Sources)
	}

	for _, gcq := range globalCustomQuotas.Items {
		key := customquotas.MakeGlobalCustomQuotaCacheKey(gcq.GetName())
		targetsByKey[key] = customQuotaStatusTargetsFromSources(gcq.Spec.Sources)
	}

	for key, targets := range targetsByKey {
		compiled, err := customquotas.CompileTargets(r.JSONPathCache, targets)
		if err != nil {
			return fmt.Errorf("compile targets for cache key %q: %w", key, err)
		}

		r.TargetsCache.Set(key, compiled)
	}

	log.V(5).Info("rebuilt custom quota targets cache",
		"targets", len(targetsByKey),
		"targetsAfter", r.TargetsCache.Stats(),
	)

	return nil
}

func customQuotaStatusTargetsFromSources(
	sources []capsulev1beta2.CustomQuotaSpecSource,
) []capsulev1beta2.CustomQuotaStatusTarget {
	targets := make([]capsulev1beta2.CustomQuotaStatusTarget, 0, len(sources))

	for _, source := range sources {
		targets = append(targets, capsulev1beta2.CustomQuotaStatusTarget{
			CustomQuotaSpecSource: source,
		})
	}

	return targets
}

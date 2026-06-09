// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func (r *CacheInvalidator) rebuildRuleStatusRegistryCache(ctx context.Context, log logr.Logger) error {
	rsList := &capsulev1beta2.RuleStatusList{}
	if err := r.List(ctx, rsList,
		client.MatchingLabels{
			meta.NewManagedByCapsuleLabel: meta.ValueController,
			meta.CapsuleNameLabel:         meta.NameForManagedRuleStatus(),
		},
	); err != nil {
		return err
	}

	log.V(5).Info("rebuilding registry cache from existing rules",
		"ruleStatuses", len(rsList.Items),
		"cacheRulesBefore", r.RegistryCache.Stats(),
	)

	r.RegistryCache.Reset()

	for i := range rsList.Items {
		item := &rsList.Items[i]

		for _, rule := range item.Status.Rules {
			if rule == nil {
				continue
			}

			if rule.Enforce == nil {
				continue
			}

			if len(rule.Enforce.Workloads.Registries) == 0 {
				continue
			}

			if _, _, err := r.RegistryCache.GetOrBuild(rule.Enforce.Workloads.Registries); err != nil {
				return fmt.Errorf(
					"build registry cache for RuleStatus %s/%s: %w",
					item.Namespace,
					item.Name,
					err,
				)
			}
		}
	}

	log.V(5).Info("rebuilt registry cache from existing rules",
		"ruleStatuses", len(rsList.Items),
		"cacheRulesAfter", r.RegistryCache.Stats(),
	)

	return nil
}

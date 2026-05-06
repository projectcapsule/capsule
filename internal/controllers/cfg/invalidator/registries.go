// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"

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
		"rules", len(rsList.Items),
		"cache_rules_before", r.RegistryCache.Stats(),
	)

	r.RegistryCache.Reset()

	for _, item := range rsList.Items {
		regs := item.Status.Rule.Enforce.Registries
		if len(regs) == 0 {
			continue
		}

		if _, _, err := r.RegistryCache.GetOrBuild(regs); err != nil {
			return err
		}
	}

	log.V(5).Info("rebuilt registry cache from existing rules",
		"rules", len(rsList.Items),
		"cache_rules_after", r.RegistryCache.Stats(),
	)

	return nil
}

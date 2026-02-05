// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func (r *Manager) getItemsForStatusRegistryCache(ctx context.Context) ([]capsulev1beta2.RuleStatus, error) {
	rsList := &capsulev1beta2.RuleStatusList{}
	if err := r.List(ctx, rsList,
		client.MatchingLabels{
			meta.NewManagedByCapsuleLabel: meta.ValueController,
			meta.CapsuleNameLabel:         meta.NameForManagedRuleStatus(),
		},
	); err != nil {
		return nil, err
	}

	return rsList.Items, nil
}

func (r *Manager) warmupRuleStatusRegistryCache(ctx context.Context, log logr.Logger, items []capsulev1beta2.RuleStatus) error {
	for _, item := range items {
		regs := item.Status.Rule.Enforce.Registries
		if len(regs) == 0 {
			continue
		}

		if _, _, err := r.RegistryCache.GetOrBuild(regs); err != nil {
			return err
		}
	}

	log.V(5).Info("warmed up cache based on existing rules", "rules", len(items), "cache_rules", r.RegistryCache.Stats())

	return nil
}

func (r *Manager) invalidateRuleStatusRegistryCache(ctx context.Context, log logr.Logger) error {
	items, err := r.getItemsForStatusRegistryCache(ctx)
	if err != nil {
		return err
	}

	log.V(5).Info("cached before invalidation", "cache_rules", r.RegistryCache.Stats())

	active := make(map[string]struct{}, len(items))

	for _, item := range items {
		regs := item.Status.Rule.Enforce.Registries
		if len(regs) == 0 {
			continue
		}

		id := r.RegistryCache.HashRules(regs)
		active[id] = struct{}{}
	}

	_ = r.RegistryCache.PruneActive(active)

	log.V(5).Info("cached after invalidation", "rules", len(items), "cache_rules", r.RegistryCache.Stats())

	return nil
}

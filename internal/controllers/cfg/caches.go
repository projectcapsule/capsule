// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// invalidateCaches invokes for all caches their invalidation functions.
func (r *Manager) invalidateCaches(ctx context.Context, log logr.Logger) error {
	err := r.invalidateRuleStatusRegistryCache(ctx, log)
	if err != nil {
		return err
	}

	now := metav1.Now()

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cfg := &capsulev1beta2.CapsuleConfiguration{}
		if err := r.Get(ctx, client.ObjectKey{Name: r.configName}, cfg); err != nil {
			return err
		}

		cfg.Status.LastCacheInvalidation = now

		return r.Status().Update(ctx, cfg)
	})
}

// populateCaches warms up all custom caches.
func (r *Manager) populateCaches(ctx context.Context, log logr.Logger) error {
	items, err := r.getItemsForStatusRegistryCache(ctx)
	if err != nil {
		return err
	}

	err = r.warmupRuleStatusRegistryCache(ctx, log, items)
	if err != nil {
		return err
	}

	return nil
}

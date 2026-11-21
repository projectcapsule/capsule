// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func Add(
	ctx context.Context,
	log logr.Logger,
	mgr manager.Manager,
	configuration configuration.Configuration,
	bootstrapConfiguration configuration.Configuration,
	bootstrapConfig *rest.Config,
	opts utils.ControllerOptions,
	impersonation *cache.ImpersonationCache,
	bootstrapImpersonation *cache.ImpersonationCache,
	conditions *cache.CELCache,
	triggers *cache.PreparedReplicationTriggers,
) (err error) {
	sources, err := newNamespaceSourceIndex(ctx, mgr.GetCache())
	if err != nil {
		return err
	}

	globalMetrics := metrics.MustMakeGlobalTenantResourceRecorder()
	localMetrics := metrics.MustMakeTenantResourceRecorder()
	applied := cache.NewAppliedResources[ssa.ApplyCacheKey, ssa.ApplyResult](func(result ssa.ApplyResult) ssa.ApplyResult {
		result.LastApply = result.LastApply.DeepCopy()

		return result
	})

	if err = (&NamespaceTrigger{
		sources:         sources,
		bootstrapConfig: bootstrapConfig,
		applied:         applied,
		log:             log.WithName("Global"),
		configuration:   bootstrapConfiguration,
		impersonation:   bootstrapImpersonation,
		conditions:      conditions,
		triggers:        triggers,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create watcher controller: %w", err)
	}

	if err = (&globalResourceController{
		applied:       applied,
		log:           log.WithName("Global"),
		configuration: configuration,
		metrics:       globalMetrics,
		triggers:      triggers,

		impersonation: impersonation,
		conditions:    conditions,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create global controller: %w", err)
	}

	if err = (&namespacedResourceController{
		sources:       sources,
		applied:       applied,
		log:           log.WithName("Namespaced"),
		configuration: configuration,
		metrics:       localMetrics,
		triggers:      triggers,

		impersonation: impersonation,
		conditions:    conditions,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create namespaced controller: %w", err)
	}

	health := &healthController{
		client: mgr.GetClient(), conditions: conditions,
		globals: impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: replicationClient{Client: mgr.GetClient(), reader: mgr.GetAPIReader()}, configuration: configuration, impersonation: impersonation, resolve: globalServiceAccount},
		locals:  impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: replicationClient{Client: mgr.GetClient(), reader: mgr.GetAPIReader()}, configuration: configuration, impersonation: impersonation, resolve: namespacedServiceAccount},
	}
	if err := health.SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("register replication health observer: %w", err)
	}

	return nil
}

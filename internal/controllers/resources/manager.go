// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	configuration configuration.Configuration,
	opts utils.ControllerOptions,
	impersonation *cache.ImpersonationCache,
	conditions *cache.CELCache,
	triggers *cache.PreparedReplicationTriggers,
) (err error) {
	globalMetrics := metrics.MustMakeGlobalTenantResourceRecorder()
	localMetrics := metrics.MustMakeTenantResourceRecorder()

	if err = (&NamespaceTrigger{
		log:           log.WithName("Global"),
		configuration: configuration,
		impersonation: impersonation,
		conditions:    conditions,
		triggers:      triggers,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create watcher controller: %w", err)
	}

	if err = (&globalResourceController{
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
		client: mgr.GetClient(), conditions: conditions, globalMetrics: globalMetrics, localMetrics: localMetrics,
		globals: impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: replicationClient{Client: mgr.GetClient(), reader: mgr.GetAPIReader()}, configuration: configuration, impersonation: impersonation, resolve: globalServiceAccount},
		locals:  impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: replicationClient{Client: mgr.GetClient(), reader: mgr.GetAPIReader()}, configuration: configuration, impersonation: impersonation, resolve: namespacedServiceAccount},
	}
	if err := health.SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("register replication health observer: %w", err)
	}

	return nil
}

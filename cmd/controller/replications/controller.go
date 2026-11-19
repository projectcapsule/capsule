// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replications

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/projectcapsule/capsule/cmd/controller/common"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/cfg/invalidator"
	"github.com/projectcapsule/capsule/internal/controllers/resources"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers"
)

const LeaderElectionID = "replications.projectcapsule.dev"

func Run(ctx context.Context, options common.Options) error {
	b, err := common.NewBootstrap(ctx, options)
	if err != nil {
		return err
	}

	managerOptions, watcher, err := options.ManagerOptions(b, LeaderElectionID)
	if err != nil {
		return err
	}

	mgr, err := ctrl.NewManager(b.Config, managerOptions)
	if err != nil {
		return fmt.Errorf("create replication manager: %w", err)
	}

	if err := common.AddHealthChecks(mgr, watcher); err != nil {
		return err
	}

	controllerOptions := options.Controller

	if err := indexers.AddReplicationsToManager(ctx, ctrl.Log.WithName("replications"), mgr); err != nil {
		return fmt.Errorf("register replication indexes: %w", err)
	}

	cfg := configuration.NewCapsuleConfiguration(ctx, mgr.GetClient(), mgr.GetAPIReader(), mgr.GetConfig(), controllerOptions.ConfigurationName)
	impersonation := cache.NewImpersonationCache()

	conditions, err := cache.NewCELCache()
	if err != nil {
		return fmt.Errorf("initialize replication CEL cache: %w", err)
	}

	selectors := cache.NewLabelSelectorCache()

	triggers := cache.NewPreparedReplicationTriggers()
	recorder := metrics.MustMakeTriggerRecorder()
	log := ctrl.Log.WithName("replications")

	dispatcher := resources.NewTriggerDispatcher(mgr.GetClient(), mgr.GetAPIReader(), log, recorder)

	if err := mgr.Add(dispatcher); err != nil {
		return fmt.Errorf("register trigger dispatcher: %w", err)
	}

	if err := resources.Add(log, mgr, cfg, controllerOptions, impersonation, conditions, triggers); err != nil {
		return err
	}
	// ResourceWatches creates a bounded dynamic client from the manager config.
	if err := resources.NewResourceWatches(mgr.GetClient(), nil, mgr.GetRESTMapper(), log, cfg, conditions, selectors, triggers, dispatcher, recorder).SetupWithManager(mgr, controllerOptions); err != nil {
		return fmt.Errorf("register resource watches: %w", err)
	}

	cacheInvalidator := &invalidator.CacheInvalidator{
		Client: mgr.GetClient(), Rest: mgr.GetConfig(), Log: log.WithName("caches"),
		Configuration: cfg, ImpersonationCache: impersonation, ReplicationsOnly: true,
	}
	if err := cacheInvalidator.SetupWithManager(mgr, controllerOptions, nil); err != nil {
		return fmt.Errorf("register replication cache invalidation: %w", err)
	}

	return mgr.Start(ctx)
}

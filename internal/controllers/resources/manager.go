// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/watch"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	configuration configuration.Configuration,
	opts utils.ControllerOptions,
	cache *cache.ImpersonationCache,
) (err error) {
	// Shared, reference-counted watch manager driving the dynamic trigger
	// informers for both controllers. Each watched kind runs in its own
	// metadata-only cache (never the shared manager cache): the factory strips
	// managedFields and the last-applied annotation, which the shared cache
	// cannot (SSA ownership reads during prune/adoption need managedFields).
	// A kind gets at most two informers: one narrowed server-side to objects
	// carrying the tenant label (shared by every namespaced TenantResource)
	// and one unfiltered (GlobalTenantResources); trigger selectors are
	// matched in the sinks. Added to the manager, the caches run leader-gated,
	// alongside the reconcilers that arm watches.
	triggers := watch.NewManager(watch.MetadataCacheFactory(mgr), mgr.GetClient(), mgr.GetRESTMapper(), log.WithName("Triggers"))
	if err = mgr.Add(triggers); err != nil {
		return fmt.Errorf("unable to register trigger watch manager: %w", err)
	}

	// Each sink enqueues matching owner keys directly into its controller's
	// workqueue via a triggerSource. The workqueue dedups by owner key, so bursts
	// to a single owner collapse to one reconcile. This does not pace a
	// self-trigger loop from a non-deterministic template (output changes every
	// apply -> new watch event -> re-render): that is a misconfiguration bounded
	// only by resyncPeriod. Deterministic templates converge on their own, because
	// a no-op SSA apply produces no resourceVersion bump and thus no event.
	globalSource := &triggerSource{}
	namespacedSource := &triggerSource{}

	triggers.RegisterSink(&globalTriggerSink{
		reader:  mgr.GetClient(),
		enqueue: globalSource.enqueue,
		log:     log.WithName("Triggers").WithName("Global"),
	})
	triggers.RegisterSink(&namespacedTriggerSink{
		reader:  mgr.GetClient(),
		enqueue: namespacedSource.enqueue,
		log:     log.WithName("Triggers").WithName("Namespaced"),
	})

	if err = (&globalResourceController{
		log:           log.WithName("Global"),
		configuration: configuration,
		metrics:       metrics.MustMakeGlobalTenantResourceRecorder(),

		impersonation: cache,

		triggers:   triggers,
		triggerSrc: globalSource,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create global controller: %w", err)
	}

	if err = (&namespacedResourceController{
		log:           log.WithName("Namespaced"),
		configuration: configuration,
		metrics:       metrics.MustMakeTenantResourceRecorder(),

		impersonation: cache,

		triggers:   triggers,
		triggerSrc: namespacedSource,
	}).SetupWithManager(mgr, opts); err != nil {
		return fmt.Errorf("unable to create namespaced controller: %w", err)
	}

	return nil
}

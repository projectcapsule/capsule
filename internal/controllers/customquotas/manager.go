// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	recorder events.EventRecorder,
	cfg utils.ControllerOptions,
	quantityCache *cache.QuantityCache[string],
	jsonPathCache *cache.JSONPathCache,
	targetsCache *cache.CompiledTargetsCache[string],
	namespaceNotifier chan event.TypedGenericEvent[*capsulev1beta2.CustomQuota],
	globalNotifier chan event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota],
) (err error) {
	if err = (&customQuotaClaimController{
		Client:        mgr.GetClient(),
		log:           log.WithName("CustomQuota"),
		recorder:      recorder,
		metrics:       metrics.MustMakeCustomQuotaRecorder(),
		jsonPathCache: jsonPathCache,
		targetsCache:  targetsCache,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create custom quota controller: %w", err)
	}

	if err = (&clusterCustomQuotaClaimController{
		Client:        mgr.GetClient(),
		log:           log.WithName("ClusterCustomQuota"),
		recorder:      recorder,
		metrics:       metrics.MustMakeGlobalCustomQuotaRecorder(),
		jsonPathCache: jsonPathCache,
		targetsCache:  targetsCache,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create cluster custom quota controller: %w", err)
	}

	return nil
}

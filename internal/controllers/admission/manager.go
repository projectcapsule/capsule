// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	recorder events.EventRecorder,
	cfg utils.ControllerOptions,
	capsuleConfig configuration.Configuration,
	conditions *cache.CELCache,
	triggerMetrics *metrics.TriggerRecorder,
	delivery *replication.DeliveryKey,
) (err error) {
	if err = (&validatingReconciler{
		triggerMetrics: triggerMetrics,
		delivery:       delivery,
		conditions:     conditions,
		client:         mgr.GetClient(),
		log:            log.WithName("admission"),
		configuration:  capsuleConfig,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create validating admission controller: %w", err)
	}

	if err = (&mutatingReconciler{
		client:        mgr.GetClient(),
		log:           log.WithName("admission"),
		configuration: capsuleConfig,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create mutating admission controller: %w", err)
	}

	return nil
}

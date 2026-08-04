// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequotas

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	recorder events.EventRecorder,
	cfg utils.ControllerOptions,
) error {
	controller := &Controller{
		Client:   mgr.GetClient(),
		log:      log,
		recorder: recorder,
		metrics:  metrics.MustMakeGlobalResourceQuotaRecorder(),
	}
	if err := controller.SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create GlobalResourceQuota controller: %w", err)
	}

	return nil
}

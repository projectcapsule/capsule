// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	recorder record.EventRecorder,
	cfg utils.ControllerOptions,
) (err error) {
	if err = (&resourcePoolController{
		Client:   mgr.GetClient(),
		log:      log.WithName("Pools"),
		recorder: recorder,
		metrics:  metrics.MustMakeResourcePoolRecorder(),
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create pool controller: %w", err)
	}

	if err = (&resourceClaimController{
		Client:   mgr.GetClient(),
		log:      log.WithName("Claims"),
		recorder: recorder,
		metrics:  metrics.MustMakeClaimRecorder(),
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create claim controller: %w", err)
	}

	return nil
}

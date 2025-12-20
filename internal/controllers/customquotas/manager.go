// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/internal/controllers/utils"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	recorder record.EventRecorder,
	cfg utils.ControllerOptions,
) (err error) {
	if err = (&customQuotaClaimController{
		Client:   mgr.GetClient(),
		log:      log.WithName("CustomQuota"),
		recorder: recorder,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create custom quota controller: %w", err)
	}

	if err = (&clusterCustomQuotaClaimController{
		Client:   mgr.GetClient(),
		log:      log.WithName("ClusterCustomQuota"),
		recorder: recorder,
	}).SetupWithManager(mgr, cfg); err != nil {
		return fmt.Errorf("unable to create cluster custom quota controller: %w", err)
	}

	return nil
}

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/metrics"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	configuration configuration.Configuration,
) (err error) {
	if err = (&globalResourceController{
		log:           log.WithName("Global"),
		configuration: configuration,
		metrics:       metrics.MustMakeGlobalTenantResourceRecorder(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create global controller: %w", err)
	}

	if err = (&namespacedResourceController{
		log:           log.WithName("Namespaced"),
		configuration: configuration,
		metrics:       metrics.MustMakeTenantResourceRecorder(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create namespaced controller: %w", err)
	}

	return nil
}

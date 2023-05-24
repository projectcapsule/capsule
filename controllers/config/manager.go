// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/controllers/utils"
	"github.com/clastix/capsule/pkg/configuration"
)

type Manager struct {
	client client.Client

	Log logr.Logger
}

func (c *Manager) SetupWithManager(mgr ctrl.Manager, configurationName string) error {
	c.client = mgr.GetClient()

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.CapsuleConfiguration{}, utils.NamesMatchingPredicate(configurationName)).
		Complete(c)
}

func (c *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	c.Log.Info("CapsuleConfiguration reconciliation started", "request.name", request.Name)

	cfg := configuration.NewCapsuleConfiguration(ctx, c.client, request.Name)
	// Validating the Capsule Configuration options
	if _, err = cfg.ProtectedNamespaceRegexp(); err != nil {
		panic(errors.Wrap(err, "Invalid configuration for protected Namespace regex"))
	}

	c.Log.Info("CapsuleConfiguration reconciliation finished", "request.name", request.Name)

	return
}

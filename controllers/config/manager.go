// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
)

type Manager struct {
	Log    logr.Logger
	Client client.Client
}

// InjectClient injects the Client interface, required by the Runnable interface
func (r *Manager) InjectClient(c client.Client) error {
	r.Client = c

	return nil
}

func filterByName(objName, desired string) bool {
	return objName == desired
}

func forOptionPerInstanceName(instanceName string) builder.ForOption {
	return builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			return filterByName(event.Object.GetName(), instanceName)
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return filterByName(deleteEvent.Object.GetName(), instanceName)
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return filterByName(updateEvent.ObjectNew.GetName(), instanceName)
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return filterByName(genericEvent.Object.GetName(), instanceName)
		},
	})
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, configurationName string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CapsuleConfiguration{}, forOptionPerInstanceName(configurationName)).
		Complete(r)
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	r.Log.Info("CapsuleConfiguration reconciliation started", "request.name", request.Name)

	cfg := configuration.NewCapsuleConfiguration(r.Client, request.Name)
	// Validating the Capsule Configuration options
	if _, err = cfg.ProtectedNamespaceRegexp(); err != nil {
		panic(errors.Wrap(err, "Invalid configuration for protected Namespace regex"))
	}

	r.Log.Info("CapsuleConfiguration reconciliation finished", "request.name", request.Name)

	return
}

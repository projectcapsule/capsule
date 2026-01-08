// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type Manager struct {
	client.Client

	Log logr.Logger
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.CapsuleConfiguration{}, utils.NamesMatchingPredicate(ctrlConfig.ConfigurationName)).
		Watches(
			&capsulev1beta2.TenantOwner{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: ctrlConfig.ConfigurationName,
						},
					},
				}
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					to, ok := e.Object.(*capsulev1beta2.TenantOwner)

					return ok && to.Spec.Aggregate
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldTo, ok1 := e.ObjectOld.(*capsulev1beta2.TenantOwner)
					newTo, ok2 := e.ObjectNew.(*capsulev1beta2.TenantOwner)

					if !ok1 || !ok2 {
						return false
					}

					if oldTo.Spec.Aggregate != newTo.Spec.Aggregate {
						return true
					}

					if oldTo.Spec.Name != newTo.Spec.Name {
						return true
					}

					if oldTo.Spec.Kind != newTo.Spec.Kind {
						return true
					}

					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					to, ok := e.Object.(*capsulev1beta2.TenantOwner)

					return ok && to.Spec.Aggregate
				},
			}),
		).
		Complete(r)
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	r.Log.V(5).Info("CapsuleConfiguration reconciliation started", "request.name", request.Name)

	cfg := configuration.NewCapsuleConfiguration(ctx, r.Client, request.Name)

	instance := &capsulev1beta2.CapsuleConfiguration{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return res, err
	}

	defer func() {
		if uerr := r.updateConfigStatus(ctx, instance); uerr != nil {
			err = fmt.Errorf("cannot update config status: %w", uerr)

			return
		}
	}()

	// Validating the Capsule Configuration options
	if _, err = cfg.ProtectedNamespaceRegexp(); err != nil {
		panic(errors.Wrap(err, "Invalid configuration for protected Namespace regex"))
	}

	r.Log.V(5).Info("Validated Regex")

	if err := r.gatherCapsuleUsers(ctx, instance, cfg); err != nil {
		return reconcile.Result{}, err
	}

	r.Log.V(5).Info("Gathered users", "users", len(instance.Status.Users))

	return res, err
}

func (r *Manager) gatherCapsuleUsers(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
	cfg configuration.Configuration,
) (err error) {
	users := cfg.Users()

	toList := &capsulev1beta2.TenantOwnerList{}
	if err := r.List(ctx, toList); err != nil {
		return fmt.Errorf("listing TenantOwner CRs: %w", err)
	}

	for i := range toList.Items {
		to := &toList.Items[i]

		if !to.Spec.Aggregate {
			continue
		}

		users.Upsert(api.UserSpec{
			Kind: to.Spec.Kind,
			Name: to.Spec.Name,
		})
	}

	instance.Status.Users = users

	return nil
}

func (r *Manager) updateConfigStatus(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.CapsuleConfiguration{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		return r.Status().Update(ctx, latest)
	})
}

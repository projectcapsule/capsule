// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type Manager struct {
	Client client.Client
	Rest   *rest.Config

	configName string

	RegistryCache *cache.RegistryRuleSetCache
	Log           logr.Logger
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	r.configName = ctrlConfig.ConfigurationName

	err = ctrl.NewControllerManagedBy(mgr).
		Named("capsule/configuration").
		For(
			&capsulev1beta2.CapsuleConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
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
	if err != nil {
		return err
	}

	// register Start(ctx) as a manager runnable.
	return mgr.Add(r)
}

// Start is the Runnable function triggered upon Manager start-up to perform cache population.
func (r *Manager) Start(ctx context.Context) error {
	if err := r.populateCaches(ctx, r.Log); err != nil {
		r.Log.Error(err, "cache population failed")

		return nil
	}

	r.Log.Info("caches populated")

	return nil
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := r.Log.WithValues("configuration", request.Name)

	instance := &capsulev1beta2.CapsuleConfiguration{}
	if err = r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("requested object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		log.Error(err, "error reading the object")

		return res, err
	}

	defer func() {
		if uerr := r.updateConfigStatus(ctx, instance); uerr != nil {
			err = fmt.Errorf("cannot update config status: %w", uerr)

			return
		}
	}()

	// Validating the Capsule Configuration options.
	if _, err = cfg.ProtectedNamespaceRegexp(); err != nil {
		panic(errors.Wrap(err, "invalid configuration for protected Namespace regex"))
	}

	if err := r.gatherCapsuleUsers(ctx, instance, cfg); err != nil {
		return reconcile.Result{}, err
	}

	log.V(5).Info("gathering capsule users", "users", len(instance.Status.Users))

	interval := cfg.CacheInvalidation()
	if cache.ShouldInvalidate(ptr.To(instance.Status.LastCacheInvalidation), time.Now(), interval.Duration) {
		log.V(3).Info("invalidating caches")

		if err := r.invalidateCaches(ctx, log); err != nil {
			return res, err
		}
	}

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: interval.Duration,
	}, err
}

func (r *Manager) gatherCapsuleUsers(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
	cfg configuration.Configuration,
) (err error) {
	users := cfg.Users()

	toList := &capsulev1beta2.TenantOwnerList{}
	if err := r.Client.List(ctx, toList); err != nil {
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
		if err = r.Client.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		return r.Client.Status().Update(ctx, latest)
	})
}

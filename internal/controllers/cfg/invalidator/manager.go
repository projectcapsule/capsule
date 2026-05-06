// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
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
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type CacheInvalidator struct {
	client.Client

	Rest *rest.Config
	Log  logr.Logger

	configName    string
	Configuration configuration.Configuration

	RegistryCache      *cache.RegistryRuleSetCache
	TargetsCache       *cache.CompiledTargetsCache[string]
	JSONPathCache      *cache.JSONPathCache
	ImpersonationCache *cache.ImpersonationCache
}

func (r *CacheInvalidator) NeedLeaderElection() bool {
	return false
}

// Start is the Runnable function triggered upon Manager start-up to perform cache population.
func (r *CacheInvalidator) Start(ctx context.Context) error {
	if err := r.rebuildCaches(ctx, r.Log); err != nil {
		r.Log.Error(err, "cache population failed")

		return nil
	}

	<-ctx.Done()

	return nil
}

func (r *CacheInvalidator) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	r.configName = ctrlConfig.ConfigurationName

	err = ctrl.NewControllerManagedBy(mgr).
		Named("config/caches").
		For(
			&capsulev1beta2.CapsuleConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.Funcs{
				UpdateFunc: func(ctx context.Context, updateEvent event.TypedUpdateEvent[client.Object], limitingInterface workqueue.TypedRateLimitingInterface[reconcile.Request]) {
					if err := r.rebuildImpersonationCache(ctx, r.Log); err != nil {
						r.Log.Error(err, "unable to invalidate impersonation cache")
					}
				},
			},
			builder.WithPredicates(
				predicates.CapsuleConfigSpecImpersonationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(
			&corev1.ServiceAccount{},
			handler.Funcs{
				DeleteFunc: func(
					ctx context.Context,
					e event.TypedDeleteEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					sa, ok := e.Object.(*corev1.ServiceAccount)
					if !ok {
						return
					}

					if err := r.invalidateServiceAccount(ctx, sa); err != nil {
						r.Log.Error(err, "unable to invalidate serviceaccount cache",
							"namespace", sa.GetNamespace(),
							"name", sa.GetName(),
						)
					}
				},
			},
			builder.WithPredicates(predicate.Funcs{
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			},
			),
		).
		Complete(r)
	if err != nil {
		return err
	}

	// register Start(ctx) as a manager runnable.
	return mgr.Add(r)
}

func (r *CacheInvalidator) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := r.Log.WithValues("configuration", request.Name)

	cfg := configuration.NewCapsuleConfiguration(ctx, r.Client, r.Rest, request.Name)

	instance := &capsulev1beta2.CapsuleConfiguration{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
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

	if err := r.rebuildCaches(ctx, log); err != nil {
		return res, err
	}

	interval := cfg.CacheInvalidation()

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: interval.Duration,
	}, err
}

func (r *CacheInvalidator) updateConfigStatus(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.CapsuleConfiguration{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		return r.Client.Status().Update(ctx, latest)
	})
}

// invalidateCaches invokes for all caches their invalidation functions.
func (r *CacheInvalidator) rebuildCaches(
	ctx context.Context,
	log logr.Logger,
) error {
	var errs []error

	if err := r.rebuildJSONPathCache(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("rebuild JSONPath cache: %w", err))
	}

	if err := r.rebuildTargetsCache(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("rebuild targets cache: %w", err))
	}

	if err := r.rebuildRuleStatusRegistryCache(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("rebuild registry cache: %w", err))
	}

	if err := r.rebuildImpersonationCache(ctx, log); err != nil {
		errs = append(errs, fmt.Errorf("rebuild impersonation cache: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

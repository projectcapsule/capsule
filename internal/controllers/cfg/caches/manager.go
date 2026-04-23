// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package caches

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type Manager struct {
	client.Client

	Rest *rest.Config
	Log  logr.Logger

	configName    string
	Configuration configuration.Configuration

	RegistryCache      *cache.RegistryRuleSetCache
	ImpersonationCache *cache.ImpersonationCache
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

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	r.configName = ctrlConfig.ConfigurationName

	err = ctrl.NewControllerManagedBy(mgr).
		Named("config/caches").
		For(
			&capsulev1beta2.CapsuleConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.CapsuleConfigSpecImpersonationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Complete(r)
	if err != nil {
		return err
	}

	// register Start(ctx) as a manager runnable.
	return mgr.Add(r)
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
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

		return r.Client.Status().Update(ctx, latest)
	})
}

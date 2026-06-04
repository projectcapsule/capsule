// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

// tenantEventMarker is placed in reconcile.Request.Namespace by the Tenant
// create/delete watch handler. CapsuleConfiguration is cluster-scoped so its
// Namespace is always empty in normal operation; a non-empty value is a
// zero-cost hint telling Reconcile that the trigger was a Tenant create/delete.
const tenantEventMarker = "tenant-event"

type Manager struct {
	client.Client

	Rest *rest.Config

	configName string
	Log        logr.Logger
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	r.configName = ctrlConfig.ConfigurationName

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/configuration").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		For(
			&capsulev1beta2.CapsuleConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(
			&capsulev1beta2.Tenant{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: ctrlConfig.ConfigurationName,
						},
					},
				}
			}),
			builder.WithPredicates(
				predicates.TenantStatusOwnersChangedPredicate{},
			),
		).
		Watches(
			&capsulev1beta2.Tenant{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      ctrlConfig.ConfigurationName,
							Namespace: tenantEventMarker,
						},
					},
				}
			}),
			builder.WithPredicates(
				predicates.TenantCountChangedPredicate{},
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
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := r.Log.WithValues("configuration", request.Name)

	// request.Namespace is empty for cluster-scoped resources. A non-empty
	// value is set by the TenantCountChangedPredicate watch to signal a Tenant
	// create/delete; only then (or on first bootstrap) do we refresh.
	isTenantEvent := request.Namespace == tenantEventMarker

	// didRefreshTenants is set to true only when gatherTenants actually runs.
	// It is threaded into updateConfigStatus so that only an authoritative
	// refresh overwrites latest.Status.Tenants.
	didRefreshTenants := false

	cfg := configuration.NewCapsuleConfiguration(ctx, r.Client, r.Rest, request.Name)

	instance := &capsulev1beta2.CapsuleConfiguration{}
	if err = r.Get(ctx, types.NamespacedName{Name: request.Name}, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(5).Info("requested object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		log.Error(err, "error reading the object")

		return res, err
	}

	defer func() {
		if uerr := r.updateConfigStatus(ctx, instance, didRefreshTenants, err); uerr != nil {
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

	// Refresh tenants on Tenant create/delete, or on first bootstrap when the
	// field is nil (uninitialized). A non-nil empty slice means the controller
	// has already run and found zero tenants — no refresh needed in steady state.
	if isTenantEvent || instance.Status.Tenants == nil {
		if err := r.gatherTenants(ctx, instance); err != nil {
			return reconcile.Result{}, err
		}

		didRefreshTenants = true
	}

	return reconcile.Result{}, err
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

		users.Upsert(rbac.UserSpec{
			Kind: to.Spec.Kind,
			Name: to.Spec.Name,
		})
	}

	instance.Status.Users = users

	return nil
}

func (r *Manager) gatherTenants(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
) error {
	tenantList := &capsulev1beta2.TenantList{}
	if err := r.List(ctx, tenantList); err != nil {
		return fmt.Errorf("listing Tenants: %w", err)
	}

	names := make([]string, 0, len(tenantList.Items))
	for i := range tenantList.Items {
		names = append(names, tenantList.Items[i].Name)
	}

	sort.Strings(names)

	// Always assign when uninitialized (nil → non-nil empty slice), so the
	// bootstrap check can distinguish "not yet run" from "ran, found zero
	// tenants". For subsequent runs the slices.Equal guard avoids spurious
	// status writes when the tenant set is unchanged.
	if instance.Status.Tenants == nil || !slices.Equal(names, instance.Status.Tenants) {
		instance.Status.Tenants = names
	}

	return nil
}

func (r *Manager) updateConfigStatus(
	ctx context.Context,
	instance *capsulev1beta2.CapsuleConfiguration,
	didRefreshTenants bool,
	reconcileErr error,
) error {
	// Log once here, outside the retry loop.  The closure below may run
	// multiple times on conflict; logging inside it would emit duplicate
	// entries for the same reconcile failure.
	if reconcileErr != nil {
		r.Log.Error(reconcileErr, "reconcile failed", "configuration", instance.GetName())
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.CapsuleConfiguration{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		// Update only the fields this reconcile is authoritative for.
		// Avoid wholesale status replacement: a non-tenant reconcile must not
		// clobber a newer status.tenants written by a concurrent tenant-event
		// reconcile.
		latest.Status.Users = instance.Status.Users
		latest.Status.ObservedGeneration = latest.GetGeneration()

		// Only overwrite Tenants when this reconcile actually refreshed them.
		// A config-spec reconcile must not clobber a newer status.tenants
		// written by a concurrent tenant-event reconcile. Using an explicit
		// boolean (not a length check) also handles the zero-tenant case
		// correctly.
		if didRefreshTenants {
			latest.Status.Tenants = instance.Status.Tenants
		}

		readyCondition := capmeta.NewReadyCondition(latest)
		readyCondition.ObservedGeneration = latest.GetGeneration()
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = capmeta.SucceededReason
		readyCondition.Message = "reconciled"

		if reconcileErr != nil {
			// Never expose raw error strings in the condition: even short errors
			// can contain sensitive details (endpoints, tokens, usernames) visible
			// to anyone who can read CapsuleConfiguration.
			readyCondition.Message = "reconcile failed; see controller logs for details"
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = capmeta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		return r.Client.Status().Update(ctx, latest)
	})
}

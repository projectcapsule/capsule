// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resourcesv1 "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	meta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type Manager struct {
	client.Client

	Metrics       *metrics.TenantRecorder
	Log           logr.Logger
	Recorder      record.EventRecorder
	Configuration configuration.Configuration
	RESTConfig    *rest.Config
	classes       supportedClasses
}

type supportedClasses struct {
	device  bool
	gateway bool
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.Tenant{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
			),
		).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&corev1.LimitRange{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
			utils.NamesMatchingPredicate(ctrlConfig.ConfigurationName),
			builder.WithPredicates(utils.CapsuleConfigSpecChangedPredicate),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.Tenant{}),
		).
		Watches(
			&storagev1.StorageClass{},
			r.statusOnlyHandlerClasses(
				r.reconcileClassStatus,
				r.collectAvailableStorageClasses,
				"cannot collect storage classes",
			),
			builder.WithPredicates(utils.UpdatedMetadataPredicate),
		).
		Watches(
			&schedulingv1.PriorityClass{},
			r.statusOnlyHandlerClasses(
				r.reconcileClassStatus,
				r.collectAvailablePriorityClasses,
				"cannot collect priority classes",
			),
			builder.WithPredicates(utils.UpdatedMetadataPredicate),
		).
		Watches(
			&nodev1.RuntimeClass{},
			r.statusOnlyHandlerClasses(
				r.reconcileClassStatus,
				r.collectAvailableRuntimeClasses,
				"cannot collect runtime classes",
			),
			builder.WithPredicates(utils.UpdatedMetadataPredicate),
		).
		Watches(
			&capsulev1beta2.TenantOwner{},
			handler.TypedFuncs[client.Object, ctrl.Request]{
				CreateFunc: func(
					ctx context.Context,
					e event.TypedCreateEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueForTenantsWithCondition(
						ctx,
						e.Object,
						q,
						func(tnt *capsulev1beta2.Tenant, c client.Object) bool {
							return len(tnt.Spec.Permissions.MatchOwners) > 0
						})
				},
				UpdateFunc: func(
					ctx context.Context,
					e event.TypedUpdateEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueForTenantsWithCondition(
						ctx,
						e.ObjectNew,
						q,
						func(tnt *capsulev1beta2.Tenant, c client.Object) bool {
							return len(tnt.Spec.Permissions.MatchOwners) > 0
						})
				},

				DeleteFunc: func(
					ctx context.Context,
					e event.TypedDeleteEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueTenantsForTenantOwner(ctx, e.Object, q)
				},
			},
		).
		Watches(
			&corev1.ServiceAccount{},
			handler.TypedFuncs[client.Object, ctrl.Request]{
				CreateFunc: func(
					ctx context.Context,
					e event.TypedCreateEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueForTenantsWithCondition(ctx, e.Object, q, func(tnt *capsulev1beta2.Tenant, c client.Object) bool {
						return slices.Contains(tnt.Status.Namespaces, c.GetNamespace())
					})
				},
				UpdateFunc: func(
					ctx context.Context,
					e event.TypedUpdateEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueForTenantsWithCondition(ctx, e.ObjectNew, q, func(tnt *capsulev1beta2.Tenant, c client.Object) bool {
						return slices.Contains(tnt.Status.Namespaces, c.GetNamespace())
					})
				},
				DeleteFunc: func(
					ctx context.Context,
					e event.TypedDeleteEvent[client.Object],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					r.enqueueForTenantsWithCondition(ctx, e.Object, q, func(tnt *capsulev1beta2.Tenant, c client.Object) bool {
						_, found := tnt.Status.Owners.FindOwner(
							serviceaccount.ServiceAccountUsernamePrefix+c.GetNamespace()+":"+c.GetName(),
							api.ServiceAccountOwner,
						)

						return found
					})
				},
			},
			builder.WithPredicates(utils.PromotedServiceaccountPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles})

	// GatewayClass is Optional
	r.classes.gateway = utils.HasGVK(mgr.GetRESTMapper(), schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "GatewayClass",
	})

	if r.classes.gateway {
		ctrlBuilder = ctrlBuilder.Watches(
			&gatewayv1.GatewayClass{},
			r.statusOnlyHandlerClasses(
				r.reconcileClassStatus,
				r.collectAvailableGatewayClasses,
				"cannot collect gateway classes",
			),
			builder.WithPredicates(utils.UpdatedMetadataPredicate),
		)
	}

	// DeviceClass is Optional
	r.classes.device = utils.HasGVK(mgr.GetRESTMapper(), schema.GroupVersionKind{
		Group:   "resource.k8s.io",
		Version: "v1",
		Kind:    "DeviceClass",
	})

	if r.classes.device {
		ctrlBuilder = ctrlBuilder.Watches(
			&resourcesv1.DeviceClass{},
			r.statusOnlyHandlerClasses(
				r.reconcileClassStatus,
				r.collectAvailableDeviceClasses,
				"cannot collect device classes",
			),
			builder.WithPredicates(utils.UpdatedMetadataPredicate),
		)
	}

	return ctrlBuilder.Complete(r)
}

func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)

	// Fetch the Tenant instance
	instance := &capsulev1beta2.Tenant{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			// If tenant was deleted or cannot be found, clean up metrics
			r.Metrics.DeleteAllMetricsForTenant(request.Name)

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return result, err
	}

	defer func() {
		r.syncTenantStatusMetrics(instance)

		if uerr := r.updateTenantStatus(ctx, instance, err); uerr != nil {
			err = fmt.Errorf("cannot update tenant status: %w", uerr)

			return
		}

		// Controller-Runtime should never receive error
		err = nil
	}()

	// Collect Ownership for Status
	if err = r.collectOwners(ctx, instance); err != nil {
		err = fmt.Errorf("cannot collect available owners: %w", err)

		return result, err
	}

	// Ensuring Metadata.
	err, updated := r.ensureMetadata(ctx, instance)
	if err != nil {
		err = fmt.Errorf("cannot ensure metadata: %w", err)

		return result, err
	}

	if updated {
		return result, nil
	}

	// Ensuring ResourceQuota
	r.Log.V(4).Info("Ensuring limit resources count is updated")

	if err = r.syncCustomResourceQuotaUsages(ctx, instance); err != nil {
		err = fmt.Errorf("cannot count limited resources: %w", err)

		return result, err
	}

	// Reconcile Namespaces
	r.Log.V(4).Info("Starting processing of Namespaces", "items", len(instance.Status.Namespaces))

	if err = r.reconcileNamespaces(ctx, instance); err != nil {
		err = fmt.Errorf("namespace(s) had reconciliation errors")

		return result, err
	}

	// Ensuring NetworkPolicy resources
	r.Log.V(4).Info("Starting processing of Network Policies")

	if err = r.syncNetworkPolicies(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync networkPolicy items: %w", err)

		return result, err
	}

	// Ensuring LimitRange resources
	r.Log.V(4).Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items)) //nolint:staticcheck

	if err = r.syncLimitRanges(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync limitrange items: %w", err)

		return result, err
	}

	// Ensuring ResourceQuota resources
	r.Log.V(4).Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))

	if err = r.syncResourceQuotas(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync resourcequota items: %w", err)

		return result, err
	}

	// Ensuring RoleBinding resources
	r.Log.V(4).Info("Ensuring RoleBindings for Owners and Tenant")

	if err = r.syncRoleBindings(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync rolebindings items: %w", err)

		return result, err
	}

	// Collect available resources
	if err = r.collectAvailableResources(ctx, instance); err != nil {
		err = fmt.Errorf("cannot collect available resources: %w", err)

		return result, err
	}

	var reconcileError error
	if err != nil {
		reconcileError = fmt.Errorf("tenant had errors reconciling, check tenant's status")
	}

	r.Log.V(4).Info("Tenant reconciling completed")

	return ctrl.Result{}, reconcileError
}

func (r *Manager) updateTenantStatus(ctx context.Context, tnt *capsulev1beta2.Tenant, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.Tenant{}
		if err = r.Get(ctx, types.NamespacedName{Name: tnt.GetName()}, latest); err != nil {
			return err
		}

		latest.Status = tnt.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(tnt)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		// Set Cordoned Condition
		cordonedCondition := meta.NewCordonedCondition(tnt)

		if tnt.Spec.Cordoned {
			latest.Status.State = capsulev1beta2.TenantStateCordoned

			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "Tenant is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		} else {
			latest.Status.State = capsulev1beta2.TenantStateActive
		}

		latest.Status.Conditions.UpdateConditionByType(cordonedCondition)

		return r.Client.Status().Update(ctx, latest)
	})
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resourcesv1 "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/cluster-api/util/patch"
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
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type Manager struct {
	client.Client

	reader client.Reader

	DiscoveryClient discovery.DiscoveryInterface
	DynamicClient   dynamic.Interface

	Metrics       *metrics.TenantRecorder
	Log           logr.Logger
	Recorder      events.EventRecorder
	Configuration configuration.Configuration
	RESTConfig    *rest.Config
	classes       supportedClasses

	discoveryCache cache.DiscoveryNamespacedResourceCache
}

type supportedClasses struct {
	device  bool
	gateway bool
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.reader = mgr.GetAPIReader()
	r.discoveryCache = cache.NewDiscoveryNamespacedResourceCache()

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("capsule/tenants").
		For(
			&capsulev1beta2.Tenant{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.UpdatedMetadataPredicate{},
				),
			),
		).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&corev1.LimitRange{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
			builder.WithPredicates(
				predicates.CapsuleConfigSpecAdministratorsChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.Tenant{}),
		).
		Watches(
			&capsulev1beta2.RuleStatus{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.Tenant{}),
		).
		Watches(
			&storagev1.StorageClass{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
		).
		Watches(
			&schedulingv1.PriorityClass{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
		).
		Watches(
			&nodev1.RuntimeClass{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
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
					r.enqueueForTenantsWithCondition(
						ctx,
						e.Object,
						q,
						func(tnt *capsulev1beta2.Tenant, _ client.Object) bool {
							return len(tnt.Spec.Permissions.MatchOwners) > 0
						},
					)
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
							rbac.ServiceAccountOwner,
						)

						return found
					})
				},
			},
			builder.WithPredicates(predicates.PromotedServiceaccountPredicate{}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles})

	// GatewayClass is Optional
	r.classes.gateway = gvk.HasGVK(mgr.GetRESTMapper(), schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "GatewayClass",
	})

	if r.classes.gateway {
		ctrlBuilder = ctrlBuilder.Watches(
			&gatewayv1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
		)
	}

	// DeviceClass is Optional
	r.classes.device = gvk.HasGVK(mgr.GetRESTMapper(), schema.GroupVersionKind{
		Group:   "resource.k8s.io",
		Version: "v1",
		Kind:    "DeviceClass",
	})

	if r.classes.device {
		ctrlBuilder = ctrlBuilder.Watches(
			&resourcesv1.DeviceClass{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllTenants),
		)
	}

	return ctrlBuilder.Complete(r)
}

func (r *Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("Request.Name", request.Name)

	// Fetch the Tenant instance
	instance := &capsulev1beta2.Tenant{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(5).Info("request object not found, could have been deleted after reconcile request")

			// If tenant was deleted or cannot be found, clean up metrics
			r.Metrics.DeleteAllMetricsForTenant(request.Name)

			return reconcile.Result{}, nil
		}

		return result, err
	}

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	if updateErr := r.updateReconcilingStatus(ctx, instance); updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, updateErr
	}

	reconcileError := r.reconcile(ctx, log, instance)

	defer func() {
		r.syncTenantStatusMetrics(instance)

		if statusErr := r.updateTenantStatus(ctx, instance, reconcileError); statusErr != nil {
			statusErr = fmt.Errorf("cannot update tenant status: %w", statusErr)

			if err == nil {
				err = statusErr
			} else {
				err = errors.Join(err, statusErr)
			}
		}
	}()

	if e := patchHelper.Patch(ctx, instance); e != nil {
		if caperrors.IgnoreGone(e) {
			err = nil

			return result, err
		}

		return reconcile.Result{}, e
	}

	// Collect available resources
	if err = r.collectAvailableResources(ctx, log, instance); err != nil {
		err = fmt.Errorf("cannot collect available resources: %w", err)

		return reconcile.Result{}, err
	}

	if instance.DeletionTimestamp != nil && len(instance.Status.Spaces) > 0 {
		return reconcile.Result{RequeueAfter: 2 * time.Second}, nil
	}

	return reconcile.Result{}, reconcileError
}

func (r *Manager) reconcile(ctx context.Context, log logr.Logger, instance *capsulev1beta2.Tenant) (err error) {
	var errs []error

	// Collect Ownership/Promotions for Status
	if err = r.collectRBAC(ctx, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot collect available rbac: %w", err))
	}

	// Reconcile Namespaces
	log.V(4).Info("starting processing of Namespaces", "items", len(instance.Status.Namespaces))

	if err = r.reconcileNamespaces(ctx, log, instance); err != nil {
		errs = append(errs, fmt.Errorf("namespace(s) had reconciliation errors: %w", err))
	}

	// Ensuring Metadata.
	err = r.ensureMetadata(ctx, instance)
	if err != nil {
		errs = append(errs, fmt.Errorf("cannot ensure metadata: %w", err))
	}

	// Ensuring ResourceQuota
	log.V(4).Info("ensuring limit resources count is updated")

	if err = r.syncCustomResourceQuotaUsages(ctx, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot count limited resources: %w", err))
	}

	// Ensuring NetworkPolicy resources
	log.V(4).Info("starting processing of Network Policies")

	if err = r.syncNetworkPolicies(ctx, log, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot sync networkPolicy items: %w", err))
	}

	// Ensuring LimitRange resources
	//nolint:staticcheck
	log.V(4).Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items))

	if err = r.syncLimitRanges(ctx, log, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot sync limitrange items: %w", err))
	}

	// Ensuring ResourceQuota resources
	log.V(4).Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))

	if err = r.syncResourceQuotas(ctx, log, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot sync resourcequota items: %w", err))
	}

	// Ensuring RoleBinding resources
	log.V(4).Info("Ensuring RoleBindings for Owners and Tenant")

	if err = r.syncRoleBindings(ctx, log, instance); err != nil {
		errs = append(errs, fmt.Errorf("cannot sync rolebindings items: %w", err))
	}

	if err = errors.Join(errs...); err != nil {
		return err
	}

	log.V(4).Info("Tenant reconciling completed")

	return err
}

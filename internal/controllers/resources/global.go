// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type globalResourceController struct {
	client        client.Client
	mapper        k8smeta.RESTMapper
	log           logr.Logger
	processor     processor.Processor
	collector     Collector
	configuration configuration.Configuration
	metrics       *metrics.GlobalTenantResourceRecorder

	impersonation *cache.ImpersonationCache
}

func (r *globalResourceController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.client = mgr.GetClient()
	r.processor = processor.Processor{
		Configuration:                r.configuration,
		AllowCrossNamespaceSelection: true,
	}
	r.collector = NewCollector(
		r.client,
		mgr.GetRESTMapper(),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.GlobalTenantResource{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.ReconcileRequestedPredicate{},
				),
			),
		).
		Watches(
			&capsulev1beta2.GlobalTenantResource{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueDependentGlobalTenantResources),
		).
		Watches(
			&capsulev1beta2.Tenant{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRequestFromTenant),
		).
		// Performs Cache Invalidation for all Impersonations Clients
		Watches(
			&corev1.ServiceAccount{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
				return nil
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					sa, ok := e.Object.(*corev1.ServiceAccount)
					if !ok {
						return false
					}

					r.impersonation.Invalidate(sa.Namespace, sa.Name)

					return false
				},
			}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *globalResourceController) enqueueDependentGlobalTenantResources(
	ctx context.Context,
	obj client.Object,
) []ctrl.Request {
	changed, ok := obj.(*capsulev1beta2.GlobalTenantResource)
	if !ok {
		return nil
	}

	var list capsulev1beta2.GlobalTenantResourceList
	if err := r.client.List(ctx, &list); err != nil {
		return nil
	}

	reqs := make([]ctrl.Request, 0)

	for _, gtr := range list.Items {
		for _, dep := range gtr.Spec.DependsOn {
			if dep.Name.String() == changed.Name {
				reqs = append(reqs, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      gtr.Name,
						Namespace: gtr.Namespace,
					},
				})
				break
			}
		}
	}

	return reqs
}

func (r *globalResourceController) enqueueRequestFromTenant(ctx context.Context, object client.Object) (reqs []reconcile.Request) {
	tnt := object.(*capsulev1beta2.Tenant) //nolint:forcetypeassert

	resList := capsulev1beta2.GlobalTenantResourceList{}
	if err := r.client.List(ctx, &resList); err != nil {
		return nil
	}

	set := sets.NewString()

	for _, res := range resList.Items {
		tntSelector := res.Spec.TenantSelector

		selector, err := metav1.LabelSelectorAsSelector(&tntSelector)
		if err != nil {
			continue
		}

		if selector.Matches(labels.Set(tnt.GetLabels())) {
			set.Insert(res.GetName())
		}
	}
	// No need of ordered value here
	for res := range set {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: res,
			},
		})
	}

	return reqs
}

func (r *globalResourceController) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := ctrllog.FromContext(ctx)

	log.V(5).Info("start processing")
	// Retrieving the GlobalTenantResource
	tntResource := &capsulev1beta2.GlobalTenantResource{}
	if err = r.client.Get(ctx, request.NamespacedName, tntResource); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteMetrics(request.Name)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	patchHelper, err := patch.NewHelper(tntResource, r.client)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to init patch helper")
	}

	defer func() {
		if uerr := r.updateStatus(ctx, tntResource, err); uerr != nil {
			err = fmt.Errorf("cannot update globaltenantresource status: %w", uerr)

			return
		}

		r.metrics.RecordConditions(tntResource)

		if e := patchHelper.Patch(ctx, tntResource); e != nil {
			if err == nil {
				err = gherrors.Wrap(e, "failed to patch GlobalTenantResource")
			}
		}

		// Controller-Runtime should never receive error
		err = nil
	}()

	if *tntResource.Spec.Cordoned {
		log.V(5).Info("tenant resource is cordoned")

		return reconcile.Result{}, err
	}

	for _, dep := range tntResource.Spec.DependsOn {
		d := &capsulev1beta2.GlobalTenantResource{}
		err = r.client.Get(ctx, types.NamespacedName{Name: dep.Name.String(), Namespace: ""}, d)
		if err != nil {
			if apierrors.IsNotFound(err) {
				err = fmt.Errorf("dependency %s not found", dep.Name)
			}

			return reconcile.Result{
				Requeue:      true,
				RequeueAfter: tntResource.Spec.ResyncPeriod.Duration,
			}, err
		}

		stat := d.Status.Conditions.GetConditionByType(meta.ReadyCondition)
		if stat.Status != metav1.ConditionTrue {
			err = fmt.Errorf("dependency %s not ready", dep.Name)

			return reconcile.Result{
				Requeue:      true,
				RequeueAfter: tntResource.Spec.ResyncPeriod.Duration,
			}, err
		}
	}

	err = r.updateReconcilingStatus(ctx, tntResource)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to update status")
	}

	c, err := r.loadClient(ctx, log, tntResource)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to load serviceaccount client")
	}

	if c == nil {
		err = fmt.Errorf("received empty client for serviceaccount")

		return reconcile.Result{}, nil
	}

	err = r.reconcile(ctx, c, tntResource)

	// Finalizers
	if len(tntResource.Status.ProcessedItems) > 0 {
		controllerutil.AddFinalizer(tntResource, meta.ControllerFinalizer)
	} else {
		controllerutil.RemoveFinalizer(tntResource, meta.ControllerFinalizer)
	}

	controllerutil.RemoveFinalizer(tntResource, meta.LegacyResourceFinalizer)

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: tntResource.Spec.ResyncPeriod.Duration,
	}, err
}

func (r *globalResourceController) reconcile(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.GlobalTenantResource,
) (err error) {
	log := ctrllog.FromContext(ctx)

	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0)
	}

	// Retrieving the list of the Tenants up to the selector provided by the GlobalTenantResource resource.
	tntSelector, err := metav1.LabelSelectorAsSelector(&tntResource.Spec.TenantSelector)
	if err != nil {
		log.Error(err, "cannot create MatchingLabelsSelector for Global filtering")

		return err
	}

	// Use Controller Client.
	tntList := capsulev1beta2.TenantList{}
	if err = r.client.List(ctx, &tntList, &client.MatchingLabelsSelector{Selector: tntSelector}); err != nil {
		log.Error(err, "cannot list Tenants matching the provided selector")

		return err
	}

	filtered := make([]capsulev1beta2.Tenant, 0, len(tntList.Items))
	for _, tnt := range tntList.Items {
		if tnt.DeletionTimestamp != nil {
			continue
		}

		filtered = append(filtered, tnt)
	}

	// Always post the processed items, as they allow users to track errors
	defer func() {
		tntResource.AssignTenants(filtered)
	}()

	acc := processor.Accumulator{}
	owner := meta.GetLooseOwnerReference(tntResource)

	// Gather Resources
	if tntResource.ObjectMeta.DeletionTimestamp.IsZero() {
		err := r.gatherResources(
			ctx,
			c,
			log,
			tntResource,
			tntList,
			acc,
		)
		if err != nil {
			return err
		}
	}

	return r.processor.Reconcile(
		ctx,
		log,
		c,
		&tntResource.Status.ProcessedItems,
		acc,
		processor.ProcessorOptions{
			FieldOwnerPrefix: getFieldOwner(tntResource.GetName(), tntResource.GetNamespace()),
			Prune:            *tntResource.Spec.PruningOnDelete,
			Adopt:            *tntResource.Spec.Adopt,
			Force:            *tntResource.Spec.Force,
			Owner:            &owner,
		})
}

func (r *globalResourceController) gatherResources(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
	tnts capsulev1beta2.TenantList,
	acc processor.Accumulator,
) error {

	opts := CollectorOptions{
		Accumulator:                  acc,
		AllowCrossNamespaceSelection: true,
	}

	for index, resource := range tntResource.Spec.Resources {
		ilog := log.WithValues("resource", index)

		for _, tnt := range tnts.Items {
			ilog = log.WithValues("tenant", tnt.GetName())

			var resourceError error

			switch tntResource.Spec.Scope {
			case api.ResourceScopeTenant:
				ilog.V(5).Info("replicating for each tenant")

				opts.Iterator = NewCollectorIteratorOptions(&tnt, nil, resource)

				resourceError = r.collector.Collect(
					ctx,
					c,
					opts,
					tnt,
					strconv.Itoa(index),
					resource,
					nil,
				)
			default:
				ilog.V(5).Info("replicating for each namespace")

				resourceError = r.collector.foreachTenantNamespace(ctx, ilog, c, tnt, resource, strconv.Itoa(index), acc, true)
			}

			// Only start pruning when the resource item itself did not throw an error
			if resourceError != nil {
				return resourceError
			}
		}
	}

	return nil
}

func (r *globalResourceController) loadClient(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
) (client.Client, error) {
	sa := r.impersonatedServiceAccount(ctx, log, tntResource)
	if sa == nil {
		log.V(4).Info("using controller client")

		return r.client, nil
	}

	re, err := r.configuration.ServiceAccountClient(ctx)
	if err != nil {
		log.Error(err, "failed to load impersonated rest client")

		return nil, err
	}

	log.V(5).Info("using impersonation client", "serviceaccount", sa.Name, "namespace", sa.Namespace)

	return r.impersonation.LoadOrCreate(ctx, log, re, r.client.Scheme(), *sa)
}

func (r *globalResourceController) impersonatedServiceAccount(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if sa := tntResource.Spec.ServiceAccount; sa != nil {
		name := sa.Name.String()
		ns := sa.Namespace.String()

		if name == "" || ns == "" {
			log.V(4).Info("serviceAccount reference is set but incomplete; ignoring",
				"name", name, "namespace", ns,
			)

			return nil
		}

		return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      sa.Name,
			Namespace: sa.Namespace,
		}
	}

	cfg := r.configuration.ServiceAccountClientProperties()

	name := cfg.GlobalDefaultServiceAccount.String()
	ns := cfg.GlobalDefaultServiceAccountNamespace.String()

	nameSet := name != ""
	nsSet := ns != ""

	if nameSet != nsSet {
		log.V(2).Info("invalid config: global default service account requires both name and namespace",
			"name", name, "namespace", ns,
		)
		return nil
	}

	if !nameSet && !nsSet {
		return nil
	}

	return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      cfg.GlobalDefaultServiceAccount,
		Namespace: cfg.GlobalDefaultServiceAccountNamespace,
	}
}

func (r *globalResourceController) updateReconcilingStatus(ctx context.Context, instance *capsulev1beta2.GlobalTenantResource) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalTenantResource{}
		if err = r.client.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			return err
		}

		latest.Status.Conditions.UpdateConditionByType(meta.NewReadyConditionReconcilingReason(instance))

		return r.client.Status().Update(ctx, latest)
	})
}

func (r *globalResourceController) updateStatus(ctx context.Context, instance *capsulev1beta2.GlobalTenantResource, reconcileError error) error {
	instance.Status.UpdateStats()

	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalTenantResource{}
		if err = r.client.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		// Set Cordoned Condition
		cordonedCondition := meta.NewCordonedCondition(instance)

		if *instance.Spec.Cordoned {
			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		}

		latest.Status.Conditions.UpdateConditionByType(cordonedCondition)

		return r.client.Status().Update(ctx, latest)
	})
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type namespacedResourceController struct {
	client        client.Client
	log           logr.Logger
	processor     processor.Processor
	collector     Collector
	configuration configuration.Configuration
	metrics       *metrics.TenantResourceRecorder

	impersonation *cache.ImpersonationCache
}

func (r *namespacedResourceController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.client = mgr.GetClient()
	r.processor = processor.Processor{
		Configuration:                r.configuration,
		AllowCrossNamespaceSelection: false,
	}
	r.collector = NewCollector(
		r.client,
		mgr.GetRESTMapper(),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.TenantResource{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.ReconcileRequestedPredicate{},
				),
			),
		).
		Watches(
			&capsulev1beta2.TenantResource{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueDependentTenantResources),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *namespacedResourceController) enqueueDependentTenantResources(
	ctx context.Context,
	obj client.Object,
) []ctrl.Request {
	changed, ok := obj.(*capsulev1beta2.TenantResource)
	if !ok {
		return nil
	}

	var list capsulev1beta2.TenantResourceList
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

func (r *namespacedResourceController) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := ctrllog.FromContext(ctx)

	log.V(5).Info("start processing")
	// Retrieving the TenantResource
	tntResource := &capsulev1beta2.TenantResource{}
	if err := r.client.Get(ctx, request.NamespacedName, tntResource); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteMetrics(request.Name, request.Namespace)

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
			err = fmt.Errorf("cannot update tenantresource status: %w", uerr)

			return
		}

		r.metrics.RecordConditions(tntResource)

		if e := patchHelper.Patch(ctx, tntResource); e != nil {
			if err == nil {
				err = gherrors.Wrap(e, "failed to patch TenantResource")
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
		d := &capsulev1beta2.TenantResource{}
		err = r.client.Get(ctx, types.NamespacedName{Name: dep.Name.String(), Namespace: tntResource.GetNamespace()}, d)
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

		return reconcile.Result{}, err
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

func (r *namespacedResourceController) reconcile(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.TenantResource,
) error {
	log := ctrllog.FromContext(ctx)

	// Adding the default value for the status
	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0)
	}

	// Retrieving the parent of the Tenant Resource:
	// can be owned, or being deployed in one of its Namespace.
	tl := &capsulev1beta2.TenantList{}
	if err := r.client.List(ctx, tl, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", tntResource.GetNamespace())}); err != nil {
		log.Error(err, "unable to detect the Tenant for the given TenantResource")

		return err
	}

	if len(tl.Items) == 0 {
		log.Info("skipping sync, the current Namespace is not belonging to any Tenant")

		return nil
	}

	tnt := tl.Items[0]
	acc := processor.Accumulator{}

	// Gather Resources
	if tntResource.ObjectMeta.DeletionTimestamp.IsZero() {
		err := r.gatherResources(
			ctx,
			c,
			log,
			tntResource,
			tnt,
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
			Owner:            nil,
		})
}

func (r *namespacedResourceController) gatherResources(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
	tnt capsulev1beta2.Tenant,
	acc processor.Accumulator,
) (err error) {
	//replicaAccumulator := processor.Accumulator{}
	//seen := make(map[gvk.ResourceKey]struct{})

	opts := CollectorOptions{
		Accumulator:                  acc,
		AllowCrossNamespaceSelection: false,
	}

	for resourceIndex, resource := range tntResource.Spec.Resources {
		namespaces, err := tenant.CollectTenantNamespaceByLabel(ctx, r.client, tnt, resource.NamespaceSelector)
		if err != nil {
			return err
		}

		for _, ns := range namespaces {
			opts.Iterator = NewCollectorIteratorOptions(&tnt, &ns, resource)

			objs, err := r.collector.CollectNamespacedItems(ctx, c, opts, resource, &ns, tnt)
			if err != nil {
				return err
			}

			for _, obj := range objs {
				for _, innerNs := range namespaces {
					if obj.GetNamespace() == innerNs.GetName() {
						continue
					}

					target := obj.DeepCopy()
					target.SetNamespace(innerNs.GetName())

					log.V(4).Info("adding replication for namespaced item", "name", target.GetName(), "namespace", target.GetNamespace(), "kind", target.GetKind())

					err = r.collector.AddToAccumulation(tnt, &innerNs, opts, resource, target, "sad-1", false)
					if err != nil {
						if err != nil {
							return err
						}

						continue
					}

				}
			}

			err = r.collector.Collect(
				ctx,
				c,
				opts,
				tnt,
				string(resourceIndex),
				resource,
				&ns,
			)
			if err != nil {
				return err
			}

		}
	}

	for index, resource := range tntResource.Spec.Resources {
		err = r.collector.foreachTenantNamespace(ctx, log, c, tnt, resource, strconv.Itoa(index), acc, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *namespacedResourceController) loadClient(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
) (client.Client, error) {
	sa := r.impersonatedServiceAccount(ctx, log, tntResource)
	if sa == nil {
		return r.client, nil
	}

	re, err := r.configuration.ServiceAccountClient(ctx)
	if err != nil {
		log.Error(err, "failed to load impersonated rest client")

		return nil, err
	}

	return r.impersonation.LoadOrCreate(ctx, log, re, r.client.Scheme(), *sa)
}

func (r *namespacedResourceController) impersonatedServiceAccount(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if tntResource.Spec.ServiceAccount != nil {
		return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      tntResource.Spec.ServiceAccount.Name,
			Namespace: meta.RFC1123SubdomainName(tntResource.Namespace),
		}
	}

	cfg := r.configuration.ServiceAccountClientProperties()

	if cfg.TenantDefaultServiceAccount == "" {
		return nil
	}

	return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      cfg.TenantDefaultServiceAccount,
		Namespace: meta.RFC1123SubdomainName(tntResource.Namespace),
	}
}

func (r *namespacedResourceController) updateReconcilingStatus(ctx context.Context, instance *capsulev1beta2.TenantResource) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.TenantResource{}
		if err = r.client.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		latest.Status.Conditions.UpdateConditionByType(meta.NewReadyConditionReconcilingReason(instance))

		return r.client.Status().Update(ctx, latest)
	})
}

func (r *namespacedResourceController) updateStatus(ctx context.Context, instance *capsulev1beta2.TenantResource, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.TenantResource{}
		if err = r.client.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
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

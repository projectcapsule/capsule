// Copyright 2020-2026 Project Capsule Authors
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
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
		GatherClient:                 mgr.GetAPIReader(),
		Mapper:                       mgr.GetRESTMapper(),
	}
	r.collector = NewCollector(
		mgr.GetAPIReader(),
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
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllResources),
			builder.WithPredicates(
				predicates.CapsuleConfigSpecImpersonationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{cfg.ConfigurationName}},
			),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueTenantResourcesForNamespace),
			builder.WithPredicates(
				predicates.LabelPresentPredicate{Label: meta.TenantLabel},
			),
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

// Requeue TenantResources if there is changes to namespaces of the same tenant
// We are not relying on the tenant status, as we might have a terminating lock caused by TenantResources
func (r *namespacedResourceController) enqueueTenantResourcesForNamespace(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil
	}

	labelValue, ok := ns.Labels[meta.TenantLabel]
	if !ok || labelValue == "" {
		return nil
	}

	var namespaces corev1.NamespaceList
	if err := r.client.List(
		ctx,
		&namespaces,
		client.MatchingLabels{meta.TenantLabel: labelValue},
	); err != nil {
		r.log.Error(err, "failed to list namespaces by label", "label", meta.TenantLabel, "value", labelValue)

		return nil
	}

	requests := make([]reconcile.Request, 0, 16)
	seen := make(map[types.NamespacedName]struct{})

	for i := range namespaces.Items {
		var trList capsulev1beta2.TenantResourceList
		if err := r.client.List(
			ctx,
			&trList,
			client.InNamespace(namespaces.Items[i].Name),
		); err != nil {
			r.log.Error(err, "failed to list TenantResources", "namespace", namespaces.Items[i].Name)

			continue
		}

		for j := range trList.Items {
			key := types.NamespacedName{
				Namespace: trList.Items[j].Namespace,
				Name:      trList.Items[j].Name,
			}

			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}

			requests = append(requests, reconcile.Request{NamespacedName: key})
		}
	}

	return requests
}

func (r *namespacedResourceController) enqueueAllResources(ctx context.Context, _ client.Object) []reconcile.Request {
	var list capsulev1beta2.TenantResourceList
	if err := r.client.List(ctx, &list); err != nil {
		r.log.V(1).Error(err, "unable to list TenantResources for config-triggered reconcile")

		return nil
	}

	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      list.Items[i].Name,
				Namespace: list.Items[i].Namespace,
			},
		})
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
		tntResource.Status.ProcessedItems = make([]meta.ObjectReferenceStatus, 0)
	}

	// Retrieving the parent of the Tenant Resource:
	// can be owned, or being deployed in one of its Namespace.
	// we cant resolve via status.namespaces, as when a namespace is deleted it is no longer references by the tenant
	// causing a deletion blockade.
	ns := &corev1.Namespace{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: tntResource.GetNamespace()}, ns); err != nil {
		return err
	}

	tnt, err := tenant.GetTenantByOwnerreferences(ctx, r.client, ns.GetOwnerReferences())
	if err != nil {
		return err
	}

	if tnt == nil {
		log.Info("skipping sync, the current Namespace is not belonging to any Tenant")

		return nil
	}

	acc := processor.Accumulator{}

	// Gather Resources
	if tntResource.ObjectMeta.DeletionTimestamp.IsZero() {
		err := r.gatherResources(
			ctx,
			c,
			log,
			tntResource,
			*tnt,
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
			Adopt:            *tntResource.Spec.Settings.Adopt,
			Force:            *tntResource.Spec.Settings.Force,
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
	opts := CollectorOptions{
		Accumulator:                  acc,
		AllowCrossNamespaceSelection: false,
	}

	for resourceIndex, resource := range tntResource.Spec.Resources {
		objs, err := r.collector.CollectNamespacedItems(ctx, c, opts, resource, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tntResource.GetNamespace()}}, tnt)
		if err != nil {
			return err
		}

		for g := range objs {
			log.V(5).Info("found replication source object", "name", g.Name, "namespace", g.Namespace, "kind", g.Kind)
		}

		namespaces, err := r.collector.selectedTenantNamespaces(ctx, log, tnt, resource)
		if err != nil {
			return err
		}

		i := 0
		for _, innerNs := range namespaces {
			opts.Iterator = NewCollectorIteratorOptions(&tnt, innerNs, resource)

			for _, obj := range objs {
				if obj.GetNamespace() == innerNs.GetName() {
					continue
				}

				target := obj.DeepCopy()
				sanitize.SanitizeObject(target, c.Scheme(), r.collector.objectSanitizeOptions)
				target.SetNamespace(innerNs.GetName())

				log.V(4).Info("adding replication for namespaced item", "name", target.GetName(), "namespace", target.GetNamespace(), "kind", target.GetKind())

				err = r.collector.AddToAccumulation(&tnt, innerNs, opts, resource, target, "replica", false)
				if err != nil {
					return err
				}
			}

			err = r.collector.Collect(
				ctx,
				c,
				opts,
				&tnt,
				strconv.Itoa((resourceIndex)),
				resource,
				innerNs,
			)
			if err != nil {
				return err
			}

			i++
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
		sa, ns := configuration.ControllerServiceAccount()

		tntResource.Status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name(sa),
			Namespace: meta.RFC1123SubdomainName(ns),
		}

		return r.client, nil
	}

	tntResource.Status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      sa.Name,
		Namespace: sa.Namespace,
	}

	re, err := r.configuration.ServiceAccountClient(ctx)
	if err != nil {
		log.Error(err, "failed to load impersonated rest client")

		return nil, err
	}

	log.V(5).Info("using impersonation client", "serviceaccount", sa.Name, "namespace", sa.Namespace)

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
	instance.Status.UpdateStats()

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

		if err := r.client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		instance.Status = latest.Status

		return nil
	})
}

func ForeachNamespace(
	ctx context.Context,
	controllerClient client.Client,
	resourceClient client.Client,
	collector Collector,
	opts CollectorOptions,
	log logr.Logger,
	resource capsulev1beta2.ResourceSpec,
	resourceIndex int,
	tnt capsulev1beta2.Tenant,
	acc processor.Accumulator,
) (err error) {
	namespaces, err := tenant.CollectTenantNamespaceByLabel(ctx, controllerClient, tnt, resource.NamespaceSelector)
	if err != nil {
		return err
	}

	for _, ns := range namespaces {
		if ns.DeletionTimestamp != nil {
			terminating, err := tenant.NamespaceIsPendingUnmanagedTerminationByStatus(ctx, controllerClient, &ns)
			if err != nil {
				return err
			}

			// Skip this namespace so resources are cleaned
			if terminating {
				continue
			}
		}

		opts.Iterator = NewCollectorIteratorOptions(&tnt, &ns, resource)

		objs, err := collector.CollectNamespacedItems(ctx, resourceClient, opts, resource, &ns, tnt)
		if err != nil {
			return err
		}

		i := 0

		for _, obj := range objs {
			for _, innerNs := range namespaces {
				if obj.GetNamespace() == innerNs.GetName() {
					continue
				}

				target := obj.DeepCopy()
				target.SetNamespace(innerNs.GetName())

				log.V(4).Info("adding replication for namespaced item", "name", target.GetName(), "namespace", target.GetNamespace(), "kind", target.GetKind())

				err = collector.AddToAccumulation(&tnt, &innerNs, opts, resource, target, strconv.Itoa(resourceIndex)+"/replica-"+strconv.Itoa(i), true)
				if err != nil {
					return err
				}
			}

			i++
		}

		err = collector.Collect(
			ctx,
			resourceClient,
			opts,
			&tnt,
			strconv.Itoa((resourceIndex)),
			resource,
			&ns,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

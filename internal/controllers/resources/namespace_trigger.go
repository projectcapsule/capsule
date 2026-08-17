// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// NamespaceTrigger is watching for Namespace events, and trigger the
// GlobalTenantResource or TenantResource target the watched object:
// this is used to propagate resources in new freshly created Namespace
// without waiting for the resyncPeriod of the Resources.
type NamespaceTrigger struct {
	client        client.Client
	reader        client.Reader
	log           logr.Logger
	configuration configuration.Configuration
	impersonation *cache.ImpersonationCache
	processor     processor.Processor
	collector     Collector

	globalClients     impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]
	namespacedClients impersonatedClientLoader[*capsulev1beta2.TenantResource]
	globalStatus      scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]
	namespacedStatus  scopedStatusPatcher[*capsulev1beta2.TenantResource]
}

func (r *NamespaceTrigger) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	var namespace corev1.Namespace
	if err := r.client.Get(ctx, request.NamespacedName, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(5).Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	tntName, found := namespace.GetLabels()[meta.TenantLabel]
	if !found {
		log.V(5).Info("cannot retrieve Tenant ownership from Namespace's Tenant label")

		return reconcile.Result{}, nil
	}

	var tnt capsulev1beta2.Tenant
	if err := r.client.Get(ctx, types.NamespacedName{Name: tntName}, &tnt); err != nil {
		return reconcile.Result{}, err
	}

	log = log.WithValues("tenant", tnt.GetName())

	syncErr := errors.Join(
		r.replicateNamespacedResources(ctx, log, tnt, &namespace),
		r.replicateGlobalResources(ctx, log, tnt, &namespace),
	)

	return reconcile.Result{}, syncErr
}

func (r *NamespaceTrigger) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.client = mgr.GetClient()
	r.reader = mgr.GetAPIReader()

	r.processor = processor.Processor{
		Configuration:                r.configuration,
		GatherClient:                 mgr.GetAPIReader(),
		AllowCrossNamespaceSelection: true,

		Mapper: mgr.GetRESTMapper(),
	}
	r.collector = NewCollector(
		mgr.GetAPIReader(),
		mgr.GetRESTMapper(),
	)
	r.globalClients = impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{
		client:        r.client,
		configuration: r.configuration,
		impersonation: r.impersonation,
		resolve:       globalServiceAccount,
	}
	r.namespacedClients = impersonatedClientLoader[*capsulev1beta2.TenantResource]{
		client:        r.client,
		configuration: r.configuration,
		impersonation: r.impersonation,
		resolve:       namespacedServiceAccount,
	}
	r.globalStatus = scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]{
		client:  r.client,
		reader:  r.reader,
		factory: func() *capsulev1beta2.GlobalTenantResource { return &capsulev1beta2.GlobalTenantResource{} },
		status: func(o *capsulev1beta2.GlobalTenantResource) *capsulev1beta2.TenantResourceCommonStatus {
			return &o.Status.TenantResourceCommonStatus
		},
	}
	r.namespacedStatus = scopedStatusPatcher[*capsulev1beta2.TenantResource]{
		client:  r.client,
		reader:  r.reader,
		factory: func() *capsulev1beta2.TenantResource { return &capsulev1beta2.TenantResource{} },
		status: func(o *capsulev1beta2.TenantResource) *capsulev1beta2.TenantResourceCommonStatus {
			return &o.Status.TenantResourceCommonStatus
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("NamespaceWatcher").
		For(
			&corev1.Namespace{},
			// Only the Namespaces of a Tenant which are created from now on: the already
			// existing ones are covered by the full reconciliation of the resources.
			builder.WithPredicates(
				predicates.LabelPresentPredicate{Label: meta.TenantLabel},
				predicates.NewCreatedAfterPredicate(time.Now()),
			),
		).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
}

// Replicates into the given Namespace every TenantResource of the Tenant.
func (r *NamespaceTrigger) replicateNamespacedResources(
	ctx context.Context,
	log logr.Logger,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
) (syncErr error) {
	var tr capsulev1beta2.TenantResourceList

	nsSet := sets.New[string](tnt.Status.Namespaces...)
	nsSet.Insert(namespace.GetName())

	for ns := range nsSet {
		var list capsulev1beta2.TenantResourceList
		if err := r.client.List(ctx, &list, client.InNamespace(ns)); err != nil {
			log.Error(err, "cannot retrieve TenantResourceList", "namespace", ns)

			return err
		}

		tr.Items = append(tr.Items, list.Items...)
	}

	for _, tntResource := range tr.Items {
		ilog := log.WithValues("tenantresource", tntResource.GetName(), "source", tntResource.GetNamespace())

		if skip, reason := skipScopedReplication(&tntResource, &tntResource.Spec.TenantResourceCommonSpec); skip {
			ilog.V(5).Info("TenantResource is not replicating, ignoring", "reason", reason)

			continue
		}

		if err := r.replicateNamespaced(ctx, ilog, &tntResource, tnt, namespace); err != nil {
			ilog.Error(err, "cannot replicate the TenantResource on the Namespace")

			syncErr = errors.Join(syncErr, err)
		}
	}

	return syncErr
}

// Replicates into the given Namespace every GlobalTenantResource selecting the Tenant.
func (r *NamespaceTrigger) replicateGlobalResources(
	ctx context.Context,
	log logr.Logger,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
) (syncErr error) {
	var gtr capsulev1beta2.GlobalTenantResourceList
	if err := r.client.List(ctx, &gtr); err != nil {
		log.Error(err, "cannot retrieve GlobalTenantResourceList")

		return err
	}

	for _, tntResource := range gtr.Items {
		ilog := log.WithValues("globaltenantresource", tntResource.GetName())

		selector, err := metav1.LabelSelectorAsSelector(&tntResource.Spec.TenantSelector)
		if err != nil {
			ilog.Error(err, "cannot create MatchingLabelsSelector for Global filtering")

			continue
		}

		if !selector.Matches(labels.Set(tnt.GetLabels())) {
			ilog.V(5).Info("Tenant is not selected by the GlobalTenantResource, ignoring")

			continue
		}

		if skip, reason := skipGlobalNamespaceReplication(tntResource); skip {
			ilog.V(5).Info("GlobalTenantResource is not replicating on a Namespace basis, ignoring", "reason", reason)

			continue
		}

		if err := r.replicateGlobal(ctx, ilog, &tntResource, tnt, namespace); err != nil {
			ilog.Error(err, "cannot replicate the GlobalTenantResource on the Namespace")

			syncErr = errors.Join(syncErr, err)
		}
	}

	return syncErr
}

// States whether the given GlobalTenantResource is out of the Namespace scoped
// replication duties, along with the reason.
func skipGlobalNamespaceReplication(tntResource capsulev1beta2.GlobalTenantResource) (bool, string) {
	// Any other scope is not replicating on a per Namespace basis: the accumulated items
	// would not be addressable by a Namespace scope at all.
	if tntResource.Spec.Scope != api.ResourceScopeNamespace {
		return true, "scope is " + string(tntResource.Spec.Scope)
	}

	return skipScopedReplication(&tntResource, &tntResource.Spec.TenantResourceCommonSpec)
}

// Replicates a single GlobalTenantResource into the given Namespace of the given Tenant,
// reporting the outcome on the status without touching the items of the other Namespaces.
func (r *NamespaceTrigger) replicateGlobal(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
) error {
	// The resolved ServiceAccount is intentionally discarded: posting it to the status is
	// a duty of the GlobalTenantResource controller.
	c, _, err := r.globalClients.Load(ctx, log, tntResource)
	if err != nil {
		return fmt.Errorf("failed to load serviceaccount client: %w", err)
	}

	scope := processor.Scope{
		Tenant:    tnt.GetName(),
		Namespace: namespace.GetName(),
	}

	acc := processor.Accumulator{}

	// Bailing out on a partial accumulation, as the full reconciliation does: reporting it
	// would drop the status entries of the items which could not be gathered, making the
	// full reconciliation lose track of them.
	if err := r.gatherGlobalResources(ctx, c, log, tntResource, tnt, namespace, acc); err != nil {
		return fmt.Errorf("failed to gather resources: %w", err)
	}

	owner := meta.GetLooseOwnerReference(tntResource)

	items, reconcileErr := r.processor.ReconcileNamespace(
		ctx,
		log,
		c,
		tntResource.Status.ProcessedItems,
		acc,
		scopedProcessorOptions(tntResource, &tntResource.Spec.TenantResourceCommonSpec, &owner),
		scope,
	)

	// The items are reported even along an error, since they carry the outcome of the
	// single objects which have been processed.
	statusErr := r.globalStatus.Patch(ctx, tntResource, scope, items)

	return errors.Join(reconcileErr, statusErr)
}

// Replicates a single TenantResource into the given Namespace of the given Tenant,
// reporting the outcome on the status without touching the items of the other Namespaces.
func (r *NamespaceTrigger) replicateNamespaced(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
) error {
	c, _, err := r.namespacedClients.Load(ctx, log, tntResource)
	if err != nil {
		return fmt.Errorf("failed to load serviceaccount client: %w", err)
	}

	scope := processor.Scope{
		Tenant:    tnt.GetName(),
		Namespace: namespace.GetName(),
	}

	acc := processor.Accumulator{}

	if err := r.gatherNamespacedResources(ctx, c, log, tntResource, tnt, namespace, acc); err != nil {
		return fmt.Errorf("failed to gather resources: %w", err)
	}

	// A TenantResource is namespaced, hence it cannot own objects living in another
	// Namespace: no owner reference is set, mirroring the full reconciliation.
	items, reconcileErr := r.processor.ReconcileNamespace(
		ctx,
		log,
		c,
		tntResource.Status.ProcessedItems,
		acc,
		scopedProcessorOptions(tntResource, &tntResource.Spec.TenantResourceCommonSpec, nil),
		scope,
	)

	statusErr := r.namespacedStatus.Patch(ctx, tntResource, scope, items)

	return errors.Join(reconcileErr, statusErr)
}

// Accumulates the items of a GlobalTenantResource the given Namespace must be holding.
func (r *NamespaceTrigger) gatherGlobalResources(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
	acc processor.Accumulator,
) error {
	opts := CollectorOptions{
		Accumulator:               acc,
		AllowClusterScopedObjects: true,
	}

	for resourceIndex, resource := range tntResource.Spec.Resources {
		targeted, err := r.targetsNamespace(log, tnt, resource, namespace, resourceIndex)
		if err != nil {
			return err
		}

		if !targeted {
			continue
		}

		// Sources are loaded cluster-wide, as they can live outside of the target Namespace.
		opts.AllowCrossNamespaceSelection = true

		sources, err := r.collector.CollectNamespacedItems(ctx, c, opts, resource, nil, tnt)
		if err != nil {
			return err
		}

		opts.AllowCrossNamespaceSelection = false

		if err := r.collector.CollectForNamespace(
			ctx,
			c,
			opts,
			tnt,
			strconv.Itoa(resourceIndex),
			resource,
			sources,
			namespace,
		); err != nil {
			return err
		}
	}

	return nil
}

// States whether the given resource specification is replicating on the given Namespace.
func (r *NamespaceTrigger) targetsNamespace(
	log logr.Logger,
	tnt capsulev1beta2.Tenant,
	resource capsulev1beta2.ResourceSpec,
	namespace *corev1.Namespace,
	resourceIndex int,
) (bool, error) {
	selector, err := r.collector.namespaceSelector(tnt, resource)
	if err != nil {
		return false, err
	}

	if !selector.Matches(labels.Set(namespace.GetLabels())) {
		log.V(5).Info("Namespace is not targeted by the resource, ignoring", "resource", resourceIndex)

		return false, nil
	}

	return true, nil
}

// Accumulates the items of a TenantResource the given Namespace must be holding.
func (r *NamespaceTrigger) gatherNamespacedResources(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
	tnt capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
	acc processor.Accumulator,
) error {
	// The Namespace has just been created, thus it may not have landed on the Tenant status
	// yet: it is a legit replication target nonetheless, and the validator must know about it
	// to not reject the items referring to it.
	allowed := sets.New[string](tnt.Status.Namespaces...)
	allowed.Insert(namespace.GetName())

	// The very same boundaries of the full reconciliation: a Tenant owner must not be able to
	// select cluster scoped objects, nor objects living outside of its own Namespaces.
	opts := CollectorOptions{
		Accumulator:                  acc,
		AllowCrossNamespaceSelection: false,
		AllowClusterScopedObjects:    false,
		ValidatorNamespaces:          tpl.NewNamespaceValidator(false, allowed),
	}

	// The sources of a TenantResource always live in the Namespace it is deployed in.
	source := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: tntResource.GetNamespace()},
	}

	for resourceIndex, resource := range tntResource.Spec.Resources {
		targeted, err := r.targetsNamespace(log, tnt, resource, namespace, resourceIndex)
		if err != nil {
			return err
		}

		if !targeted {
			continue
		}

		sources, err := r.collector.CollectNamespacedItems(ctx, c, opts, resource, source, tnt)
		if err != nil {
			return err
		}

		if err := r.collector.CollectForNamespace(
			ctx,
			c,
			opts,
			tnt,
			strconv.Itoa(resourceIndex),
			resource,
			sources,
			namespace,
		); err != nil {
			return err
		}
	}

	return nil
}

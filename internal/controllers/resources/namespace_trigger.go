// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// NamespaceTrigger is watching for Namespace events, and trigger the
// GlobalTenantResource or TenantResource target the watched object:
// this is used to propagate resources in new freshly created Namespace
// without waiting for the resyncPeriod of the Resources.
type NamespaceTrigger struct {
	bootstrapConfig  *rest.Config
	bootstraps       namespaceBootstraps
	bootstrapWorkers int
	applied          ssa.ApplyCache
	sources          *namespaceSourceIndex
	selectors        *cache.LabelSelectorCache
	client           client.Client
	reader           client.Reader
	log              logr.Logger
	configuration    configuration.Configuration
	impersonation    *cache.ImpersonationCache
	conditions       *cache.CELCache
	triggers         *cache.PreparedReplicationTriggers
	processor        processor.Processor
	collector        Collector

	globalClients     impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]
	namespacedClients impersonatedClientLoader[*capsulev1beta2.TenantResource]
	globalStatus      scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]
	namespacedStatus  scopedStatusPatcher[*capsulev1beta2.TenantResource]
}

func (r *NamespaceTrigger) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if request.Namespace == "bootstrap" || strings.HasPrefix(request.Namespace, "bootstrap-") {
		return r.reconcileBootstrap(ctx, request)
	}

	return r.reconcileNamespace(ctx, request)
}

func (r *NamespaceTrigger) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.bootstrapWorkers = max(1, ctrlConfig.Runtime.MaxConcurrentReconciles)
	r.client = mgr.GetClient()
	r.reader = mgr.GetAPIReader()

	bootstrapClient := r.client

	if r.bootstrapConfig != nil {
		var err error

		bootstrapClient, err = client.New(r.bootstrapConfig, client.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()})
		if err != nil {
			return fmt.Errorf("create namespace bootstrap client: %w", err)
		}

		r.reader = bootstrapClient
	}

	r.processor = processor.Processor{
		Applied:                      r.applied,
		ReplicationOwners:            ssa.NewReplicationOwnerResolver(mgr.GetClient(), mgr.GetAPIReader()),
		Conditions:                   r.conditions,
		Configuration:                r.configuration,
		GatherClient:                 r.reader,
		AllowCrossNamespaceSelection: true,

		Mapper: mgr.GetRESTMapper(),
	}
	r.collector = NewCollector(
		r.reader,
		mgr.GetRESTMapper(),
	)
	r.globalClients = impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{
		client:        replicationClient{Client: bootstrapClient, reader: r.reader, identity: mgr.GetConfig()},
		configuration: r.configuration,
		impersonation: r.impersonation,
		resolve:       globalServiceAccount,
	}
	r.namespacedClients = impersonatedClientLoader[*capsulev1beta2.TenantResource]{
		client:        replicationClient{Client: bootstrapClient, reader: r.reader, identity: mgr.GetConfig()},
		configuration: r.configuration,
		impersonation: r.impersonation,
		resolve:       namespacedServiceAccount,
	}
	r.globalStatus = scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]{
		client:  bootstrapClient,
		reader:  r.reader,
		factory: func() *capsulev1beta2.GlobalTenantResource { return &capsulev1beta2.GlobalTenantResource{} },
		status: func(o *capsulev1beta2.GlobalTenantResource) *capsulev1beta2.TenantResourceCommonStatus {
			return &o.Status.TenantResourceCommonStatus
		},
	}
	r.namespacedStatus = scopedStatusPatcher[*capsulev1beta2.TenantResource]{
		client:  bootstrapClient,
		reader:  r.reader,
		factory: func() *capsulev1beta2.TenantResource { return &capsulev1beta2.TenantResource{} },
		status: func(o *capsulev1beta2.TenantResource) *capsulev1beta2.TenantResourceCommonStatus {
			return &o.Status.TenantResourceCommonStatus
		},
	}
	r.selectors = cache.NewLabelSelectorCache()

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/replications/namespace-trigger").
		Watches(&corev1.Namespace{}, r.bootstrapHandler()).
		Watches(&capsulev1beta2.Tenant{}, r.bootstrapTenantHandler()).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *NamespaceTrigger) reconcileNamespace(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
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
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	owned := false

	for _, owner := range namespace.OwnerReferences {
		if tenant.IsTenantOwnerReference(owner) && owner.Name == tnt.Name && owner.UID == tnt.UID {
			owned = true

			break
		}
	}

	if !owned || !namespace.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	log = log.WithValues("tenant", tnt.GetName())

	syncErr := errors.Join(
		r.replicateNamespacedResources(ctx, log, tnt, &namespace),
		r.replicateGlobalResources(ctx, log, tnt, &namespace),
	)

	return reconcile.Result{}, syncErr
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

	var err error

	tr.Items, err = r.sources.list(ctx, r.client, nsSet)
	if err != nil {
		return err
	}

	ownership := make(map[string]bool)

	for _, tntResource := range tr.Items {
		owned, checked := ownership[tntResource.Namespace]
		if !checked {
			owned, err = r.sourceNamespaceOwned(ctx, tntResource.Namespace, tnt)
			if err != nil {
				return err
			}

			ownership[tntResource.Namespace] = owned
		}

		if !owned {
			continue
		}

		ilog := log.WithValues("tenantresource", tntResource.GetName(), "source", tntResource.GetNamespace())

		if skip, reason := skipScopedReplication(&tntResource, &tntResource.Spec.TenantResourceCommonSpec); skip {
			ilog.V(5).Info("TenantResource is not replicating, ignoring", "reason", reason)

			continue
		}

		if err := r.replicateNamespaced(ctx, ilog, &tntResource, tnt, namespace); err != nil {
			ilog.Error(err, "cannot replicate the TenantResource on the Namespace")
			// The source controller owns durable failure reporting and retries.
			// A broken source must not hold every new namespace in this Tenant
			// behind the same bootstrap workqueue key.
			fallback := meta.TriggerRequestReconcileAnnotation(ctx, replicationClient{Client: r.client, reader: r.reader}, capsulev1beta2.GroupVersion.WithKind("TenantResource"), client.ObjectKeyFromObject(&tntResource))
			syncErr = errors.Join(syncErr, client.IgnoreNotFound(fallback))
		}
	}

	return syncErr
}

// Tenant status and informers can lag namespace reassignment independently. A
// source selected through that relationship must still belong to this Tenant.
func (r *NamespaceTrigger) sourceNamespaceOwned(ctx context.Context, name string, tnt capsulev1beta2.Tenant) (bool, error) {
	var namespace corev1.Namespace
	if err := r.reader.Get(ctx, client.ObjectKey{Name: name}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	if !namespace.DeletionTimestamp.IsZero() {
		return false, nil
	}

	for _, owner := range namespace.OwnerReferences {
		if tenant.IsTenantOwnerReference(owner) && owner.Name == tnt.Name && owner.UID == tnt.UID {
			return true, nil
		}
	}

	return false, nil
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

		selector, err := r.tenantSelector(&tntResource.Spec.TenantSelector)
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

			fallback := meta.TriggerRequestReconcileAnnotation(ctx, replicationClient{Client: r.client, reader: r.reader}, capsulev1beta2.GroupVersion.WithKind("GlobalTenantResource"), client.ObjectKeyFromObject(&tntResource))
			syncErr = errors.Join(syncErr, client.IgnoreNotFound(fallback))
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
	if err := validateReplicationChecks(tntResource, &tntResource.Spec.TenantResourceCommonSpec, false, r.triggers, r.conditions); err != nil {
		return err
	}

	// The resolved ServiceAccount is intentionally discarded: posting it to the status is
	// a duty of the GlobalTenantResource controller.
	if err := r.bootstrapDependenciesReady(ctx, tntResource); err != nil {
		return err
	}

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

	if len(acc) == 0 {
		return nil
	}

	if !controllerutil.ContainsFinalizer(tntResource, meta.ControllerFinalizer) {
		return fmt.Errorf("replication cleanup has not been initialized")
	}

	owner := meta.GetLooseOwnerReference(tntResource)

	items, reconcileErr := r.processor.ReconcileNamespace(
		ctx,
		log,
		c,
		tntResource.Status.ProcessedItems,
		acc,
		replicationProcessorOptions(tntResource, &tntResource.Spec.TenantResourceCommonSpec, &owner),
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
	if err := validateReplicationChecks(tntResource, &tntResource.Spec.TenantResourceCommonSpec, true, r.triggers, r.conditions); err != nil {
		return err
	}

	if err := r.bootstrapDependenciesReady(ctx, tntResource); err != nil {
		return err
	}

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

	if len(acc) == 0 {
		return nil
	}

	if !controllerutil.ContainsFinalizer(tntResource, meta.ControllerFinalizer) {
		return fmt.Errorf("replication cleanup has not been initialized")
	}

	// A TenantResource is namespaced, hence it cannot own objects living in another
	// Namespace: no owner reference is set, mirroring the full reconciliation.
	items, reconcileErr := r.processor.ReconcileNamespace(
		ctx,
		log,
		c,
		tntResource.Status.ProcessedItems,
		acc,
		replicationProcessorOptions(tntResource, &tntResource.Spec.TenantResourceCommonSpec, nil),
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
	replicationContext, err := newReplicationContext(tntResource)
	if err != nil {
		return err
	}

	opts := CollectorOptions{
		Accumulator:               acc,
		AllowClusterScopedObjects: true,
		ReplicationContext:        replicationContext,
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
	replicationContext, err := newReplicationContext(tntResource)
	if err != nil {
		return err
	}

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
		ReplicationContext:           replicationContext,
		ValidatorNamespaces:          tpl.NewNamespaceValidator(false, allowed),
	}

	// The sources of a TenantResource always live in the Namespace it is deployed in.
	source := &corev1.Namespace{
		Name: tntResource.GetNamespace(),
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

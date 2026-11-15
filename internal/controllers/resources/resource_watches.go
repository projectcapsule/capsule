// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
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
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

const maxResourceWatches = 64

type resourceWatchKey struct {
	resource  schema.GroupVersionResource
	namespace string
	identity  resourceWatchIdentity
	caBundle  [sha256.Size]byte
}

// ResourceWatches shares namespace-scoped watch streams on the elected replica.
// Stream sharing preserves each subscription's execution identity and scope.
// The controller maintains the watch set; event handling uses the registered
// manager-cache index and immutable prepared source projections.
type ResourceWatches struct {
	newStream         func(context.Context, resourceWatchKey, schema.GroupVersionKind) (resourceEventSource, error)
	client            client.Client
	configuration     configuration.Configuration
	configurationName string
	baseREST          *rest.Config
	dynamic           dynamic.Interface
	mapper            apimeta.RESTMapper
	log               logr.Logger
	conditions        *cache.CELCache
	selectors         *cache.LabelSelectorCache
	prepared          *cache.PreparedReplicationTriggers
	notifier          replication.Notifier
	metrics           *metrics.TriggerRecorder
	mu                sync.Mutex
	desired           map[resourceWatchKey]schema.GroupVersionKind
	changed           chan struct{}
	events            chan resourceWatchEvent
	eventBytes        atomic.Int64
	listSlots         chan struct{}
	now               func() time.Time
	rotationDue       time.Time
	priorityGroup     string
	nextPriorityGroup string
}

func NewResourceWatches(c client.Client, dyn dynamic.Interface, mapper apimeta.RESTMapper, log logr.Logger, cfg configuration.Configuration, conditions *cache.CELCache, selectors *cache.LabelSelectorCache, prepared *cache.PreparedReplicationTriggers, notifier replication.Notifier, recorder *metrics.TriggerRecorder) *ResourceWatches {
	return &ResourceWatches{client: c, dynamic: dyn, mapper: mapper, log: log, configuration: cfg, conditions: conditions, selectors: selectors, prepared: prepared, notifier: notifier, metrics: recorder, desired: make(map[resourceWatchKey]schema.GroupVersionKind), changed: make(chan struct{}, 1), events: make(chan resourceWatchEvent, 256), listSlots: make(chan struct{}, 2), now: time.Now}
}

func (r *ResourceWatches) NeedLeaderElection() bool { return true }

func (r *ResourceWatches) SetupWithManager(mgr ctrl.Manager, options utils.ControllerOptions) error {
	r.configurationName = options.ConfigurationName
	r.baseREST = rest.CopyConfig(mgr.GetConfig())
	// One controller-identity client shares its transport and rate limiter across
	// all streams. The bounded response wrapper applies only to watch LIST calls.
	watchConfig := rest.CopyConfig(r.baseREST)
	limitWatchListResponses(watchConfig)

	watchClient, err := dynamic.NewForConfig(watchConfig)
	if err != nil {
		return fmt.Errorf("create bounded resource watch client: %w", err)
	}

	r.dynamic = watchClient

	enqueue := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{Name: "resource-watches"}}
	})

	if err := ctrl.NewControllerManagedBy(mgr).Named("capsule/resources/watches").
		Watches(&capsulev1beta2.TenantResource{}, enqueue, builder.WithPredicates(watchSubscriptionChanged())).
		Watches(&capsulev1beta2.GlobalTenantResource{}, enqueue, builder.WithPredicates(watchSubscriptionChanged())).
		Watches(&capsulev1beta2.CapsuleConfiguration{}, enqueue, builder.WithPredicates(predicates.CapsuleConfigSpecImpersonationChangedPredicate{}, predicates.NamesMatchingPredicate{Names: []string{options.ConfigurationName}})).
		WithOptions(options.Runtime.ToControllerOptions()).Complete(reconcile.Func(func(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
		return r.Reconcile(ctx, reconcile.Request{})
	})); err != nil {
		return err
	}

	return mgr.Add(r)
}

func triggerSpec(obj client.Object) *capsulev1beta2.TenantResourceCommonSpec {
	switch source := obj.(type) {
	case *capsulev1beta2.TenantResource:
		return &source.Spec.TenantResourceCommonSpec
	case *capsulev1beta2.GlobalTenantResource:
		return &source.Spec.TenantResourceCommonSpec
	default:
		return nil
	}
}

func watchSubscriptionChanged() predicate.Predicate {
	hasTriggers := func(obj client.Object) bool {
		spec := triggerSpec(obj)

		return spec != nil && len(spec.Triggers) > 0
	}

	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return hasTriggers(e.Object) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return hasTriggers(e.Object) },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return (hasTriggers(e.ObjectOld) || hasTriggers(e.ObjectNew)) && (e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || e.ObjectOld.GetDeletionTimestamp().IsZero() != e.ObjectNew.GetDeletionTimestamp().IsZero())
		},
	}
}

func (r *ResourceWatches) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	if r.configuration == nil {
		return reconcile.Result{}, fmt.Errorf("watch execution configuration is unavailable")
	}

	local := &capsulev1beta2.TenantResourceList{}
	global := &capsulev1beta2.GlobalTenantResourceList{}

	filter := client.MatchingFields{replication.TriggerIndexerField: replication.TriggerEnabledKey}
	if err := r.client.List(ctx, local, filter, client.UnsafeDisableDeepCopy); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.client.List(ctx, global, filter, client.UnsafeDisableDeepCopy); err != nil {
		return reconcile.Result{}, err
	}

	active := make(map[types.UID]int64, len(local.Items)+len(global.Items))

	sources := make([]client.Object, 0, len(local.Items)+len(global.Items))
	for i := range local.Items {
		sources = append(sources, &local.Items[i])
		active[local.Items[i].UID] = local.Items[i].Generation
	}

	for i := range global.Items {
		sources = append(sources, &global.Items[i])
		active[global.Items[i].UID] = global.Items[i].Generation
	}

	r.prepared.PruneActive(active)
	sort.Slice(sources, func(i, j int) bool {
		a, b := sources[i], sources[j]

		return a.GetNamespace()+"/"+a.GetName() < b.GetNamespace()+"/"+b.GetName()
	})

	plan, omitted, err := r.planSources(ctx, sources)
	if err != nil {
		return reconcile.Result{}, err
	}

	retain := make(map[types.UID]struct{}, len(plan.selected))
	for _, source := range plan.selected {
		retain[source.GetUID()] = struct{}{}
	}

	r.prepared.RetainPrepared(retain)

	groups := make(map[string]*watchKeySet)
	opportunity := make(map[string]bool)

	props, err := r.watchProperties(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, source := range plan.selected {
		owner := plan.owners[source.GetUID()]

		prepared, err := r.prepareSource(source)
		if err != nil {
			omitted++
			opportunity[owner] = true

			r.log.Error(err, "cannot prepare resource watches; periodic reconciliation remains active", "name", source.GetName(), "namespace", source.GetNamespace())

			continue
		}

		identity := watchIdentity(source, props)

		resources := prepared.Resources()
		if len(resources) == 0 {
			opportunity[owner] = true
		}

		for _, resource := range resources {
			group := groups[owner]
			if group == nil {
				group = &watchKeySet{}
				groups[owner] = group
			}

			group.add(resourceWatchKey{resource: resource, namespace: source.GetNamespace(), identity: identity})
		}
	}

	selected, capacityOmitted := planWatchKeys(plan.order, groups, opportunity)
	omitted += capacityOmitted

	r.nextWatchGroup(plan.order, opportunity)

	desired := make(map[resourceWatchKey]schema.GroupVersionKind)
	trust := watchTrustRevision{}

	type discovered struct {
		kind schema.GroupVersionKind
		err  error
	}

	resources := make(map[schema.GroupVersionResource]discovered)
	for key := range selected {
		mapping, known := resources[key.resource]
		if !known {
			mapping.kind, mapping.err = r.discoverWatchResource(key.resource)

			resources[key.resource] = mapping

			if mapping.err != nil {
				r.log.Error(mapping.err, "cannot discover trigger resource", "resource", key.resource.String())
			}
		}

		if mapping.err != nil {
			omitted++

			continue
		}

		trusted, err := r.withWatchTrust(ctx, key, &trust)
		if err != nil {
			omitted++

			continue
		}

		desired[trusted] = mapping.kind
	}

	r.metrics.OmittedWatches.Set(float64(omitted))
	r.mu.Lock()
	r.desired = desired
	r.mu.Unlock()

	// Also retry streams whose construction failed (for example while the
	// configured CA Secret was unavailable). Healthy streams are reused.
	select {
	case r.changed <- struct{}{}:
	default:
	}

	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ResourceWatches) prepareSource(source client.Object) (*replication.PreparedTriggers, error) {
	if prepared, ok := r.prepared.Get(source.GetUID(), source.GetGeneration()); ok {
		return prepared, nil
	}

	spec := triggerSpec(source)
	count := 0

	for _, trigger := range spec.Triggers {
		count += len(trigger.MatchConditions)
	}

	encoded, err := json.Marshal(spec.Triggers)
	if err != nil {
		return nil, err
	}

	fingerprint := sha256.Sum256(encoded)

	return r.prepared.GetOrBuild(source.GetUID(), source.GetGeneration(), count, fingerprint, func() (*replication.PreparedTriggers, error) {
		return replication.PrepareTriggers(spec, source.GetNamespace() != "", r.selectors, r.conditions)
	})
}

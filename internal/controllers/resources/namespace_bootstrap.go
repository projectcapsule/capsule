// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"hash/fnv"
	"reflect"
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Bootstrap uses the controller's ordinary workqueue, with at most one key per
// worker per Tenant. Each key processes one namespace per turn, so a namespace
// burst cannot occupy the whole queue ahead of another Tenant. Multiple keys
// preserve parallelism within a Tenant. Pending namespaces remain deduplicated.
type namespaceBootstraps struct {
	mu       sync.Mutex
	pending  map[string]*tenantBootstraps
	sequence uint64
}

type namespaceBootstrap struct {
	name     string
	sequence uint64
}

type tenantBootstraps struct {
	order    []string
	versions map[string]uint64
}

func bootstrapQueueKey(request reconcile.Request) string {
	if request.Namespace == "bootstrap" {
		return request.Name
	}

	return request.Namespace + "/" + request.Name
}

func (r *NamespaceTrigger) enqueueBootstrap(tenant, namespace string, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	request := reconcile.Request{Namespace: "bootstrap", Name: tenant}

	if r.bootstrapWorkers > 1 {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(namespace))
		request.Namespace += "-" + strconv.FormatInt(int64(hash.Sum32())%int64(r.bootstrapWorkers), 10)
	}

	r.bootstraps.add(bootstrapQueueKey(request), namespace)
	queue.Add(request)
}

func (b *namespaceBootstraps) add(tenant, namespace string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.pending == nil {
		b.pending = make(map[string]*tenantBootstraps)
	}

	b.sequence++

	items := b.pending[tenant]
	if items == nil {
		items = &tenantBootstraps{versions: make(map[string]uint64)}
		b.pending[tenant] = items
	}

	if _, exists := items.versions[namespace]; !exists {
		items.order = append(items.order, namespace)
	}

	items.versions[namespace] = b.sequence
}

func (b *namespaceBootstraps) first(tenant string) namespaceBootstrap {
	b.mu.Lock()
	defer b.mu.Unlock()

	items := b.pending[tenant]
	if items == nil || len(items.order) == 0 {
		return namespaceBootstrap{}
	}

	name := items.order[0]

	return namespaceBootstrap{name: name, sequence: items.versions[name]}
}

func (b *namespaceBootstraps) done(tenant string, namespace namespaceBootstrap) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	items := b.pending[tenant]
	if items == nil {
		return false
	}

	if len(items.order) > 0 && items.order[0] == namespace.name && items.versions[namespace.name] == namespace.sequence {
		items.order = items.order[1:]
		delete(items.versions, namespace.name)
	}

	if len(items.order) == 0 {
		delete(b.pending, tenant)

		return false
	}

	return true
}

func (r *NamespaceTrigger) bootstrapHandler() handler.EventHandler {
	enqueue := func(object client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		if object == nil {
			return
		}

		name := object.GetLabels()[meta.TenantLabel]
		if name == "" || !object.GetDeletionTimestamp().IsZero() {
			return
		}

		r.enqueueBootstrap(name, object.GetName(), queue)
	}

	return handler.Funcs{
		CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if !e.IsInInitialList {
				enqueue(e.Object, q)
			}
		},
		UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if e.ObjectOld != nil && e.ObjectNew != nil && (e.ObjectOld.GetLabels()[meta.TenantLabel] != e.ObjectNew.GetLabels()[meta.TenantLabel] || !reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences())) {
				enqueue(e.ObjectNew, q)
			}
		},
	}
}

// Tenant status is a second, reliable notification for newly assigned namespaces,
// including existing namespaces which receive their Tenant ownership after startup.
func (r *NamespaceTrigger) bootstrapTenantHandler() handler.EventHandler {
	return handler.Funcs{UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		old, ok := e.ObjectOld.(*capsulev1beta2.Tenant)
		if !ok {
			return
		}

		current, ok := e.ObjectNew.(*capsulev1beta2.Tenant)
		if !ok {
			return
		}

		existing := sets.New(old.Status.Namespaces...)
		for _, namespace := range current.Status.Namespaces {
			if !existing.Has(namespace) {
				r.enqueueBootstrap(current.Name, namespace, q)
			}
		}
	}}
}

func namespaceAdditionsOnly(old, current *capsulev1beta2.Tenant) bool {
	if old.UID != current.UID || !reflect.DeepEqual(old.Labels, current.Labels) || !old.DeletionTimestamp.Equal(current.DeletionTimestamp) {
		return false
	}

	before, after := sets.New(old.Status.Namespaces...), sets.New(current.Status.Namespaces...)

	return before.Len() < after.Len() && after.IsSuperset(before)
}

// Raw manifests only use tenant.name and namespace fast variables. Generators and
// copied sources can depend on the full Tenant or on its changing source set, so
// they retain the authoritative full gather/prune path. Health checks likewise
// need a full apply checkpoint after the fast bootstrap has installed its targets.
func needsFullNamespaceAddition(spec *capsulev1beta2.TenantResourceCommonSpec) bool {
	if spec.HealthChecks.Enabled() {
		return true
	}

	for _, resource := range spec.Resources {
		if len(resource.Generators) > 0 || len(resource.NamespacedItems) > 0 || resource.Context != nil {
			return true
		}
	}

	return false
}

// Scoped bootstrap must respect the same dependency checkpoint as a full apply,
// including observedGeneration for health-enabled dependencies.
func (r *NamespaceTrigger) bootstrapDependenciesReady(ctx context.Context, source client.Object) error {
	spec, _ := healthSource(source)
	for _, reference := range spec.DependsOn {
		var dependency client.Object = &capsulev1beta2.GlobalTenantResource{}
		if source.GetNamespace() != "" {
			dependency = &capsulev1beta2.TenantResource{}
		}

		key := client.ObjectKey{Namespace: source.GetNamespace(), Name: reference.Name.String()}
		if err := r.client.Get(ctx, key, dependency); err != nil {
			return fmt.Errorf("read replication dependency %s: %w", key, err)
		}

		if !replicationDependencyReady(dependency) {
			return fmt.Errorf("dependency %s not ready", key)
		}
	}

	return nil
}

func (r *globalResourceController) tenantHandler() handler.EventHandler {
	enqueue := func(ctx context.Context, object client.Object, additions bool, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		for _, request := range r.requestsFromTenant(ctx, object, additions) {
			q.Add(request)
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, e.Object, false, q)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, e.Object, false, q)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			old, ok := e.ObjectOld.(*capsulev1beta2.Tenant)
			if !ok {
				return
			}

			current, ok := e.ObjectNew.(*capsulev1beta2.Tenant)
			if !ok {
				return
			}

			additions := namespaceAdditionsOnly(old, current)
			if !reflect.DeepEqual(old.Labels, current.Labels) {
				enqueue(ctx, old, additions, q)
			}

			enqueue(ctx, current, additions, q)
		},
	}
}

func (r *NamespaceTrigger) reconcileBootstrap(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	key := bootstrapQueueKey(request)

	namespace := r.bootstraps.first(key)
	if namespace.name == "" {
		return reconcile.Result{}, nil
	}

	result, err := r.reconcileNamespace(ctx, reconcile.Request{Name: namespace.name})
	if err != nil {
		return result, err
	}

	if r.bootstraps.done(key, namespace) {
		result.RequeueAfter = time.Nanosecond
	}

	return result, nil
}

func (r *globalResourceController) requestsFromTenant(ctx context.Context, object client.Object, additions bool) []reconcile.Request {
	return r.enqueueSelectedTenantResources(ctx, object, func(resource *capsulev1beta2.GlobalTenantResource) bool {
		return !additions || resource.Spec.Scope != api.ResourceScopeNamespace || needsFullNamespaceAddition(&resource.Spec.TenantResourceCommonSpec)
	})
}

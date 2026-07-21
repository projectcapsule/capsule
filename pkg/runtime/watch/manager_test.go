// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	authorizationv1 "k8s.io/api/authorization/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var (
	secretGVK     = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
	configMapGVK  = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	namespaceGVK  = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	deploymentGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
)

// --- fakes for the per-watch caches the factory hands out ---

type fakeRegistration struct{}

func (fakeRegistration) HasSynced() bool { return true }

type fakeInformer struct {
	handlers []toolscache.ResourceEventHandler
}

func (f *fakeInformer) AddEventHandler(h toolscache.ResourceEventHandler) (toolscache.ResourceEventHandlerRegistration, error) {
	f.handlers = append(f.handlers, h)

	return fakeRegistration{}, nil
}

func (f *fakeInformer) AddEventHandlerWithResyncPeriod(h toolscache.ResourceEventHandler, _ time.Duration) (toolscache.ResourceEventHandlerRegistration, error) {
	return f.AddEventHandler(h)
}

func (f *fakeInformer) AddEventHandlerWithOptions(h toolscache.ResourceEventHandler, _ toolscache.HandlerOptions) (toolscache.ResourceEventHandlerRegistration, error) {
	return f.AddEventHandler(h)
}

func (f *fakeInformer) RemoveEventHandler(toolscache.ResourceEventHandlerRegistration) error {
	return nil
}

func (f *fakeInformer) AddIndexers(toolscache.Indexers) error { return nil }
func (f *fakeInformer) HasSynced() bool                       { return true }
func (f *fakeInformer) IsStopped() bool                       { return false }

// fakeCache backs exactly one watch, like the real per-watch caches.
type fakeCache struct {
	ctrlcache.Cache

	selector  string
	gvk       schema.GroupVersionKind
	informer  *fakeInformer
	startedCh chan struct{}
}

func (c *fakeCache) GetInformer(_ context.Context, obj client.Object, _ ...ctrlcache.InformerGetOption) (ctrlcache.Informer, error) {
	c.gvk = obj.GetObjectKind().GroupVersionKind()

	if c.informer == nil {
		c.informer = &fakeInformer{}
	}

	return c.informer, nil
}

func (c *fakeCache) Start(ctx context.Context) error {
	close(c.startedCh)
	<-ctx.Done()

	return nil
}

// fakeFactory records every cache it built, in creation order.
type fakeFactory struct {
	caches []*fakeCache
}

func (f *fakeFactory) new(sel labels.Selector) (ctrlcache.Cache, error) {
	c := &fakeCache{
		selector:  sel.String(),
		startedCh: make(chan struct{}),
	}

	f.caches = append(f.caches, c)

	return c, nil
}

// createdFor counts how many caches (i.e. informers, i.e. LISTs against the
// apiserver) were ever built for a kind.
func (f *fakeFactory) createdFor(gvk schema.GroupVersionKind) int {
	n := 0

	for _, c := range f.caches {
		if c.gvk == gvk {
			n++
		}
	}

	return n
}

// handlerFor returns the event handler registered for a kind's first cache.
func (f *fakeFactory) handlerFor(t *testing.T, gvk schema.GroupVersionKind) toolscache.ResourceEventHandler {
	t.Helper()

	for _, c := range f.caches {
		if c.gvk == gvk && c.informer != nil && len(c.informer.handlers) > 0 {
			return c.informer.handlers[0]
		}
	}

	t.Fatalf("no handler registered for %s", gvk)

	return nil
}

// entryCount counts the manager's live watches for a kind (any selector).
func entryCount(m *Manager, gvk schema.GroupVersionKind) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0

	for k := range m.entries {
		if k.gvk == gvk {
			n++
		}
	}

	return n
}

// fakeAuthorizer answers SelfSubjectAccessReviews; resources listed in denied
// are refused. Reviews run concurrently, so it locks.
type fakeAuthorizer struct {
	mu     sync.Mutex
	denied map[string]struct{}
	calls  int
}

func (f *fakeAuthorizer) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	review, ok := obj.(*authorizationv1.SelfSubjectAccessReview)
	if !ok {
		return fmt.Errorf("unexpected object %T", obj)
	}

	if _, deny := f.denied[review.Spec.ResourceAttributes.Resource]; deny {
		review.Status.Allowed = false
		review.Status.Reason = "RBAC: no rule grants access"

		return nil
	}

	review.Status.Allowed = true

	return nil
}

func allowAll() *fakeAuthorizer { return &fakeAuthorizer{} }

func testMapper() apimeta.RESTMapper {
	m := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Version: "v1"},
		{Group: "apps", Version: "v1"},
	})
	m.Add(secretGVK, apimeta.RESTScopeNamespace)
	m.Add(configMapGVK, apimeta.RESTScopeNamespace)
	m.Add(namespaceGVK, apimeta.RESTScopeRoot)
	m.Add(deploymentGVK, apimeta.RESTScopeNamespace)

	return m
}

func newTestManager() (*fakeFactory, *Manager) {
	f := &fakeFactory{}

	return f, NewManager(f.new, allowAll(), testMapper(), logr.Discard())
}

func unfiltered(gvks ...schema.GroupVersionKind) []Spec {
	specs := make([]Spec, 0, len(gvks))
	for _, g := range gvks {
		specs = append(specs, Spec{GVK: g})
	}

	return specs
}

func syncSpecsOrFatal(t *testing.T, m *Manager, owner string, specs ...Spec) {
	t.Helper()

	if err := m.Sync(context.Background(), owner, specs); err != nil {
		t.Fatalf("Sync(%q) unexpected error: %v", owner, err)
	}
}

func syncOrFatal(t *testing.T, m *Manager, owner string, gvks ...schema.GroupVersionKind) {
	t.Helper()

	syncSpecsOrFatal(t, m, owner, unfiltered(gvks...)...)
}

// fakeClock drives the manager's release timestamps (eviction order) in tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newFakeClock() *fakeClock               { return &fakeClock{t: time.Unix(1000, 0)} }
func withFakeClock(m *Manager) *fakeClock {
	c := newFakeClock()
	m.now = c.now

	return c
}

func TestManager_RefcountAndWarmRelease(t *testing.T) {
	f, m := newTestManager()

	syncOrFatal(t, m, "a", secretGVK)
	syncOrFatal(t, m, "b", secretGVK)

	if f.createdFor(secretGVK) != 1 {
		t.Fatalf("watch should be created once, got %d", f.createdFor(secretGVK))
	}

	// Releasing one of two owners keeps the shared watch alive.
	syncOrFatal(t, m, "a")

	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("watch must survive while owner b holds it")
	}

	// Releasing the last owner keeps the watch warm instead of tearing it
	// down, so delete/recreate cycles do not cause a full LIST per cycle.
	syncOrFatal(t, m, "b")

	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("watch must stay warm after last release")
	}

	// Re-arming reuses the warm watch without a new LIST.
	syncOrFatal(t, m, "a", secretGVK)

	if f.createdFor(secretGVK) != 1 {
		t.Fatalf("warm watch must be reused, created=%d", f.createdFor(secretGVK))
	}
}

func TestManager_WarmGracePeriod(t *testing.T) {
	f, m := newTestManager()
	clock := withFakeClock(m)

	syncOrFatal(t, m, "a", secretGVK)
	syncOrFatal(t, m, "a")

	// Within the grace period the warm watch survives sweeps.
	clock.advance(m.warmGracePeriod / 2)
	m.sweepExpired()

	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("watch must survive sweeps within the grace period")
	}

	// Re-arming reuses the warm watch without a new LIST and resets the warm
	// state: the entry is owned again and the old release timestamp must not
	// count against it.
	syncOrFatal(t, m, "a", secretGVK)

	if f.createdFor(secretGVK) != 1 {
		t.Fatalf("warm watch must be reused, created=%d", f.createdFor(secretGVK))
	}

	clock.advance(m.warmGracePeriod * 2)
	m.sweepExpired()

	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("owned watch must never be swept")
	}

	// Once ownerless past the grace period, the sweep tears it down and frees
	// its cached objects.
	syncOrFatal(t, m, "a")
	clock.advance(m.warmGracePeriod + time.Second)
	m.sweepExpired()

	if entryCount(m, secretGVK) != 0 {
		t.Fatalf("expired warm watch must be torn down")
	}
}

func TestManager_ReleasedWatchEvictedUnderSlotPressure(t *testing.T) {
	f, m := newTestManager()
	m.maxWatches = 2

	// A fully released watch stays warm and a returning owner reuses it without
	// a new LIST (the sweeper never runs in this test).
	syncOrFatal(t, m, "a", secretGVK)
	syncOrFatal(t, m, "a")
	syncOrFatal(t, m, "a", secretGVK)
	syncOrFatal(t, m, "a")

	if f.createdFor(secretGVK) != 1 {
		t.Fatalf("warm watch must be reused, created=%d", f.createdFor(secretGVK))
	}

	// Slot pressure reclaims it before the grace period: filling the limit
	// evicts the ownerless watch for a new kind.
	syncOrFatal(t, m, "b", configMapGVK)
	syncOrFatal(t, m, "b", configMapGVK, deploymentGVK)

	if entryCount(m, secretGVK) != 0 {
		t.Fatalf("ownerless watch must be evicted under slot pressure")
	}

	if entryCount(m, deploymentGVK) != 1 {
		t.Fatalf("new watch must take the reclaimed slot")
	}
}

func TestManager_TenantScopedAndGlobalWatchesOfAKind(t *testing.T) {
	f, m := newTestManager()

	// The trigger layer's two selector classes for one kind: the shared
	// tenant-label-exists watch (namespaced TenantResources) and the
	// unfiltered watch (GlobalTenantResources).
	tenantScoped := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      "projectcapsule.dev/tenant",
		Operator: metav1.LabelSelectorOpExists,
	}}}

	// Owners across different tenants share the single tenant-scoped watch.
	syncSpecsOrFatal(t, m, "tenant-a/tr", Spec{GVK: secretGVK, Selector: tenantScoped})
	syncSpecsOrFatal(t, m, "tenant-b/tr", Spec{GVK: secretGVK, Selector: tenantScoped})

	if entryCount(m, secretGVK) != 1 || f.createdFor(secretGVK) != 1 {
		t.Fatalf("all tenant-scoped owners must share one watch, entries=%d created=%d", entryCount(m, secretGVK), f.createdFor(secretGVK))
	}

	// A global owner of the same kind gets its own unfiltered watch.
	syncOrFatal(t, m, "global/gtr", secretGVK)

	if entryCount(m, secretGVK) != 2 || f.createdFor(secretGVK) != 2 {
		t.Fatalf("the global watch must be a second, distinct entry, entries=%d created=%d", entryCount(m, secretGVK), f.createdFor(secretGVK))
	}

	// Releasing every tenant-scoped owner leaves the global watch owned; the
	// tenant-scoped one goes warm.
	syncOrFatal(t, m, "tenant-a/tr")
	syncOrFatal(t, m, "tenant-b/tr")

	m.mu.Lock()
	for k, e := range m.entries {
		owned := len(e.owners) > 0
		if k.selector == "" && !owned {
			t.Fatalf("the global watch must remain owned")
		}

		if k.selector != "" && owned {
			t.Fatalf("the tenant-scoped watch must be fully released, owners=%v", e.owners)
		}
	}
	m.mu.Unlock()
}

func TestManager_EvictsOldestWarmFirst(t *testing.T) {
	f, m := newTestManager()
	clock := withFakeClock(m)
	m.maxWatches = 2

	syncOrFatal(t, m, "a", secretGVK)
	syncOrFatal(t, m, "b", configMapGVK)

	// Release secret first, configmap later: secret is the older warm entry.
	syncOrFatal(t, m, "a")
	clock.advance(time.Minute)
	syncOrFatal(t, m, "b")

	syncOrFatal(t, m, "c", deploymentGVK)

	if entryCount(m, secretGVK) != 0 {
		t.Fatalf("oldest warm watch must be evicted first")
	}

	if entryCount(m, configMapGVK) != 1 {
		t.Fatalf("younger warm watch must survive")
	}

	if f.createdFor(deploymentGVK) != 1 {
		t.Fatalf("deployment watch should have been created, created=%d", f.createdFor(deploymentGVK))
	}
}

func TestManager_SyncSwapsGVKs(t *testing.T) {
	f, m := newTestManager()

	syncOrFatal(t, m, "a", secretGVK)
	// Same owner now wants a different kind: old goes warm, new is armed.
	syncOrFatal(t, m, "a", configMapGVK)

	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("secret watch should stay warm")
	}

	if f.createdFor(configMapGVK) != 1 {
		t.Fatalf("configmap watch should have been created, created=%d", f.createdFor(configMapGVK))
	}
}

func TestManager_MaxWatchesEvictsWarmOnly(t *testing.T) {
	f, m := newTestManager()
	m.maxWatches = 1

	syncOrFatal(t, m, "a", secretGVK)

	// The slot is owned: arming another kind must fail.
	err := m.Sync(context.Background(), "a", unfiltered(secretGVK, configMapGVK))
	if err == nil {
		t.Fatalf("expected error when exceeding the watch limit")
	}

	if f.createdFor(configMapGVK) != 0 {
		t.Fatalf("configmap watch must not be created past the limit")
	}

	// The already-armed secret watch must be unaffected.
	if entryCount(m, secretGVK) != 1 {
		t.Fatalf("secret watch must remain armed")
	}

	// Once the secret watch is merely warm, its slot is reclaimed for new kinds.
	syncOrFatal(t, m, "a")
	syncOrFatal(t, m, "a", configMapGVK)

	if entryCount(m, secretGVK) != 0 {
		t.Fatalf("warm secret watch must be evicted under limit pressure")
	}

	if f.createdFor(configMapGVK) != 1 {
		t.Fatalf("configmap watch should have been created after eviction, created=%d", f.createdFor(configMapGVK))
	}
}

func TestManager_SelectorWatches(t *testing.T) {
	f, m := newTestManager()

	selA := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}
	selAEquivalent := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}
	selB := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "b"}}

	// Same kind under two different selectors: two independent watches.
	syncSpecsOrFatal(t, m, "a", Spec{GVK: secretGVK, Selector: selA}, Spec{GVK: secretGVK, Selector: selB})

	if entryCount(m, secretGVK) != 2 || len(f.caches) != 2 {
		t.Fatalf("expected two watches for two selectors, entries=%d caches=%d", entryCount(m, secretGVK), len(f.caches))
	}

	// The selector must reach the cache factory (i.e. the apiserver).
	got := map[string]bool{}
	for _, c := range f.caches {
		got[c.selector] = true
	}

	if !got["app=a"] || !got["app=b"] {
		t.Fatalf("expected caches for app=a and app=b, got %v", got)
	}

	// An equal selector from another owner shares the existing watch.
	syncSpecsOrFatal(t, m, "b", Spec{GVK: secretGVK, Selector: selAEquivalent})

	if len(f.caches) != 2 {
		t.Fatalf("equal selectors must share a watch, caches=%d", len(f.caches))
	}

	// Filtered and unfiltered watches of the same kind are distinct entries.
	syncOrFatal(t, m, "c", secretGVK)

	if entryCount(m, secretGVK) != 3 {
		t.Fatalf("unfiltered watch must be its own entry, entries=%d", entryCount(m, secretGVK))
	}

	// Invalid selectors surface as errors and arm nothing.
	bad := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "k", Operator: "Bogus"}}}
	if err := m.Sync(context.Background(), "d", []Spec{{GVK: configMapGVK, Selector: bad}}); err == nil {
		t.Fatalf("expected error for invalid selector")
	}

	if f.createdFor(configMapGVK) != 0 {
		t.Fatalf("no watch must be created for an invalid selector")
	}
}

func TestManager_StartRunsCaches(t *testing.T) {
	f, m := newTestManager()

	// Armed before Start: the cache exists but is not running.
	syncOrFatal(t, m, "a", secretGVK)

	select {
	case <-f.caches[0].startedCh:
		t.Fatalf("cache must not start before the manager starts")
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- m.Start(ctx) }()

	// Start runs pre-existing caches...
	select {
	case <-f.caches[0].startedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("pre-armed cache was not started")
	}

	// ...and caches armed while running start immediately.
	syncOrFatal(t, m, "a", secretGVK, configMapGVK)

	select {
	case <-f.caches[1].startedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("cache armed after start was not started")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return on context cancellation")
	}
}

func TestManager_UnresolvableGVK(t *testing.T) {
	f, m := newTestManager()

	bad := schema.GroupVersionKind{Group: "x", Version: "v1", Kind: "Nope"}
	if err := m.Sync(context.Background(), "a", unfiltered(bad)); err == nil {
		t.Fatalf("expected error for unresolvable GVK")
	}

	if len(f.caches) != 0 {
		t.Fatalf("no watch should be created for an unresolvable GVK, got %d", len(f.caches))
	}
}

func TestManager_ResolveVersionKinds(t *testing.T) {
	_, m := newTestManager()

	namespaced, clusterScoped, err := m.ResolveVersionKinds([]capruntime.VersionKind{
		{Kind: "Secret"},                           // empty apiVersion means core v1
		{APIVersion: "v1", Kind: "Secret"},         // duplicate after resolution
		{APIVersion: "apps/*", Kind: "Deployment"}, // bare group resolves to the preferred version
		{APIVersion: "v1", Kind: "Namespace"},      // cluster-scoped
	})
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}

	wantNamespaced := []schema.GroupVersionKind{secretGVK, deploymentGVK}
	if len(namespaced) != len(wantNamespaced) {
		t.Fatalf("expected namespaced %v, got %v", wantNamespaced, namespaced)
	}

	for i, g := range wantNamespaced {
		if namespaced[i] != g {
			t.Fatalf("namespaced[%d] = %v, want %v", i, namespaced[i], g)
		}
	}

	if len(clusterScoped) != 1 || clusterScoped[0] != namespaceGVK {
		t.Fatalf("expected cluster-scoped [%v], got %v", namespaceGVK, clusterScoped)
	}

	// An unresolvable selector yields an error but keeps the resolvable remainder.
	namespaced, _, err = m.ResolveVersionKinds([]capruntime.VersionKind{
		{Kind: "Nope"},
		{APIVersion: "v1", Kind: "Secret"},
	})
	if err == nil {
		t.Fatalf("expected error for unresolvable selector")
	}

	if len(namespaced) != 1 || namespaced[0] != secretGVK {
		t.Fatalf("resolvable selectors must survive an error, got %v", namespaced)
	}
}

// recSink records what the manager dispatches.
type recSink struct {
	ops  []Operation
	objs []string
}

func (s *recSink) Notify(_ context.Context, _ schema.GroupVersionKind, op Operation, obj metav1.Object) {
	s.ops = append(s.ops, op)
	s.objs = append(s.objs, obj.GetName())
}

func TestManager_DispatchOperations(t *testing.T) {
	f, m := newTestManager()

	sink := &recSink{}
	m.RegisterSink(sink)

	syncOrFatal(t, m, "a", secretGVK)

	handler := f.handlerFor(t, secretGVK)

	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns", ResourceVersion: "1"}}
	updated := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns", ResourceVersion: "2"}}
	handler.OnAdd(obj, false)
	handler.OnUpdate(obj, updated)
	handler.OnDelete(updated)

	wantOps := []Operation{OperationCreate, OperationUpdate, OperationDelete}

	if len(sink.ops) != len(wantOps) {
		t.Fatalf("expected %d dispatches, got %d (%v)", len(wantOps), len(sink.ops), sink.ops)
	}

	for i, op := range wantOps {
		if sink.ops[i] != op {
			t.Fatalf("op[%d] = %q, want %q", i, sink.ops[i], op)
		}

		if sink.objs[i] != "s1" {
			t.Fatalf("obj[%d] = %q, want s1", i, sink.objs[i])
		}
	}
}

func TestManager_InitialListNotDispatched(t *testing.T) {
	f, m := newTestManager()

	sink := &recSink{}
	m.RegisterSink(sink)

	syncOrFatal(t, m, "a", secretGVK)

	handler := f.handlerFor(t, secretGVK)

	// The informer replays every pre-existing object as an add with
	// isInInitialList=true when it syncs (startup and re-arm relist). Those
	// are not creations and must not be dispatched.
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"}}
	handler.OnAdd(obj, true)

	if len(sink.ops) != 0 {
		t.Fatalf("expected no dispatch for initial-list add, got %v", sink.ops)
	}

	// A genuine create afterwards still dispatches.
	handler.OnAdd(obj, false)

	if len(sink.ops) != 1 || sink.ops[0] != OperationCreate {
		t.Fatalf("expected a single OperationCreate, got %v", sink.ops)
	}
}

func TestManager_ResyncUpdatesNotDispatched(t *testing.T) {
	f, m := newTestManager()

	sink := &recSink{}
	m.RegisterSink(sink)

	syncOrFatal(t, m, "a", secretGVK)

	handler := f.handlerFor(t, secretGVK)

	// A periodic resync replays the cached object against itself: the
	// resourceVersion is unchanged, so no sink must be notified.
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns", ResourceVersion: "7"}}
	handler.OnUpdate(obj, obj)

	if len(sink.ops) != 0 {
		t.Fatalf("expected no dispatch for a resync replay, got %v", sink.ops)
	}

	// A genuine write bumps the resourceVersion and dispatches.
	updated := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns", ResourceVersion: "8"}}
	handler.OnUpdate(obj, updated)

	if len(sink.ops) != 1 || sink.ops[0] != OperationUpdate {
		t.Fatalf("expected a single OperationUpdate, got %v", sink.ops)
	}
}

func TestManager_ForbiddenKindNotArmed(t *testing.T) {
	f := &fakeFactory{}
	authz := &fakeAuthorizer{denied: map[string]struct{}{"secrets": {}}}
	m := NewManager(f.new, authz, testMapper(), logr.Discard())

	err := m.Sync(context.Background(), "a", unfiltered(secretGVK, configMapGVK))
	if err == nil || !strings.Contains(err.Error(), "not permitted to list secrets") {
		t.Fatalf("expected a permission error naming the resource, got %v", err)
	}

	if f.createdFor(secretGVK) != 0 {
		t.Fatalf("no watch must be created for a kind the controller cannot watch")
	}

	// Permitted kinds in the same Sync are unaffected.
	if f.createdFor(configMapGVK) != 1 {
		t.Fatalf("configmap watch should have been created, created=%d", f.createdFor(configMapGVK))
	}

	// Already-armed kinds are not re-checked on subsequent Syncs.
	calls := authz.calls

	syncOrFatal(t, m, "b", configMapGVK)

	if authz.calls != calls {
		t.Fatalf("armed kinds must not be re-checked, calls went %d -> %d", calls, authz.calls)
	}

	// Once permissions are granted, the next Sync arms the kind.
	authz.denied = nil
	syncOrFatal(t, m, "a", secretGVK, configMapGVK)

	if f.createdFor(secretGVK) != 1 {
		t.Fatalf("secret watch should be created after permissions are granted, created=%d", f.createdFor(secretGVK))
	}
}

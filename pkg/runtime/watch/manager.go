// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package watch manages dynamic, metadata-only informers for object kinds that
// are only known at runtime (e.g. declared in a spec). Watches are keyed by
// kind plus an optional server-side label selector, reference counted across
// owners and shared between consumers; a fully released watch is kept warm for
// a grace period (absorbing delete/recreate cycles without a relist) and then
// torn down, so its cached objects are not held indefinitely. Object change
// events are fanned out to registered sinks. Sinks still apply their own
// matching: the server-side selector is a bandwidth and memory optimization,
// not the source of truth for which events matter.
package watch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
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

const (
	// defaultMaxWatches bounds the number of distinct watches (kind + selector
	// combinations) the manager will run concurrently, guarding against a
	// runaway set of owners exhausting informer memory.
	defaultMaxWatches = 50

	// defaultWarmGracePeriod is how long an ownerless watch is kept warm before
	// it is torn down. Long enough to absorb delete/recreate cycles (GitOps
	// prune-and-apply, CI) without a relist storm, short enough that a released
	// watch cannot hold its cached objects — which, for cluster-wide kinds like
	// Secret or ConfigMap, dominate the watch's memory — indefinitely.
	defaultWarmGracePeriod = 10 * time.Minute

	// defaultSweepInterval is how often the manager checks for expired warm
	// watches.
	defaultSweepInterval = time.Minute

	// accessCheckTimeout bounds the arm-time permission check. It runs under
	// the manager lock, so it must not hang on a slow apiserver.
	accessCheckTimeout = 5 * time.Second
)

// Operation is the object lifecycle event a watch reports.
type Operation string

const (
	// OperationCreate reports creations of watched objects.
	OperationCreate Operation = "CREATE"
	// OperationUpdate reports updates of watched objects.
	OperationUpdate Operation = "UPDATE"
	// OperationDelete reports deletions of watched objects.
	OperationDelete Operation = "DELETE"
)

// Spec selects one kind to watch. A non-nil Selector is pushed down to the
// apiserver: only matching objects are streamed and cached, so memory and
// traffic scale with the selection instead of the cluster. Two owners asking
// for the same kind with the same (canonicalized) selector share one watch.
type Spec struct {
	GVK      schema.GroupVersionKind
	Selector *metav1.LabelSelector
}

// Sink receives raw object change events for every watched kind. Sinks decide
// themselves which events are relevant to them. When the same kind is watched
// under overlapping selectors, a sink can receive the same event once per
// watch; consumers must stay idempotent (the trigger sinks are: the workqueue
// dedups by owner key).
type Sink interface {
	Notify(ctx context.Context, gvk schema.GroupVersionKind, op Operation, obj metav1.Object)
}

// AccessReviewer creates access-review objects against the apiserver. Any
// controller-runtime client.Client satisfies it — writes always bypass the
// cache, so the regular manager client is fine here.
type AccessReviewer interface {
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
}

// CacheFactory builds the cache backing a single watch, scoped to the given
// label selector (labels.Everything() when the watch is unfiltered). Use
// MetadataCacheFactory in production; tests inject fakes.
type CacheFactory func(selector labels.Selector) (ctrlcache.Cache, error)

// watchKey identifies a watch: a kind plus the canonical string form of its
// selector (labels.Selector sorts requirements, so semantically equal
// selectors share a key; "" means unfiltered).
type watchKey struct {
	gvk      schema.GroupVersionKind
	selector string
}

func (k watchKey) String() string {
	s := k.gvk.String()
	if k.selector != "" {
		s += " [" + k.selector + "]"
	}

	return s
}

type entry struct {
	// owners is the set of owner keys keeping this watch alive. An entry whose
	// owner set drained is kept warm for the grace period so delete/recreate
	// cycles of an owner do not pay a relist, then swept. A warm watch is not
	// free: beyond the informer's fixed ~37 KiB it retains every matched
	// object's stripped metadata (~0.75 KiB each measured), which for a
	// cluster-wide kind is the dominant cost — hence the bounded warm window.
	owners map[string]struct{}
	// releasedAt is when the last owner released this watch; zero while owned.
	// It bounds the warm window (sweepExpired) and orders evictions (oldest
	// released goes first) under limit pressure.
	releasedAt time.Time
	// cache is the dedicated single-kind cache backing this watch; stopping it
	// (stop) tears the informer down. It is created unstarted and started by
	// Start, or immediately when the manager is already running; stop is nil
	// until then.
	cache ctrlcache.Cache
	stop  context.CancelFunc
}

// Manager installs and tears down metadata-only watches for dynamically
// requested kind+selector combinations and fans object change events out to
// the registered sinks. Each watch runs in its own dedicated cache built by
// the factory, so tearing one down is a context cancellation and can never
// affect informers other consumers use. Watches are reference counted across
// all owners; a fully released watch is kept warm for a grace period so a
// returning owner re-arms without a relist, then swept — a warm watch retains
// every matched object's metadata (~0.75 KiB each), so it is not held longer
// than the grace period, and the watch limit can evict a warm watch sooner to
// free a slot. It is safe for concurrent use.
type Manager struct {
	newCache CacheFactory
	authz    AccessReviewer
	mapper   apimeta.RESTMapper
	log      logr.Logger

	maxWatches      int
	warmGracePeriod time.Duration
	sweepInterval   time.Duration
	now             func() time.Time

	mu      sync.Mutex
	entries map[watchKey]*entry
	// runCtx is the manager's Start context: watch caches must live and die
	// with the manager runnable, not with the reconcile call that armed them,
	// so entries created before Start are started by Start and entries created
	// later start immediately from it.
	runCtx context.Context //nolint:containedctx

	// sinks has its own lock so that dispatch — which runs for every event of
	// every watched kind — never contends with Sync holding mu across informer
	// arming (SSAR round trips, informer creation).
	sinksMu sync.RWMutex
	sinks   []Sink
}

// NewManager builds a manager that creates a dedicated cache per watch via the
// given factory (use MetadataCacheFactory), verifies permissions through the
// access reviewer (the regular manager client), and resolves kinds via the
// REST mapper. Add it to the controller manager (mgr.Add) so the watch caches
// run and the warm-watch sweeper runs.
func NewManager(newCache CacheFactory, authz AccessReviewer, mapper apimeta.RESTMapper, log logr.Logger) *Manager {
	return &Manager{
		newCache:        newCache,
		authz:           authz,
		mapper:          mapper,
		log:             log,
		maxWatches:      defaultMaxWatches,
		warmGracePeriod: defaultWarmGracePeriod,
		sweepInterval:   defaultSweepInterval,
		now:             time.Now,
		entries:         make(map[watchKey]*entry),
	}
}

// Start implements manager.Runnable: it runs the caches backing the armed
// watches and periodically tears down watches that have been ownerless for
// longer than the warm grace period. It runs on the leader only, alongside the
// reconcilers that arm the watches; cancelling its context stops every watch
// cache.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.runCtx = ctx

	for _, e := range m.entries {
		m.startEntryLocked(ctx, e)
	}
	m.mu.Unlock()

	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.sweepExpired()
		}
	}
}

// RegisterSink adds a sink that receives every watched object change event.
// Sinks must not call RegisterSink from Notify.
func (m *Manager) RegisterSink(s Sink) {
	m.sinksMu.Lock()
	defer m.sinksMu.Unlock()

	m.sinks = append(m.sinks, s)
}

// ResolveVersionKinds resolves kind selectors to concrete, de-duplicated
// GroupVersionKinds via the REST mapper, split by scope. Selectors without a
// concrete version (e.g. a bare API group) resolve to the mapper's preferred
// version for that group. Selectors that cannot be resolved contribute an
// error; the resolvable remainder is returned regardless.
func (m *Manager) ResolveVersionKinds(vks []capruntime.VersionKind) (namespaced, clusterScoped []schema.GroupVersionKind, _ error) {
	seen := make(map[schema.GroupVersionKind]struct{}, len(vks))

	var errs []error

	for _, vk := range vks {
		gvk := vk.GroupVersionKind()

		var versions []string
		if gvk.Version != "" && gvk.Version != capruntime.WildcardVersionKindMatcher {
			versions = append(versions, gvk.Version)
		}

		mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), versions...)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot resolve kind %s: %w", gvk, err))

			continue
		}

		resolved := mapping.GroupVersionKind
		if _, ok := seen[resolved]; ok {
			continue
		}

		seen[resolved] = struct{}{}

		if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
			namespaced = append(namespaced, resolved)
		} else {
			clusterScoped = append(clusterScoped, resolved)
		}
	}

	return namespaced, clusterScoped, errors.Join(errs...)
}

// specKey canonicalizes a Spec into its watch key and parsed selector. A nil
// Selector means unfiltered (labels.Everything()), never labels.Nothing().
func specKey(s Spec) (watchKey, labels.Selector, error) {
	sel := labels.Everything()

	if s.Selector != nil {
		parsed, err := metav1.LabelSelectorAsSelector(s.Selector)
		if err != nil {
			return watchKey{}, nil, fmt.Errorf("invalid selector for %s: %w", s.GVK, err)
		}

		sel = parsed
	}

	return watchKey{gvk: s.GVK, selector: sel.String()}, sel, nil
}

// Sync reconciles the exact set of watches an owner wants. New kind+selector
// combinations get a dedicated informer + event handler; watches the owner no
// longer references are released and kept warm for the grace period (or until
// the watch limit needs their slot) in case the owner comes back. Passing an
// empty slice releases every watch held by the owner (used on delete). Errors
// from individual specs (e.g. unresolvable GVK, watch limit reached) are joined
// and returned; successfully armed watches are unaffected.
//
// Success means a watch is armed with list/watch permission verified, not that
// the informer synced: arming never blocks on the potentially slow initial
// list. Failures after arming (e.g. permissions revoked later) are retried by
// the informer in the background and surface only in the controller logs.
func (m *Manager) Sync(ctx context.Context, ownerKey string, specs []Spec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	desired := make(map[watchKey]labels.Selector, len(specs))

	for _, s := range specs {
		k, sel, err := specKey(s)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		desired[k] = sel
	}

	checks := m.verifyAccessLocked(ctx, desired)

	for k, sel := range desired {
		if err := m.ensureWatchLocked(ctx, k, sel, ownerKey, checks); err != nil {
			errs = append(errs, err)
		}
	}

	// entry.owners is the single source of truth for ownership: release every
	// watch this owner holds that is no longer desired.
	for k, e := range m.entries {
		if _, owned := e.owners[ownerKey]; !owned {
			continue
		}

		if _, ok := desired[k]; ok {
			continue
		}

		m.releaseLocked(k, ownerKey)
	}

	return errors.Join(errs...)
}

// startEntryLocked runs the entry's cache under the manager's run context so
// the watch lives and dies with the manager runnable, not with the reconcile
// call that armed it.
func (m *Manager) startEntryLocked(ctx context.Context, e *entry) {
	if e.stop != nil {
		return
	}

	cctx, cancel := context.WithCancel(ctx)
	e.stop = cancel

	go func() {
		if err := e.cache.Start(cctx); err != nil {
			m.log.Error(err, "watch cache terminated")
		}
	}()
}

// verifyAccessLocked resolves and permission-checks every kind in the desired
// set that is not armed yet, concurrently. The SSAR round trips dominate cold
// arm latency: running them serially made arming N kinds pay 2N sequential
// round trips under the manager lock. Kinds appearing under several selectors
// are checked once. The result maps each checked kind to nil or its
// resolve/permission error; kinds already armed are absent.
func (m *Manager) verifyAccessLocked(ctx context.Context, desired map[watchKey]labels.Selector) map[schema.GroupVersionKind]error {
	results := make(map[schema.GroupVersionKind]error)

	var (
		gvks []schema.GroupVersionKind
		gvrs []schema.GroupVersionResource
	)

	for k := range desired {
		if _, ok := m.entries[k]; ok {
			continue
		}

		if _, seen := results[k.gvk]; seen {
			continue
		}

		// Validate the kind is resolvable before creating an informer for it.
		mapping, err := m.mapper.RESTMapping(k.gvk.GroupKind(), k.gvk.Version)
		if err != nil {
			results[k.gvk] = fmt.Errorf("cannot resolve kind %s: %w", k.gvk, err)

			continue
		}

		results[k.gvk] = nil

		gvks = append(gvks, k.gvk)
		gvrs = append(gvrs, mapping.Resource)
	}

	if len(gvks) == 0 {
		return results
	}

	// Bound the burst against the apiserver; reviews are answered in memory by
	// the authorizer, so a small fan-out already collapses the latency. Errors
	// land in checkErrs per kind; the group itself never fails.
	checkErrs := make([]error, len(gvks))

	var eg errgroup.Group

	eg.SetLimit(10)

	for i := range gvks {
		eg.Go(func() error {
			if err := m.assertWatchable(ctx, gvrs[i]); err != nil {
				checkErrs[i] = fmt.Errorf("cannot watch %s: %w", gvks[i], err)
			}

			return nil
		})
	}

	_ = eg.Wait()

	for i, gvk := range gvks {
		if checkErrs[i] != nil {
			results[gvk] = checkErrs[i]
		}
	}

	return results
}

func (m *Manager) ensureWatchLocked(ctx context.Context, k watchKey, sel labels.Selector, ownerKey string, checks map[schema.GroupVersionKind]error) error {
	if e, ok := m.entries[k]; ok {
		e.owners[ownerKey] = struct{}{}
		e.releasedAt = time.Time{}

		return nil
	}

	if err := checks[k.gvk]; err != nil {
		return err
	}

	// A warm (ownerless) watch is torn down early when its slot is needed.
	if len(m.entries) >= m.maxWatches && !m.evictWarmLocked() {
		return fmt.Errorf("watch limit reached (%d), not watching %s", m.maxWatches, k)
	}

	c, err := m.armCache(ctx, k.gvk, sel)
	if err != nil {
		return fmt.Errorf("failed to arm %s: %w", k, err)
	}

	e := &entry{
		owners: map[string]struct{}{ownerKey: {}},
		cache:  c,
	}

	if m.runCtx != nil {
		// The cache's lifetime is deliberately bound to the manager's run
		// context, not this reconcile's ctx.
		m.startEntryLocked(m.runCtx, e) //nolint:contextcheck
	}

	m.entries[k] = e

	m.log.V(3).Info("armed watch", "watch", k.String())

	return nil
}

// armCache builds the dedicated cache backing one watch and registers the
// event handler. It never blocks on the initial cache sync: a large list can
// be slow, and holding the manager lock through it would stall every other
// reconcile arming watches. Events flow once the informer syncs.
func (m *Manager) armCache(ctx context.Context, gvk schema.GroupVersionKind, sel labels.Selector) (ctrlcache.Cache, error) {
	c, err := m.newCache(sel)
	if err != nil {
		return nil, fmt.Errorf("failed to build cache: %w", err)
	}

	informer, err := c.GetInformer(ctx, partialFor(gvk), ctrlcache.BlockUntilSynced(false))
	if err != nil {
		return nil, fmt.Errorf("failed to get informer: %w", err)
	}

	// Informer callbacks carry no context; dispatch synthesizes a background one.
	if _, err := informer.AddEventHandler(m.handlerFor(gvk)); err != nil { //nolint:contextcheck
		return nil, fmt.Errorf("failed to add event handler: %w", err)
	}

	return c, nil
}

// releaseLocked drops an owner from a watch. The watch is kept warm for the
// grace period: an owner that is deleted and quickly recreated (CI, GitOps
// prune-and-apply) re-arms instantly instead of paying a full LIST per cycle,
// while a watch nobody re-claims cannot hold its cached objects (the dominant
// cost for a cluster-wide kind) beyond the grace period.
func (m *Manager) releaseLocked(k watchKey, ownerKey string) {
	e, ok := m.entries[k]
	if !ok {
		return
	}

	delete(e.owners, ownerKey)

	if len(e.owners) == 0 {
		e.releasedAt = m.now()

		m.log.V(3).Info("watch released, keeping warm", "watch", k.String(), "gracePeriod", m.warmGracePeriod.String())
	}
}

// sweepExpired tears down watches that have been ownerless for longer than the
// warm grace period, freeing their cached objects. Deleting from m.entries
// while ranging over it is safe in Go.
func (m *Manager) sweepExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	deadline := m.now().Add(-m.warmGracePeriod)

	for k, e := range m.entries {
		if len(e.owners) > 0 || e.releasedAt.After(deadline) {
			continue
		}

		m.removeEntryLocked(k)

		m.log.V(3).Info("warm watch expired", "watch", k.String())
	}
}

// evictWarmLocked tears down the longest-released ownerless watch to free a
// slot for a new one. Returns false when every watch is still owned.
func (m *Manager) evictWarmLocked() bool {
	var (
		victim watchKey
		oldest *entry
	)

	for k, e := range m.entries {
		if len(e.owners) > 0 {
			continue
		}

		if oldest == nil || e.releasedAt.Before(oldest.releasedAt) {
			victim, oldest = k, e
		}
	}

	if oldest == nil {
		return false
	}

	m.removeEntryLocked(victim)

	m.log.V(3).Info("evicted warm watch", "watch", victim.String())

	return true
}

// removeEntryLocked stops the watch's dedicated cache and forgets its entry.
// Cancelling the cache context halts its reflector and processor and releases
// the registered handler with it; a cache created before Start (never started)
// has nothing to stop.
func (m *Manager) removeEntryLocked(k watchKey) {
	if e, ok := m.entries[k]; ok && e.stop != nil {
		e.stop()
	}

	delete(m.entries, k)
}

// assertWatchable verifies via SelfSubjectAccessReview that the controller's
// own identity may list and watch the resource before an informer is created
// for it. An informer that lacks these permissions never errors anywhere a
// caller can see — the reflector retries forever and only logs — so this is
// the one place a permission problem can surface into the owner's status
// (same pattern Kyverno uses for its background controller). The reviews are
// answered by the apiserver's authorizer in memory: no objects are listed, and
// creating SelfSubjectAccessReviews is granted to every authenticated identity
// via the system:basic-user role.
func (m *Manager) assertWatchable(ctx context.Context, gvr schema.GroupVersionResource) error {
	ctx, cancel := context.WithTimeout(ctx, accessCheckTimeout)
	defer cancel()

	for _, verb := range []string{"list", "watch"} {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb:     verb,
					Group:    gvr.Group,
					Version:  gvr.Version,
					Resource: gvr.Resource,
				},
			},
		}

		if err := m.authz.Create(ctx, review); err != nil {
			return fmt.Errorf("failed to verify %q permission: %w", verb, err)
		}

		if !review.Status.Allowed {
			reason := review.Status.Reason
			if reason == "" {
				reason = "RBAC denies it"
			}

			return fmt.Errorf("controller is not permitted to %s %s: %s", verb, gvr.GroupResource(), reason)
		}
	}

	return nil
}

// handlerFor builds the informer event handler for a watched kind. Informer
// callbacks carry no context, so dispatch synthesizes a background one.
func (m *Manager) handlerFor(gvk schema.GroupVersionKind) toolscache.ResourceEventHandlerDetailedFuncs {
	return toolscache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj any, isInInitialList bool) {
			// Skip the informer's initial LIST replay: those objects predate the
			// watch and are not creations. The owner reconciles independently, so
			// nothing is missed.
			if isInInitialList {
				return
			}

			m.dispatch(gvk, OperationCreate, obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			n := metaObject(newObj)
			if n == nil {
				return
			}

			// Periodic resyncs replay every cached object as an update with an
			// unchanged resourceVersion; a real write always bumps it. Dropping
			// those here keeps a resync from fanning no-op updates of every
			// cached object out to every sink.
			if o := metaObject(oldObj); o != nil && o.GetResourceVersion() == n.GetResourceVersion() {
				return
			}

			m.dispatchObj(gvk, OperationUpdate, n)
		},
		DeleteFunc: func(obj any) {
			m.dispatch(gvk, OperationDelete, obj)
		},
	}
}

func (m *Manager) dispatch(gvk schema.GroupVersionKind, op Operation, raw any) {
	obj := metaObject(raw)
	if obj == nil {
		return
	}

	m.dispatchObj(gvk, op, obj)
}

func (m *Manager) dispatchObj(gvk schema.GroupVersionKind, op Operation, obj metav1.Object) {
	m.sinksMu.RLock()
	defer m.sinksMu.RUnlock()

	// Informer callbacks have no inbound context; a fresh one is correct here.
	ctx := context.Background()
	for _, s := range m.sinks {
		s.Notify(ctx, gvk, op, obj)
	}
}

func metaObject(raw any) metav1.Object {
	if tombstone, ok := raw.(toolscache.DeletedFinalStateUnknown); ok {
		raw = tombstone.Obj
	}

	obj, err := apimeta.Accessor(raw)
	if err != nil {
		return nil
	}

	return obj
}

func partialFor(gvk schema.GroupVersionKind) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(gvk)

	return obj
}

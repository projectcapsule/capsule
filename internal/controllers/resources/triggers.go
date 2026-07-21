// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/watch"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// syncTriggers arms (or, while terminating, releases) the trigger watches for
// a resource and returns the Triggers condition summarizing the outcome.
// namespacedOwner says the owner is a namespaced TenantResource, which implies
// two narrowings at once: cluster-scoped kinds are not watched and are
// surfaced through the condition instead (such a resource can only observe
// objects inside Tenant namespaces), and every watch is narrowed server-side
// to objects carrying Capsule's tenant label, with the sink routing each event
// to the right owner.
func syncTriggers(
	ctx context.Context,
	m *watch.Manager,
	ownerKey string,
	obj client.Object,
	spec capsulev1beta2.TenantResourceCommonSpec,
	namespacedOwner bool,
	log logr.Logger,
) capsulemeta.Condition {
	if !obj.GetDeletionTimestamp().IsZero() {
		if err := m.Sync(ctx, ownerKey, nil); err != nil {
			log.V(2).Error(err, "failed to release trigger watches")
		}

		return newTriggersCondition(obj, 0, nil)
	}

	specs, rejected, specErr := triggerWatchSpecs(m, spec.Triggers, namespacedOwner)

	armErr := errors.Join(specErr, m.Sync(ctx, ownerKey, specs))

	// A namespaced TenantResource cannot observe cluster-scoped objects. Fold the
	// rejected kinds into the condition error regardless of armErr:
	// triggerWatchSpecs returns them even alongside an error, so the user sees
	// the rejection on the first reconcile instead of only after fixing an
	// unrelated armErr.
	if len(rejected) > 0 {
		armErr = errors.Join(armErr, fmt.Errorf(
			"cluster-scoped triggers are not allowed on a namespaced TenantResource: %v", rejected))
	}

	return newTriggersCondition(obj, len(specs), armErr)
}

// versionKindResolver resolves kind selectors to concrete GroupVersionKinds,
// split by scope. Satisfied by *watch.Manager.
type versionKindResolver interface {
	ResolveVersionKinds([]capruntime.VersionKind) (namespaced, clusterScoped []schema.GroupVersionKind, err error)
}

// triggerWatchSpecs resolves the triggers' kinds into one watch spec per
// distinct kind. Trigger label selectors are deliberately NOT pushed down to
// the apiserver: they are matched in the sinks (TriggerSpec.Matches), so every
// owner of a scope shares one informer per kind instead of one per selector —
// and operation semantics stay honest (a selector-filtered watch would report
// an object relabeled into the selection as CREATE and out of it as DELETE).
// Cluster-scoped kinds are dropped into rejected when namespacedOwner is set.
//
// When namespacedOwner is set, every watch is narrowed server-side to objects
// carrying Capsule's tenant label: one informer per kind across all tenants,
// without per-tenant informers or a growing tenant-value selector. Deliberate
// trade-off: objects missing the label (created while the webhook was down,
// its failurePolicy is Ignore, or predating it and never written since) do not
// reach a tenant-scoped watch and cannot fire triggers until a write stamps
// them. GlobalTenantResource watches stay unfiltered — they may legitimately
// observe unlabeled objects (non-tenant namespaces, cluster-scoped kinds) — so
// a kind watched by both scopes runs two informers.
func triggerWatchSpecs(
	r versionKindResolver,
	triggers []capsulev1beta2.TriggerSpec,
	namespacedOwner bool,
) (specs []watch.Spec, rejected []string, _ error) {
	var errs []error

	kinds := sets.New[schema.GroupVersionKind]()
	rejectedSeen := sets.New[schema.GroupVersionKind]()

	for _, t := range triggers {
		namespaced, clusterScoped, resolveErr := r.ResolveVersionKinds(t.VersionKinds.VersionKinds())
		if resolveErr != nil {
			errs = append(errs, resolveErr)
		}

		gvks := namespaced

		if namespacedOwner {
			for _, g := range clusterScoped {
				if rejectedSeen.Has(g) {
					continue
				}

				rejectedSeen.Insert(g)

				rejected = append(rejected, g.String())
			}
		} else {
			gvks = append(gvks, clusterScoped...)
		}

		kinds.Insert(gvks...)
	}

	var sel *metav1.LabelSelector
	if namespacedOwner {
		sel = tenantScopeSelector()
	}

	specs = make([]watch.Spec, 0, kinds.Len())

	for g := range kinds {
		specs = append(specs, watch.Spec{GVK: g, Selector: sel})
	}

	return specs, rejected, errors.Join(errs...)
}

// tenantScopeSelector requires Capsule's tenant label to exist on the watched
// object; it is the only selector ever pushed down for namespaced
// TenantResource watches.
func tenantScopeSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      capsulemeta.NewTenantLabel,
			Operator: metav1.LabelSelectorOpExists,
		}},
	}
}

// globalTriggerSink enqueues GlobalTenantResources whose triggers match a
// change. Capsule-replicated objects are deliberately NOT filtered out, so
// triggers can react to deletion of or tampering with rendered objects; the
// resulting self-trigger echo converges once the rendered output stops changing
// (a no-op SSA apply produces no watch event) and the workqueue dedups bursts by
// owner key.
type globalTriggerSink struct {
	reader  client.Reader
	enqueue func(types.NamespacedName)
	log     logr.Logger
}

func (s *globalTriggerSink) Notify(ctx context.Context, gvk schema.GroupVersionKind, op watch.Operation, obj metav1.Object) {
	list := &capsulev1beta2.GlobalTenantResourceList{}
	if err := s.reader.List(ctx, list, client.MatchingFields{
		tenantresource.TriggersIndexerFieldName: tenantresource.TriggerGKKey(gvk.GroupKind()),
	}); err != nil {
		s.log.V(2).Error(err, "failed to list globaltenantresources for trigger", "gvk", gvk.String())

		return
	}

	// watch.Operation values match TriggerOperation by construction.
	op2 := capsulev1beta2.TriggerOperation(op)

	for i := range list.Items {
		gtr := &list.Items[i]
		if !s.matches(ctx, gtr.Spec.Triggers, gvk, op2, obj) {
			continue
		}

		s.enqueue(types.NamespacedName{Name: gtr.Name})
	}
}

func (s *globalTriggerSink) matches(
	ctx context.Context,
	triggers []capsulev1beta2.TriggerSpec,
	gvk schema.GroupVersionKind,
	op capsulev1beta2.TriggerOperation,
	obj metav1.Object,
) bool {
	for _, t := range triggers {
		if !t.Matches(gvk, op, obj.GetLabels()) {
			continue
		}

		if !s.namespaceSelectorMatches(ctx, t.NamespaceSelector, obj.GetNamespace()) {
			continue
		}

		return true
	}

	return false
}

func (s *globalTriggerSink) namespaceSelectorMatches(ctx context.Context, sel *metav1.LabelSelector, namespace string) bool {
	// A namespace selector is meaningless for cluster-scoped objects, and a nil
	// selector matches everything without fetching the Namespace.
	if sel == nil || namespace == "" {
		return true
	}

	ns := &corev1.Namespace{}
	if err := s.reader.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return false
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, sel)

	return err == nil && ok
}

// namespacedTriggerSink enqueues TenantResources whose triggers match a change,
// scoped to objects living in the namespaces of the resource's own Tenant.
// Like the global sink, capsule-replicated objects are not filtered out; the
// self-trigger echo converges once the rendered output stops changing and the
// workqueue dedups bursts by owner key.
type namespacedTriggerSink struct {
	reader  client.Reader
	enqueue func(types.NamespacedName)
	log     logr.Logger
}

func (s *namespacedTriggerSink) Notify(ctx context.Context, gvk schema.GroupVersionKind, op watch.Operation, obj metav1.Object) {
	// Namespaced TenantResources only react to namespaced objects.
	if obj.GetNamespace() == "" {
		return
	}

	objTenant := s.tenantOfNamespace(ctx, obj.GetNamespace())
	if objTenant == "" {
		return
	}

	list := &capsulev1beta2.TenantResourceList{}
	if err := s.reader.List(ctx, list, client.MatchingFields{
		tenantresource.TriggersIndexerFieldName: tenantresource.TriggerGKKey(gvk.GroupKind()),
	}); err != nil {
		s.log.V(2).Error(err, "failed to list tenantresources for trigger", "gvk", gvk.String())

		return
	}

	// watch.Operation values match TriggerOperation by construction.
	op2 := capsulev1beta2.TriggerOperation(op)

	// Many TenantResources share namespaces; resolve each namespace's tenant at
	// most once per event, and only for resources whose triggers match at all.
	tenants := map[string]string{obj.GetNamespace(): objTenant}

	for i := range list.Items {
		tr := &list.Items[i]

		if !triggerMatches(tr.Spec.Triggers, gvk, op2, obj.GetLabels()) {
			continue
		}

		// Only fire for objects in a namespace of the resource's own Tenant.
		trTenant, ok := tenants[tr.GetNamespace()]
		if !ok {
			trTenant = s.tenantOfNamespace(ctx, tr.GetNamespace())
			tenants[tr.GetNamespace()] = trTenant
		}

		if trTenant != objTenant {
			continue
		}

		s.enqueue(types.NamespacedName{Name: tr.Name, Namespace: tr.Namespace})
	}
}

func (s *namespacedTriggerSink) tenantOfNamespace(ctx context.Context, name string) string {
	ns := &corev1.Namespace{}
	if err := s.reader.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
		return ""
	}

	return tenant.TenanLabelValue(ns)
}

// triggerSource is a controller-runtime source that lets trigger sinks enqueue
// reconcile requests straight into the controller's workqueue. It replaces
// source.Channel: instead of wrapping each owner key in a fake typed object,
// pushing it through a buffered channel, and unwrapping it back into a request,
// the sink calls enqueue with the key. The workqueue itself provides the dedup
// by owner key and the backpressure the channel buffer used to approximate.
type triggerSource struct {
	mu    sync.Mutex
	queue workqueue.TypedRateLimitingInterface[reconcile.Request]
}

// Start records the controller's workqueue; it is called once when the manager
// starts.
func (s *triggerSource) Start(_ context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	s.mu.Lock()
	s.queue = q
	s.mu.Unlock()

	return nil
}

// enqueue schedules a reconcile of the owner identified by key. Calls before the
// controller has started are dropped and recovered by the resyncPeriod.
func (s *triggerSource) enqueue(key types.NamespacedName) {
	s.mu.Lock()
	q := s.queue
	s.mu.Unlock()

	if q == nil {
		return
	}

	q.Add(reconcile.Request{NamespacedName: key})
}

// triggerMatches reports whether any trigger reacts to a change of the given kind
// and operation whose object carries the given labels. NamespaceSelector is not
// considered here; namespace scoping for namespaced TenantResources is enforced by
// the caller via the owning Tenant.
func triggerMatches(
	triggers []capsulev1beta2.TriggerSpec,
	gvk schema.GroupVersionKind,
	op capsulev1beta2.TriggerOperation,
	lbls map[string]string,
) bool {
	for _, t := range triggers {
		if t.Matches(gvk, op, lbls) {
			return true
		}
	}

	return false
}

// Owner keys are namespaced by consumer ("triggers/...") so future watch
// consumers (e.g. health checks) can share the manager without collisions.
func globalTriggerOwnerKey(name string) string {
	return "triggers/global/" + name
}

func namespacedTriggerOwnerKey(namespace, name string) string {
	return "triggers/namespaced/" + namespace + "/" + name
}

// newTriggersCondition builds the status condition summarizing the armed watches.
func newTriggersCondition(obj client.Object, count int, err error) capsulemeta.Condition {
	cond := capsulemeta.NewTriggersCondition(obj)

	switch {
	case err != nil:
		cond.Status = metav1.ConditionFalse
		cond.Reason = capsulemeta.FailedReason
		cond.Message = err.Error()
	case count == 0:
		cond.Message = "no triggers configured"
	default:
		cond.Message = fmt.Sprintf("watching %d kind(s)", count)
	}

	return cond
}

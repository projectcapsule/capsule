// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Cache-first lookups for admission.
//
// Most namespaced writes in a Tenant namespace pass through several admission
// handlers, and each of them resolves the Namespace, its Tenant and the managed
// RuleStatus. Through the direct API reader those are separate round trips per
// handler, and they share the manager's client-side rate limiter with every
// controller. The helpers below read the manager's informer cache first and
// use the direct API reader only when the cached state cannot be trusted to
// give the answer the API server would give:
//
//   - A cached Get fails. NotFound means the informer has not observed a new
//     object yet: a cold cache, or a Namespace, Tenant or RuleStatus created a
//     moment ago. Any other error comes from an informer that is not started
//     or not synced yet. The cached reads are bounded by cacheFirstReadTimeout,
//     so requests served while the manager starts do not wait for the sync.
//   - The cached Namespace has a deletionTimestamp. Requests in a terminating
//     Namespace are mostly cleanup, and the Namespace may already be gone and
//     recreated under the same name with another owner.
//   - The cached Namespace does not carry exactly one Tenant ownerReference
//     with a non-empty UID. Without one, the Namespace may have been adopted
//     after the cached revision, and trusting the cache would skip the Tenant
//     policies. With several, the API reader decides which one counts. Without
//     a UID, the cached Tenant cannot be matched to the reference.
//   - The cached Tenant UID differs from that ownerReference UID: the Tenant
//     was deleted and recreated with the same name, or one of the two
//     informers lags behind the other.
//   - The cached RuleStatus is missing, has not observed its current
//     generation yet (see IsRuleStatusObserved), is not controlled by the
//     resolved Tenant, or was created before the resolved Namespace. The last
//     two mean that it belongs to an earlier Namespace with the same name,
//     under another Tenant or the same one, which the RuleStatus informer has
//     not seen deleted yet. Only the RuleStatus is read again from the API
//     reader then. When the ownership itself falls back, the RuleStatus is
//     read from the API reader too, so a cached RuleStatus is never paired
//     with an ownership answer the cache could not give.
//
// A fallback runs the existing authoritative lookup and its result is used as
// is: its Tenant, its absence, and its errors, such as an ownerReference UID
// mismatch, which the handlers turn into an errored response. When the API
// RuleStatus is missing or pending as well, the caller renders the rules from
// the Tenant and Namespace snapshot. That snapshot then comes from the API
// reader too: after a RuleStatus-only fallback the Namespace and its Tenant
// are read again, so the rendered rules never come from cached state.
//
// Accepted staleness: when every check passes, the answer can lag the API
// server by the watch delay of the manager informers, usually milliseconds.
// A Tenant rule change or a Namespace label change reaches admission after
// that delay. On the leader replica the rulestatus controller reads the same
// informers, so it publishes the effective rules after the same delay; the
// other replicas answer from their own informers, which lag independently.
// A move of the Namespace to another existing Tenant lags by the same delay.
// Callers whose decision needs the authoritative state keep the direct API
// reader: authorization decisions, such as ServiceAccount promotion and the
// PersistentVolume tenant checks of the PersistentVolumeClaim handlers,
// DELETE requests, quota and ledger accounting, the replications webhook,
// the Namespace webhooks and termination handling.

// cacheFirstReadTimeout bounds the cached reads of one lookup while the
// informers are not synced yet. A synced informer answers from memory.
const cacheFirstReadTimeout = 25 * time.Millisecond

// GetTenantRuleStatusByNamespaceCacheFirst resolves the owning Tenant, the
// Namespace snapshot and the managed RuleStatus of a Namespace, reading the
// cache first. The RuleStatus is nil when the API reader does not find one,
// and it can be pending (see IsRuleStatusObserved): the caller then renders
// the rules from the snapshot. The errors keep the precedence of the API
// path: an ownership error first, then no Tenant (the RuleStatus error is
// ignored), then a RuleStatus error. A nil cache reads only the API reader.
func GetTenantRuleStatusByNamespaceCacheFirst(
	ctx context.Context,
	cache client.Reader,
	reader client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, *corev1.Namespace, *capsulev1beta2.RuleStatus, error) {
	if cache != nil {
		if tnt, ns, rs, ok, err := cachedTenantRuleStatus(ctx, cache, reader, namespace); ok {
			return tnt, ns, rs, err
		}
	}

	return getTenantRuleStatusByNamespace(ctx, reader, namespace)
}

// getTenantRuleStatusByNamespace is the authoritative path: it reads the
// RuleStatus while the ownership resolves.
func getTenantRuleStatusByNamespace(
	ctx context.Context,
	reader client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, *corev1.Namespace, *capsulev1beta2.RuleStatus, error) {
	type ruleStatusResult struct {
		rs  *capsulev1beta2.RuleStatus
		err error
	}

	ruleStatusCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered: the read never blocks on its send, whether or not it is awaited.
	result := make(chan ruleStatusResult, 1)

	go func() {
		rs, err := GetManagedRuleStatus(ruleStatusCtx, reader, &corev1.Namespace{Name: namespace})

		result <- ruleStatusResult{rs: rs, err: client.IgnoreNotFound(err)}
	}()

	tnt, ns, err := GetTenantByNamespaceWithSnapshot(ctx, reader, namespace)
	if err != nil || tnt == nil {
		// The RuleStatus is not needed: cancel its read, and wait only for the
		// cancelled read to return, so that no goroutine outlives the request.
		cancel()
		<-result

		return nil, ns, nil, err
	}

	got := <-result
	if got.err != nil {
		return tnt, ns, nil, got.err
	}

	return tnt, ns, got.rs, nil
}

// cachedTenantRuleStatus resolves the ownership from the cache. When the cache
// answers it (ok), the RuleStatus comes from the cache when it passes the
// checks described above, and from the API reader otherwise.
func cachedTenantRuleStatus(
	ctx context.Context,
	cache client.Reader,
	reader client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, *corev1.Namespace, *capsulev1beta2.RuleStatus, bool, error) {
	cacheCtx, cancel := context.WithTimeout(ctx, cacheFirstReadTimeout)
	tnt, ns, cached, ok := cachedProfile(cacheCtx, cache, namespace)

	cancel()

	if !ok {
		return nil, nil, nil, false, nil
	}

	if cached != nil {
		return tnt, ns, cached, true, nil
	}

	// The API reads use the request context: the cache context may have
	// expired on an informer that is not synced.
	rs, err := GetManagedRuleStatus(ctx, reader, ns)
	if err = client.IgnoreNotFound(err); err != nil {
		return tnt, ns, nil, true, err
	}

	if IsRuleStatusObserved(rs) {
		return tnt, ns, rs, true, nil
	}

	// The caller renders the rules from the Tenant and the Namespace: read both
	// from the API reader instead of rendering the cached snapshot.
	tnt, ns, err = GetTenantByNamespaceWithSnapshot(ctx, reader, namespace)
	if err != nil || tnt == nil {
		return nil, ns, nil, true, err
	}

	return tnt, ns, rs, true, nil
}

// cachedProfile reads the Namespace, its Tenant and its managed RuleStatus
// from the cache. It reports ok when the ownership passes the checks described
// above; the RuleStatus is nil unless it passes them too.
func cachedProfile(
	ctx context.Context,
	cache client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, *corev1.Namespace, *capsulev1beta2.RuleStatus, bool) {
	tnt, ns, ok := cachedNamespaceTenant(ctx, cache, namespace)
	if !ok {
		return nil, nil, nil, false
	}

	return tnt, ns, cachedRuleStatus(ctx, cache, tnt, ns), true
}

// GetTenantNameByNamespaceCacheFirst returns the name of the Tenant that owns
// a Namespace, like GetTenantNameByNamespace, reading the cache first. It does
// not read the Tenant: the name comes from the Namespace ownerReference, as it
// does on the API path. A nil cache reads only the API reader.
func GetTenantNameByNamespaceCacheFirst(
	ctx context.Context,
	cache client.Reader,
	reader client.Reader,
	namespace string,
) (string, error) {
	if cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, cacheFirstReadTimeout)
		_, ref, ok := cachedTenantNamespace(cacheCtx, cache, namespace)

		cancel()

		if ok {
			return ref.Name, nil
		}
	}

	return GetTenantNameByNamespace(ctx, reader, namespace)
}

// IsRuleStatusObserved reports whether the effective rules in the status of a
// RuleStatus belong to its current generation. A newly published spec can
// precede its effective status.
func IsRuleStatusObserved(rs *capsulev1beta2.RuleStatus) bool {
	return rs != nil && rs.Status.ObservedGeneration == rs.Generation
}

// cachedNamespaceTenant returns the cached Namespace and its cached Tenant
// when both pass the checks described above.
func cachedNamespaceTenant(
	ctx context.Context,
	cache client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, *corev1.Namespace, bool) {
	ns, ref, ok := cachedTenantNamespace(ctx, cache, namespace)
	if !ok {
		return nil, nil, false
	}

	tnt := &capsulev1beta2.Tenant{}
	if err := cache.Get(ctx, client.ObjectKey{Name: ref.Name}, tnt); err != nil {
		return nil, nil, false
	}

	if tnt.GetUID() != ref.UID {
		return nil, nil, false
	}

	return tnt, ns, true
}

// cachedTenantNamespace returns the cached Namespace and its only Tenant
// ownerReference when the Namespace passes the checks described above.
func cachedTenantNamespace(
	ctx context.Context,
	cache client.Reader,
	namespace string,
) (*corev1.Namespace, metav1.OwnerReference, bool) {
	ns := &corev1.Namespace{}
	if err := cache.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return nil, metav1.OwnerReference{}, false
	}

	if ns.GetDeletionTimestamp() != nil {
		return nil, metav1.OwnerReference{}, false
	}

	var (
		ref   metav1.OwnerReference
		count int
	)

	for _, or := range ns.GetOwnerReferences() {
		if IsTenantOwnerReference(or) {
			ref = or
			count++
		}
	}

	if count != 1 || ref.UID == "" {
		return nil, metav1.OwnerReference{}, false
	}

	return ns, ref, true
}

// cachedRuleStatus returns the cached managed RuleStatus of the Namespace ns
// owned by tnt when it passes the checks described above, and nil otherwise.
func cachedRuleStatus(
	ctx context.Context,
	cache client.Reader,
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) *capsulev1beta2.RuleStatus {
	rs, err := GetManagedRuleStatus(ctx, cache, ns)
	if err != nil {
		return nil
	}

	// The tenant controller sets the Tenant as the controller of the
	// RuleStatus. A RuleStatus of an earlier Namespace with the same name has
	// another controller, or was created before this Namespace.
	if ref := metav1.GetControllerOfNoCopy(rs); ref == nil || ref.UID != tnt.GetUID() {
		return nil
	}

	if rs.CreationTimestamp.Before(&ns.CreationTimestamp) {
		return nil
	}

	if !IsRuleStatusObserved(rs) {
		return nil
	}

	return rs
}

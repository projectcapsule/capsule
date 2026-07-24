// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const immediatePendingDeleteRequeue = 2 * time.Second

func usagePercentage(used, limit resource.Quantity) float64 {
	if limit.MilliValue() <= 0 {
		return 0
	}

	return (float64(used.MilliValue()) / float64(limit.MilliValue())) * 100
}

type GroupedTarget struct {
	GVK     schema.GroupVersionKind
	Targets []capsulev1beta2.CustomQuotaStatusTarget
}

type CompiledTarget struct {
	capsulev1beta2.CustomQuotaStatusTarget

	CompiledPath      *jsonpath.CompiledJSONPath
	CompiledSelectors []selectors.CompiledSelectorWithFields
}

func CompileTargets(
	jcache *cache.JSONPathCache,
	targets []capsulev1beta2.CustomQuotaStatusTarget,
) ([]cache.CompiledTarget, error) {
	out := make([]cache.CompiledTarget, 0, len(targets))

	for _, target := range targets {
		pt := cache.CompiledTarget{
			CustomQuotaStatusTarget: target,
		}

		switch target.Operation {
		case quota.OpCount:
			// no usage path needed

		case quota.OpAdd, quota.OpSub:
			compiledPath, err := jcache.GetOrCompile(target.Path)
			if err != nil {
				return nil, fmt.Errorf(
					"compile usage path %q for %s %q: %w",
					target.Path,
					target.String(),
					target.Operation,
					err,
				)
			}

			pt.CompiledPath = compiledPath

		default:
			return nil, fmt.Errorf("unsupported operation %q for %s", target.Operation, target.String())
		}

		compiledSelectors, err := CompileSelectorsWithFields(jcache, target.Selectors)
		if err != nil {
			return nil, fmt.Errorf(
				"compile selectors for %s: %w",
				target.String(),
				err,
			)
		}

		pt.CompiledSelectors = compiledSelectors

		out = append(out, pt)
	}

	return out, nil
}

func MatchesCompiledSelectorsWithFields(
	u unstructured.Unstructured,
	selectors []selectors.CompiledSelectorWithFields,
) (bool, error) {
	if len(selectors) == 0 {
		return true, nil
	}

	itemLabels := labels.Set(u.GetLabels())

	for _, sel := range selectors {
		if !sel.LabelSelector.Matches(itemLabels) {
			continue
		}

		allFieldsMatch := true

		for _, matcher := range sel.FieldMatchers {
			ok, err := evaluateCompiledFieldSelector(u, matcher)
			if err != nil {
				return false, err
			}

			if !ok {
				allFieldsMatch = false

				break
			}
		}

		if allFieldsMatch {
			return true, nil
		}
	}

	return false, nil
}

func evaluateCompiledFieldSelector(
	u unstructured.Unstructured,
	matcher selectors.CompiledFieldSelector,
) (bool, error) {
	switch matcher.Operator {
	case selectors.FieldSelectorTruthy:
		return jsonpath.EvaluateTruthyFromCompiled(u, matcher.Compiled)

	case selectors.FieldSelectorEquals:
		actual, err := matcher.Compiled.Execute(u)
		if err != nil {
			return false, err
		}

		return strings.TrimSpace(actual) == matcher.Value, nil

	case selectors.FieldSelectorNotEquals:
		actual, err := matcher.Compiled.Execute(u)
		if err != nil {
			return false, err
		}

		return strings.TrimSpace(actual) != matcher.Value, nil

	default:
		return false, fmt.Errorf("unsupported field selector operator %q", matcher.Operator)
	}
}

func MakeCustomQuotaCacheKey(namespace, name string) string {
	return namespace + "/" + name
}

func MakeGlobalCustomQuotaCacheKey(name string) string {
	return "C/" + name
}

func CompileSelectorsWithFields(
	cache *cache.JSONPathCache,
	in []selectors.SelectorWithFields,
) ([]selectors.CompiledSelectorWithFields, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]selectors.CompiledSelectorWithFields, 0, len(in))

	for _, selector := range in {
		lblSel := labels.Everything()

		if selector.LabelSelector != nil {
			compiled, err := metav1.LabelSelectorAsSelector(selector.LabelSelector)
			if err != nil {
				return nil, fmt.Errorf("compile label selector with fields: %w", err)
			}

			lblSel = compiled
		}

		fieldMatchers := make([]selectors.CompiledFieldSelector, 0, len(selector.FieldSelectors))

		for _, raw := range selector.FieldSelectors {
			compiledSelector, err := utils.CompileFieldSelector(cache, raw)
			if err != nil {
				return nil, fmt.Errorf("compile field selector %q: %w", raw, err)
			}

			fieldMatchers = append(fieldMatchers, compiledSelector)
		}

		out = append(out, selectors.CompiledSelectorWithFields{
			LabelSelector: lblSel,
			FieldMatchers: fieldMatchers,
		})
	}

	return out, nil
}

func shouldIgnoreLedgerEnsureError(err error) bool {
	if err == nil {
		return false
	}

	if apierrors.IsNotFound(err) {
		return true
	}

	if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
		return true
	}

	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		if statusErr.ErrStatus.Reason == metav1.StatusReasonForbidden &&
			strings.Contains(statusErr.ErrStatus.Message, "because it is being terminated") {
			return true
		}
	}

	return false
}

func getResourcesByGVK(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	kubeClient client.Reader,
	namespaced bool,
	scopeSelectors []metav1.LabelSelector,
	namespaces ...string,
) ([]unstructured.Unstructured, error) {
	compiledSelectors, err := compileScopeSelectors(scopeSelectors)
	if err != nil {
		return nil, err
	}

	filterByNamespace, namespaceSet := namespaceFilter(namespaces)
	listNamespaces := resourceListNamespaces(namespaced, filterByNamespace, namespaceSet)

	items := make([]unstructured.Unstructured, 0)
	seen := make(map[string]struct{})

	for _, namespace := range listNamespaces {
		candidates, err := listResourcesForGVK(ctx, gvk, kubeClient, namespace, compiledSelectors)
		if err != nil {
			return nil, err
		}

		for i := range candidates {
			item := candidates[i]
			if !resourceMatchesQuotaScope(item, filterByNamespace, namespaceSet, compiledSelectors) {
				continue
			}

			key := client.ObjectKeyFromObject(&item).String()
			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}

			items = append(items, item)
		}
	}

	// Sort by oldest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetCreationTimestamp().Time.Before(items[j].GetCreationTimestamp().Time)
	})

	return items, nil
}

func compileScopeSelectors(scopeSelectors []metav1.LabelSelector) ([]labels.Selector, error) {
	compiledSelectors := make([]labels.Selector, 0, len(scopeSelectors))

	for _, selector := range scopeSelectors {
		compiled, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}

		compiledSelectors = append(compiledSelectors, compiled)
	}

	return compiledSelectors, nil
}

func namespaceFilter(namespaces []string) (filter bool, namespaceSet map[string]struct{}) {
	namespaceSet = make(map[string]struct{}, len(namespaces))

	for _, namespace := range namespaces {
		if namespace == "*" {
			return false, nil
		}

		namespaceSet[namespace] = struct{}{}
	}

	return true, namespaceSet
}

func resourceListNamespaces(
	namespaced bool,
	filterByNamespace bool,
	namespaceSet map[string]struct{},
) []string {
	if !namespaced || !filterByNamespace {
		return []string{""}
	}

	orderedNamespaces := make([]string, 0, len(namespaceSet))
	for namespace := range namespaceSet {
		orderedNamespaces = append(orderedNamespaces, namespace)
	}

	sort.Strings(orderedNamespaces)

	return orderedNamespaces
}

func listResourcesForGVK(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	kubeClient client.Reader,
	namespace string,
	compiledSelectors []labels.Selector,
) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	options := make([]client.ListOption, 0, 2)
	if namespace != "" {
		options = append(options, client.InNamespace(namespace))
	}

	// A single scope selector can be pushed into the informer/API list.
	// Multiple selectors have OR semantics and are filtered below.
	if len(compiledSelectors) == 1 {
		options = append(options, client.MatchingLabelsSelector{Selector: compiledSelectors[0]})
	}

	if err := kubeClient.List(ctx, list, options...); err != nil {
		return nil, err
	}

	return list.Items, nil
}

func resourceMatchesQuotaScope(
	item unstructured.Unstructured,
	filterByNamespace bool,
	namespaceSet map[string]struct{},
	compiledSelectors []labels.Selector,
) bool {
	// Skip objects that are already definitely deleting:
	// deletionTimestamp is set and there are no finalizers left.
	if item.GetDeletionTimestamp() != nil && len(item.GetFinalizers()) == 0 {
		return false
	}

	// Namespace filtering remains necessary for cluster-wide lists and
	// preserves the previous behavior for cluster-scoped targets.
	if filterByNamespace {
		if _, ok := namespaceSet[item.GetNamespace()]; !ok {
			return false
		}
	}

	if len(compiledSelectors) == 0 {
		return true
	}

	itemLabels := labels.Set(item.GetLabels())
	for _, selector := range compiledSelectors {
		if selector.Matches(itemLabels) {
			return true
		}
	}

	return false
}

func minDurationPtr(cur *time.Duration, cand time.Duration) *time.Duration {
	if cand < 0 {
		cand = 0
	}

	if cur == nil || cand < *cur {
		return &cand
	}

	return cur
}

func pendingDeleteStillPresent(
	pd capsulev1beta2.QuantityLedgerPendingDelete,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) bool {
	for _, claim := range claims {
		if sameLedgerObject(pd.ObjectRef, claim) {
			return true
		}
	}

	return false
}

const (
	unresolvedReservationInitialRequeue = 2 * time.Second
	unresolvedReservationRequeue        = 5 * time.Second
	unresolvedReservationLongRequeue    = 15 * time.Second
	ledgerWorkDebounce                  = 500 * time.Millisecond
	ledgerWorkMaximumDelay              = 2 * time.Second
)

func quantityLedgerWorkDelay(
	now time.Time,
	ledger *capsulev1beta2.QuantityLedger,
) (hasWork bool, delay time.Duration) {
	var oldest, newest time.Time

	record := func(ts time.Time) {
		hasWork = true

		if ts.IsZero() {
			return
		}

		if oldest.IsZero() || ts.Before(oldest) {
			oldest = ts
		}

		if newest.IsZero() || ts.After(newest) {
			newest = ts
		}
	}

	for _, reservation := range ledger.Status.Reservations {
		ts := reservation.UpdatedAt.Time
		if ts.IsZero() {
			ts = reservation.CreatedAt.Time
		}

		record(ts)
	}

	for _, pendingDelete := range ledger.Status.PendingDeletes {
		record(pendingDelete.CreatedAt.Time)
	}

	if !hasWork || oldest.IsZero() || newest.IsZero() {
		return hasWork, 0
	}

	readyAt := newest.Add(ledgerWorkDebounce)

	maximumAt := oldest.Add(ledgerWorkMaximumDelay)
	if maximumAt.Before(readyAt) {
		readyAt = maximumAt
	}

	if !now.Before(readyAt) {
		return true, 0
	}

	return true, readyAt.Sub(now)
}

func nextReservationMaterializationRequeue(
	now metav1.Time,
	res capsulev1beta2.QuantityLedgerReservation,
) time.Duration {
	if res.ExpiresAt == nil {
		return unresolvedReservationRequeue
	}

	untilExpiry := res.ExpiresAt.Sub(now.Time)
	if untilExpiry <= 0 {
		return 0
	}

	age := now.Sub(res.UpdatedAt.Time)

	var candidate time.Duration

	switch {
	case age < 5*time.Second:
		candidate = unresolvedReservationInitialRequeue
	case age < 30*time.Second:
		candidate = unresolvedReservationRequeue
	default:
		candidate = unresolvedReservationLongRequeue
	}

	if untilExpiry < candidate {
		return untilExpiry
	}

	return candidate
}

func reconcileQuantityLedgerAllocation(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	log logr.Logger,
	key types.NamespacedName,
	observedUsed resource.Quantity,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) (*time.Duration, error) {
	var requeueAfter *time.Duration

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		// Conflicts here are normally caused by admission updating the ledger.
		// The informer cache can keep returning the same stale resourceVersion,
		// making RetryOnConflict ineffective, so retries must read directly.
		if err := reader.Get(ctx, key, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		now := metav1.Now()
		pendingDeleteTTL := 30 * time.Second

		pendingDeletePresent := make([]bool, len(ledger.Status.PendingDeletes))
		confirmedTransitions := make(map[string][]capsulev1beta2.QuantityLedgerObjectRef)

		for i, pendingDelete := range ledger.Status.PendingDeletes {
			pendingDeletePresent[i] = pendingDeleteStillPresent(pendingDelete, claims)
			if pendingDeletePresent[i] {
				continue
			}

			key := ledgerReservationObjectKey(pendingDelete.ObjectRef)
			confirmedTransitions[key] = append(confirmedTransitions[key], pendingDelete.ObjectRef)
		}

		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		materializedThrough := materializedReservationPositions(ledger.Status.Reservations, claims)

		for i, res := range ledger.Status.Reservations {
			materialized := materializedThrough[ledgerReservationObjectKey(res.ObjectRef)] > i
			transitioned := reservationHasConfirmedTransition(res, confirmedTransitions)
			expired := res.ExpiresAt != nil && res.ExpiresAt.Before(&now)

			log.V(5).Info("evaluating ledger reservation",
				"ledger", key.String(),
				"reservationID", res.ID,
				"usage", res.Usage.String(),
				"uid", string(res.ObjectRef.UID),
				"group", res.ObjectRef.APIGroup,
				"version", res.ObjectRef.APIVersion,
				"kind", res.ObjectRef.Kind,
				"namespace", res.ObjectRef.Namespace,
				"name", res.ObjectRef.Name,
				"materialized", materialized,
				"transitioned", transitioned,
				"expired", expired,
			)

			switch {
			case materialized:
				continue

			case transitioned:
				// A persisted update/delete moved this object out of the
				// matching claims before reconciliation observed its earlier
				// state. Its pending-delete hint confirms that the admission
				// operation materialized, so older reservations for the same
				// object can be released without waiting for their TTL.
				continue

			case expired:
				continue

			default:
				activeReservations = append(activeReservations, res)

				requeueAfter = minDurationPtr(
					requeueAfter,
					nextReservationMaterializationRequeue(now, res),
				)
			}
		}

		activeDeletes := make([]capsulev1beta2.QuantityLedgerPendingDelete, 0, len(ledger.Status.PendingDeletes))

		for i, pd := range ledger.Status.PendingDeletes {
			stillPresent := pendingDeletePresent[i]
			expired := now.Sub(pd.CreatedAt.Time) >= pendingDeleteTTL

			log.V(5).Info("evaluating pending delete",
				"ledger", key.String(),
				"uid", string(pd.ObjectRef.UID),
				"group", pd.ObjectRef.APIGroup,
				"version", pd.ObjectRef.APIVersion,
				"kind", pd.ObjectRef.Kind,
				"namespace", pd.ObjectRef.Namespace,
				"name", pd.ObjectRef.Name,
				"stillPresent", stillPresent,
				"expired", expired,
			)

			// Pending deletes are admission hints, not durable desired state.
			// The admitted update/delete may fail after the webhook returns,
			// and a transient policy snapshot must not leave a hint that
			// requeues this quota forever. Once the hint expires, the current
			// observed claims are authoritative.
			if !stillPresent || expired {
				continue
			}

			activeDeletes = append(activeDeletes, pd)
			requeueAfter = minDurationPtr(requeueAfter, immediatePendingDeleteRequeue)
		}

		reserved := resource.MustParse("0")
		for _, res := range activeReservations {
			reserved.Add(quantityLedgerReservationDelta(res))
		}

		allocated := observedUsed.DeepCopy()
		allocated.Add(reserved)
		quota.ClampQuantityToZero(&allocated)

		originalStatus := ledger.Status.DeepCopy()

		ledger.Status.Reservations = activeReservations
		ledger.Status.PendingDeletes = activeDeletes
		ledger.Status.Reserved = reserved
		ledger.Status.Allocated = allocated

		if reflect.DeepEqual(*originalStatus, ledger.Status) {
			return nil
		}

		return c.Status().Update(ctx, ledger)
	})
	if err != nil {
		return nil, err
	}

	return requeueAfter, nil
}

func reservationHasConfirmedTransition(
	reservation capsulev1beta2.QuantityLedgerReservation,
	confirmed map[string][]capsulev1beta2.QuantityLedgerObjectRef,
) bool {
	for _, ref := range confirmed[ledgerReservationObjectKey(reservation.ObjectRef)] {
		if sameLedgerObjectRef(reservation.ObjectRef, ref) {
			return true
		}
	}

	return false
}

func quantityLedgerReservationDelta(
	res capsulev1beta2.QuantityLedgerReservation,
) resource.Quantity {
	if res.Delta == nil {
		return res.Usage.DeepCopy()
	}

	return res.Delta.DeepCopy()
}

func materializedReservationPositions(
	reservations []capsulev1beta2.QuantityLedgerReservation,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) map[string]int {
	positions := make(map[string]int)

	// Reservations are kept in ledger update order. When a later update for
	// the same object is observed, its persisted usage also supersedes every
	// earlier reservation for that object. Clearing the prefix avoids holding
	// already-materialized deltas until their TTL after rapid updates.
	for i, reservation := range reservations {
		if reservationMaterializedLedger(reservation, claims) {
			positions[ledgerReservationObjectKey(reservation.ObjectRef)] = i + 1
		}
	}

	return positions
}

func ledgerReservationObjectKey(ref capsulev1beta2.QuantityLedgerObjectRef) string {
	return strings.Join([]string{
		ref.APIGroup,
		ref.APIVersion,
		ref.Kind,
		ref.Namespace,
		ref.Name,
	}, "\x00")
}

func reservationMaterializedLedger(
	res capsulev1beta2.QuantityLedgerReservation,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) bool {
	for _, claim := range claims {
		if !sameLedgerObject(res.ObjectRef, claim) {
			continue
		}

		// Important for updates:
		// UID/name match alone is not enough. The controller must have observed
		// the same usage that the webhook reserved.
		if claim.Usage.Cmp(res.Usage) != 0 {
			continue
		}

		return true
	}

	return false
}

func sameLedgerObject(
	ref capsulev1beta2.QuantityLedgerObjectRef,
	claim capsulev1beta2.CustomQuotaClaimItem,
) bool {
	return sameLedgerObjectRef(ref, capsulev1beta2.QuantityLedgerObjectRef{
		APIGroup:   claim.Group,
		APIVersion: claim.Version,
		Kind:       claim.Kind,
		Namespace:  string(claim.Namespace),
		Name:       claim.Name,
		UID:        claim.UID,
	})
}

func sameLedgerObjectRef(
	a capsulev1beta2.QuantityLedgerObjectRef,
	b capsulev1beta2.QuantityLedgerObjectRef,
) bool {
	if a.APIGroup != b.APIGroup ||
		a.APIVersion != b.APIVersion ||
		a.Kind != b.Kind ||
		a.Namespace != b.Namespace ||
		a.Name != b.Name {
		return false
	}

	// CREATE admissions often do not have a UID yet.
	if a.UID != "" && b.UID != "" {
		return a.UID == b.UID
	}

	return true
}

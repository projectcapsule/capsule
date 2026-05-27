// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"fmt"
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
)

const immediatePendingDeleteRequeue = 500 * time.Millisecond

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
			ok, err := jsonpath.EvaluateTruthyFromCompiled(u, matcher)
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

		fieldMatchers := make([]*jsonpath.CompiledJSONPath, 0, len(selector.FieldSelectors))

		for _, path := range selector.FieldSelectors {
			compiledPath, err := cache.GetOrCompile(path)
			if err != nil {
				return nil, fmt.Errorf("compile field selector path %q: %w", path, err)
			}

			fieldMatchers = append(fieldMatchers, compiledPath)
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
	scopeSelectors []metav1.LabelSelector,
	namespaces ...string,
) ([]unstructured.Unstructured, error) {
	compiledSelectors := make([]labels.Selector, 0, len(scopeSelectors))

	for _, selector := range scopeSelectors {
		sel, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}

		compiledSelectors = append(compiledSelectors, sel)
	}

	filterByNamespace := true
	namespaceSet := make(map[string]struct{}, len(namespaces))

	for _, ns := range namespaces {
		if ns == "*" {
			filterByNamespace = false
			namespaceSet = nil

			break
		}

		namespaceSet[ns] = struct{}{}
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	if err := kubeClient.List(ctx, list); err != nil {
		return nil, err
	}

	items := make([]unstructured.Unstructured, 0, len(list.Items))
	seen := make(map[string]struct{}, len(list.Items))

	for i := range list.Items {
		item := list.Items[i]

		// Skip objects that are already definitely deleting:
		// deletionTimestamp is set and there are no finalizers left.
		if item.GetDeletionTimestamp() != nil && len(item.GetFinalizers()) == 0 {
			continue
		}

		// Namespace filter
		if filterByNamespace {
			if _, ok := namespaceSet[item.GetNamespace()]; !ok {
				continue
			}
		}

		// Label selector filter (OR semantics)
		if len(compiledSelectors) > 0 {
			itemLabels := labels.Set(item.GetLabels())

			matched := false

			for _, sel := range compiledSelectors {
				if sel.Matches(itemLabels) {
					matched = true

					break
				}
			}

			if !matched {
				continue
			}
		}

		key := item.GetNamespace() + "/" + item.GetName()
		if item.GetNamespace() == "" {
			key = item.GetName()
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}

		items = append(items, item)
	}

	// Sort by oldest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetCreationTimestamp().Time.Before(items[j].GetCreationTimestamp().Time)
	})

	return items, nil
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
		if pd.ObjectRef.UID != "" && claim.UID != "" && pd.ObjectRef.UID == claim.UID {
			return true
		}

		if pd.ObjectRef.APIGroup == claim.Group &&
			pd.ObjectRef.APIVersion == claim.Version &&
			pd.ObjectRef.Kind == claim.Kind &&
			pd.ObjectRef.Namespace == string(claim.Namespace) &&
			pd.ObjectRef.Name == claim.Name {
			return true
		}
	}

	return false
}

const unresolvedReservationRequeue = 250 * time.Millisecond

func nextReservationMaterializationRequeue(
	now metav1.Time,
	res capsulev1beta2.QuantityLedgerReservation,
) time.Duration {
	if res.ExpiresAt == nil {
		return unresolvedReservationRequeue
	}

	untilExpiry := time.Until(res.ExpiresAt.Time)
	if untilExpiry <= 0 {
		return 0
	}

	if untilExpiry < unresolvedReservationRequeue {
		return untilExpiry
	}

	return unresolvedReservationRequeue
}

func reconcileQuantityLedgerAllocation(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	key types.NamespacedName,
	observedUsed resource.Quantity,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) (*time.Duration, error) {
	var requeueAfter *time.Duration

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := c.Get(ctx, key, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		now := metav1.Now()
		pendingDeleteTTL := 30 * time.Second

		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))

		for _, res := range ledger.Status.Reservations {
			materialized := reservationMaterializedLedger(res, claims)
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
				"expired", expired,
			)

			switch {
			case materialized:
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

		for _, pd := range ledger.Status.PendingDeletes {
			stillPresent := pendingDeleteStillPresent(pd, claims)
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

			if stillPresent {
				activeDeletes = append(activeDeletes, pd)
				requeueAfter = minDurationPtr(requeueAfter, immediatePendingDeleteRequeue)
			}
		}

		reserved := resource.MustParse("0")
		for _, res := range activeReservations {
			reserved.Add(res.Usage)
		}

		allocated := observedUsed.DeepCopy()
		allocated.Add(reserved)
		quota.ClampQuantityToZero(&allocated)

		ledger.Status.Reservations = activeReservations
		ledger.Status.PendingDeletes = activeDeletes
		ledger.Status.Reserved = reserved
		ledger.Status.Allocated = allocated

		return c.Status().Update(ctx, ledger)
	})
	if err != nil {
		return nil, err
	}

	return requeueAfter, nil
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
	if ref.APIGroup != claim.Group ||
		ref.APIVersion != claim.Version ||
		ref.Kind != claim.Kind ||
		ref.Namespace != string(claim.Namespace) ||
		ref.Name != claim.Name {
		return false
	}

	// CREATE admissions often do not have a UID yet.
	if ref.UID != "" && claim.UID != "" {
		return ref.UID == claim.UID
	}

	return true
}

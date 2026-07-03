// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/health"
)

// maxUnhealthyInMessage caps how many offending objects are named in the Healthy
// condition message. The complete, non-truncated per-item health is recorded on
// status.processedItems.
const maxUnhealthyInMessage = 3

// evaluateHealthCondition evaluates the health of every successfully-applied item,
// records the per-item health on each entry (mutating items in place), and returns
// the aggregate Healthy condition for obj.
//
// The impersonated client is used to fetch live objects so RBAC stays consistent
// with the apply. A compile error in the health checks becomes a False/Failed
// condition rather than failing the reconcile.
func evaluateHealthCondition(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	checks []capsulev1beta2.HealthCheckSpec,
	items meta.ProcessedItems,
) meta.Condition {
	checker, err := health.NewChecker(checks)
	if err != nil {
		cond := meta.NewHealthyCondition(obj)
		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.FailedReason
		cond.Message = fmt.Sprintf("invalid healthChecks: %v", err)

		return cond
	}

	var (
		unhealthy   []string
		progressing int
		evaluated   int
	)

	for i := range items {
		item := &items[i]

		// Only evaluate items whose apply succeeded; the rest are covered by Ready.
		if item.Status != metav1.ConditionTrue {
			item.Healthy = metav1.ConditionUnknown
			item.HealthMessage = "apply not completed"

			continue
		}

		res := checkItem(ctx, c, checker, item)
		evaluated++

		//nolint:exhaustive
		switch res.Status {
		case health.StatusHealthy:
			item.Healthy = metav1.ConditionTrue
		case health.StatusUnhealthy:
			item.Healthy = metav1.ConditionFalse

			unhealthy = append(unhealthy, itemDisplayName(item))
		default:
			item.Healthy = metav1.ConditionUnknown

			progressing++
		}

		item.HealthMessage = res.Message
	}

	return aggregateHealth(obj, evaluated, unhealthy, progressing)
}

// checkItem fetches the live object referenced by item and evaluates its health.
// A get error is treated as in-progress, since the object may not be visible yet.
func checkItem(
	ctx context.Context,
	c client.Client,
	checker *health.Checker,
	item *meta.ObjectReferenceStatus,
) health.Result {
	live := &unstructured.Unstructured{}
	live.SetGroupVersionKind(item.GetGVK())

	if err := c.Get(ctx, types.NamespacedName{Name: item.Name, Namespace: item.Namespace}, live); err != nil {
		return health.Result{Status: health.StatusProgressing, Message: err.Error()}
	}

	return checker.Check(live)
}

// aggregateHealth folds the per-item outcomes into a single Healthy condition:
// any unhealthy -> False/Failed, any progressing -> Unknown/Progressing, otherwise
// True/Succeeded (including when there is nothing to evaluate).
func aggregateHealth(obj client.Object, evaluated int, unhealthy []string, progressing int) meta.Condition {
	cond := meta.NewHealthyCondition(obj)

	switch {
	case len(unhealthy) > 0:
		sort.Strings(unhealthy)

		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.FailedReason
		cond.Message = unhealthyMessage(unhealthy)
	case progressing > 0:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = meta.ProgressingReason
		cond.Message = fmt.Sprintf("%d object(s) still progressing", progressing)
	case evaluated == 0:
		cond.Message = "no objects to evaluate"
	default:
		cond.Message = fmt.Sprintf("all %d object(s) are healthy", evaluated)
	}

	return cond
}

// unhealthyMessage renders the first maxUnhealthyInMessage offending objects,
// appending an "and N more" suffix when the list is longer.
func unhealthyMessage(unhealthy []string) string {
	shown := unhealthy
	suffix := ""

	if len(unhealthy) > maxUnhealthyInMessage {
		shown = unhealthy[:maxUnhealthyInMessage]
		suffix = fmt.Sprintf(" and %d more", len(unhealthy)-maxUnhealthyInMessage)
	}

	return fmt.Sprintf("%d unhealthy: %s%s", len(unhealthy), strings.Join(shown, ", "), suffix)
}

// itemDisplayName renders a stable, human-readable identifier for a processed item.
func itemDisplayName(item *meta.ObjectReferenceStatus) string {
	name := item.Name
	if item.Namespace != "" {
		name = item.Namespace + "/" + item.Name
	}

	if item.Kind != "" {
		return fmt.Sprintf("%s (%s)", name, item.Kind)
	}

	return name
}

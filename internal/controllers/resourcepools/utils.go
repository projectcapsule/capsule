// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

// Update the Status of a claim and emit an event if Status changed.
func updateStatusAndEmitEvent(
	ctx context.Context,
	c client.Client,
	recorder events.EventRecorder,
	claim *capsulev1beta2.ResourcePoolClaim,
	condition meta.Condition,
) (err error) {
	updated := claim.Status.Conditions.UpdateConditionByTypeWithStatus(condition)

	if !updated {
		return nil
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.ResourcePoolClaim{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(claim), current); err != nil {
			return fmt.Errorf("failed to refetch instance before update: %w", err)
		}

		current.Status.Conditions = claim.Status.Conditions

		return c.Status().Update(ctx, current)
	})
	if err != nil {
		return err
	}

	eventType := corev1.EventTypeNormal
	if condition.Status == metav1.ConditionFalse {
		eventType = corev1.EventTypeWarning
	}

	recorder.Eventf(
		claim,
		claim,
		eventType,
		condition.Reason,
		evt.ActionReconciled,
		condition.Message,
	)

	return err
}

func filterResourceListByKeys(in corev1.ResourceList, keys corev1.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{}

	for k := range keys {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}

	return out
}

func selectClaimsCoveringUsageGreedy(
	used corev1.ResourceList,
	claims []capsulev1beta2.ResourcePoolClaim,
) map[string]struct{} {
	selected := map[string]struct{}{}

	if resourceListAllZero(used) || len(claims) == 0 {
		return selected
	}

	// Stable deterministic order for ties (creation ts, name, namespace)
	sort.Slice(claims, func(i, j int) bool {
		a, b := claims[i], claims[j]
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.Before(&b.CreationTimestamp)
		}

		if a.Name != b.Name {
			return a.Name < b.Name
		}

		return a.Namespace < b.Namespace
	})

	remaining := used.DeepCopy()
	// Greedy: repeatedly pick best claim against remaining.
	for !resourceListAllZero(remaining) {
		bestIdx := -1
		bestScore := float64(0)

		for i := range claims {
			uid := string(claims[i].UID)
			if _, ok := selected[uid]; ok {
				continue
			}

			score := claimCoverageScore(remaining, claims[i].Spec.ResourceClaims)
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		if bestIdx == -1 || bestScore == 0 {
			// Can't cover remaining with available claims (or used contains resources not claimed).
			break
		}

		chosen := claims[bestIdx]
		selected[string(chosen.UID)] = struct{}{}

		// remaining -= chosen.request (clamped at zero)
		for rName, req := range chosen.Spec.ResourceClaims {
			if rem, ok := remaining[rName]; ok {
				rem.Sub(req)

				if rem.Sign() < 0 {
					rem = rem.DeepCopy()
					rem.Set(0)
				}

				remaining[rName] = rem
			}
		}
	}

	return selected
}

func claimCoverageScore(remaining corev1.ResourceList, reqs corev1.ResourceList) float64 {
	var score float64

	for rName, rem := range remaining {
		if rem.IsZero() {
			continue
		}

		req, ok := reqs[rName]
		if !ok || req.IsZero() {
			continue
		}

		// covered = min(rem, req)
		covered := req.DeepCopy()
		if covered.Cmp(rem) > 0 {
			covered = rem.DeepCopy()
		}

		// Approx score: float is OK as heuristic
		score += covered.AsApproximateFloat64()
	}

	return score
}

func resourceListAllZero(rl corev1.ResourceList) bool {
	for _, q := range rl {
		if !q.IsZero() && q.Sign() > 0 {
			return false
		}
	}

	return true
}

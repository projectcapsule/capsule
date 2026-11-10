// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// triggerSchedule leaves lifecycle/spec/manual requests unthrottled. The pending
// token is never removed from metadata, so a new notification arriving during a
// reconciliation cannot be accidentally acknowledged by that reconciliation.
func triggerSchedule(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status *capsulev1beta2.TenantResourceCommonStatus, now time.Time) (time.Duration, string) {
	request := obj.GetAnnotations()[replication.TriggerAnnotation]
	if len(spec.Triggers) == 0 || request == "" || (status.Trigger != nil && status.Trigger.ObservedRequest == request) {
		return 0, ""
	}

	if status.NextResyncAt != nil && !now.Before(status.NextResyncAt.Time) {
		return 0, request
	}

	if status.Trigger != nil && obj.GetDeletionTimestamp().IsZero() && status.ObservedGeneration == obj.GetGeneration() && obj.GetAnnotations()[meta.ReconcileAnnotation] == "" {
		if remaining := status.Trigger.LastTriggeredAt.Add(replication.Interval(spec)).Sub(now); remaining > 0 {
			// An immediate watch event consumes an existing delayed queue item.
			// Explicitly preserve the earlier periodic wakeup when coalescing it.
			if status.NextResyncAt != nil {
				remaining = min(remaining, status.NextResyncAt.Sub(now))
			}

			return remaining, request
		}
	}

	return 0, request
}

func recordNextResync(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status *capsulev1beta2.TenantResourceCommonStatus, delay time.Duration) {
	status.NextResyncAt = nil

	if len(spec.Triggers) > 0 && delay > 0 && !spec.IsCordoned() && obj.GetDeletionTimestamp().IsZero() {
		next := metav1.NewMicroTime(time.Now().Add(delay))
		status.NextResyncAt = &next
	}
}

func recordTrigger(status *capsulev1beta2.TenantResourceCommonStatus, request string, now time.Time) {
	if request != "" {
		status.Trigger = &capsulev1beta2.TenantResourceTriggerStatus{ObservedRequest: request, LastTriggeredAt: metav1.NewMicroTime(now)}
	}
}

func startTrigger(status *capsulev1beta2.TenantResourceCommonStatus, request string, now time.Time) {
	if request == "" {
		return
	}

	observed := ""
	if status.Trigger != nil {
		observed = status.Trigger.ObservedRequest
	}

	recordTrigger(status, request, now)
	status.Trigger.ObservedRequest = observed
}

// Failed attempts retain their pending token and retry under the same interval,
// rather than waiting for a possibly very long periodic resync.
func finishTrigger(status *capsulev1beta2.TenantResourceCommonStatus, request string, now time.Time, reconcileError error) bool {
	if request == "" {
		return false
	}

	observed := request
	if reconcileError != nil {
		observed = ""
		if status.Trigger != nil {
			observed = status.Trigger.ObservedRequest
		}
	}

	recordTrigger(status, request, now)
	status.Trigger.ObservedRequest = observed

	return reconcileError != nil
}

// Trigger-enabled replications can deliberately have very long resync periods.
// Retry transient apply/dependency failures promptly even before the first event,
// including the initial protected-resource ownership handshake.
func triggerRetryAfter(spec *capsulev1beta2.TenantResourceCommonSpec, scheduled time.Duration, reconcileError error) time.Duration {
	if reconcileError != nil && len(spec.Triggers) > 0 {
		interval := min(replication.Interval(spec), time.Minute)
		if scheduled == 0 || scheduled > interval {
			return interval
		}
	}

	return scheduled
}

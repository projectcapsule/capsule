// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// Only the dispatcher throttles trigger notifications. Controllers must remain
// responsive to dependency, namespace, tenant, configuration and periodic events,
// even while an earlier trigger token is pending.
func pendingTriggerRequest(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status *capsulev1beta2.TenantResourceCommonStatus) string {
	request := obj.GetAnnotations()[replication.TriggerAnnotation]
	if len(spec.Triggers) == 0 || request == "" || (status.Trigger != nil && status.Trigger.ObservedRequest == request) {
		return ""
	}

	return request
}

func recordNextResync(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status *capsulev1beta2.TenantResourceCommonStatus, delay time.Duration, now time.Time) time.Duration {
	if len(spec.Triggers) == 0 || delay <= 0 || spec.IsCordoned() || !obj.GetDeletionTimestamp().IsZero() {
		status.NextResyncAt = nil

		return delay
	}

	next := now.Add(delay)
	if previous := status.NextResyncAt; previous != nil && previous.After(now) && previous.Time.Before(next) {
		next = previous.Time
	}

	stamp := metav1.NewMicroTime(next)
	status.NextResyncAt = &stamp

	return next.Sub(now)
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

// Failed attempts retain their pending token for the controller's retry path.
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

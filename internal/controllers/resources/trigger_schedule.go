// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type triggerValidationError struct{ error }

func (e triggerValidationError) Unwrap() error { return e.error }

// Trigger scheduling and subsequent status writes must start from the same
// authoritative annotation/acknowledgement snapshot, including retained trigger
// state after subscriptions are removed. Other replications keep cached reads.
func readReplicationSource(ctx context.Context, cached, reader client.Reader, key client.ObjectKey, object client.Object) error {
	if err := cached.Get(ctx, key, object); err != nil {
		return err
	}

	triggered := object.GetAnnotations()[replication.TriggerAnnotation] != ""
	switch object := object.(type) {
	case *capsulev1beta2.TenantResource:
		triggered = triggered || len(object.Spec.Triggers) > 0 || object.Status.Trigger != nil
		if triggered {
			*object = capsulev1beta2.TenantResource{TypeMeta: object.TypeMeta}
		}
	case *capsulev1beta2.GlobalTenantResource:
		triggered = triggered || len(object.Spec.Triggers) > 0 || object.Status.Trigger != nil
		if triggered {
			*object = capsulev1beta2.GlobalTenantResource{TypeMeta: object.TypeMeta}
		}
	}

	if !triggered {
		return nil
	}

	// Clear mutable fields before decoding, while preserving the API identity
	// used by owner references. Omitted fields must not retain cached
	// subscriptions, annotations or acknowledgements after removal.
	gvk := object.GetObjectKind().GroupVersionKind()

	if err := reader.Get(ctx, key, object); err != nil {
		return err
	}

	// The typed REST decoder clears GVK, unlike the manager cache.
	object.GetObjectKind().SetGroupVersionKind(gvk)

	return nil
}

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
	if reconcileError == nil || len(spec.Triggers) == 0 {
		return scheduled
	}

	if _, ok := errors.AsType[triggerValidationError](reconcileError); ok {
		return scheduled
	}

	interval := min(replication.Interval(spec), time.Minute)
	if scheduled == 0 || scheduled > interval {
		return interval
	}

	return scheduled
}

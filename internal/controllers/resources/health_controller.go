// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	kstatus "github.com/fluxcd/cli-utils/pkg/kstatus/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// healthController observes persisted successful applications. Its own bounded
// queue keeps health retries independent of rendering, applying and trigger limits.
// It shares the existing replication manager's clients, caches and election lease.
type healthController struct {
	client     client.Client
	conditions *cache.CELCache
	globals    impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]
	locals     impersonatedClientLoader[*capsulev1beta2.TenantResource]
}

func healthSource(obj client.Object) (*capsulev1beta2.TenantResourceCommonSpec, *capsulev1beta2.TenantResourceCommonStatus) {
	switch obj := obj.(type) {
	case *capsulev1beta2.TenantResource:
		return &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	case *capsulev1beta2.GlobalTenantResource:
		return &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	default:
		return nil, nil
	}
}

func healthRequested(obj client.Object) bool {
	spec, status := healthSource(obj)

	return spec != nil && spec.HealthChecks.Enabled() && !spec.IsCordoned() && obj.GetDeletionTimestamp().IsZero() && status.Health != nil && !status.Health.Applying && status.Health.Revision != "" && status.Health.ObservedGeneration == obj.GetGeneration()
}

func (r *healthController) SetupWithManager(mgr ctrl.Manager, options utils.ControllerOptions) error {
	selection := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return healthRequested(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !healthRequested(e.ObjectNew) {
				return false
			}

			_, old := healthSource(e.ObjectOld)
			_, fresh := healthSource(e.ObjectNew)

			return old == nil || old.Health == nil || old.Health.Revision != fresh.Health.Revision || old.Health.RequestedAt != fresh.Health.RequestedAt || old.Health.Applying
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).Named("capsule/replications/health").
		For(&capsulev1beta2.TenantResource{}, builder.WithPredicates(selection)).
		Watches(&capsulev1beta2.GlobalTenantResource{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{Name: obj.GetName()}}
		}), builder.WithPredicates(selection)).
		WithOptions(options.Runtime.ToControllerOptions()).Complete(r)
}

func (r *healthController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	obj := newHealthSource(request.Namespace)

	if err := r.client.Get(ctx, request.NamespacedName, obj); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !healthRequested(obj) {
		return reconcile.Result{}, nil
	}

	spec, status := healthSource(obj)

	now := time.Now()
	if status.Health.NextCheckAt.After(now) {
		return reconcile.Result{RequeueAfter: status.Health.NextCheckAt.Sub(now)}, nil
	}

	before := status.Health.DeepCopy()
	beforeConditions := slices.Clone(status.Conditions)

	var (
		c        client.Client
		err      error
		identity *meta.NamespacedRFC1123ObjectReferenceWithNamespace
	)

	switch obj := obj.(type) {
	case *capsulev1beta2.TenantResource:
		c, identity, err = r.locals.Load(ctx, ctrl.LoggerFrom(ctx), obj)
	case *capsulev1beta2.GlobalTenantResource:
		c, identity, err = r.globals.Load(ctx, ctrl.LoggerFrom(ctx), obj)
	}

	if err == nil && !reflect.DeepEqual(identity, status.ServiceAccount) {
		err = fmt.Errorf("execution identity changed; waiting for application")
	}

	checks, compileErr := compileHealthChecks(spec, r.conditions)
	if err == nil {
		err = compileErr
	}

	if err != nil {
		setHealthCondition(obj, status, metav1.ConditionUnknown, "HealthCheckError", err.Error())

		status.Health.NextCheckAt = metav1.NewTime(now.Add(healthPollInterval))
		if !now.Before(status.Health.Deadline.Time) {
			setHealthCondition(obj, status, metav1.ConditionFalse, healthTimeout, err.Error())
		}
	} else {
		observationCtx, cancel := context.WithTimeout(ctx, healthObservationTimeout)
		observeHealthBatch(observationCtx, c, obj, status, checks, now)
		cancel()
	}

	if ctx.Err() != nil {
		return reconcile.Result{}, ctx.Err()
	}

	status.Conditions.UpdateConditionByType(healthReadyCondition(obj, spec, status, meta.NewReadyCondition(obj)))

	published, err := r.publish(ctx, obj, status, before, beforeConditions)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !published {
		return reconcile.Result{RequeueAfter: healthPollInterval}, nil
	}

	if status.Health.NextCheckAt.IsZero() {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{RequeueAfter: max(time.Millisecond, time.Until(status.Health.NextCheckAt.Time))}, nil
}

func observeHealthBatch(ctx context.Context, c client.Reader, obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus, checks healthEvaluator, now time.Time) {
	phase := status.Health
	if phase.Cursor < 0 || int(phase.Cursor) > len(status.ProcessedItems) {
		phase.Cursor = 0
	}

	if phase.Cursor == 0 {
		phase.Pending = false
		phase.Failed = false
		phase.Message = ""
	}

	processed := 0
	for int(phase.Cursor) < len(status.ProcessedItems) && processed < healthBatchSize {
		if ctx.Err() != nil {
			break
		}

		item := status.ProcessedItems[phase.Cursor]
		phase.Cursor++

		if item.Health == nil {
			continue
		}

		processed++
		observed, message := observeHealthTarget(ctx, c, item, checks)
		key := client.ObjectKey{Name: item.Name, Namespace: item.Namespace}

		if observed != kstatus.CurrentStatus {
			phase.Pending = true

			firstFailure := observed == kstatus.FailedStatus && !phase.Failed

			if observed == kstatus.FailedStatus {
				phase.Failed = true
			}

			if phase.Message == "" || firstFailure {
				phase.Message = fmt.Sprintf("%s %s: %s", item.Kind, key, message)
				if len(phase.Message) > 1000 {
					phase.Message = phase.Message[:1000]
				}
			}
		}
	}

	phase.NextCheckAt = metav1.NewTime(now.Add(healthPollInterval))
	if int(phase.Cursor) < len(status.ProcessedItems) {
		phase.NextCheckAt = metav1.NewTime(now.Add(100 * time.Millisecond))

		if phase.Failed {
			setHealthCondition(obj, status, metav1.ConditionFalse, healthFailed, phase.Message)
		} else if !now.Before(phase.Deadline.Time) {
			setHealthCondition(obj, status, metav1.ConditionFalse, healthTimeout, "deadline expired while checking remaining applied resources")
		} else if previous := status.Conditions.GetConditionByType(meta.HealthyCondition); previous == nil || previous.Status != metav1.ConditionFalse {
			setHealthCondition(obj, status, metav1.ConditionUnknown, healthProgressing, "checking applied resources")
		}

		return
	}

	phase.Cursor = 0

	switch {
	case !phase.Pending:
		setHealthCondition(obj, status, metav1.ConditionTrue, meta.SucceededReason, "all applied resources are healthy")

		phase.NextCheckAt = metav1.Time{}
	case phase.Failed:
		setHealthCondition(obj, status, metav1.ConditionFalse, healthFailed, phase.Message)
	case !now.Before(phase.Deadline.Time):
		setHealthCondition(obj, status, metav1.ConditionFalse, healthTimeout, phase.Message)
	default:
		setHealthCondition(obj, status, metav1.ConditionUnknown, healthProgressing, phase.Message)
	}
}

// Publication uses the exact cached source resourceVersion as an optimistic
// precondition. A changed UID, generation, apply checkpoint or annotation rejects
// the patch at the API server; no stale observation can complete a newer rollout.
func (r *healthController) publish(ctx context.Context, obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus, before *capsulev1beta2.TenantResourceHealthStatus, beforeConditions meta.ConditionList) (bool, error) {
	if reflect.DeepEqual(status.Health, before) && reflect.DeepEqual(status.Conditions, beforeConditions) {
		return true, nil
	}

	payload, err := healthObservationPatch(obj, status)
	if err != nil {
		return false, err
	}

	if err := r.client.Status().Patch(ctx, obj, client.RawPatch(types.MergePatchType, payload)); err != nil {
		if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// Keep the patch independent of inventory size. Explicit zero values clear the
// previous scan cursor and diagnostic flags under JSON merge-patch semantics.
func healthObservationPatch(obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus) ([]byte, error) {
	phase, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status.Health)
	if err != nil {
		return nil, err
	}

	phase["cursor"] = int64(status.Health.Cursor)
	phase["pending"] = status.Health.Pending
	phase["failed"] = status.Health.Failed
	phase["applying"] = status.Health.Applying

	phase["message"] = status.Health.Message

	if status.Health.NextCheckAt.IsZero() {
		phase["nextCheckAt"] = nil
	}

	return json.Marshal(map[string]any{"metadata": map[string]any{"resourceVersion": obj.GetResourceVersion()}, "status": map[string]any{"health": phase, "conditions": status.Conditions}})
}

func observeHealthTarget(ctx context.Context, c client.Reader, item meta.ObjectReferenceStatus, checks healthEvaluator) (kstatus.Status, string) {
	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(item.GetGVK())

	key := client.ObjectKey{Name: item.Name, Namespace: item.Namespace}
	if item.ClusterScoped {
		key.Namespace = ""
	}

	observed := kstatus.InProgressStatus

	var message string

	err := c.Get(ctx, key, target)

	switch {
	case apierrors.IsNotFound(err):
		message = "applied target is missing"
	case err != nil:
		message = err.Error()
	case item.Health.UID == "" || target.GetUID() != item.Health.UID || target.GetGeneration() != item.Health.Generation:
		message = "target identity or generation differs from the applied revision"
	default:
		observed, message, err = checks.evaluate(ctx, target)
		if err != nil {
			message = err.Error()
		}
	}

	return observed, message
}

func newHealthSource(namespace string) client.Object {
	if namespace != "" {
		return &capsulev1beta2.TenantResource{}
	}

	return &capsulev1beta2.GlobalTenantResource{}
}

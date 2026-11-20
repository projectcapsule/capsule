// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	kstatus "github.com/fluxcd/cli-utils/pkg/kstatus/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

const (
	healthPollInterval       = 5 * time.Second
	healthBatchSize          = 64
	healthObservationTimeout = 10 * time.Second
	healthProgressing        = "HealthChecking"
	healthFailed             = "HealthCheckFailed"
	healthTimeout            = "HealthCheckTimeout"
)

type healthExpression struct {
	progress, failed, current *celruntime.CompiledExpression
}
type healthEvaluator map[schema.GroupKind]healthExpression

func compileHealthChecks(spec *capsulev1beta2.TenantResourceCommonSpec, conditions *cache.CELCache) (healthEvaluator, error) {
	if !spec.HealthChecks.Enabled() {
		return nil, nil
	}

	result := healthEvaluator{}

	if spec.HealthChecks.Timeout != nil && (spec.HealthChecks.Timeout.Duration <= 0 || spec.HealthChecks.Timeout.Duration > time.Hour) {
		return nil, fmt.Errorf("healthChecks.timeout must be positive and at most one hour")
	}

	if len(spec.HealthChecks.Expressions) > 32 {
		return nil, fmt.Errorf("at most 32 healthChecks.expressions are supported")
	}

	for i, expr := range spec.HealthChecks.Expressions {
		gv, err := schema.ParseGroupVersion(expr.APIVersion)
		if err != nil || expr.APIVersion == "" || expr.Kind == "" || len(validation.IsDNS1035Label(gv.Version)) > 0 || (gv.Group != "" && len(validation.IsDNS1123Subdomain(gv.Group)) > 0) {
			return nil, fmt.Errorf("healthChecks.expressions[%d] requires a valid apiVersion and kind", i)
		}

		key := schema.GroupKind{Group: gv.Group, Kind: expr.Kind}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("healthChecks.expressions[%d] duplicates %s", i, key)
		}

		var compiled healthExpression
		for _, pair := range []struct {
			text   string
			target **celruntime.CompiledExpression
		}{{expr.Current, &compiled.current}, {expr.InProgress, &compiled.progress}, {expr.Failed, &compiled.failed}} {
			if pair.text == "" && pair.target != &compiled.current {
				continue
			}

			program, err := conditions.GetOrCompileHealthCondition(pair.text)
			if err != nil {
				return nil, fmt.Errorf("healthChecks.expressions[%d]: %w", i, err)
			}

			*pair.target = program
		}

		result[key] = compiled
	}

	return result, nil
}

func (e healthEvaluator) evaluate(ctx context.Context, obj *unstructured.Unstructured) (kstatus.Status, string, error) {
	if !obj.GetDeletionTimestamp().IsZero() {
		return kstatus.InProgressStatus, "target is terminating", nil
	}

	observed, found, err := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	if err != nil {
		return kstatus.UnknownStatus, "", err
	}

	if found && observed != obj.GetGeneration() {
		return kstatus.InProgressStatus, "target generation is not observed", nil
	}

	if stale, err := staleReadyGeneration(obj); err != nil {
		return kstatus.UnknownStatus, "", err
	} else if stale {
		return kstatus.InProgressStatus, "Ready condition belongs to an older generation", nil
	}

	expr, custom := e[obj.GroupVersionKind().GroupKind()]
	if !custom {
		result, err := kstatus.Compute(obj)
		if err != nil {
			return kstatus.UnknownStatus, "", err
		}

		return result.Status, result.Message, nil
	}

	vars := map[string]any{"object": obj.Object}

	for _, key := range []string{"apiVersion", "kind", "metadata", "spec", "status"} {
		if value, ok := obj.Object[key]; ok {
			vars[key] = value
		}
	}

	for _, check := range []struct {
		program *celruntime.CompiledExpression
		status  kstatus.Status
	}{{expr.progress, kstatus.InProgressStatus}, {expr.failed, kstatus.FailedStatus}, {expr.current, kstatus.CurrentStatus}} {
		if check.program == nil {
			continue
		}

		matches, err := check.program.EvaluateBooleanWithVariables(ctx, vars)
		if err != nil {
			return kstatus.UnknownStatus, "", err
		}

		if matches {
			return check.status, string(check.status), nil
		}
	}

	return kstatus.InProgressStatus, "current expression is false", nil
}

func healthRevision(obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus) string {
	entries := make([]string, 0, len(status.ProcessedItems))
	entryBytes := 0

	for _, item := range status.ProcessedItems {
		if item.Health != nil {
			// Preserve the persisted revision format without formatting every
			// field through fmt on each observer batch.
			var entry strings.Builder

			entry.Grow(len(item.Group) + len(item.Version) + len(item.Kind) + len(item.Namespace) + len(item.Name) + len(item.Health.UID) + len(item.Health.Digest) + 28)

			for _, field := range []string{item.Group, item.Version, item.Kind, item.Namespace, item.Name} {
				entry.WriteString(field)
				entry.WriteByte('\x1f')
			}

			entry.WriteByte('/')
			entry.WriteString(string(item.Health.UID))
			entry.WriteByte('/')
			entry.WriteString(strconv.FormatInt(item.Health.Generation, 10))
			entry.WriteByte('/')
			entry.WriteString(item.Health.Digest)
			entries = append(entries, entry.String())
			entryBytes += entry.Len()
		}
	}

	slices.Sort(entries)

	identity := ""
	if status.ServiceAccount != nil {
		identity = status.ServiceAccount.Namespace.String() + "/" + status.ServiceAccount.Name.String()
	}

	var data strings.Builder

	data.Grow(len(obj.GetUID()) + len(identity) + entryBytes + len(entries) + 23)
	data.WriteString(string(obj.GetUID()))
	data.WriteByte('|')
	data.WriteString(strconv.FormatInt(obj.GetGeneration(), 10))
	data.WriteByte('|')
	data.WriteString(identity)
	data.WriteByte('|')

	for i, entry := range entries {
		if i > 0 {
			data.WriteByte('|')
		}

		data.WriteString(entry)
	}

	digest := sha256.Sum256([]byte(data.String()))

	return hex.EncodeToString(digest[:])
}

// completeHealthApplication merges the observer-owned checkpoint after a full
// apply. Repeated unchanged applies retain the original incomplete deadline.
func completeHealthApplication(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status, latest *capsulev1beta2.TenantResourceCommonStatus, applyErr error, now time.Time) {
	if !spec.HealthChecks.Enabled() {
		status.Health = nil
		if status.Conditions.GetConditionByType(meta.HealthyCondition) != nil {
			status.Conditions.RemoveConditionByType(meta.HealthyCondition)
		}

		return
	}

	if spec.IsCordoned() || !obj.GetDeletionTimestamp().IsZero() {
		status.Health = nil
		setHealthCondition(obj, status, metav1.ConditionUnknown, "ApplicationIncomplete", "health checks are paused")

		return
	}

	if applyErr != nil {
		status.Health = latest.Health.DeepCopy()
		if status.Health != nil {
			status.Health.Applying = true
			if !sameHealthScanInventory(status.ProcessedItems, latest.ProcessedItems) {
				restartHealthScan(status.Health, now)
			}
		}

		setHealthCondition(obj, status, metav1.ConditionUnknown, "ApplicationIncomplete", "health checks await a successful application")

		return
	}

	revision := healthRevision(obj, status)

	previous := latest.Health

	if previous != nil && previous.Revision == revision {
		status.Health = previous.DeepCopy()
		if !sameHealthScanInventory(status.ProcessedItems, latest.ProcessedItems) {
			// Cursor indexes every inventory entry, including skipped targets. A
			// changed ordering must restart the scan within the same rollout budget.
			restartHealthScan(status.Health, now)
			setHealthCondition(obj, status, metav1.ConditionUnknown, healthProgressing, "checking applied resources")
		} else if condition := latest.Conditions.GetConditionByType(meta.HealthyCondition); condition != nil {
			status.Conditions.UpdateConditionByType(*condition)
		}
	} else {
		status.Health = &capsulev1beta2.TenantResourceHealthStatus{Revision: revision, ObservedGeneration: obj.GetGeneration()}
		setHealthCondition(obj, status, metav1.ConditionUnknown, healthProgressing, "checking applied resources")
	}

	if status.Health.NextCheckAt.IsZero() {
		timeout := 5 * time.Minute
		if spec.HealthChecks.Timeout != nil {
			timeout = spec.HealthChecks.Timeout.Duration
		}

		status.Health.StartedAt = metav1.NewTime(now)
		status.Health.Deadline = metav1.NewTime(now.Add(timeout))
		status.Health.NextCheckAt = metav1.NewTime(now)
	}

	status.Health.Applying = false
	status.Health.RequestedAt = metav1.NewMicroTime(now)
}

func restartHealthScan(phase *capsulev1beta2.TenantResourceHealthStatus, now time.Time) {
	phase.Cursor = 0
	phase.Pending = false
	phase.Failed = false
	phase.Message = ""
	phase.NextCheckAt = metav1.NewTime(now)
}

// An empty, applying checkpoint durably requests a full apply through the source
// controller's status predicate, including after a controller restart.
func invalidateHealthApplication(obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus, now time.Time) {
	if status.Health == nil {
		// These fields are required by the status schema even before the first
		// health-enabled apply completes. Its completion starts the real budget.
		status.Health = &capsulev1beta2.TenantResourceHealthStatus{ObservedGeneration: obj.GetGeneration(), StartedAt: metav1.NewTime(now), Deadline: metav1.NewTime(now), RequestedAt: metav1.NewMicroTime(now)}
	}

	status.Health.Revision = ""
	status.Health.Applying = true
	restartHealthScan(status.Health, now)
	setHealthCondition(obj, status, metav1.ConditionUnknown, "ApplicationIncomplete", "health checks await a full application of the current inventory")
	spec, _ := healthSource(obj)
	status.Conditions.UpdateConditionByType(healthReadyCondition(obj, spec, status, meta.NewReadyCondition(obj)))
}

func healthApplicationInvalidated() predicate.Predicate {
	invalidated := func(obj client.Object) bool {
		spec, status := healthSource(obj)

		return spec != nil && spec.HealthChecks.Enabled() && status.Health != nil && status.Health.Applying && status.Health.Revision == ""
	}

	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		UpdateFunc:  func(e event.UpdateEvent) bool { return invalidated(e.ObjectNew) && !invalidated(e.ObjectOld) },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func sameHealthScanInventory(a, b meta.ProcessedItems) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].ResourceID != b[i].ResourceID || (a[i].Health == nil) != (b[i].Health == nil) {
			return false
		}

		if a[i].Health != nil && *a[i].Health != *b[i].Health {
			return false
		}
	}

	return true
}

func setHealthCondition(obj client.Object, status *capsulev1beta2.TenantResourceCommonStatus, state metav1.ConditionStatus, reason, message string) {
	if len(message) > 1000 {
		message = strings.ToValidUTF8(message[:1000], "?")
	}

	status.Conditions.UpdateConditionByType(meta.Condition{Type: meta.HealthyCondition, Status: state, ObservedGeneration: obj.GetGeneration(), Reason: reason, Message: message, LastTransitionTime: metav1.Now()})
}

func healthReadyCondition(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, status *capsulev1beta2.TenantResourceCommonStatus, ready meta.Condition) meta.Condition {
	if !spec.HealthChecks.Enabled() {
		return ready
	}

	ready.ObservedGeneration = obj.GetGeneration()

	healthy := status.Conditions.GetConditionByType(meta.HealthyCondition)

	if healthy == nil || healthy.Status != metav1.ConditionTrue || healthy.ObservedGeneration != obj.GetGeneration() || status.Health == nil || status.Health.Applying {
		ready.Status = metav1.ConditionFalse
		ready.Reason = healthProgressing

		ready.Message = "waiting for healthy applied resources"

		if healthy != nil {
			ready.Reason = healthy.Reason
			ready.Message = healthy.Message
		}
	}

	return ready
}

func validateReplicationChecks(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, triggers *cache.PreparedReplicationTriggers, conditions *cache.CELCache) error {
	if len(spec.Triggers) > 0 {
		if err := triggers.Validate(obj.GetUID(), obj.GetGeneration(), spec, namespaced, conditions); err != nil {
			return replicationValidationError{err}
		}
	}

	if _, err := compileHealthChecks(spec, conditions); err != nil {
		return replicationValidationError{err}
	}

	return nil
}

func replicationDependencyReady(obj client.Object) bool {
	spec, status := healthSource(obj)
	ready := status.Conditions.GetConditionByType(meta.ReadyCondition)

	return ready != nil && ready.Status == metav1.ConditionTrue && (!spec.HealthChecks.Enabled() || status.ObservedGeneration == obj.GetGeneration())
}

// Some controllers publish the observed generation only on the Ready condition.
// Honor it when present rather than accepting a previous rollout's Ready=True.
func staleReadyGeneration(obj *unstructured.Unstructured) (bool, error) {
	rawConditions, _, err := unstructured.NestedFieldNoCopy(obj.Object, "status", "conditions")
	if err != nil {
		return false, err
	}

	conditions, _ := rawConditions.([]any) // Custom CEL may handle a non-standard conditions field.

	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok || condition["type"] != "Ready" || condition["status"] != "True" {
			continue
		}

		generation, found, err := unstructured.NestedInt64(condition, "observedGeneration")
		if err != nil {
			return false, err
		}

		if found && generation != obj.GetGeneration() {
			return true, nil
		}
	}

	return false, nil
}

// Replication sources enqueue themselves on startup. Replaying every namespace
// then would repeat tenant-wide lookups quadratically before processing any work.
func healthNamespaceEvents() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return e.Object != nil && !e.IsInInitialList },
		DeleteFunc: func(e event.DeleteEvent) bool { return e.Object != nil },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld != nil && e.ObjectNew != nil && (!reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) || !reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) || (e.ObjectOld.GetDeletionTimestamp() == nil) != (e.ObjectNew.GetDeletionTimestamp() == nil))
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

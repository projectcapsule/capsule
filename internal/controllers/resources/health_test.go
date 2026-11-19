// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	kstatus "github.com/fluxcd/cli-utils/pkg/kstatus/status"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	tenantresourceindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func healthSpec() capsulev1beta2.TenantResourceCommonSpec {
	return capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Timeout: &metav1.Duration{Duration: time.Minute}, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", InProgress: `has(object.data) && 'state' in object.data && object.data.state == 'pending'`, Failed: `has(object.data) && 'state' in object.data && object.data.state == 'failed'`, Current: `has(object.data) && 'state' in object.data && object.data.state == 'healthy'`}}}, Cordoned: new(false)}
}

func TestHealthExpressionSemantics(t *testing.T) {
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	spec := healthSpec()
	checks, err := compileHealthChecks(&spec, conditions)
	require.NoError(t, err)
	for _, tc := range []struct {
		state string
		want  kstatus.Status
	}{{"pending", kstatus.InProgressStatus}, {"failed", kstatus.FailedStatus}, {"healthy", kstatus.CurrentStatus}, {"other", kstatus.InProgressStatus}} {
		obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "data": map[string]any{"state": tc.state}}}
		before := obj.DeepCopy()
		got, _, err := checks.evaluate(t.Context(), obj)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
		require.Equal(t, before, obj)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}}
	got, _, err := checks.evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.InProgressStatus, got)
	spec.HealthChecks.Expressions[0].Current = "status.ready == true"
	spec.HealthChecks.Expressions[0].Failed = ""
	spec.HealthChecks.Expressions[0].InProgress = ""
	checks, err = compileHealthChecks(&spec, conditions)
	require.NoError(t, err)
	_, _, err = checks.evaluate(t.Context(), obj)
	require.Error(t, err)
	obj.Object["status"] = map[string]any{"ready": true, "observedGeneration": int64(1)}
	obj.SetGeneration(2)
	got, _, err = checks.evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.InProgressStatus, got)
	obj.Object["status"].(map[string]any)["observedGeneration"] = int64(2)
	got, _, err = checks.evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.CurrentStatus, got)
	// A custom evaluator wins over the built-in assumption that ConfigMaps are current.
	spec.HealthChecks.Expressions[0].Current = "false"
	spec.HealthChecks.Expressions[0].InProgress = "true"
	spec.HealthChecks.Expressions[0].Failed = "true"
	checks, err = compileHealthChecks(&spec, conditions)
	require.NoError(t, err)
	got, _, err = checks.evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.InProgressStatus, got)
}

func TestHealthValidationAndBuiltinStatus(t *testing.T) {
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, change := range []func(*capsulev1beta2.TenantResourceCommonSpec){
		func(s *capsulev1beta2.TenantResourceCommonSpec) { s.HealthChecks.Timeout.Duration = 0 },
		func(s *capsulev1beta2.TenantResourceCommonSpec) { s.HealthChecks.Timeout.Duration = 2 * time.Hour },
		func(s *capsulev1beta2.TenantResourceCommonSpec) { s.HealthChecks.Expressions[0].APIVersion = "a/b/c" },
		func(s *capsulev1beta2.TenantResourceCommonSpec) { s.HealthChecks.Expressions[0].Kind = "" },
		func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.HealthChecks.Expressions = append(s.HealthChecks.Expressions, s.HealthChecks.Expressions[0])
		},
		func(s *capsulev1beta2.TenantResourceCommonSpec) { s.HealthChecks.Expressions[0].Current = "'not bool'" },
		func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.HealthChecks.Expressions[0].Current = "request.userInfo.username == 'admin'"
		},
	} {
		spec := healthSpec()
		change(&spec)
		_, err := compileHealthChecks(&spec, conditions)
		require.Error(t, err)
	}
	legacy := healthSpec()
	legacy.HealthChecks.Wait = false
	legacy.HealthChecks.Expressions[0].Current = "invalid"
	_, err = compileHealthChecks(&legacy, nil)
	require.NoError(t, err)
	deployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"generation": int64(2)}, "spec": map[string]any{"replicas": int64(1)}, "status": map[string]any{"observedGeneration": int64(1)}}}
	got, _, err := (healthEvaluator{}).evaluate(t.Context(), deployment)
	require.NoError(t, err)
	require.Equal(t, kstatus.InProgressStatus, got)
	deployment.Object["status"] = map[string]any{"observedGeneration": int64(2), "replicas": int64(1), "updatedReplicas": int64(1), "availableReplicas": int64(1), "readyReplicas": int64(1), "conditions": []any{map[string]any{"type": "Available", "status": "True"}, map[string]any{"type": "Progressing", "status": "True", "reason": "NewReplicaSetAvailable"}}}
	got, _, err = (healthEvaluator{}).evaluate(t.Context(), deployment)
	require.NoError(t, err)
	require.Equal(t, kstatus.CurrentStatus, got)
}

func TestHealthDeadlineSurvivesUnchangedApplies(t *testing.T) {
	parent := &capsulev1beta2.TenantResource{Name: "parent", Namespace: "tenant-a", UID: "parent", Generation: 1}
	spec := healthSpec()
	now := time.Now()
	status := &parent.Status.TenantResourceCommonStatus
	status.ProcessedItems = meta.ProcessedItems{{Name: "target", Namespace: "tenant-a", Kind: "ConfigMap", Version: "v1", Health: &meta.ResourceHealthReference{UID: "target", Digest: "first"}}}
	completeHealthApplication(parent, &spec, status, &capsulev1beta2.TenantResourceCommonStatus{}, nil, now)
	deadline := status.Health.Deadline
	previous := status.DeepCopy()
	previous.Health.Cursor = 3
	previous.Health.Applying = true
	completeHealthApplication(parent, &spec, status, previous, nil, now.Add(30*time.Second))
	require.Equal(t, deadline, status.Health.Deadline)
	require.Equal(t, int32(3), status.Health.Cursor)
	require.False(t, status.Health.Applying)
	status.ProcessedItems[0].Health.Digest = "changed-without-generation"
	completeHealthApplication(parent, &spec, status, previous, nil, now.Add(30*time.Second))
	require.True(t, status.Health.Deadline.After(deadline.Time))
	spec.HealthChecks.Wait = false
	completeHealthApplication(parent, &spec, status, status.DeepCopy(), nil, now)
	require.Nil(t, status.Health)
	require.Nil(t, status.Conditions.GetConditionByType(meta.HealthyCondition))
}

func TestNestedHealthChecksDefaults(t *testing.T) {
	for _, checks := range []*capsulev1beta2.TenantResourceHealthChecks{nil, {}, {Wait: false}, {Wait: true}} {
		spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: checks, Cordoned: new(false)}
		compiled, err := compileHealthChecks(&spec, nil)
		require.NoError(t, err)
		parent := &capsulev1beta2.TenantResource{Name: "parent", Namespace: "tenant-a", Generation: 1}
		now := time.Now()
		status := &parent.Status.TenantResourceCommonStatus
		completeHealthApplication(parent, &spec, status, status.DeepCopy(), nil, now)
		if checks.Enabled() {
			require.NotNil(t, compiled)
			require.NotNil(t, status.Health)
			require.Equal(t, 5*time.Minute, status.Health.Deadline.Sub(status.Health.StartedAt.Time))
		} else {
			require.Nil(t, compiled)
			require.Nil(t, status.Health)
			require.Nil(t, status.Conditions.GetConditionByType(meta.HealthyCondition))
		}
	}
}

func healthFixture(t *testing.T, global bool) (*healthController, client.WithWatch, client.Object, *corev1.ConfigMap) {
	t.Helper()
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	var parent client.Object = &capsulev1beta2.TenantResource{Name: "parent", Namespace: "tenant-a", UID: "parent", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: healthSpec()}}
	if global {
		parent = &capsulev1beta2.GlobalTenantResource{Name: "parent", UID: "parent", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: healthSpec()}}
	}
	target := &corev1.ConfigMap{Name: "target", Namespace: "tenant-a", UID: "target", Data: map[string]string{"state": "pending"}}
	spec, status := healthSource(parent)
	status.ObservedGeneration = 1
	status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "capsule", Namespace: "capsule-system"}
	status.ProcessedItems = meta.ProcessedItems{{ResourceID: gvk.NewResourceID(&unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "target", "namespace": "tenant-a"}}}, "tenant-a", ""), Health: &meta.ResourceHealthReference{UID: target.UID, Digest: "desired"}}}
	completeHealthApplication(parent, spec, status, &capsulev1beta2.TenantResourceCommonStatus{}, nil, time.Now())
	cfg := &capsulev1beta2.CapsuleConfiguration{Name: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(parent, target, cfg, &capsulev1beta2.Tenant{Name: "tenant-a", UID: "a"}, &capsulev1beta2.Tenant{Name: "tenant-b", UID: "b"}, &corev1.ConfigMap{Name: "private", Namespace: "tenant-b", UID: "private"}).Build()
	config := configuration.NewCapsuleConfiguration(t.Context(), c, c, nil, "default")
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	r := &healthController{
		client: c, conditions: conditions,
		localMetrics: metrics.NewTenantResourceRecorder(), globalMetrics: metrics.NewGlobalTenantResourceRecorder(),
		locals:  impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: c, configuration: config, resolve: namespacedServiceAccount},
		globals: impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: c, configuration: config, resolve: globalServiceAccount},
	}
	return r, c, parent, target
}

func TestHealthObserverRecoversWithoutApplyingOrReadingOtherTenants(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			r, c, parent, target := healthFixture(t, global)
			assertMetrics := func(want float64) {
				t.Helper()
				collector := r.localMetrics.Collectors()[0]
				family := "capsule_resource_condition"
				if global {
					collector = r.globalMetrics.Collectors()[0]
					family = "capsule_global_resource_condition"
				}
				registry := prometheus.NewRegistry()
				registry.MustRegister(collector)
				families, err := registry.Gather()
				require.NoError(t, err)
				require.Len(t, families, 1)
				require.Equal(t, family, families[0].GetName())
				require.Len(t, families[0].Metric, 2)
				for _, sample := range families[0].Metric {
					labels := map[string]string{}
					for _, label := range sample.Label {
						labels[label.GetName()] = label.GetValue()
					}
					require.Contains(t, []string{meta.HealthyCondition, meta.ReadyCondition}, labels["condition"])
					expected := map[string]string{"condition": labels["condition"], "name": parent.GetName()}
					if !global {
						expected["target_namespace"] = parent.GetNamespace()
					}
					require.Equal(t, expected, labels)
					require.Equal(t, want, sample.GetGauge().GetValue())
				}
			}
			writes, reads := 0, 0
			guarded := interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*unstructured.Unstructured); ok {
						require.Equal(t, "tenant-a", key.Namespace)
						require.Equal(t, "target", key.Name)
						reads++
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					writes++
					return errors.New("health observation must not apply")
				},
				Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
					writes++
					return errors.New("health observation must not create")
				},
			})
			r.locals.client = guarded
			r.globals.client = guarded
			req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)}
			result, err := r.Reconcile(t.Context(), req)
			require.NoError(t, err)
			require.Positive(t, result.RequeueAfter)
			require.Equal(t, 1, reads)
			require.Zero(t, writes)
			require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
			_, status := healthSource(parent)
			require.Equal(t, metav1.ConditionUnknown, status.Conditions.GetConditionByType(meta.HealthyCondition).Status)
			require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition))
			assertMetrics(0)
			deadline := status.Health.Deadline
			status.Health.Deadline = metav1.NewTime(time.Now().Add(-time.Second))
			status.Health.NextCheckAt = metav1.Time{}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			result, err = r.Reconcile(t.Context(), req)
			require.NoError(t, err)
			require.Positive(t, result.RequeueAfter)
			require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
			_, status = healthSource(parent)
			require.Equal(t, healthTimeout, status.Conditions.GetConditionByType(meta.HealthyCondition).Reason)
			assertMetrics(0)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(target), target))
			target.Data["state"] = "healthy"
			require.NoError(t, c.Update(t.Context(), target))
			status.Health.NextCheckAt = metav1.Time{}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			// A replacement observer uses only the durable API checkpoint.
			replacement := *r
			result, err = replacement.Reconcile(t.Context(), req)
			require.NoError(t, err)
			require.Zero(t, result.RequeueAfter)
			require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
			_, status = healthSource(parent)
			require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
			require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition))
			assertMetrics(1)
			require.NotEqual(t, deadline, status.Health.Deadline)
			require.Equal(t, 3, reads)
			require.Zero(t, writes)
		})
	}
}

func TestHealthIdentityAndPublicationGuards(t *testing.T) {
	for _, scenario := range []string{"replacement", "generation", "forbidden", "applying", "stale-result", "deleted", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			r, c, parent, target := healthFixture(t, false)
			spec, status := healthSource(parent)
			target.Data["state"] = "healthy"
			if scenario == "replacement" {
				status.ProcessedItems[0].Health.UID = "old-target"
			}
			if scenario == "generation" {
				status.ProcessedItems[0].Health.Generation = 2
			}
			if scenario == "applying" {
				status.Health.Applying = true
			}
			if scenario == "disabled" {
				spec.HealthChecks.Wait = false
				require.NoError(t, c.Update(t.Context(), parent))
			}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			require.NoError(t, c.Update(t.Context(), target))
			if scenario == "deleted" {
				require.NoError(t, c.Delete(t.Context(), parent))
			}
			r.locals.client = interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if scenario == "forbidden" {
					return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, key.Name, errors.New("denied target read"))
				}
				if scenario == "stale-result" {
					fresh := &capsulev1beta2.TenantResource{}
					require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), fresh))
					fresh.Status.Health.RequestedAt = metav1.NewMicroTime(time.Now().Add(time.Second))
					require.NoError(t, c.Status().Update(ctx, fresh))
				}
				return c.Get(ctx, key, obj, opts...)
			}})
			_, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
			require.NoError(t, err)
			if scenario != "deleted" {
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				_, status = healthSource(parent)
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
			}
		})
	}
}

func TestHealthReadBudgetAndSkippedTargets(t *testing.T) {
	r, c, parent, target := healthFixture(t, false)
	_, status := healthSource(parent)
	for range healthBatchSize + 5 {
		status.ProcessedItems = append(status.ProcessedItems, status.ProcessedItems[0])
	}
	target.Data["state"] = "healthy"
	require.NoError(t, c.Update(t.Context(), target))
	reads := 0
	reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		reads++
		return c.Get(ctx, key, obj, opts...)
	}})
	checks, err := compileHealthChecks(&parent.(*capsulev1beta2.TenantResource).Spec.TenantResourceCommonSpec, r.conditions)
	require.NoError(t, err)
	observeHealthBatch(t.Context(), reader, parent, status, checks, time.Now())
	require.Equal(t, healthBatchSize, reads)
	require.Equal(t, int32(healthBatchSize), status.Health.Cursor)
	require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
	observeHealthBatch(t.Context(), reader, parent, status, checks, time.Now())
	require.Equal(t, len(status.ProcessedItems), reads)
	require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
	status.ProcessedItems[0].Health = nil
	status.ProcessedItems = status.ProcessedItems[:1]
	reads = 0
	observeHealthBatch(t.Context(), reader, parent, status, checks, time.Now())
	require.Zero(t, reads)
	require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
}

func BenchmarkControllerHealthObservation(b *testing.B) {
	for _, count := range []int{1, 64, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			target := &corev1.ConfigMap{Name: "target", Namespace: "tenant-a", UID: types.UID("target"), Data: map[string]string{"state": "healthy"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(target).Build()
			parent := &capsulev1beta2.TenantResource{Generation: 1}
			phase := &capsulev1beta2.TenantResourceHealthStatus{Deadline: metav1.NewTime(time.Now().Add(time.Hour))}
			parent.Status.Health = phase
			item := meta.ObjectReferenceStatus{Version: "v1", Kind: "ConfigMap", Name: "target", Namespace: "tenant-a", Health: &meta.ResourceHealthReference{UID: target.UID}}
			for range count {
				parent.Status.ProcessedItems = append(parent.Status.ProcessedItems, item)
			}
			conditions, _ := cache.NewCELCache()
			spec := healthSpec()
			checks, err := compileHealthChecks(&spec, conditions)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				phase.Cursor = 0
				observeHealthBatch(b.Context(), c, parent, &parent.Status.TenantResourceCommonStatus, checks, time.Now())
			}
			b.ReportMetric(float64(min(count, healthBatchSize)), "GET/op")
		})
	}
}

func TestHealthDeadlineSurvivesTransientApplicationFailure(t *testing.T) {
	_, _, parent, _ := healthFixture(t, false)
	spec, status := healthSource(parent)
	before := status.DeepCopy()
	completeHealthApplication(parent, spec, status, before, errors.New("transient apply conflict"), time.Now().Add(20*time.Second))
	require.Equal(t, before.Health.Deadline, status.Health.Deadline)
	require.True(t, status.Health.Applying)
	require.False(t, healthRequested(parent))
	completeHealthApplication(parent, spec, status, status.DeepCopy(), nil, time.Now().Add(30*time.Second))
	require.Equal(t, before.Health.Deadline, status.Health.Deadline)
	require.False(t, status.Health.Applying)
}

func TestHealthMetadataPatchPreservesObservationAndConcurrentMetadata(t *testing.T) {
	_, c, parent, _ := healthFixture(t, false)
	parent.SetFinalizers([]string{"other.projectcapsule.dev/finalizer"})
	parent.SetAnnotations(map[string]string{meta.ReconcileAnnotation: "new-request", "projectcapsule.dev/other": "kept"})
	require.NoError(t, c.Update(t.Context(), parent))
	_, status := healthSource(parent)
	status.Health.Cursor = 64
	setHealthCondition(parent, status, metav1.ConditionTrue, meta.SucceededReason, "observed")
	require.NoError(t, c.Status().Update(t.Context(), parent))
	desired := parent.DeepCopyObject().(client.Object)
	desired.SetFinalizers([]string{meta.ControllerFinalizer})
	desired.SetAnnotations(nil)
	require.NoError(t, patchHealthReplicationMetadata(t.Context(), c, c, desired, nil, "old-request"))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	_, status = healthSource(parent)
	require.Equal(t, int32(64), status.Health.Cursor)
	require.True(t, meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition))
	require.Contains(t, parent.GetFinalizers(), "other.projectcapsule.dev/finalizer")
	require.Contains(t, parent.GetFinalizers(), meta.ControllerFinalizer)
	require.Equal(t, "new-request", parent.GetAnnotations()[meta.ReconcileAnnotation])
	require.NoError(t, patchHealthReplicationMetadata(t.Context(), c, c, desired, nil, "new-request"))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.Empty(t, parent.GetAnnotations()[meta.ReconcileAnnotation])
	require.Equal(t, "kept", parent.GetAnnotations()["projectcapsule.dev/other"])
}

func TestHealthDependencyRequiresObservedGeneration(t *testing.T) {
	resource := &capsulev1beta2.TenantResource{Generation: 2, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}}}}
	resource.Status.ObservedGeneration = 1
	resource.Status.Conditions.UpdateConditionByType(meta.NewReadyCondition(resource))
	require.False(t, replicationDependencyReady(resource))
	resource.Status.ObservedGeneration = 2
	require.True(t, replicationDependencyReady(resource))
	resource.Spec.HealthChecks.Wait = false
	resource.Status.ObservedGeneration = 1
	require.True(t, replicationDependencyReady(resource), "legacy dependency behavior is preserved")
}

func TestHealthApplicationRetryPreservesDisabledAndValidationBehavior(t *testing.T) {
	transient := errors.New("initial ownership is not yet indexed")
	for _, tc := range []struct {
		name      string
		checks    *capsulev1beta2.TenantResourceHealthChecks
		err       error
		scheduled time.Duration
		want      time.Duration
	}{
		{"omitted", nil, transient, 24 * time.Hour, 24 * time.Hour},
		{"disabled", &capsulev1beta2.TenantResourceHealthChecks{}, transient, 24 * time.Hour, 24 * time.Hour},
		{"initial apply", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, transient, 24 * time.Hour, 10 * time.Second},
		{"shorter wakeup", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, transient, time.Second, time.Second},
		{"no scheduled wakeup", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, transient, 0, 10 * time.Second},
		{"successful apply", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, nil, 24 * time.Hour, 24 * time.Hour},
		{"invalid expression", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, fmt.Errorf("validation: %w", replicationValidationError{transient}), 24 * time.Hour, 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: tc.checks}
			require.Equal(t, tc.want, replicationRetryAfter(&spec, tc.scheduled, tc.err, false))
		})
	}
}

func TestReplicationPublishesResolvedIdentityBeforeApplying(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, previous := range []string{"", "old-runner", "runner"} {
			t.Run(fmt.Sprintf("global=%t/previous=%s", global, previous), func(t *testing.T) {
				scheme := runtime.NewScheme()
				require.NoError(t, capsulev1beta2.AddToScheme(scheme))
				var parent client.Object
				if global {
					parent = &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "source", Generation: 1}
				} else {
					parent = &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", UID: "source", Generation: 1}
				}
				_, status := healthSource(parent)
				status.ObservedGeneration = 1
				status.Conditions.UpdateConditionByType(meta.NewReadyCondition(parent))
				status.ProcessedItems = meta.ProcessedItems{{Kind: "ConfigMap", Name: "existing", Namespace: "tenant-a", Status: metav1.ConditionTrue, Type: meta.ReadyCondition}}
				if previous != "" {
					status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: meta.RFC1123Name(previous), Namespace: "tenant-a"}
				}
				writes := 0
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).WithStatusSubresource(parent).WithInterceptorFuncs(interceptor.Funcs{
					SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, options ...client.SubResourceUpdateOption) error {
						writes++
						return c.SubResource(subresource).Update(ctx, obj, options...)
					},
				}).Build()
				status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: "tenant-a"}
				update := func() error {
					if global {
						return (&globalResourceController{client: c, reader: c}).updateReconcilingStatus(t.Context(), parent.(*capsulev1beta2.GlobalTenantResource))
					}
					return (&namespacedResourceController{client: c, reader: c}).updateReconcilingStatus(t.Context(), parent.(*capsulev1beta2.TenantResource))
				}
				require.NoError(t, update())
				fresh := parent.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
				_, persisted := healthSource(fresh)
				require.Equal(t, status.ServiceAccount, persisted.ServiceAccount)
				require.Equal(t, status.ProcessedItems, persisted.ProcessedItems)
				want := 1
				if previous == "runner" {
					want = 0
				}
				require.Equal(t, want, writes)
				require.NoError(t, update())
				require.Equal(t, want, writes, "unchanged identity must not add status writes")
			})
		}
	}
}

func TestHealthDependencyRetriesWithoutChangingLegacyDependencies(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("global=%t/healthDependency=%t", global, enabled), func(t *testing.T) {
				scheme := runtime.NewScheme()
				require.NoError(t, capsulev1beta2.AddToScheme(scheme))
				common := capsulev1beta2.TenantResourceCommonSpec{Cordoned: new(false), ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, DependsOn: []meta.LocalRFC1123ObjectReference{{Name: "dependency"}}}
				var parent, dependency client.Object
				if global {
					parent = &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "source", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
					dependency = &capsulev1beta2.GlobalTenantResource{Name: "dependency"}
				} else {
					parent = &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", UID: "source", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
					dependency = &capsulev1beta2.TenantResource{Name: "dependency", Namespace: "tenant-a"}
				}
				spec, _ := healthSource(dependency)
				spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: enabled}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, dependency).WithStatusSubresource(parent, dependency).Build()
				var controller reconcile.Reconciler
				if global {
					controller = &globalResourceController{client: c, reader: c, metrics: metrics.NewGlobalTenantResourceRecorder()}
				} else {
					controller = &namespacedResourceController{client: c, reader: c, metrics: metrics.NewTenantResourceRecorder()}
				}
				result, err := controller.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
				require.NoError(t, err)
				if enabled {
					require.Equal(t, 10*time.Second, result.RequeueAfter)
				} else {
					require.Greater(t, result.RequeueAfter, 12*time.Hour)
				}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				_, status := healthSource(parent)
				require.False(t, meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition))
				require.Contains(t, status.Conditions.GetConditionByType(meta.ReadyCondition).Message, "dependency dependency not ready")
			})
		}
	}
}

func BenchmarkReplicationRetryAfter(b *testing.B) {
	for _, tc := range []struct {
		name       string
		enabled    bool
		dependency bool
	}{{"false", false, false}, {"true", true, false}, {"dependency", false, true}} {
		b.Run(tc.name, func(b *testing.B) {
			spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: tc.enabled}}
			err := errors.New("transient apply failure")
			b.ReportAllocs()
			for b.Loop() {
				_ = replicationRetryAfter(&spec, 24*time.Hour, err, tc.dependency)
			}
		})
	}
}

func TestHealthNamespaceMappingUsesEligibleIndexAndTenantIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	tenantA := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "a", Labels: map[string]string{"profile": "a"}}
	tenantB := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "b", Labels: map[string]string{"profile": "b"}}
	namespace := &corev1.Namespace{Name: "tenant-a-ns", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenantA.Name, UID: tenantA.UID}}}
	a := &capsulev1beta2.GlobalTenantResource{Name: "selected-a", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"profile": "a"}}, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}}}}
	b := a.DeepCopy()
	b.Name = "selected-b"
	b.Spec.TenantSelector.MatchLabels["profile"] = "b"
	legacy := a.DeepCopy()
	legacy.Name = "legacy"
	legacy.Spec.HealthChecks.Wait = false
	index := tenantresourceindexer.GlobalHealthNamespaces{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenantA, tenantB, a, b, legacy).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	lists := 0
	indexed := interceptor.NewClient(c, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		options := (&client.ListOptions{}).ApplyOptions(opts)
		require.NotNil(t, options.FieldSelector)
		require.Equal(t, index.Field()+"=true", options.FieldSelector.String())
		lists++
		return c.List(ctx, list, opts...)
	}})
	r := &globalResourceController{client: indexed, log: logr.Discard()}
	require.Equal(t, []reconcile.Request{{Name: a.Name}}, r.enqueueHealthNamespaceResources(t.Context(), namespace))
	require.Equal(t, 1, lists)
	namespace.OwnerReferences[0].UID = "stale-tenant"
	require.Empty(t, r.enqueueHealthNamespaceResources(t.Context(), namespace))
	require.Equal(t, 1, lists)
}

func TestHealthRejectsStaleReadyConditionGeneration(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.projectcapsule.dev/v1", "kind": "Example", "metadata": map[string]any{"generation": int64(2)}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": int64(1)}}}}}
	result, _, err := (healthEvaluator{}).evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.InProgressStatus, result)
	conditions := obj.Object["status"].(map[string]any)["conditions"].([]any)
	conditions[0].(map[string]any)["observedGeneration"] = int64(2)
	result, _, err = (healthEvaluator{}).evaluate(t.Context(), obj)
	require.NoError(t, err)
	require.Equal(t, kstatus.CurrentStatus, result)
}

func TestHealthyObservationDoesNotRewriteUnchangedStatus(t *testing.T) {
	r, c, parent, target := healthFixture(t, false)
	target.Data["state"] = "healthy"
	require.NoError(t, c.Update(t.Context(), target))
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)}
	_, err := r.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), request.NamespacedName, parent))
	before := parent.GetResourceVersion()
	_, err = r.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), request.NamespacedName, parent))
	require.Equal(t, before, parent.GetResourceVersion())
}

func TestHealthPatchHasConstantInventoryCostAndClearsCheckpointFlags(t *testing.T) {
	_, c, parent, target := healthFixture(t, false)
	_, status := healthSource(parent)
	status.Health.Cursor = 0
	status.Health.Pending = false
	status.Health.Failed = false
	status.Health.Message = ""
	status.Health.NextCheckAt = metav1.Time{}
	small, err := healthObservationPatch(parent, status)
	require.NoError(t, err)
	for range 1024 {
		status.ProcessedItems = append(status.ProcessedItems, status.ProcessedItems[0])
	}
	large, err := healthObservationPatch(parent, status)
	require.NoError(t, err)
	require.Equal(t, small, large)
	require.NotContains(t, string(large), "processedItems")
	require.Contains(t, string(large), `"cursor":0`)
	require.Contains(t, string(large), `"pending":false`)
	require.Contains(t, string(large), `"nextCheckAt":null`)
	// Resetting a completed scan must clear previously persisted nonzero values.
	status.ProcessedItems = status.ProcessedItems[:1]
	status.Health.Cursor = 1
	status.Health.Pending = true
	status.Health.Failed = true
	status.Health.Message = "old failure"
	status.Health.NextCheckAt = metav1.Now()
	require.NoError(t, c.Status().Update(t.Context(), parent))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	_, status = healthSource(parent)
	status.Health.Cursor = 0
	status.Health.Pending = false
	status.Health.Failed = false
	status.Health.Message = ""
	status.Health.NextCheckAt = metav1.Time{}
	payload, err := healthObservationPatch(parent, status)
	require.NoError(t, err)
	require.NoError(t, c.Status().Patch(t.Context(), parent, client.RawPatch(types.MergePatchType, payload)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	_, status = healthSource(parent)
	require.Zero(t, status.Health.Cursor)
	require.False(t, status.Health.Pending)
	require.False(t, status.Health.Failed)
	require.Empty(t, status.Health.Message)
	require.True(t, status.Health.NextCheckAt.IsZero())
	require.NotNil(t, target)
}

func TestNamespacedHealthProfileMappingPreservesTenantIsolation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	a := &capsulev1beta2.Tenant{Name: "a", UID: "a", Status: capsulev1beta2.TenantStatus{Namespaces: []string{"a-source", "a-empty"}}}
	b := &capsulev1beta2.Tenant{Name: "b", UID: "b", Status: capsulev1beta2.TenantStatus{Namespaces: []string{"b-source"}}}
	selected := &capsulev1beta2.TenantResource{Name: "selected", Namespace: "a-source", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: healthSpec()}}
	legacy := selected.DeepCopy()
	legacy.Name = "legacy"
	legacy.Spec.HealthChecks.Wait = false
	other := selected.DeepCopy()
	other.Namespace = "b-source"
	index := tenantresourceindexer.NamespacedHealthNamespaces{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b, selected, legacy, other).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	lists := 0
	counted := interceptor.NewClient(c, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		options := (&client.ListOptions{}).ApplyOptions(opts)
		require.IsType(t, &capsulev1beta2.TenantResourceList{}, list)
		require.Contains(t, []string{"a-source", "a-empty", "b-source"}, options.Namespace)
		require.Equal(t, index.Field()+"="+options.Namespace, options.FieldSelector.String())
		lists++
		return c.List(ctx, list, opts...)
	}})
	controller := &namespacedResourceController{client: counted, log: logr.Discard()}
	namespace := &corev1.Namespace{Name: "a-target", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: a.Name, UID: a.UID}}}
	require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(selected)}}, controller.enqueueHealthTenantResourcesForNamespace(t.Context(), namespace))
	require.Equal(t, 2, lists)
	namespace.OwnerReferences[0].UID = "stale"
	require.Empty(t, controller.enqueueHealthTenantResourcesForNamespace(t.Context(), namespace))
	require.Equal(t, 2, lists)
	namespace.OwnerReferences[0].Name = b.Name
	namespace.OwnerReferences[0].UID = b.UID
	require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(other)}}, controller.enqueueHealthTenantResourcesForNamespace(t.Context(), namespace))
	require.Equal(t, 3, lists)
	namespace.OwnerReferences = nil
	require.Empty(t, controller.enqueueHealthTenantResourcesForNamespace(t.Context(), namespace))
	require.Equal(t, 3, lists)
}

func TestHealthNamespaceProfileEvents(t *testing.T) {
	p := healthNamespaceEvents()
	before := &corev1.Namespace{Name: "profile", Labels: map[string]string{"profile": "a"}}
	after := before.DeepCopy()
	require.False(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
	after.Annotations = map[string]string{"unrelated": "changed"}
	require.False(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
	after.Labels["profile"] = "b"
	require.True(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
	after = before.DeepCopy()
	after.OwnerReferences = []metav1.OwnerReference{{UID: "new-tenant"}}
	require.True(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
	after = before.DeepCopy()
	after.DeletionTimestamp = new(metav1.Now())
	require.True(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
	require.False(t, p.Update(event.UpdateEvent{}))
	require.False(t, p.Create(event.CreateEvent{}))
	require.True(t, p.Create(event.CreateEvent{Object: before}))
	require.True(t, p.Delete(event.DeleteEvent{Object: before}))
}

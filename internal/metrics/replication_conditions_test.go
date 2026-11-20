// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type replicationConditionFixture struct {
	family string
	gauge  *prometheus.GaugeVec
	record func(client.Object)
	remove func(client.Object)
}

func newReplicationConditionFixture(global bool) replicationConditionFixture {
	if global {
		r := NewGlobalTenantResourceRecorder()
		return replicationConditionFixture{
			family: "capsule_global_resource_condition", gauge: r.resourceConditionGauge,
			record: func(obj client.Object) { r.RecordConditions(obj.(*capsulev1beta2.GlobalTenantResource)) },
			remove: func(obj client.Object) { r.DeleteMetrics(obj.GetName()) },
		}
	}
	r := NewTenantResourceRecorder()
	return replicationConditionFixture{
		family: "capsule_resource_condition", gauge: r.resourceConditionGauge,
		record: func(obj client.Object) { r.RecordConditions(obj.(*capsulev1beta2.TenantResource)) },
		remove: func(obj client.Object) { r.DeleteMetrics(obj.GetName(), obj.GetNamespace()) },
	}
}

func replicationConditionObject(global bool, name, namespace string) (client.Object, *capsulev1beta2.TenantResourceCommonStatus) {
	if global {
		obj := &capsulev1beta2.GlobalTenantResource{Name: name}
		return obj, &obj.Status.TenantResourceCommonStatus
	}
	obj := &capsulev1beta2.TenantResource{Name: name, Namespace: namespace}
	return obj, &obj.Status.TenantResourceCommonStatus
}

func replicationConditionValue(t *testing.T, registry *prometheus.Registry, family string, obj client.Object, condition string) (float64, bool) {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, candidate := range families {
		if candidate.GetName() != family {
			continue
		}
		for _, metric := range candidate.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["name"] == obj.GetName() && labels["target_namespace"] == obj.GetNamespace() && labels["condition"] == condition {
				return metric.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func TestReplicationConditionsTrackHealthAndCleanup(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			t.Parallel()
			fixture := newReplicationConditionFixture(global)
			registry := prometheus.NewRegistry()
			registry.MustRegister(fixture.gauge)
			obj, status := replicationConditionObject(global, "replication", "tenant-a")
			otherName := "replication"
			if global {
				otherName = "other-replication"
			}
			other, otherStatus := replicationConditionObject(global, otherName, "tenant-b")
			otherStatus.Conditions = meta.ConditionList{{Type: meta.HealthyCondition, Status: metav1.ConditionTrue}}
			fixture.record(other)
			for _, state := range []metav1.ConditionStatus{metav1.ConditionUnknown, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionUnknown} {
				status.Conditions = meta.ConditionList{
					{Type: meta.ReadyCondition, Status: metav1.ConditionTrue},
					{Type: meta.CordonedCondition, Status: metav1.ConditionFalse},
					{Type: meta.HealthyCondition, Status: state},
				}
				fixture.record(obj)
				want := float64(0)
				if state == metav1.ConditionTrue {
					want = 1
				}
				value, found := replicationConditionValue(t, registry, fixture.family, obj, meta.HealthyCondition)
				require.True(t, found, "Healthy=%s must be exported", state)
				require.Equal(t, want, value)
				value, found = replicationConditionValue(t, registry, fixture.family, obj, meta.ReadyCondition)
				require.True(t, found)
				require.Equal(t, float64(1), value)
				value, found = replicationConditionValue(t, registry, fixture.family, obj, meta.CordonedCondition)
				require.True(t, found)
				require.Zero(t, value)
			}
			status.Conditions.RemoveConditionByType(meta.HealthyCondition)
			fixture.record(obj)
			_, found := replicationConditionValue(t, registry, fixture.family, obj, meta.HealthyCondition)
			require.False(t, found, "disabling health checks must remove the series")
			status.Conditions = nil
			fixture.record(obj)
			_, found = replicationConditionValue(t, registry, fixture.family, obj, meta.ReadyCondition)
			require.False(t, found)
			status.Conditions = meta.ConditionList{{Type: meta.HealthyCondition, Status: metav1.ConditionTrue}}
			fixture.record(obj)
			fixture.remove(obj)
			_, found = replicationConditionValue(t, registry, fixture.family, obj, meta.HealthyCondition)
			require.False(t, found, "deletion must remove the series")
			value, found := replicationConditionValue(t, registry, fixture.family, other, meta.HealthyCondition)
			require.True(t, found, "another replication's series must remain")
			require.Equal(t, float64(1), value)
		})
	}
}

func BenchmarkReplicationConditionMetrics(b *testing.B) {
	for _, global := range []bool{false, true} {
		for _, healthy := range []bool{false, true} {
			for _, unrelated := range []int{0, 1024} {
				b.Run(fmt.Sprintf("global=%t/health=%t/unrelated=%d", global, healthy, unrelated), func(b *testing.B) {
					fixture := newReplicationConditionFixture(global)
					obj, status := replicationConditionObject(global, "target", "tenant-a")
					status.Conditions = meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionTrue}, {Type: meta.CordonedCondition, Status: metav1.ConditionFalse}}
					if healthy {
						status.Conditions = append(status.Conditions, meta.Condition{Type: meta.HealthyCondition, Status: metav1.ConditionTrue})
					}
					for i := range unrelated {
						other, otherStatus := replicationConditionObject(global, fmt.Sprintf("other-%d", i), "tenant-b")
						otherStatus.Conditions = status.Conditions
						fixture.record(other)
					}
					fixture.record(obj)
					b.ReportAllocs()
					for b.Loop() {
						fixture.record(obj)
					}
				})
			}
		}
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestResourcePermitTemplateConditionMetrics(t *testing.T) {
	t.Parallel()
	local := NewResourcePermitTemplateRecorder()
	global := NewGlobalResourcePermitTemplateRecorder()
	registry := prometheus.NewRegistry()
	registry.MustRegister(local.Collectors()...)
	registry.MustRegister(global.Collectors()...)

	ready := meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionTrue}}
	failed := meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionFalse}}
	a := &capsulev1beta2.ResourcePermitTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "team-a"},
		Status:     capsulev1beta2.ResourcePermitTemplateStatus{Conditions: ready},
	}
	b := &capsulev1beta2.ResourcePermitTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "team-b"},
		Status:     capsulev1beta2.ResourcePermitTemplateStatus{Conditions: failed},
	}
	g := &capsulev1beta2.GlobalResourcePermitTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "example"},
		Status:     capsulev1beta2.GlobalResourcePermitTemplateStatus{Conditions: ready},
	}
	local.RecordConditions(a)
	local.RecordConditions(b)
	global.RecordConditions(g)
	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 2)
	for _, family := range families {
		switch family.GetName() {
		case "capsule_resourcepermittemplate_condition":
			require.Len(t, family.Metric, 2)
		case "capsule_globalresourcepermittemplate_condition":
			require.Len(t, family.Metric, 1)
		default:
			t.Fatalf("unexpected metric family %q", family.GetName())
		}
		for _, metric := range family.Metric {
			labels := make(map[string]string)
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			require.Equal(t, "example", labels["name"])
			require.Equal(t, "Ready", labels["condition"])
			if family.GetName() == "capsule_resourcepermittemplate_condition" {
				require.Len(t, labels, 3)
				require.Contains(t, []string{"team-a", "team-b"}, labels["target_namespace"])
			} else {
				require.Len(t, labels, 2)
			}
		}
	}
	assertGauge(t, local.resourceConditionGauge, 1, a.Name, a.Namespace, meta.ReadyCondition)
	assertGauge(t, local.resourceConditionGauge, 0, b.Name, b.Namespace, meta.ReadyCondition)
	assertGauge(t, global.resourceConditionGauge, 1, g.Name, meta.ReadyCondition)

	// Updates replace the previous value; deleting one namespace's template
	// must not remove another namespace's identically named template.
	a.Status.Conditions = failed
	g.Status.Conditions = failed
	local.RecordConditions(a)
	global.RecordConditions(g)
	assertGauge(t, local.resourceConditionGauge, 0, a.Name, a.Namespace, meta.ReadyCondition)
	assertGauge(t, global.resourceConditionGauge, 0, g.Name, meta.ReadyCondition)
	local.DeleteMetrics(a.Name, a.Namespace)
	require.Equal(t, 1, metricCount(local.resourceConditionGauge))
	assertGauge(t, local.resourceConditionGauge, 0, b.Name, b.Namespace, meta.ReadyCondition)
	global.DeleteMetrics(g.Name)
	require.Zero(t, metricCount(global.resourceConditionGauge))

	// A missing condition removes a previously published series.
	b.Status.Conditions = nil
	local.RecordConditions(b)
	global.RecordConditions(g)
	g.Status.Conditions = nil
	global.RecordConditions(g)
	families, err = registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
}

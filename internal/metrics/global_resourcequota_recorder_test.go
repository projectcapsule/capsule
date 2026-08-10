// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestGlobalResourceQuotaRecorderTracksAggregateAndNamespaceUsage(t *testing.T) {
	t.Parallel()

	recorder := NewGlobalResourceQuotaRecorder()
	quota := &capsulev1beta2.GlobalResourceQuota{}
	quota.Name = "shared"
	quota.Status.Total = capsulev1beta2.GlobalResourceQuotaUsage{
		Hard:      corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("8")},
		Used:      corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("2")},
		Available: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("6")},
	}
	quota.Status.NamespaceUsage = capsulev1beta2.GlobalResourceQuotaNamespaceUsage{
		"tenant-a": {
			Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1.5")},
		},
	}

	recorder.Record(quota)

	assertGauge(t, recorder.ResourceLimitGauge, 8, "shared", string(corev1.ResourceRequestsCPU))
	assertGauge(t, recorder.ResourceUsageGauge, 2, "shared", string(corev1.ResourceRequestsCPU))
	assertGauge(t, recorder.ResourceAvailableGauge, 6, "shared", string(corev1.ResourceRequestsCPU))
	assertGauge(t, recorder.ResourceUsagePercentageGauge, 25, "shared", string(corev1.ResourceRequestsCPU))
	assertGauge(
		t,
		recorder.NamespaceUsageGauge,
		1.5,
		"shared",
		"tenant-a",
		string(corev1.ResourceRequestsCPU),
	)
	assertGauge(
		t,
		recorder.NamespaceUsagePercentageGauge,
		18.75,
		"shared",
		"tenant-a",
		string(corev1.ResourceRequestsCPU),
	)

	recorder.Delete(quota.Name)
	if got := metricCount(recorder.ResourceUsageGauge); got != 0 {
		t.Fatalf("usage metric count after delete = %d, want 0", got)
	}
}

type gaugeMetric interface {
	GetMetricWithLabelValues(lvs ...string) (prometheus.Gauge, error)
}

func assertGauge(t *testing.T, gauge gaugeMetric, want float64, labels ...string) {
	t.Helper()

	metric, err := gauge.GetMetricWithLabelValues(labels...)
	if err != nil {
		t.Fatal(err)
	}
	value := &dto.Metric{}
	if err := metric.Write(value); err != nil {
		t.Fatal(err)
	}
	if got := value.GetGauge().GetValue(); got != want {
		t.Fatalf("metric %v = %v, want %v", labels, got, want)
	}
}

func metricCount(collector prometheus.Collector) int {
	metrics := make(chan prometheus.Metric, 32)
	collector.Collect(metrics)
	close(metrics)

	count := 0
	for range metrics {
		count++
	}

	return count
}

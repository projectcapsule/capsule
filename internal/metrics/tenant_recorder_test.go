// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestTenantReconcilePhaseMetrics(t *testing.T) {
	recorder := NewTenantRecorder()
	registry := prometheus.NewRegistry()
	registry.MustRegister(recorder.Collectors()...)
	recorder.ObserveReconcilePhase("role_bindings", 2*time.Second, nil)
	recorder.ObserveReconcilePhase("namespace_cleanup", 3*time.Second, errors.New("failed"))
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	samples := 0
	for _, family := range families {
		if family.GetName() != "capsule_tenant_reconcile_phase_duration_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if len(labels) != 2 {
				t.Fatalf("unexpected label cardinality: %v", labels)
			}
			if metric.Histogram.GetSampleCount() != 1 {
				t.Fatal("missing phase observation")
			}
			if labels["phase"] == "role_bindings" && (labels["result"] != "success" || metric.Histogram.GetSampleSum() != 2) {
				t.Fatalf("wrong success sample: %v", metric)
			}
			if labels["phase"] == "namespace_cleanup" && (labels["result"] != "error" || metric.Histogram.GetSampleSum() != 3) {
				t.Fatalf("wrong failure sample: %v", metric)
			}
			samples++
		}
	}
	if samples != 2 {
		t.Fatalf("got %d observations", samples)
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type TriggerRecorder struct {
	Notifications        *prometheus.CounterVec
	OmittedRegistrations prometheus.Gauge
}

func NewTriggerRecorder() *TriggerRecorder {
	return &TriggerRecorder{Notifications: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metricsPrefix, Name: "replication_trigger_notifications_total", Help: "Replication trigger notifications by bounded outcome."}, []string{"outcome"}), OmittedRegistrations: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsPrefix, Name: "replication_trigger_registrations_omitted", Help: "Native trigger groups omitted because the registration budget is exhausted."})}
}

func MustMakeTriggerRecorder() *TriggerRecorder {
	r := NewTriggerRecorder()
	ctrlmetrics.Registry.MustRegister(r.Notifications, r.OmittedRegistrations)

	return r
}

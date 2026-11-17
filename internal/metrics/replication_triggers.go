// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type TriggerRecorder struct {
	Notifications  *prometheus.CounterVec
	ActiveWatches  prometheus.Gauge
	OmittedWatches prometheus.Gauge
	Events         *prometheus.CounterVec
	SnapshotBytes  prometheus.Gauge
	QueuedBytes    prometheus.Gauge
}

func NewTriggerRecorder() *TriggerRecorder {
	return &TriggerRecorder{
		Notifications:  prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metricsPrefix, Name: "replication_trigger_notifications_total", Help: "Replication trigger notifications by bounded outcome."}, []string{"outcome"}),
		ActiveWatches:  prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsPrefix, Name: "replication_trigger_watches", Help: "Shared trigger resource watch streams on this replica."}),
		OmittedWatches: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsPrefix, Name: "replication_trigger_watches_omitted", Help: "Watch resources or subscriptions omitted by validation, discovery or capacity limits."}),
		Events:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metricsPrefix, Name: "replication_trigger_events_total", Help: "Resource watch events by bounded processing outcome."}, []string{"outcome"}),
		SnapshotBytes:  prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsPrefix, Name: "replication_trigger_snapshot_bytes", Help: "Conservatively accounted bytes retained in bounded watch snapshots."}),
		QueuedBytes:    prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsPrefix, Name: "replication_trigger_queued_bytes", Help: "Conservatively accounted bytes retained by queued and processing watch events."}),
	}
}

func MustMakeTriggerRecorder() *TriggerRecorder {
	r := NewTriggerRecorder()
	ctrlmetrics.Registry.MustRegister(r.Notifications, r.ActiveWatches, r.OmittedWatches, r.Events, r.SnapshotBytes, r.QueuedBytes)

	return r
}

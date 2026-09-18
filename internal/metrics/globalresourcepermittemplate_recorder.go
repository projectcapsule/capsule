// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type GlobalResourcePermitTemplateRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeGlobalResourcePermitTemplateRecorder() *GlobalResourcePermitTemplateRecorder {
	recorder := NewGlobalResourcePermitTemplateRecorder()
	crtlmetrics.Registry.MustRegister(recorder.Collectors()...)

	return recorder
}

func NewGlobalResourcePermitTemplateRecorder() *GlobalResourcePermitTemplateRecorder {
	return &GlobalResourcePermitTemplateRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "globalresourcepermittemplate_condition",
			Help:      "The current condition status of a GlobalResourcePermitTemplate resource.",
		}, []string{"name", "condition"}),
	}
}

func (r *GlobalResourcePermitTemplateRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{r.resourceConditionGauge}
}

func (r *GlobalResourcePermitTemplateRecorder) RecordConditions(resource *capsulev1beta2.GlobalResourcePermitTemplate) {
	if r == nil || resource == nil {
		return
	}

	condition := resource.Status.Conditions.GetConditionByType(meta.ReadyCondition)
	if condition == nil {
		r.DeleteMetrics(resource.Name)

		return
	}

	var value float64
	if condition.Status == metav1.ConditionTrue {
		value = 1
	}

	r.resourceConditionGauge.WithLabelValues(resource.Name, meta.ReadyCondition).Set(value)
}

func (r *GlobalResourcePermitTemplateRecorder) DeleteMetrics(name string) {
	if r == nil {
		return
	}

	r.resourceConditionGauge.DeletePartialMatch(prometheus.Labels{"name": name})
}

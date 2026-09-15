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

type ResourcePermitTemplateRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeResourcePermitTemplateRecorder() *ResourcePermitTemplateRecorder {
	recorder := NewResourcePermitTemplateRecorder()
	crtlmetrics.Registry.MustRegister(recorder.Collectors()...)

	return recorder
}

func NewResourcePermitTemplateRecorder() *ResourcePermitTemplateRecorder {
	return &ResourcePermitTemplateRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "resourcepermittemplate_condition",
			Help:      "The current condition status of a ResourcePermitTemplate resource.",
		}, []string{"name", "target_namespace", "condition"}),
	}
}

func (r *ResourcePermitTemplateRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{r.resourceConditionGauge}
}

func (r *ResourcePermitTemplateRecorder) RecordConditions(resource *capsulev1beta2.ResourcePermitTemplate) {
	if r == nil || resource == nil {
		return
	}

	condition := resource.Status.Conditions.GetConditionByType(meta.ReadyCondition)
	if condition == nil {
		r.DeleteMetrics(resource.Name, resource.Namespace)

		return
	}

	var value float64
	if condition.Status == metav1.ConditionTrue {
		value = 1
	}

	r.resourceConditionGauge.WithLabelValues(resource.Name, resource.Namespace, meta.ReadyCondition).Set(value)
}

func (r *ResourcePermitTemplateRecorder) DeleteMetrics(name, namespace string) {
	if r == nil {
		return
	}

	r.resourceConditionGauge.DeletePartialMatch(prometheus.Labels{"name": name, "target_namespace": namespace})
}

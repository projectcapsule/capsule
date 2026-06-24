// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func (h *podRules) validateQoSClasses(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return evaluatePodRules[corev1.PodQOSClass](
		pod,
		enforceBodies,
		podRuleSet[corev1.PodQOSClass]{
			Name:        "QoS class",
			EventReason: events.ReasonForbiddenPodQoSClass,
			Values: func(pod *corev1.Pod) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: string(workloads.GetPodQoSClass(pod)),
						Path:  "status.qosClass",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []corev1.PodQOSClass {
				if enforce == nil {
					return nil
				}

				return enforce.Workloads.QoSClasses
			},
			Matches: func(match corev1.PodQOSClass, value ruleengine.Value) (ruleengine.Match, error) {
				return ruleengine.Match{
					Matched: string(match) == value.Value,
				}, nil
			},
			RuleDescription: func(rule corev1.PodQOSClass) string {
				return string(rule)
			},
			AllowedDescription: "Allowed QoS classes",
		},
	)
}

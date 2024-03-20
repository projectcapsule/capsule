// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package podoptions

import (
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func aggregateSchedulingOptions(pod *corev1.Pod, scheduling api.PodSchedulingOptions) {
	nodeselector := scheduling.NodeSelector
	if nodeselector != nil {
		for k, v := range nodeselector {
			pod.Spec.NodeSelector[k] = v
		}
	}

	tolerations := scheduling.Tolerations
	if tolerations != nil {
		// Merge tolerations
		pod.Spec.Tolerations = append(pod.Spec.Tolerations, tolerations...)
	}

	topologies := scheduling.TopologySpreadConstraints
	if topologies != nil {
		pod.Spec.TopologySpreadConstraints = append(pod.Spec.TopologySpreadConstraints, topologies...)
	}

	return
}

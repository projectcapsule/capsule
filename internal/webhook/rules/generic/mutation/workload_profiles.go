// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func mutatePodSecurityProfiles(pod *corev1.Pod, desired *rules.WorkloadMutation, replace bool) {
	if (desired.Security.SeccompProfile == nil && desired.Security.AppArmorProfile == nil) || (pod.Spec.OS != nil && pod.Spec.OS.Name == corev1.Windows) {
		return
	}

	if pod.Spec.SecurityContext == nil {
		pod.Spec.SecurityContext = &corev1.PodSecurityContext{}
	}

	current := pod.Spec.SecurityContext
	if desired.Security.SeccompProfile != nil && (replace || current.SeccompProfile == nil) {
		current.SeccompProfile = desired.Security.SeccompProfile.DeepCopy()
	}

	if desired.Security.AppArmorProfile != nil && (replace || current.AppArmorProfile == nil) {
		current.AppArmorProfile = desired.Security.AppArmorProfile.DeepCopy()
	}
}

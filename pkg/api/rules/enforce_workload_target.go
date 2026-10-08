// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// WorkloadValidationTarget selects a native workload and optional Pod-spec location.
// +kubebuilder:validation:Pattern=`^(pod(/(initcontainers|ephemeralcontainers|containers|volumes))?|(deployment|statefulset|daemonset|replicaset|replicationcontroller|job|cronjob)(/(initcontainers|containers|volumes))?)$`
type WorkloadValidationTarget string

const (
	DeprecatedValidateImages      WorkloadValidationTarget = "pod/images"
	ValidatePod                   WorkloadValidationTarget = "pod"
	ValidateInitContainers        WorkloadValidationTarget = "pod/initcontainers"
	ValidateEphemeralContainers   WorkloadValidationTarget = "pod/ephemeralcontainers"
	ValidateContainers            WorkloadValidationTarget = "pod/containers"
	ValidateVolumes               WorkloadValidationTarget = "pod/volumes"
	ValidateDeployment            WorkloadValidationTarget = "deployment"
	ValidateStatefulSet           WorkloadValidationTarget = "statefulset"
	ValidateDaemonSet             WorkloadValidationTarget = "daemonset"
	ValidateReplicaSet            WorkloadValidationTarget = "replicaset"
	ValidateReplicationController WorkloadValidationTarget = "replicationcontroller"
	ValidateJob                   WorkloadValidationTarget = "job"
	ValidateCronJob               WorkloadValidationTarget = "cronjob"
)

// GroupKind resolves a target without consulting discovery or user-controlled labels.
func (t WorkloadValidationTarget) GroupKind() (schema.GroupKind, bool) {
	kind, part, _ := strings.Cut(string(t), "/")
	if part != "" && part != "containers" && part != "initcontainers" && part != "volumes" &&
		(kind != "pod" || (part != "ephemeralcontainers" && part != "images")) {
		return schema.GroupKind{}, false
	}

	if strings.HasSuffix(string(t), "/") {
		return schema.GroupKind{}, false
	}

	var gk schema.GroupKind

	switch kind {
	case string(ValidatePod):
		gk.Kind = "Pod"
	case string(ValidateReplicationController):
		gk.Kind = "ReplicationController"
	case string(ValidateDeployment):
		gk = schema.GroupKind{Group: "apps", Kind: "Deployment"}
	case string(ValidateStatefulSet):
		gk = schema.GroupKind{Group: "apps", Kind: "StatefulSet"}
	case string(ValidateDaemonSet):
		gk = schema.GroupKind{Group: "apps", Kind: "DaemonSet"}
	case string(ValidateReplicaSet):
		gk = schema.GroupKind{Group: "apps", Kind: "ReplicaSet"}
	case string(ValidateJob):
		gk = schema.GroupKind{Group: "batch", Kind: "Job"}
	case string(ValidateCronJob):
		gk = schema.GroupKind{Group: "batch", Kind: "CronJob"}
	default:
		return schema.GroupKind{}, false
	}

	return gk, true
}

func WorkloadTargetForGVK(gvk schema.GroupVersionKind) (WorkloadValidationTarget, bool) {
	var target WorkloadValidationTarget

	switch gvk.Kind {
	case "Pod":
		target = ValidatePod
	case "ReplicationController":
		target = ValidateReplicationController
	case "Deployment":
		target = ValidateDeployment
	case "StatefulSet":
		target = ValidateStatefulSet
	case "DaemonSet":
		target = ValidateDaemonSet
	case "ReplicaSet":
		target = ValidateReplicaSet
	case "Job":
		target = ValidateJob
	case "CronJob":
		target = ValidateCronJob
	default:
		return "", false
	}

	gk, ok := target.GroupKind()

	return target, ok && gk == gvk.GroupKind()
}

// TargetsOnly distinguishes matching kinds from scoping property policies.
func (w NamespaceRuleEnforceWorkloadsBody) TargetsOnly() bool {
	return len(w.Targets) > 0 && !w.HasPolicies()
}

func (w NamespaceRuleEnforceWorkloadsBody) HasPolicies() bool {
	return w.DisruptionBudgets != nil || w.HasPodSpecPolicies()
}

// HasPodSpecPolicies excludes coverage policies which only inspect labels and
// related resources, so template admission can skip decoding an unused Pod spec.
func (w NamespaceRuleEnforceWorkloadsBody) HasPodSpecPolicies() bool {
	return len(w.Placement.NodeSelector) > 0 || len(w.Placement.Tolerations) > 0 || len(w.Placement.TopologySpreadConstraints) > 0 ||
		len(w.Placement.Affinity) > 0 || w.Resources != nil || len(w.QoSClasses) > 0 || len(w.Registries) > 0 || len(w.Placement.Schedulers) > 0 ||
		len(w.Schedulers) > 0 || len(w.Security.SeccompProfiles) > 0 || len(w.Security.AppArmorProfiles) > 0
}

// PodTargets scopes a rule to gvk, translating controller-template locations to
// the existing Pod validators. Returned slices are read-only. Empty targets
// retain the established defaults for Pods and never opt controllers in.
func (w NamespaceRuleEnforceWorkloadsBody) PodTargets(gvk schema.GroupVersionKind) ([]WorkloadValidationTarget, bool) {
	kind, supported := WorkloadTargetForGVK(gvk)
	if !supported {
		return nil, false
	}

	if len(w.Targets) == 0 {
		return nil, kind == ValidatePod
	}

	if kind == ValidatePod {
		allPod := true

		for _, target := range w.Targets {
			gk, ok := target.GroupKind()
			if !ok || gk != gvk.GroupKind() {
				allPod = false

				break
			}
		}

		if allPod {
			return w.Targets, true
		}
	}

	var targets []WorkloadValidationTarget

	for _, target := range w.Targets {
		gk, ok := target.GroupKind()
		if !ok || gk != gvk.GroupKind() {
			continue
		}

		_, part, _ := strings.Cut(string(target), "/")
		if part == "" && kind != ValidatePod {
			return nil, true
		}

		mapped := ValidatePod
		if part != "" {
			mapped = WorkloadValidationTarget("pod/" + part)
		}

		targets = append(targets, mapped)
	}

	return targets, len(targets) > 0
}

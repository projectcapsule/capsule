// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
)

// MutationAction selects merge or replacement of supplied workload properties.
type MutationAction string

const (
	MutationActionMerge   MutationAction = "merge"
	MutationActionReplace MutationAction = "replace"
)

// NamespaceRuleMutation applies typed mutations independently of enforce.action.
// Mutated values remain subject to all applicable enforcement rules.
// +kubebuilder:object:generate=true
type NamespaceRuleMutation struct {
	// Conditions gate this entire mutation entry and inspect the object after
	// preceding mutations. All conditions must be true; empty means apply.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Conditions []AdmissionCondition `json:"conditions,omitempty"`

	// Action chooses how explicitly supplied properties are applied.
	// Merge fills an empty schedulerName and absent security profiles, sets hostUsers, readOnlyRootFilesystem, registries.imagePullPolicy,
	// and map keys, adds missing imagePullSecrets by name, upserts lists, and conjoins required affinity.
	// Replace replaces each supplied property in full: scheduler, hostUsers, readOnlyRootFilesystem, registries.imagePullPolicy, nodeSelector,
	// registries.imagePullSecrets, tolerations, topologySpreadConstraints, affinity, or security profiles. Omitted properties are
	// retained. Supplying affinity replaces all its branches, including omitted ones.
	// +optional
	// +kubebuilder:default=merge
	// +kubebuilder:validation:Enum=merge;replace
	Action MutationAction `json:"action,omitempty"`

	// +optional
	Workloads WorkloadMutation `json:"workloads,omitempty"`
}

// WorkloadMutation contains typed native Pod values. Empty maps/lists are
// preserved so replace can clear a property; nil means the property is omitted.
// It applies on Pod creation. Security.ReadOnlyRootFilesystem and
// Registries.ImagePullPolicy also apply to newly added ephemeral containers on
// subresource updates. Existing containers are never reconciled.
// On merge, later entries override hostUsers, readOnlyRootFilesystem, registries.imagePullPolicy, matching map keys,
// tolerations and spread constraints.
// On merge, required affinity restrictions from applicable entries are ANDed.
// +kubebuilder:object:generate=true
type WorkloadMutation struct {
	// Targets selects compatible Pod locations. Omitted or empty selects all
	// compatible locations. The pod target includes Pod-level properties and all
	// container groups; pod/containers, pod/initcontainers and pod/ephemeralcontainers
	// narrow selection to one group. Controller templates and volumes are not supported.
	// +optional
	// +kubebuilder:validation:MaxItems=4
	// +kubebuilder:validation:items:Enum=pod;pod/containers;pod/initcontainers;pod/ephemeralcontainers
	// +listType=set
	Targets []WorkloadValidationTarget `json:"targets,omitempty"`

	// Registries configures Pod image pull secrets and selected containers' pull policies.
	// +optional
	Registries WorkloadRegistryMutation `json:"registries,omitzero"`

	// Placement configures the Pod scheduler and scheduling constraints.
	// +optional
	Placement WorkloadPlacementMutation `json:"placement,omitzero"`

	// Security configures Pod and container security settings.
	// +optional
	Security WorkloadSecurityMutation `json:"security,omitzero"`
}

// WorkloadPlacementMutation contains native Pod scheduling values.
// Empty maps/lists are preserved so replace can clear an individual property.
// +kubebuilder:object:generate=true
type WorkloadPlacementMutation struct {
	// Scheduler sets spec.schedulerName on Pod creation. Merge fills only an empty
	// schedulerName, preserving all non-empty names, including default-scheduler.
	// Kubernetes defaults omitted schedulerName before admission. Use replace with
	// a condition to override default-scheduler while preserving custom schedulers.
	// Replace always overwrites schedulerName when the entry's conditions match.
	// Omitted or null leaves the Pod value unchanged.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Scheduler string `json:"scheduler,omitempty"`

	// NodeSelector sets configured keys on merge, or replaces the map on replace.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitzero"`

	// Tolerations merges by key, operator (default Equal), value and effect.
	// Changing any identity field adds another toleration, retaining the old one.
	// A matching toleration's duration is replaced; omitting it makes it unlimited.
	// Replace replaces the entire list, including entries with other identities.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitzero"`

	// TopologySpreadConstraints merges by topologyKey and whenUnsatisfiable.
	// The entire matching constraint is replaced, including its selector and
	// optional fields; other constraints remain. Replace replaces the entire list.
	// +optional
	// +listType=map
	// +listMapKey=topologyKey
	// +listMapKey=whenUnsatisfiable
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitzero"`

	// Affinity on merge conjoins required restrictions and upserts preferred terms.
	// Preferred terms match by their complete term, excluding weight and normalized
	// ordering. A different term is added; a matching term gets the supplied weight.
	// Required node affinity is distributed over existing OR alternatives, with
	// at most 256 resulting alternatives. Empty node selector terms match no nodes.
	// Replace replaces all affinity, including any branches omitted from the rule.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// WorkloadRegistryMutation contains Pod and container image registry settings.
// +kubebuilder:object:generate=true
type WorkloadRegistryMutation struct {
	// ImagePullPolicy sets imagePullPolicy on every selected regular or init
	// container at Pod creation, and newly added ephemeral containers on subresource
	// updates. Both merge and replace overwrite explicit and Kubernetes-defaulted
	// values. Omitted or null preserves the existing policy. Applies to all Pod OSes.
	// +optional
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets sets spec.imagePullSecrets on Pod creation. Merge appends
	// missing names and removes existing duplicates, retaining first-occurrence order. Replace sets
	// the complete list; an explicit empty list clears it. Omitted or null preserves it.
	// Requires the pod target or omitted/empty targets. References always use the
	// Pod's namespace; Capsule does not create, copy, or check the referenced Secrets.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:XValidation:rule="has(self.name) && self.name != ''",message="secret name must not be empty"
	// +listType=map
	// +listMapKey=name
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitzero"`
}

// WorkloadSecurityMutation contains native Pod and container security values.
// +kubebuilder:object:generate=true
type WorkloadSecurityMutation struct {
	// ReadOnlyRootFilesystem sets securityContext.readOnlyRootFilesystem on every
	// selected regular or init container at Pod creation, and newly added ephemeral
	// containers on subresource updates. Both merge and replace overwrite the value.
	// False is an explicit setting; nil preserves it. Windows Pods are skipped.
	// +optional
	ReadOnlyRootFilesystem *bool `json:"readOnlyRootFilesystem,omitempty"`

	// SeccompProfile supplies the Pod-level securityContext.seccompProfile on
	// Linux Pod creation. Merge fills an absent profile; replace replaces the
	// complete profile. Explicit container profiles are preserved. Nil omits it.
	// +optional
	SeccompProfile *corev1.SeccompProfile `json:"seccompProfile,omitempty"`

	// AppArmorProfile supplies the Pod-level securityContext.appArmorProfile on
	// Linux Pod creation. Merge fills an absent profile; replace replaces the
	// complete profile. Explicit container profiles are preserved. Nil omits it.
	// AppArmor and any Localhost profile must be available on eligible nodes.
	// +optional
	AppArmorProfile *corev1.AppArmorProfile `json:"appArmorProfile,omitempty"`

	// HostUsers sets spec.hostUsers on both merge and replace. False requests a
	// separate user namespace; true uses the host user namespace. Omitted or null
	// leaves the Pod value unchanged. Requires Kubernetes/runtime support.
	// +optional
	HostUsers *bool `json:"hostUsers,omitempty"`
}

// GetWorkloadTargets reports whether a mutation selects this Pod location.
func (w WorkloadMutation) GetWorkloadTargets(target WorkloadValidationTarget) bool {
	return len(w.Targets) == 0 || slices.Contains(w.Targets, ValidatePod) || slices.Contains(w.Targets, target)
}

// HasPodProperties reports whether any Pod-level property is configured.
func (w WorkloadMutation) HasPodProperties() bool {
	return w.Placement.Scheduler != "" || w.Security.HostUsers != nil || w.Placement.NodeSelector != nil || w.Placement.Tolerations != nil || w.Placement.TopologySpreadConstraints != nil || w.Placement.Affinity != nil || w.Security.SeccompProfile != nil || w.Security.AppArmorProfile != nil || w.Registries.ImagePullSecrets != nil
}

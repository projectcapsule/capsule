// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import k8sruntime "k8s.io/apimachinery/pkg/runtime"

// ResourceCreationPolicy controls whether a rendered target may adopt an
// existing Kubernetes resource.
type ResourceCreationPolicy string

const (
	// ResourceCreationPolicyOwner requires the resource to have been created by
	// the applying controller. This is also the runtime default.
	ResourceCreationPolicyOwner ResourceCreationPolicy = "Owner"

	// ResourceCreationPolicyMerge allows an existing resource to be adopted and
	// managed through server-side apply.
	ResourceCreationPolicyMerge ResourceCreationPolicy = "Merge"
)

// ResourceDeletionPolicy controls what happens to a rendered target when its
// parent stops managing it.
type ResourceDeletionPolicy string

const (
	// ResourceDeletionPolicyRemove removes resources created for the parent and
	// relinquishes adopted resources. This is also the runtime default.
	ResourceDeletionPolicyRemove ResourceDeletionPolicy = "Remove"

	// ResourceDeletionPolicyOrphan keeps the resource and removes Capsule's
	// lifecycle metadata when the parent stops managing it.
	ResourceDeletionPolicyOrphan ResourceDeletionPolicy = "Orphan"
)

// +kubebuilder:object:generate=true
type ResourceTemplatePolicy struct {
	// Creation controls how an existing target is handled. Owner requires the
	// resource to have been created by the applying controller and otherwise
	// returns an error. Merge adopts an existing resource when possible and
	// creates the resource when it does not exist.
	// +kubebuilder:default=Owner
	// +kubebuilder:validation:Enum=Owner;Merge
	Creation ResourceCreationPolicy `json:"creation,omitempty"`

	// Protect prevents users from changing or deleting the target through
	// admission while it is managed. Protection is enabled by default.
	// +kubebuilder:default=true
	Protect *bool `json:"protect,omitempty"`

	// Force allows server-side apply to acquire conflicting field ownership.
	// +kubebuilder:default=false
	Force bool `json:"force,omitempty"`

	// Deletion controls what happens when the parent stops managing the target.
	// Remove deletes resources created for the parent and relinquishes adopted
	// resources. Orphan keeps the resource and removes Capsule's lifecycle
	// metadata. Removal is the default.
	// +kubebuilder:default=Remove
	// +kubebuilder:validation:Enum=Remove;Orphan
	Deletion ResourceDeletionPolicy `json:"deletion,omitempty"`
}

// AllowsAdoption reports whether an existing resource may be adopted.
func (p ResourceTemplatePolicy) AllowsAdoption() bool {
	return p.Creation == ResourceCreationPolicyMerge
}

// IsProtected reports whether admission protection is enabled. A missing
// value is treated as true so callers which did not pass through API defaulting
// behave consistently with persisted custom resources.
func (p ResourceTemplatePolicy) IsProtected() bool {
	return p.Protect == nil || *p.Protect
}

// ShouldOrphan reports whether the target should be retained when its parent
// stops managing it. A missing value uses Remove semantics.
func (p ResourceTemplatePolicy) ShouldOrphan() bool {
	return p.Deletion == ResourceDeletionPolicyOrphan
}

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.targets) && size(self.targets) > 0) || has(self.template)",message="at least one target or a template is required"
type ResourceTemplate struct {
	// Policy controls how every target in this resource group is created and managed.
	// +kubebuilder:default={}
	Policy ResourceTemplatePolicy `json:"policy,omitempty"`

	// Targets are Kubernetes resource objects to render and manage with Policy.
	// +optional
	// +kubebuilder:validation:MinItems=1
	Targets []k8sruntime.RawExtension `json:"targets,omitempty"`

	// Template is an optional Go template which may render one or more YAML or
	// JSON Kubernetes resources separated by YAML document markers. Request
	// parameters are available under .params and loaded resources under
	// .context.resources.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template,omitempty"`
}

// RenderedResource is an execution-ready group of Kubernetes manifests
// produced from a ResourceTemplate. It intentionally cannot contain a source
// template so status consumers and reconcilers only observe concrete targets.
// +kubebuilder:object:generate=true
type RenderedResource struct {
	// Policy controls how every rendered target is created and managed.
	Policy ResourceTemplatePolicy `json:"policy,omitempty"`

	// Targets are the fully rendered Kubernetes manifests.
	// +kubebuilder:validation:MinItems=1
	Targets []k8sruntime.RawExtension `json:"targets"`
}

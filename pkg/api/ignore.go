package api

import "github.com/fluxcd/pkg/apis/kustomize"

// +kubebuilder:object:generate=true
type IgnoreRule struct {
	// Paths is a list of JSON Pointer (RFC 6901) paths to be excluded from
	// consideration in a Kubernetes object.
	// +required
	Paths []string `json:"paths"`

	// Target is a selector for specifying Kubernetes objects to which this
	// rule applies.
	// If Target is not set, the Paths will be ignored for all Kubernetes
	// objects within the manifest of the Helm release.
	// +optional
	Target *kustomize.Selector `json:"target,omitempty"`
}

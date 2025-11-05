package api

import (
	"github.com/fluxcd/pkg/apis/kustomize"
	"github.com/fluxcd/pkg/ssa/jsondiff"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

func (i *IgnoreRule) Matches(obj *unstructured.Unstructured) bool {
	if i == nil || i.Target == nil {
		return true
	}

	sr, err := jsondiff.NewSelectorRegex(&jsondiff.Selector{
		Group:              i.Target.Group,
		Version:            i.Target.Version,
		Kind:               i.Target.Kind,
		Namespace:          i.Target.Namespace,
		Name:               i.Target.Name,
		LabelSelector:      i.Target.LabelSelector,
		AnnotationSelector: i.Target.AnnotationSelector,
	})
	if err != nil {
		return false
	}
	return sr.MatchUnstructured(obj)
}

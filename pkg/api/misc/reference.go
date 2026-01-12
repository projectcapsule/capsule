// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package misc

import (
	"context"
	"fmt"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reference
// +kubebuilder:object:generate=true
type ResourceReference struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind" protobuf:"bytes,1,opt,name=kind"`
	// API version of the referent.
	APIVersion string `json:"apiVersion" protobuf:"bytes,5,opt,name=apiVersion"`
	// Name of the values referent. This is useful
	// when you traying to get a specific resource
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace of the values referent.
	// +optional
	Namespace meta.RFC1123SubdomainName `json:"namespace,omitempty"`
	// Selector which allows to get any amount of these resources based on labels
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// Only relevant if name is set. If an item is not optional, there will be an error thrown when it does not exist
	// +kubebuilder:default:=true
	Optional bool `json:"optional,omitempty"`
}

func (t ResourceReference) LoadResources(
	ctx context.Context,
	kubeClient client.Client,
	namespace string,
) ([]*unstructured.Unstructured, error) {
	if namespace != "" {
		t.Namespace = meta.RFC1123SubdomainName(namespace)
	}

	// For a single item we are not using list
	if t.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(t.APIVersion)
		obj.SetKind(t.Kind)

		key := client.ObjectKey{
			Name:      t.Name,
			Namespace: string(t.Namespace),
		}

		if err := kubeClient.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("failed to get %s/%s: %w", t.Kind, t.Name, err)
		}

		return []*unstructured.Unstructured{obj}, nil
	}

	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion(t.APIVersion)
	list.SetKind(t.Kind + "List")

	// Prepare list options.
	var opts []client.ListOption
	if t.Namespace != "" {
		opts = append(opts, client.InNamespace(t.Namespace))
	}

	if t.Selector != nil {
		selector, err := metav1.LabelSelectorAsSelector(t.Selector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}

	// List the resources.
	if err := kubeClient.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("failed to list: %w", err)
	}

	// Prepare a result map. For example, mapping resource name to its UID.
	results := []*unstructured.Unstructured{}
	for _, item := range list.Items {
		results = append(results, &item)
	}

	return results, nil
}

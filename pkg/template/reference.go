// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package template

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
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

func (t ResourceReference) RequiresTemplating() bool {
	if RequiresFastTemplate(t.Name) {
		return true
	}

	if RequiresFastTemplate(string(t.Namespace)) {
		return true
	}

	if SelectorRequiresTemplating(t.Selector) {
		return true
	}

	return false
}

func (t ResourceReference) LoadTemplated(templateContext map[string]string) (ResourceReference, error) {
	if !t.RequiresTemplating() || templateContext == nil {
		return t, nil
	}

	out := t

	// Name + Namespace
	if out.Name != "" {
		out.Name = FastTemplate(out.Name, templateContext)
	}
	if out.Namespace != "" {
		out.Namespace = meta.RFC1123SubdomainName(
			FastTemplate(string(out.Namespace), templateContext),
		)
	}

	// Selector
	if out.Selector != nil {
		selCopy, err := FastTemplateLabelSelector(out.Selector, templateContext)
		if err != nil {
			return ResourceReference{}, err
		}
		out.Selector = selCopy
	}

	return out, nil
}

func (t ResourceReference) LoadResources(
	ctx context.Context,
	kubeClient client.Client,
	restMapper k8smeta.RESTMapper,
	namespace string,
	additionSelectors []labels.Selector,
	templateContext map[string]string,
	allowClusterScoped bool,
) ([]*unstructured.Unstructured, error) {
	isNamespaced, err := t.IsNamespacedGVK(restMapper)
	if err != nil {
		return nil, err
	}

	if !allowClusterScoped && !isNamespaced {
		return nil, fmt.Errorf("cluster-scoped kind %s/%s is not allowed", t.APIVersion, t.Kind)
	}

	ref, err := t.LoadTemplated(templateContext)
	if err != nil {
		return nil, err
	}

	return ref.loadResources(ctx, kubeClient, restMapper, namespace, additionSelectors)
}

func (t ResourceReference) loadResources(
	ctx context.Context,
	kubeClient client.Client,
	restMapper k8smeta.RESTMapper,
	namespace string,
	additionSelectors []labels.Selector,
) ([]*unstructured.Unstructured, error) {
	ns := t.Namespace

	if namespace != "" {
		ns = meta.RFC1123SubdomainName(namespace)
	}

	// GET path (single object)
	if t.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(t.APIVersion)
		obj.SetKind(t.Kind)

		key := client.ObjectKey{
			Name:      t.Name,
			Namespace: string(ns),
		}

		if err := kubeClient.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) && t.Optional {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get %s/%s: %w", t.Kind, t.Name, err)
		}

		return []*unstructured.Unstructured{obj}, nil
	}

	// LIST path
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion(t.APIVersion)
	list.SetKind(t.Kind + "List")

	var opts []client.ListOption
	if ns != "" {
		opts = append(opts, client.InNamespace(string(ns)))
	}

	// Convert t.Selector (metav1) to labels.Selector if present
	var tenantSel labels.Selector
	if t.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(t.Selector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}
		tenantSel = s
	}

	all := make([]labels.Selector, 0, len(additionSelectors)+1)
	for _, s := range additionSelectors {
		if s != nil {
			all = append(all, s)
		}
	}
	if tenantSel != nil {
		all = append(all, tenantSel)
	}

	if len(all) > 0 {
		combined := selectors.CombineSelectors(all...)
		opts = append(opts, client.MatchingLabelsSelector{Selector: combined})
	}

	if err := kubeClient.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", t.Kind, err)
	}

	results := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		results = append(results, list.Items[i].DeepCopy())
	}

	return results, nil
}

func (t ResourceReference) IsNamespacedGVK(
	restMapper k8smeta.RESTMapper,
) (bool, error) {
	gv, err := schema.ParseGroupVersion(t.APIVersion)
	if err != nil {
		return false, fmt.Errorf("invalid apiVersion %q: %w", t.APIVersion, err)
	}
	gvk := gv.WithKind(t.Kind)

	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, fmt.Errorf("failed to resolve GVK %s: %w", gvk.String(), err)
	}

	isNamespaced := mapping.Scope.Name() == k8smeta.RESTScopeNameNamespace

	return isNamespaced, nil
}

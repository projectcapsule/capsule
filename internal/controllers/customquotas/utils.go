// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func GetUsageFromUnstructured(u unstructured.Unstructured, sourcePath string) (string, error) {
	j := jsonpath.New("usagePath")

	err := j.Parse(fmt.Sprintf("{%s}", sourcePath))
	if err != nil {
		return "", err
	}

	buf := new(bytes.Buffer)

	err = j.Execute(buf, u.Object)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func GetNamespacesMatchingSelectors(ctx context.Context, namespaceSelector []metav1.LabelSelector, kubeClient client.Client) ([]string, error) {
	set := map[string]struct{}{}

	for _, selector := range namespaceSelector {
		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}

		nsList := v1.NamespaceList{}

		err = kubeClient.List(ctx, &nsList, &client.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return nil, err
		}

		for _, ns := range nsList.Items {
			set[ns.Name] = struct{}{}
		}
	}

	return slices.Collect(maps.Keys(set)), nil
}

func getResources(ctx context.Context, source *capsulev1beta2.CustomQuotaSpecSource, kubeClient client.Client, scopeSelectors []metav1.LabelSelector, namespaces ...string) ([]unstructured.Unstructured, error) {
	items := []unstructured.Unstructured{}

	for _, selector := range scopeSelectors {
		u := &unstructured.UnstructuredList{}

		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}

		gr, err := schema.ParseGroupVersion(source.Version)
		if err != nil {
			return nil, err
		}

		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gr.Group,
			Kind:    source.Kind + "List",
			Version: gr.Version,
		})

		for _, namespace := range namespaces {
			err = kubeClient.List(ctx, u, &client.ListOptions{
				Namespace:     namespace,
				LabelSelector: labelSelector,
			})
			if err != nil {
				return nil, err
			}

			items = slices.Concat(items, u.Items)
		}
	}

	return items, nil
}

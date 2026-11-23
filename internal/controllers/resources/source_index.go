// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	toolscache "k8s.io/client-go/tools/cache"
	runtimecache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Reuse the manager informer's existing namespace index for a union lookup.
// The cache client's List supports one namespace at a time; querying the index
// directly avoids constructing and filtering an empty list for every namespace.
// No independent reverse map or authorization decision is cached here.
type namespaceSourceIndex struct{ index toolscache.Indexer }

func newNamespaceSourceIndex(ctx context.Context, c runtimecache.Cache) (*namespaceSourceIndex, error) {
	informer, err := c.GetInformer(ctx, &capsulev1beta2.TenantResource{}, runtimecache.BlockUntilSynced(false))
	if err != nil {
		return nil, fmt.Errorf("get TenantResource informer: %w", err)
	}

	indexed, ok := informer.(interface{ GetIndexer() toolscache.Indexer })
	if !ok {
		return nil, fmt.Errorf("TenantResource informer does not expose its namespace index")
	}

	return &namespaceSourceIndex{index: indexed.GetIndexer()}, nil
}

func (s *namespaceSourceIndex) list(ctx context.Context, reader client.Reader, namespaces sets.Set[string]) ([]capsulev1beta2.TenantResource, error) {
	var result []capsulev1beta2.TenantResource

	for namespace := range namespaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Standalone controllers in tests can use their injected reader. Manager
		// setup requires the shared index; production never falls back to a scan.
		if s == nil {
			var list capsulev1beta2.TenantResourceList
			if err := reader.List(ctx, &list, client.InNamespace(namespace)); err != nil {
				return nil, err
			}

			result = append(result, list.Items...)

			continue
		}

		objects, err := s.index.ByIndex(toolscache.NamespaceIndex, namespace)
		if err != nil {
			return nil, fmt.Errorf("lookup TenantResources in %s: %w", namespace, err)
		}

		for _, object := range objects {
			source, ok := object.(*capsulev1beta2.TenantResource)
			if !ok {
				return nil, fmt.Errorf("unexpected TenantResource index object %T", object)
			}

			result = append(result, *source.DeepCopy())
		}
	}

	return result, nil
}

func (r *NamespaceTrigger) tenantSelector(selector *metav1.LabelSelector) (labels.Selector, error) {
	if r.selectors == nil {
		return metav1.LabelSelectorAsSelector(selector)
	}

	return r.selectors.GetOrCompile(selector)
}

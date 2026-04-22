// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

const immediatePendingDeleteRequeue = 500 * time.Millisecond

type GroupedTarget struct {
	GVK     schema.GroupVersionKind
	Targets []capsulev1beta2.CustomQuotaStatusTarget
}

type CompiledTarget struct {
	capsulev1beta2.CustomQuotaStatusTarget

	CompiledPath      *jsonpath.CompiledJSONPath
	CompiledSelectors []selectors.CompiledSelectorWithFields
}

func CompileTargets(
	jcache *cache.JSONPathCache,
	targets []capsulev1beta2.CustomQuotaStatusTarget,
) ([]cache.CompiledTarget, error) {
	out := make([]cache.CompiledTarget, 0, len(targets))

	for _, target := range targets {
		pt := cache.CompiledTarget{
			CustomQuotaStatusTarget: target,
		}

		switch target.Operation {
		case quota.OpCount:
			// no usage path needed
		default:
			compiledPath, err := jcache.GetOrCompile(target.Path)
			if err != nil {
				return nil, fmt.Errorf(
					"compile usage path %q for %s %q: %w",
					target.Path,
					target.GroupVersionKind.String(),
					target.Operation,
					err,
				)
			}
			pt.CompiledPath = compiledPath
		}

		compiledSelectors, err := CompileSelectorsWithFields(jcache, target.Selectors)
		if err != nil {
			return nil, fmt.Errorf(
				"compile selectors for %s: %w",
				target.GroupVersionKind.String(),
				err,
			)
		}
		pt.CompiledSelectors = compiledSelectors

		out = append(out, pt)
	}

	return out, nil
}

func MatchesCompiledSelectorsWithFields(
	u unstructured.Unstructured,
	selectors []selectors.CompiledSelectorWithFields,
) (bool, error) {
	if len(selectors) == 0 {
		return true, nil
	}

	itemLabels := labels.Set(u.GetLabels())

	for _, sel := range selectors {
		if !sel.LabelSelector.Matches(itemLabels) {
			continue
		}

		allFieldsMatch := true
		for _, matcher := range sel.FieldMatchers {
			ok, err := jsonpath.EvaluateTruthyFromCompiled(u, matcher)
			if err != nil {
				return false, err
			}
			if !ok {
				allFieldsMatch = false
				break
			}
		}

		if allFieldsMatch {
			return true, nil
		}
	}

	return false, nil
}

func MakeCustomQuotaCacheKey(namespace, name string) string {
	return namespace + "/" + name
}

func MakeGlobalCustomQuotaCacheKey(name string) string {
	return "C/" + name
}

func CompileSelectorsWithFields(
	cache *cache.JSONPathCache,
	in []selectors.SelectorWithFields,
) ([]selectors.CompiledSelectorWithFields, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]selectors.CompiledSelectorWithFields, 0, len(in))

	for _, selector := range in {
		lblSel := labels.Everything()
		if selector.LabelSelector != nil {
			compiled, err := metav1.LabelSelectorAsSelector(selector.LabelSelector)
			if err != nil {
				return nil, fmt.Errorf("compile label selector with fields: %w", err)
			}
			lblSel = compiled
		}

		fieldMatchers := make([]*jsonpath.CompiledJSONPath, 0, len(selector.FieldSelectors))
		for _, path := range selector.FieldSelectors {
			compiledPath, err := cache.GetOrCompile(path)
			if err != nil {
				return nil, fmt.Errorf("compile field selector path %q: %w", path, err)
			}
			fieldMatchers = append(fieldMatchers, compiledPath)
		}

		out = append(out, selectors.CompiledSelectorWithFields{
			LabelSelector: lblSel,
			FieldMatchers: fieldMatchers,
		})
	}

	return out, nil
}

func getResourcesByGVK(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	kubeClient client.Reader,
	scopeSelectors []metav1.LabelSelector,
	namespaces ...string,
) ([]unstructured.Unstructured, error) {
	compiledSelectors := make([]labels.Selector, 0, len(scopeSelectors))
	for _, selector := range scopeSelectors {
		sel, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}
		compiledSelectors = append(compiledSelectors, sel)
	}

	filterByNamespace := true
	namespaceSet := make(map[string]struct{}, len(namespaces))

	for _, ns := range namespaces {
		if ns == "*" {
			filterByNamespace = false
			namespaceSet = nil
			break
		}

		namespaceSet[ns] = struct{}{}
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	if err := kubeClient.List(ctx, list); err != nil {
		return nil, err
	}

	items := make([]unstructured.Unstructured, 0, len(list.Items))
	seen := make(map[string]struct{}, len(list.Items))

	for i := range list.Items {
		item := list.Items[i]

		// Skip objects that are already definitely deleting:
		// deletionTimestamp is set and there are no finalizers left.
		if item.GetDeletionTimestamp() != nil && len(item.GetFinalizers()) == 0 {
			continue
		}

		// Namespace filter
		if filterByNamespace {
			if _, ok := namespaceSet[item.GetNamespace()]; !ok {
				continue
			}
		}

		// Label selector filter (OR semantics)
		if len(compiledSelectors) > 0 {
			itemLabels := labels.Set(item.GetLabels())

			matched := false
			for _, sel := range compiledSelectors {
				if sel.Matches(itemLabels) {
					matched = true
					break
				}
			}

			if !matched {
				continue
			}
		}

		key := item.GetNamespace() + "/" + item.GetName()
		if item.GetNamespace() == "" {
			key = item.GetName()
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		items = append(items, item)
	}

	// Sort by oldest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetCreationTimestamp().Time.Before(items[j].GetCreationTimestamp().Time)
	})

	return items, nil
}

func minDurationPtr(cur *time.Duration, cand time.Duration) *time.Duration {
	if cand < 0 {
		cand = 0
	}
	if cur == nil || cand < *cur {
		return &cand
	}
	return cur
}

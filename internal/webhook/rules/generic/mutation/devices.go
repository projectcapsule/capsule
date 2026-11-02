// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func isDeviceClaim(gvk schema.GroupVersionKind) bool {
	return gvk.Group == resourcev1.GroupName && (gvk.Version == "v1" || gvk.Version == "v1beta2") &&
		(gvk.Kind == "ResourceClaim" || gvk.Kind == "ResourceClaimTemplate")
}

// mutateDeviceClassDefaults fills class names only on existing request forms.
// Copy only changed request maps and their containing projection; publish after
// all entries succeed. Explicit classes, including scheduler allocations' input,
// never change. No DeviceClass reads or authorization-result caches are needed.
func mutateDeviceClassDefaults(ctx context.Context, obj *unstructured.Unstructured, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if obj == nil {
		return false, nil
	}

	path := []string{"spec", "devices", "requests"}
	if obj.GetKind() == "ResourceClaimTemplate" {
		path = append([]string{"spec"}, path...)
	}

	working := obj

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			mutation := &body.Mutate[j]
			if mutation.Devices.Class == "" {
				continue
			}

			conditions.ResetObject()

			matched, err := conditions.Matches(ctx, working, mutation.Conditions)
			if err != nil {
				return false, fmt.Errorf("rules[%d].mutate[%d]: %w", i, j, err)
			}

			if !matched {
				continue
			}

			if mutation.Action != "" && mutation.Action != rules.MutationActionMerge {
				return false, fmt.Errorf("rules[%d].mutate[%d].devices.class: only merge is supported", i, j)
			}

			raw, _, err := unstructured.NestedFieldNoCopy(working.Object, path...)
			if err != nil {
				return false, err
			}

			if raw == nil {
				continue
			}

			requests, ok := raw.([]any)
			if !ok {
				return false, fmt.Errorf("%s must be an array", strings.Join(path, "."))
			}

			updated, changed, err := defaultDeviceRequests(requests, mutation.Devices.Class)
			if err != nil {
				return false, fmt.Errorf("%s: %w", strings.Join(path, "."), err)
			}

			if changed {
				working, err = copyDeviceRequestProjection(working, path, updated)
				if err != nil {
					return false, err
				}
			}
		}
	}

	if working == obj {
		return false, nil
	}

	obj.Object = working.Object

	return true, nil
}

func defaultDeviceRequests(requests []any, class string) ([]any, bool, error) {
	working := requests
	changed := false

	for i, raw := range requests {
		request, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("request[%d] must be an object", i)
		}

		var updated map[string]any

		if exact := request["exactly"]; exact != nil {
			fields, ok := exact.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("request[%d].exactly must be an object", i)
			}

			value, defaulted, err := defaultClassName(fields, class)
			if err != nil {
				return nil, false, fmt.Errorf("request[%d].exactly: %w", i, err)
			}

			if defaulted {
				updated = maps.Clone(request)
				updated["exactly"] = value
			}
		}

		values, defaulted, err := defaultClassAlternatives(request["firstAvailable"], class)
		if err != nil {
			return nil, false, fmt.Errorf("request[%d].firstAvailable: %w", i, err)
		}

		if defaulted {
			if updated == nil {
				updated = maps.Clone(request)
			}

			updated["firstAvailable"] = values
		}

		if updated != nil {
			if !changed {
				working = slices.Clone(requests)
			}

			working[i], changed = updated, true
		}
	}

	return working, changed, nil
}

func defaultClassAlternatives(input any, class string) ([]any, bool, error) {
	if input == nil {
		return nil, false, nil
	}

	alternatives, ok := input.([]any)
	if !ok {
		return nil, false, fmt.Errorf("must be an array")
	}

	working := alternatives
	changed := false

	for i, raw := range alternatives {
		fields, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("alternative[%d] must be an object", i)
		}

		value, defaulted, err := defaultClassName(fields, class)
		if err != nil {
			return nil, false, fmt.Errorf("alternative[%d]: %w", i, err)
		}

		if defaulted {
			if !changed {
				working = slices.Clone(alternatives)
			}

			working[i], changed = value, true
		}
	}

	return working, changed, nil
}

func defaultClassName(fields map[string]any, class string) (map[string]any, bool, error) {
	if fields == nil {
		return nil, false, fmt.Errorf("device request must be an object")
	}

	if value := fields["deviceClassName"]; value != nil {
		name, ok := value.(string)
		if !ok {
			return nil, false, fmt.Errorf("deviceClassName must be a string")
		}

		if name != "" {
			return fields, false, nil
		}
	}

	updated := maps.Clone(fields)
	updated["deviceClassName"] = class

	return updated, true, nil
}

func copyDeviceRequestProjection(obj *unstructured.Unstructured, path []string, requests []any) (*unstructured.Unstructured, error) {
	working := &unstructured.Unstructured{Object: maps.Clone(obj.Object)}
	before, after := obj.Object, working.Object

	for _, key := range path[:len(path)-1] {
		original, ok := before[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}

		projection := maps.Clone(original)
		after[key] = projection
		before, after = original, projection
	}

	after[path[len(path)-1]] = requests

	return working, nil
}

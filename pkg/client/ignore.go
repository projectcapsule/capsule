// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"fmt"
	"strconv"
	"strings"

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

// jsonPointerGet returns (value, true) if JSON pointer p exists.
func JsonPointerGet(obj map[string]any, p string) (any, bool) {
	if p == "" || p == "/" {
		return obj, true
	}
	parts := strings.Split(p, "/")[1:]
	cur := any(obj)
	for _, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[key]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func JsonPointerSet(obj map[string]any, p string, val any) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot set root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			cur[key] = val
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			n := map[string]any{}
			cur[key] = n
			cur = n
			continue
		}
		switch m := nxt.(type) {
		case map[string]any:
			cur = m
		default:
			n := map[string]any{}
			cur[key] = n
			cur = n
		}
	}
	return nil
}

func JsonPointerDelete(obj map[string]any, p string) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot delete root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			delete(cur, key)
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			return nil
		}
		m, ok := nxt.(map[string]any)
		if !ok {
			return nil
		}
		cur = m
	}
	return nil
}

func PreserveIgnoredPaths(desired, live map[string]any, ptrs []string) {
	for _, p := range ptrs {
		if v, ok := JsonPointerGet(live, p); ok {
			_ = JsonPointerSet(desired, p, v)
		} else {
			_ = JsonPointerDelete(desired, p)
		}
	}
}

func MatchIgnorePaths(rules []IgnoreRule, obj *unstructured.Unstructured) []string {
	var out []string
	for _, r := range rules {
		if !r.Matches(obj) {
			continue
		}

		out = append(out, r.Paths...)
	}

	return out
}

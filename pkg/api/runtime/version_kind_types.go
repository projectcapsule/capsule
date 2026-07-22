// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"fmt"
	"strings"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	WildcardVersionKindMatcher = "*"
	CoreAPIVersion             = "v1"
)

// +kubebuilder:object:generate=true
type VersionKind struct {
	// Kind of the referent.
	//
	// Use "*" to match all kinds.
	//
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind" protobuf:"bytes,1,opt,name=kind"`

	// API version, API group, or API group/version selector of the referent.
	//
	// Empty APIVersion means the core Kubernetes API version "v1".
	// Use "*" to explicitly match all API groups and versions.
	//
	// Examples:
	// - "" means core "v1".
	// - "v1" means core "v1".
	// - "apps" means any version in the "apps" API group.
	// - "apps/v1" means the "apps/v1" API group/version.
	// - "apps/*" means any version in the "apps" API group.
	//
	// +optional
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,5,opt,name=apiVersion"`
}

func (s VersionKind) GroupVersionKind() schema.GroupVersionKind {
	apiVersion := normalizeAPIVersion(s.APIVersion)

	if apiVersion == CoreAPIVersion {
		return schema.GroupVersionKind{
			Group:   "",
			Version: CoreAPIVersion,
			Kind:    s.Kind,
		}
	}

	if apiVersion == WildcardVersionKindMatcher {
		return schema.GroupVersionKind{
			Group:   "",
			Version: WildcardVersionKindMatcher,
			Kind:    s.Kind,
		}
	}

	if strings.Contains(apiVersion, "/") {
		gv, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			return schema.GroupVersionKind{
				Kind: s.Kind,
			}
		}

		return gv.WithKind(s.Kind)
	}

	return schema.GroupVersionKind{
		Group: apiVersion,
		Kind:  s.Kind,
	}
}

// MatchesGroupVersionKind returns true when the receiver matches the provided GVK.
//
// Matching is exact unless the receiver contains '*'.
// Empty APIVersion is treated as "v1".
// Kind must be set. Use "*" to explicitly match all kinds.
func (s VersionKind) MatchesGroupVersionKind(gvk schema.GroupVersionKind) bool {
	return matchAPIGroupPattern(normalizeAPIVersion(s.APIVersion), gvk) &&
		matchPattern(s.Kind, gvk.Kind)
}

// MatchesVersionKind returns true when the receiver matches another VersionKind.
//
// The receiver is interpreted as the pattern.
// The provided VersionKind is interpreted as the concrete value.
func (s VersionKind) MatchesVersionKind(value VersionKind) bool {
	return s.MatchesGroupVersionKind(value.GroupVersionKind())
}

// HasWildcard returns true when APIVersion or Kind contains a wildcard matcher.
func (s VersionKind) HasWildcard() bool {
	return strings.Contains(s.APIVersion, WildcardVersionKindMatcher) ||
		strings.Contains(s.Kind, WildcardVersionKindMatcher)
}

// +kubebuilder:object:generate=true
type VersionKinds struct {
	// API groups or API group/version selectors of the referents.
	//
	// Empty or omitted APIGroups means the core Kubernetes API version "v1".
	// Use "*" to match all API groups and versions.
	//
	// Examples:
	// - [] or [""] means core "v1".
	// - ["v1"] means core "v1".
	// - ["apps"] means any version in the "apps" API group.
	// - ["apps/v1"] means only "apps/v1".
	// - ["apps", "batch/v1"] means any "apps" version and "batch/v1".
	// - ["*"] means all API groups and versions.
	//
	// +optional
	APIGroups []string `json:"apiGroups,omitempty"`

	// Kinds of the referents.
	//
	// Use "*" to match all kinds.
	//
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Kinds []string `json:"kinds"`
}

func (s VersionKinds) VersionKinds() []VersionKind {
	apiGroups := s.NormalizedAPIGroups()
	kinds := s.normalizedKinds()

	out := make([]VersionKind, 0, len(apiGroups)*len(kinds))

	for _, apiGroup := range apiGroups {
		for _, kind := range kinds {
			out = append(out, VersionKind{
				APIVersion: apiGroupPatternToAPIVersionPattern(apiGroup),
				Kind:       kind,
			})
		}
	}

	return out
}

func (s VersionKinds) MatchesGroupVersionKind(gvk schema.GroupVersionKind) bool {
	for _, kind := range s.normalizedKinds() {
		if !matchPattern(kind, gvk.Kind) {
			continue
		}

		for _, apiGroup := range s.NormalizedAPIGroups() {
			if matchAPIGroupPattern(apiGroup, gvk) {
				return true
			}
		}
	}

	return false
}

func (s VersionKinds) HasWildcard() bool {
	for _, apiGroup := range s.APIGroups {
		if strings.Contains(apiGroup, WildcardVersionKindMatcher) {
			return true
		}
	}

	for _, kind := range s.Kinds {
		if strings.Contains(kind, WildcardVersionKindMatcher) {
			return true
		}
	}

	return false
}

// ValidateKnownKinds validates concrete apiGroup/kind or apiGroupVersion/kind combinations against the RESTMapper.
// Wildcard API groups or wildcard kinds are intentionally skipped because they are selectors,
// not concrete Kubernetes resources.
func (s VersionKinds) ValidateKnownKinds(mapper apimeta.RESTMapper, fieldPath string) error {
	return s.ValidateKnownKindsWithScope(mapper, fieldPath, nil)
}

// ValidateKnownKindsWithScope validates concrete targets and optionally their
// REST scope. Wildcard selectors are skipped because discovery cannot enumerate
// their complete set reliably.
func (s VersionKinds) ValidateKnownKindsWithScope(
	mapper apimeta.RESTMapper,
	fieldPath string,
	allowScope func(schema.GroupVersionKind, apimeta.RESTScope) bool,
) error {
	if mapper == nil {
		return nil
	}

	kinds := s.normalizedKinds()
	apiGroups := s.NormalizedAPIGroups()

	for kindIndex, kind := range kinds {
		if strings.Contains(kind, WildcardVersionKindMatcher) {
			continue
		}

		for apiGroupIndex, apiGroup := range apiGroups {
			if strings.Contains(apiGroup, WildcardVersionKindMatcher) {
				continue
			}

			mapping, err := restMappingForAPIGroup(mapper, apiGroup, kind)
			if err != nil {
				return fmt.Errorf(
					"%s.kinds[%d] %q for apiGroups[%d] %q is invalid: %w",
					fieldPath,
					kindIndex,
					kind,
					apiGroupIndex,
					apiGroup,
					err,
				)
			}

			if allowScope != nil && !allowScope(mapping.GroupVersionKind, mapping.Scope) {
				return fmt.Errorf(
					"%s.kinds[%d] %q for apiGroups[%d] %q is invalid: GVK %s has unsupported scope %q",
					fieldPath, kindIndex, kind, apiGroupIndex, apiGroup,
					mapping.GroupVersionKind.String(), mapping.Scope.Name(),
				)
			}
		}
	}

	return nil
}

func restMappingForAPIGroup(
	mapper apimeta.RESTMapper,
	apiGroup string,
	kind string,
) (*apimeta.RESTMapping, error) {
	apiGroup = strings.TrimSpace(apiGroup)

	apiGroup = normalizeAPIVersion(apiGroup)

	if apiGroup == CoreAPIVersion {
		return mapper.RESTMapping(schema.GroupKind{Kind: kind}, CoreAPIVersion)
	}

	if gv, err := schema.ParseGroupVersion(apiGroup); err == nil && strings.Contains(apiGroup, "/") {
		return mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
	}

	return mapper.RESTMapping(schema.GroupKind{Group: apiGroup, Kind: kind})
}

func (s VersionKinds) StatusAPIGroups() []string {
	apiGroups := s.NormalizedAPIGroups()
	if len(apiGroups) == 0 {
		return []string{CoreAPIVersion}
	}

	out := make([]string, 0, len(apiGroups))
	seen := make(map[string]struct{}, len(apiGroups))

	for _, apiGroup := range apiGroups {
		apiGroup = strings.TrimSpace(apiGroup)
		if apiGroup == "" {
			apiGroup = CoreAPIVersion
		}

		if _, ok := seen[apiGroup]; ok {
			continue
		}

		seen[apiGroup] = struct{}{}

		out = append(out, apiGroup)
	}

	if len(out) == 0 {
		return []string{CoreAPIVersion}
	}

	return out
}

func (s VersionKinds) NormalizedAPIGroups() []string {
	if len(s.APIGroups) == 0 {
		return []string{CoreAPIVersion}
	}

	out := make([]string, 0, len(s.APIGroups))

	for _, apiGroup := range s.APIGroups {
		apiGroup = strings.TrimSpace(apiGroup)
		if apiGroup == "" {
			apiGroup = CoreAPIVersion
		}

		out = append(out, apiGroup)
	}

	if len(out) == 0 {
		return []string{CoreAPIVersion}
	}

	return out
}

func (s VersionKinds) normalizedKinds() []string {
	if len(s.Kinds) == 0 {
		return nil
	}

	out := make([]string, 0, len(s.Kinds))

	for _, kind := range s.Kinds {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			continue
		}

		out = append(out, kind)
	}

	return out
}

func apiGroupPatternToAPIVersionPattern(apiGroup string) string {
	apiGroup = normalizeAPIVersion(apiGroup)

	if apiGroup == CoreAPIVersion {
		return ""
	}

	if apiGroup == WildcardVersionKindMatcher {
		return WildcardVersionKindMatcher
	}

	if strings.Contains(apiGroup, "/") {
		return apiGroup
	}

	return apiGroup + "/" + WildcardVersionKindMatcher
}

func normalizeAPIVersion(apiVersion string) string {
	if apiVersion == "" {
		return CoreAPIVersion
	}

	return apiVersion
}

func matchAPIGroupPattern(pattern string, gvk schema.GroupVersionKind) bool {
	pattern = normalizeAPIVersion(strings.TrimSpace(pattern))

	if pattern == WildcardVersionKindMatcher {
		return true
	}

	target := gvk.Group
	if pattern == CoreAPIVersion || strings.Contains(pattern, "/") {
		target = gvk.GroupVersion().String()
	}

	return matchPattern(pattern, target)
}

func matchPattern(pattern, value string) bool {
	if pattern == WildcardVersionKindMatcher {
		return true
	}

	if !strings.Contains(pattern, WildcardVersionKindMatcher) {
		return pattern == value
	}

	parts := strings.Split(pattern, WildcardVersionKindMatcher)

	if len(parts) == 2 {
		if parts[0] == "" {
			return strings.HasSuffix(value, parts[1])
		}

		if parts[1] == "" {
			return strings.HasPrefix(value, parts[0])
		}
	}

	idx := 0

	if parts[0] != "" {
		if !strings.HasPrefix(value, parts[0]) {
			return false
		}

		idx = len(parts[0])
	}

	lastPartIndex := len(parts) - 1
	suffix := parts[lastPartIndex]
	limit := len(value)

	if suffix != "" {
		if !strings.HasSuffix(value, suffix) {
			return false
		}

		limit -= len(suffix)
	}

	for _, part := range parts[1:lastPartIndex] {
		if part == "" {
			continue
		}

		if idx > limit {
			return false
		}

		found := strings.Index(value[idx:limit], part)
		if found < 0 {
			return false
		}

		idx += found + len(part)
	}

	return true
}

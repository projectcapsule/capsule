// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"reflect"
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestVersionKindGroupVersionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKind
		want schema.GroupVersionKind
	}{
		{
			name: "empty api version defaults to core v1",
			in: VersionKind{
				APIVersion: "",
				Kind:       "ConfigMap",
			},
			want: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		},
		{
			name: "explicit core v1",
			in: VersionKind{
				APIVersion: "v1",
				Kind:       "Service",
			},
			want: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			},
		},
		{
			name: "group only api version",
			in: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			want: schema.GroupVersionKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		{
			name: "group version api version",
			in: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		},
		{
			name: "wildcard api version is represented as wildcard version selector",
			in: VersionKind{
				APIVersion: "*",
				Kind:       "*",
			},
			want: schema.GroupVersionKind{
				Group:   "",
				Version: "*",
				Kind:    "*",
			},
		},
		{
			name: "partial wildcard group version is parsed",
			in: VersionKind{
				APIVersion: "apps/*",
				Kind:       "*Set",
			},
			want: schema.GroupVersionKind{
				Group:   "apps",
				Version: "*",
				Kind:    "*Set",
			},
		},
		{
			name: "invalid group version keeps kind only",
			in: VersionKind{
				APIVersion: "apps/v1/extra",
				Kind:       "Deployment",
			},
			want: schema.GroupVersionKind{
				Kind: "Deployment",
			},
		},
		{
			name: "empty kind remains empty",
			in: VersionKind{
				APIVersion: "v1",
				Kind:       "",
			},
			want: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "",
			},
		},
		{
			name: "spaces are preserved when parsing group version",
			in: VersionKind{
				APIVersion: " apps/v1 ",
				Kind:       "Deployment",
			},
			want: schema.GroupVersionKind{
				Group:   " apps",
				Version: "v1 ",
				Kind:    "Deployment",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.GroupVersionKind()
			if got != tt.want {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestVersionKindMatchesGroupVersionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern VersionKind
		value   schema.GroupVersionKind
		want    bool
	}{
		{
			name: "empty api version matches core v1 kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "ConfigMap",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: true,
		},
		{
			name: "empty api version does not match grouped kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "explicit core v1 matches core kind",
			pattern: VersionKind{
				APIVersion: "v1",
				Kind:       "Service",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			},
			want: true,
		},
		{
			name: "group only matches any version in group",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1beta1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "group only does not match different group",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "batch",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "group version matches exact group version",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "group version does not match different version",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1beta1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "api version wildcard matches core",
			pattern: VersionKind{
				APIVersion: "*",
				Kind:       "ConfigMap",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: true,
		},
		{
			name: "api version wildcard matches grouped",
			pattern: VersionKind{
				APIVersion: "*",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "partial group version wildcard matches",
			pattern: VersionKind{
				APIVersion: "apps/*",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "partial group version wildcard does not match other group",
			pattern: VersionKind{
				APIVersion: "apps/*",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "batch",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "kind wildcard matches exact api group",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "*",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "StatefulSet",
			},
			want: true,
		},
		{
			name: "partial kind wildcard matches suffix",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "*Set",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "ReplicaSet",
			},
			want: true,
		},
		{
			name: "partial kind wildcard matches prefix",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deploy*",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "empty kind is not wildcard",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: false,
		},
		{
			name: "explicit wildcard kind matches empty kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "*",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "",
			},
			want: true,
		},
		{
			name: "matching is case-sensitive for kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "configmap",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: false,
		},
		{
			name: "matching is case-sensitive for api group",
			pattern: VersionKind{
				APIVersion: "Apps",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "wrong kind does not match",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "Service",
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: false,
		},
		{
			name: "partial group version wildcard matches empty version suffix",
			pattern: VersionKind{
				APIVersion: "apps/*",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "spaces are trimmed when matching VersionKind api version",
			pattern: VersionKind{
				APIVersion: " apps ",
				Kind:       "Deployment",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "spaces are significant in VersionKind kind",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       " Deployment ",
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "api version wildcard and kind wildcard match everything",
			pattern: VersionKind{
				APIVersion: "*",
				Kind:       "*",
			},
			value: schema.GroupVersionKind{
				Group:   "example.corp",
				Version: "v1alpha1",
				Kind:    "Widget",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.pattern.MatchesGroupVersionKind(tt.value)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestVersionKindMatchesVersionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern VersionKind
		value   VersionKind
		want    bool
	}{
		{
			name: "matches same core kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "ConfigMap",
			},
			value: VersionKind{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			want: true,
		},
		{
			name: "group only pattern matches group version value",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			value: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "group version pattern matches same group version value",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			value: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "group version pattern does not match group only value",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			value: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			want: false,
		},
		{
			name: "api version wildcard matches grouped value",
			pattern: VersionKind{
				APIVersion: "*",
				Kind:       "Deployment",
			},
			value: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "kind wildcard matches value kind",
			pattern: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "*",
			},
			value: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "partial wildcard matches",
			pattern: VersionKind{
				APIVersion: "apps/*",
				Kind:       "*Set",
			},
			value: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
			},
			want: true,
		},
		{
			name: "different kind does not match",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "Service",
			},
			value: VersionKind{
				APIVersion: "",
				Kind:       "ConfigMap",
			},
			want: false,
		},
		{
			name: "empty kind does not match value kind",
			pattern: VersionKind{
				APIVersion: "",
				Kind:       "",
			},
			value: VersionKind{
				APIVersion: "",
				Kind:       "ConfigMap",
			},
			want: false,
		},
		{
			name: "partial wildcard api version value is treated through GroupVersionKind",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       "StatefulSet",
			},
			value: VersionKind{
				APIVersion: "apps/*",
				Kind:       "StatefulSet",
			},
			want: true,
		},
		{
			name: "invalid value group version keeps kind only and does not match grouped pattern",
			pattern: VersionKind{
				APIVersion: "apps",
				Kind:       "Deployment",
			},
			value: VersionKind{
				APIVersion: "apps/v1/extra",
				Kind:       "Deployment",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.pattern.MatchesVersionKind(tt.value)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestVersionKindHasWildcard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKind
		want bool
	}{
		{
			name: "no wildcard",
			in: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			want: false,
		},
		{
			name: "api version wildcard",
			in: VersionKind{
				APIVersion: "*",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "partial api version wildcard",
			in: VersionKind{
				APIVersion: "apps/*",
				Kind:       "Deployment",
			},
			want: true,
		},
		{
			name: "kind wildcard",
			in: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "*",
			},
			want: true,
		},
		{
			name: "partial kind wildcard",
			in: VersionKind{
				APIVersion: "apps/v1",
				Kind:       "*Set",
			},
			want: true,
		},
		{
			name: "empty values are not wildcards",
			in: VersionKind{
				APIVersion: "",
				Kind:       "",
			},
			want: false,
		},
		{
			name: "spaces around wildcard still contain wildcard",
			in: VersionKind{
				APIVersion: " * ",
				Kind:       "Deployment",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.HasWildcard()
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestVersionKindsVersionKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKinds
		want []VersionKind
	}{
		{
			name: "empty kinds returns empty",
			in: VersionKinds{
				APIGroups: []string{"apps"},
				Kinds:     nil,
			},
			want: []VersionKind{},
		},
		{
			name: "omitted api groups defaults to core v1",
			in: VersionKinds{
				Kinds: []string{
					"ConfigMap",
					"Service",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "",
					Kind:       "ConfigMap",
				},
				{
					APIVersion: "",
					Kind:       "Service",
				},
			},
		},
		{
			name: "blank api groups default to core v1",
			in: VersionKinds{
				APIGroups: []string{
					"",
					"   ",
				},
				Kinds: []string{
					"ConfigMap",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "",
					Kind:       "ConfigMap",
				},
				{
					APIVersion: "",
					Kind:       "ConfigMap",
				},
			},
		},
		{
			name: "group only api group expands to group wildcard version",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
					"StatefulSet",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
				{
					APIVersion: "apps/*",
					Kind:       "StatefulSet",
				},
			},
		},
		{
			name: "exact group version is preserved",
			in: VersionKinds{
				APIGroups: []string{
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
			},
		},
		{
			name: "multiple api groups and kinds expand as cross product",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"batch/v1",
				},
				Kinds: []string{
					"Deployment",
					"Job",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
				{
					APIVersion: "apps/*",
					Kind:       "Job",
				},
				{
					APIVersion: "batch/v1",
					Kind:       "Deployment",
				},
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
				},
			},
		},
		{
			name: "trims api groups and kinds",
			in: VersionKinds{
				APIGroups: []string{
					" apps ",
				},
				Kinds: []string{
					" Deployment ",
					"",
					"   ",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
			},
		},
		{
			name: "wildcard api group is preserved",
			in: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"*",
					"*Set",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "*",
					Kind:       "*",
				},
				{
					APIVersion: "*",
					Kind:       "*Set",
				},
			},
		},
		{
			name: "partial wildcard api group version is preserved",
			in: VersionKinds{
				APIGroups: []string{
					"apps/*",
				},
				Kinds: []string{
					"*Set",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/*",
					Kind:       "*Set",
				},
			},
		},
		{
			name: "duplicates are preserved",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"apps",
				},
				Kinds: []string{
					"Deployment",
					"Deployment",
				},
			},
			want: []VersionKind{
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
				{
					APIVersion: "apps/*",
					Kind:       "Deployment",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.VersionKinds()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestVersionKindsMatchesGroupVersionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern VersionKinds
		value   schema.GroupVersionKind
		want    bool
	}{
		{
			name: "empty kinds do not match",
			pattern: VersionKinds{
				APIGroups: []string{"*"},
				Kinds:     nil,
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: false,
		},
		{
			name: "omitted api groups match core v1",
			pattern: VersionKinds{
				Kinds: []string{
					"ConfigMap",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: true,
		},
		{
			name: "omitted api groups do not match grouped resource",
			pattern: VersionKinds{
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "blank api group matches core v1",
			pattern: VersionKinds{
				APIGroups: []string{
					"",
				},
				Kinds: []string{
					"Service",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			},
			want: true,
		},
		{
			name: "group only matches any version in group",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1beta1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "group only does not match another group",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "batch",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "group version matches exact version",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "group version does not match another version",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1beta1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "multiple api groups match second group",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
					"batch/v1",
				},
				Kinds: []string{
					"Deployment",
					"Job",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "batch",
				Version: "v1",
				Kind:    "Job",
			},
			want: true,
		},
		{
			name: "multiple kinds match second kind",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
					"StatefulSet",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "StatefulSet",
			},
			want: true,
		},
		{
			name: "api group wildcard matches any group",
			pattern: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"Widget",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "example.corp",
				Version: "v1alpha1",
				Kind:    "Widget",
			},
			want: true,
		},
		{
			name: "kind wildcard matches any kind in group",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"*",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "ReplicaSet",
			},
			want: true,
		},
		{
			name: "partial kind wildcard matches suffix",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"*Set",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "ReplicaSet",
			},
			want: true,
		},
		{
			name: "partial api group wildcard matches group version",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps/*",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "blank kinds are ignored",
			pattern: VersionKinds{
				APIGroups: []string{
					"v1",
				},
				Kinds: []string{
					"",
					"   ",
					"ConfigMap",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: true,
		},
		{
			name: "only blank kinds do not match",
			pattern: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"",
					"   ",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "matching is case-sensitive for kind",
			pattern: VersionKinds{
				APIGroups: []string{
					"v1",
				},
				Kinds: []string{
					"configmap",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
			want: false,
		},
		{
			name: "matching is case-sensitive for api group",
			pattern: VersionKinds{
				APIGroups: []string{
					"Apps",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "api group and kind wildcard match all",
			pattern: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"*",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "example.corp",
				Version: "v1alpha1",
				Kind:    "Widget",
			},
			want: true,
		},
		{
			name: "trimmed api groups and kinds match",
			pattern: VersionKinds{
				APIGroups: []string{
					" apps ",
				},
				Kinds: []string{
					" Deployment ",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "no api group match prevents kind match",
			pattern: VersionKinds{
				APIGroups: []string{
					"batch",
				},
				Kinds: []string{
					"*",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
		{
			name: "no kind match prevents api group match",
			pattern: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Job",
				},
			},
			value: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.pattern.MatchesGroupVersionKind(tt.value)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestVersionKindsHasWildcard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKinds
		want bool
	}{
		{
			name: "no wildcard",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"batch/v1",
				},
				Kinds: []string{
					"Deployment",
					"Job",
				},
			},
			want: false,
		},
		{
			name: "api group wildcard",
			in: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			want: true,
		},
		{
			name: "partial api group wildcard",
			in: VersionKinds{
				APIGroups: []string{
					"apps/*",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			want: true,
		},
		{
			name: "wildcard among api groups",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"batch/*",
				},
				Kinds: []string{
					"Job",
				},
			},
			want: true,
		},
		{
			name: "kind wildcard",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"*",
				},
			},
			want: true,
		},
		{
			name: "partial kind wildcard",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"*Set",
				},
			},
			want: true,
		},
		{
			name: "wildcard among kinds",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
					"*Set",
				},
			},
			want: true,
		},
		{
			name: "empty values are not wildcards",
			in: VersionKinds{
				APIGroups: []string{
					"",
				},
				Kinds: []string{
					"",
				},
			},
			want: false,
		},
		{
			name: "spaces around wildcard still contain wildcard",
			in: VersionKinds{
				APIGroups: []string{
					" * ",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.HasWildcard()
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestVersionKindsValidateKnownKinds(t *testing.T) {
	t.Parallel()

	mapper := newVersionKindTestRESTMapper()

	tests := []struct {
		name      string
		in        VersionKinds
		mapper    apimeta.RESTMapper
		fieldPath string
		wantErr   []string
	}{
		{
			name: "nil mapper skips validation",
			in: VersionKinds{
				APIGroups: []string{
					"unknown.example.com/v1",
				},
				Kinds: []string{
					"NotAThing",
				},
			},
			mapper:    nil,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "omitted api groups validates core v1",
			in: VersionKinds{
				Kinds: []string{
					"ConfigMap",
					"Service",
					"Pod",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "blank api groups validate core v1",
			in: VersionKinds{
				APIGroups: []string{
					"",
					"   ",
				},
				Kinds: []string{
					"ConfigMap",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "known group only kind is valid",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
					"StatefulSet",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "known exact group version kind is valid",
			in: VersionKinds{
				APIGroups: []string{
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "multiple concrete api groups validate all combinations",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"apps/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "unknown core kind is invalid",
			in: VersionKinds{
				Kinds: []string{
					"NotAThing",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0] "NotAThing"`,
				`apiGroups[0] "v1"`,
			},
		},
		{
			name: "wrong api group kind combination is invalid",
			in: VersionKinds{
				APIGroups: []string{
					"batch/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0] "Deployment"`,
				`apiGroups[0] "batch/v1"`,
			},
		},
		{
			name: "exact group version validates exact version",
			in: VersionKinds{
				APIGroups: []string{
					"apps/v1beta1",
				},
				Kinds: []string{
					"StatefulSet",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0] "StatefulSet"`,
				`apiGroups[0] "apps/v1beta1"`,
			},
		},
		{
			name: "multiple api groups report failing api group index",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"batch/v1",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0] "Deployment"`,
				`apiGroups[1] "batch/v1"`,
			},
		},
		{
			name: "multiple kinds report failing kind index",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Deployment",
					"NotADeployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[1] "NotADeployment"`,
				`apiGroups[0] "apps"`,
			},
		},
		{
			name: "wildcard api group skips discovery validation",
			in: VersionKinds{
				APIGroups: []string{
					"*",
				},
				Kinds: []string{
					"NotAThing",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "partial wildcard api group skips discovery validation",
			in: VersionKinds{
				APIGroups: []string{
					"apps/*",
				},
				Kinds: []string{
					"NotAThing",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "wildcard kind skips discovery validation",
			in: VersionKinds{
				APIGroups: []string{
					"unknown.example.com/v1",
				},
				Kinds: []string{
					"*",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "partial wildcard kind skips discovery validation",
			in: VersionKinds{
				APIGroups: []string{
					"unknown.example.com/v1",
				},
				Kinds: []string{
					"*Set",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "blank kinds are ignored",
			in: VersionKinds{
				APIGroups: []string{
					"v1",
				},
				Kinds: []string{
					"",
					"   ",
					"ConfigMap",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "only blank kinds produce no concrete validation",
			in: VersionKinds{
				APIGroups: []string{
					"unknown.example.com/v1",
				},
				Kinds: []string{
					"",
					"   ",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
		{
			name: "invalid group version is invalid",
			in: VersionKinds{
				APIGroups: []string{
					"apps/v1/extra",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0] "Deployment"`,
				`apiGroups[0] "apps/v1/extra"`,
			},
		},
		{
			name: "custom field path is reported",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
				},
				Kinds: []string{
					"Missing",
				},
			},
			mapper:    mapper,
			fieldPath: "spec.rules[3].enforce.metadata[2]",
			wantErr: []string{
				`spec.rules[3].enforce.metadata[2].kinds[0] "Missing"`,
				`apiGroups[0] "apps"`,
			},
		},
		{
			name: "case-sensitive kind fails",
			in: VersionKinds{
				APIGroups: []string{
					"v1",
				},
				Kinds: []string{
					"configmap",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`configmap`,
				`apiGroups[0] "v1"`,
			},
		},
		{
			name: "case-sensitive api group fails",
			in: VersionKinds{
				APIGroups: []string{
					"Apps",
				},
				Kinds: []string{
					"Deployment",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
			wantErr: []string{
				`Deployment`,
				`apiGroups[0] "Apps"`,
			},
		},
		{
			name: "trimmed api group validates",
			in: VersionKinds{
				APIGroups: []string{
					" apps ",
				},
				Kinds: []string{
					" Deployment ",
				},
			},
			mapper:    mapper,
			fieldPath: "rules[0].enforce.metadata[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.in.ValidateKnownKinds(tt.mapper, tt.fieldPath)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}

			for _, expected := range tt.wantErr {
				if !strings.Contains(err.Error(), expected) {
					t.Fatalf("expected error containing %q, got %q", expected, err.Error())
				}
			}
		})
	}
}

func TestVersionKindsNormalizedAPIGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKinds
		want []string
	}{
		{
			name: "nil defaults to core v1",
			in:   VersionKinds{},
			want: []string{"v1"},
		},
		{
			name: "empty slice defaults to core v1",
			in: VersionKinds{
				APIGroups: []string{},
			},
			want: []string{"v1"},
		},
		{
			name: "blank values become core v1",
			in: VersionKinds{
				APIGroups: []string{
					"",
					"   ",
				},
			},
			want: []string{
				"v1",
				"v1",
			},
		},
		{
			name: "trims values",
			in: VersionKinds{
				APIGroups: []string{
					" apps ",
					" batch/v1 ",
				},
			},
			want: []string{
				"apps",
				"batch/v1",
			},
		},
		{
			name: "preserves wildcard values",
			in: VersionKinds{
				APIGroups: []string{
					"*",
					"apps/*",
				},
			},
			want: []string{
				"*",
				"apps/*",
			},
		},
		{
			name: "preserves duplicates",
			in: VersionKinds{
				APIGroups: []string{
					"apps",
					"apps",
				},
			},
			want: []string{
				"apps",
				"apps",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.NormalizedAPIGroups()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestVersionKindsStatusAPIGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKinds
		want []string
	}{
		{
			name: "nil defaults to core v1",
			in:   VersionKinds{},
			want: []string{"v1"},
		},
		{
			name: "blank values become core v1",
			in: VersionKinds{
				APIGroups: []string{
					"",
					"   ",
				},
			},
			want: []string{"v1"},
		},
		{
			name: "explicit wildcard is preserved",
			in: VersionKinds{
				APIGroups: []string{
					"*",
				},
			},
			want: []string{"*"},
		},
		{
			name: "duplicates are removed after normalization",
			in: VersionKinds{
				APIGroups: []string{
					"",
					"v1",
					"apps/v1",
					"apps/v1",
				},
			},
			want: []string{
				"v1",
				"apps/v1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.StatusAPIGroups()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestVersionKindsNormalizedKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   VersionKinds
		want []string
	}{
		{
			name: "nil returns nil",
			in:   VersionKinds{},
			want: nil,
		},
		{
			name: "empty slice returns nil",
			in: VersionKinds{
				Kinds: []string{},
			},
			want: nil,
		},
		{
			name: "trims values and skips blanks",
			in: VersionKinds{
				Kinds: []string{
					" ConfigMap ",
					"",
					"   ",
					"Service",
				},
			},
			want: []string{
				"ConfigMap",
				"Service",
			},
		},
		{
			name: "preserves wildcard values",
			in: VersionKinds{
				Kinds: []string{
					"*",
					"*Set",
				},
			},
			want: []string{
				"*",
				"*Set",
			},
		},
		{
			name: "preserves duplicates",
			in: VersionKinds{
				Kinds: []string{
					"ConfigMap",
					"ConfigMap",
				},
			},
			want: []string{
				"ConfigMap",
				"ConfigMap",
			},
		},
		{
			name: "only blanks returns empty slice",
			in: VersionKinds{
				Kinds: []string{
					"",
					"   ",
				},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.normalizedKinds()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestAPIGroupPatternToAPIVersionPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty becomes legacy empty api version",
			in:   "",
			want: "",
		},
		{
			name: "core v1 becomes legacy empty api version",
			in:   "v1",
			want: "",
		},
		{
			name: "wildcard is preserved",
			in:   "*",
			want: "*",
		},
		{
			name: "group only becomes group wildcard version",
			in:   "apps",
			want: "apps/*",
		},
		{
			name: "exact group version is preserved",
			in:   "apps/v1",
			want: "apps/v1",
		},
		{
			name: "partial group version is preserved",
			in:   "apps/*",
			want: "apps/*",
		},
		{
			name: "spaces are not trimmed by helper",
			in:   " apps ",
			want: " apps /*",
		},
		{
			name: "space padded v1 is treated as group name",
			in:   " v1 ",
			want: " v1 /*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := apiGroupPatternToAPIVersionPattern(tt.in)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeAPIVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty defaults to core v1",
			in:   "",
			want: "v1",
		},
		{
			name: "core v1 stays core v1",
			in:   "v1",
			want: "v1",
		},
		{
			name: "group only stays group only",
			in:   "apps",
			want: "apps",
		},
		{
			name: "group version stays group version",
			in:   "apps/v1",
			want: "apps/v1",
		},
		{
			name: "wildcard stays wildcard",
			in:   "*",
			want: "*",
		},
		{
			name: "spaces are not trimmed",
			in:   " v1 ",
			want: " v1 ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeAPIVersion(tt.in)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestMatchAPIGroupPattern(t *testing.T) {
	t.Parallel()

	coreConfigMap := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
	appsDeployment := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	appsDeploymentBeta := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1beta1",
		Kind:    "Deployment",
	}

	tests := []struct {
		name    string
		pattern string
		gvk     schema.GroupVersionKind
		want    bool
	}{
		{
			name:    "empty pattern defaults to core v1",
			pattern: "",
			gvk:     coreConfigMap,
			want:    true,
		},
		{
			name:    "core v1 pattern matches core",
			pattern: "v1",
			gvk:     coreConfigMap,
			want:    true,
		},
		{
			name:    "core v1 pattern does not match grouped",
			pattern: "v1",
			gvk:     appsDeployment,
			want:    false,
		},
		{
			name:    "group only matches grouped resource",
			pattern: "apps",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "group only matches another version",
			pattern: "apps",
			gvk:     appsDeploymentBeta,
			want:    true,
		},
		{
			name:    "group only does not match other group",
			pattern: "batch",
			gvk:     appsDeployment,
			want:    false,
		},
		{
			name:    "group version matches exact",
			pattern: "apps/v1",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "group version does not match other version",
			pattern: "apps/v1",
			gvk:     appsDeploymentBeta,
			want:    false,
		},
		{
			name:    "wildcard matches core",
			pattern: "*",
			gvk:     coreConfigMap,
			want:    true,
		},
		{
			name:    "wildcard matches grouped",
			pattern: "*",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "partial group wildcard matches group name",
			pattern: "app*",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "partial group version wildcard matches version",
			pattern: "apps/*",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "partial group version wildcard does not match different group",
			pattern: "batch/*",
			gvk:     appsDeployment,
			want:    false,
		},
		{
			name:    "pattern is trimmed",
			pattern: " apps ",
			gvk:     appsDeployment,
			want:    true,
		},
		{
			name:    "case-sensitive mismatch",
			pattern: "Apps",
			gvk:     appsDeployment,
			want:    false,
		},
		{
			name:    "empty version suffix matches apps slash",
			pattern: "apps/*",
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "",
				Kind:    "Deployment",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchAPIGroupPattern(tt.pattern, tt.gvk)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{
			name:    "wildcard constant matches anything",
			pattern: "*",
			value:   "anything",
			want:    true,
		},
		{
			name:    "wildcard constant matches empty",
			pattern: "*",
			value:   "",
			want:    true,
		},
		{
			name:    "exact match",
			pattern: "abc",
			value:   "abc",
			want:    true,
		},
		{
			name:    "exact mismatch",
			pattern: "abc",
			value:   "abd",
			want:    false,
		},
		{
			name:    "empty pattern matches empty",
			pattern: "",
			value:   "",
			want:    true,
		},
		{
			name:    "empty pattern does not match non-empty",
			pattern: "",
			value:   "abc",
			want:    false,
		},
		{
			name:    "prefix wildcard",
			pattern: "*bc",
			value:   "abc",
			want:    true,
		},
		{
			name:    "prefix wildcard mismatch",
			pattern: "*bd",
			value:   "abc",
			want:    false,
		},
		{
			name:    "suffix wildcard",
			pattern: "ab*",
			value:   "abc",
			want:    true,
		},
		{
			name:    "suffix wildcard mismatch",
			pattern: "ac*",
			value:   "abc",
			want:    false,
		},
		{
			name:    "middle wildcard",
			pattern: "a*c",
			value:   "abc",
			want:    true,
		},
		{
			name:    "middle wildcard spans many characters",
			pattern: "a*z",
			value:   "abcdefghijklmnopqrstuvwxyz",
			want:    true,
		},
		{
			name:    "middle wildcard mismatch",
			pattern: "a*d",
			value:   "abc",
			want:    false,
		},
		{
			name:    "multiple wildcards match ordered parts",
			pattern: "a*c*e",
			value:   "abcde",
			want:    true,
		},
		{
			name:    "multiple wildcards mismatch missing suffix",
			pattern: "a*c*f",
			value:   "abcde",
			want:    false,
		},
		{
			name:    "multiple wildcards preserve prefix anchor",
			pattern: "a*c",
			value:   "xxabc",
			want:    false,
		},
		{
			name:    "multiple wildcards preserve suffix anchor",
			pattern: "a*c",
			value:   "abcyy",
			want:    false,
		},
		{
			name:    "wildcards around middle part",
			pattern: "*bc*",
			value:   "abcde",
			want:    true,
		},
		{
			name:    "wildcards around missing middle part",
			pattern: "*bd*",
			value:   "abcde",
			want:    false,
		},
		{
			name:    "consecutive wildcards only",
			pattern: "**",
			value:   "anything",
			want:    true,
		},
		{
			name:    "consecutive wildcards with literals",
			pattern: "a**c",
			value:   "abc",
			want:    true,
		},
		{
			name:    "consecutive wildcards can consume zero characters",
			pattern: "a**c",
			value:   "ac",
			want:    true,
		},
		{
			name:    "wildcard can match separators",
			pattern: "apps/*",
			value:   "apps/v1",
			want:    true,
		},
		{
			name:    "wildcard separator mismatch",
			pattern: "apps/*",
			value:   "batch/v1",
			want:    false,
		},
		{
			name:    "wildcard exact separator match",
			pattern: "apps/*/v1",
			value:   "apps/foo/v1",
			want:    true,
		},
		{
			name:    "wildcard over multiple separators",
			pattern: "apps/*",
			value:   "apps/foo/bar",
			want:    true,
		},
		{
			name:    "asterisk is the only operator",
			pattern: "a.b",
			value:   "acb",
			want:    false,
		},
		{
			name:    "regex-looking pattern is literal",
			pattern: "a[bc]",
			value:   "ab",
			want:    false,
		},
		{
			name:    "question mark is literal",
			pattern: "a?c",
			value:   "abc",
			want:    false,
		},
		{
			name:    "case-sensitive mismatch",
			pattern: "ABC",
			value:   "abc",
			want:    false,
		},
		{
			name:    "unicode exact match",
			pattern: "Ä",
			value:   "Ä",
			want:    true,
		},
		{
			name:    "unicode wildcard match",
			pattern: "Ä*",
			value:   "Äbc",
			want:    true,
		},
		{
			name:    "unicode mismatch",
			pattern: "Ä",
			value:   "A",
			want:    false,
		},
		{
			name:    "spaces are literal",
			pattern: "a c",
			value:   "a c",
			want:    true,
		},
		{
			name:    "spaces mismatch",
			pattern: "a c",
			value:   "abc",
			want:    false,
		},
		{
			name:    "wildcard can consume literal star in value",
			pattern: "a*b",
			value:   "a*b",
			want:    true,
		},
		{
			name:    "literal star cannot be escaped",
			pattern: `a\*b`,
			value:   "a*b",
			want:    false,
		},
		{
			name:    "required suffix is enforced",
			pattern: "*abc",
			value:   "ab",
			want:    false,
		},
		{
			name:    "required prefix is enforced",
			pattern: "abc*",
			value:   "zabc",
			want:    false,
		},
		{
			name:    "trailing stars accepted after value exhausted",
			pattern: "abc***",
			value:   "abc",
			want:    true,
		},
		{
			name:    "trailing non-star rejected after value exhausted",
			pattern: "abc*d",
			value:   "abc",
			want:    false,
		},
		{
			name:    "value longer than exact pattern does not match",
			pattern: "abc",
			value:   "abcdef",
			want:    false,
		},
		{
			name:    "pattern longer than value without wildcard does not match",
			pattern: "abcdef",
			value:   "abc",
			want:    false,
		},
		{
			name:    "zero-length wildcard between literals",
			pattern: "abc*def",
			value:   "abcdef",
			want:    true,
		},
		{
			name:    "non-zero wildcard between literals",
			pattern: "abc*def",
			value:   "abcXYZdef",
			want:    true,
		},
		{
			name:    "missing required suffix after wildcard",
			pattern: "abc*deg",
			value:   "abcdef",
			want:    false,
		},
		{
			name:    "wildcard cannot reorder characters",
			pattern: "a*b*c",
			value:   "acb",
			want:    false,
		},
		{
			name:    "wildcard with repeated chars",
			pattern: "a*a",
			value:   "aaaa",
			want:    true,
		},
		{
			name:    "wildcard cannot create missing required char",
			pattern: "a*b",
			value:   "aaaa",
			want:    false,
		},
		{
			name:    "dot literal with wildcard",
			pattern: "*.k8s.io/v1",
			value:   "apiextensions.k8s.io/v1",
			want:    true,
		},
		{
			name:    "dot literal with wildcard mismatch",
			pattern: "*.k8s.io/v1",
			value:   "apps/v1",
			want:    false,
		},
		{
			name:    "wildcard can match empty suffix",
			pattern: "apps/*",
			value:   "apps/",
			want:    true,
		},
		{
			name:    "wildcard can match empty prefix",
			pattern: "*/v1",
			value:   "/v1",
			want:    true,
		},
		{
			name:    "anchored prefix and suffix with no middle parts",
			pattern: "apps/*/v1",
			value:   "apps//v1",
			want:    true,
		},
		{
			name:    "empty value does not satisfy literal",
			pattern: "ConfigMap",
			value:   "",
			want:    false,
		},
		{
			name:    "single star matches literal star",
			pattern: "*",
			value:   "*",
			want:    true,
		},
		{
			name:    "only literal part and wildcard",
			pattern: "***abc***",
			value:   "abc",
			want:    true,
		},
		{
			name:    "only literal part and wildcard mismatch",
			pattern: "***abc***",
			value:   "ab",
			want:    false,
		},
		{
			name:    "ordered middle parts must all exist",
			pattern: "*ab*cd*ef*",
			value:   "xxabyycdefzz",
			want:    true,
		},
		{
			name:    "ordered middle parts cannot be reversed",
			pattern: "*ab*cd*",
			value:   "xxcdyyab",
			want:    false,
		},
		{
			name:    "prefix suffix and middle part must fit before suffix",
			pattern: "a*b*c",
			value:   "abxc",
			want:    true,
		},
		{
			name:    "middle part cannot overlap suffix region",
			pattern: "a*b*bc",
			value:   "abc",
			want:    false,
		},
		{
			name:    "middle part before suffix region",
			pattern: "a*b*bc",
			value:   "abbc",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func newVersionKindTestRESTMapper() apimeta.RESTMapper {
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{
		{
			Group:   "",
			Version: "v1",
		},
		{
			Group:   "apps",
			Version: "v1",
		},
		{
			Group:   "apps",
			Version: "v1beta1",
		},
		{
			Group:   "batch",
			Version: "v1",
		},
	})

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Service",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1beta1",
			Kind:    "Deployment",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "StatefulSet",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "batch",
			Version: "v1",
			Kind:    "Job",
		},
		apimeta.RESTScopeNamespace,
	)

	return mapper
}

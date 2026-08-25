// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestMetadataRuleNamespaceRequiresExplicitKind(t *testing.T) {
	t.Parallel()

	namespace := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	tests := []struct {
		name  string
		kinds []string
		want  bool
	}{
		{name: "explicit namespace", kinds: []string{"Namespace"}, want: true},
		{name: "wildcard only", kinds: []string{"*"}, want: false},
		{name: "partial wildcard only", kinds: []string{"Name*"}, want: false},
		{name: "explicit alongside wildcard", kinds: []string{"*", "Namespace"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := MetadataRule{VersionKinds: runtime.VersionKinds{
				APIGroups: []string{"*"},
				Kinds:     tt.kinds,
			}}
			if got := rule.MatchesGroupVersionKind(namespace); got != tt.want {
				t.Fatalf("MatchesGroupVersionKind() = %v, want %v", got, tt.want)
			}
		})
	}
}

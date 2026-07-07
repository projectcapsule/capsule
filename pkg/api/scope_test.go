// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestResourceScopeString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		scope api.ResourceScope
		want  string
	}{
		{scope: api.ResourceScopeNamespace, want: "Namespace"},
		{scope: api.ResourceScopeTenant, want: "Tenant"},
		{scope: api.ResourceScopeNone, want: "None"},
		{scope: api.ResourceScope("Custom"), want: "Custom"},
	} {
		if got := tt.scope.String(); got != tt.want {
			t.Fatalf("ResourceScope.String() = %q, want %q", got, tt.want)
		}
	}
}

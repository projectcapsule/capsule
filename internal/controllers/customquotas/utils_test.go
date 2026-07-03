// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestUsagePercentage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		used  string
		limit string
		want  float64
	}{
		{
			name:  "returns zero for zero limit",
			used:  "1",
			limit: "0",
			want:  0,
		},
		{
			name:  "calculates whole quantity percentage",
			used:  "2",
			limit: "8",
			want:  25,
		},
		{
			name:  "calculates milli quantity percentage",
			used:  "250m",
			limit: "1",
			want:  25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := usagePercentage(resource.MustParse(tt.used), resource.MustParse(tt.limit))
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

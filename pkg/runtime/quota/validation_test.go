// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestValidateHardLimitScopeChange(t *testing.T) {
	t.Parallel()

	previous := corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("8"),
	}

	tests := []struct {
		name         string
		hard         corev1.ResourceList
		scopeChanged bool
		wantErr      string
	}{
		{
			name: "rejects decrease while scope changes",
			hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("0"),
			},
			scopeChanged: true,
			wantErr:      "cannot be reduced from 8 to 0 while namespace selectors are changing",
		},
		{
			name:         "rejects removal while scope changes",
			hard:         corev1.ResourceList{},
			scopeChanged: true,
			wantErr:      "cannot be removed while namespace selectors are changing",
		},
		{
			name: "allows equal limit while scope changes",
			hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("8"),
			},
			scopeChanged: true,
		},
		{
			name: "allows increase while scope changes",
			hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("10"),
			},
			scopeChanged: true,
		},
		{
			name: "defers unchanged-scope decrease to usage validation",
			hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("0"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateHardLimitScopeChange(
				"spec.quota.hard",
				test.hard,
				previous,
				test.scopeChanged,
			)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateHardLimitScopeChange() error = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateHardLimitScopeChange() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

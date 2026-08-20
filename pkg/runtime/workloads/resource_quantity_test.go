// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPodLevelResourceSupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      corev1.ResourceName
		supported bool
	}{
		{name: corev1.ResourceCPU, supported: true},
		{name: corev1.ResourceMemory, supported: true},
		{name: corev1.ResourceName("hugepages-2Mi"), supported: true},
		{name: corev1.ResourceEphemeralStorage, supported: false},
		{name: corev1.ResourceName("example.com/gpu"), supported: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			t.Parallel()

			if got := PodLevelResourceSupported(tt.name); got != tt.supported {
				t.Fatalf("PodLevelResourceSupported(%q) = %t, want %t", tt.name, got, tt.supported)
			}
		})
	}
}

func TestLimitForRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource corev1.ResourceName
		request  string
		ratio    string
		want     string
	}{
		{
			name:     "memory",
			resource: corev1.ResourceMemory,
			request:  "1Gi",
			ratio:    "1.5",
			want:     "1536Mi",
		},
		{
			name:     "cpu",
			resource: corev1.ResourceCPU,
			request:  "100m",
			ratio:    "1.5",
			want:     "150m",
		},
		{
			name:     "cpu rounds down to milliCPU",
			resource: corev1.ResourceCPU,
			request:  "1m",
			ratio:    "1.5",
			want:     "1m",
		},
		{
			name:     "storage rounds down to bytes",
			resource: corev1.ResourceEphemeralStorage,
			request:  "3",
			ratio:    "1.5",
			want:     "4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := LimitForRatio(
				tt.resource,
				resource.MustParse(tt.request),
				resource.MustParse(tt.ratio),
			)
			if err != nil {
				t.Fatalf("LimitForRatio() error = %v", err)
			}

			want := resource.MustParse(tt.want)
			if got.Cmp(want) != 0 {
				t.Fatalf("LimitForRatio() = %s, want %s", got.String(), want.String())
			}
		})
	}
}

func TestLimitForRatioRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := LimitForRatio(corev1.ResourceName("example.com/gpu"), resource.MustParse("1"), resource.MustParse("1.5")); err == nil {
		t.Fatal("LimitForRatio() accepted an extended resource")
	}

	if _, err := LimitForRatio(corev1.ResourceCPU, resource.MustParse("0"), resource.MustParse("1.5")); err == nil {
		t.Fatal("LimitForRatio() accepted a zero request")
	}

	if _, err := LimitForRatio(corev1.ResourceCPU, resource.MustParse("1"), resource.MustParse("0.5")); err == nil {
		t.Fatal("LimitForRatio() accepted a ratio below one")
	}
}

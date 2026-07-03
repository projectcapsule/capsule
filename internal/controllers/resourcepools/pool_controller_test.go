// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestResourcePoolFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		claimSize        uint
		namespaceSize    uint
		initialFinalizer bool
		wantFinalizer    bool
	}{
		{
			name:             "adds finalizer when namespaces are managed without claims",
			namespaceSize:    2,
			initialFinalizer: false,
			wantFinalizer:    true,
		},
		{
			name:             "adds finalizer when claims are managed",
			claimSize:        1,
			initialFinalizer: false,
			wantFinalizer:    true,
		},
		{
			name:             "removes finalizer when no managed resources remain",
			initialFinalizer: true,
			wantFinalizer:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &capsulev1beta2.ResourcePool{
				Status: capsulev1beta2.ResourcePoolStatus{
					ClaimSize:     tt.claimSize,
					NamespaceSize: tt.namespaceSize,
				},
			}

			if tt.initialFinalizer {
				controllerutil.AddFinalizer(pool, meta.ControllerFinalizer)
			}

			(&resourcePoolController{}).finalize(context.Background(), pool)

			if got := controllerutil.ContainsFinalizer(pool, meta.ControllerFinalizer); got != tt.wantFinalizer {
				t.Fatalf("finalizer presence = %v, want %v", got, tt.wantFinalizer)
			}
		})
	}
}

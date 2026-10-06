// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestEnsureMetadataProtectsEmptyActiveTenant(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		tnt := &capsulev1beta2.Tenant{Name: "tenant-a", Finalizers: []string{"example.com/other"}}
		if deleting {
			now := metav1.Now()
			tnt.DeletionTimestamp = &now
		}
		manager := &Manager{}
		if err := manager.ensureMetadata(context.Background(), tnt); err != nil {
			t.Fatal(err)
		}
		if controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) == deleting {
			t.Fatalf("deleting=%t finalizers=%v", deleting, tnt.Finalizers)
		}
		if !controllerutil.ContainsFinalizer(tnt, "example.com/other") {
			t.Fatal("unrelated finalizer removed")
		}
		if tnt.Labels[meta.TenantNameLabel] != tnt.Name {
			t.Fatal("missing Tenant name label")
		}
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

func TestActiveTenantRetainsProtectionAfterLastNamespaceDeletion(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Finalizers: []string{meta.ControllerFinalizer}}
	// The namespace has disappeared but its previous status entry remains.
	tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: "gone", UID: "gone-uid"}}
	tnt.Status.Namespaces = []string{"gone"}
	tnt.Status.Size = 1
	manager, _ := namespaceCleanupFixture(t, tnt)
	for _, add := range []func(*runtime.Scheme) error{nodev1.AddToScheme, schedulingv1.AddToScheme, storagev1.AddToScheme} {
		if err := add(manager.Scheme()); err != nil {
			t.Fatal(err)
		}
	}
	req := reconcile.Request{Name: tnt.Name}
	for range 2 {
		if _, err := manager.Reconcile(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), tnt); err != nil {
			t.Fatal(err)
		}
		if !controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) || tnt.DeletionTimestamp != nil ||
			len(tnt.Status.Spaces) != 0 || len(tnt.Status.Namespaces) != 0 || tnt.Status.Size != 0 {
			t.Fatalf("empty active Tenant lost lifecycle protection: %+v", tnt)
		}
	}
	if err := manager.Delete(t.Context(), tnt); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), tnt); !apierrors.IsNotFound(err) {
		t.Fatalf("empty deleting Tenant was not finalized: %v", err)
	}
}

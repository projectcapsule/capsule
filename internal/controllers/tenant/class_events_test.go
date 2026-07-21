// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"reflect"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestTenantClassEventHandlerUpdatesClassStatusDirectly(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant"},
		Status: capsulev1beta2.TenantStatus{
			TenantAvailableStatus: capsulev1beta2.TenantAvailableStatus{
				Classes: capsulev1beta2.TenantAvailableClassesStatus{
					PriorityClasses: []string{"high"},
				},
			},
		},
	}
	storageClass := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithObjects(tenant, storageClass).
		Build()
	manager := &Manager{Client: cl, reader: cl}
	h := manager.tenantClassEventHandler(manager.collectAvailableStorageClasses)

	h.CreateFunc(
		context.Background(),
		event.TypedCreateEvent[client.Object]{Object: storageClass},
		nil,
	)

	updated := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(tenant), updated); err != nil {
		t.Fatalf("get updated Tenant: %v", err)
	}
	if !reflect.DeepEqual(updated.Status.Classes.StorageClasses, []string{"fast"}) {
		t.Fatalf("storage classes = %#v, want [fast]", updated.Status.Classes.StorageClasses)
	}
	if !reflect.DeepEqual(updated.Status.Classes.PriorityClasses, []string{"high"}) {
		t.Fatalf("priority classes = %#v, want preserved [high]", updated.Status.Classes.PriorityClasses)
	}
}

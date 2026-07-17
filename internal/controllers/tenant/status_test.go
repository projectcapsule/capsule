// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestUpdateReconcilingStatusSynchronizesCurrentStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	latest := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant", Generation: 2},
		Status: capsulev1beta2.TenantStatus{
			ObservedGeneration: 2,
			Conditions: capmeta.ConditionList{
				capmeta.NewReadyCondition(&capsulev1beta2.Tenant{}),
			},
		},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(latest).Build()
	manager := &Manager{Client: reader, reader: reader}

	stale := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: latest.Name, Generation: latest.Generation},
	}
	if err := manager.updateReconcilingStatus(context.Background(), stale); err != nil {
		t.Fatalf("update reconciling status: %v", err)
	}

	if len(stale.Status.Conditions) == 0 {
		t.Fatal("current API status was not copied to stale reconcile instance")
	}
	if stale.Status.ObservedGeneration != latest.Status.ObservedGeneration {
		t.Fatalf("observed generation = %d, want %d", stale.Status.ObservedGeneration, latest.Status.ObservedGeneration)
	}
}

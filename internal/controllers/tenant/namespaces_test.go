// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
)

func TestActiveTenantReconcilePrunesMissingNamespacesFromStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"gone"},
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{
				Name: "gone",
			}},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(
			&corev1.Namespace{},
			".metadata.ownerReferences[*].capsule",
			func(client.Object) []string { return nil },
		).
		Build()
	manager := &Manager{Client: cl, Metrics: metrics.NewTenantRecorder()}

	if err := manager.reconcileActiveTenantNamespaces(context.Background(), logr.Discard(), tenant); err != nil {
		t.Fatalf("reconcile active Tenant namespaces: %v", err)
	}

	if len(tenant.Status.Namespaces) != 0 {
		t.Fatalf("status.namespaces = %v, want empty", tenant.Status.Namespaces)
	}

	if len(tenant.Status.Spaces) != 0 {
		t.Fatalf("status.spaces = %v, want empty", tenant.Status.Spaces)
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
)

func TestActiveTenantDoesNotInheritNamespacesFromPreviousUID(t *testing.T) {
	for _, terminating := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminating=%t", terminating), func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "replacement-uid"}
			owned := cleanupOwnedNamespace(tnt, "owned", true)
			previous := cleanupOwnedNamespace(&capsulev1beta2.Tenant{Name: tnt.Name, UID: "previous-uid"}, "previous", terminating)
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			manager, _ := namespaceCleanupFixture(t, tnt, owned, previous, other, foreign)
			if err := manager.reconcileActiveTenantNamespaces(t.Context(), logr.Discard(), tnt); err != nil {
				t.Fatal(err)
			}
			if tnt.Status.Size != 1 || len(tnt.Status.Namespaces) != 1 || tnt.Status.Namespaces[0] != owned.Name || len(tnt.Status.Spaces) != 1 || tnt.Status.Spaces[0].UID != owned.UID {
				t.Fatalf("recreated Tenant inherited another owner: %+v", tnt.Status)
			}
			for _, ns := range []*corev1.Namespace{previous, foreign} {
				current := &corev1.Namespace{}
				if err := manager.Get(t.Context(), client.ObjectKeyFromObject(ns), current); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(current.OwnerReferences, ns.OwnerReferences) || !reflect.DeepEqual(current.Labels, ns.Labels) {
					t.Fatalf("unrelated namespace changed: %+v", current)
				}
			}
		})
	}
}

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
		Name: "tenant-a",
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

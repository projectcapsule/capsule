// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	captenant "github.com/projectcapsule/capsule/pkg/tenant"
)

func TestEnsureMetadataKeepsFinalizerForTenantLabeledNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a-prod",
			Labels: map[string]string{
				meta.TenantLabel: tnt.GetName(),
			},
		},
	}

	c := newTenantTestClient(t, ns)
	r := &Manager{Client: c}

	if err := r.ensureMetadata(ctx, tnt); err != nil {
		t.Fatalf("ensureMetadata() error = %v", err)
	}

	if !controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) {
		t.Fatalf("expected tenant finalizer to be kept while a namespace has %s", meta.TenantLabel)
	}
}

func TestReconcileDeletingTenantNamespacesDeletesLabeledNamespaceMissingFromStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := metav1.Now()
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "tenant-a",
			DeletionTimestamp: &now,
			Finalizers:        []string{meta.ControllerFinalizer},
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a-prod",
			Labels: map[string]string{
				meta.TenantLabel: tnt.GetName(),
			},
		},
	}

	c := newTenantTestClient(t, tnt, ns)
	r := &Manager{
		Client:  c,
		Metrics: metrics.NewTenantRecorder(),
		Log:     logr.Discard(),
	}

	if err := r.reconcileDeletingTenantNamespaces(ctx, logr.Discard(), tnt); err != nil {
		t.Fatalf("reconcileDeletingTenantNamespaces() error = %v", err)
	}

	current := &corev1.Namespace{}
	err := c.Get(ctx, types.NamespacedName{Name: ns.GetName()}, current)
	if !errors.IsNotFound(err) {
		t.Fatalf("expected namespace to be deleted, got err %v", err)
	}
}

func newTenantTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(capsule): %v", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&corev1.Namespace{}, ".metadata.ownerReferences[*].capsule", func(obj client.Object) []string {
			ns, ok := obj.(*corev1.Namespace)
			if !ok {
				return nil
			}

			names := make([]string, 0, len(ns.OwnerReferences))
			for _, ref := range ns.OwnerReferences {
				if captenant.IsTenantOwnerReference(ref) {
					names = append(names, ref.Name)
				}
			}

			return names
		}).
		Build()
}

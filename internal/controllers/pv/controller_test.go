// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pv

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestReconcileRepairsPersistentVolumeTenantLabelFromNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		labels      map[string]string
		wantUpdates int
	}{
		{
			name:        "missing label",
			wantUpdates: 1,
		},
		{
			name:        "empty label",
			labels:      map[string]string{meta.TenantLabel: ""},
			wantUpdates: 1,
		},
		{
			name:        "stale label",
			labels:      map[string]string{meta.TenantLabel: "another-tenant"},
			wantUpdates: 1,
		},
		{
			name:        "current label",
			labels:      map[string]string{meta.TenantLabel: "tenant-a"},
			wantUpdates: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := persistentVolumeTestScheme(t)
			tnt := persistentVolumeTestTenant()
			ns := persistentVolumeTestNamespace(tnt)
			pv := persistentVolumeTestVolume(ns.Name, tt.labels)
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, ns, pv).Build()
			counting := &updateCountingClient{Client: base}
			controller := &Controller{
				client: counting,
				reader: base,
				label:  meta.TenantLabel,
			}

			if _, err := controller.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: pv.Name},
			}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			updated := &corev1.PersistentVolume{}
			if err := base.Get(context.Background(), client.ObjectKeyFromObject(pv), updated); err != nil {
				t.Fatalf("get PersistentVolume: %v", err)
			}

			if got := updated.GetLabels()[meta.TenantLabel]; got != tnt.Name {
				t.Fatalf("tenant label = %q, want %q", got, tnt.Name)
			}

			if counting.updates != tt.wantUpdates {
				t.Fatalf("updates = %d, want %d", counting.updates, tt.wantUpdates)
			}
		})
	}
}

func TestReconcileSkipsPersistentVolumeFromUnmanagedOrDeletedNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace *corev1.Namespace
	}{
		{
			name: "unmanaged namespace",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name: "unmanaged",
			}},
		},
		{
			name: "deleted namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := persistentVolumeTestScheme(t)
			objects := []client.Object{}
			namespace := "deleted"
			if tt.namespace != nil {
				objects = append(objects, tt.namespace)
				namespace = tt.namespace.Name
			}

			pv := persistentVolumeTestVolume(namespace, nil)
			objects = append(objects, pv)
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			counting := &updateCountingClient{Client: base}
			controller := &Controller{client: counting, reader: base, label: meta.TenantLabel}

			if _, err := controller.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: pv.Name},
			}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			if counting.updates != 0 {
				t.Fatalf("updates = %d, want 0", counting.updates)
			}
		})
	}
}

func TestReconcileRetriesInconsistentOrFailedNamespaceResolution(t *testing.T) {
	t.Parallel()

	t.Run("inconsistent ownership", func(t *testing.T) {
		t.Parallel()

		scheme := persistentVolumeTestScheme(t)
		tnt := persistentVolumeTestTenant()
		ns := persistentVolumeTestNamespace(tnt)
		delete(ns.Labels, meta.TenantLabel)
		pv := persistentVolumeTestVolume(ns.Name, nil)
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, ns, pv).Build()
		controller := &Controller{client: base, reader: base, label: meta.TenantLabel}

		_, err := controller.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Name: pv.Name},
		})
		if err == nil {
			t.Fatal("Reconcile() error = nil, want inconsistent ownership error")
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()

		scheme := persistentVolumeTestScheme(t)
		pv := persistentVolumeTestVolume("tenant-a-ns", nil)
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pv).Build()
		controller := &Controller{
			client: base,
			reader: &namespaceFailingReader{
				Reader: base,
				err:    errors.New("temporary API failure"),
			},
			label: meta.TenantLabel,
		}

		_, err := controller.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Name: pv.Name},
		})
		if err == nil || !errors.Is(err, controller.reader.(*namespaceFailingReader).err) {
			t.Fatalf("Reconcile() error = %v, want temporary API failure", err)
		}
	})
}

func TestPersistentVolumePredicate(t *testing.T) {
	t.Parallel()

	pred := persistentVolumePredicate(meta.TenantLabel)
	claimed := persistentVolumeTestVolume("tenant-a-ns", map[string]string{meta.TenantLabel: "tenant-a"})
	unclaimed := claimed.DeepCopy()
	unclaimed.Spec.ClaimRef = nil

	if !pred.Create(event.CreateEvent{Object: claimed}) {
		t.Fatal("claimed PersistentVolume create should reconcile")
	}

	if pred.Create(event.CreateEvent{Object: unclaimed}) {
		t.Fatal("unclaimed PersistentVolume create should not reconcile")
	}

	if pred.Delete(event.DeleteEvent{Object: claimed}) {
		t.Fatal("PersistentVolume delete should not reconcile")
	}

	if pred.Generic(event.GenericEvent{Object: claimed}) {
		t.Fatal("PersistentVolume generic event should not reconcile")
	}

	labelChanged := claimed.DeepCopy()
	labelChanged.Labels[meta.TenantLabel] = ""
	if !pred.Update(event.UpdateEvent{ObjectOld: claimed, ObjectNew: labelChanged}) {
		t.Fatal("tenant label change should reconcile")
	}

	missingLabel := claimed.DeepCopy()
	delete(missingLabel.Labels, meta.TenantLabel)
	emptyLabel := missingLabel.DeepCopy()
	emptyLabel.Labels[meta.TenantLabel] = ""

	if !pred.Update(event.UpdateEvent{ObjectOld: missingLabel, ObjectNew: emptyLabel}) {
		t.Fatal("adding an empty tenant label should reconcile")
	}

	if !pred.Update(event.UpdateEvent{ObjectOld: emptyLabel, ObjectNew: missingLabel}) {
		t.Fatal("removing an empty tenant label should reconcile")
	}

	claimChanged := claimed.DeepCopy()
	claimChanged.Spec.ClaimRef = claimChanged.Spec.ClaimRef.DeepCopy()
	claimChanged.Spec.ClaimRef.Namespace = "tenant-b-ns"
	if !pred.Update(event.UpdateEvent{ObjectOld: claimed, ObjectNew: claimChanged}) {
		t.Fatal("claim reference change should reconcile")
	}

	unrelatedChange := claimed.DeepCopy()
	unrelatedChange.Status.Phase = corev1.VolumeBound
	if pred.Update(event.UpdateEvent{ObjectOld: claimed, ObjectNew: unrelatedChange}) {
		t.Fatal("unrelated PersistentVolume update should not reconcile")
	}
}

func persistentVolumeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	return scheme
}

func persistentVolumeTestTenant() *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{
		Name: "tenant-a",
		UID:  types.UID("tenant-a-uid"),
	}}
}

func persistentVolumeTestNamespace(tnt *capsulev1beta2.Tenant) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "tenant-a-ns",
		Labels: map[string]string{meta.TenantLabel: tnt.Name},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: capsulev1beta2.GroupVersion.String(),
			Kind:       "Tenant",
			Name:       tnt.Name,
			UID:        tnt.UID,
		}},
	}}
}

func persistentVolumeTestVolume(namespace string, labels map[string]string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "pv-" + namespace,
			Labels: labels,
		},
		Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{
			Namespace: namespace,
			Name:      "claim",
		}},
	}
}

type updateCountingClient struct {
	client.Client

	updates int
}

func (c *updateCountingClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	c.updates++

	return c.Client.Update(ctx, obj, opts...)
}

type namespaceFailingReader struct {
	client.Reader

	err error
}

func (r *namespaceFailingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := obj.(*corev1.Namespace); ok {
		return fmt.Errorf("get namespace: %w", r.err)
	}

	return r.Reader.Get(ctx, key, obj, opts...)
}

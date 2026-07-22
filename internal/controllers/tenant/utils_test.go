// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestReadyTenantNamespaces(t *testing.T) {
	t.Parallel()

	tenant := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{
				{Name: "without-condition"},
				{
					Name: "ready",
					Conditions: meta.ConditionList{{
						Type:   meta.ReadyCondition,
						Status: metav1.ConditionTrue,
					}},
				},
				{
					Name: "not-ready",
					Conditions: meta.ConditionList{{
						Type:   meta.ReadyCondition,
						Status: metav1.ConditionFalse,
					}},
				},
				{
					Name: "terminating",
					Conditions: meta.ConditionList{{
						Type:   meta.TerminatingCondition,
						Status: metav1.ConditionTrue,
					}},
				},
			},
		},
	}

	got := readyTenantNamespaces(tenant)
	if len(got) != 2 || got[0] != "without-condition" || got[1] != "ready" {
		t.Fatalf("readyTenantNamespaces() = %v, want [without-condition ready]", got)
	}
}

func TestRunGarbageCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		new  func(string, string, map[string]string) client.Object
	}{
		{
			name: "LimitRange",
			new: func(name, namespace string, labels map[string]string) client.Object {
				return &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
			},
		},
		{
			name: "NetworkPolicy",
			new: func(name, namespace string, labels map[string]string) client.Object {
				return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
			},
		},
		{
			name: "ResourceQuota",
			new: func(name, namespace string, labels map[string]string) client.Object {
				return &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
			},
		},
		{
			name: "RoleBinding",
			new: func(name, namespace string, labels map[string]string) client.Object {
				return &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
			},
		},
		{
			name: "RuleStatus",
			new: func(name, namespace string, labels map[string]string) client.Object {
				return &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			if err := networkingv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			if err := rbacv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			now := metav1.Now()
			namespaces := []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "current"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "departed"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name:              "terminating",
					DeletionTimestamp: &now,
					Finalizers:        []string{"test.projectcapsule.dev/finalizer"},
				}},
			}

			managedLabels := map[string]string{
				meta.NewManagedByCapsuleLabel: meta.ValueController,
				meta.NewTenantLabel:           "green",
			}
			objects := []client.Object{
				tt.new("managed", "current", managedLabels),
				tt.new("managed", "departed", managedLabels),
				tt.new("managed", "terminating", managedLabels),
				tt.new("unmanaged", "departed", map[string]string{meta.NewTenantLabel: "green"}),
				tt.new("other-tenant", "departed", map[string]string{
					meta.NewManagedByCapsuleLabel: meta.ValueController,
					meta.NewTenantLabel:           "blue",
				}),
			}

			allObjects := append(namespaces, objects...)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(allObjects...).Build()
			manager := &Manager{Client: cl, reader: cl}
			tenant := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "green"},
				Status: capsulev1beta2.TenantStatus{Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{
					Name: "current",
					Conditions: meta.ConditionList{{
						Type:   meta.ReadyCondition,
						Status: metav1.ConditionFalse,
					}},
				}}},
			}

			if err := manager.runGarbageCollection(context.Background(), tenant, tt.new("", "", nil)); err != nil {
				t.Fatal(err)
			}

			assertObjectExists(t, cl, tt.new("managed", "current", nil), true)
			assertObjectExists(t, cl, tt.new("managed", "departed", nil), false)
			assertObjectExists(t, cl, tt.new("managed", "terminating", nil), true)
			assertObjectExists(t, cl, tt.new("unmanaged", "departed", nil), true)
			assertObjectExists(t, cl, tt.new("other-tenant", "departed", nil), true)
		})
	}
}

func assertObjectExists(t *testing.T, cl client.Client, obj client.Object, want bool) {
	t.Helper()

	err := cl.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
	if want && err != nil {
		t.Fatalf("expected %T %s to exist: %v", obj, client.ObjectKeyFromObject(obj), err)
	}

	if !want && !apierrors.IsNotFound(err) {
		t.Fatalf("expected %T %s to be deleted, got: %v", obj, client.ObjectKeyFromObject(obj), err)
	}
}

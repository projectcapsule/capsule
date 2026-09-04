// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestGlobalBreakRequestTemplateReconciler(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		selectors  []selectors.NamespaceSelector
		namespaces []string
	}{
		{
			name:       "unrestricted template",
			namespaces: []string{"*"},
		},
		{
			name: "selected namespaces",
			selectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"break-glass": "enabled"}},
			}},
			namespaces: []string{"team-a", "team-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			template := &capsulev1beta2.GlobalBreakRequestTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "template", Generation: 4},
				Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
					NamespaceSelectors: tt.selectors,
				},
			}
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&capsulev1beta2.GlobalBreakRequestTemplate{}).
				WithObjects(
					template,
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"break-glass": "enabled"}}},
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-b"}},
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-c", Labels: map[string]string{"break-glass": "enabled"}}},
				).
				Build()

			r := &GlobalBreakRequestTemplateReconciler{Client: cl}
			if _, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: client.ObjectKey{Name: template.Name},
			}); err != nil {
				t.Fatal(err)
			}

			got := &capsulev1beta2.GlobalBreakRequestTemplate{}
			if err := cl.Get(context.Background(), client.ObjectKey{Name: template.Name}, got); err != nil {
				t.Fatal(err)
			}
			if got.Status.ObservedGeneration != template.Generation {
				t.Fatalf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, template.Generation)
			}
			if len(got.Status.Namespaces) != len(tt.namespaces) {
				t.Fatalf("namespaces = %v, want %v", got.Status.Namespaces, tt.namespaces)
			}
			for i := range tt.namespaces {
				if got.Status.Namespaces[i] != tt.namespaces[i] {
					t.Fatalf("namespaces = %v, want %v", got.Status.Namespaces, tt.namespaces)
				}
			}
		})
	}
}

func TestGlobalBreakRequestTemplateStatusNamespacePresent(t *testing.T) {
	t.Parallel()

	selected := capsulev1beta2.GlobalBreakRequestTemplateStatus{Namespaces: []string{"team-a"}}
	if !selected.NamespacePresent("team-a") || selected.NamespacePresent("team-b") {
		t.Fatalf("selected namespace lookup returned unexpected result")
	}

	unrestricted := capsulev1beta2.GlobalBreakRequestTemplateStatus{Namespaces: []string{"*"}}
	if !unrestricted.NamespacePresent("any-namespace") {
		t.Fatalf("wildcard status should match every namespace")
	}
}

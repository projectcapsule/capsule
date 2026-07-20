// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestSyncAdditionalRoleBindingDoesNotMutateSpecMetadata(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	labels := map[string]string{"example.com/label": "value"}
	annotations := map[string]string{"example.com/annotation": "value"}
	originalLabels := maps.Clone(labels)
	originalAnnotations := maps.Clone(annotations)
	binding := rbac.AdditionalRoleBindingsSpec{
		ClusterRoleName: "custom:pod-viewer",
		Subjects:        []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "alice"}},
		Labels:          labels,
		Annotations:     annotations,
	}

	manager := &Manager{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
	tenant := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "green"}}

	if err := manager.syncAdditionalRoleBinding(
		context.Background(),
		logr.Discard(),
		tenant,
		"green-app",
		map[string]rbac.AdditionalRoleBindingsSpec{"hash": binding},
	); err != nil {
		t.Fatal(err)
	}

	if !maps.Equal(labels, originalLabels) {
		t.Fatalf("binding labels were mutated: got %v, want %v", labels, originalLabels)
	}

	if !maps.Equal(annotations, originalAnnotations) {
		t.Fatalf("binding annotations were mutated: got %v, want %v", annotations, originalAnnotations)
	}
}

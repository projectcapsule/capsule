// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestNewReplicationContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		object        metav1.Object
		wantName      string
		wantNamespace string
	}{
		{
			name: "TenantResource",
			object: &capsulev1beta2.TenantResource{ObjectMeta: replicationObjectMeta(
				"tenant-distribution",
				"solar-system",
			)},
			wantName:      "tenant-distribution",
			wantNamespace: "solar-system",
		},
		{
			name: "GlobalTenantResource",
			object: &capsulev1beta2.GlobalTenantResource{ObjectMeta: replicationObjectMeta(
				"global-distribution",
				"",
			)},
			wantName: "global-distribution",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			context, err := newReplicationContext(test.object)
			if err != nil {
				t.Fatalf("newReplicationContext() error = %v", err)
			}

			metadata, ok := context["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("metadata = %#v", context["metadata"])
			}
			if metadata["name"] != test.wantName {
				t.Fatalf("metadata.name = %#v, want %q", metadata["name"], test.wantName)
			}
			if namespace, _ := metadata["namespace"].(string); namespace != test.wantNamespace {
				t.Fatalf("metadata.namespace = %q, want %q", namespace, test.wantNamespace)
			}
			if metadata["uid"] != "replication-uid" {
				t.Fatalf("metadata.uid = %#v", metadata["uid"])
			}
			if metadata["generation"] != int64(3) {
				t.Fatalf("metadata.generation = %#v", metadata["generation"])
			}
			if metadata["labels"].(map[string]any)["company.example/team"] != "platform" {
				t.Fatalf("metadata.labels = %#v", metadata["labels"])
			}

			annotations := metadata["annotations"].(map[string]any)
			if annotations["company.example/source"] != "git" {
				t.Fatalf("metadata.annotations = %#v", annotations)
			}
			if _, exists := annotations["kubectl.kubernetes.io/last-applied-configuration"]; exists {
				t.Fatalf("last-applied annotation was exposed: %#v", annotations)
			}
			if _, exists := metadata["managedFields"]; exists {
				t.Fatalf("managedFields were exposed: %#v", metadata["managedFields"])
			}
		})
	}
}

func replicationObjectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            name,
		Namespace:       namespace,
		UID:             types.UID("replication-uid"),
		ResourceVersion: "7",
		Generation:      3,
		Labels: map[string]string{
			"company.example/team": "platform",
		},
		Annotations: map[string]string{
			"company.example/source":                           "git",
			"kubectl.kubernetes.io/last-applied-configuration": "large-payload",
		},
		Finalizers: []string{"capsule.clastix.io/replication"},
		ManagedFields: []metav1.ManagedFieldsEntry{{
			Manager: "capsule",
		}},
	}
}

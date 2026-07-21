// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestReconcileManagedMetadataRequiresRESTConfig(t *testing.T) {
	t.Parallel()

	err := (Manager{}).reconcileManagedMetadata(
		context.Background(),
		&capsulev1beta2.RuleStatus{},
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "REST config is required") {
		t.Fatalf("expected missing REST config error, got %v", err)
	}
}

func TestRemovedMetadataKeys(t *testing.T) {
	t.Parallel()

	removed := removedMetadataKeys(
		map[string]string{"kept": "old", "removed": "value"},
		map[string]string{"kept": "new", "added": "value"},
	)
	if len(removed) != 1 {
		t.Fatalf("expected one removed key, got %#v", removed)
	}
	if value, ok := removed["removed"]; !ok || value != nil {
		t.Fatalf("expected removed key to have null merge-patch value, got %#v", removed)
	}
}

func TestRemoveManagedMetadata(t *testing.T) {
	t.Parallel()

	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName("solar-test")
	obj.SetLabels(map[string]string{"kept": "value", "test": "true"})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)

	err := removeManagedMetadata(
		context.Background(),
		dynamicClient,
		gvr,
		"",
		obj.GetName(),
		map[string]any{"test": nil},
		nil,
	)
	if err != nil {
		t.Fatalf("removeManagedMetadata() error = %v", err)
	}

	got, err := dynamicClient.Resource(gvr).Get(context.Background(), obj.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get patched Namespace: %v", err)
	}
	if _, ok := got.GetLabels()["test"]; ok {
		t.Fatalf("managed label was not removed: %#v", got.GetLabels())
	}
	if got.GetLabels()["kept"] != "value" {
		t.Fatalf("unmanaged label changed: %#v", got.GetLabels())
	}
}

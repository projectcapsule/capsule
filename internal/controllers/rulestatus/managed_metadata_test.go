// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
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

func TestApplyManagedMetadataSkipsGoneObjects(t *testing.T) {
	t.Parallel()

	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("patch", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "settings")
	})

	err := applyManagedMetadata(
		context.Background(),
		dynamicClient,
		schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		"solar-test",
		"settings",
		map[string]string{"managed": "true"},
		nil,
		"test-manager",
	)
	if err != nil {
		t.Fatalf("applyManagedMetadata() error = %v", err)
	}
}

func TestApplyManagedMetadataReturnsNonNotFoundPatchErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patchErr error
	}{
		{name: "forbidden", patchErr: apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "settings", errors.New("denied"))},
		{name: "other error", patchErr: errors.New("patch failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			dynamicClient.PrependReactor("patch", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, tt.patchErr
			})

			err := applyManagedMetadata(
				context.Background(),
				dynamicClient,
				schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
				schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
				"solar-test",
				"settings",
				map[string]string{"managed": "true"},
				nil,
				"test-manager",
			)
			if !errors.Is(err, tt.patchErr) {
				t.Fatalf("applyManagedMetadata() error = %v, want %v", err, tt.patchErr)
			}
		})
	}
}

func TestReconcileObjectManagedMetadataStopsAfterGoneRemoval(t *testing.T) {
	t.Parallel()

	patches := 0
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("patch", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		patches++

		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "settings")
	})

	err := reconcileObjectManagedMetadata(
		context.Background(),
		dynamicClient,
		schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		"solar-test",
		"settings",
		map[string]string{"removed": "value"},
		nil,
		nil,
		nil,
		"test-manager",
	)
	if err != nil {
		t.Fatalf("reconcileObjectManagedMetadata() error = %v", err)
	}
	if patches != 1 {
		t.Fatalf("reconcileObjectManagedMetadata() made %d patches, want 1", patches)
	}
}

func TestManagedMetadataTargetsOnlyReferencedGVKs(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Version: "v1"},
		{Group: "apps", Version: "v1"},
	})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, k8smeta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, k8smeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, k8smeta.RESTScopeNamespace)

	managed := "controlled"
	bodies := []*rules.NamespaceRuleBodyNamespace{{
		Enforce: &rules.NamespaceRuleEnforceBody{
			Metadata: []rules.MetadataRule{
				{
					VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Namespace", "ConfigMap"}},
					Labels:       map[string]rules.MetadataValueRule{"example.com/managed": {Managed: &managed}},
				},
				{
					VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"apps/v1"}, Kinds: []string{"Deployment"}},
					Annotations:  map[string]rules.MetadataValueRule{"example.com/managed": {Managed: &managed}},
				},
			},
		},
	}}

	targets, err := managedMetadataTargets(mapper, bodies, bodies)
	if err != nil {
		t.Fatalf("managedMetadataTargets() error = %v", err)
	}

	want := []managedMetadataTarget{
		{
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			gvk: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		},
		{
			gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			gvk: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		},
	}

	if len(targets) != len(want) {
		t.Fatalf("managedMetadataTargets() = %#v, want %#v", targets, want)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Fatalf("managedMetadataTargets()[%d] = %#v, want %#v", i, targets[i], want[i])
		}
	}
}

func TestManagedMetadataTargetsRejectWildcards(t *testing.T) {
	t.Parallel()

	managed := "controlled"
	bodies := []*rules.NamespaceRuleBodyNamespace{{
		Enforce: &rules.NamespaceRuleEnforceBody{
			Metadata: []rules.MetadataRule{{
				VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"*"}, Kinds: []string{"ConfigMap"}},
				Labels:       map[string]rules.MetadataValueRule{"example.com/managed": {Managed: &managed}},
			}},
		},
	}}

	_, err := managedMetadataTargets(nil, bodies)
	if err == nil || !strings.Contains(err.Error(), "requires concrete apiGroups and kinds") {
		t.Fatalf("managedMetadataTargets() error = %v", err)
	}
}

func TestReconcileManagedMetadataTargetUsesPagination(t *testing.T) {
	t.Parallel()

	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "ConfigMapList"},
	)
	listCalls := 0
	patchCalls := 0
	dynamicClient.PrependReactor("list", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listCalls++

		listAction, ok := action.(interface{ GetListOptions() metav1.ListOptions })
		if !ok {
			t.Fatalf("list action does not expose list options: %T", action)
		}
		opts := listAction.GetListOptions()
		if opts.Limit != managedMetadataListPageSize {
			t.Fatalf("list limit = %d, want %d", opts.Limit, managedMetadataListPageSize)
		}

		item := unstructured.Unstructured{}
		item.SetName("settings")
		items := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}
		if listCalls == 1 {
			if opts.Continue != "" {
				t.Fatalf("first list continue token = %q, want empty", opts.Continue)
			}
			items.SetContinue("next-page")
		} else if opts.Continue != "next-page" {
			t.Fatalf("second list continue token = %q, want next-page", opts.Continue)
		}

		return true, items, nil
	})
	dynamicClient.PrependReactor("patch", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		patchCalls++

		return true, &unstructured.Unstructured{}, nil
	})

	err := reconcileManagedMetadataTarget(
		context.Background(),
		dynamicClient,
		managedMetadataTarget{
			gvr: gvr,
			gvk: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		},
		"solar-test",
		nil,
		nil,
		map[string]string{"managed": "true"},
		nil,
		"test-manager",
	)
	if err != nil {
		t.Fatalf("reconcileManagedMetadataTarget() error = %v", err)
	}
	if listCalls != 2 {
		t.Fatalf("list calls = %d, want 2", listCalls)
	}
	if patchCalls != 2 {
		t.Fatalf("patch calls = %d, want 2", patchCalls)
	}
}

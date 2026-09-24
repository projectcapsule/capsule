// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/template"
)

func TestCollectorCarriesBlockPolicyToEveryItem(t *testing.T) {
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	c := fake.NewClientBuilder().Build()
	collector := NewCollector(c, mapper)
	source := newUnstructured("v1", "ConfigMap", "source", "copied")
	key, _ := gvk.KeyFromUnstructured(source)
	sources := map[gvk.ResourceKey]*unstructured.Unstructured{key: source}
	for _, policy := range []*apiruntime.ResourceReplicationPolicy{nil, {Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Force: true, Deletion: apiruntime.ResourceDeletionPolicyOrphan}} {
		spec := capsulev1beta2.ResourceSpec{
			Policy:     policy,
			RawItems:   []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"raw"}}`)}},
			Generators: []capsulev1beta2.TemplateItemSpec{{MissingKey: template.MissingKeyZero, Template: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"generated"}}`}},
		}
		acc := processor.Accumulator{}
		for _, name := range []string{"tenant-a", "tenant-b"} {
			if err := collector.CollectForNamespace(t.Context(), c, CollectorOptions{Accumulator: acc}, capsulev1beta2.Tenant{Name: name}, "0", spec, sources, &corev1.Namespace{Name: name + "-target"}); err != nil {
				t.Fatal(err)
			}
		}
		if len(acc) != 6 {
			t.Fatalf("collected %d items, want 6", len(acc))
		}
		for _, item := range acc {
			if item.Resource.Namespace != item.Resource.Tenant+"-target" {
				t.Fatalf("cross-tenant item: %#v", item.Resource)
			}
			for _, object := range *item.Objects {
				if !reflect.DeepEqual(object.Policy, policy) {
					t.Fatalf("policy missing from %s", item.Resource.Name)
				}
			}
		}
	}
}

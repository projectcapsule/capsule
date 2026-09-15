// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

func BenchmarkCollectorCollect(b *testing.B) {
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	c := fake.NewClientBuilder().Build()
	collector := NewCollector(c, mapper)
	tnt := &capsulev1beta2.Tenant{ObjectMeta: collectorBenchmarkMetadata("tenant")}
	for i := range 100 {
		tnt.Status.Namespaces = append(tnt.Status.Namespaces, fmt.Sprintf("target-%d", i))
	}
	namespace := &corev1.Namespace{ObjectMeta: collectorBenchmarkMetadata("target")}

	for _, test := range []struct {
		name string
		spec capsulev1beta2.ResourceSpec
	}{
		{name: "no-authored-items"},
		{
			name: "raw",
			spec: capsulev1beta2.ResourceSpec{
				RawItems: []capsulev1beta2.RawExtension{{RawExtension: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"example"},"data":{"tenant":"{{tenant.name}}"}}`),
				}}},
				AdditionalMetadata: &api.AdditionalMetadataSpec{
					Labels:      map[string]string{"tenant": "{{tenant.name}}"},
					Annotations: map[string]string{"target": "{{namespace}}"},
				},
			},
		},
		{
			name: "generator",
			spec: capsulev1beta2.ResourceSpec{
				Generators: []capsulev1beta2.TemplateItemSpec{{
					MissingKey: "error",
					Template:   `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"example"},"data":{"tenant":"{{ .tenant.metadata.name }}","namespace":"{{ .namespace.metadata.name }}"}}`,
				}},
			},
		},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				// Give every iteration the same input, including metadata that older collectors mutate.
				ns := namespace.DeepCopy()
				opts := CollectorOptions{
					Accumulator: processor.Accumulator{},
					Iterator:    NewCollectorIteratorOptions(tnt, ns, test.spec),
				}
				if err := collector.Collect(b.Context(), c, opts, tnt, "0", test.spec, ns); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCollectorReplicate(b *testing.B) {
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	source := &corev1.ConfigMap{
		ObjectMeta: collectorBenchmarkMetadata("example"),
		Data:       map[string]string{"config": strings.Repeat("x", 4096)},
	}
	source.Namespace = "source"
	c := fake.NewClientBuilder().WithObjects(source).WithReturnManagedFields().Build()
	collector := NewCollector(c, mapper)
	tnt := capsulev1beta2.Tenant{ObjectMeta: collectorBenchmarkMetadata("tenant")}
	spec := capsulev1beta2.ResourceSpec{
		NamespacedItems: []tpl.ResourceReference{{
			VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
			Name:        source.Name,
			Namespace:   source.Namespace,
		}},
	}
	targets := make([]corev1.Namespace, 10)
	for i := range targets {
		targets[i].Name = fmt.Sprintf("target-%d", i)
	}

	b.ReportAllocs()
	for b.Loop() {
		opts := CollectorOptions{Accumulator: processor.Accumulator{}, AllowCrossNamespaceSelection: true}
		sources, err := collector.CollectNamespacedItems(b.Context(), c, opts, spec, nil, tnt)
		if err != nil {
			b.Fatal(err)
		}
		opts.AllowCrossNamespaceSelection = false
		for i := range targets {
			if err := collector.CollectForNamespace(b.Context(), c, opts, tnt, "0", spec, sources, &targets[i]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func collectorBenchmarkMetadata(name string) metav1.ObjectMeta {
	fields := make([]string, 100)
	for i := range fields {
		fields[i] = fmt.Sprintf(`"f:field-%d":{}`, i)
	}

	return metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			"team": "platform",
		},
		Annotations: map[string]string{
			"description": "benchmark",
			"kubectl.kubernetes.io/last-applied-configuration": strings.Repeat("x", 16*1024),
		},
		ManagedFields: []metav1.ManagedFieldsEntry{{
			Manager:    "kubectl",
			Operation:  metav1.ManagedFieldsOperationUpdate,
			APIVersion: "v1",
			FieldsType: "FieldsV1",
			FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:spec":{` + strings.Join(fields, ",") + `}}`)},
		}},
	}
}

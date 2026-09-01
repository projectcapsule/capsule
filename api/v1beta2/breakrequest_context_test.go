// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

func TestBreakRequestLoadsParameterTemplatedContextForAllItems(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-settings", Namespace: "team-a"},
		Data:       map[string]string{"environment": "production"},
	}).Build()
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)

	paramSchema := runtime.RawExtension{Raw: []byte(`{
		"type":"object",
		"required":["name","source"],
		"properties":{
			"name":{"type":"string"},
			"source":{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}
		}
	}`)}
	templateContext := &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
		ResourceReference: tpl.ResourceReference{
			VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
			Name:        "{{ .source.name }}",
		},
		Index: "settings",
	}}}
	resources := []apiruntime.ResourceTemplate{
		{Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .name }}-one"},"data":{"environment":"{{ (index .settings 0).data.environment }}"}}`)}}},
		{Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .name }}-two"},"data":{"environment":"{{ (index .settings 0).data.environment }}"}}`)}}},
	}
	br := &BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
		Spec: BreakRequestSpec{Params: &runtime.RawExtension{Raw: []byte(`{
			"name":"temporary-access",
			"source":{"name":"platform-settings"}
		}`)}},
	}

	loaded, err := br.LoadTemplateContext(context.Background(), cl, mapper, &paramSchema, templateContext)
	if err != nil {
		t.Fatalf("LoadTemplateContext() error = %v", err)
	}
	items, err := br.RenderResources(&paramSchema, resources, loaded)
	if err != nil {
		t.Fatalf("RenderResources() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("RenderResources() resources = %d, want 2", len(items))
	}

	for i, item := range items {
		rendered := string(item.Targets[0].Raw)
		if !strings.Contains(rendered, "temporary-access-") || !strings.Contains(rendered, "production") {
			t.Fatalf("rendered item %d does not contain parameter and loaded context: %s", i, rendered)
		}
	}
}

func TestBreakRequestRendersTargetsAndStructuredMultiYAMLTemplate(t *testing.T) {
	t.Parallel()

	br := &BreakRequest{Spec: BreakRequestSpec{Params: &runtime.RawExtension{Raw: []byte(`{"name":"temporary-access"}`)}}}
	resources, err := br.RenderResources(
		&runtime.RawExtension{Raw: []byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)},
		[]apiruntime.ResourceTemplate{{
			Policy:  apiruntime.ResourceTemplatePolicy{Force: true},
			Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .name }}-direct"}}`)}},
			Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ $.params.name }}-first
data:
  group: {{ (index $.context.resources.crd 0).spec.group }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ $.params.name }}-second
`,
		}},
		tpl.ReferenceContext{"crd": []map[string]any{{"spec": map[string]any{"group": "platform.example.io"}}}},
	)
	if err != nil {
		t.Fatalf("RenderResources() error = %v", err)
	}
	if len(resources) != 1 || len(resources[0].Targets) != 3 {
		t.Fatalf("RenderResources() groups/targets = %d/%d, want 1/3", len(resources), len(resources[0].Targets))
	}
	if !resources[0].Policy.Force {
		t.Fatal("RenderResources() did not preserve resource policy")
	}
	direct := string(resources[0].Targets[0].Raw)
	if !strings.Contains(direct, "temporary-access-direct") {
		t.Fatalf("direct target did not use flat params: %s", direct)
	}
	first, ok := resources[0].Targets[1].Object.(interface {
		GetName() string
	})
	if !ok || first.GetName() != "temporary-access-first" {
		t.Fatalf("first template target = %#v", resources[0].Targets[1].Object)
	}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resources[0].Targets[1].Object)
	if err != nil {
		t.Fatalf("converting first template target: %v", err)
	}
	if group, _, _ := unstructured.NestedString(content, "data", "group"); group != "platform.example.io" {
		t.Fatalf("first template target group = %q", group)
	}
}

func TestBreakRequestContextCannotReplaceParameter(t *testing.T) {
	t.Parallel()

	br := &BreakRequest{Spec: BreakRequestSpec{Params: &runtime.RawExtension{Raw: []byte(`{"name":"request-value"}`)}}}
	_, err := br.RenderResources(
		&runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`)},
		[]apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .name }}"}}`)}}}},
		tpl.ReferenceContext{"name": "context-value"},
	)
	if err == nil || !strings.Contains(err.Error(), "conflicts with a request parameter") {
		t.Fatalf("RenderResources() error = %v, want parameter conflict", err)
	}
}

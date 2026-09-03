// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
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

func TestBreakRequestRendersTrustedRequestContext(t *testing.T) {
	t.Parallel()

	created := metav1.NewTime(time.Date(2026, time.September, 1, 8, 30, 0, 0, time.UTC))
	br := &BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "temporary-access", CreationTimestamp: created},
		Spec: BreakRequestSpec{Requestor: breaktheglass.AccessEntity{
			Name:   "alice",
			Groups: []string{"system:authenticated", "platform-on-call"},
		}},
	}

	resources, err := br.RenderResources(
		&runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
		[]apiruntime.ResourceTemplate{{
			Targets: []runtime.RawExtension{{Raw: []byte(`{
				"apiVersion":"v1",
				"kind":"ConfigMap",
				"metadata":{"name":"raw-request-context"},
				"data":{
					"name":"{{ .request.name }}",
					"username":"{{ .request.username }}",
					"groups":"{{ join "," .request.groups }}",
					"timestamp":"{{ .request.timestamp }}"
				}
			}`)}},
			Template: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: generated-request-context
data:
  name: '{{ $.request.name }}'
  username: '{{ $.request.username }}'
  groups: '{{ join "," $.request.groups }}'
  timestamp: '{{ $.request.timestamp }}'
`,
		}},
	)
	if err != nil {
		t.Fatalf("RenderResources() error = %v", err)
	}
	if len(resources) != 1 || len(resources[0].Targets) != 2 {
		t.Fatalf("RenderResources() groups/targets = %d/%d, want 1/2", len(resources), len(resources[0].Targets))
	}

	direct := string(resources[0].Targets[0].Raw)
	for _, expected := range []string{
		`"name":"temporary-access"`,
		`"username":"alice"`,
		`"groups":"system:authenticated,platform-on-call"`,
		`"timestamp":"2026-09-01T08:30:00Z"`,
	} {
		if !strings.Contains(direct, expected) {
			t.Fatalf("direct target does not contain %s: %s", expected, direct)
		}
	}

	generated, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resources[0].Targets[1].Object)
	if err != nil {
		t.Fatalf("converting generated target: %v", err)
	}
	data, found, err := unstructured.NestedStringMap(generated, "data")
	if err != nil || !found {
		t.Fatalf("generated data not found: found=%t err=%v", found, err)
	}
	want := map[string]string{
		"name":      "temporary-access",
		"username":  "alice",
		"groups":    "system:authenticated,platform-on-call",
		"timestamp": "2026-09-01T08:30:00Z",
	}
	for key, value := range want {
		if data[key] != value {
			t.Fatalf("generated data[%q] = %q, want %q", key, data[key], value)
		}
	}
}

func TestBreakRequestRequestContextKeyIsReserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schema   *runtime.RawExtension
		params   *runtime.RawExtension
		contexts []tpl.ReferenceContext
		want     string
	}{
		{
			name:   "parameter",
			schema: &runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"request":{"type":"string"}}}`)},
			params: &runtime.RawExtension{Raw: []byte(`{"request":"untrusted"}`)},
			want:   `request parameter key "request" is reserved`,
		},
		{
			name:     "loaded context",
			schema:   &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
			contexts: []tpl.ReferenceContext{{"request": []any{}}},
			want:     `template context key "request" is reserved`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			br := &BreakRequest{Spec: BreakRequestSpec{Params: tt.params}}
			_, err := br.RenderResources(
				tt.schema,
				[]apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Raw: []byte(
					`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test"}}`,
				)}}}},
				tt.contexts...,
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RenderResources() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestBreakRequestRendersRequesterRoleBindingCartesianProduct(t *testing.T) {
	t.Parallel()

	created := metav1.NewTime(time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC))
	br := &BreakRequest{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: created},
		Spec: BreakRequestSpec{
			Requestor: breaktheglass.AccessEntity{Name: "alice"},
			Params: &runtime.RawExtension{Raw: []byte(`{
				"clusterRoles":["view","edit"],
				"namespaces":["solar-dev","solar-test"]
			}`)},
		},
	}

	resources, err := br.RenderResources(
		&runtime.RawExtension{Raw: []byte(`{
			"type":"object",
			"required":["clusterRoles","namespaces"],
			"properties":{
				"clusterRoles":{"type":"array","items":{"type":"string"}},
				"namespaces":{"type":"array","items":{"type":"string"}}
			}
		}`)},
		[]apiruntime.ResourceTemplate{{Template: `
{{ range $namespace := $.params.namespaces }}
{{ range $clusterRole := $.params.clusterRoles }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: 'btg-{{ deterministicUUID $.request.username $.request.timestamp $namespace $clusterRole | toLower }}'
  namespace: '{{ $namespace }}'
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: '{{ $clusterRole }}'
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: '{{ $.request.username }}'
{{ end }}
{{ end }}
`}},
	)
	if err != nil {
		t.Fatalf("RenderResources() error = %v", err)
	}
	if len(resources) != 1 || len(resources[0].Targets) != 4 {
		t.Fatalf("RenderResources() groups/targets = %d/%d, want 1/4", len(resources), len(resources[0].Targets))
	}

	want := map[string]bool{
		"solar-dev/view":  false,
		"solar-dev/edit":  false,
		"solar-test/view": false,
		"solar-test/edit": false,
	}
	for _, target := range resources[0].Targets {
		obj, ok := target.Object.(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("target object type = %T, want *unstructured.Unstructured", target.Object)
		}
		role, found, err := unstructured.NestedString(obj.Object, "roleRef", "name")
		if err != nil || !found {
			t.Fatalf("target roleRef.name not found: found=%t err=%v", found, err)
		}
		subjects, found, err := unstructured.NestedSlice(obj.Object, "subjects")
		if err != nil || !found || len(subjects) != 1 {
			t.Fatalf("target subjects = %#v, found=%t err=%v", subjects, found, err)
		}
		subjectMap, ok := subjects[0].(map[string]any)
		if !ok || subjectMap["name"] != "alice" {
			t.Fatalf("target subject = %#v, want alice", subjects[0])
		}

		key := obj.GetNamespace() + "/" + role
		if _, found := want[key]; !found {
			t.Fatalf("unexpected namespace/ClusterRole pair %q", key)
		}
		want[key] = true
	}
	for key, found := range want {
		if !found {
			t.Fatalf("missing namespace/ClusterRole pair %q", key)
		}
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

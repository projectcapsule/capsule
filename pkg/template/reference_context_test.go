// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResourceReferenceTemplating(t *testing.T) {
	t.Parallel()

	ref := tpl.ResourceReference{
		Name:      "{{ tenant.name }}-config",
		Namespace: "{{ namespace }}",
		Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "{{ tenant.name }}"}},
	}
	if !ref.RequiresTemplating() {
		t.Fatalf("RequiresTemplating() = false, want true")
	}

	got, err := ref.LoadTemplated(map[string]string{"tenant.name": "team-a", "namespace": "team-a-dev"})
	if err != nil {
		t.Fatalf("LoadTemplated() unexpected error: %v", err)
	}
	if got.Name != "team-a-config" || got.Namespace != "team-a-dev" || got.Selector.MatchLabels["app"] != "team-a" {
		t.Fatalf("LoadTemplated() = %#v", got)
	}
	if ref.Name == got.Name {
		t.Fatalf("LoadTemplated() mutated original reference")
	}
}

func TestResourceReferenceLoadResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		configMap("team-a", "settings", map[string]string{"app": "api", "tenant": "team-a"}),
		configMap("team-a", "other", map[string]string{"app": "worker", "tenant": "team-a"}),
		configMap("team-b", "settings", map[string]string{"app": "api", "tenant": "team-b"}),
	).Build()
	mapper := templateRESTMapper()

	t.Run("loads named object", func(t *testing.T) {
		t.Parallel()

		got, err := (tpl.ResourceReference{
			VersionKind: tplVersionKind("v1", "ConfigMap"),
			Name:        "settings",
			Optional:    true,
		}).LoadResources(ctx, cl, mapper, "team-a", nil, nil, false, nil)
		if err != nil {
			t.Fatalf("LoadResources() unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].GetName() != "settings" || got[0].GetNamespace() != "team-a" {
			t.Fatalf("LoadResources() = %#v", got)
		}
	})

	t.Run("optional missing named object returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := (tpl.ResourceReference{
			VersionKind: tplVersionKind("v1", "ConfigMap"),
			Name:        "missing",
			Optional:    true,
		}).LoadResources(ctx, cl, mapper, "team-a", nil, nil, false, nil)
		if err != nil {
			t.Fatalf("LoadResources() unexpected error for optional missing object: %v", err)
		}
		if got != nil {
			t.Fatalf("LoadResources() = %#v, want nil for optional missing object", got)
		}
	})

	t.Run("lists with combined selectors", func(t *testing.T) {
		t.Parallel()

		additional := labels.SelectorFromSet(labels.Set{"tenant": "team-a"})
		got, err := (tpl.ResourceReference{
			VersionKind: tplVersionKind("v1", "ConfigMap"),
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		}).LoadResources(ctx, cl, mapper, "team-a", []labels.Selector{additional}, nil, false, nil)
		if err != nil {
			t.Fatalf("LoadResources() unexpected error for list: %v", err)
		}
		if len(got) != 1 || got[0].GetName() != "settings" {
			t.Fatalf("LoadResources() = %#v, want only team-a/settings", got)
		}
	})

	t.Run("validates namespace", func(t *testing.T) {
		t.Parallel()

		_, err := (tpl.ResourceReference{
			VersionKind: tplVersionKind("v1", "ConfigMap"),
		}).LoadResources(ctx, cl, mapper, "team-a", nil, nil, false, func(ns string) error {
			if ns != "team-a" {
				t.Fatalf("validated namespace = %q", ns)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("LoadResources() unexpected validation error: %v", err)
		}
	})

	t.Run("invalid apiVersion returns error", func(t *testing.T) {
		t.Parallel()

		_, err := (tpl.ResourceReference{
			VersionKind: tplVersionKind("not/a/version", "ConfigMap"),
		}).LoadResources(ctx, cl, mapper, "team-a", nil, nil, false, nil)
		if err == nil {
			t.Fatalf("LoadResources() expected error for invalid apiVersion")
		}
	})
}

func TestTemplateContextGatherContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		configMap("team-a", "settings", map[string]string{"app": "api"}),
	).Build()

	templateContext := tpl.TemplateContext{
		Resources: []*tpl.TemplateResourceReference{{
			ResourceReference: tpl.ResourceReference{
				VersionKind: tplVersionKind("v1", "ConfigMap"),
				Name:        "settings",
				Optional:    true,
			},
			Index: "configs",
		}},
	}

	got, err := templateContext.GatherContext(ctx, cl, templateRESTMapper(), nil, "team-a", nil, nil)
	if err != nil {
		t.Fatalf("GatherContext() unexpected error: %v", err)
	}
	items := got["configs"].([]map[string]any)
	if len(items) != 1 || items[0]["metadata"].(map[string]any)["name"] != "settings" {
		t.Fatalf("GatherContext() = %#v", got)
	}

	asString, err := got.String()
	if err != nil {
		t.Fatalf("ReferenceContext.String() unexpected error: %v", err)
	}
	if !strings.Contains(asString, "settings") {
		t.Fatalf("ReferenceContext.String() = %q, want settings", asString)
	}

	empty, err := (&tpl.TemplateContext{}).GatherContext(ctx, cl, templateRESTMapper(), nil, "team-a", nil, nil)
	if err != nil {
		t.Fatalf("GatherContext(empty) unexpected error: %v", err)
	}
	if !reflect.DeepEqual(empty, tpl.ReferenceContext{}) {
		t.Fatalf("GatherContext(empty) = %#v, want empty context", empty)
	}
}

func configMap(namespace, name string, lbls map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: lbls}}
}

func templateRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)

	return mapper
}

func tplVersionKind(apiVersion, kind string) apiruntime.VersionKind {
	return apiruntime.VersionKind{APIVersion: apiVersion, Kind: kind}
}

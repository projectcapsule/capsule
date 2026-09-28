// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestMutateMetadataDefaultsAndManaged(t *testing.T) {
	t.Parallel()
	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{"default-present": "user", "managed": "user"})
	obj.Object["roleRef"] = map[string]any{
		"apiGroup": "rbac.authorization.k8s.io",
		"kind":     "ClusterRole",
		"name":     "admin",
	}
	obj.Object["subjects"] = []any{map[string]any{"kind": "User", "name": "alice"}}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Metadata: []rules.MetadataRule{{
			APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"},
			Labels: map[string]rules.MetadataValueRule{
				"default-missing": {Default: new("fallback")},
				"default-present": {Default: new("fallback")},
				"managed":         {Default: new("fallback"), Managed: new("controlled")},
			},
		}},
	}}}

	if changed := MutateMetadata(obj, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, bodies); !changed {
		t.Fatal("MutateMetadata() changed = false, want true")
	}
	if got := obj.GetLabels()["default-missing"]; got != "fallback" {
		t.Fatalf("default = %q", got)
	}
	if got := obj.GetLabels()["default-present"]; got != "user" {
		t.Fatalf("present default = %q", got)
	}
	if got := obj.GetLabels()["managed"]; got != "controlled" {
		t.Fatalf("managed = %q", got)
	}
	roleRef, ok := obj.Object["roleRef"].(map[string]any)
	if !ok || roleRef["kind"] != "ClusterRole" || roleRef["name"] != "admin" {
		t.Fatalf("roleRef was changed: %#v", obj.Object["roleRef"])
	}
	if _, ok := obj.Object["subjects"]; !ok {
		t.Fatal("subjects were removed")
	}
}

func TestMutateMetadataReportsNoop(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{"existing": "value"})

	if changed := MutateMetadata(
		obj,
		schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		nil,
	); changed {
		t.Fatal("MutateMetadata() changed = true without matching rules")
	}
}

func TestMutateMetadataAddsEmptyManagedValues(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{}
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: rules.ActionTypeAllow,
		Metadata: []rules.MetadataRule{{
			Kinds:       []string{"Namespace"},
			Labels:      map[string]rules.MetadataValueRule{"example.corp/empty": {Managed: new("")}},
			Annotations: map[string]rules.MetadataValueRule{"example.corp/empty": {Managed: new("")}},
		}},
	}}}
	if !MutateMetadata(obj, gvk, bodies) {
		t.Fatal("empty managed values were not added")
	}
	for _, metadata := range []map[string]string{obj.GetLabels(), obj.GetAnnotations()} {
		if value, present := metadata["example.corp/empty"]; !present || value != "" {
			t.Fatalf("managed metadata = %#v, want present empty value", metadata)
		}
	}
	if MutateMetadata(obj, gvk, bodies) {
		t.Fatal("unchanged empty managed values should be a no-op")
	}
}

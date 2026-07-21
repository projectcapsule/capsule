// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
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
			VersionKinds: runtime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}},
			Labels: map[string]rules.MetadataValueRule{
				"default-missing": {Default: ptr.To("fallback")},
				"default-present": {Default: ptr.To("fallback")},
				"managed":         {Default: ptr.To("fallback"), Managed: ptr.To("controlled")},
			},
		}},
	}}}

	MutateMetadata(obj, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, bodies)
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

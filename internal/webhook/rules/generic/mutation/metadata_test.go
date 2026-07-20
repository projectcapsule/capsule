// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestMutateMetadataDefaultsAndManaged(t *testing.T) {
	t.Parallel()
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{"default-present": "user", "managed": "user"},
	}}
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
}

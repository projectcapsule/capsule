// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource_test

import (
	"reflect"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestTriggerGKKey(t *testing.T) {
	a := tenantresource.TriggerGKKey(schema.GroupKind{Kind: "Secret"})
	b := tenantresource.TriggerGKKey(schema.GroupKind{Kind: "Secret"})
	c := tenantresource.TriggerGKKey(schema.GroupKind{Group: "apps", Kind: "Deployment"})

	if a != b {
		t.Fatalf("equal GroupKinds must produce equal keys: %q != %q", a, b)
	}

	if a == c {
		t.Fatalf("different GroupKinds must produce different keys, both %q", a)
	}
}

func triggerSpec(apiGroups []string, kinds ...string) capsulev1beta2.TriggerSpec {
	return capsulev1beta2.TriggerSpec{VersionKinds: capruntime.VersionKinds{APIGroups: apiGroups, Kinds: kinds}}
}

func wantKeys(gks ...schema.GroupKind) []string {
	out := make([]string, 0, len(gks))
	for _, gk := range gks {
		out = append(out, tenantresource.TriggerGKKey(gk))
	}

	sort.Strings(out)

	return out
}

func TestGlobalTriggers_Func(t *testing.T) {
	var idx tenantresource.GlobalTriggers

	if _, ok := idx.Object().(*capsulev1beta2.GlobalTenantResource); !ok {
		t.Fatalf("unexpected object type %T", idx.Object())
	}

	if idx.Field() != tenantresource.TriggersIndexerFieldName {
		t.Fatalf("unexpected field %q", idx.Field())
	}

	gtr := &capsulev1beta2.GlobalTenantResource{}
	gtr.Spec.Triggers = []capsulev1beta2.TriggerSpec{
		triggerSpec(nil, "Secret"),
		triggerSpec([]string{"v1"}, "Secret"),       // duplicate collapses to one key
		triggerSpec([]string{"apps"}, "Deployment"), // bare group keys by group+kind
		triggerSpec([]string{"apps/v1"}, "Deployment"),
	}

	got := idx.Func()(gtr)
	sort.Strings(got)

	want := wantKeys(
		schema.GroupKind{Kind: "Secret"},
		schema.GroupKind{Group: "apps", Kind: "Deployment"},
	)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestNamespacedTriggers_Func(t *testing.T) {
	var idx tenantresource.NamespacedTriggers

	if _, ok := idx.Object().(*capsulev1beta2.TenantResource); !ok {
		t.Fatalf("unexpected object type %T", idx.Object())
	}

	if idx.Field() != tenantresource.TriggersIndexerFieldName {
		t.Fatalf("unexpected field %q", idx.Field())
	}

	tr := &capsulev1beta2.TenantResource{}
	tr.Spec.Triggers = []capsulev1beta2.TriggerSpec{triggerSpec(nil, "ConfigMap")}

	got := idx.Func()(tr)
	want := wantKeys(schema.GroupKind{Kind: "ConfigMap"})

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys mismatch\n got: %v\nwant: %v", got, want)
	}
}

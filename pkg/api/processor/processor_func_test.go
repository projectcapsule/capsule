// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestObjectForProcessedItem(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, k8smeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, k8smeta.RESTScopeRoot)

	p := &Processor{Mapper: mapper}

	t.Run("keeps namespace for namespaced resource", func(t *testing.T) {
		t.Parallel()

		obj, err := p.objectForProcessedItem(meta.ObjectReferenceStatus{
			Version:   "v1",
			Kind:      "Secret",
			Namespace: "tenant-a",
			Name:      "example",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "tenant-a" {
			t.Fatalf("expected namespace tenant-a, got %q", obj.GetNamespace())
		}
	})

	t.Run("drops tracking namespace for mapped cluster scoped resource", func(t *testing.T) {
		t.Parallel()

		obj, err := p.objectForProcessedItem(meta.ObjectReferenceStatus{
			Version:   "v1",
			Kind:      "Namespace",
			Namespace: "tenant-a",
			Name:      "example",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "" {
			t.Fatalf("expected empty namespace, got %q", obj.GetNamespace())
		}
	})

	t.Run("uses status flag without mapper lookup", func(t *testing.T) {
		t.Parallel()

		obj, err := (&Processor{}).objectForProcessedItem(meta.ObjectReferenceStatus{
			Version:       "v1",
			Kind:          "UnknownClusterKind",
			Namespace:     "tenant-a",
			Name:          "example",
			ClusterScoped: true,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "" {
			t.Fatalf("expected empty namespace, got %q", obj.GetNamespace())
		}
	})
}

func TestFailAndRecord(t *testing.T) {
	t.Parallel()

	processed := meta.ProcessedItems{}
	itemErrors := 0
	item := meta.ObjectReferenceStatus{
		Version: "v1",
		Kind:    "Secret",
		Name:    "example",
		Status:  metav1.ConditionTrue,
	}

	if failAndRecord(&processed, &itemErrors, item, "prefix: ", nil) {
		t.Fatal("expected nil error to be ignored")
	}

	if itemErrors != 0 {
		t.Fatalf("expected no item errors, got %d", itemErrors)
	}

	if failAndRecord(&processed, &itemErrors, item, "prefix: ", errors.New("boom")) != true {
		t.Fatal("expected error to be recorded")
	}

	if itemErrors != 1 {
		t.Fatalf("expected one item error, got %d", itemErrors)
	}

	got := processed.GetItem(item.ResourceID)
	if got == nil {
		t.Fatal("expected processed item to be recorded")
	}

	if got.Status != metav1.ConditionFalse {
		t.Fatalf("expected status False, got %q", got.Status)
	}

	if got.Message != "prefix: boom" {
		t.Fatalf("expected message %q, got %q", "prefix: boom", got.Message)
	}
}

func TestProcessorScopeMatches(t *testing.T) {
	t.Parallel()

	scope := Scope{Tenant: "tenant-a", Namespace: "ns-a"}

	for _, tc := range []struct {
		name string
		id   gvk.ResourceID
		want bool
	}{
		{
			name: "same tenant and namespace",
			id:   resourceID("tenant-a", "ns-a", "settings"),
			want: true,
		},
		{
			name: "other namespace",
			id:   resourceID("tenant-a", "ns-b", "settings"),
			want: false,
		},
		{
			name: "other tenant",
			id:   resourceID("tenant-b", "ns-a", "settings"),
			want: false,
		},
		{
			name: "no namespace",
			id:   resourceID("tenant-a", "", "settings"),
			want: false,
		},
	} {
		if got := scope.Matches(tc.id); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestScopedAccumulator(t *testing.T) {
	t.Parallel()

	inScope := resourceID("tenant-a", "ns-a", "settings")
	otherNs := resourceID("tenant-a", "ns-b", "settings")
	otherTnt := resourceID("tenant-b", "ns-a", "settings")

	acc := Accumulator{
		inScope.GetKey(""):  {Resource: inScope},
		otherNs.GetKey(""):  {Resource: otherNs},
		otherTnt.GetKey(""): {Resource: otherTnt},
		"empty":             nil,
	}

	scoped, ignored := scopedAccumulator(acc, Scope{Tenant: "tenant-a", Namespace: "ns-a"})

	if len(scoped) != 1 {
		t.Fatalf("expected a single scoped item, got %d", len(scoped))
	}

	if scoped[inScope.GetKey("")] == nil {
		t.Fatal("expected the in scope item to be retained")
	}

	if ignored != 2 {
		t.Fatalf("expected 2 ignored items, got %d", ignored)
	}

	// The nil entry is dropped without being accounted as ignored.
	if _, ok := scoped["empty"]; ok {
		t.Fatal("expected the empty entry to be dropped")
	}
}

func TestReconcileNamespaceRequiresNamespace(t *testing.T) {
	t.Parallel()

	items, err := (&Processor{}).ReconcileNamespace(
		context.Background(),
		logr.Discard(),
		nil,
		nil,
		Accumulator{},
		ProcessorOptions{},
		Scope{Tenant: "tenant-a"},
	)
	if err == nil {
		t.Fatal("expected an error for a scope without a Namespace")
	}

	if items != nil {
		t.Fatalf("expected no processed item, got %+v", items)
	}
}

func TestReconcileNamespaceSeedsScopeOnly(t *testing.T) {
	t.Parallel()

	inScope := resourceID("tenant-a", "ns-a", "settings")
	otherNs := resourceID("tenant-a", "ns-b", "settings")

	current := meta.ProcessedItems{
		{ResourceID: otherNs, Created: true},
		{ResourceID: inScope, Created: true},
	}

	// An empty Accumulator reaches out to no client at all: the outcome is only made
	// of what was already tracked for the reconciled scope.
	items, err := (&Processor{}).ReconcileNamespace(
		context.Background(),
		logr.Discard(),
		nil,
		current,
		Accumulator{},
		ProcessorOptions{},
		Scope{Tenant: "tenant-a", Namespace: "ns-a"},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 || items[0].ResourceID != inScope {
		t.Fatalf("expected only the in scope item, got %+v", items)
	}

	if !items[0].Created {
		t.Fatal("expected the tracked creation flag to be carried over")
	}

	if len(current) != 2 {
		t.Fatalf("expected the given items to be left untouched, got %+v", current)
	}
}

func resourceID(tenant, namespace, name string) gvk.ResourceID {
	return gvk.ResourceID{
		Tenant:    tenant,
		Version:   "v1",
		Kind:      "ConfigMap",
		Name:      name,
		Namespace: namespace,
	}
}


func TestProcessorMigratesPositionalGeneratorOriginsWithoutRecreatingObjects(t *testing.T) {
	t.Parallel()

	p, _ := policyProcessor()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "repro-target"}}
	c := fake.NewClientBuilder().WithObjects(ns).WithReturnManagedFields().Build()
	p.GatherClient = c

	opts := ProcessorOptions{
		FieldOwnerPrefix: "2lclct9cwq6mg",
		Prune:            true,
	}

	makeObj := func(name, val string) *unstructured.Unstructured {
		obj := policyConfigMap("repro-target", name)
		obj.Object["data"] = map[string]any{"from": val}

		return obj
	}

	// Simulate an existing cluster created before the fix with positional origins 0/generator-0-{0,1,2}.
	legacyAcc := Accumulator{}
	for idx, name := range []string{"gen-item-a", "gen-item-b", "gen-item-c"} {
		obj := makeObj(name, name)
		origin := "0/generator-0-" + string(rune('0'+idx))
		id := gvk.NewResourceID(obj, "None", origin)
		AccumulatorAdd(legacyAcc, id, AccumulatorObject{
			Object: obj,
			Origin: id.TenantResourceIDWithOrigin,
			Policy: &apiruntime.ResourceReplicationPolicy{Deletion: apiruntime.ResourceDeletionPolicyRemove},
		})
	}

	processed := meta.ProcessedItems{}
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, legacyAcc, opts))
	require.Len(t, processed, 3)

	// Assign distinct UIDs so any delete+recreate would immediately replace or clear the UID.
	uidsBefore := map[string]types.UID{
		"gen-item-a": "uid-item-a",
		"gen-item-b": "uid-item-b",
		"gen-item-c": "uid-item-c",
	}
	for name, uid := range uidsBefore {
		live := makeObj(name, "")
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(live), live))
		live.SetUID(uid)
		require.NoError(t, c.Update(t.Context(), live))
	}

	// Upgrade + remove gen-item-a simultaneously: surviving items now carry occurrence-0 origin 0/generator-0-0.
	upgradedAcc := Accumulator{}
	for _, name := range []string{"gen-item-b", "gen-item-c"} {
		obj := makeObj(name, name)
		id := gvk.NewResourceID(obj, "None", "0/generator-0-0")
		AccumulatorAdd(upgradedAcc, id, AccumulatorObject{
			Object: obj,
			Origin: id.TenantResourceIDWithOrigin,
			Policy: &apiruntime.ResourceReplicationPolicy{Deletion: apiruntime.ResourceDeletionPolicyRemove},
		})
	}

	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, upgradedAcc, opts))
	require.Len(t, processed, 2)

	// gen-item-a was genuinely removed and must be deleted.
	deletedA := makeObj("gen-item-a", "")
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(deletedA), deletedA)))

	// gen-item-b and gen-item-c must keep their original UIDs and migrated field managers.
	for _, name := range []string{"gen-item-b", "gen-item-c"} {
		live := makeObj(name, "")
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(live), live))
		require.Equal(t, uidsBefore[name], live.GetUID(), "%s was recreated instead of migrated in place", name)

		item := processed.GetItem(gvk.NewResourceID(live, "None", "0/generator-0-0"))
		require.NotNil(t, item)
		require.True(t, item.Created)

		wantOwner := opts.FieldOwnerPrefix + "/" + item.FieldOwner("")
		hasMigratedOwner := false
		for _, mf := range live.GetManagedFields() {
			if mf.Manager == wantOwner {
				hasMigratedOwner = true
			}
			require.NotContains(t, mf.Manager, "0/generator-0-1/")
			require.NotContains(t, mf.Manager, "0/generator-0-2/")
		}
		require.True(t, hasMigratedOwner, "expected migrated SSA manager %q on %s", wantOwner, name)
	}
}

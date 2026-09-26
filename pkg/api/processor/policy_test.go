// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
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

func TestProcessorItemPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                                                                  string
		policy                                                                *apiruntime.ResourceReplicationPolicy
		existing, legacyAdopt, legacyForce, wantError, wantForce, wantProtect bool
	}{
		{name: "legacy settings", existing: true, legacyAdopt: true, legacyForce: true, wantForce: true},
		{name: "owner rejects adoption despite legacy setting", policy: &apiruntime.ResourceReplicationPolicy{}, existing: true, legacyAdopt: true, wantError: true},
		{name: "merge overrides legacy settings", policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge}, existing: true, legacyForce: true, wantProtect: true},
		{name: "owner creates protected resource", policy: &apiruntime.ResourceReplicationPolicy{}, wantProtect: true},
		{name: "unprotected forced merge", policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Force: true}, existing: true, wantForce: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := policyConfigMap("target", "example")
			var objects []client.Object
			if tc.existing {
				objects = append(objects, obj.DeepCopy())
			}
			p, c := policyProcessor(objects...)
			id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
			acc := Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: tc.policy})
			processed := meta.ProcessedItems{}
			err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{
				FieldOwnerPrefix: "test", Adopt: tc.legacyAdopt, Force: tc.legacyForce,
			})
			if tc.wantError {
				if err == nil || len(processed) != 1 || !strings.Contains(processed[0].Message, "cannot be adopted") || len(c.applies) != 0 {
					t.Fatalf("expected adoption rejection before apply, got %v, %#v", err, processed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(c.applies) != 1 || len(processed) != 1 {
				t.Fatalf("applies=%d, status=%#v", len(c.applies), processed)
			}
			if c.force[0] != tc.wantForce {
				t.Fatalf("force=%v, want %v", c.force[0], tc.wantForce)
			}
			protected := c.applies[0].GetLabels()[meta.ProtectedByCapsuleLabel] == meta.ValueControllerReplications
			if protected != tc.wantProtect {
				t.Fatalf("protected=%v, want %v", protected, tc.wantProtect)
			}
			if !reflect.DeepEqual(processed[0].Policy, processedPolicy(tc.policy)) {
				t.Fatalf("policy was not saved: %#v", processed[0].Policy)
			}
			if tc.policy != nil && processed[0].Policy == &tc.policy.ResourceTemplatePolicy {
				t.Fatal("status shares mutable spec policy")
			}
		})
	}
}

func TestProcessorRemovedItemPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                string
		policy              *apiruntime.ResourceTemplatePolicy
		prune, wantRetained bool
	}{
		{name: "orphan overrides pruning", policy: &apiruntime.ResourceTemplatePolicy{Deletion: apiruntime.ResourceDeletionPolicyOrphan}, prune: true, wantRetained: true},
		{name: "remove overrides legacy retention", policy: &apiruntime.ResourceTemplatePolicy{}, wantRetained: false},
		{name: "legacy retention", wantRetained: true},
		{name: "legacy removal", prune: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := policyConfigMap("target", "removed")
			owner := metav1.OwnerReference{APIVersion: "capsule.clastix.io/v1beta2", Kind: "GlobalTenantResource", Name: "parent", UID: "parent-uid"}
			obj.SetOwnerReferences([]metav1.OwnerReference{owner})
			obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications, meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications})
			if tc.policy != nil {
				obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications, meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications, meta.ProtectedByCapsuleLabel: meta.ValueControllerReplications})
			}
			p, c := policyProcessor(obj)
			processed := meta.ProcessedItems{{ResourceID: gvk.NewResourceID(obj, "tenant-a", "0/raw-0"), LastApply: metav1.Now(), Created: true, Policy: tc.policy}}
			if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, ProcessorOptions{Prune: tc.prune, Owner: &owner}); err != nil {
				t.Fatal(err)
			}
			if len(processed) != 0 {
				t.Fatalf("cleanup retained status: %#v", processed)
			}
			actual := policyConfigMap("target", "removed")
			err := c.Get(t.Context(), client.ObjectKeyFromObject(actual), actual)
			if !tc.wantRetained {
				if !apierrors.IsNotFound(err) {
					t.Fatalf("expected deletion, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(actual.GetOwnerReferences()) != 0 || actual.GetLabels()[meta.NewManagedByCapsuleLabel] != "" {
				t.Fatalf("retained lifecycle metadata: %#v", actual.Object)
			}
			if actual.GetLabels()[meta.CreatedByCapsuleLabel] != "" || actual.GetLabels()[meta.ProtectedByCapsuleLabel] != "" {
				t.Fatalf("orphan retained tracking/protection: %#v", actual.GetLabels())
			}
			if actual.Object["data"] == nil {
				t.Fatal("orphan lost applied data")
			}
		})
	}
}

func TestProcessorFailedPolicyUpdateRetainsCleanupState(t *testing.T) {
	obj := policyConfigMap("target", "example")
	p, c := policyProcessor(obj)
	c.applyError = errors.New("injected apply failure")
	id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
	old := meta.ObjectReferenceStatus{ResourceID: id, LastApply: metav1.Now(), Policy: &apiruntime.ResourceTemplatePolicy{Deletion: apiruntime.ResourceDeletionPolicyOrphan}}
	processed := meta.ProcessedItems{old}
	acc := Accumulator{}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge}})
	if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err == nil {
		t.Fatal("expected apply failure")
	}
	if len(processed) != 1 || !reflect.DeepEqual(processed[0].Policy, old.Policy) || !processed[0].LastApply.Equal(&old.LastApply) {
		t.Fatalf("failed update lost cleanup state: %#v", processed)
	}
	c.applyError = nil
	if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err != nil {
		t.Fatal(err)
	}
	if processed[0].Status != metav1.ConditionTrue || processed[0].Message != "" || !processed[0].Policy.AllowsAdoption() {
		t.Fatalf("successful retry retained failure or old policy: %#v", processed[0])
	}
}

func BenchmarkProcessorPolicy(b *testing.B) {
	for _, items := range []int{1, 100} {
		for _, explicit := range []bool{false, true} {
			b.Run(fmt.Sprintf("items=%d/policy=%t", items, explicit), func(b *testing.B) {
				p, c := policyProcessor()
				acc := Accumulator{}
				for i := range items {
					obj := policyConfigMap("target", fmt.Sprintf("item-%d", i))
					var policy *apiruntime.ResourceReplicationPolicy
					if explicit {
						policy = &apiruntime.ResourceReplicationPolicy{}
					}
					AccumulatorAdd(acc, gvk.NewResourceID(obj, "tenant-a", "0/raw"), AccumulatorObject{Object: obj, Policy: policy})
				}
				processed := meta.ProcessedItems{}
				b.ReportAllocs()
				for b.Loop() {
					c.applies = c.applies[:0]
					c.force = c.force[:0]
					if err := p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err != nil {
						b.Fatal(err)
					}
					if len(processed) != items {
						b.Fatal("missing processed items")
					}
				}
			})
		}
	}

	b.Run("metadata-failure", func(b *testing.B) {
		for _, items := range []int{1, 100} {
			b.Run(fmt.Sprintf("items=%d", items), func(b *testing.B) {
				p, c := policyProcessor()
				c.metadataError = errors.New("injected metadata failure")
				acc := Accumulator{}
				for i := range items {
					obj := policyConfigMap("target", fmt.Sprintf("item-%d", i))
					AccumulatorAdd(acc, gvk.NewResourceID(obj, "tenant-a", "0/raw"), AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}})
				}
				processed := meta.ProcessedItems{}
				b.ReportAllocs()
				for b.Loop() {
					c.applies = c.applies[:0]
					c.force = c.force[:0]
					c.metadataPatches = 0
					if err := p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err == nil {
						b.Fatal("metadata failure was ignored")
					}
					if len(processed) != items || len(c.applies) != items || c.metadataPatches != items {
						b.Fatalf("did not exercise each apply and metadata failure: applies=%d metadata=%d status=%+v", len(c.applies), c.metadataPatches, processed)
					}
				}
				b.ReportMetric(float64(items), "applies/op")
				b.ReportMetric(float64(items), "metadata-patches/op")
			})
		}
	})
}

func policyConfigMap(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"namespace": namespace, "name": name}, "data": map[string]any{"key": "value"},
	}}
}

func policyProcessor(objects ...client.Object) (*Processor, *policyRecordingClient) {
	objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target"}})
	c := &policyRecordingClient{Client: fake.NewClientBuilder().WithObjects(objects...).WithReturnManagedFields().Build()}
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	return &Processor{GatherClient: c, Mapper: mapper}, c
}

// Records SSA options; real SSA merge/conflict behavior is covered by e2e tests.
type policyRecordingClient struct {
	client.Client
	applies         []*unstructured.Unstructured
	force           []bool
	applyError      error
	metadataError   error
	metadataPatches int
}

func (c *policyRecordingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if patch.Type() != types.ApplyPatchType {
		if (&client.PatchOptions{}).ApplyOptions(opts).FieldManager != meta.ResourceControllerFieldOwnerPrefix() {
			return c.Client.Patch(ctx, obj, patch, opts...)
		}
		c.metadataPatches++
		if c.metadataError != nil {
			return c.metadataError
		}
		return c.Client.Patch(ctx, obj, patch, opts...)
	}
	options := (&client.PatchOptions{}).ApplyOptions(opts)
	c.applies = append(c.applies, obj.DeepCopyObject().(*unstructured.Unstructured))
	c.force = append(c.force, options.Force != nil && *options.Force)
	if c.applyError != nil {
		return c.applyError
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

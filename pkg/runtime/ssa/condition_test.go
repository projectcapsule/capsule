// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestConditionalApply(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, condition                               string
		existing, skip, wantError, conflict, failRead bool
		expected                                      *string
	}{
		{name: "absent false", condition: "false", skip: true},
		{name: "existing false", condition: "false", existing: true, skip: true},
		{name: "initial creation", condition: "object == null", expected: new("")},
		{name: "existing true", condition: "object != null", existing: true, expected: new("7")},
		{name: "stale context", condition: "true", existing: true, expected: new("6"), wantError: true},
		{name: "context expected absent", condition: "true", existing: true, expected: new(""), wantError: true},
		{name: "target disappeared", condition: "true", expected: new("7"), wantError: true},
		{name: "invalid condition", condition: "object..bad", wantError: true},
		{name: "evaluation error", condition: "object.data.missing == 'x'", existing: true, wantError: true},
		{name: "read denied", condition: "true", failRead: true, wantError: true},
		{name: "concurrent write", condition: "true", existing: true, conflict: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.existing {
				obj := configMap("guarded", map[string]any{"value": "old"})
				obj.SetResourceVersion("7")
				objects = append(objects, obj)
			}
			c := &conditionalClient{Client: fake.NewClientBuilder().WithObjects(objects...).Build(), conflict: tc.conflict, failRead: tc.failRead}
			manager := Manager{Conditions: compiler, Metadata: Metadata{CreatedByValue: testCreatedBy, ManagedByValue: testCreatedBy}}
			desired := configMap("guarded", map[string]any{"value": "new"})
			original := desired.DeepCopy()
			result, err := manager.Apply(t.Context(), c, desired, ApplyOptions{FieldOwner: testFieldOwner, Adopt: true, Condition: tc.condition, ExpectedResourceVersion: tc.expected})
			if (err != nil) != tc.wantError || result.Skipped != tc.skip {
				t.Fatalf("result=%+v, error=%v", result, err)
			}
			if !reflect.DeepEqual(desired, original) {
				t.Fatal("mutated caller's object")
			}
			if tc.skip || tc.wantError && !tc.conflict {
				if c.patches != 0 || c.creates != 0 {
					t.Fatal("wrote target despite skip/error")
				}
			}
			if tc.conflict && c.patches != 1 {
				t.Fatalf("retried stale payload %d times", c.patches)
			}
			if !tc.skip && !tc.wantError && tc.existing && c.applied.GetResourceVersion() != "7" {
				t.Fatal("missing resourceVersion precondition")
			}
		})
	}
}

func TestConditionalCreateRace(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	c := &conditionalClient{Client: fake.NewClientBuilder().Build(), createRace: true}
	m := Manager{Conditions: compiler, Metadata: Metadata{CreatedByValue: testCreatedBy, ManagedByValue: testCreatedBy}}
	_, err = m.Apply(t.Context(), c, configMap("guarded", nil), ApplyOptions{FieldOwner: testFieldOwner, Condition: "object == null"})
	if !apierrors.IsAlreadyExists(err) || c.patches != 0 {
		t.Fatalf("creation race was not stopped before apply: %v", err)
	}
}

func TestSkippedConditionRecoversOnlyItsOwnInitialization(t *testing.T) {
	manager := Manager{Metadata: Metadata{CreatedByValue: testCreatedBy}}
	for _, owner := range []string{testFieldOwner, "another-resource"} {
		t.Run(owner, func(t *testing.T) {
			obj := configMap("guarded", nil)
			obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy})
			now := metav1.Now()
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: owner, Operation: metav1.ManagedFieldsOperationUpdate, Time: &now}})
			result := manager.skippedResult(obj, ApplyOptions{FieldOwner: testFieldOwner})
			ours := owner == testFieldOwner
			if !result.Skipped || result.Created != ours || (result.LastApply != nil) != ours {
				t.Fatalf("incorrect initialization recovery: %+v", result)
			}
		})
	}
}

type conditionalClient struct {
	client.Client
	patches, creates               int
	conflict, failRead, createRace bool
	applied                        *unstructured.Unstructured
}

func (c *conditionalClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if c.failRead {
		return errors.New("denied")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}
func (c *conditionalClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.creates++
	if c.createRace {
		return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, obj.GetName())
	}
	return c.Client.Create(ctx, obj, opts...)
}
func (c *conditionalClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.patches++
	if patch.Type() != types.ApplyPatchType {
		return c.Client.Patch(ctx, obj, patch, opts...)
	}
	c.applied = obj.DeepCopyObject().(*unstructured.Unstructured)
	if c.conflict {
		return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), errors.New("changed"))
	}
	return c.Client.Update(ctx, obj)
}

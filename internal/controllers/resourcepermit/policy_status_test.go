// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func TestPermitPolicyStatus(t *testing.T) {
	for _, tc := range []struct {
		name, condition, failure                      string
		existing, owned, previous, legacy, wantPolicy bool
		defaultPolicy                                 bool
	}{
		{name: "default policy", defaultPolicy: true, wantPolicy: true},
		{name: "create", wantPolicy: true},
		{name: "conditional create", condition: "object == null", wantPolicy: true},
		{name: "adopt", existing: true, wantPolicy: true},
		{name: "update", existing: true, owned: true, previous: true, wantPolicy: true},
		{name: "managed skip", condition: "false", existing: true, owned: true, previous: true, wantPolicy: true},
		{name: "recover managed skip", condition: "false", existing: true, owned: true, wantPolicy: true},
		{name: "missing skip", condition: "false"},
		{name: "unmanaged skip", condition: "false", existing: true},
		{name: "evaluation failure", condition: "object.missing.field == true", existing: true, owned: true, previous: true, failure: "ConditionEvaluationFailed"},
		{name: "first apply failure", failure: "apply"},
		{name: "update failure", existing: true, owned: true, previous: true, failure: "apply"},
		{name: "first metadata failure", failure: "metadata", wantPolicy: true},
		{name: "conditional first metadata failure", condition: "true", failure: "metadata", wantPolicy: true},
		{name: "adopted metadata failure", existing: true, failure: "metadata", wantPolicy: true},
		{name: "update metadata failure", existing: true, owned: true, previous: true, failure: "metadata"},
		{name: "legacy metadata failure", existing: true, owned: true, previous: true, legacy: true, failure: "metadata"},
		{name: "skipped metadata failure", condition: "false", existing: true, owned: true, previous: true, failure: "metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conditions, err := cache.NewCELCache()
			require.NoError(t, err)
			permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a"}
			target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "target", Namespace: permit.Namespace, Data: map[string]string{"key": "old"}}
			desired := target.DeepCopy()
			desired.Data["key"] = "new"
			var objects []client.Object
			if tc.owned {
				timestamp := metav1.Now()
				target.Labels = map[string]string{meta.ResourcePermitProtectionLabel: meta.ValueTrue, meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit}
				target.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: meta.ResourcePermitFieldOwner(permit), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", Time: &timestamp, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}}
			}
			if tc.existing {
				objects = append(objects, target)
			}
			c := fake.NewClientBuilder().WithObjects(objects...).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					options := (&client.PatchOptions{}).ApplyOptions(opts)
					if (tc.failure == "apply" && patch.Type() == types.ApplyPatchType) ||
						(tc.failure == "metadata" && options.FieldManager == meta.ResourceControllerFieldOwnerPrefix()) {
						return errors.New("injected " + tc.failure + " failure")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			r := ResourcePermitReconciler{Client: c, resources: ssa.Manager{Conditions: conditions}}
			permit.Status.Active = &capsulev1beta2.ActivePeriod{}
			policy := apiruntime.ResourceTemplatePolicy{Condition: tc.condition, Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan, Force: true}
			if tc.defaultPolicy {
				policy = apiruntime.ResourceTemplatePolicy{}
			}
			permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{Resources: []apiruntime.RenderedResource{{Policy: policy, Targets: []runtime.RawExtension{{Object: desired}}}}}
			var previous *apiruntime.ResourceTemplatePolicy
			if tc.previous {
				obj, err := object(runtime.RawExtension{Object: target})
				require.NoError(t, err)
				item, err := managedResourceStatus(r.resources, obj)
				require.NoError(t, err)
				item.LastApply = metav1.Now()
				if !tc.legacy {
					item.Policy = &apiruntime.ResourceTemplatePolicy{Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyRemove}
				}
				previous = item.Policy
				permit.Status.ProcessedItems = meta.ProcessedItems{item}
			}
			previousCopy := previous.DeepCopy()
			policyCopy := policy.DeepCopy()
			err = r.reconcileItems(t.Context(), permit, c)
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, permit.Status.ProcessedItems, 1)
			item := permit.Status.ProcessedItems[0]
			if tc.wantPolicy {
				expected := policy.DeepCopy()
				expected.Condition = ""
				require.Equal(t, expected, item.Policy)
				if policy.Protect != nil {
					require.NotSame(t, policy.Protect, item.Policy.Protect, "status must not share the request's mutable policy")
					*item.Policy.Protect = true
				}
			} else {
				require.Equal(t, previousCopy, item.Policy, "failed or unowned targets must retain their previous snapshot")
			}
			require.Equal(t, policyCopy, &permit.Status.Request.Resources[0].Policy)
			require.Equal(t, previousCopy, previous)
		})
	}
}

func permitPolicyStatusFixture(t testing.TB, tenantCount, targetCount int, condition string) (*ResourcePermitReconciler, []*capsulev1beta2.ResourcePermit, client.WithWatch) {
	t.Helper()
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	var objects []client.Object
	var permits []*capsulev1beta2.ResourcePermit
	for tenant := range tenantCount {
		permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: fmt.Sprintf("tenant-%d", tenant)}
		permit.Status.Active = &capsulev1beta2.ActivePeriod{}
		permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{}
		for target := range targetCount {
			obj := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: fmt.Sprintf("target-%d", target), Namespace: permit.Namespace, Data: map[string]string{"key": permit.Namespace}}
			desired := obj.DeepCopy()
			timestamp := metav1.Now()
			obj.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: meta.ResourcePermitFieldOwner(permit), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", Time: &timestamp, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}}
			objects = append(objects, obj)
			permit.Status.Request.Resources = append(permit.Status.Request.Resources, apiruntime.RenderedResource{
				Policy:  apiruntime.ResourceTemplatePolicy{Condition: condition, Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan},
				Targets: []runtime.RawExtension{{Object: desired}},
			})
		}
		permits = append(permits, permit)
	}
	c := fake.NewClientBuilder().WithObjects(objects...).WithReturnManagedFields().Build()
	return &ResourcePermitReconciler{Client: c, resources: ssa.Manager{Conditions: conditions}}, permits, c
}

func BenchmarkPermitPolicyStatus(b *testing.B) {
	for _, tenants := range []int{1, 4} {
		for _, targets := range []int{1, 25} {
			for _, condition := range []string{"", "false"} {
				b.Run(fmt.Sprintf("tenants=%d/targets=%d/condition=%q", tenants, targets, condition), func(b *testing.B) {
					r, permits, base := permitPolicyStatusFixture(b, tenants, targets, condition)
					reads, writes := 0, 0
					c := interceptor.NewClient(base, interceptor.Funcs{
						Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							reads++
							return c.Get(ctx, key, obj, opts...)
						},
						Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
							writes++
							return c.Patch(ctx, obj, patch, opts...)
						},
					})
					r.ControllerClient = c
					for _, permit := range permits {
						require.NoError(b, r.reconcileItems(b.Context(), permit, c))
					}
					reads, writes = 0, 0
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						for _, permit := range permits {
							execution, err := r.resourceClient(b.Context(), logr.Discard(), permit, nil)
							if err != nil {
								b.Fatal(err)
							}
							if err := r.reconcileItems(b.Context(), permit, execution); err != nil {
								b.Fatal(err)
							}
							if len(permit.Status.ProcessedItems) != targets {
								b.Fatal("missing processed targets")
							}
							if _, err := json.Marshal(permit.Status.ProcessedItems); err != nil {
								b.Fatal(err)
							}
						}
					}
					b.ReportMetric(float64(reads)/float64(b.N), "reads/op")
					b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
				})
			}
		}
	}
}

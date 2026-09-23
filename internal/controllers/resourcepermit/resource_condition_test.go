// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func TestPermitConditionRetainsAppliedStatus(t *testing.T) {
	conditions, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
	target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "target", Namespace: "tenant-a", Data: map[string]string{"key": "reviewed"}}
	c := fake.NewClientBuilder().WithObjects(target).Build()
	r := ResourcePermitReconciler{Client: c, resources: ssa.Manager{Mapper: mapper, Conditions: conditions}}
	permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: target.Namespace}
	permit.Status.Active = &capsulev1beta2.ActivePeriod{}
	permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{Resources: []apiruntime.RenderedResource{{Policy: apiruntime.ResourceTemplatePolicy{Condition: "false"}, Targets: []runtime.RawExtension{{Object: target}}}}}
	obj, err := object(runtime.RawExtension{Object: target})
	if err != nil {
		t.Fatal(err)
	}
	item, err := managedResourceStatus(r.resources, obj)
	if err != nil {
		t.Fatal(err)
	}
	item.Created = true
	item.LastApply = metav1.Now()
	permit.Status.ProcessedItems = meta.ProcessedItems{item}
	if err := r.reconcileItems(t.Context(), permit, c); err != nil {
		t.Fatal(err)
	}
	actual := permit.Status.ProcessedItems[0]
	if !actual.Created || !actual.LastApply.Equal(&item.LastApply) || actual.Message != ssa.ConditionNotMet {
		t.Fatal("skip lost lifecycle state")
	}
	permit.Status.Request.Resources[0].Policy.Condition = "object.missing.field == true"
	if err := r.reconcileItems(t.Context(), permit, c); err == nil {
		t.Fatal("evaluation error was ignored")
	}
	if !permit.Status.ProcessedItems[0].Created || !permit.Status.ProcessedItems[0].LastApply.Equal(&item.LastApply) {
		t.Fatal("evaluation failure lost lifecycle state")
	}
}

func TestPermitConditionCleanupRecoversMissingApplyStatus(t *testing.T) {
	for _, hasStatus := range []bool{false, true} {
		t.Run(fmt.Sprintf("zero-status=%t", hasStatus), func(t *testing.T) {
			permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a", UID: "permit-uid"}
			target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "target", Namespace: permit.Namespace, Data: map[string]string{"key": "applied"}}
			target.Labels = map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerResourcePermit}
			target.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: meta.ResourcePermitFieldOwner(permit), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}}
			c := fake.NewClientBuilder().WithObjects(target, &corev1.Namespace{Name: permit.Namespace}).WithReturnManagedFields().Build()
			r := ResourcePermitReconciler{Client: c}
			permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{Resources: []apiruntime.RenderedResource{{Policy: apiruntime.ResourceTemplatePolicy{Condition: "false"}, Targets: []runtime.RawExtension{{Object: target.DeepCopy()}}}}}
			if hasStatus {
				obj, err := object(runtime.RawExtension{Object: target})
				require.NoError(t, err)
				item, err := managedResourceStatus(r.resources, obj)
				require.NoError(t, err)
				permit.Status.ProcessedItems = meta.ProcessedItems{item}
			}
			require.NoError(t, r.pruneItems(t.Context(), permit, c))
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(target), &corev1.ConfigMap{})))
			require.Empty(t, permit.Status.ProcessedItems)
		})
	}
}

func TestPermitConditionPreflight(t *testing.T) {
	for _, expression := range []string{"object == null", "false", "object.data.missing == true"} {
		t.Run(expression, func(t *testing.T) {
			conditions, err := cache.NewCELCache()
			require.NoError(t, err)
			sa := &corev1.ServiceAccount{Name: "runner", Namespace: "tenant-a"}
			c := fake.NewClientBuilder().WithObjects(sa).Build()
			r := ResourcePermitReconciler{Client: c, resources: ssa.Manager{Conditions: conditions}}
			target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "target", Namespace: sa.Namespace, Data: map[string]string{"key": "preview"}}
			permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: sa.Namespace}
			permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{
				Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: meta.RFC1123Name(sa.Name), Namespace: meta.RFC1123SubdomainName(sa.Namespace)},
				Resources:     []apiruntime.RenderedResource{{Policy: apiruntime.ResourceTemplatePolicy{Condition: expression}, Targets: []runtime.RawExtension{{Object: target}}}},
			}
			err = r.dryRunItems(t.Context(), permit, c)
			if expression == "object.data.missing == true" {
				require.ErrorContains(t, err, "ConditionEvaluationFailed")
			} else {
				require.NoError(t, err)
			}
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(target), &corev1.ConfigMap{})), "preflight must not persist a target")
		})
	}
}

func BenchmarkPermitConditionCleanup(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("skipped=%d", count), func(b *testing.B) {
			permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a"}
			permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{}
			var objects []client.Object
			var items meta.ProcessedItems
			for i := range count {
				target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: fmt.Sprintf("external-%d", i), Namespace: permit.Namespace}
				objects = append(objects, target)
				permit.Status.Request.Resources = append(permit.Status.Request.Resources, apiruntime.RenderedResource{Policy: apiruntime.ResourceTemplatePolicy{Condition: "false"}, Targets: []runtime.RawExtension{{Object: target}}})
				obj, err := object(runtime.RawExtension{Object: target})
				if err != nil {
					b.Fatal(err)
				}
				item, err := managedResourceStatus(ssa.Manager{}, obj)
				if err != nil {
					b.Fatal(err)
				}
				items = append(items, item)
			}
			c := fake.NewClientBuilder().WithObjects(objects...).Build()
			r := ResourcePermitReconciler{Client: c}
			b.ReportAllocs()
			for b.Loop() {
				permit.Status.ProcessedItems = append(meta.ProcessedItems(nil), items...)
				if err := r.pruneItems(b.Context(), permit, c); err != nil {
					b.Fatal(err)
				}
				if len(permit.Status.ProcessedItems) != 0 {
					b.Fatal("skipped items were not removed from tracking")
				}
			}
		})
	}
}

func TestPermitConditionDoesNotCleanUpUnappliedTargets(t *testing.T) {
	for _, deletion := range []apiruntime.ResourceDeletionPolicy{apiruntime.ResourceDeletionPolicyRemove, apiruntime.ResourceDeletionPolicyOrphan} {
		for _, expression := range []string{"false", "object.missing.field == true"} {
			t.Run(fmt.Sprintf("%s/%s", deletion, expression), func(t *testing.T) {
				conditions, err := cache.NewCELCache()
				require.NoError(t, err)
				mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
				mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
				target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "external", Namespace: "tenant-a", Data: map[string]string{"key": "untouched"}}
				target.Labels = map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerResourcePermit, meta.NewManagedByCapsuleLabel: meta.ValueControllerResourcePermit}
				writes := 0
				c := fake.NewClientBuilder().WithObjects(target, &corev1.Namespace{Name: target.Namespace}).WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						writes++
						return cl.Patch(ctx, obj, patch, opts...)
					},
					Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						writes++
						return cl.Delete(ctx, obj, opts...)
					},
				}).Build()
				r := ResourcePermitReconciler{Client: c, resources: ssa.Manager{Mapper: mapper, Conditions: conditions}}
				permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: target.Namespace}
				permit.Status.Active = &capsulev1beta2.ActivePeriod{}
				permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{Resources: []apiruntime.RenderedResource{{Policy: apiruntime.ResourceTemplatePolicy{Condition: expression, Deletion: deletion}, Targets: []runtime.RawExtension{{Object: target}}}}}
				err = r.reconcileItems(t.Context(), permit, c)
				if expression == "false" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "ConditionEvaluationFailed")
				}
				require.Len(t, permit.Status.ProcessedItems, 1)
				require.True(t, permit.Status.ProcessedItems[0].LastApply.IsZero())
				require.NoError(t, r.pruneItems(t.Context(), permit, c))
				require.Zero(t, writes, "skipped or failed targets must not be patched or deleted during cleanup")
				require.Empty(t, permit.Status.ProcessedItems)
				actual := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(target), actual))
				require.Equal(t, target.Data, actual.Data)
				require.Equal(t, target.Labels, actual.Labels)
			})
		}
	}
}

func TestPermitConditionCleanupAfterStatusRoundTrip(t *testing.T) {
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
	permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a", UID: "permit-uid"}
	permit.Status.Active = &capsulev1beta2.ActivePeriod{}
	permit.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{}
	var existing []client.Object
	existing = append(existing, &corev1.Namespace{Name: permit.Namespace})
	for _, tc := range []struct {
		name, condition string
		exists          bool
		orphan          bool
	}{
		{"conditional-target", "object == null", false, false},
		{"existing-remove", "object == null", true, false},
		{"skipped-target", "false", false, true},
		{"existing-orphan", "false", true, true},
		{"unconditional-target", "", false, false},
	} {
		target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: tc.name, Data: map[string]string{"value": "rendered"}}
		policy := apiruntime.ResourceTemplatePolicy{Condition: tc.condition}
		if tc.orphan {
			policy.Deletion = apiruntime.ResourceDeletionPolicyOrphan
		}
		permit.Status.Request.Resources = append(permit.Status.Request.Resources, apiruntime.RenderedResource{Policy: policy, Targets: []runtime.RawExtension{{Object: target}}})
		if tc.exists {
			other := target.DeepCopy()
			other.Namespace = permit.Namespace
			other.Data["value"] = "external"
			existing = append(existing, other)
		}
	}
	c := fake.NewClientBuilder().WithObjects(existing...).WithReturnManagedFields().Build()
	r := ResourcePermitReconciler{Client: c, resources: ssa.Manager{Mapper: mapper, Conditions: conditions}}
	require.NoError(t, r.reconcileItems(t.Context(), permit, c))
	require.Len(t, permit.Status.ProcessedItems, 5)
	// Controllers read RawExtensions from the API rather than retaining Object.
	raw, err := json.Marshal(permit)
	require.NoError(t, err)
	stored := &capsulev1beta2.ResourcePermit{}
	require.NoError(t, json.Unmarshal(raw, stored))
	for range 2 {
		require.NoError(t, r.pruneItems(t.Context(), stored, c))
		require.Empty(t, stored.Status.ProcessedItems)
	}
	for _, name := range []string{"conditional-target", "unconditional-target", "skipped-target"} {
		require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKey{Namespace: permit.Namespace, Name: name}, &corev1.ConfigMap{})))
	}
	for _, name := range []string{"existing-remove", "existing-orphan"} {
		actual := &corev1.ConfigMap{}
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: permit.Namespace, Name: name}, actual))
		require.Equal(t, map[string]string{"value": "external"}, actual.Data)
	}
}

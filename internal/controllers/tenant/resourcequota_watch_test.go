// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestResourceQuotaWatchSyncsOnlyTheOwnerTenantQuotas(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	controller := true
	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a",
			Annotations: map[string]string{
				capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10",
			},
		},
		Spec: capsulev1beta2.TenantSpec{ResourceQuota: api.ResourceQuotaSpec{
			Scope: api.ResourceQuotaScopeNamespace,
			Items: []corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			}}},
		}},
		Status: capsulev1beta2.TenantStatus{Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{Name: "team-a"}}},
	}
	trigger := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{
		Name:      "capsule-tenant-a-0",
		Namespace: "team-a",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: capsulev1beta2.GroupVersion.String(),
			Kind:       "Tenant",
			Name:       tenant.Name,
			Controller: &controller,
		}},
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant, trigger).Build()
	gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr: "WidgetList",
	})
	dynamicClient.PrependReactor("list", "widgets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{
			Object: map[string]any{"metadata": map[string]any{"name": "widget"}},
		}}}, nil
	})
	manager := &Manager{Client: cl, DynamicClient: dynamicClient, Metrics: metrics.NewTenantRecorder()}

	manager.syncResourceQuotasForResourceQuota(context.Background(), trigger)

	updated := &corev1.ResourceQuota{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(trigger), updated); err != nil {
		t.Fatalf("get synchronized ResourceQuota: %v", err)
	}
	if got := updated.Spec.Hard[corev1.ResourceLimitsCPU]; got.Cmp(resource.MustParse("1")) != 0 {
		t.Fatalf("CPU hard quota = %s, want 1", got.String())
	}
	if updated.Labels[meta.NewTenantLabel] != tenant.Name {
		t.Fatalf("tenant label = %q, want %q", updated.Labels[meta.NewTenantLabel], tenant.Name)
	}

	updatedTenant := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(tenant), updatedTenant); err != nil {
		t.Fatalf("get synchronized Tenant: %v", err)
	}
	if got := updatedTenant.Annotations[capsulev1beta2.UsedAnnotationForResource("widgets.example.com_v1")]; got != "1" {
		t.Fatalf("custom ResourceQuota usage annotation = %q, want 1", got)
	}
}

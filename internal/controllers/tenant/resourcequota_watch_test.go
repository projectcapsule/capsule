// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestResourceQuotaReconcileSyncsTenantQuotas(t *testing.T) {
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
	manager := &Manager{Client: cl, reader: cl, DynamicClient: dynamicClient, Metrics: metrics.NewTenantRecorder()}

	if _, err := manager.reconcileResourceQuotas(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: tenant.Name},
	}); err != nil {
		t.Fatalf("reconcile ResourceQuotas: %v", err)
	}

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

func TestResourceQuotaInitialEventsCoalesceByOwnerTenant(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{capsulev1beta2.GroupVersion})
	mapper.Add(capsulev1beta2.GroupVersion.WithKind("Tenant"), k8smeta.RESTScopeRoot)

	h := resourceQuotaEventHandler(scheme, mapper)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()

	controller := true
	for i := 0; i < 100; i++ {
		quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{
			Name:      "quota",
			Namespace: "team-a",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: capsulev1beta2.GroupVersion.String(),
				Kind:       "Tenant",
				Name:       "tenant-a",
				Controller: &controller,
			}},
		}}

		h.Create(context.Background(), event.CreateEvent{Object: quota, IsInInitialList: true}, queue)
	}

	if got := queue.Len(); got != 1 {
		t.Fatalf("queued requests = %d, want 1", got)
	}
}

type namespaceGoneClient struct {
	client.Client
}

func (c *namespaceGoneClient) Create(
	ctx context.Context,
	obj client.Object,
	opts ...client.CreateOption,
) error {
	if quota, ok := obj.(*corev1.ResourceQuota); ok {
		return apierrors.NewNotFound(corev1.Resource("namespaces"), quota.Namespace)
	}

	return c.Client.Create(ctx, obj, opts...)
}

func TestResourceQuotaReconcileSkipsMissingStatusNamespace(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: "tenant-a"},
		Spec: capsulev1beta2.TenantSpec{ResourceQuota: api.ResourceQuotaSpec{
			Scope: api.ResourceQuotaScopeNamespace,
			Items: []corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			}}},
		}},
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"gone"},
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{
				Name: "gone",
			}},
		},
	}

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant).Build()
	cl := &namespaceGoneClient{Client: base}
	manager := &Manager{Client: cl, reader: cl, Metrics: metrics.NewTenantRecorder()}

	if _, err := manager.reconcileResourceQuotas(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: tenant.Name},
	}); err != nil {
		t.Fatalf("reconcile ResourceQuotas with missing namespace: %v", err)
	}
}

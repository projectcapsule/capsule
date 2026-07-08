// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestSyncCustomResourceQuotaUsagesCountsReadyNamespacesOnly(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("cannot add capsule scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a",
			Annotations: map[string]string{
				capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10",
			},
		},
		Status: capsulev1beta2.TenantStatus{
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{
				{
					Name: "ready",
					Conditions: meta.ConditionList{{
						Type:   meta.ReadyCondition,
						Status: metav1.ConditionTrue,
					}},
				},
				{
					Name: "terminating",
					Conditions: meta.ConditionList{{
						Type:   meta.TerminatingCondition,
						Status: metav1.ConditionTrue,
					}},
				},
			},
		},
	}

	gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr: "WidgetList",
	})
	var selectors []string
	dynamicClient.PrependReactor("list", "widgets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		listAction := action.(k8stesting.ListAction)
		selectors = append(selectors, listAction.GetListRestrictions().Fields.String())

		active := unstructured.Unstructured{}
		active.SetName("active")

		deleting := unstructured.Unstructured{}
		deleting.SetName("deleting")
		now := metav1.Now()
		deleting.SetDeletionTimestamp(&now)

		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{active, deleting}}, nil
	})

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	manager := &Manager{
		Client:        kubeClient,
		DynamicClient: dynamicClient,
	}

	if err := manager.syncCustomResourceQuotaUsages(ctx, tnt); err != nil {
		t.Fatalf("syncCustomResourceQuotaUsages() unexpected error: %v", err)
	}

	if len(selectors) != 1 {
		t.Fatalf("expected one resource list call, got %d: %v", len(selectors), selectors)
	}

	if selectors[0] != "metadata.namespace=ready" {
		t.Fatalf("unexpected field selector %q", selectors[0])
	}

	updated := &capsulev1beta2.Tenant{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: tnt.Name}, updated); err != nil {
		t.Fatalf("cannot get updated tenant: %v", err)
	}

	if got := updated.Annotations[capsulev1beta2.UsedAnnotationForResource("widgets.example.com_v1")]; got != "1" {
		t.Fatalf("used annotation = %q, want 1", got)
	}
}

func TestSyncCustomResourceQuotaUsagesIgnoresMissingResource(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("cannot add capsule scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a",
			Annotations: map[string]string{
				capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10",
			},
		},
		Status: capsulev1beta2.TenantStatus{
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{
				Name: "ready",
				Conditions: meta.ConditionList{{
					Type:   meta.ReadyCondition,
					Status: metav1.ConditionTrue,
				}},
			}},
		},
	}

	gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr: "WidgetList",
	})
	dynamicClient.PrependReactor("list", "widgets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "example.com", Resource: "widgets"}, "")
	})

	manager := &Manager{
		Client:        fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build(),
		DynamicClient: dynamicClient,
	}

	if err := manager.syncCustomResourceQuotaUsages(ctx, tnt); err != nil {
		t.Fatalf("syncCustomResourceQuotaUsages() unexpected error: %v", err)
	}
}

func TestSyncCustomResourceQuotaUsagesSkipsTerminatingTenant(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "tenant-a",
			DeletionTimestamp: &metav1.Time{},
		},
	}

	manager := &Manager{}

	if err := manager.syncCustomResourceQuotaUsages(context.Background(), tnt); err != nil {
		t.Fatalf("syncCustomResourceQuotaUsages() unexpected error: %v", err)
	}
}

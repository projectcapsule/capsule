// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type staleResourceQuotaClient struct {
	client.Client
	stale   *corev1.ResourceQuota
	updates atomic.Int32
}

func (c *staleResourceQuotaClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if quota, ok := obj.(*corev1.ResourceQuota); ok {
		c.stale.DeepCopyInto(quota)

		return nil
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *staleResourceQuotaClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	quota, ok := obj.(*corev1.ResourceQuota)
	if ok && (quota.ResourceVersion == c.stale.ResourceVersion || c.updates.Add(1) == 1) {
		return apierrors.NewConflict(
			schema.GroupResource{Resource: "resourcequotas"},
			quota.Name,
			errors.New("concurrent status update"),
		)
	}

	return c.Client.Update(ctx, obj, opts...)
}

func TestResourceQuotasUpdateRefetchesFromAPIReaderAfterConflict(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	current := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "capsule-tenant-a-0",
			Namespace:       "tenant-a-ns",
			ResourceVersion: "2",
			Labels:          map[string]string{"tenant": "tenant-a"},
		},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceLimitsCPU: resource.MustParse("3"),
		}},
	}
	stale := current.DeepCopy()
	stale.ResourceVersion = "1"

	apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	cachedClient := &staleResourceQuotaClient{Client: apiReader, stale: stale}
	manager := &Manager{Client: cachedClient, reader: apiReader}

	desired := current.DeepCopy()
	desired.Spec.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("5")
	actual := resource.MustParse("2")
	limit := resource.MustParse("5")

	err := manager.resourceQuotasUpdate(
		context.Background(),
		corev1.ResourceLimitsCPU,
		actual,
		sets.New(corev1.ResourceLimitsCPU),
		limit,
		*desired,
	)
	if err != nil {
		t.Fatalf("resourceQuotasUpdate() unexpected error: %v", err)
	}
	if cachedClient.updates.Load() < 2 {
		t.Fatalf("updates = %d, want a conflict followed by a retry", cachedClient.updates.Load())
	}

	updated := &corev1.ResourceQuota{}
	if err := apiReader.Get(context.Background(), types.NamespacedName{
		Namespace: current.Namespace,
		Name:      current.Name,
	}, updated); err != nil {
		t.Fatalf("get updated ResourceQuota: %v", err)
	}
	if got := updated.Spec.Hard[corev1.ResourceLimitsCPU]; got.Cmp(resource.MustParse("5")) != 0 {
		t.Fatalf("hard CPU = %s, want 5", got.String())
	}

	usedKey, err := capsulev1beta2.UsedQuotaFor(corev1.ResourceLimitsCPU)
	if err != nil {
		t.Fatalf("build used quota annotation: %v", err)
	}
	if got := updated.Annotations[usedKey]; got != "2" {
		t.Fatalf("used quota annotation = %q, want 2", got)
	}
}

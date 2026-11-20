// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// A ResourceQuota listed at the beginning of the sync can be deleted before it is
// updated (typically because its Namespace is being torn down by a CI pipeline).
// The sync must skip it instead of failing, which would requeue the whole Tenant.
func TestResourceQuotasUpdateSkipsVanishedResourceQuota(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	surviving := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "capsule-tenant-a-0",
			Namespace:       "tenant-a-alive",
			ResourceVersion: "1",
			Labels:          map[string]string{"tenant": "tenant-a"},
		},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceLimitsCPU: resource.MustParse("3"),
		}},
	}
	// Part of the listed items, but gone from the API server by the time it is read back.
	vanished := surviving.DeepCopy()
	vanished.Namespace = "tenant-a-deleted"

	apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(surviving).Build()
	manager := &Manager{Client: apiReader, reader: apiReader}

	desiredVanished := vanished.DeepCopy()
	desiredVanished.Spec.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("5")
	desiredSurviving := surviving.DeepCopy()
	desiredSurviving.Spec.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("5")

	err := manager.resourceQuotasUpdate(
		context.Background(),
		corev1.ResourceLimitsCPU,
		resource.MustParse("2"),
		sets.New(corev1.ResourceLimitsCPU),
		resource.MustParse("5"),
		*desiredVanished,
		*desiredSurviving,
	)
	if err != nil {
		t.Fatalf("resourceQuotasUpdate() must not fail because of a vanished ResourceQuota, got: %v", err)
	}

	updated := &corev1.ResourceQuota{}
	if err := apiReader.Get(context.Background(), types.NamespacedName{
		Namespace: surviving.Namespace,
		Name:      surviving.Name,
	}, updated); err != nil {
		t.Fatalf("get surviving ResourceQuota: %v", err)
	}
	if got := updated.Spec.Hard[corev1.ResourceLimitsCPU]; got.Cmp(resource.MustParse("5")) != 0 {
		t.Fatalf("hard CPU of the surviving ResourceQuota = %s, want 5", got.String())
	}

	usedKey, err := capsulev1beta2.UsedQuotaFor(corev1.ResourceLimitsCPU)
	if err != nil {
		t.Fatalf("build used quota annotation: %v", err)
	}
	if got := updated.Annotations[usedKey]; got != "2" {
		t.Fatalf("used quota annotation of the surviving ResourceQuota = %q, want 2", got)
	}
}

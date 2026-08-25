// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestSyncResourceQuotasPrunesRemovedItemsFromUnreadyNamespaces(t *testing.T) {
	t.Parallel()

	tenant := quotaCleanupTenant([]corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("1"),
	}}})
	tenant.Spec.ResourceQuota.Scope = api.ResourceQuotaScopeNamespace
	tenant.Status.Spaces[0].Conditions = capsulemeta.ConditionList{{
		Type:   capsulemeta.ReadyCondition,
		Status: metav1.ConditionFalse,
	}}

	keep := managedResourceQuota(tenant.Name, "team-a", "0", corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("1"),
	})
	remove := managedResourceQuota(tenant.Name, "team-a", "1", corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
	})
	manager := quotaCleanupManager(t, tenant, keep, remove)
	manager.Metrics.TenantResourceUsageGauge.WithLabelValues(tenant.Name, corev1.ResourceLimitsMemory.String(), "1").Set(1)
	manager.Metrics.TenantResourceLimitGauge.WithLabelValues(tenant.Name, corev1.ResourceLimitsMemory.String(), "1").Set(1)

	if err := manager.syncResourceQuotas(context.Background(), logr.Discard(), tenant); err != nil {
		t.Fatalf("syncResourceQuotas() unexpected error: %v", err)
	}

	deleted := &corev1.ResourceQuota{}
	err := manager.Get(context.Background(), client.ObjectKeyFromObject(remove), deleted)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("removed ResourceQuota lookup error = %v, want NotFound", err)
	}
	assertQuotaMetricAbsent(t, manager.Metrics, tenant.Name, corev1.ResourceLimitsMemory, "1")
}

func TestSyncResourceQuotasUsesLatestTenantSpecForCleanup(t *testing.T) {
	t.Parallel()

	latest := quotaCleanupTenant([]corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("1"),
	}}})
	latest.Spec.ResourceQuota.Scope = api.ResourceQuotaScopeNamespace

	stale := latest.DeepCopy()
	stale.Spec.ResourceQuota.Items = append(stale.Spec.ResourceQuota.Items, corev1.ResourceQuotaSpec{
		Hard: corev1.ResourceList{corev1.ResourceLimitsMemory: resource.MustParse("1Gi")},
	})

	keep := managedResourceQuota(latest.Name, "team-a", "0", corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("1"),
	})
	remove := managedResourceQuota(latest.Name, "team-a", "1", corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
	})
	manager := quotaCleanupManager(t, latest, keep, remove)
	manager.Metrics.TenantResourceUsageGauge.WithLabelValues(latest.Name, corev1.ResourceLimitsMemory.String(), "1").Set(1)
	manager.Metrics.TenantResourceLimitGauge.WithLabelValues(latest.Name, corev1.ResourceLimitsMemory.String(), "1").Set(1)

	if err := manager.syncResourceQuotas(context.Background(), logr.Discard(), stale); err != nil {
		t.Fatalf("syncResourceQuotas() unexpected error: %v", err)
	}

	deleted := &corev1.ResourceQuota{}
	err := manager.Get(context.Background(), client.ObjectKeyFromObject(remove), deleted)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("ResourceQuota from stale Tenant spec lookup error = %v, want NotFound", err)
	}
	assertQuotaMetricAbsent(t, manager.Metrics, latest.Name, corev1.ResourceLimitsMemory, "1")
}

func TestSyncResourceQuotasPrunesRemovedHardResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		desiredHard corev1.ResourceList
	}{
		{
			name: "remaining resource is at its limit",
			desiredHard: corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			},
		},
		{
			name:        "hard map is empty",
			desiredHard: corev1.ResourceList{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tenant := quotaCleanupTenant([]corev1.ResourceQuotaSpec{{Hard: tt.desiredHard}})
			quota := managedResourceQuota(tenant.Name, "team-a", "0", corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("1"),
				corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
			})
			quota.Status.Used = corev1.ResourceList{
				corev1.ResourceLimitsCPU: resource.MustParse("1"),
			}
			usedMemory, err := capsulev1beta2.UsedQuotaFor(corev1.ResourceLimitsMemory)
			if err != nil {
				t.Fatalf("build used-memory annotation: %v", err)
			}
			hardMemory, err := capsulev1beta2.HardQuotaFor(corev1.ResourceLimitsMemory)
			if err != nil {
				t.Fatalf("build hard-memory annotation: %v", err)
			}
			quota.Annotations = map[string]string{usedMemory: "1Gi", hardMemory: "1Gi"}

			manager := quotaCleanupManager(t, tenant, quota)
			manager.Metrics.TenantResourceUsageGauge.WithLabelValues(tenant.Name, corev1.ResourceLimitsMemory.String(), "0").Set(1)
			manager.Metrics.TenantResourceLimitGauge.WithLabelValues(tenant.Name, corev1.ResourceLimitsMemory.String(), "0").Set(1)

			if err := manager.syncResourceQuotas(context.Background(), logr.Discard(), tenant); err != nil {
				t.Fatalf("syncResourceQuotas() unexpected error: %v", err)
			}

			updated := &corev1.ResourceQuota{}
			if err := manager.Get(context.Background(), client.ObjectKeyFromObject(quota), updated); err != nil {
				t.Fatalf("get updated ResourceQuota: %v", err)
			}
			if _, ok := updated.Spec.Hard[corev1.ResourceLimitsMemory]; ok {
				t.Fatalf("removed memory hard quota is still present: %#v", updated.Spec.Hard)
			}
			if _, ok := updated.Annotations[usedMemory]; ok {
				t.Fatalf("removed memory usage annotation is still present: %#v", updated.Annotations)
			}
			if _, ok := updated.Annotations[hardMemory]; ok {
				t.Fatalf("removed memory hard annotation is still present: %#v", updated.Annotations)
			}
			assertQuotaMetricAbsent(t, manager.Metrics, tenant.Name, corev1.ResourceLimitsMemory, "0")
		})
	}
}

func quotaCleanupTenant(items []corev1.ResourceQuotaSpec) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		Spec: capsulev1beta2.TenantSpec{ResourceQuota: api.ResourceQuotaSpec{
			Scope: api.ResourceQuotaScopeTenant,
			Items: items,
		}},
		Status: capsulev1beta2.TenantStatus{
			Size: 1,
			Spaces: []*capsulev1beta2.TenantStatusNamespaceItem{{
				Name: "team-a",
			}},
		},
	}
}

func managedResourceQuota(
	tenant string,
	namespace string,
	index string,
	hard corev1.ResourceList,
) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "capsule-" + tenant + "-" + index,
			Namespace: namespace,
			Labels: map[string]string{
				capsulemeta.NewTenantLabel:           tenant,
				capsulemeta.NewManagedByCapsuleLabel: capsulemeta.ValueController,
				capsulemeta.ResourceQuotaLabel:       index,
			},
		},
		Spec: corev1.ResourceQuotaSpec{Hard: hard},
	}
}

func quotaCleanupManager(t *testing.T, objects ...client.Object) *Manager {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	return &Manager{
		Client:  cl,
		reader:  cl,
		Metrics: metrics.NewTenantRecorder(),
		Log:     logr.Discard(),
	}
}

func assertQuotaMetricAbsent(
	t *testing.T,
	recorder *metrics.TenantRecorder,
	tenant string,
	resourceName corev1.ResourceName,
	index string,
) {
	t.Helper()

	if recorder.TenantResourceUsageGauge.DeleteLabelValues(tenant, resourceName.String(), index) {
		t.Fatalf("stale usage metric still exists for %s quota %s", resourceName, index)
	}
	if recorder.TenantResourceLimitGauge.DeleteLabelValues(tenant, resourceName.String(), index) {
		t.Fatalf("stale limit metric still exists for %s quota %s", resourceName, index)
	}
}

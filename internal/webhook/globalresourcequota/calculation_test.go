// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequota

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestReserveIsAtomicAcrossResources(t *testing.T) {
	t.Parallel()

	key := types.NamespacedName{Namespace: "capsule-system", Name: "rule-quota"}
	hard := corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("8"),
		corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
	}
	quota := globalQuotaForTest("atomic", hard)
	ledger := initializedLedger(key, quota, corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("7"),
		corev1.ResourceRequestsMemory: resource.MustParse("10Gi"),
	})
	cl := ledgerClient(t, ledger)

	denied := reservationForTest("denied", corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("7Gi"),
	})
	allowed, _, applied, err := reserve(context.Background(), cl, cl, key, quota, denied, false)
	if err != nil {
		t.Fatalf("reserve(denied) error = %v", err)
	}
	if allowed || applied {
		t.Fatalf("reserve(denied) = allowed %v, applied %v; want false, false", allowed, applied)
	}

	current := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(context.Background(), key, current); err != nil {
		t.Fatal(err)
	}
	if len(current.Status.ResourceQuota.Reservations) != 0 {
		t.Fatalf("denied reservation was persisted: %#v", current.Status.ResourceQuota.Reservations)
	}

	accepted := reservationForTest("accepted", corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("6Gi"),
	})
	allowed, _, applied, err = reserve(context.Background(), cl, cl, key, quota, accepted, false)
	if err != nil {
		t.Fatalf("reserve(accepted) error = %v", err)
	}
	if !allowed || !applied {
		t.Fatalf("reserve(accepted) = allowed %v, applied %v; want true, true", allowed, applied)
	}
}

func TestReserveReportsUpdatedReservationForRollback(t *testing.T) {
	t.Parallel()

	key := types.NamespacedName{Namespace: "capsule-system", Name: "updated"}
	hard := corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("10")}
	quota := globalQuotaForTest("updated", hard)
	ledger := initializedLedger(key, quota, zeroResourceList(hard))
	existing := reservationForTest("same-admission", corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("1"),
	})
	ledger.Status.ResourceQuota.Reservations = []capsulev1beta2.QuantityLedgerResourceQuotaReservation{existing}
	ledger.Status.ResourceQuota.Reserved = existing.Delta.DeepCopy()
	ledger.Status.ResourceQuota.Allocated = existing.Delta.DeepCopy()
	cl := ledgerClient(t, ledger)

	updated := reservationForTest("same-admission", corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("2"),
	})
	allowed, _, applied, err := reserve(context.Background(), cl, cl, key, quota, updated, false)
	if err != nil {
		t.Fatalf("reserve(updated) error = %v", err)
	}
	if !allowed || !applied {
		t.Fatalf("reserve(updated) = allowed %v, applied %v; want true, true", allowed, applied)
	}
	persisted := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(context.Background(), key, persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Status.ResourceQuota.Reservations) != 1 {
		t.Fatalf("stored reservations = %d, want 1", len(persisted.Status.ResourceQuota.Reservations))
	}
	assertLedgerQuantity(
		t,
		persisted.Status.ResourceQuota.Reservations[0].Delta,
		corev1.ResourceRequestsCPU,
		"2",
	)

	if err := rollbackReservation(context.Background(), cl, cl, key, updated.ID); err != nil {
		t.Fatalf("rollbackReservation(updated) error = %v", err)
	}

	current := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(context.Background(), key, current); err != nil {
		t.Fatal(err)
	}
	if len(current.Status.ResourceQuota.Reservations) != 0 {
		t.Fatalf("updated reservation was not rolled back: %#v", current.Status.ResourceQuota.Reservations)
	}
	assertLedgerQuantity(t, current.Status.ResourceQuota.Reserved, corev1.ResourceRequestsCPU, "0")
	assertLedgerQuantity(t, current.Status.ResourceQuota.Allocated, corev1.ResourceRequestsCPU, "0")
}

func TestConcurrentReservationsCannotOversubscribe(t *testing.T) {
	t.Parallel()

	key := types.NamespacedName{Namespace: "capsule-system", Name: "concurrent"}
	hard := corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("10")}
	quota := globalQuotaForTest("concurrent", hard)
	cl := ledgerClient(t, initializedLedger(key, quota, zeroResourceList(hard)))

	var allowed atomic.Int32
	errs := make(chan error, 20)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			reservation := reservationForTest(
				fmt.Sprintf("request-%d", index),
				corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")},
			)
			ok, _, _, err := reserve(context.Background(), cl, cl, key, quota, reservation, false)
			if err != nil {
				errs <- err

				return
			}
			if ok {
				allowed.Add(1)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent reserve error = %v", err)
	}

	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed reservations = %d, want 10", got)
	}

	current := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(context.Background(), key, current); err != nil {
		t.Fatal(err)
	}
	if len(current.Status.ResourceQuota.Reservations) != 10 {
		t.Fatalf("stored reservations = %d, want 10", len(current.Status.ResourceQuota.Reservations))
	}
	assertLedgerQuantity(t, current.Status.ResourceQuota.Allocated, corev1.ResourceRequestsCPU, "10")
}

func TestManagedResourceQuotaIsExcludedFromAccounting(t *testing.T) {
	t.Parallel()

	req := admissionRequest("resourcequotas")
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{
			meta.NewManagedByCapsuleLabel: meta.ValueController,
			meta.GlobalResourceQuotaLabel: "shared",
		},
	}}

	if !isManagedResourceQuota(req, object) {
		t.Fatal("managed GlobalResourceQuota child was not excluded")
	}

	object.Labels = nil
	if isManagedResourceQuota(req, object) {
		t.Fatal("unmanaged ResourceQuota was excluded")
	}
}

func TestValidateGlobalResourceQuota(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		quota   *capsulev1beta2.GlobalResourceQuota
		wantErr bool
	}{
		{
			name:    "requires hard resources",
			quota:   globalQuotaForTest("empty", nil),
			wantErr: true,
		},
		{
			name: "rejects negative quantities",
			quota: globalQuotaForTest("negative", corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("-1"),
			}),
			wantErr: true,
		},
		{
			name: "accepts an empty selector as all namespaces",
			quota: func() *capsulev1beta2.GlobalResourceQuota {
				quota := globalQuotaForTest("valid", corev1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("8"),
				})
				quota.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
					LabelSelector: &metav1.LabelSelector{},
				}}

				return quota
			}(),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateGlobalResourceQuota(test.quota)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateGlobalResourceQuota() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateHardLimitAgainstAllocatedUsage(t *testing.T) {
	t.Parallel()

	allocated := corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("3"),
	}

	if err := validateHardLimit(corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("3"),
	}, allocated); err != nil {
		t.Fatalf("equal hard limit rejected: %v", err)
	}
	if err := validateHardLimit(corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("2"),
	}, allocated); err == nil {
		t.Fatal("hard limit below allocated usage was accepted")
	}
	if err := validateHardLimit(corev1.ResourceList{}, allocated); err == nil {
		t.Fatal("allocated resource was removed from hard limit")
	}
}

func TestRuleManagedGlobalResourceQuotaCannotBeReducedBelowUsage(t *testing.T) {
	t.Parallel()

	oldQuota := globalQuotaForTest("tenant-a-shared", corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("3"),
	})
	oldQuota.Status.Total.Used = corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("3"),
	}
	controller := true
	oldQuota.Labels = map[string]string{
		meta.NewManagedByCapsuleLabel: meta.ValueController,
		meta.RuleQuotaLabel:           "shared",
	}
	oldQuota.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: capsulev1beta2.GroupVersion.String(),
		Kind:       "Tenant",
		Name:       "tenant-a",
		UID:        types.UID("tenant-a-uid"),
		Controller: &controller,
	}}

	newQuota := oldQuota.DeepCopy()
	newQuota.Spec.Quota.Hard[corev1.ResourceRequestsCPU] = resource.MustParse("2")

	request := globalResourceQuotaUpdateRequest(t, oldQuota, newQuota)
	if response := validateGlobalResourceQuotaRequest(context.Background(), ledgerClient(t), request); response == nil || response.Allowed {
		t.Fatalf("managed rule quota decrease below usage was accepted: %#v", response)
	}
}

func TestGlobalResourceQuotaCannotChangeScopeAndReduceHardLimit(t *testing.T) {
	t.Parallel()

	oldQuota := globalQuotaForTest("shared", corev1.ResourceList{
		corev1.ResourceLimitsCPU: resource.MustParse("8"),
	})
	oldQuota.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "application"}},
	}}

	newQuota := oldQuota.DeepCopy()
	newQuota.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "wind"}},
	}}
	newQuota.Spec.Quota.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("0")

	request := globalResourceQuotaUpdateRequest(t, oldQuota, newQuota)
	response := validateGlobalResourceQuotaRequest(context.Background(), ledgerClient(t), request)
	if response == nil || response.Allowed || response.Result == nil {
		t.Fatalf("scope change with hard-limit decrease was accepted: %#v", response)
	}
	if !strings.Contains(
		response.Result.Message,
		`spec.quota.hard["limits.cpu"] cannot be reduced from 8 to 0 while namespace selectors are changing`,
	) {
		t.Fatalf("denial message = %q", response.Result.Message)
	}
}

func TestFormatExceededResources(t *testing.T) {
	t.Parallel()

	requested := corev1.ResourceList{
		corev1.ResourceLimitsCPU:      resource.MustParse("1"),
		corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
		corev1.ResourceRequestsCPU:    resource.MustParse("100m"),
		corev1.ResourceRequestsMemory: resource.MustParse("256Mi"),
	}
	projected := corev1.ResourceList{
		corev1.ResourceLimitsCPU:      resource.MustParse("9"),
		corev1.ResourceLimitsMemory:   resource.MustParse("9Gi"),
		corev1.ResourceRequestsCPU:    resource.MustParse("900m"),
		corev1.ResourceRequestsMemory: resource.MustParse("2304Mi"),
	}
	hard := corev1.ResourceList{
		corev1.ResourceLimitsCPU:      resource.MustParse("8"),
		corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
		corev1.ResourceRequestsCPU:    resource.MustParse("8"),
		corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
	}

	want := "limits.cpu (requested=1, current=8, projected=9, hard=8, exceededBy=1)"
	if got := formatExceededResources(requested, projected, hard); got != want {
		t.Fatalf("formatExceededResources() = %q, want %q", got, want)
	}
}

func TestFormatExceededResourcesSortsAndFormatsEveryExceededLimit(t *testing.T) {
	t.Parallel()

	requested := corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
		corev1.ResourceRequestsCPU:  resource.MustParse("750m"),
	}
	projected := corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("1536Mi"),
		corev1.ResourceRequestsCPU:  resource.MustParse("1500m"),
	}
	hard := corev1.ResourceList{
		corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
		corev1.ResourceRequestsCPU:  resource.MustParse("1"),
	}

	want := "limits.memory (requested=512Mi, current=1Gi, projected=1536Mi, hard=1Gi, exceededBy=512Mi); " +
		"requests.cpu (requested=750m, current=750m, projected=1500m, hard=1, exceededBy=500m)"
	if got := formatExceededResources(requested, projected, hard); got != want {
		t.Fatalf("formatExceededResources() = %q, want %q", got, want)
	}
}

func admissionRequest(resourceName string) admission.Request {
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Resource: metav1.GroupVersionResource{Group: "", Version: "v1", Resource: resourceName},
	}}
}

func initializedLedger(
	key types.NamespacedName,
	quota *capsulev1beta2.GlobalResourceQuota,
	used corev1.ResourceList,
) *capsulev1beta2.QuantityLedger {
	hard := quota.Spec.Quota.Hard

	return &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec: capsulev1beta2.QuantityLedgerSpec{
			TargetRef: capsulev1beta2.QuantityLedgerTargetRef{
				Kind: "GlobalResourceQuota",
				Name: quota.Name,
				UID:  quota.UID,
			},
		},
		Status: capsulev1beta2.QuantityLedgerStatus{
			ResourceQuota: &capsulev1beta2.QuantityLedgerResourceQuotaStatus{
				ObservedGeneration: quota.Generation,
				Initialized:        true,
				Namespaces:         []string{"tenant-a"},
				Used:               used.DeepCopy(),
				Reserved:           zeroResourceList(hard),
				Allocated:          used.DeepCopy(),
			},
		},
	}
}

func globalQuotaForTest(name string, hard corev1.ResourceList) *capsulev1beta2.GlobalResourceQuota {
	return &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid"), Generation: 1},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			Quota: corev1.ResourceQuotaSpec{Hard: hard.DeepCopy()},
		},
	}
}

func globalResourceQuotaUpdateRequest(
	t *testing.T,
	oldQuota *capsulev1beta2.GlobalResourceQuota,
	newQuota *capsulev1beta2.GlobalResourceQuota,
) admission.Request {
	t.Helper()

	oldRaw, err := json.Marshal(oldQuota)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, err := json.Marshal(newQuota)
	if err != nil {
		t.Fatal(err)
	}

	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: newRaw},
		OldObject: runtime.RawExtension{Raw: oldRaw},
	}}
}

func reservationForTest(
	id string,
	delta corev1.ResourceList,
) capsulev1beta2.QuantityLedgerResourceQuotaReservation {
	now := metav1.Now()

	return capsulev1beta2.QuantityLedgerResourceQuotaReservation{
		ID:    id,
		Usage: delta.DeepCopy(),
		Delta: delta.DeepCopy(),
		ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{
			APIVersion: "v1",
			Kind:       "Pod",
			Namespace:  "tenant-a",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func ledgerClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(objects...).
		Build()
}

func assertLedgerQuantity(
	t *testing.T,
	list corev1.ResourceList,
	name corev1.ResourceName,
	want string,
) {
	t.Helper()

	got := list[name]
	if got.Cmp(resource.MustParse(want)) != 0 {
		t.Fatalf("%s = %s, want %s", name, got.String(), want)
	}
}

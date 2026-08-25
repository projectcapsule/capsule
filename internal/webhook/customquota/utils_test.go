// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"context"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	caprunt "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestReserveCreateOnLedgerDryRunDoesNotMutate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant-a", Name: "pods"}
	ledger := ledgerForTest(key, "2")
	cl := ledgerClientForTest(t, ledger)

	usage := resource.MustParse("1")
	reservation := capsulev1beta2.QuantityLedgerReservation{
		ID:    "dry-run",
		Usage: usage.DeepCopy(),
		Delta: quantityPtr(usage),
	}

	allowed, effective, _, err := reserveCreateOnLedger(
		ctx,
		cl,
		cl,
		evaluatedQuota{MatchedQuota: quota.MatchedQuota{
			Name:      key.Name,
			Namespace: key.Namespace,
			Limit:     resource.MustParse("3"),
		}},
		&reservation,
		true,
	)
	if err != nil {
		t.Fatalf("reserveCreateOnLedger() error = %v", err)
	}
	if !allowed {
		t.Fatal("reserveCreateOnLedger() dry-run unexpectedly denied")
	}
	if effective.Cmp(resource.MustParse("3")) != 0 {
		t.Fatalf("effective allocation = %s, want 3", effective.String())
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if got.Status.Allocated.Cmp(resource.MustParse("2")) != 0 {
		t.Fatalf("persisted allocation = %s, want 2", got.Status.Allocated.String())
	}
	if len(got.Status.Reservations) != 0 {
		t.Fatalf("persisted reservations = %d, want 0", len(got.Status.Reservations))
	}
}

func TestReserveCreateOnLedgerReleasesExpiredReservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant-a", Name: "pods"}
	ledger := ledgerForTest(key, "2")
	expiredAt := metav1.NewTime(time.Now().Add(-time.Minute))
	expiredUsage := resource.MustParse("1")
	ledger.Status.Reserved = expiredUsage.DeepCopy()
	ledger.Status.Reservations = []capsulev1beta2.QuantityLedgerReservation{
		{
			ID:        "expired",
			Usage:     expiredUsage.DeepCopy(),
			Delta:     quantityPtr(expiredUsage),
			ExpiresAt: &expiredAt,
		},
	}
	cl := ledgerClientForTest(t, ledger)

	usage := resource.MustParse("1")
	reservation := capsulev1beta2.QuantityLedgerReservation{
		ID:    "current",
		Usage: usage.DeepCopy(),
		Delta: quantityPtr(usage),
	}

	allowed, effective, _, err := reserveCreateOnLedger(
		ctx,
		cl,
		cl,
		evaluatedQuota{MatchedQuota: quota.MatchedQuota{
			Name:      key.Name,
			Namespace: key.Namespace,
			Limit:     resource.MustParse("2"),
		}},
		&reservation,
		false,
	)
	if err != nil {
		t.Fatalf("reserveCreateOnLedger() error = %v", err)
	}
	if !allowed {
		t.Fatal("reserveCreateOnLedger() denied after an expired reservation")
	}
	if effective.Cmp(resource.MustParse("2")) != 0 {
		t.Fatalf("effective allocation = %s, want 2", effective.String())
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if len(got.Status.Reservations) != 1 || got.Status.Reservations[0].ID != "current" {
		t.Fatalf("active reservations = %#v, want only current", got.Status.Reservations)
	}
}

func TestReplaceUsageOnLedgerDoesNotReleaseDecreaseBeforePersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant-a", Name: "cpu"}
	ledger := ledgerForTest(key, "10")
	cl := ledgerClientForTest(t, ledger)

	newUsage := resource.MustParse("1")
	zero := resource.MustParse("0")
	reservation := capsulev1beta2.QuantityLedgerReservation{
		ID:    "decrease",
		Usage: newUsage.DeepCopy(),
		Delta: quantityPtr(zero),
	}

	allowed, _, _, err := replaceUsageOnLedger(
		ctx,
		cl,
		cl,
		evaluatedQuota{MatchedQuota: quota.MatchedQuota{
			Name:      key.Name,
			Namespace: key.Namespace,
			Limit:     resource.MustParse("10"),
		}},
		resource.MustParse("10"),
		newUsage,
		&reservation,
		nil,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("replaceUsageOnLedger() error = %v", err)
	}
	if !allowed {
		t.Fatal("replaceUsageOnLedger() unexpectedly denied a decrease")
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if got.Status.Allocated.Cmp(resource.MustParse("10")) != 0 {
		t.Fatalf("allocation was released before persistence: got %s, want 10", got.Status.Allocated.String())
	}
	if len(got.Status.Reservations) != 1 {
		t.Fatalf("reservations = %d, want 1", len(got.Status.Reservations))
	}
	if delta := reservationDelta(got.Status.Reservations[0]); !delta.IsZero() {
		t.Fatalf("decrease reservation delta = %s, want 0", delta.String())
	}
}

func TestReplaceUsageOnLedgerReservesOnlyPositiveDelta(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant-a", Name: "cpu"}
	ledger := ledgerForTest(key, "5")
	cl := ledgerClientForTest(t, ledger)

	newUsage := resource.MustParse("8")
	delta := resource.MustParse("3")
	reservation := capsulev1beta2.QuantityLedgerReservation{
		ID:    "increase",
		Usage: newUsage.DeepCopy(),
		Delta: quantityPtr(delta),
	}

	allowed, _, _, err := replaceUsageOnLedger(
		ctx,
		cl,
		cl,
		evaluatedQuota{MatchedQuota: quota.MatchedQuota{
			Name:      key.Name,
			Namespace: key.Namespace,
			Limit:     resource.MustParse("8"),
		}},
		resource.MustParse("5"),
		newUsage,
		&reservation,
		nil,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("replaceUsageOnLedger() error = %v", err)
	}
	if !allowed {
		t.Fatal("replaceUsageOnLedger() unexpectedly denied")
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if got.Status.Allocated.Cmp(resource.MustParse("8")) != 0 {
		t.Fatalf("allocation = %s, want 8", got.Status.Allocated.String())
	}
	if got.Status.Reserved.Cmp(delta) != 0 {
		t.Fatalf("reserved = %s, want 3", got.Status.Reserved.String())
	}
}

func TestRollbackUsageReplacementRemovesPendingDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant-a", Name: "cpu"}
	ref := capsulev1beta2.QuantityLedgerObjectRef{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  "tenant-a",
		Name:       "pod-a",
		UID:        "pod-uid",
	}
	ledger := ledgerForTest(key, "10")
	ledger.Status.PendingDeletes = []capsulev1beta2.QuantityLedgerPendingDelete{
		{ID: "request-a", ObjectRef: ref, CreatedAt: metav1.Now()},
		{ID: "request-b", ObjectRef: ref, CreatedAt: metav1.Now()},
	}
	cl := ledgerClientForTest(t, ledger)
	pendingDelete := &capsulev1beta2.QuantityLedgerPendingDelete{
		ID:        "request-a",
		ObjectRef: ref,
	}

	if err := rollbackUsageReplacementOnLedger(
		ctx,
		cl,
		cl,
		key,
		"",
		resource.MustParse("10"),
		resource.MustParse("1"),
		pendingDelete,
	); err != nil {
		t.Fatalf("rollbackUsageReplacementOnLedger() error = %v", err)
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if len(got.Status.PendingDeletes) != 1 {
		t.Fatalf("pending deletes = %d, want 1", len(got.Status.PendingDeletes))
	}
	if got.Status.PendingDeletes[0].ID != "request-b" {
		t.Fatalf("remaining pending delete = %q, want request-b", got.Status.PendingDeletes[0].ID)
	}
	if got.Status.Allocated.Cmp(resource.MustParse("10")) != 0 {
		t.Fatalf("allocation = %s, want 10", got.Status.Allocated.String())
	}
}

func TestReservationDeltaBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	usage := resource.MustParse("4")
	got := reservationDelta(capsulev1beta2.QuantityLedgerReservation{Usage: usage})
	if got.Cmp(usage) != 0 {
		t.Fatalf("legacy reservation delta = %s, want %s", got.String(), usage.String())
	}
}

func TestStatusSubresourceUpdateQueuesQuotaReconciliation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	key := types.NamespacedName{Namespace: "tenant-a", Name: "active-pod-cpu"}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: key.Namespace},
	}
	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:       key.Name,
			Namespace:  key.Namespace,
			Generation: 1,
		},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Limit: resource.MustParse("2"),
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpAdd,
						Path:      ".spec.containers[*].resources.limits.cpu",
						Selectors: []selectors.SelectorWithFields{
							{
								FieldSelectors: []string{
									".status.phase!=Succeeded",
									".status.phase!=Failed",
									".status.phase!=Unknown",
								},
							},
						},
					},
				},
			},
		},
		Status: capsulev1beta2.CustomQuotaStatus{
			ObservedGeneration: 1,
			Usage: capsulev1beta2.CustomQuotaStatusUsage{
				Used:      resource.MustParse("2"),
				Available: resource.MustParse("0"),
			},
			Conditions: meta.ConditionList{
				{Type: meta.ReadyCondition, Status: metav1.ConditionTrue},
			},
		},
	}
	ledger := ledgerForTest(key, "2")

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(namespace, customQuota, ledger).
		Build()

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "completed",
			Namespace: key.Namespace,
			UID:       "pod-uid",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	newPod := oldPod.DeepCopy()
	newPod.Status.Phase = corev1.PodSucceeded

	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:         "status-request",
		Operation:   admissionv1.Update,
		Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Namespace:   key.Namespace,
		Name:        oldPod.Name,
		SubResource: "status",
		OldObject:   runtime.RawExtension{Object: oldPod},
		Object:      runtime.RawExtension{Object: newPod},
	}}

	if resp := handler.OnUpdate(cl, cl, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("status update response = %#v, want allowed", resp)
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if len(got.Status.PendingDeletes) != 1 {
		t.Fatalf("pending deletes = %d, want 1", len(got.Status.PendingDeletes))
	}
	if got.Status.PendingDeletes[0].ObjectRef.UID != oldPod.UID {
		t.Fatalf(
			"pending delete UID = %q, want %q",
			got.Status.PendingDeletes[0].ObjectRef.UID,
			oldPod.UID,
		)
	}
}

func TestStatusSubresourceUpdateSkipsNotReadyQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "not-ready",
			Namespace:  namespace.Name,
			Generation: 1,
		},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Limit: resource.MustParse("1"),
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpCount,
					},
				},
			},
		},
		Status: capsulev1beta2.CustomQuotaStatus{
			ObservedGeneration: 1,
			Conditions: meta.ConditionList{
				{Type: meta.ReadyCondition, Status: metav1.ConditionFalse},
			},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, customQuota).
		Build()

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: namespace.Name, UID: "pod-uid"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	newPod := oldPod.DeepCopy()
	newPod.Status.Phase = corev1.PodRunning

	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	req := statusUpdateRequest("not-ready-status", oldPod, newPod)

	if resp := handler.OnUpdate(cl, cl, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("status update response = %#v, want fail-open allow", resp)
	}
}

func TestStatusSubresourceUpdateUsesOnePolicySnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	key := types.NamespacedName{Namespace: "tenant-a", Name: "tracked-pods"}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: key.Namespace}}
	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:       key.Name,
			Namespace:  key.Namespace,
			Generation: 1,
		},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Limit: resource.MustParse("10"),
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpCount,
						Selectors: []selectors.SelectorWithFields{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"track": "yes"},
								},
							},
						},
					},
				},
			},
		},
		Status: capsulev1beta2.CustomQuotaStatus{
			ObservedGeneration: 1,
			Usage: capsulev1beta2.CustomQuotaStatusUsage{
				Used:      resource.MustParse("1"),
				Available: resource.MustParse("9"),
			},
			Conditions: meta.ConditionList{
				{Type: meta.ReadyCondition, Status: metav1.ConditionTrue},
			},
		},
	}
	ledger := ledgerForTest(key, "1")
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(namespace, customQuota, ledger).
		Build()
	reader := &readinessFlappingReader{Reader: cl}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a",
			Namespace: key.Namespace,
			UID:       "pod-uid",
			Labels:    map[string]string{"track": "yes"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	newPod := oldPod.DeepCopy()
	newPod.Status.Phase = corev1.PodRunning

	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	req := statusUpdateRequest("unchanged-label-status", oldPod, newPod)

	if resp := handler.OnUpdate(cl, reader, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("status update response = %#v, want allowed", resp)
	}
	if reader.customQuotaListCalls != 1 {
		t.Fatalf(
			"CustomQuota policy list calls = %d, want one immutable snapshot for old and new objects",
			reader.customQuotaListCalls,
		)
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if len(got.Status.Reservations) != 0 || len(got.Status.PendingDeletes) != 0 {
		t.Fatalf(
			"unchanged matching status update queued ledger work: reservations=%+v pendingDeletes=%+v",
			got.Status.Reservations,
			got.Status.PendingDeletes,
		)
	}
}

func TestStatusSubresourceIncreaseQueuesZeroDeltaWithoutEnforcement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	key := types.NamespacedName{Namespace: "tenant-a", Name: "running-pods"}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: key.Namespace}}
	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:       key.Name,
			Namespace:  key.Namespace,
			Generation: 1,
		},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Limit: resource.MustParse("2"),
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpCount,
						Selectors: []selectors.SelectorWithFields{
							{FieldSelectors: []string{".status.phase=Running"}},
						},
					},
				},
			},
		},
		Status: capsulev1beta2.CustomQuotaStatus{
			ObservedGeneration: 1,
			Usage: capsulev1beta2.CustomQuotaStatusUsage{
				Used:      resource.MustParse("2"),
				Available: resource.MustParse("0"),
			},
			Conditions: meta.ConditionList{
				{Type: meta.ReadyCondition, Status: metav1.ConditionTrue},
			},
		},
	}
	ledger := ledgerForTest(key, "2")
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(namespace, customQuota, ledger).
		Build()

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: key.Namespace, UID: "pod-uid"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	newPod := oldPod.DeepCopy()
	newPod.Status.Phase = corev1.PodRunning

	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	req := statusUpdateRequest("increase-status", oldPod, newPod)

	if resp := handler.OnUpdate(cl, cl, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("status update response = %#v, want allowed beyond quota limit", resp)
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := cl.Get(ctx, key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if len(got.Status.Reservations) != 1 {
		t.Fatalf("reservations = %d, want one reconciliation notification", len(got.Status.Reservations))
	}
	if delta := reservationDelta(got.Status.Reservations[0]); !delta.IsZero() {
		t.Fatalf("status notification delta = %s, want 0", delta.String())
	}
	if got.Status.Allocated.Cmp(resource.MustParse("2")) != 0 {
		t.Fatalf("allocated = %s, want unchanged value 2", got.Status.Allocated.String())
	}
}

func TestTerminatingNamespaceBypassesQuotaProcessing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	now := metav1.Now()
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "terminating",
			DeletionTimestamp: &now,
			Finalizers:        []string{"kubernetes"},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace).
		Build()
	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:         "terminating-status",
		Operation:   admissionv1.Update,
		Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Namespace:   namespace.Name,
		Name:        "pod-a",
		SubResource: "status",
	}}

	if resp := handler.OnUpdate(cl, cl, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("terminating namespace status response = %#v, want allowed", resp)
	}

	req.Operation = admissionv1.Delete
	req.SubResource = ""
	if resp := handler.OnDelete(cl, cl, nil, nil)(ctx, req); resp != nil {
		t.Fatalf("terminating namespace delete response = %#v, want allowed", resp)
	}
}

func TestCustomQuotaReadyForAdmissionRequiresCurrentGeneration(t *testing.T) {
	t.Parallel()

	status := capsulev1beta2.CustomQuotaStatus{
		ObservedGeneration: 3,
		Conditions: meta.ConditionList{
			{
				Type:   meta.ReadyCondition,
				Status: metav1.ConditionTrue,
			},
		},
	}

	if !customQuotaReadyForAdmission(3, status) {
		t.Fatal("current ready generation must be active")
	}
	if customQuotaReadyForAdmission(4, status) {
		t.Fatal("stale ready status must not activate a new generation")
	}

	status.Conditions[0].Status = metav1.ConditionFalse
	if customQuotaReadyForAdmission(3, status) {
		t.Fatal("non-ready quota must not be active")
	}
}

func TestMatchAllQuotasFailsClosedUntilCurrentGenerationIsReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pods",
			Namespace:  "tenant-a",
			Generation: 2,
		},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Limit: resource.MustParse("10"),
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpCount,
					},
				},
			},
		},
		Status: capsulev1beta2.CustomQuotaStatus{
			ObservedGeneration: 1,
			Conditions: meta.ConditionList{
				{Type: meta.ReadyCondition, Status: metav1.ConditionTrue},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(customQuota).Build()
	handler := &objectCalculationHandler{
		targetsCache:  cache.NewCompiledTargetsCache[string](),
		jsonPathCache: cache.NewJSONPathCache(),
	}
	request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Namespace: "tenant-a",
		Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
	}}
	object := unstructured.Unstructured{}
	object.SetAPIVersion("v1")
	object.SetKind("Pod")
	object.SetNamespace("tenant-a")
	object.SetName("pod-a")

	_, err := handler.matchAllQuotas(ctx, cl, request, object)
	if err == nil || !strings.Contains(err.Error(), "not ready for generation 2") {
		t.Fatalf("matchAllQuotas() error = %v, want current-generation readiness error", err)
	}

	current := &capsulev1beta2.CustomQuota{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "tenant-a", Name: "pods"}, current); err != nil {
		t.Fatalf("get CustomQuota: %v", err)
	}
	current.Status.ObservedGeneration = current.Generation
	if err := cl.Update(ctx, current); err != nil {
		t.Fatalf("mark CustomQuota ready: %v", err)
	}

	matched, err := handler.matchAllQuotas(ctx, cl, request, object)
	if err != nil {
		t.Fatalf("matchAllQuotas() error = %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("matchAllQuotas() matches = %d, want 1", len(matched))
	}
}

func TestCompiledTargetsCacheRefreshesInPlace(t *testing.T) {
	t.Parallel()

	targetsCache := cache.NewCompiledTargetsCache[string]()
	handler := &objectCalculationHandler{
		targetsCache:  targetsCache,
		jsonPathCache: cache.NewJSONPathCache(),
	}
	customQuota := &capsulev1beta2.CustomQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "objects", Namespace: "tenant-a"},
		Spec: capsulev1beta2.CustomQuotaSpec{
			Sources: []capsulev1beta2.CustomQuotaSpecSource{
				{
					VersionKind: caprunt.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpCount,
					},
				},
			},
		},
	}
	key := "tenant-a/objects"
	targetsCache.Set(key, []cache.CompiledTarget{
		{
			CustomQuotaStatusTarget: capsulev1beta2.CustomQuotaStatusTarget{
				GroupVersionKind: metav1.GroupVersionKind{Version: "v1", Kind: "Service"},
			},
		},
	})

	compiled, err := handler.getOrCompileCustomQuotaTargets(customQuota)
	if err != nil {
		t.Fatalf("getOrCompileCustomQuotaTargets() error = %v", err)
	}
	if len(compiled) != 1 || compiled[0].Kind != "Pod" {
		t.Fatalf("compiled targets = %#v, want current Pod source", compiled)
	}
	if entries := targetsCache.Stats(); entries != 1 {
		t.Fatalf("compiled target cache entries = %d, want one stable quota key", entries)
	}
}

func ledgerForTest(key types.NamespacedName, allocated string) *capsulev1beta2.QuantityLedger {
	return &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Status: capsulev1beta2.QuantityLedgerStatus{
			Allocated: resource.MustParse(allocated),
		},
	}
}

func statusUpdateRequest(
	uid types.UID,
	oldPod *corev1.Pod,
	newPod *corev1.Pod,
) admission.Request {
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:         uid,
		Operation:   admissionv1.Update,
		Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Namespace:   oldPod.Namespace,
		Name:        oldPod.Name,
		SubResource: "status",
		OldObject:   runtime.RawExtension{Object: oldPod},
		Object:      runtime.RawExtension{Object: newPod},
	}}
}

func ledgerClientForTest(t *testing.T, ledger *capsulev1beta2.QuantityLedger) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(ledger).
		Build()
}

type readinessFlappingReader struct {
	client.Reader

	customQuotaListCalls int
}

func (r *readinessFlappingReader) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	if err := r.Reader.List(ctx, list, opts...); err != nil {
		return err
	}

	quotas, ok := list.(*capsulev1beta2.CustomQuotaList)
	if !ok {
		return nil
	}

	r.customQuotaListCalls++
	if r.customQuotaListCalls < 2 {
		return nil
	}

	for i := range quotas.Items {
		condition := quotas.Items[i].Status.Conditions.GetConditionByType(meta.ReadyCondition)
		if condition != nil {
			condition.Status = metav1.ConditionFalse
		}
	}

	return nil
}

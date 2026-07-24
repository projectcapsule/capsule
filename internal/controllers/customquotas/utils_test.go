// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestCompiledTargetsSupportMixedJSONPathAndCELSelectors(t *testing.T) {
	t.Parallel()

	celCache, err := cache.NewCELCache()
	if err != nil {
		t.Fatalf("NewCELCache() error = %v", err)
	}

	targets, err := CompileTargets(
		cache.NewJSONPathCache(),
		celCache,
		[]capsulev1beta2.CustomQuotaStatusTarget{
			{
				CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
					Operation: quota.OpCount,
					Selectors: []selectors.SelectorWithFields{
						{
							FieldSelectors: []string{".spec.restartPolicy=Always"},
							CELExpressions: []string{
								`object.spec.containers.exists(c, c.image == "nginx:1.27.0")`,
							},
						},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("CompileTargets() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"restartPolicy": "Always",
			"containers": []any{
				map[string]any{"image": "nginx:1.27.0"},
			},
		},
	}}

	matched, err := MatchesCompiledSelectorsWithFields(
		context.Background(),
		object,
		targets[0].CompiledSelectors,
	)
	if err != nil {
		t.Fatalf("MatchesCompiledSelectorsWithFields() error = %v", err)
	}
	if !matched {
		t.Fatal("mixed JSONPath and CEL selectors did not match")
	}

	object.Object["spec"].(map[string]any)["restartPolicy"] = "Never"
	matched, err = MatchesCompiledSelectorsWithFields(
		context.Background(),
		object,
		targets[0].CompiledSelectors,
	)
	if err != nil {
		t.Fatalf("MatchesCompiledSelectorsWithFields() JSONPath mismatch error = %v", err)
	}
	if matched {
		t.Fatal("selector matched when its JSONPath condition was false")
	}

	object.Object["spec"].(map[string]any)["restartPolicy"] = "Always"
	object.Object["spec"].(map[string]any)["containers"] = []any{
		map[string]any{"image": "nginx:1.26.0"},
	}
	matched, err = MatchesCompiledSelectorsWithFields(
		context.Background(),
		object,
		targets[0].CompiledSelectors,
	)
	if err != nil {
		t.Fatalf("MatchesCompiledSelectorsWithFields() CEL mismatch error = %v", err)
	}
	if matched {
		t.Fatal("selector matched when its CEL condition was false")
	}
}

func TestUsageForTargetSupportsCELQuantityLists(t *testing.T) {
	t.Parallel()

	celCache, err := cache.NewCELCache()
	if err != nil {
		t.Fatalf("NewCELCache() error = %v", err)
	}

	targets, err := CompileTargets(
		cache.NewJSONPathCache(),
		celCache,
		[]capsulev1beta2.CustomQuotaStatusTarget{
			{
				CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
					Operation: quota.OpAdd,
					CEL: `object.spec.containers` +
						`.map(c, quantity(c.resources.requests["cpu"]))`,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("CompileTargets() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "250m"},
					},
				},
				map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "500m"},
					},
				},
			},
		},
	}}

	usage, err := usageForTarget(context.Background(), object, targets[0])
	if err != nil {
		t.Fatalf("usageForTarget() error = %v", err)
	}
	if usage.Cmp(resource.MustParse("750m")) != 0 {
		t.Fatalf("usageForTarget() = %s, want 750m", usage.String())
	}
}

func TestUsagePercentage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		used  string
		limit string
		want  float64
	}{
		{
			name:  "returns zero for zero limit",
			used:  "1",
			limit: "0",
			want:  0,
		},
		{
			name:  "calculates whole quantity percentage",
			used:  "2",
			limit: "8",
			want:  25,
		},
		{
			name:  "calculates milli quantity percentage",
			used:  "250m",
			limit: "1",
			want:  25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := usagePercentage(resource.MustParse(tt.used), resource.MustParse(tt.limit))
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestQuantityLedgerWorkDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)

	t.Run("settled ledger", func(t *testing.T) {
		t.Parallel()

		hasWork, delay := quantityLedgerWorkDelay(now, &capsulev1beta2.QuantityLedger{})
		if hasWork || delay != 0 {
			t.Fatalf("quantityLedgerWorkDelay() = (%v, %s), want (false, 0)", hasWork, delay)
		}
	})

	t.Run("fresh work is debounced", func(t *testing.T) {
		t.Parallel()

		updated := metav1.NewTime(now.Add(-100 * time.Millisecond))
		ledger := &capsulev1beta2.QuantityLedger{
			Status: capsulev1beta2.QuantityLedgerStatus{
				Reservations: []capsulev1beta2.QuantityLedgerReservation{
					{ID: "request", UpdatedAt: updated},
				},
			},
		}

		hasWork, delay := quantityLedgerWorkDelay(now, ledger)
		if !hasWork {
			t.Fatal("quantityLedgerWorkDelay() did not detect work")
		}
		if delay != 400*time.Millisecond {
			t.Fatalf("quantityLedgerWorkDelay() delay = %s, want 400ms", delay)
		}
	})

	t.Run("maximum batch delay bounds continuous work", func(t *testing.T) {
		t.Parallel()

		old := metav1.NewTime(now.Add(-3 * time.Second))
		fresh := metav1.NewTime(now.Add(-100 * time.Millisecond))
		ledger := &capsulev1beta2.QuantityLedger{
			Status: capsulev1beta2.QuantityLedgerStatus{
				Reservations: []capsulev1beta2.QuantityLedgerReservation{
					{ID: "old", UpdatedAt: old},
					{ID: "fresh", UpdatedAt: fresh},
				},
			},
		}

		hasWork, delay := quantityLedgerWorkDelay(now, ledger)
		if !hasWork || delay != 0 {
			t.Fatalf("quantityLedgerWorkDelay() = (%v, %s), want ready work", hasWork, delay)
		}
	})
}

func TestQuantityLedgerReservationDelta(t *testing.T) {
	t.Parallel()

	usage := resource.MustParse("5")
	legacy := quantityLedgerReservationDelta(capsulev1beta2.QuantityLedgerReservation{Usage: usage})
	if legacy.Cmp(usage) != 0 {
		t.Fatalf("legacy delta = %s, want %s", legacy.String(), usage.String())
	}

	zero := resource.MustParse("0")
	explicit := quantityLedgerReservationDelta(capsulev1beta2.QuantityLedgerReservation{
		Usage: usage,
		Delta: &zero,
	})
	if !explicit.IsZero() {
		t.Fatalf("explicit delta = %s, want 0", explicit.String())
	}
}

func TestMaterializedReservationPositionsSupersedesOlderUpdates(t *testing.T) {
	t.Parallel()

	ref := capsulev1beta2.QuantityLedgerObjectRef{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  "tenant-a",
		Name:       "pod-a",
		UID:        "pod-uid",
	}
	reservations := []capsulev1beta2.QuantityLedgerReservation{
		{ID: "older", ObjectRef: ref, Usage: resource.MustParse("8")},
		{ID: "newer", ObjectRef: ref, Usage: resource.MustParse("9")},
	}
	claims := []capsulev1beta2.CustomQuotaClaimItem{
		{
			GroupVersionKind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
			NamespacedObjectWithUIDReference: capsulemeta.NamespacedObjectWithUIDReference{
				Name:      "pod-a",
				Namespace: "tenant-a",
				UID:       types.UID("pod-uid"),
			},
			Usage: resource.MustParse("9"),
		},
	}

	positions := materializedReservationPositions(reservations, claims)
	if got := positions[ledgerReservationObjectKey(ref)]; got != 2 {
		t.Fatalf("materialized position = %d, want 2 reservations cleared through the newer update", got)
	}
}

func TestReconcileQuantityLedgerAllocationHandlesFastObjectTransition(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	key := types.NamespacedName{Namespace: "capsule-system", Name: "pods"}
	ref := capsulev1beta2.QuantityLedgerObjectRef{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  "tenant-a",
		Name:       "pod-a",
		UID:        "pod-uid",
	}
	now := metav1.Now()
	expiresAt := metav1.NewTime(now.Add(time.Minute))
	delta := resource.MustParse("1")

	t.Run("releases an earlier reservation after a confirmed transition out of claims", func(t *testing.T) {
		t.Parallel()

		ledger := &capsulev1beta2.QuantityLedger{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Status: capsulev1beta2.QuantityLedgerStatus{
				Allocated: resource.MustParse("1"),
				Reserved:  resource.MustParse("1"),
				Reservations: []capsulev1beta2.QuantityLedgerReservation{
					{
						ID:        "create",
						Usage:     resource.MustParse("1"),
						Delta:     &delta,
						ObjectRef: ref,
						CreatedAt: now,
						UpdatedAt: now,
						ExpiresAt: &expiresAt,
					},
				},
				PendingDeletes: []capsulev1beta2.QuantityLedgerPendingDelete{
					{ID: "status-update", ObjectRef: ref, CreatedAt: now},
				},
			},
		}
		kubeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
			WithObjects(ledger).
			Build()

		requeueAfter, err := reconcileQuantityLedgerAllocation(
			context.Background(),
			kubeClient,
			kubeClient,
			logr.Discard(),
			key,
			resource.MustParse("0"),
			nil,
		)
		if err != nil {
			t.Fatalf("reconcileQuantityLedgerAllocation() error = %v", err)
		}
		if requeueAfter != nil {
			t.Fatalf("requeueAfter = %s, want nil", requeueAfter.String())
		}

		got := &capsulev1beta2.QuantityLedger{}
		if err := kubeClient.Get(context.Background(), key, got); err != nil {
			t.Fatalf("get ledger: %v", err)
		}
		if len(got.Status.Reservations) != 0 || len(got.Status.PendingDeletes) != 0 {
			t.Fatalf(
				"ledger work was not settled: reservations=%+v pendingDeletes=%+v",
				got.Status.Reservations,
				got.Status.PendingDeletes,
			)
		}
		if !got.Status.Reserved.IsZero() || !got.Status.Allocated.IsZero() {
			t.Fatalf(
				"ledger quantities were not released: reserved=%s allocated=%s",
				got.Status.Reserved.String(),
				got.Status.Allocated.String(),
			)
		}
	})

	t.Run("keeps reservations when the prior matching claim is still present", func(t *testing.T) {
		t.Parallel()

		ledger := &capsulev1beta2.QuantityLedger{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Status: capsulev1beta2.QuantityLedgerStatus{
				Allocated: resource.MustParse("2"),
				Reserved:  resource.MustParse("1"),
				Reservations: []capsulev1beta2.QuantityLedgerReservation{
					{
						ID:        "update",
						Usage:     resource.MustParse("2"),
						Delta:     &delta,
						ObjectRef: ref,
						CreatedAt: now,
						UpdatedAt: now,
						ExpiresAt: &expiresAt,
					},
				},
				PendingDeletes: []capsulev1beta2.QuantityLedgerPendingDelete{
					{ID: "delete", ObjectRef: ref, CreatedAt: now},
				},
			},
		}
		kubeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
			WithObjects(ledger).
			Build()
		claims := []capsulev1beta2.CustomQuotaClaimItem{
			{
				GroupVersionKind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
				NamespacedObjectWithUIDReference: capsulemeta.NamespacedObjectWithUIDReference{
					Name:      ref.Name,
					Namespace: capsulemeta.RFC1123SubdomainName(ref.Namespace),
					UID:       ref.UID,
				},
				Usage: resource.MustParse("1"),
			},
		}

		requeueAfter, err := reconcileQuantityLedgerAllocation(
			context.Background(),
			kubeClient,
			kubeClient,
			logr.Discard(),
			key,
			resource.MustParse("1"),
			claims,
		)
		if err != nil {
			t.Fatalf("reconcileQuantityLedgerAllocation() error = %v", err)
		}
		if requeueAfter == nil {
			t.Fatal("requeueAfter = nil, want pending work to be retried")
		}

		got := &capsulev1beta2.QuantityLedger{}
		if err := kubeClient.Get(context.Background(), key, got); err != nil {
			t.Fatalf("get ledger: %v", err)
		}
		if len(got.Status.Reservations) != 1 || len(got.Status.PendingDeletes) != 1 {
			t.Fatalf(
				"ledger work was released before the transition persisted: reservations=%+v pendingDeletes=%+v",
				got.Status.Reservations,
				got.Status.PendingDeletes,
			)
		}
	})

	t.Run("drops an expired pending delete when the object still matches", func(t *testing.T) {
		t.Parallel()

		expiredCreatedAt := metav1.NewTime(now.Add(-31 * time.Second))
		ledger := &capsulev1beta2.QuantityLedger{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Status: capsulev1beta2.QuantityLedgerStatus{
				Allocated: resource.MustParse("1"),
				PendingDeletes: []capsulev1beta2.QuantityLedgerPendingDelete{
					{ID: "failed-or-false-update", ObjectRef: ref, CreatedAt: expiredCreatedAt},
				},
			},
		}
		kubeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
			WithObjects(ledger).
			Build()
		claims := []capsulev1beta2.CustomQuotaClaimItem{
			{
				GroupVersionKind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
				NamespacedObjectWithUIDReference: capsulemeta.NamespacedObjectWithUIDReference{
					Name:      ref.Name,
					Namespace: capsulemeta.RFC1123SubdomainName(ref.Namespace),
					UID:       ref.UID,
				},
				Usage: resource.MustParse("1"),
			},
		}

		requeueAfter, err := reconcileQuantityLedgerAllocation(
			context.Background(),
			kubeClient,
			kubeClient,
			logr.Discard(),
			key,
			resource.MustParse("1"),
			claims,
		)
		if err != nil {
			t.Fatalf("reconcileQuantityLedgerAllocation() error = %v", err)
		}
		if requeueAfter != nil {
			t.Fatalf("requeueAfter = %s, want nil after expired hint is discarded", requeueAfter.String())
		}

		got := &capsulev1beta2.QuantityLedger{}
		if err := kubeClient.Get(context.Background(), key, got); err != nil {
			t.Fatalf("get ledger: %v", err)
		}
		if len(got.Status.PendingDeletes) != 0 {
			t.Fatalf("expired pending deletes = %+v, want none", got.Status.PendingDeletes)
		}
		if got.Status.Allocated.Cmp(resource.MustParse("1")) != 0 {
			t.Fatalf("allocated = %s, want observed usage 1", got.Status.Allocated.String())
		}
	})
}

func TestReconcileQuantityLedgerAllocationUsesDirectReader(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	key := types.NamespacedName{Namespace: "tenant-a", Name: "pods"}
	ledger := &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
	}
	direct := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.QuantityLedger{}).
		WithObjects(ledger).
		Build()
	stale := &rejectingGetClient{Client: direct}

	if _, err := reconcileQuantityLedgerAllocation(
		context.Background(),
		stale,
		direct,
		logr.Discard(),
		key,
		resource.MustParse("1"),
		nil,
	); err != nil {
		t.Fatalf("reconcileQuantityLedgerAllocation() error = %v", err)
	}

	got := &capsulev1beta2.QuantityLedger{}
	if err := direct.Get(context.Background(), key, got); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if got.Status.Allocated.Cmp(resource.MustParse("1")) != 0 {
		t.Fatalf("allocated = %s, want 1", got.Status.Allocated.String())
	}
}

type rejectingGetClient struct {
	client.Client
}

func (c *rejectingGetClient) Get(
	context.Context,
	client.ObjectKey,
	client.Object,
	...client.GetOption,
) error {
	return errors.New("cached Get must not be used for ledger conflict retries")
}

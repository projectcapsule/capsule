// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
)

func quotaFixture(t *testing.T, quota int32, namespaces []string, reservations map[string]time.Time) (client.Client, *capsulev1beta2.Tenant) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	tnt := &capsulev1beta2.Tenant{Name: "solar", UID: "solar-uid"}
	tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{Quota: &quota}
	tnt.Status.Namespaces = namespaces

	if reservations != nil {
		value, err := json.Marshal(reservations)
		if err != nil {
			t.Fatal(err)
		}

		tnt.Annotations = map[string]string{meta.NamespaceReservationsAnnotation: string(value)}
	}

	objects := []client.Object{tnt}
	for _, name := range namespaces {
		objects = append(objects, &corev1.Namespace{Name: name})
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(tnt).Build()

	current := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: tnt.Name}, current); err != nil {
		t.Fatal(err)
	}

	return cl, current
}

func createNamespace(cl client.Client, tnt *capsulev1beta2.Tenant, name string) *admission.Response {
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Name: name}}

	return QuotaHandler().OnCreate(cl, cl, users.AdmissionUser{}, &corev1.Namespace{Name: name}, nil, events.NewDiscardRecorder(), tnt.DeepCopy())(context.Background(), req)
}

func reservedNamespaces(t *testing.T, cl client.Client) map[string]time.Time {
	t.Helper()

	tnt := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "solar"}, tnt); err != nil {
		t.Fatal(err)
	}

	reservations := map[string]time.Time{}
	if value := tnt.Annotations[meta.NamespaceReservationsAnnotation]; value != "" {
		if err := json.Unmarshal([]byte(value), &reservations); err != nil {
			t.Fatal(err)
		}
	}

	return reservations
}

// Concurrent creations all see the same Tenant: before the fix every one of
// them was admitted. Only the free slots may be taken.
func TestQuotaHandlerConcurrentCreatesDoNotExceedQuota(t *testing.T) {
	cl, tnt := quotaFixture(t, 3, []string{"solar-dev"}, nil)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed []string
	)

	for i := range 5 {
		name := fmt.Sprintf("solar-r%d", i)

		wg.Go(func() {
			if response := createNamespace(cl, tnt, name); response == nil || response.Allowed {
				mu.Lock()
				allowed = append(allowed, name)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if len(allowed) != 2 {
		t.Fatalf("admitted %d namespaces (%v) for 2 free slots", len(allowed), allowed)
	}

	reservations := reservedNamespaces(t, cl)
	for _, name := range allowed {
		if _, ok := reservations[name]; !ok {
			t.Errorf("admitted namespace %s holds no reservation: %v", name, reservations)
		}
	}
}

func TestQuotaHandlerReservations(t *testing.T) {
	now := time.Now()

	for _, tc := range []struct {
		name         string
		quota        int32
		namespaces   []string
		reservations map[string]time.Time
		create       string
		allowed      bool
		reserved     []string
	}{
		{
			name: "free slot is reserved", quota: 2, namespaces: []string{"solar-dev"},
			create: "solar-prod", allowed: true, reserved: []string{"solar-prod"},
		},
		{
			name: "a pending reservation takes the last slot", quota: 2, namespaces: []string{"solar-dev"},
			reservations: map[string]time.Time{"solar-prod": now.Add(time.Minute)},
			create:       "solar-test", allowed: false, reserved: []string{"solar-prod"},
		},
		{
			name: "an expired reservation frees its slot", quota: 2, namespaces: []string{"solar-dev"},
			reservations: map[string]time.Time{"solar-gone": now.Add(-time.Second)},
			create:       "solar-test", allowed: true, reserved: []string{"solar-test"},
		},
		{
			name: "a reservation already in the status is not counted twice", quota: 2, namespaces: []string{"solar-dev"},
			reservations: map[string]time.Time{"solar-dev": now.Add(time.Minute)},
			create:       "solar-test", allowed: true, reserved: []string{"solar-test"},
		},
		{
			name: "the same namespace admitted again keeps its reservation", quota: 2, namespaces: []string{"solar-dev"},
			reservations: map[string]time.Time{"solar-prod": now.Add(time.Minute)},
			create:       "solar-prod", allowed: true, reserved: []string{"solar-prod"},
		},
		{
			name: "full tenant", quota: 1, namespaces: []string{"solar-dev"},
			create: "solar-prod", allowed: false,
		},
		{
			name: "an existing namespace is left to the API server", quota: 1, namespaces: []string{"solar-dev"},
			create: "solar-dev", allowed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, tnt := quotaFixture(t, tc.quota, tc.namespaces, tc.reservations)

			response := createNamespace(cl, tnt, tc.create)
			if allowed := response == nil || response.Allowed; allowed != tc.allowed {
				t.Fatalf("allowed=%t, want %t: %v", allowed, tc.allowed, response)
			}

			active := activeNamespaceReservations(func() *capsulev1beta2.Tenant {
				current := &capsulev1beta2.Tenant{}
				_ = cl.Get(context.Background(), types.NamespacedName{Name: "solar"}, current)

				return current
			}(), time.Now())
			if len(active) != len(tc.reserved) {
				t.Fatalf("reservations=%v, want %v", active, tc.reserved)
			}

			for _, name := range tc.reserved {
				if _, ok := active[name]; !ok {
					t.Fatalf("reservations=%v, want %v", active, tc.reserved)
				}
			}
		})
	}
}

func TestQuotaHandlerDryRunDoesNotReserve(t *testing.T) {
	cl, tnt := quotaFixture(t, 2, []string{"solar-dev"}, nil)

	dryRun := true
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Name: "solar-prod", DryRun: &dryRun}}

	response := QuotaHandler().OnCreate(cl, cl, users.AdmissionUser{}, &corev1.Namespace{Name: "solar-prod"}, nil, events.NewDiscardRecorder(), tnt)(context.Background(), req)
	if response != nil && !response.Allowed {
		t.Fatalf("dry run denied: %v", response)
	}

	if reservations := reservedNamespaces(t, cl); len(reservations) != 0 {
		t.Fatalf("dry run reserved %v", reservations)
	}
}

func TestQuotaHandlerWithoutQuotaDoesNotWrite(t *testing.T) {
	cl, tnt := quotaFixture(t, 1, nil, nil)
	tnt.Spec.NamespaceOptions = nil

	if response := createNamespace(cl, tnt, "solar-prod"); response != nil {
		t.Fatalf("response=%v", response)
	}

	if reservations := reservedNamespaces(t, cl); len(reservations) != 0 {
		t.Fatalf("reserved %v without a quota", reservations)
	}
}

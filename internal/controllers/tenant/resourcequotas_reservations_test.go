// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

const recountResource = "widgets.example.com_v1"

var recountGVR = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}

func admitRecountWidget(ctx context.Context, c client.Client, ns *corev1.Namespace, id, name string) *admission.Response {
	request := admission.Request{UID: types.UID(id), Namespace: ns.Name, Name: name, Resource: metav1.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}}
	return generic.ResourceCounterHandler(c).OnCreate(c, c, nil, events.NewDiscardRecorder())(ctx, request)
}

func TestLegacyRecountCannotEraseFastQueueReservations(t *testing.T) {
	for _, materialized := range []bool{false, true} {
		t.Run(fmt.Sprintf("materialized=%v", materialized), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "1"}
			foreign := provisioningTenant()
			foreign.Name, foreign.UID = "tenant-b", "tenant-b-uid"
			foreign.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "1"}
			r, dyn := namespaceCleanupFixture(t, tnt, foreign)
			base := r.Client
			started, release := make(chan struct{}), make(chan struct{})
			var first sync.Once
			var reads atomic.Int32
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if nsList, ok := list.(*corev1.NamespaceList); ok {
						reads.Add(1)
						first.Do(func() { close(started) })
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
						// Model an informer that has not observed the new namespace.
						nsList.Items = nil
						return nil
					}
					return c.List(ctx, list, opts...)
				},
			})
			done := make(chan error, 1)
			go func() { done <- r.syncCustomResourceQuotaUsages(ctx, tnt.DeepCopy()) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			ns := cleanupOwnedNamespace(tnt, "new-space", false)
			if err := base.Create(ctx, ns); err != nil {
				t.Fatal(err)
			}
			// Permission provisioning must complete while the recount is paused.
			if _, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}); err != nil {
				t.Fatal(err)
			}
			assertBindingSubject(t, ctx, base, ns.Name, "alice", true)
			if response := admitRecountWidget(ctx, base, ns, "first", "first"); response != nil {
				t.Fatalf("first create: %+v", response)
			}
			if materialized {
				obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"name": "first", "namespace": ns.Name}}}
				if _, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if reads.Load() < 2 {
				t.Fatal("write conflict did not restart the whole recount")
			}
			// Another replica with the same stale index must retain the namespace
			// even after a first recount has observed the object materialize.
			otherReplica := &Manager{Client: r.Client, reader: base, DynamicClient: dyn}
			if err := otherReplica.syncCustomResourceQuotaUsages(ctx, tnt.DeepCopy()); err != nil {
				t.Fatal(err)
			}
			current := &capsulev1beta2.Tenant{}
			if err := base.Get(ctx, client.ObjectKeyFromObject(tnt), current); err != nil {
				t.Fatal(err)
			}
			if got := current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)]; got != "1" {
				t.Fatalf("reservation overwritten: used=%s", got)
			}
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			if err != nil {
				t.Fatal(err)
			}
			if state.Namespaces[ns.Name] != ns.UID {
				t.Fatal("namespace visibility pin was lost before status publication")
			}
			if response := admitRecountWidget(ctx, base, ns, "second", "second"); response == nil || response.Allowed {
				t.Fatal("second create bypassed limit one")
			}
			unchanged := &capsulev1beta2.Tenant{}
			if err := base.Get(ctx, client.ObjectKeyFromObject(foreign), unchanged); err != nil {
				t.Fatal(err)
			}
			if unchanged.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)] != "" {
				t.Fatal("recount changed another tenant")
			}
		})
	}
}

func TestLegacyRecountDoesNotCommitPartialCounts(t *testing.T) {
	tnt := provisioningTenant()
	tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "2", capsulev1beta2.UsedAnnotationForResource(recountResource): "1"}
	ns := cleanupOwnedNamespace(tnt, "selected", false)
	r, dyn := namespaceCleanupFixture(t, tnt, ns)
	failure := errors.New("resource API unavailable")
	dyn.PrependReactor("list", "widgets", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, failure })
	if err := r.syncCustomResourceQuotaUsages(t.Context(), tnt); !errors.Is(err, failure) {
		t.Fatalf("lost count error: %v", err)
	}
	current := &capsulev1beta2.Tenant{}
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(tnt), current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)] != "1" {
		t.Fatal("failed recount changed the counter")
	}
}

func TestLegacyRecountReservationLifecycle(t *testing.T) {
	for _, mode := range []string{"pending", "materialized", "expired", "namespace-recreated", "namespace-reassigned", "namespace-gone", "status-published", "limit-removed", "tenant-recreated", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "1"}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			r, dyn := namespaceCleanupFixture(t, tnt, ns)
			require.Nil(t, admitRecountWidget(ctx, r.Client, ns, "first", "first"))
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			wantUsed := "0"
			wantPending := 0
			wantPin := false
			switch mode {
			case "pending":
				wantUsed = "1"
				wantPending = 1
				wantPin = true
			case "materialized":
				wantUsed = "1"
				wantPin = true
				obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"name": "first", "namespace": ns.Name}}}
				_, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, obj, metav1.CreateOptions{})
				require.NoError(t, err)
			case "expired":
				entry := state.Pending[recountResource][0]
				expiry := metav1.NewTime(time.Now().Add(-time.Second))
				entry.ExpiresAt = &expiry
				state.Pending[recountResource][0] = entry
				require.NoError(t, capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state))
				require.NoError(t, r.Update(ctx, current))
				wantPin = true
			case "namespace-recreated":
				ns.UID = "replacement"
				require.NoError(t, r.Update(ctx, ns))
			case "namespace-reassigned":
				ns.OwnerReferences[0].UID = "other-tenant"
				require.NoError(t, r.Update(ctx, ns))
			case "namespace-gone":
				require.NoError(t, r.Delete(ctx, ns))
			case "status-published":
				current.Status.Namespaces = []string{ns.Name}
				current.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID}}
				require.NoError(t, r.Status().Update(ctx, current))
				wantUsed = "1"
				wantPending = 1
			case "limit-removed":
				delete(current.Annotations, capsulev1beta2.LimitAnnotationForResource(recountResource))
				require.NoError(t, r.Update(ctx, current))
				wantUsed = "1"
			case "tenant-recreated":
				current.UID = "replacement"
				require.NoError(t, r.Update(ctx, current))
				wantUsed = "1"
				wantPending = 1
				wantPin = true
			case "malformed":
				current.Annotations[capsuletenant.LegacyQuotaReservationsAnnotation] = "invalid"
				require.NoError(t, r.Update(ctx, current))
			}
			err = r.syncCustomResourceQuotaUsages(ctx, tnt)
			if mode == "malformed" {
				require.ErrorContains(t, err, "decode legacy quota")
				return
			}
			require.NoError(t, err)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, wantUsed, current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			state, err = capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Len(t, state.Pending[recountResource], wantPending)
			require.Equal(t, wantPin, state.Namespaces[ns.Name] == ns.UID || (mode == "tenant-recreated" && len(state.Namespaces) == 1))
			if mode == "expired" {
				require.Nil(t, admitRecountWidget(ctx, r.Client, ns, "next", "next"))
			}
		})
	}
}

func TestLegacyRecountReadAndWriteErrors(t *testing.T) {
	for _, mode := range []string{"tenant-read", "namespace-index", "namespace-read", "write"} {
		t.Run(mode, func(t *testing.T) {
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "2", capsulev1beta2.UsedAnnotationForResource(recountResource): "1"}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			r, _ := namespaceCleanupFixture(t, tnt, ns)
			base := r.Client
			failure := errors.New("injected API failure")
			c := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					switch obj.(type) {
					case *capsulev1beta2.Tenant:
						if mode == "tenant-read" {
							return failure
						}
					case *corev1.Namespace:
						if mode == "namespace-read" {
							return failure
						}
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if mode == "namespace-index" {
						return failure
					}
					return c.List(ctx, list, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if mode == "write" {
						return failure
					}
					return c.Update(ctx, obj, opts...)
				},
			})
			r.Client, r.reader = c, c
			require.ErrorIs(t, r.syncCustomResourceQuotaUsages(t.Context(), tnt), failure)
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, "1", current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
		})
	}
}

func TestLegacyRecountRetainsReservationsExpiringDuringEnumeration(t *testing.T) {
	for _, materialized := range []bool{false, true} {
		t.Run(fmt.Sprintf("materialized=%v", materialized), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "1", capsulev1beta2.UsedAnnotationForResource(recountResource): "1"}
			first, second := cleanupOwnedNamespace(tnt, "first", false), cleanupOwnedNamespace(tnt, "second", false)
			foreign := provisioningTenant()
			foreign.Name, foreign.UID = "tenant-b", "tenant-b-uid"
			now := metav1.Now()
			// Annotation timestamps have second precision. Leave a full second
			// for the recount to start, then block the later LIST across expiry.
			expiry := metav1.NewTime(time.Now().Truncate(time.Second).Add(2 * time.Second))
			state, err := capsuletenant.ReadLegacyQuotaReservations(nil)
			require.NoError(t, err)
			state.Pending[recountResource] = []capsuletenant.LegacyQuotaReservation{{ID: "held", NamespaceUID: first.UID, CreatedAt: now, UpdatedAt: now, ExpiresAt: &expiry, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: first.Name, Name: "held"}}}
			require.NoError(t, capsuletenant.WriteLegacyQuotaReservations(tnt.Annotations, state))
			r, dyn := namespaceCleanupFixture(t, tnt, first, second, foreign)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), tnt))
			blocked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			dyn.PrependReactor("list", "widgets", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetNamespace() == second.Name {
					once.Do(func() { close(blocked) })
					select {
					case <-release:
					case <-ctx.Done():
						return true, nil, ctx.Err()
					}
				}
				return false, nil, nil
			})
			done := make(chan error, 1)
			go func() { done <- r.syncCustomResourceQuotaUsages(ctx, tnt.DeepCopy()) }()
			select {
			case <-blocked:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.True(t, time.Now().Before(expiry.Time), "reservation must be live when the first namespace is enumerated")
			timer := time.NewTimer(time.Until(expiry.Time))
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if materialized {
				obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"namespace": first.Name, "name": "held"}}}
				// The first namespace's LIST already completed. Object persistence
				// must not create a Tenant conflict that would mask the expiry race.
				require.NoError(t, dyn.Tracker().Create(recountGVR, obj, first.Name))
			}
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, tnt.ResourceVersion, current.ResourceVersion)
			close(release)
			require.NoError(t, <-done)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, "1", current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			state, err = capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Len(t, state.Pending[recountResource], 1)
			denied := admitRecountWidget(ctx, r.Client, first, "second", "second")
			require.NotNil(t, denied)
			require.Contains(t, denied.Result.Message, "quota limit of 1")
			// The next count can safely observe the persisted object or release
			// the expired abandoned reservation without counting either twice.
			require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			want := "0"
			if materialized {
				want = "1"
			}
			require.Equal(t, want, current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			state, err = capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Empty(t, state.Pending)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(foreign), current))
			require.Equal(t, foreign.Annotations, current.Annotations)
		})
	}
}

func TestLegacyRecountMatchesGeneratedNameReservations(t *testing.T) {
	ctx := t.Context()
	tnt := provisioningTenant()
	tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "2"}
	ns := cleanupOwnedNamespace(tnt, "selected", false)
	r, dyn := namespaceCleanupFixture(t, tnt, ns)
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"namespace": ns.Name, "name": "widget-generated", "generateName": "widget-"}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	req := admission.Request{UID: "generated", Namespace: ns.Name, Resource: metav1.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}, Object: runtime.RawExtension{Raw: raw}}
	onCreate := generic.ResourceCounterHandler(r.Client).OnCreate(r.Client, r.Client, admission.NewDecoder(r.Scheme()), events.NewDiscardRecorder())
	require.Nil(t, onCreate(ctx, req))
	require.Nil(t, onCreate(ctx, req), "repeated admission uses the same resolved identity")
	_, err = dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, obj, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
	current := &capsulev1beta2.Tenant{}
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
	require.Equal(t, "1", current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
	state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
	require.NoError(t, err)
	require.Empty(t, state.Pending)
	require.Nil(t, admitRecountWidget(ctx, r.Client, ns, "next", "next"), "one generated object must leave the second slot available")
	denied := admitRecountWidget(ctx, r.Client, ns, "third", "third")
	require.NotNil(t, denied)
	require.Contains(t, denied.Result.Message, "quota limit of 2")
}

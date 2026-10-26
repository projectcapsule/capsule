// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

func TestLegacyQuotaPeriodicRecoveryAfterDelayedDelete(t *testing.T) {
	for _, mode := range []string{"full-reservations", "full-annotations", "expired-hint", "restart-without-hints"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			tnt, other := provisioningTenant(), provisioningTenant()
			other.Name, other.UID = "tenant-b", "tenant-b-uid"
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "1", capsulev1beta2.UsedAnnotationForResource(recountResource): "1"}
			ns, foreign := cleanupOwnedNamespace(tnt, "selected", false), cleanupOwnedNamespace(other, "foreign", false)
			now := metav1.Now()
			if mode == "full-reservations" {
				expires := metav1.NewTime(now.Add(time.Minute))
				entries := make([]map[string]any, 1024)
				for i := range entries {
					entries[i] = map[string]any{"id": fmt.Sprint(i), "namespaceUid": ns.UID, "expiresAt": expires, "objectRef": map[string]any{"namespace": ns.Name, "name": fmt.Sprintf("already-gone-%d", i)}}
				}
				raw, err := json.Marshal(map[string]any{"deleting": map[string]any{recountResource: entries}})
				require.NoError(t, err)
				tnt.Annotations[capsuletenant.LegacyQuotaReservationsAnnotation] = string(raw)
			}
			if mode == "expired-hint" {
				state, _ := capsuletenant.ReadLegacyQuotaReservations(nil)
				expires := metav1.NewTime(now.Add(-time.Second))
				state.Deleting = map[string][]capsuletenant.LegacyQuotaDeletion{recountResource: {{ID: "expired", NamespaceUID: ns.UID, CreatedAt: metav1.NewTime(now.Add(-3 * time.Minute)), ExpiresAt: &expires, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: "target", UID: "target-uid"}}}}
				require.NoError(t, capsuletenant.WriteLegacyQuotaReservations(tnt.Annotations, state))
			}
			if mode == "full-annotations" {
				size := len("unrelated.example.com/padding")
				for key, value := range tnt.Annotations {
					size += len(key) + len(value)
				}
				tnt.Annotations["unrelated.example.com/padding"] = strings.Repeat("x", apivalidation.TotalAnnotationSizeLimitB-size)
			}
			r, dyn := namespaceCleanupFixture(t, tnt, ns, other, foreign)
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"name": "target", "namespace": ns.Name, "uid": "target-uid"}}}
			_, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, obj, metav1.CreateOptions{})
			require.NoError(t, err)
			if strings.HasPrefix(mode, "full-") {
				raw, err := json.Marshal(obj)
				require.NoError(t, err)
				req := admission.Request{UID: "delayed-delete", Operation: admissionv1.Delete, Name: obj.GetName(), Namespace: ns.Name, Resource: metav1.GroupVersionResource{Group: recountGVR.Group, Version: recountGVR.Version, Resource: recountGVR.Resource}, OldObject: runtime.RawExtension{Raw: raw}}
				require.Nil(t, generic.ResourceCounterHandler(r.Client).OnDelete(r.Client, r.Client, admission.NewDecoder(r.Scheme()), events.NewDiscardRecorder())(ctx, req))
			}
			// Drain all transient entries and the last retry they scheduled while
			// the DELETE is still delayed by another webhook.
			request := reconcile.Request{Name: tnt.Name}
			for range 2 {
				result, err := r.reconcileResourceQuotas(ctx, request)
				require.NoError(t, err)
				require.Positive(t, result.RequeueAfter)
				require.LessOrEqual(t, result.RequeueAfter, legacyQuotaRecountInterval)
			}
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Empty(t, state.Deleting)
			require.Equal(t, "1", current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			denied := admitRecountWidget(ctx, r.Client, ns, "too-early", "too-early")
			require.NotNil(t, denied)
			require.Contains(t, denied.Result.Message, "quota limit of 1")
			// Another replica has no local deletion hints. Initial Tenant events
			// seed the same periodic queue on startup/leader replacement.
			require.True(t, legacyQuotaRecountPredicate().Create(event.CreateEvent{Object: current, IsInInitialList: true}))
			restarted := &Manager{Client: r.Client, reader: r.reader, DynamicClient: dyn, Metrics: r.Metrics, Log: r.Log, Configuration: r.Configuration}
			require.NoError(t, dyn.Resource(recountGVR).Namespace(ns.Name).Delete(ctx, obj.GetName(), metav1.DeleteOptions{}))
			result, err := restarted.reconcileResourceQuotas(ctx, request)
			require.NoError(t, err)
			require.Equal(t, legacyQuotaRecountInterval, result.RequeueAfter)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, "0", current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			if mode == "full-annotations" {
				delete(current.Annotations, "unrelated.example.com/padding")
				require.NoError(t, r.Update(ctx, current))
			}
			require.Nil(t, admitRecountWidget(ctx, r.Client, ns, "after-delete", "after-delete"))
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(other), current))
			require.Equal(t, other.Annotations, current.Annotations)
		})
	}
}

func TestLegacyQuotaRecoveryQueueSelection(t *testing.T) {
	limit := capsulev1beta2.LimitAnnotationForResource(recountResource)
	predicate := legacyQuotaRecountPredicate()
	for _, mode := range []string{"add", "change", "remove", "add-empty", "status", "usage", "tracking", "unrelated", "no-limit", "wrong-type", "nil"} {
		t.Run(mode, func(t *testing.T) {
			before := provisioningTenant()
			before.Annotations = map[string]string{limit: "1"}
			after := before.DeepCopy()
			want := false
			switch mode {
			case "add", "add-empty":
				delete(before.Annotations, limit)
				if mode == "add-empty" {
					after.Annotations[limit] = ""
				}
				want = true
			case "change":
				after.Annotations[limit] = "2"
				want = true
			case "remove":
				delete(after.Annotations, limit)
				want = true
			case "status":
				after.Status.Size++
			case "usage":
				after.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)] = "1"
			case "tracking":
				after.Annotations[capsuletenant.LegacyQuotaReservationsAnnotation] = "{}"
			case "unrelated":
				after.Annotations["example.com/unrelated"] = "changed"
			case "no-limit":
				delete(before.Annotations, limit)
				delete(after.Annotations, limit)
			case "wrong-type":
				require.False(t, predicate.Update(event.UpdateEvent{ObjectOld: &corev1.Namespace{}, ObjectNew: after}))
				require.False(t, predicate.Create(event.CreateEvent{Object: &corev1.Namespace{}}))
				return
			case "nil":
				require.False(t, predicate.Create(event.CreateEvent{}))
				require.False(t, predicate.Update(event.UpdateEvent{}))
				return
			}
			require.Equal(t, want, predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}))
			require.Equal(t, mode != "remove" && mode != "no-limit", predicate.Create(event.CreateEvent{Object: after, IsInInitialList: true}))
			require.False(t, predicate.Delete(event.DeleteEvent{Object: after}))
			require.False(t, predicate.Generic(event.GenericEvent{Object: after}))
		})
	}
	plain := provisioningTenant()
	require.Zero(t, legacyQuotaRecountAfter(plain, time.Now()), "tenants without legacy quotas must not be polled")
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

func TestLegacyQuotaSameNameReplacementAndDeletion(t *testing.T) {
	for _, recountBeforeDelete := range []bool{false, true} {
		t.Run(fmt.Sprintf("recount-before-delete=%v", recountBeforeDelete), func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "2", capsulev1beta2.UsedAnnotationForResource(recountResource): "1"}
			foreign := provisioningTenant()
			foreign.Name, foreign.UID = "tenant-b", "tenant-b-uid"
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			r, dyn := namespaceCleanupFixture(t, tnt, ns, foreign, cleanupOwnedNamespace(foreign, "foreign", false))
			object := func(name, uid string) *unstructured.Unstructured {
				return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"namespace": ns.Name, "name": name, "uid": uid}}}
			}
			old, incoming := object("shared", "old-uid"), object("shared", "replacement-uid")
			_, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, old, metav1.CreateOptions{})
			require.NoError(t, err)
			raw, err := json.Marshal(incoming)
			require.NoError(t, err)
			req := admission.Request{UID: "delayed-create", Operation: admissionv1.Create, Name: "shared", Namespace: ns.Name, Resource: metav1.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}, Object: runtime.RawExtension{Raw: raw}}
			handler := generic.ResourceCounterHandler(r.Client)
			decoder := admission.NewDecoder(r.Scheme())
			require.Nil(t, handler.OnCreate(r.Client, r.Client, decoder, events.NewDiscardRecorder())(ctx, req))
			if recountBeforeDelete {
				require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			}
			current := &capsulev1beta2.Tenant{}
			check := func(used string, pending, deleting int) {
				t.Helper()
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
				require.Equal(t, used, current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
				state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
				require.NoError(t, err)
				require.Len(t, state.Pending[recountResource], pending)
				require.Len(t, state.Deleting[recountResource], deleting)
			}
			check("2", 1, 0)
			oldRaw, err := json.Marshal(old)
			require.NoError(t, err)
			del := req
			del.Operation, del.UID, del.Object, del.OldObject = admissionv1.Delete, "delete-old", runtime.RawExtension{}, runtime.RawExtension{Raw: oldRaw}
			onDelete := handler.OnDelete(r.Client, r.Client, decoder, events.NewDiscardRecorder())
			for range 2 {
				require.Nil(t, onDelete(ctx, del))
			}
			check("2", 1, 1)
			// A later webhook can reject DELETE. Until storage changes, repeated
			// deletion admission and recounting must not release either slot.
			require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			check("2", 1, 1)
			denied := admitRecountWidget(ctx, r.Client, ns, "too-early", "too-early")
			require.NotNil(t, denied)
			require.Contains(t, denied.Result.Message, "quota limit of 2")
			require.NoError(t, dyn.Resource(recountGVR).Namespace(ns.Name).Delete(ctx, old.GetName(), metav1.DeleteOptions{}))
			require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			check("1", 1, 0)
			require.Nil(t, admitRecountWidget(ctx, r.Client, ns, "other", "other"))
			denied = admitRecountWidget(ctx, r.Client, ns, "overflow", "overflow")
			require.NotNil(t, denied)
			require.Contains(t, denied.Result.Message, "quota limit of 2")
			for _, item := range []*unstructured.Unstructured{incoming, object("other", "object-other")} {
				_, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, item, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			check("2", 0, 0)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(foreign), current))
			require.Equal(t, foreign.Annotations, current.Annotations)
		})
	}
}

func TestLegacyQuotaDeletionHintLifecycle(t *testing.T) {
	for _, mode := range []string{"present", "gone", "replaced", "terminating", "expired", "namespace-recreated", "limit-removed", "unknown-create-uid"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "3"}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			now := metav1.Now()
			expires := metav1.NewTime(now.Add(time.Minute))
			ref := capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: "widget", UID: "old"}
			state, err := capsuletenant.ReadLegacyQuotaReservations(nil)
			require.NoError(t, err)
			state.Deleting = map[string][]capsuletenant.LegacyQuotaDeletion{recountResource: {{ID: "delete", ObjectRef: ref, CreatedAt: now, NamespaceUID: ns.UID, ExpiresAt: &expires}}}
			item := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"name": ref.Name, "namespace": ns.Name, "uid": string(ref.UID)}}}
			wantUsed, wantHints := "1", 0
			switch mode {
			case "present":
				wantHints = 1
			case "gone":
				wantUsed = "0"
			case "replaced":
				item.SetUID("new")
			case "terminating":
				item.SetDeletionTimestamp(&now)
				wantUsed = "0"
				wantHints = 1
			case "expired":
				expires.Time = now.Add(-time.Second)
			case "namespace-recreated":
				ns.UID = "recreated"
				wantHints = 0
			case "limit-removed":
				delete(tnt.Annotations, capsulev1beta2.LimitAnnotationForResource(recountResource))
				wantUsed = ""
			case "unknown-create-uid":
				ref.UID = ""
				state.Pending[recountResource] = []capsuletenant.LegacyQuotaReservation{{NamespaceUID: ns.UID, ID: "legacy", CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires, ObjectRef: ref}}
				wantUsed, wantHints = "2", 1
			}
			require.NoError(t, capsuletenant.WriteLegacyQuotaReservations(tnt.Annotations, state))
			r, dyn := namespaceCleanupFixture(t, tnt, ns)
			if mode != "gone" {
				_, err := dyn.Resource(recountGVR).Namespace(ns.Name).Create(ctx, item, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			require.NoError(t, r.syncCustomResourceQuotaUsages(ctx, tnt))
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), current))
			require.Equal(t, wantUsed, current.Annotations[capsulev1beta2.UsedAnnotationForResource(recountResource)])
			state, err = capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Len(t, state.Deleting[recountResource], wantHints)
			if mode == "unknown-create-uid" {
				require.Len(t, state.Pending[recountResource], 1)
			}
		})
	}
}

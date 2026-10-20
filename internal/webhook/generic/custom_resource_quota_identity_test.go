// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestLegacyQuotaObjectUIDAndDeleteSafety(t *testing.T) {
	for _, mode := range []string{"changed-create-uid", "changed-name", "wrong-namespace", "delete-older", "delete-matching", "delete-unknown", "duplicate-delete", "dry-run", "malformed-old", "capacity-delete", "capacity-create"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			c := legacyQuotaFixture(t, 2)
			req := legacyQuotaRequest(admissionv1.Create)
			req.UID = "first"
			require.Nil(t, invokeLegacyQuota(ctx, c, c, req))
			current := &capsulev1beta2.Tenant{}
			require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "tenant-0"}, current))
			if strings.HasPrefix(mode, "capacity-") {
				// Seed valid compact legacy JSON at the entry bound. Its next
				// write must enforce both CREATE and DELETE coordination limits.
				expiry := metav1.NewTime(time.Now().Add(time.Minute))
				entries := make([]map[string]any, 1024)
				for i := range entries {
					entries[i] = map[string]any{"id": fmt.Sprint(i), "namespaceUid": "tenant-0-ns", "objectRef": map[string]any{"namespace": "tenant-0-ns", "name": "held"}, "expiresAt": expiry}
				}
				raw, err := json.Marshal(map[string]any{"pending": map[string]any{quotaTestResource: entries}})
				require.NoError(t, err)
				current.Annotations[tenant.LegacyQuotaReservationsAnnotation] = string(raw)
				require.NoError(t, c.Update(ctx, current))
			}
			before := current.DeepCopy()
			req.Operation = admissionv1.Delete
			switch mode {
			case "changed-create-uid":
				req.Operation = admissionv1.Create
				req.Object = quotaTestObject(req.Namespace, req.Name, "different-uid")
			case "changed-name":
				req.Operation = admissionv1.Create
				req.Object = quotaTestObject(req.Namespace, "different-name", "widget-uid")
			case "wrong-namespace":
				req.Operation = admissionv1.Create
				req.Object = quotaTestObject("tenant-1-ns", req.Name, "widget-uid")
			case "delete-older":
				req.OldObject = quotaTestObject(req.Namespace, req.Name, "old-uid")
			case "delete-unknown":
				req.OldObject = quotaTestObject(req.Namespace, req.Name, "")
			case "dry-run":
				req.DryRun = new(true)
			case "malformed-old":
				req.OldObject.Raw = []byte("{")
			case "capacity-create":
				req.Operation = admissionv1.Create
				req.UID = "next"
			}
			response := invokeLegacyQuota(ctx, c, c, req)
			rejected := mode == "changed-create-uid" || mode == "changed-name" || mode == "wrong-namespace" || mode == "malformed-old" || mode == "capacity-create"
			if rejected {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				if mode == "capacity-create" {
					require.Contains(t, response.Result.Message, "capacity exceeded")
				}
				if mode == "changed-create-uid" || mode == "changed-name" || mode == "wrong-namespace" {
					require.Contains(t, response.Result.Message, "identity")
				}
			} else {
				require.Nil(t, response)
			}
			if mode == "duplicate-delete" {
				req.UID = "second-delete"
				require.Nil(t, invokeLegacyQuota(ctx, c, c, req))
			}
			require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "tenant-0"}, current))
			require.Equal(t, "2", current.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)], "admission must not release capacity before storage confirms deletion")
			if rejected || mode == "dry-run" || mode == "capacity-delete" {
				require.Equal(t, before, current)
				return
			}
			state, err := tenant.ReadLegacyQuotaReservations(current.Annotations)
			require.NoError(t, err)
			require.Len(t, state.Deleting[quotaTestResource], 1)
			wantPending := 0
			if mode == "delete-older" || mode == "delete-unknown" {
				wantPending = 1
			}
			require.Len(t, state.Pending[quotaTestResource], wantPending)
		})
	}
}

func TestLegacyQuotaDeleteDecodesOnceAcrossConflicts(t *testing.T) {
	base := legacyQuotaFixture(t, 1)
	writes := 0
	c := interceptor.NewClient(base, interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		writes++
		if writes == 1 {
			return apierrors.NewConflict(schema.GroupResource{Resource: "tenants"}, "tenant-0", errors.New("retry"))
		}
		return c.Update(ctx, obj, opts...)
	}})
	req := legacyQuotaRequest(admissionv1.Delete)
	req.UID = types.UID("deletion")
	decoder := &quotaCountingDecoder{Decoder: admission.NewDecoder(c.Scheme())}
	handler := ResourceCounterHandler(c).OnDelete(c, c, decoder, events.NewDiscardRecorder())
	require.Nil(t, handler(t.Context(), req))
	require.Nil(t, handler(t.Context(), req))
	require.Equal(t, 2, decoder.calls, "one decode per request, not per conflict retry")
	require.Equal(t, 2, writes, "duplicate DELETE must not write or decrement again")
	current := &capsulev1beta2.Tenant{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant-0"}, current))
	state, err := tenant.ReadLegacyQuotaReservations(current.Annotations)
	require.NoError(t, err)
	require.Len(t, state.Deleting[quotaTestResource], 1)
	require.Equal(t, "1", current.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)])
}

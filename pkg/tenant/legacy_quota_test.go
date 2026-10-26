// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestLegacyQuotaTotalAnnotationBudget(t *testing.T) {
	for _, mode := range []string{"below", "exact", "over", "replace", "remove", "usage-growth"} {
		t.Run(mode, func(t *testing.T) {
			state, err := ReadLegacyQuotaReservations(nil)
			require.NoError(t, err)
			state.Pending["widgets.example.com_v1"] = []LegacyQuotaReservation{testLegacyReservation(time.Now())}
			annotations := map[string]string{"used": "9"}
			require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
			size := len("unrelated.example.com/padding")
			for key, value := range annotations {
				size += len(key) + len(value)
			}
			padding := apivalidation.TotalAnnotationSizeLimitB - size
			if mode == "below" {
				padding--
			}
			if mode == "over" {
				padding++
			}
			annotations["unrelated.example.com/padding"] = strings.Repeat("x", padding)
			if mode == "replace" {
				annotations[LegacyQuotaReservationsAnnotation] = "{}"
			}
			if mode == "remove" {
				clear(state.Pending)
			}
			if mode == "usage-growth" {
				annotations["used"] = "10"
			}
			before := maps.Clone(annotations)
			err = WriteLegacyQuotaReservations(annotations, state)
			if mode == "over" || mode == "usage-growth" {
				require.ErrorIs(t, err, ErrLegacyQuotaStateCapacity)
				require.Equal(t, before, annotations, "failed preflight must restore the previous reservation value")
			} else {
				require.NoError(t, err)
				require.NoError(t, apivalidation.ValidateAnnotationsSize(annotations))
				if mode == "remove" {
					require.NotContains(t, annotations, LegacyQuotaReservationsAnnotation)
				}
			}
			require.Equal(t, before["unrelated.example.com/padding"], annotations["unrelated.example.com/padding"])
		})
	}
}

func testLegacyReservation(now time.Time) LegacyQuotaReservation {
	expires := metav1.NewTime(now.Add(2 * time.Minute))
	return LegacyQuotaReservation{NamespaceUID: "space-uid", ID: "request", CreatedAt: metav1.NewTime(now), ExpiresAt: &expires, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: "space", Name: "widget"}}
}

func TestLegacyQuotaReservationState(t *testing.T) {
	for _, raw := range []string{"null", "[]", "{", `{"namespaces":{"space":""}}`, `{"pending":{"widgets.example.com_v1":[{"id":"request"}]}}`, `{"deleting":{"widgets.example.com_v1":[{"id":"request"}]}}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := ReadLegacyQuotaReservations(map[string]string{LegacyQuotaReservationsAnnotation: raw})
			require.Error(t, err)
		})
	}
	annotations := map[string]string{"unrelated": "preserved"}
	state, err := ReadLegacyQuotaReservations(nil)
	require.NoError(t, err)
	require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
	require.NotContains(t, annotations, LegacyQuotaReservationsAnnotation)
	state.Pending["widgets.example.com_v1"] = []LegacyQuotaReservation{testLegacyReservation(time.Now().UTC().Truncate(time.Second))}
	state.Namespaces["space"] = "space-uid"
	require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
	decoded, err := ReadLegacyQuotaReservations(annotations)
	require.NoError(t, err)
	require.Equal(t, state.Namespaces, decoded.Namespaces)
	require.Equal(t, state.Pending["widgets.example.com_v1"][0].ID, decoded.Pending["widgets.example.com_v1"][0].ID)
	require.True(t, state.Pending["widgets.example.com_v1"][0].ExpiresAt.Equal(decoded.Pending["widgets.example.com_v1"][0].ExpiresAt))
	decoded.Pending["widgets.example.com_v1"][0].ID = "changed"
	again, err := ReadLegacyQuotaReservations(annotations)
	require.NoError(t, err)
	require.Equal(t, "request", again.Pending["widgets.example.com_v1"][0].ID, "reads must not share mutable state")
	clear(state.Pending)
	clear(state.Namespaces)
	require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
	require.Equal(t, map[string]string{"unrelated": "preserved"}, annotations)
}

func TestLegacyQuotaDeletionStateAndBounds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reservation := testLegacyReservation(now)
	deletion := LegacyQuotaDeletion{ID: "delete", ObjectRef: reservation.ObjectRef, CreatedAt: reservation.CreatedAt, NamespaceUID: reservation.NamespaceUID, ExpiresAt: reservation.ExpiresAt}
	state := &LegacyQuotaReservations{Deleting: map[string][]LegacyQuotaDeletion{"widgets.example.com_v1": {deletion}}}
	annotations := map[string]string{}
	require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
	decoded, err := ReadLegacyQuotaReservations(annotations)
	require.NoError(t, err)
	require.Equal(t, deletion.ID, decoded.Deleting["widgets.example.com_v1"][0].ID)
	require.Equal(t, deletion.NamespaceUID, decoded.Deleting["widgets.example.com_v1"][0].NamespaceUID)
	require.Equal(t, 2*time.Second, LegacyQuotaRequeueAfter(annotations, now))
	decoded.Deleting["widgets.example.com_v1"][0].ID = "mutated"
	again, err := ReadLegacyQuotaReservations(annotations)
	require.NoError(t, err)
	require.Equal(t, "delete", again.Deleting["widgets.example.com_v1"][0].ID)
	state.Pending = map[string][]LegacyQuotaReservation{"widgets.example.com_v1": make([]LegacyQuotaReservation, 1024)}
	require.ErrorIs(t, WriteLegacyQuotaReservations(annotations, state), ErrLegacyQuotaStateCapacity)
	clear(state.Pending)
	clear(state.Deleting)
	require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
	require.Empty(t, annotations)
}

func TestLegacyQuotaReservationBounds(t *testing.T) {
	state, _ := ReadLegacyQuotaReservations(nil)
	state.Pending["widgets.example.com_v1"] = make([]LegacyQuotaReservation, 1025)
	require.ErrorContains(t, WriteLegacyQuotaReservations(map[string]string{}, state), "too many pending")
	state.Pending["widgets.example.com_v1"] = []LegacyQuotaReservation{testLegacyReservation(time.Now())}
	state.Namespaces[strings.Repeat("x", 192*1024)] = "uid"
	require.ErrorContains(t, WriteLegacyQuotaReservations(map[string]string{}, state), "metadata budget")
}

func TestLegacyQuotaReservationRequeue(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	require.Zero(t, LegacyQuotaRequeueAfter(nil, now))
	require.Equal(t, 2*time.Second, LegacyQuotaRequeueAfter(map[string]string{LegacyQuotaReservationsAnnotation: "invalid"}, now))
	for _, tc := range []struct {
		name                 string
		age, remaining, want time.Duration
		pin                  bool
	}{
		{"new", 0, time.Minute, 2 * time.Second, false},
		{"waiting", 10 * time.Second, time.Minute, 5 * time.Second, false},
		{"old", time.Minute, time.Minute, 15 * time.Second, false},
		{"near-expiry", time.Minute, time.Second, time.Second, false},
		{"expired", 3 * time.Minute, -time.Minute, time.Millisecond, false},
		{"namespace-not-yet-in-status", time.Minute, time.Minute, 2 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := testLegacyReservation(now.Add(-tc.age))
			expiry := metav1.NewTime(now.Add(tc.remaining))
			entry.ExpiresAt = &expiry
			state := &LegacyQuotaReservations{Pending: map[string][]LegacyQuotaReservation{"widgets.example.com_v1": {entry}}, Namespaces: map[string]types.UID{}}
			if tc.pin {
				state.Namespaces["space"] = "space-uid"
			}
			annotations := map[string]string{}
			require.NoError(t, WriteLegacyQuotaReservations(annotations, state))
			require.Equal(t, tc.want, LegacyQuotaRequeueAfter(annotations, now))
		})
	}
}

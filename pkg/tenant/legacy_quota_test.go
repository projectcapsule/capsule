// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func testLegacyReservation(now time.Time) LegacyQuotaReservation {
	expires := metav1.NewTime(now.Add(2 * time.Minute))
	return LegacyQuotaReservation{NamespaceUID: "space-uid", ID: "request", CreatedAt: metav1.NewTime(now), ExpiresAt: &expires, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: "space", Name: "widget"}}
}

func TestLegacyQuotaReservationState(t *testing.T) {
	for _, raw := range []string{"null", "[]", "{", `{"namespaces":{"space":""}}`, `{"pending":{"widgets.example.com_v1":[{"id":"request"}]}}`} {
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

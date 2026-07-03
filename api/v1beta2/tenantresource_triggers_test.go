// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2_test

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func trigger(apiGroups []string, kinds []string, ops ...capsulev1beta2.TriggerOperation) capsulev1beta2.TriggerSpec {
	return capsulev1beta2.TriggerSpec{
		VersionKinds: capruntime.VersionKinds{APIGroups: apiGroups, Kinds: kinds},
		Operations:   ops,
	}
}

func TestTriggerSpec_MatchesOperation(t *testing.T) {
	tests := []struct {
		name string
		spec capsulev1beta2.TriggerSpec
		op   capsulev1beta2.TriggerOperation
		want bool
	}{
		{
			name: "empty operations matches every op",
			spec: trigger(nil, []string{"Secret"}),
			op:   capsulev1beta2.TriggerOperationDelete,
			want: true,
		},
		{
			name: "listed operation matches",
			spec: trigger(nil, []string{"Secret"}, capsulev1beta2.TriggerOperationCreate, capsulev1beta2.TriggerOperationUpdate),
			op:   capsulev1beta2.TriggerOperationUpdate,
			want: true,
		},
		{
			name: "unlisted operation does not match",
			spec: trigger(nil, []string{"Secret"}, capsulev1beta2.TriggerOperationCreate),
			op:   capsulev1beta2.TriggerOperationDelete,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.MatchesOperation(tc.op); got != tc.want {
				t.Fatalf("MatchesOperation(%q) = %v, want %v", tc.op, got, tc.want)
			}
		})
	}
}

func TestTriggerVersionKinds(t *testing.T) {
	spec := capsulev1beta2.TenantResourceCommonSpec{
		Triggers: []capsulev1beta2.TriggerSpec{
			// Empty apiGroups means core v1.
			trigger(nil, []string{"Secret"}),
			// Duplicate selector must be de-duplicated.
			trigger([]string{"v1"}, []string{"Secret"}, capsulev1beta2.TriggerOperationUpdate),
			// Concrete group/version.
			trigger([]string{"apps/v1"}, []string{"Deployment"}),
			// Bare group expands to a version-less selector.
			trigger([]string{"batch"}, []string{"Job", "CronJob"}),
			// Empty kind must be dropped.
			trigger([]string{"v1"}, []string{""}),
		},
	}

	got := spec.TriggerVersionKinds()

	want := map[capruntime.VersionKind]struct{}{
		{APIVersion: "", Kind: "Secret"}:            {},
		{APIVersion: "apps/v1", Kind: "Deployment"}: {},
		{APIVersion: "batch/*", Kind: "Job"}:        {},
		{APIVersion: "batch/*", Kind: "CronJob"}:    {},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d selectors, got %d: %v", len(want), len(got), got)
	}

	for _, vk := range got {
		if _, ok := want[vk]; !ok {
			t.Fatalf("unexpected selector %+v", vk)
		}
	}
}

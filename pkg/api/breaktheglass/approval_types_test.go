// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestApprovalSpecIsApprover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     breaktheglass.ApprovalSpec
		username string
		groups   []string
		want     bool
	}{
		{name: "unrestricted when omitted", username: "alice", want: true},
		{
			name:     "user",
			spec:     breaktheglass.ApprovalSpec{Approvers: rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}}},
			username: "alice",
			want:     true,
		},
		{
			name:     "group",
			spec:     breaktheglass.ApprovalSpec{Approvers: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "on-call"}}},
			username: "alice",
			groups:   []string{"developers", "on-call"},
			want:     true,
		},
		{
			name: "service account",
			spec: breaktheglass.ApprovalSpec{Approvers: rbac.UserListSpec{{
				Kind: rbac.ServiceAccountOwner,
				Name: "system:serviceaccount:operations:reviewer",
			}}},
			username: "system:serviceaccount:operations:reviewer",
			want:     true,
		},
		{
			name:     "not listed",
			spec:     breaktheglass.ApprovalSpec{Approvers: rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}}},
			username: "bob",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.spec.IsApprover(tt.username, tt.groups); got != tt.want {
				t.Fatalf("IsApprover() = %v, want %v", got, tt.want)
			}
		})
	}
}

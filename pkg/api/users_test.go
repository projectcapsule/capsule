// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestUserSpec_Subject_ServiceAccount(t *testing.T) {
	tests := []struct {
		name string
		in   api.UserSpec
		want rbacv1.Subject
	}{
		{
			name: "system serviceaccount format",
			in: api.UserSpec{
				Kind: api.ServiceAccountOwner,
				Name: "system:serviceaccount:capsule-system:capsule",
			},
			want: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Namespace: "capsule-system",
				Name:      "capsule",
			},
		},
		{
			name: "minimal ns:name style (still splits from end)",
			in: api.UserSpec{
				Kind: api.ServiceAccountOwner,
				Name: "ns:sa",
			},
			want: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Namespace: "ns",
				Name:      "sa",
			},
		},
		{
			name: "extra segments (uses last two)",
			in: api.UserSpec{
				Kind: api.ServiceAccountOwner,
				Name: "a:b:c:d",
			},
			want: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Namespace: "c",
				Name:      "d",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Subject()
			if got != tt.want {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestUserSpec_Subject_UserAndGroup(t *testing.T) {
	tests := []struct {
		name string
		in   api.UserSpec
		want rbacv1.Subject
	}{
		{
			name: "user subject",
			in: api.UserSpec{
				Kind: api.UserOwner,
				Name: "alice",
			},
			want: rbacv1.Subject{
				APIGroup: rbacv1.GroupName,
				Kind:     "User",
				Name:     "alice",
			},
		},
		{
			name: "group subject",
			in: api.UserSpec{
				Kind: api.GroupOwner,
				Name: "devops",
			},
			want: rbacv1.Subject{
				APIGroup: rbacv1.GroupName,
				Kind:     "Group",
				Name:     "devops",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Subject()
			if got != tt.want {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

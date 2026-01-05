// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestRoleBindingHashFunc_Deterministic(t *testing.T) {
	b := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects: []rbacv1.Subject{
			{Kind: "User", Name: "alice"},
			{Kind: "Group", Name: "devops"},
		},
	}

	h1 := utils.RoleBindingHashFunc(b)
	h2 := utils.RoleBindingHashFunc(b)

	if h1 != h2 {
		t.Fatalf("expected deterministic hash, got %q and %q", h1, h2)
	}
	if h1 == "" {
		t.Fatalf("expected non-empty hash")
	}
}

func TestRoleBindingHashFunc_ChangesWhenClusterRoleChanges(t *testing.T) {
	b1 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects:        []rbacv1.Subject{{Kind: "User", Name: "alice"}},
	}
	b2 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "view",
		Subjects:        []rbacv1.Subject{{Kind: "User", Name: "alice"}},
	}

	h1 := utils.RoleBindingHashFunc(b1)
	h2 := utils.RoleBindingHashFunc(b2)

	if h1 == h2 {
		t.Fatalf("expected different hashes when ClusterRoleName changes, got %q", h1)
	}
}

func TestRoleBindingHashFunc_ChangesWhenSubjectKindChanges(t *testing.T) {
	b1 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects:        []rbacv1.Subject{{Kind: "User", Name: "alice"}},
	}
	b2 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects:        []rbacv1.Subject{{Kind: "Group", Name: "alice"}},
	}

	h1 := utils.RoleBindingHashFunc(b1)
	h2 := utils.RoleBindingHashFunc(b2)

	if h1 == h2 {
		t.Fatalf("expected different hashes when subject Kind changes, got %q", h1)
	}
}

func TestRoleBindingHashFunc_ChangesWhenSubjectNameChanges(t *testing.T) {
	b1 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects:        []rbacv1.Subject{{Kind: "User", Name: "alice"}},
	}
	b2 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects:        []rbacv1.Subject{{Kind: "User", Name: "bob"}},
	}

	h1 := utils.RoleBindingHashFunc(b1)
	h2 := utils.RoleBindingHashFunc(b2)

	if h1 == h2 {
		t.Fatalf("expected different hashes when subject Name changes, got %q", h1)
	}
}

func TestRoleBindingHashFunc_EmptyInputsStillProduceHash(t *testing.T) {
	b := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "",
		Subjects:        nil,
	}

	h := utils.RoleBindingHashFunc(b)
	if h == "" {
		t.Fatalf("expected non-empty hash even for empty input")
	}
}

func TestRoleBindingHashFunc_SubjectOrderMatters_CurrentBehavior(t *testing.T) {
	// This test documents the CURRENT behavior:
	// the hash is order-dependent because subjects are written in slice order.
	b1 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects: []rbacv1.Subject{
			{Kind: "User", Name: "alice"},
			{Kind: "Group", Name: "devops"},
		},
	}
	b2 := api.AdditionalRoleBindingsSpec{
		ClusterRoleName: "admin",
		Subjects: []rbacv1.Subject{
			{Kind: "Group", Name: "devops"},
			{Kind: "User", Name: "alice"},
		},
	}

	h1 := utils.RoleBindingHashFunc(b1)
	h2 := utils.RoleBindingHashFunc(b2)

	if h1 == h2 {
		t.Fatalf("expected different hashes when subject order changes (current behavior), got %q", h1)
	}
}

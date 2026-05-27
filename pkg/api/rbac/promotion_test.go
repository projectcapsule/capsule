// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestPromotionSpec_ToAdditionalRolebindings(t *testing.T) {
	tests := []struct {
		name      string
		promotion PromotionSpec
		expected  []AdditionalRoleBindingsWithNamespaceSpec
	}{
		{
			name: "creates rolebindings for every target and clusterrole",
			promotion: PromotionSpec{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source-ns:gitops",
				},
				ClusterRoles: []string{"configmap-replicator", "secret-replicator"},
				Targets:      []string{"target-a", "target-b"},
			},
			expected: []AdditionalRoleBindingsWithNamespaceSpec{
				{
					Namespace: meta.RFC1123SubdomainName("target-a"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "configmap-replicator",
						Subjects: []rbacv1.Subject{
							{
								Kind:      rbacv1.ServiceAccountKind,
								Name:      "gitops",
								Namespace: "source-ns",
							},
						},
					},
				},
				{
					Namespace: meta.RFC1123SubdomainName("target-a"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "secret-replicator",
						Subjects: []rbacv1.Subject{
							{
								Kind:      rbacv1.ServiceAccountKind,
								Name:      "gitops",
								Namespace: "source-ns",
							},
						},
					},
				},
				{
					Namespace: meta.RFC1123SubdomainName("target-b"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "configmap-replicator",
						Subjects: []rbacv1.Subject{
							{
								Kind:      rbacv1.ServiceAccountKind,
								Name:      "gitops",
								Namespace: "source-ns",
							},
						},
					},
				},
				{
					Namespace: meta.RFC1123SubdomainName("target-b"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "secret-replicator",
						Subjects: []rbacv1.Subject{
							{
								Kind:      rbacv1.ServiceAccountKind,
								Name:      "gitops",
								Namespace: "source-ns",
							},
						},
					},
				},
			},
		},
		{
			name: "returns empty list without targets",
			promotion: PromotionSpec{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source-ns:gitops",
				},
				ClusterRoles: []string{"configmap-replicator"},
			},
			expected: []AdditionalRoleBindingsWithNamespaceSpec{},
		},
		{
			name: "returns empty list without clusterroles",
			promotion: PromotionSpec{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source-ns:gitops",
				},
				Targets: []string{"target-a"},
			},
			expected: []AdditionalRoleBindingsWithNamespaceSpec{},
		},
		{
			name: "creates user subject",
			promotion: PromotionSpec{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
			expected: []AdditionalRoleBindingsWithNamespaceSpec{
				{
					Namespace: meta.RFC1123SubdomainName("target-a"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "view",
						Subjects: []rbacv1.Subject{
							{
								Kind:     rbacv1.UserKind,
								Name:     "alice",
								APIGroup: rbacv1.GroupName,
							},
						},
					},
				},
			},
		},
		{
			name: "creates group subject",
			promotion: PromotionSpec{
				UserSpec: UserSpec{
					Kind: GroupOwner,
					Name: "developers",
				},
				ClusterRoles: []string{"edit"},
				Targets:      []string{"target-a"},
			},
			expected: []AdditionalRoleBindingsWithNamespaceSpec{
				{
					Namespace: meta.RFC1123SubdomainName("target-a"),
					AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
						ClusterRoleName: "edit",
						Subjects: []rbacv1.Subject{
							{
								Kind:     rbacv1.GroupKind,
								Name:     "developers",
								APIGroup: rbacv1.GroupName,
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.promotion.ToAdditionalRolebindings()

			if !equalAdditionalRoleBindingsWithNamespace(got, tt.expected) {
				t.Fatalf("unexpected rolebindings\nexpected: %#v\ngot:      %#v", tt.expected, got)
			}
		})
	}
}

func equalAdditionalRoleBindingsWithNamespace(a, b []AdditionalRoleBindingsWithNamespaceSpec) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Namespace != b[i].Namespace {
			return false
		}

		if a[i].ClusterRoleName != b[i].ClusterRoleName {
			return false
		}

		if !equalSubjects(a[i].Subjects, b[i].Subjects) {
			return false
		}
	}

	return true
}

func equalSubjects(a, b []rbacv1.Subject) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Kind != b[i].Kind {
			return false
		}

		if a[i].Name != b[i].Name {
			return false
		}

		if a[i].Namespace != b[i].Namespace {
			return false
		}

		if a[i].APIGroup != b[i].APIGroup {
			return false
		}
	}

	return true
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"testing"
)

func TestPromotionStatusListSpec_Upsert(t *testing.T) {
	t.Run("adds new promotion", func(t *testing.T) {
		promotions := PromotionStatusListSpec{}

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"view"},
			Targets:      []string{"target-a"},
		})

		expected := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source:gitops",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
		}

		if !equalPromotions(promotions, expected) {
			t.Fatalf("unexpected promotions\nexpected: %#v\ngot:      %#v", expected, promotions)
		}
	})

	t.Run("merges clusterroles for same kind name and targets", func(t *testing.T) {
		promotions := PromotionStatusListSpec{}

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"secret-replicator"},
			Targets:      []string{"target-b", "target-a"},
		})

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"configmap-replicator"},
			Targets:      []string{"target-a", "target-b"},
		})

		expected := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source:gitops",
				},
				ClusterRoles: []string{"configmap-replicator", "secret-replicator"},
				Targets:      []string{"target-a", "target-b"},
			},
		}

		if !equalPromotions(promotions, expected) {
			t.Fatalf("unexpected promotions\nexpected: %#v\ngot:      %#v", expected, promotions)
		}
	})

	t.Run("keeps dedicated entries for same owner with different targets", func(t *testing.T) {
		promotions := PromotionStatusListSpec{}

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"view"},
			Targets:      []string{"target-a", "target-b"},
		})

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"secret-replicator"},
			Targets:      []string{"target-b"},
		})

		expected := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source:gitops",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a", "target-b"},
			},
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source:gitops",
				},
				ClusterRoles: []string{"secret-replicator"},
				Targets:      []string{"target-b"},
			},
		}

		if !equalPromotions(promotions, expected) {
			t.Fatalf("unexpected promotions\nexpected: %#v\ngot:      %#v", expected, promotions)
		}
	})

	t.Run("deduplicates clusterroles and targets", func(t *testing.T) {
		promotions := PromotionStatusListSpec{}

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"view", "view", "edit"},
			Targets:      []string{"target-b", "target-a", "target-a"},
		})

		expected := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"edit", "view"},
				Targets:      []string{"target-a", "target-b"},
			},
		}

		if !equalPromotions(promotions, expected) {
			t.Fatalf("unexpected promotions\nexpected: %#v\ngot:      %#v", expected, promotions)
		}
	})

	t.Run("sorts promotions by kind name and targets", func(t *testing.T) {
		promotions := PromotionStatusListSpec{}

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: UserOwner,
				Name: "bob",
			},
			ClusterRoles: []string{"view"},
			Targets:      []string{"target-b"},
		})

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: ServiceAccountOwner,
				Name: "system:serviceaccount:source:gitops",
			},
			ClusterRoles: []string{"view"},
			Targets:      []string{"target-a"},
		})

		promotions.Upsert(PromotionSpec{
			UserSpec: UserSpec{
				Kind: UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"view"},
			Targets:      []string{"target-a"},
		})

		expected := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "system:serviceaccount:source:gitops",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "bob",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-b"},
			},
		}

		if !equalPromotions(promotions, expected) {
			t.Fatalf("unexpected promotions\nexpected: %#v\ngot:      %#v", expected, promotions)
		}
	})
}

func TestPromotionStatusListSpec_FindUser(t *testing.T) {
	t.Run("finds and aggregates promotions for user", func(t *testing.T) {
		promotions := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a", "target-b"},
			},
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"edit"},
				Targets:      []string{"target-b", "target-c"},
			},
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "bob",
				},
				ClusterRoles: []string{"admin"},
				Targets:      []string{"target-d"},
			},
		}

		got, found := promotions.FindUser("alice", UserOwner)
		if !found {
			t.Fatal("expected user to be found")
		}

		expected := PromotionSpec{
			UserSpec: UserSpec{
				Kind: UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"edit", "view"},
			Targets:      []string{"target-a", "target-b", "target-c"},
		}

		if !equalPromotion(got, expected) {
			t.Fatalf("unexpected promotion\nexpected: %#v\ngot:      %#v", expected, got)
		}
	})

	t.Run("does not find user with different kind", func(t *testing.T) {
		promotions := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: ServiceAccountOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
		}

		_, found := promotions.FindUser("alice", UserOwner)
		if found {
			t.Fatal("expected user not to be found for different kind")
		}
	})

	t.Run("returns false when user does not exist", func(t *testing.T) {
		promotions := PromotionStatusListSpec{
			{
				UserSpec: UserSpec{
					Kind: UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"view"},
				Targets:      []string{"target-a"},
			},
		}

		got, found := promotions.FindUser("missing", UserOwner)
		if found {
			t.Fatal("expected user not to be found")
		}

		if !equalPromotion(got, PromotionSpec{}) {
			t.Fatalf("expected zero promotion, got %#v", got)
		}
	})
}

func TestMergeSortedStrings(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		incoming []string
		expected []string
	}{
		{
			name:     "returns nil when both slices are empty",
			existing: nil,
			incoming: nil,
			expected: nil,
		},
		{
			name:     "merges sorts and deduplicates values",
			existing: []string{"b", "a"},
			incoming: []string{"c", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "handles empty existing values",
			existing: nil,
			incoming: []string{"b", "a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "handles empty incoming values",
			existing: []string{"b", "a", "b"},
			incoming: nil,
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSortedStrings(tt.existing, tt.incoming)

			if !equalStrings(got, tt.expected) {
				t.Fatalf("unexpected strings\nexpected: %#v\ngot:      %#v", tt.expected, got)
			}
		})
	}
}

func equalPromotions(a, b PromotionStatusListSpec) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !equalPromotion(a[i], b[i]) {
			return false
		}
	}

	return true
}

func equalPromotion(a, b PromotionSpec) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		equalStrings(a.ClusterRoles, b.ClusterRoles) &&
		equalStrings(a.Targets, b.Targets)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

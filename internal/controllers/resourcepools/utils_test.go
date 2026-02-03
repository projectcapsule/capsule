// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// ---------- helpers ----------

func q(s string) resource.Quantity { return resource.MustParse(s) }

func rl(m map[corev1.ResourceName]string) corev1.ResourceList {
	out := corev1.ResourceList{}
	for k, v := range m {
		out[k] = q(v)
	}
	return out
}

func claim(t *testing.T, uid, ns, name string, ts time.Time, req corev1.ResourceList) capsulev1beta2.ResourcePoolClaim {
	t.Helper()

	return capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			UID:               types.UID(uid),
			Namespace:         ns,
			Name:              name,
			CreationTimestamp: metav1.NewTime(ts),
		},
		Spec: capsulev1beta2.ResourcePoolClaimSpec{
			ResourceClaims: req,
		},
	}
}

func setEq(got map[string]struct{}, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			return false
		}
	}
	return true
}

// ---------- filterResourceListByKeys tests ----------

func TestFilterResourceListByKeys(t *testing.T) {
	t.Parallel()

	t.Run("empty keys returns empty", func(t *testing.T) {
		t.Parallel()

		in := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "100m",
			corev1.ResourceMemory: "128Mi",
		})
		keys := corev1.ResourceList{}

		got := filterResourceListByKeys(in, keys)
		if len(got) != 0 {
			t.Fatalf("expected empty result, got=%v", got)
		}
	})

	t.Run("filters by intersection", func(t *testing.T) {
		t.Parallel()

		in := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "250m",
			corev1.ResourceMemory: "512Mi",
			corev1.ResourcePods:   "10",
		})
		keys := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:  "0", // values ignored, only keys matter
			corev1.ResourcePods: "0",
		})

		got := filterResourceListByKeys(in, keys)
		if len(got) != 2 {
			t.Fatalf("expected 2 keys, got=%v", got)
		}
		cpu := got[corev1.ResourceCPU]
		if cpu.Cmp(q("250m")) != 0 {
			t.Fatalf("expected cpu=250m, got=%v", cpu.String())
		}

		pods := got[corev1.ResourcePods]
		if pods.Cmp(q("10")) != 0 {
			t.Fatalf("expected pods=10, got=%v", pods.String())
		}
		if _, ok := got[corev1.ResourceMemory]; ok {
			t.Fatalf("did not expect memory in result, got=%v", got)
		}
	})

	t.Run("keys not present in input are ignored", func(t *testing.T) {
		t.Parallel()

		in := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "100m",
		})
		keys := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "0",
			corev1.ResourceMemory: "0",
		})

		got := filterResourceListByKeys(in, keys)
		if len(got) != 1 {
			t.Fatalf("expected 1 key, got=%v", got)
		}
		if _, ok := got[corev1.ResourceCPU]; !ok {
			t.Fatalf("expected cpu key present")
		}
		if _, ok := got[corev1.ResourceMemory]; ok {
			t.Fatalf("did not expect memory key present")
		}
	})
}

// ---------- resourceListAllZero tests ----------

func TestResourceListAllZero(t *testing.T) {
	t.Parallel()

	t.Run("nil list is all zero", func(t *testing.T) {
		t.Parallel()
		if !resourceListAllZero(nil) {
			t.Fatalf("expected true")
		}
	})

	t.Run("empty list is all zero", func(t *testing.T) {
		t.Parallel()
		if !resourceListAllZero(corev1.ResourceList{}) {
			t.Fatalf("expected true")
		}
	})

	t.Run("positive quantity returns false", func(t *testing.T) {
		t.Parallel()
		rl := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "1",
		})
		if resourceListAllZero(rl) {
			t.Fatalf("expected false")
		}
	})

	t.Run("explicit zero quantities returns true", func(t *testing.T) {
		t.Parallel()
		rl := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "0",
			corev1.ResourceMemory: "0",
		})
		if !resourceListAllZero(rl) {
			t.Fatalf("expected true")
		}
	})
}

// ---------- claimCoverageScore tests ----------

func TestClaimCoverageScore(t *testing.T) {
	t.Parallel()

	t.Run("no overlap yields zero score", func(t *testing.T) {
		t.Parallel()

		remaining := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "500m",
		})
		reqs := rl(map[corev1.ResourceName]string{
			corev1.ResourceMemory: "128Mi",
		})

		if got := claimCoverageScore(remaining, reqs); got != 0 {
			t.Fatalf("expected score 0, got=%v", got)
		}
	})

	t.Run("caps at remaining (min(rem, req))", func(t *testing.T) {
		t.Parallel()

		remaining := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "500m",
		})
		reqs := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "2",
		})

		// covered = remaining (500m) -> score > 0
		got := claimCoverageScore(remaining, reqs)
		if got <= 0 {
			t.Fatalf("expected positive score, got=%v", got)
		}

		// Sanity: if remaining is smaller, score should not increase.
		remaining2 := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "250m",
		})
		got2 := claimCoverageScore(remaining2, reqs)
		if got2 <= 0 || got2 >= got {
			t.Fatalf("expected smaller positive score; got=%v got2=%v", got, got2)
		}
	})

	t.Run("ignores zero remaining", func(t *testing.T) {
		t.Parallel()

		remaining := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "0",
		})
		reqs := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "1",
		})

		if got := claimCoverageScore(remaining, reqs); got != 0 {
			t.Fatalf("expected 0, got=%v", got)
		}
	})

	t.Run("sums across resources", func(t *testing.T) {
		t.Parallel()

		remaining := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "500m",
			corev1.ResourceMemory: "1Gi",
		})
		reqs := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "250m",
			corev1.ResourceMemory: "512Mi",
		})

		got := claimCoverageScore(remaining, reqs)
		if got <= 0 {
			t.Fatalf("expected positive score, got=%v", got)
		}
	})
}

// ---------- selectClaimsCoveringUsageGreedy tests ----------

func TestSelectClaimsCoveringUsageGreedy(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("empty used returns empty selection", func(t *testing.T) {
		t.Parallel()

		used := corev1.ResourceList{}
		claims := []capsulev1beta2.ResourcePoolClaim{
			claim(t, "u1", "ns", "c1", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})),
		}

		got := selectClaimsCoveringUsageGreedy(used, claims)
		if len(got) != 0 {
			t.Fatalf("expected empty, got=%v", got)
		}
	})

	t.Run("no claims returns empty selection", func(t *testing.T) {
		t.Parallel()

		used := rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})
		got := selectClaimsCoveringUsageGreedy(used, nil)
		if len(got) != 0 {
			t.Fatalf("expected empty, got=%v", got)
		}
	})

	t.Run("selects single best claim that covers usage", func(t *testing.T) {
		t.Parallel()

		used := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "900m",
		})

		claims := []capsulev1beta2.ResourcePoolClaim{
			claim(t, "u1", "ns", "small", base.Add(1*time.Second), rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "500m"})),
			claim(t, "u2", "ns", "big", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "2"})),
		}

		got := selectClaimsCoveringUsageGreedy(used, claims)
		if !setEq(got, []string{"u2"}) {
			t.Fatalf("expected [u2], got=%v", got)
		}
	})

	t.Run("selects multiple claims to cover usage (clamps remaining at zero)", func(t *testing.T) {
		t.Parallel()

		used := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "1500m",
		})

		claims := []capsulev1beta2.ResourcePoolClaim{
			claim(t, "u1", "ns", "c1", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})), // 1000m
			claim(t, "u2", "ns", "c2", base.Add(1*time.Second), rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})),
			claim(t, "u3", "ns", "c3", base.Add(2*time.Second), rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "250m"})),
		}

		got := selectClaimsCoveringUsageGreedy(used, claims)

		// Greedy should take u1 (covers 1000m), then u2 (covers remaining 500m).
		if !setEq(got, []string{"u1", "u2"}) {
			t.Fatalf("expected [u1 u2], got=%v", got)
		}
	})

	t.Run("handles used resources not present in any claim (stops when cannot improve)", func(t *testing.T) {
		t.Parallel()

		used := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU:    "500m",
			corev1.ResourceMemory: "256Mi",
		})

		claims := []capsulev1beta2.ResourcePoolClaim{
			claim(t, "u1", "ns", "cpu-only", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})),
		}

		got := selectClaimsCoveringUsageGreedy(used, claims)
		// It can reduce CPU remaining, but cannot cover memory at all; algorithm should select cpu-only then stop.
		if !setEq(got, []string{"u1"}) {
			t.Fatalf("expected [u1], got=%v", got)
		}
	})

	t.Run("deterministic tie-break: equal score chooses oldest, then name, then namespace", func(t *testing.T) {
		t.Parallel()

		used := rl(map[corev1.ResourceName]string{
			corev1.ResourceCPU: "500m",
		})

		// Three identical claims by resources; selection should be deterministic by the sort.
		cA := claim(t, "uA", "ns-b", "a", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"}))
		cB := claim(t, "uB", "ns-a", "b", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"}))
		cC := claim(t, "uC", "ns-a", "a", base, rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"}))

		claims := []capsulev1beta2.ResourcePoolClaim{cA, cB, cC}

		// Shuffle input order to ensure determinism isn't accidental
		sort.Slice(claims, func(i, j int) bool { return i > j })

		got := selectClaimsCoveringUsageGreedy(used, claims)

		// After sorting: same ts -> name asc -> namespace asc
		// Name "a" beats "b". For name "a", namespace "ns-a" beats "ns-b".
		// That means uC (ns-a, a) should be selected.
		if !setEq(got, []string{"uC"}) {
			t.Fatalf("expected [uC], got=%v", got)
		}
	})
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func BenchmarkApplyCreation(b *testing.B) {
	for _, condition := range []string{"", "object == null"} {
		b.Run("condition="+condition, func(b *testing.B) {
			m := skippedPolicyManager(b)
			c := fake.NewClientBuilder().WithReturnManagedFields().Build()
			obj := configMap("created", map[string]any{"value": "managed"})
			b.ReportAllocs()
			for b.Loop() {
				result, err := m.Apply(b.Context(), c, obj, ApplyOptions{FieldOwner: testFieldOwner, Condition: condition})
				if err != nil || !result.Created {
					b.Fatalf("creation: %+v, %v", result, err)
				}
				b.StopTimer()
				if err := c.Delete(b.Context(), obj); client.IgnoreNotFound(err) != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

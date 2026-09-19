// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"fmt"
	"testing"

	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

const benchmarkMetadataRuleYAML = `
rules:
  - audience:
      - kind: Custom
        name: CapsuleUser
    enforce:
      action: deny
      metadata:
        - apiGroups: [v1]
          kinds: [Namespace]
          labels:
            ".*example.com/.*": {required: false}
            "policy.example.com/enforce": {required: false}
          annotations:
            ".*example.com/.*": {required: false}
            "policy.example.com/enforce": {required: false}
`

func benchmarkRuleTenant(b *testing.B, namespaces int, withRules bool) *capsulev1beta2.Tenant {
	b.Helper()
	tnt := &capsulev1beta2.Tenant{Name: "benchmark"}
	var spec struct {
		Rules []*rules.NamespaceRuleBodyTenant `json:"rules"`
	}
	if err := yaml.Unmarshal([]byte(benchmarkMetadataRuleYAML), &spec); err != nil {
		b.Fatal(err)
	}
	if withRules {
		tnt.Spec.Rules = spec.Rules
	}
	tnt.Status.State = capsulev1beta2.TenantStateActive
	tnt.Status.Conditions = meta.ConditionList{meta.NewReadyCondition(tnt)}
	for i := range namespaces {
		name := fmt.Sprintf("benchmark-%04d", i)
		tnt.Status.Namespaces = append(tnt.Status.Namespaces, name)
		tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{
			Name:       name,
			Conditions: meta.ConditionList{meta.NewReadyCondition(tnt), meta.NewCordonedCondition(tnt)},
			Metadata: &capsulev1beta2.TenantStatusNamespaceMetadata{
				Labels:      map[string]string{"example.com/team": "benchmark", "example.com/environment": "test"},
				Annotations: map[string]string{"example.com/owner": "benchmark", "example.com/description": "A tenant namespace with representative status metadata"},
			},
		})
	}
	return tnt
}

func BenchmarkNamespaceMetadataRuleProjection(b *testing.B) {
	scheme := runtime.NewScheme()
	for _, n := range []int{1, 10, 100, 1000} {
		for _, withRules := range []bool{false, true} {
			b.Run(fmt.Sprintf("namespaces=%d/rules=%t", n, withRules), func(b *testing.B) {
				tnt := benchmarkRuleTenant(b, n, withRules)
				ns := &corev1.Namespace{Name: "benchmark-0000"}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkNamespaceMetadataRuleNamespaceBatch(b *testing.B) {
	scheme := runtime.NewScheme()
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("namespaces=%d", n), func(b *testing.B) {
			tnt := benchmarkRuleTenant(b, n, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var group errgroup.Group
				group.SetLimit(8)
				for _, name := range tnt.Status.Namespaces {
					group.Go(func() error {
						ns := &corev1.Namespace{Name: name}
						_, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
						return err
					})
				}
				if err := group.Wait(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

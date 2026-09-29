// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// BenchmarkEvaluateEnforce measures the shared path used by existing workload
// matchers, including the cost of extracting values and constructing decisions.
func BenchmarkEvaluateEnforce(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			bodies := make([]*rules.NamespaceRuleEnforceBody, count)
			for i := range bodies {
				bodies[i] = &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow}
			}
			items := []string{"default-scheduler"}
			set := Set[string, string]{
				Name: "scheduler",
				Values: func(value string) []Value {
					return []Value{{Value: value, Path: "spec.schedulerName"}}
				},
				Rules: func(*rules.NamespaceRuleEnforceBody) []string { return items },
				Matches: func(rule string, value Value) (Match, error) {
					return Match{Matched: rule == value.Value}, nil
				},
				RuleDescription: func(rule string) string { return rule },
			}
			b.ReportAllocs()
			for b.Loop() {
				result, err := EvaluateEnforce("default-scheduler", bodies, set)
				if err != nil || result.Final == nil || result.BlockingError() != nil {
					b.Fatalf("unexpected result: %+v, %v", result, err)
				}
			}
		})
	}
}

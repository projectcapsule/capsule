// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestProtectionWebhookRegistration(t *testing.T) {
	for _, hook := range []string{"replications", "resourcePermit"} {
		matches := protectionWebhookMatcher(t, hook)
		cases := []struct {
			name   string
			labels map[string]string
			want   bool
		}{
			{"no labels", nil, false},
			{"other controller", map[string]string{meta.CreatedByCapsuleLabel: meta.ValueController}, false},
			{"created only", map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications}, hook == "replications"},
			{"managed only", map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications}, hook == "replications"},
			{"legacy replication protection", map[string]string{meta.ProtectedByCapsuleLabel: meta.ValueControllerReplications}, hook == "replications"},
			{"legacy permit protection", map[string]string{meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit}, hook == "resourcePermit"},
			{"replication protection", map[string]string{meta.ReplicationProtectionLabel: meta.ValueTrue}, hook == "replications"},
			{"permit protection", map[string]string{meta.ResourcePermitProtectionLabel: meta.ValueTrue}, hook == "resourcePermit"},
			{"both controllers", map[string]string{meta.ReplicationProtectionLabel: meta.ValueTrue, meta.ResourcePermitProtectionLabel: meta.ValueTrue}, true},
			{"disabled markers", map[string]string{meta.ReplicationProtectionLabel: "false", meta.ResourcePermitProtectionLabel: "false"}, false},
		}
		for _, tc := range cases {
			for _, operation := range []string{"update", "remove labels", "add labels", "delete"} {
				t.Run(hook+"/"+tc.name+"/"+operation, func(t *testing.T) {
					object, old := protectionRegistrationObject(tc.labels), protectionRegistrationObject(tc.labels)
					switch operation {
					case "remove labels":
						object = protectionRegistrationObject(nil)
					case "add labels":
						old = protectionRegistrationObject(nil)
					case "delete":
						object = nil
					}
					require.Equal(t, tc.want, matches(object, old))
				})
			}
		}
	}
}

// Exercise the actual chart defaults, including selector AND condition behavior.
// The e2e cases verify their installation by the admission controller as well.
func protectionWebhookMatcher(t testing.TB, hook string) func(map[string]any, map[string]any) bool {
	t.Helper()
	raw, err := os.ReadFile("../../../charts/capsule/values.yaml")
	require.NoError(t, err)
	var values struct {
		Webhooks struct {
			Hooks map[string]admissionregistrationv1.ValidatingWebhook
		}
	}
	require.NoError(t, yaml.Unmarshal(raw, &values))
	registration, exists := values.Webhooks.Hooks[hook]
	require.True(t, exists)
	selector := labels.Everything()
	if registration.ObjectSelector != nil {
		selector, err = metav1.LabelSelectorAsSelector(registration.ObjectSelector)
		require.NoError(t, err)
	}
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("oldObject", cel.DynType))
	require.NoError(t, err)
	programs := make([]cel.Program, 0, len(registration.MatchConditions))
	for _, condition := range registration.MatchConditions {
		ast, issues := env.Compile(condition.Expression)
		require.NoError(t, issues.Err())
		program, err := env.Program(ast)
		require.NoError(t, err)
		programs = append(programs, program)
	}
	return func(object, old map[string]any) bool {
		selected := false
		for _, obj := range []map[string]any{object, old} {
			if obj == nil {
				continue
			}
			set, _ := obj["metadata"].(map[string]any)["labels"].(map[string]string)
			selected = selected || selector.Matches(labels.Set(set))
		}
		if !selected {
			return false
		}
		var currentValue, oldValue any
		if object != nil {
			currentValue = object
		}
		if old != nil {
			oldValue = old
		}
		for _, program := range programs {
			result, _, err := program.Eval(map[string]any{"object": currentValue, "oldObject": oldValue})
			require.NoError(t, err)
			if result.Value() != true {
				return false
			}
		}
		return true
	}
}

func protectionRegistrationObject(labels map[string]string) map[string]any {
	metadata := map[string]any{}
	if labels != nil {
		metadata["labels"] = labels
	}
	return map[string]any{"metadata": metadata}
}

func BenchmarkProtectionWebhookMatching(b *testing.B) {
	for _, hook := range []string{"replications", "resourcePermit"} {
		for _, selected := range []bool{false, true} {
			b.Run(fmt.Sprintf("hook=%s/selected=%t", hook, selected), func(b *testing.B) {
				matches := protectionWebhookMatcher(b, hook)
				fields := map[string]string{"example.org/unrelated": "value"}
				if selected {
					fields[meta.ReplicationProtectionLabel] = meta.ValueTrue
					fields[meta.ResourcePermitProtectionLabel] = meta.ValueTrue
				}
				object := protectionRegistrationObject(fields)
				b.ReportAllocs()
				for b.Loop() {
					if matches(object, object) != selected {
						b.Fatal("unexpected webhook selection")
					}
				}
			})
		}
	}
}

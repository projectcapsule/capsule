// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestPodRulesValidateRegistriesPreconditions(t *testing.T) {
	t.Run("nil registry cache returns error", func(t *testing.T) {
		h := &podRules{}

		evaluation, err := h.validateRegistries(
			registryPodForTest("harbor/platform/app:1.0.0", corev1.PullAlways),
			[]*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
				),
			},
		)

		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "registry rule set cache is nil") {
			t.Fatalf("expected registry cache error, got %q", err.Error())
		}

		if evaluation != nil {
			t.Fatalf("expected nil evaluation, got %#v", evaluation)
		}
	})

	t.Run("nil pod returns nil evaluation when cache exists", func(t *testing.T) {
		h := &podRules{
			registryCache: cache.NewRegistryRuleSetCache(cache.NewRegexCache()),
		}

		evaluation, err := h.validateRegistries(
			nil,
			[]*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
				),
			},
		)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if evaluation != nil {
			t.Fatalf("expected nil evaluation, got %#v", evaluation)
		}
	})

	t.Run("empty enforce bodies returns nil evaluation when cache exists", func(t *testing.T) {
		h := &podRules{
			registryCache: cache.NewRegistryRuleSetCache(cache.NewRegexCache()),
		}

		evaluation, err := h.validateRegistries(
			registryPodForTest("harbor/platform/app:1.0.0", corev1.PullAlways),
			nil,
		)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if evaluation != nil {
			t.Fatalf("expected nil evaluation, got %#v", evaluation)
		}
	})
}

func TestPodRulesValidateRegistries(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantBlocking  bool
		wantFinal     bool
		wantAudits    int
		wantErr       string
		wantMessage   []string
	}{
		{
			name: "empty container image reference is denied before registry evaluation",
			pod:  registryPodForTest("", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
				),
			},
			wantBlocking: true,
			wantMessage: []string{
				"containers[0] has empty reference",
			},
		},
		{
			name: "blank container image reference is denied before registry evaluation",
			pod:  registryPodForTest("   ", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
				),
			},
			wantBlocking: true,
			wantMessage: []string{
				"containers[0] has empty reference",
			},
		},
		{
			name: "allow matching registry",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/platform/.*"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule`,
				`exp=harbor/platform/.*`,
			},
		},
		{
			name: "allow exact registry",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExactForTest("harbor/platform/app:1.0.0"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule`,
				`exact=harbor/platform/app:1.0.0`,
			},
		},
		{
			name: "allow miss denies registry and reports allowed registries",
			pod:  registryPodForTest("docker.io/library/nginx:latest", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
					registryExpressionForTest("registry.local/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantMessage: []string{
				`registry "docker.io/library/nginx:latest" at containers[0] is not allowed by namespace rule`,
				`Allowed registries`,
				`exp: harbor/.*`,
				`exp: registry.local/.*`,
			},
		},
		{
			name: "deny matching registry",
			pod:  registryPodForTest("harbor/customer/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeDeny,
					nil,
					registryExpressionForTest("harbor/customer/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/customer/app:1.0.0" is denied by registry rule`,
				`exp=harbor/customer/.*`,
			},
		},
		{
			name: "default action is deny",
			pod:  registryPodForTest("harbor/customer/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					"",
					nil,
					registryExpressionForTest("harbor/customer/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/customer/app:1.0.0" is denied by registry rule`,
				`exp=harbor/customer/.*`,
			},
		},
		{
			name: "later deny overrides earlier allow",
			pod:  registryPodForTest("harbor/customer/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/.*"),
				),
				registryEnforceForTest(
					apirules.ActionTypeDeny,
					nil,
					registryExpressionForTest("harbor/customer/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/customer/app:1.0.0" is denied by registry rule`,
				`exp=harbor/customer/.*`,
			},
		},
		{
			name: "later allow overrides earlier deny",
			pod:  registryPodForTest("harbor/customer/prod/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeDeny,
					nil,
					registryExpressionForTest("harbor/customer/.*"),
				),
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("harbor/customer/prod/.*"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/customer/prod/app:1.0.0" is allowed by registry rule`,
				`exp=harbor/customer/prod/.*`,
			},
		},
		{
			name: "audit match is observational",
			pod:  registryPodForTest("audit/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAudit,
					nil,
					registryExpressionForTest("audit/.*"),
				),
			},
			wantBlocking: false,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`containers[0] reference "audit/app:1.0.0" matched audit registry rule`,
				`exp=audit/.*`,
			},
		},
		{
			name: "audit does not satisfy allow list",
			pod:  registryPodForTest("audit/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAudit,
					nil,
					registryExpressionForTest("audit/.*"),
				),
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					nil,
					registryExpressionForTest("allowed/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    false,
			wantAudits:   1,
			wantMessage: []string{
				`registry "audit/app:1.0.0" at containers[0] is not allowed by namespace rule`,
				`Allowed registries`,
				`exp: allowed/.*`,
			},
		},
		{
			name: "invalid registry regex returns error",
			pod:  registryPodForTest("harbor/app:1.0.0", corev1.PullAlways),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeDeny,
					nil,
					registryExpressionForTest("["),
				),
			},
			wantErr: "registry: invalid rule",
		},
		{
			name: "pull policy missing is denied when matching allow rule requires policy",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", ""),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					[]corev1.PullPolicy{corev1.PullAlways},
					registryExpressionForTest("harbor/platform/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" must explicitly set pullPolicy`,
				`allowed: Always`,
			},
		},
		{
			name: "pull policy mismatch is denied after allow",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", corev1.PullNever),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					[]corev1.PullPolicy{corev1.PullAlways, corev1.PullIfNotPresent},
					registryExpressionForTest("harbor/platform/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" uses pullPolicy=Never which is not allowed`,
				`allowed: Always, IfNotPresent`,
			},
		},
		{
			name: "pull policy match is allowed after allow",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", corev1.PullIfNotPresent),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeAllow,
					[]corev1.PullPolicy{corev1.PullAlways, corev1.PullIfNotPresent},
					registryExpressionForTest("harbor/platform/.*"),
				),
			},
			wantBlocking: false,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule`,
			},
		},
		{
			name: "pull policy is not evaluated for final deny",
			pod:  registryPodForTest("harbor/platform/app:1.0.0", corev1.PullNever),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				registryEnforceForTest(
					apirules.ActionTypeDeny,
					[]corev1.PullPolicy{corev1.PullAlways},
					registryExpressionForTest("harbor/platform/.*"),
				),
			},
			wantBlocking: true,
			wantFinal:    true,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" is denied by registry rule`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &podRules{
				registryCache: cache.NewRegistryRuleSetCache(cache.NewRegexCache()),
			}

			evaluation, err := h.validateRegistries(tt.pod, tt.enforceBodies)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if evaluation == nil {
				t.Fatalf("expected evaluation, got nil")
			}

			if tt.wantBlocking && evaluation.Blocking == nil {
				t.Fatalf("expected blocking decision, got nil")
			}

			if !tt.wantBlocking && evaluation.Blocking != nil {
				t.Fatalf("expected no blocking decision, got %#v", evaluation.Blocking)
			}

			if tt.wantFinal && evaluation.Final == nil {
				t.Fatalf("expected final decision, got nil")
			}

			if !tt.wantFinal && evaluation.Final != nil {
				t.Fatalf("expected no final decision, got %#v", evaluation.Final)
			}

			if len(evaluation.Audits) != tt.wantAudits {
				t.Fatalf("expected %d audit decisions, got %d", tt.wantAudits, len(evaluation.Audits))
			}

			if len(tt.wantMessage) > 0 {
				msg := decisionMessageForRegistryTest(evaluation)

				for _, expected := range tt.wantMessage {
					if !strings.Contains(msg, expected) {
						t.Fatalf("expected message %q to contain %q", msg, expected)
					}
				}
			}

			if evaluation.Final != nil {
				if evaluation.Final.EventReason != events.ReasonForbiddenContainerRegistry {
					t.Fatalf("final event reason = %q, want %q", evaluation.Final.EventReason, events.ReasonForbiddenContainerRegistry)
				}
			}

			if evaluation.Blocking != nil {
				switch evaluation.Blocking.EventReason {
				case events.ReasonForbiddenContainerRegistry, events.ReasonForbiddenPullPolicy:
				default:
					t.Fatalf(
						"blocking event reason = %q, want %q or %q",
						evaluation.Blocking.EventReason,
						events.ReasonForbiddenContainerRegistry,
						events.ReasonForbiddenPullPolicy,
					)
				}
			}

			for _, audit := range evaluation.Audits {
				if audit.EventReason != events.ReasonForbiddenContainerRegistry {
					t.Fatalf("audit event reason = %q, want %q", audit.EventReason, events.ReasonForbiddenContainerRegistry)
				}
			}
		})
	}
}

func TestPodRulesEvaluateRegistryReference(t *testing.T) {
	h := &podRules{
		registryCache: cache.NewRegistryRuleSetCache(cache.NewRegexCache()),
	}

	ref := registryReference{
		Target:     apirules.ValidateContainers,
		Reference:  "harbor/platform/app:1.0.0",
		PullPolicy: corev1.PullAlways,
		Path:       "containers[0]",
	}

	evaluation, err := h.evaluateRegistryReference(ref, []*apirules.NamespaceRuleEnforceBody{
		registryEnforceForTest(
			apirules.ActionTypeAllow,
			nil,
			registryExpressionForTest("harbor/platform/.*"),
		),
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if evaluation == nil {
		t.Fatalf("expected evaluation, got nil")
	}

	if evaluation.Final == nil {
		t.Fatalf("expected final decision, got nil")
	}

	if evaluation.Final.Action != apirules.ActionTypeAllow {
		t.Fatalf("final action = %q, want %q", evaluation.Final.Action, apirules.ActionTypeAllow)
	}

	if evaluation.Final.MatchedValue == nil {
		t.Fatalf("expected matched value")
	}
}

func TestDescribeRegistryRuleSet(t *testing.T) {
	tests := []struct {
		name string
		rule registryRuleSet
		want string
	}{
		{
			name: "empty rule set",
			rule: registryRuleSet{},
			want: "",
		},
		{
			name: "exact",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					registryExactForTest("harbor/platform/app:1.0.0"),
				},
			},
			want: "exact: harbor/platform/app:1.0.0",
		},
		{
			name: "expression",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					registryExpressionForTest("harbor/.*"),
				},
			},
			want: "exp: harbor/.*",
		},
		{
			name: "exact and expression",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					{
						ExpressionMatch: api.ExpressionMatch{
							Exact: []string{
								"harbor/platform/app:1.0.0",
							},
							ExpressionRegex: api.ExpressionRegex{
								Expression: "harbor/shared/.*",
							},
						},
					},
				},
			},
			want: "exact: harbor/platform/app:1.0.0; exp: harbor/shared/.*",
		},
		{
			name: "multiple registries",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					registryExpressionForTest("harbor/.*"),
					registryExpressionForTest("registry.local/.*"),
				},
			},
			want: "exp: harbor/.*, exp: registry.local/.*",
		},
		{
			name: "skips empty description",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					{},
					registryExpressionForTest("harbor/.*"),
				},
			},
			want: "exp: harbor/.*",
		},
		{
			name: "negated expression",
			rule: registryRuleSet{
				Registries: []apirules.OCIRegistry{
					{
						ExpressionMatch: api.ExpressionMatch{
							ExpressionRegex: api.ExpressionRegex{
								Expression: "trusted/.*",
								Negate:     true,
							},
						},
					},
				},
			},
			want: "not exp: trusted/.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeRegistryRuleSet(tt.rule)
			if got != tt.want {
				t.Fatalf("describeRegistryRuleSet() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegistryReferencesFromPod(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "init",
					Image:           "harbor/init/app:1.0.0",
					ImagePullPolicy: corev1.PullAlways,
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "app",
					Image:           "harbor/app/app:1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
			},
			EphemeralContainers: []corev1.EphemeralContainer{
				{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{
						Name:            "debug",
						Image:           "harbor/debug/app:1.0.0",
						ImagePullPolicy: corev1.PullNever,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{},
					},
				},
				{
					Name: "artifact",
					VolumeSource: corev1.VolumeSource{
						Image: &corev1.ImageVolumeSource{
							Reference:  "harbor/volume/artifact:1.0.0",
							PullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	got := registryReferencesFromPod(pod)

	want := []registryReference{
		{
			Target:     apirules.ValidateInitContainers,
			Reference:  "harbor/init/app:1.0.0",
			PullPolicy: corev1.PullAlways,
			Path:       "initContainers[0]",
		},
		{
			Target:     apirules.ValidateContainers,
			Reference:  "harbor/app/app:1.0.0",
			PullPolicy: corev1.PullIfNotPresent,
			Path:       "containers[0]",
		},
		{
			Target:     apirules.ValidateEphemeralContainers,
			Reference:  "harbor/debug/app:1.0.0",
			PullPolicy: corev1.PullNever,
			Path:       "ephemeralContainers[0]",
		},
		{
			Target:     apirules.ValidateVolumes,
			Reference:  "harbor/volume/artifact:1.0.0",
			PullPolicy: corev1.PullAlways,
			Path:       "volumes[1](artifact)",
		},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d refs, got %d: %#v", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestRegistryReferencesFromPodNil(t *testing.T) {
	if got := registryReferencesFromPod(nil); got != nil {
		t.Fatalf("registryReferencesFromPod(nil) = %#v, want nil", got)
	}
}

func TestRegistryDecisionMessage(t *testing.T) {
	matched := compiledRegistryRuleForTest(
		registryExpressionForTest("harbor/platform/.*"),
		nil,
	)

	tests := []struct {
		name   string
		action apirules.ActionType
		value  ruleengine.Value
		match  any
		want   string
	}{
		{
			name:   "audit",
			action: apirules.ActionTypeAudit,
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: matched,
			want:  `containers[0] reference "harbor/platform/app:1.0.0" matched audit registry rule "exp=harbor/platform/.*"`,
		},
		{
			name:   "deny",
			action: apirules.ActionTypeDeny,
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: matched,
			want:  `containers[0] reference "harbor/platform/app:1.0.0" is denied by registry rule "exp=harbor/platform/.*"`,
		},
		{
			name:   "allow",
			action: apirules.ActionTypeAllow,
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: matched,
			want:  `containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule "exp=harbor/platform/.*"`,
		},
		{
			name:   "unknown action",
			action: apirules.ActionType("custom"),
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: matched,
			want:  `containers[0] reference "harbor/platform/app:1.0.0" matched registry rule "exp=harbor/platform/.*" with action "custom"`,
		},
		{
			name:   "nil matched value",
			action: apirules.ActionTypeAllow,
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: nil,
			want:  `containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule "<unknown>"`,
		},
		{
			name:   "wrong matched value type",
			action: apirules.ActionTypeAllow,
			value: ruleengine.Value{
				Value: "harbor/platform/app:1.0.0",
				Path:  "containers[0]",
			},
			match: "not-a-compiled-rule",
			want:  `containers[0] reference "harbor/platform/app:1.0.0" is allowed by registry rule "<unknown>"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registryDecisionMessage(tt.action, tt.value, tt.match)
			if got != tt.want {
				t.Fatalf("registryDecisionMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegistryRuleDescription(t *testing.T) {
	tests := []struct {
		name    string
		matched *cache.CompiledRule
		want    string
	}{
		{
			name:    "nil",
			matched: nil,
			want:    "<unknown>",
		},
		{
			name: "unknown empty rule",
			matched: &cache.CompiledRule{
				Match: api.ExpressionMatch{},
			},
			want: "<unknown>",
		},
		{
			name: "exact values are sorted",
			matched: compiledRegistryRuleForTest(
				registryExactForTest("z.registry/app:1.0.0", "a.registry/app:1.0.0"),
				nil,
			),
			want: "exact=a.registry/app:1.0.0,z.registry/app:1.0.0",
		},
		{
			name: "expression",
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				nil,
			),
			want: "exp=harbor/platform/.*",
		},
		{
			name: "negated expression",
			matched: compiledRegistryRuleForTest(
				apirules.OCIRegistry{
					ExpressionMatch: api.ExpressionMatch{
						ExpressionRegex: api.ExpressionRegex{
							Expression: "trusted/.*",
							Negate:     true,
						},
					},
				},
				nil,
			),
			want: "exp=trusted/.*,negate=true",
		},
		{
			name: "exact and expression",
			matched: compiledRegistryRuleForTest(
				apirules.OCIRegistry{
					ExpressionMatch: api.ExpressionMatch{
						Exact: []string{
							"harbor/platform/app:1.0.0",
						},
						ExpressionRegex: api.ExpressionRegex{
							Expression: "harbor/shared/.*",
						},
					},
				},
				nil,
			),
			want: "exact=harbor/platform/app:1.0.0;exp=harbor/shared/.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registryRuleDescription(tt.matched)
			if got != tt.want {
				t.Fatalf("registryRuleDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegistryPullPolicyDecision(t *testing.T) {
	ref := registryReference{
		Target:    apirules.ValidateContainers,
		Reference: "harbor/platform/app:1.0.0",
		Path:      "containers[0]",
	}

	tests := []struct {
		name        string
		ref         registryReference
		matched     *cache.CompiledRule
		wantNil     bool
		wantReason  string
		wantMessage []string
	}{
		{
			name:    "nil matched rule",
			ref:     ref,
			matched: nil,
			wantNil: true,
		},
		{
			name: "no allowed policy",
			ref:  ref,
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				nil,
			),
			wantNil: true,
		},
		{
			name: "empty allowed policy",
			ref:  ref,
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				[]corev1.PullPolicy{},
			),
			wantNil: true,
		},
		{
			name: "missing pull policy is denied",
			ref:  ref,
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				[]corev1.PullPolicy{corev1.PullAlways},
			),
			wantReason: events.ReasonForbiddenPullPolicy,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" must explicitly set pullPolicy`,
				`allowed: Always`,
			},
		},
		{
			name: "disallowed pull policy is denied",
			ref: registryReference{
				Target:     ref.Target,
				Reference:  ref.Reference,
				Path:       ref.Path,
				PullPolicy: corev1.PullNever,
			},
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				[]corev1.PullPolicy{
					corev1.PullAlways,
					corev1.PullIfNotPresent,
				},
			),
			wantReason: events.ReasonForbiddenPullPolicy,
			wantMessage: []string{
				`containers[0] reference "harbor/platform/app:1.0.0" uses pullPolicy=Never which is not allowed`,
				`allowed: Always, IfNotPresent`,
			},
		},
		{
			name: "allowed pull policy succeeds",
			ref: registryReference{
				Target:     ref.Target,
				Reference:  ref.Reference,
				Path:       ref.Path,
				PullPolicy: corev1.PullIfNotPresent,
			},
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				[]corev1.PullPolicy{
					corev1.PullAlways,
					corev1.PullIfNotPresent,
				},
			),
			wantNil: true,
		},
		{
			name: "allowed policies are sorted in message",
			ref: registryReference{
				Target:     ref.Target,
				Reference:  ref.Reference,
				Path:       ref.Path,
				PullPolicy: corev1.PullNever,
			},
			matched: compiledRegistryRuleForTest(
				registryExpressionForTest("harbor/platform/.*"),
				[]corev1.PullPolicy{
					corev1.PullIfNotPresent,
					corev1.PullAlways,
				},
			),
			wantReason: events.ReasonForbiddenPullPolicy,
			wantMessage: []string{
				`allowed: Always, IfNotPresent`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registryPullPolicyDecision(tt.ref, tt.matched)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil decision, got %#v", got)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected decision, got nil")
			}

			if got.EventReason != tt.wantReason {
				t.Fatalf("event reason = %q, want %q", got.EventReason, tt.wantReason)
			}

			if got.Action != apirules.ActionTypeDeny {
				t.Fatalf("action = %q, want %q", got.Action, apirules.ActionTypeDeny)
			}

			if got.SetName != "registry" {
				t.Fatalf("set name = %q, want registry", got.SetName)
			}

			if got.Value.Value != tt.ref.Reference {
				t.Fatalf("decision value = %q, want %q", got.Value.Value, tt.ref.Reference)
			}

			if got.Value.Path != tt.ref.Path {
				t.Fatalf("decision path = %q, want %q", got.Value.Path, tt.ref.Path)
			}

			for _, expected := range tt.wantMessage {
				if !strings.Contains(got.Message, expected) {
					t.Fatalf("expected message %q to contain %q", got.Message, expected)
				}
			}
		})
	}
}

func TestFormatAllowedPullPolicies(t *testing.T) {
	tests := []struct {
		name     string
		policies map[corev1.PullPolicy]struct{}
		want     string
	}{
		{
			name:     "nil",
			policies: nil,
			want:     "",
		},
		{
			name:     "empty",
			policies: map[corev1.PullPolicy]struct{}{},
			want:     "",
		},
		{
			name: "single",
			policies: map[corev1.PullPolicy]struct{}{
				corev1.PullAlways: {},
			},
			want: "Always",
		},
		{
			name: "sorted",
			policies: map[corev1.PullPolicy]struct{}{
				corev1.PullNever:        {},
				corev1.PullAlways:       {},
				corev1.PullIfNotPresent: {},
			},
			want: "Always, IfNotPresent, Never",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAllowedPullPolicies(tt.policies)
			if got != tt.want {
				t.Fatalf("formatAllowedPullPolicies() = %q, want %q", got, tt.want)
			}
		})
	}
}

func registryEnforceForTest(
	action apirules.ActionType,
	policies []corev1.PullPolicy,
	registries ...apirules.OCIRegistry,
) *apirules.NamespaceRuleEnforceBody {
	out := &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			Registries: registries,
		},
	}

	if policies == nil {
		return out
	}

	for i := range out.Workloads.Registries {
		out.Workloads.Registries[i].Policy = policies
	}

	return out
}

func registryExactForTest(values ...string) apirules.OCIRegistry {
	return apirules.OCIRegistry{
		ExpressionMatch: api.ExpressionMatch{
			Exact: values,
		},
	}
}

func registryExpressionForTest(expression string) apirules.OCIRegistry {
	return apirules.OCIRegistry{
		ExpressionMatch: api.ExpressionMatch{
			ExpressionRegex: api.ExpressionRegex{
				Expression: expression,
			},
		},
	}
}

func registryPodForTest(reference string, pullPolicy corev1.PullPolicy) *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "app",
					Image:           reference,
					ImagePullPolicy: pullPolicy,
				},
			},
		},
	}
}

func compiledRegistryRuleForTest(
	registry apirules.OCIRegistry,
	policies []corev1.PullPolicy,
) *cache.CompiledRule {
	out := &cache.CompiledRule{
		Match: registry.ExpressionMatch,
	}

	if len(policies) == 0 {
		return out
	}

	out.AllowedPolicy = make(map[corev1.PullPolicy]struct{}, len(policies))
	for _, policy := range policies {
		out.AllowedPolicy[policy] = struct{}{}
	}

	return out
}

func decisionMessageForRegistryTest(evaluation interface {
}) string {
	e, ok := evaluation.(*ruleengine.Evaluation)
	if !ok || e == nil {
		return ""
	}

	switch {
	case e.Blocking != nil:
		return e.Blocking.Message
	case e.Final != nil:
		return e.Final.Message
	case len(e.Audits) > 0:
		return e.Audits[0].Message
	default:
		return ""
	}
}

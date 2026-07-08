// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/internal/cache"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func deploymentGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
}

func deploymentObject(images ...string) genericObject {
	containers := make([]any, 0, len(images))
	for _, image := range images {
		containers = append(containers, map[string]any{"image": image})
	}

	return &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": containers,
					},
				},
			},
		},
	}
}

func pvcObject(storageClass string) genericObject {
	spec := map[string]any{}
	if storageClass != "" {
		spec["storageClassName"] = storageClass
	}

	return &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
}

func fieldRule(
	kinds []string,
	apiGroups []string,
	path string,
	match ...runtime.ExpressionMatch,
) apirules.FieldRule {
	return apirules.FieldRule{
		VersionKinds: runtime.VersionKinds{
			APIGroups: apiGroups,
			Kinds:     kinds,
		},
		Path:  path,
		Match: match,
	}
}

func newFieldRules() *genericRules {
	return GenericRules(cache.NewRegexCache(), cache.NewJSONPathCache()).(*genericRules)
}

func TestValidateFields(t *testing.T) {
	t.Parallel()

	containerImagePath := ".spec.template.spec.containers[*].image"

	t.Run("nil object returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(nil, deploymentGVK(), []*apirules.NamespaceRuleEnforceBody{{}})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if got != nil {
			t.Fatalf("expected nil evaluation, got %#v", got)
		}
	})

	t.Run("no field rules returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			deploymentObject("nginx"),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeDeny,
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if got != nil {
			t.Fatalf("expected nil evaluation, got %#v", got)
		}
	})

	t.Run("rules for other kinds are ignored", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			deploymentObject("docker.io/nginx"),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeDeny,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"StatefulSet"},
							[]string{"apps"},
							containerImagePath,
							runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: `^docker\.io/`}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if got != nil {
			t.Fatalf("expected nil evaluation, got %#v", got)
		}
	})

	t.Run("deny rule blocks matching value with configured path", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			deploymentObject("ghcr.io/app", "docker.io/nginx"),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeDeny,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"Deployment"},
							[]string{"apps"},
							containerImagePath,
							runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: `^docker\.io/`}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		blockingErr := got.BlockingError()
		if blockingErr == nil {
			t.Fatalf("expected blocking error")
		}

		if !strings.Contains(blockingErr.Error(), ".spec.template.spec.containers[*].image") {
			t.Fatalf("expected configured path in message, got %q", blockingErr.Error())
		}

		if !strings.Contains(blockingErr.Error(), "docker.io/nginx") {
			t.Fatalf("expected denied value in message, got %q", blockingErr.Error())
		}
	})

	t.Run("allow rule admits matching values", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			pvcObject("fast-ssd"),
			coreGVK("PersistentVolumeClaim"),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"PersistentVolumeClaim"},
							nil,
							".spec.storageClassName",
							runtime.ExpressionMatch{Exact: []string{"fast-ssd", "standard"}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected no blocking error, got %v", blockingErr)
		}
	})

	t.Run("allow rule blocks non matching value", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			pvcObject("slow-hdd"),
			coreGVK("PersistentVolumeClaim"),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"PersistentVolumeClaim"},
							nil,
							".spec.storageClassName",
							runtime.ExpressionMatch{Exact: []string{"fast-ssd", "standard"}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		blockingErr := got.BlockingError()
		if blockingErr == nil {
			t.Fatalf("expected blocking error")
		}

		if !strings.Contains(blockingErr.Error(), "Allowed field values") {
			t.Fatalf("expected allow-miss message, got %q", blockingErr.Error())
		}
	})

	t.Run("missing path leaves object untouched", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			pvcObject(""),
			coreGVK("PersistentVolumeClaim"),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"PersistentVolumeClaim"},
							nil,
							".spec.storageClassName",
							runtime.ExpressionMatch{Exact: []string{"fast-ssd"}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected no blocking error, got %v", blockingErr)
		}
	})

	t.Run("explicit empty value is not evaluated", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"storageClassName": "",
				},
			},
		}

		got, err := newFieldRules().validateFields(
			obj,
			coreGVK("PersistentVolumeClaim"),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"PersistentVolumeClaim"},
							nil,
							".spec.storageClassName",
							runtime.ExpressionMatch{Exact: []string{"fast-ssd"}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected empty value to be skipped, got %v", blockingErr)
		}
	})

	t.Run("empty array element beside non-empty sibling is not evaluated", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			deploymentObject("registry.internal/app", ""),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"Deployment"},
							[]string{"apps"},
							containerImagePath,
							runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: `^registry\.internal/`}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected empty element to be skipped, got %v", blockingErr)
		}
	})

	t.Run("audit rule records audit decision without blocking", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"hostNetwork": true,
						},
					},
				},
			},
		}

		got, err := newFieldRules().validateFields(
			obj,
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAudit,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"Deployment"},
							[]string{"apps"},
							".spec.template.spec.hostNetwork",
							runtime.ExpressionMatch{Exact: []string{"true"}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected no blocking error, got %v", blockingErr)
		}

		if len(got.Audits) != 1 {
			t.Fatalf("expected one audit decision, got %d", len(got.Audits))
		}
	})

	t.Run("later allow wins over earlier deny", func(t *testing.T) {
		t.Parallel()

		deny := &apirules.NamespaceRuleEnforceBody{
			Action: apirules.ActionTypeDeny,
			Fields: []apirules.FieldRule{
				fieldRule(
					[]string{"Deployment"},
					[]string{"apps"},
					containerImagePath,
					runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: `^docker\.io/`}},
				),
			},
		}
		allow := &apirules.NamespaceRuleEnforceBody{
			Action: apirules.ActionTypeAllow,
			Fields: []apirules.FieldRule{
				fieldRule(
					[]string{"Deployment"},
					[]string{"apps"},
					containerImagePath,
					runtime.ExpressionMatch{Exact: []string{"docker.io/nginx"}},
				),
			},
		}

		got, err := newFieldRules().validateFields(
			deploymentObject("docker.io/nginx"),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{deny, allow},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if blockingErr := got.BlockingError(); blockingErr != nil {
			t.Fatalf("expected last allow to win, got %v", blockingErr)
		}
	})

	t.Run("wildcard kind matches any resource", func(t *testing.T) {
		t.Parallel()

		got, err := newFieldRules().validateFields(
			deploymentObject("docker.io/nginx"),
			deploymentGVK(),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeDeny,
					Fields: []apirules.FieldRule{
						fieldRule(
							[]string{"*"},
							[]string{"*"},
							containerImagePath,
							runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: `^docker\.io/`}},
						),
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if got.BlockingError() == nil {
			t.Fatalf("expected blocking error")
		}
	})
}

func TestFieldRulePaths(t *testing.T) {
	t.Parallel()

	bodies := []*apirules.NamespaceRuleEnforceBody{
		nil,
		{
			Action: apirules.ActionTypeDeny,
			Fields: []apirules.FieldRule{
				fieldRule([]string{"Deployment"}, []string{"apps"}, ".spec.b"),
				fieldRule([]string{"Deployment"}, []string{"apps"}, ".spec.a"),
				fieldRule([]string{"StatefulSet"}, []string{"apps"}, ".spec.c"),
			},
		},
		{
			Action: apirules.ActionTypeAllow,
			Fields: []apirules.FieldRule{
				fieldRule([]string{"Deployment"}, []string{"apps"}, ".spec.a"),
			},
		},
	}

	got := fieldRulePaths(deploymentGVK(), bodies)

	want := []string{".spec.a", ".spec.b"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

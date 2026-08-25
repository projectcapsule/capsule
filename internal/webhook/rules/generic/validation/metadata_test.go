// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestValidateMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		obj           genericObject
		gvk           schema.GroupVersionKind
		enforceBodies []*apirules.NamespaceRuleEnforceBody
		wantNil       bool
		wantBlocking  bool
		wantAudits    int
		wantMessage   string
		wantPath      string
	}{
		{
			name:    "nil object returns nil",
			obj:     nil,
			gvk:     coreGVK("ConfigMap"),
			wantNil: true,
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
		},
		{
			name:    "empty enforce bodies returns nil",
			obj:     metadataObject(nil, nil),
			gvk:     coreGVK("ConfigMap"),
			wantNil: true,
		},
		{
			name: "required missing label denies",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			wantBlocking: true,
			wantMessage:  `metadata label "env" is required`,
			wantPath:     `metadata.labels["env"]`,
		},
		{
			name: "required present matching label allows",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
		},
		{
			name: "required present non matching label denies",
			obj: metadataObject(
				map[string]string{"env": "stage"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			wantBlocking: true,
			wantMessage:  "Allowed metadata values",
			wantPath:     `metadata.labels["env"]`,
		},
		{
			name: "required without values enforces presence only",
			obj: metadataObject(
				map[string]string{"env": "anything"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true),
					},
					nil,
				),
			},
		},
		{
			name: "required without values denies when missing",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true),
					},
					nil,
				),
			},
			wantBlocking: true,
			wantMessage:  `metadata label "env" is required`,
			wantPath:     `metadata.labels["env"]`,
		},
		{
			name: "optional missing label is ignored",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("prod")),
					},
					nil,
				),
			},
			wantNil: true,
		},
		{
			name: "optional present invalid label denies",
			obj: metadataObject(
				map[string]string{"env": "stage"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("prod")),
					},
					nil,
				),
			},
			wantBlocking: true,
			wantMessage:  "Allowed metadata values",
			wantPath:     `metadata.labels["env"]`,
		},
		{
			name: "annotation regex match allows",
			obj: metadataObject(
				nil,
				map[string]string{"example.corp/cost-center": "INV-1234"},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					nil,
					map[string]apirules.MetadataValueRule{
						"example.corp/cost-center": metadataPolicy(false, expression("^INV-[0-9]{4}$")),
					},
				),
			},
		},
		{
			name: "deny matching label blocks",
			obj: metadataObject(
				map[string]string{"env": "blocked"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeDeny,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("blocked")),
					},
					nil,
				),
			},
			wantBlocking: true,
			wantMessage:  "denied",
			wantPath:     `metadata.labels["env"]`,
		},
		{
			name: "deny required missing label is ignored",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeDeny,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("blocked")),
					},
					nil,
				),
			},
			wantNil: true,
		},
		{
			name: "audit matching label emits audit and does not block",
			obj: metadataObject(
				map[string]string{"audit": "audit-this"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAudit,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"audit": metadataPolicy(false, expression("^audit-.*")),
					},
					nil,
				),
			},
			wantAudits: 1,
		},
		{
			name: "audit required missing label is ignored",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAudit,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"audit": metadataPolicy(true, exact("true")),
					},
					nil,
				),
			},
			wantNil: true,
		},
		{
			name: "non matching gvk returns nil",
			obj: metadataObject(
				map[string]string{"env": "stage"},
				nil,
			),
			gvk: coreGVK("Secret"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			wantNil: true,
		},
		{
			name: "empty metadata value is evaluated",
			obj: metadataObject(
				map[string]string{"env": ""},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("")),
					},
					nil,
				),
			},
		},
		{
			name: "invalid regex returns error",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, expression("[")),
					},
					nil,
				),
			},
			wantBlocking: false,
			wantMessage:  "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newMetadataTestRules(nil, nil)

			got, err := h.validateMetadata(tt.obj, tt.gvk, tt.enforceBodies)
			if tt.wantMessage == "error" {
				if err == nil {
					t.Fatalf("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil evaluation, got %#v", got)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected evaluation")
			}

			if len(got.Audits) != tt.wantAudits {
				t.Fatalf("expected %d audit decisions, got %d", tt.wantAudits, len(got.Audits))
			}

			blockingErr := got.BlockingError()
			if tt.wantBlocking && blockingErr == nil {
				t.Fatalf("expected blocking error")
			}
			if !tt.wantBlocking && blockingErr != nil {
				t.Fatalf("expected no blocking error, got %v", blockingErr)
			}

			if tt.wantMessage != "" {
				var message string
				if got.Blocking != nil {
					message = got.Blocking.Message
				} else if len(got.Audits) > 0 {
					message = got.Audits[0].Message
				}

				if !strings.Contains(message, tt.wantMessage) {
					t.Fatalf("expected message to contain %q, got %q", tt.wantMessage, message)
				}
			}

			if tt.wantPath != "" {
				if got.Blocking == nil {
					t.Fatalf("expected blocking decision with path %q", tt.wantPath)
				}

				if got.Blocking.Value.Path != tt.wantPath {
					t.Fatalf("expected path %q, got %q", tt.wantPath, got.Blocking.Value.Path)
				}
			}
		})
	}
}

func TestControlledMetadataEntriesMatchesKeyPatternsWithRegexCache(t *testing.T) {
	t.Parallel()

	h := newMetadataTestRules(nil, nil)
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{
			"example.corp/cost-center": "INV-1234",
			"unrelated":                "ignored",
		},
	}}
	enforce := []*apirules.NamespaceRuleEnforceBody{{
		Metadata: []apirules.MetadataRule{{
			VersionKinds: runtime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Namespace"}},
			Annotations: map[string]apirules.MetadataValueRule{
				"example.corp/*": {},
			},
		}},
	}}

	entries, err := h.controlledMetadataEntries(
		obj,
		schema.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		enforce,
	)
	if err != nil {
		t.Fatalf("controlledMetadataEntries() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "example.corp/cost-center" {
		t.Fatalf("unexpected pattern-matched entries: %#v", entries)
	}
	if h.regexCache.Stats() != 1 {
		t.Fatalf("expected key expression to use regex cache, got %d entries", h.regexCache.Stats())
	}
}

func TestControlledMetadataEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		managedLabels      []string
		managedAnnotations []string
		obj                genericObject
		gvk                schema.GroupVersionKind
		enforceBodies      []*apirules.NamespaceRuleEnforceBody
		want               []metadataEntry
	}{
		{
			name:          "no matching metadata returns nil",
			obj:           metadataObject(nil, nil),
			gvk:           coreGVK("ConfigMap"),
			enforceBodies: nil,
			want:          nil,
		},
		{
			name: "collects present label and annotation entries",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				map[string]string{"example.corp/cost-center": "INV-1234"},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("prod")),
					},
					map[string]apirules.MetadataValueRule{
						"example.corp/cost-center": metadataPolicy(false, expression("^INV-")),
					},
				),
			},
			want: []metadataEntry{
				{
					Field:   metadataFieldAnnotation,
					Key:     "example.corp/cost-center",
					Value:   "INV-1234",
					Path:    `metadata.annotations["example.corp/cost-center"]`,
					Present: true,
				},
				{
					Field:   metadataFieldLabel,
					Key:     "env",
					Value:   "prod",
					Path:    `metadata.labels["env"]`,
					Present: true,
				},
			},
		},
		{
			name: "includes missing required allow label",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldLabel,
					Key:      "env",
					Path:     `metadata.labels["env"]`,
					Required: true,
				},
			},
		},
		{
			name: "skips missing optional allow label",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("prod")),
					},
					nil,
				),
			},
			want: nil,
		},
		{
			name: "skips missing required deny and audit labels",
			obj:  metadataObject(nil, nil),
			gvk:  coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeDeny,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"deny-required": metadataPolicy(true, exact("true")),
					},
					nil,
				),
				enforceMetadata(
					apirules.ActionTypeAudit,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"audit-required": metadataPolicy(true, exact("true")),
					},
					nil,
				),
			},
			want: nil,
		},
		{
			name: "required flag is merged across matching rules",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(false, exact("prod")),
					},
					nil,
				),
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldLabel,
					Key:      "env",
					Value:    "prod",
					Path:     `metadata.labels["env"]`,
					Present:  true,
					Required: true,
				},
			},
		},
		{
			name: "does not collect non matching gvk rule",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("Secret"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: nil,
		},
		{
			name: "apiVersion empty matches only core v1",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{""},
					[]string{"Deployment"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: nil,
		},
		{
			name: "apiVersion wildcard matches grouped resources",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"Deployment"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldLabel,
					Key:      "env",
					Value:    "prod",
					Path:     `metadata.labels["env"]`,
					Present:  true,
					Required: true,
				},
			},
		},
		{
			name: "multiple kinds match selected kind",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("Service"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap", "Service"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldLabel,
					Key:      "env",
					Value:    "prod",
					Path:     `metadata.labels["env"]`,
					Present:  true,
					Required: true,
				},
			},
		},
		{
			name: "kind wildcard matches any kind",
			obj: metadataObject(
				map[string]string{"env": "prod"},
				nil,
			),
			gvk: coreGVK("Service"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"*"},
					map[string]apirules.MetadataValueRule{
						"env": metadataPolicy(true, exact("prod")),
					},
					nil,
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldLabel,
					Key:      "env",
					Value:    "prod",
					Path:     `metadata.labels["env"]`,
					Present:  true,
					Required: true,
				},
			},
		},
		{
			name: "managed label and annotation are skipped",
			obj: metadataObject(
				map[string]string{
					meta.TenantLabel: "tenant-a",
				},
				map[string]string{
					meta.ReconcileAnnotation: "true",
				},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						meta.TenantLabel: metadataPolicy(true, exact("tenant-b")),
					},
					map[string]apirules.MetadataValueRule{
						meta.ReconcileAnnotation: metadataPolicy(true, exact("false")),
					},
				),
			},
			want: nil,
		},
		{
			name: "managed metadata does not satisfy required selectors",
			obj: metadataObject(
				map[string]string{
					meta.TenantLabel: "tenant-a",
				},
				map[string]string{
					meta.ReconcileAnnotation: "true",
				},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"capsule.clastix.io/*": metadataPolicy(true, exact("tenant-a")),
					},
					map[string]apirules.MetadataValueRule{
						"reconcile.projectcapsule.dev/*": metadataPolicy(true, exact("true")),
					},
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldAnnotation,
					Key:      "reconcile.projectcapsule.dev/*",
					Path:     `metadata.annotations["reconcile.projectcapsule.dev/*"]`,
					Required: true,
				},
				{
					Field:    metadataFieldLabel,
					Key:      "capsule.clastix.io/*",
					Path:     `metadata.labels["capsule.clastix.io/*"]`,
					Required: true,
				},
			},
		},
		{
			name: "custom parameters add label and annotation to managed metadata",
			managedLabels: []string{
				meta.TenantLabel,
			},
			managedAnnotations: []string{
				meta.ReconcileAnnotation,
			},
			obj: metadataObject(
				map[string]string{
					meta.TenantLabel: "tenant-a",
				},
				map[string]string{
					meta.ReconcileAnnotation: "true",
				},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						meta.TenantLabel: metadataPolicy(true, exact("tenant-a")),
					},
					map[string]apirules.MetadataValueRule{
						meta.ReconcileAnnotation: metadataPolicy(true, exact("true")),
					},
				),
			},
			want: nil,
		},
		{
			name: "managed annotation prefixes are skipped",
			obj: metadataObject(
				nil,
				map[string]string{
					meta.ResourceQuotaAnnotationPrefix + "cpu": "1",
				},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					nil,
					map[string]apirules.MetadataValueRule{
						meta.ResourceQuotaAnnotationPrefix + "cpu": metadataPolicy(true, exact("2")),
					},
				),
			},
			want: nil,
		},
		{
			name: "same key in labels and annotations is tracked independently",
			obj: metadataObject(
				map[string]string{"shared": "label"},
				map[string]string{"shared": "annotation"},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"shared": metadataPolicy(true, exact("label")),
					},
					map[string]apirules.MetadataValueRule{
						"shared": metadataPolicy(true, exact("annotation")),
					},
				),
			},
			want: []metadataEntry{
				{
					Field:    metadataFieldAnnotation,
					Key:      "shared",
					Value:    "annotation",
					Path:     `metadata.annotations["shared"]`,
					Present:  true,
					Required: true,
				},
				{
					Field:    metadataFieldLabel,
					Key:      "shared",
					Value:    "label",
					Path:     `metadata.labels["shared"]`,
					Present:  true,
					Required: true,
				},
			},
		},
		{
			name: "output is sorted by path",
			obj: metadataObject(
				map[string]string{
					"z": "1",
					"a": "1",
				},
				map[string]string{
					"m": "1",
				},
			),
			gvk: coreGVK("ConfigMap"),
			enforceBodies: []*apirules.NamespaceRuleEnforceBody{
				enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{
						"z": metadataPolicy(false, exact("1")),
						"a": metadataPolicy(false, exact("1")),
					},
					map[string]apirules.MetadataValueRule{
						"m": metadataPolicy(false, exact("1")),
					},
				),
			},
			want: []metadataEntry{
				{
					Field:   metadataFieldAnnotation,
					Key:     "m",
					Value:   "1",
					Path:    `metadata.annotations["m"]`,
					Present: true,
				},
				{
					Field:   metadataFieldLabel,
					Key:     "a",
					Value:   "1",
					Path:    `metadata.labels["a"]`,
					Present: true,
				},
				{
					Field:   metadataFieldLabel,
					Key:     "z",
					Value:   "1",
					Path:    `metadata.labels["z"]`,
					Present: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newMetadataTestRules(tt.managedLabels, tt.managedAnnotations)

			got, err := h.controlledMetadataEntries(tt.obj, tt.gvk, tt.enforceBodies)
			if err != nil {
				t.Fatalf("controlledMetadataEntries() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("unexpected entries\nwant: %#v\n got: %#v", tt.want, got)
			}
		})
	}
}

func TestMetadataSet(t *testing.T) {
	t.Parallel()

	t.Run("values returns the captured metadata entry value", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		entry := metadataEntry{
			Field: metadataFieldLabel,
			Key:   "env",
			Value: "prod",
			Path:  `metadata.labels["env"]`,
		}

		set := h.metadataSet(coreGVK("ConfigMap"), entry)
		got := set.Values(metadataObject(nil, nil))

		want := []ruleengine.Value{
			{
				Value: "prod",
				Path:  `metadata.labels["env"]`,
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	})

	t.Run("rules returns label values from matching gvk only", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		entry := metadataEntry{
			Field: metadataFieldLabel,
			Key:   "env",
			Value: "prod",
			Path:  `metadata.labels["env"]`,
		}

		set := h.metadataSet(coreGVK("ConfigMap"), entry)

		got, err := set.RulesWithError(enforceMetadata(
			apirules.ActionTypeAllow,
			[]string{"*"},
			[]string{"ConfigMap"},
			map[string]apirules.MetadataValueRule{
				"env": metadataPolicy(true, exact("prod"), expression("^p")),
			},
			nil,
		))
		if err != nil {
			t.Fatalf("RulesWithError() error = %v", err)
		}

		if len(got) != 2 {
			t.Fatalf("expected two matchers, got %d", len(got))
		}
		if got[0].Exact[0] != "prod" {
			t.Fatalf("expected first exact matcher, got %#v", got[0])
		}
		if got[1].Expression != "^p" {
			t.Fatalf("expected second expression matcher, got %#v", got[1])
		}
	})

	t.Run("rules returns annotation values from matching gvk only", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		entry := metadataEntry{
			Field: metadataFieldAnnotation,
			Key:   "cost-center",
			Value: "INV-1234",
			Path:  `metadata.annotations["cost-center"]`,
		}

		set := h.metadataSet(coreGVK("ConfigMap"), entry)

		got, err := set.RulesWithError(enforceMetadata(
			apirules.ActionTypeAllow,
			[]string{"*"},
			[]string{"ConfigMap"},
			nil,
			map[string]apirules.MetadataValueRule{
				"cost-center": metadataPolicy(true, expression("^INV-[0-9]{4}$")),
			},
		))
		if err != nil {
			t.Fatalf("RulesWithError() error = %v", err)
		}

		if len(got) != 1 {
			t.Fatalf("expected one matcher, got %d", len(got))
		}
		if got[0].Expression != "^INV-[0-9]{4}$" {
			t.Fatalf("expected expression matcher, got %#v", got[0])
		}
	})

	t.Run("rules returns nil for nil enforce", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{
			Field: metadataFieldLabel,
			Key:   "env",
		})

		got, err := set.RulesWithError(nil)
		if err != nil {
			t.Fatalf("RulesWithError() error = %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("rules returns nil for non matching gvk", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("Secret"), metadataEntry{
			Field: metadataFieldLabel,
			Key:   "env",
		})

		got, err := set.RulesWithError(enforceMetadata(
			apirules.ActionTypeAllow,
			[]string{"*"},
			[]string{"ConfigMap"},
			map[string]apirules.MetadataValueRule{
				"env": metadataPolicy(true, exact("prod")),
			},
			nil,
		))
		if err != nil {
			t.Fatalf("RulesWithError() error = %v", err)
		}

		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("rules returns invalid key selector errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			entry metadataEntry
			body  *apirules.NamespaceRuleEnforceBody
		}{
			{
				name:  "label",
				entry: metadataEntry{Field: metadataFieldLabel, Key: "env"},
				body: enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					map[string]apirules.MetadataValueRule{"[": metadataPolicy(false, exact("prod"))},
					nil,
				),
			},
			{
				name:  "annotation",
				entry: metadataEntry{Field: metadataFieldAnnotation, Key: "env"},
				body: enforceMetadata(
					apirules.ActionTypeAllow,
					[]string{"*"},
					[]string{"ConfigMap"},
					nil,
					map[string]apirules.MetadataValueRule{"[": metadataPolicy(false, exact("prod"))},
				),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				set := newMetadataTestRules(nil, nil).metadataSet(coreGVK("ConfigMap"), tt.entry)
				if _, err := set.RulesWithError(tt.body); err == nil {
					t.Fatal("RulesWithError() error = nil, want invalid selector error")
				}
			})
		}
	})

	t.Run("matches exact value", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{})

		got, err := set.Matches(exact("prod"), ruleengine.Value{Value: "prod"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !got.Matched {
			t.Fatalf("expected match")
		}
	})

	t.Run("matches regex value", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{})

		got, err := set.Matches(expression("^prod|test$"), ruleengine.Value{Value: "prod"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !got.Matched {
			t.Fatalf("expected match")
		}
	})

	t.Run("returns regex error", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{})

		if _, err := set.Matches(expression("["), ruleengine.Value{Value: "prod"}); err == nil {
			t.Fatalf("expected regex error")
		}
	})

	t.Run("rule description delegates to runtime description", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{})

		got := set.RuleDescription(expression("^prod$"))
		if !strings.Contains(got, "^prod$") {
			t.Fatalf("expected description to contain expression, got %q", got)
		}
	})

	t.Run("set metadata fields are stable", func(t *testing.T) {
		t.Parallel()

		h := newMetadataTestRules(nil, nil)
		set := h.metadataSet(coreGVK("ConfigMap"), metadataEntry{
			Field: metadataFieldAnnotation,
			Key:   "cost-center",
		})

		if set.Name != "metadata annotation" {
			t.Fatalf("expected metadata annotation set name, got %q", set.Name)
		}
		if set.EventReason != events.ReasonForbiddenMetadata {
			t.Fatalf("expected event reason %q, got %q", events.ReasonForbiddenMetadata, set.EventReason)
		}
		if set.AllowedDescription != "Allowed metadata values" {
			t.Fatalf("expected allowed description, got %q", set.AllowedDescription)
		}
	})
}

func TestMetadataRequiredDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry metadataEntry
		want  *ruleengine.Decision
	}{
		{
			name: "label decision",
			entry: metadataEntry{
				Field: metadataFieldLabel,
				Key:   "env",
				Path:  `metadata.labels["env"]`,
			},
			want: &ruleengine.Decision{
				SetName:     "metadata label",
				EventReason: events.ReasonForbiddenMetadata,
				Action:      apirules.ActionTypeDeny,
				Value: ruleengine.Value{
					Value: "",
					Path:  `metadata.labels["env"]`,
				},
				Message: `metadata label "env" is required at metadata.labels["env"]`,
			},
		},
		{
			name: "annotation decision",
			entry: metadataEntry{
				Field: metadataFieldAnnotation,
				Key:   "cost-center",
				Path:  `metadata.annotations["cost-center"]`,
			},
			want: &ruleengine.Decision{
				SetName:     "metadata annotation",
				EventReason: events.ReasonForbiddenMetadata,
				Action:      apirules.ActionTypeDeny,
				Value: ruleengine.Value{
					Value: "",
					Path:  `metadata.annotations["cost-center"]`,
				},
				Message: `metadata annotation "cost-center" is required at metadata.annotations["cost-center"]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := metadataRequiredDecision(tt.entry)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("unexpected decision\nwant: %#v\n got: %#v", tt.want, got)
			}
		})
	}
}

func TestMetadataHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "label set name",
			got:  metadataSetName(metadataFieldLabel),
			want: "metadata label",
		},
		{
			name: "annotation set name",
			got:  metadataSetName(metadataFieldAnnotation),
			want: "metadata annotation",
		},
		{
			name: "unknown set name",
			got:  metadataSetName(metadataField("unknown")),
			want: "metadata",
		},
		{
			name: "label path",
			got:  metadataLabelPath("example.com/key"),
			want: `metadata.labels["example.com/key"]`,
		},
		{
			name: "annotation path",
			got:  metadataAnnotationPath("example.com/key"),
			want: `metadata.annotations["example.com/key"]`,
		},
		{
			name: "label path quotes key",
			got:  metadataLabelPath(`a"b`),
			want: `metadata.labels["a\"b"]`,
		},
		{
			name: "annotation path quotes key",
			got:  metadataAnnotationPath(`a"b`),
			want: `metadata.annotations["a\"b"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, tt.got)
			}
		})
	}
}

func newMetadataTestRules(
	managedLabels []string,
	managedAnnotations []string,
) *genericRules {
	return &genericRules{
		regexCache: cache.NewRegexCache(),
		managedMetadata: meta.NewManagedMetadata(
			managedLabels,
			managedAnnotations,
		),
	}
}

func metadataObject(
	labels map[string]string,
	annotations map[string]string,
) genericObject {
	return &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
	}
}

func coreGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    kind,
	}
}

func enforceMetadata(
	action apirules.ActionType,
	apiVersion []string,
	kinds []string,
	labels map[string]apirules.MetadataValueRule,
	annotations map[string]apirules.MetadataValueRule,
) *apirules.NamespaceRuleEnforceBody {
	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Metadata: []apirules.MetadataRule{
			{
				VersionKinds: runtime.VersionKinds{
					APIGroups: apiVersion,
					Kinds:     kinds,
				},
				Labels:      labels,
				Annotations: annotations,
			},
		},
	}
}

func metadataPolicy(
	required bool,
	values ...runtime.ExpressionMatch,
) apirules.MetadataValueRule {
	return apirules.MetadataValueRule{
		Required: required,
		Values:   values,
	}
}

func exact(values ...string) runtime.ExpressionMatch {
	return runtime.ExpressionMatch{
		Exact: values,
	}
}

func expression(value string) runtime.ExpressionMatch {
	return runtime.ExpressionMatch{
		ExpressionRegex: runtime.ExpressionRegex{
			Expression: value,
		},
	}
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"
	"testing"
)

func TestValidateSchemaCapsuleFormExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{
			name: "core namespaced resource",
			schema: `
type: object
properties:
  secret:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: v1
        kind: Secret
        namespace: request
        labelSelector: app.kubernetes.io/part-of=payments
        fieldSelector: metadata.name!=ignored
      option:
        labelTemplate: '{{ .metadata.name }} ({{ .metadata.namespace }})'
        valueTemplate: '{{ .metadata.namespace }}/{{ .metadata.name }}'
`,
		},
		{
			name: "arbitrary custom resource GVK",
			schema: `
type: object
properties:
  route:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: gateway.networking.k8s.io/v1
        kind: HTTPRoute
        namespace: tenant-ingress
`,
		},
		{
			name: "cluster scoped resource with default option rendering",
			schema: `
type: object
properties:
  storageClass:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: storage.k8s.io/v1
        kind: StorageClass
`,
		},
		{
			name: "all namespaces",
			schema: `
type: object
properties:
  configMap:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: v1
        kind: ConfigMap
        namespace: '*'
`,
		},
		{
			name: "extension in reusable definition",
			schema: `
type: object
$defs:
  secretName:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: v1
        kind: Secret
properties:
  secret:
    $ref: '#/$defs/secretName'
`,
		},
		{
			name: "unknown widget",
			schema: `
type: string
x-capsule-form:
  widget: remote-api
  source:
    apiVersion: v1
    kind: Secret
`,
			wantErr: `widget must be "kubernetes-resource"`,
		},
		{
			name: "missing source",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
`,
			wantErr: "source is required",
		},
		{
			name: "missing API version",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    kind: Secret
`,
			wantErr: "apiVersion is required",
		},
		{
			name: "invalid API version",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: apps//v1
    kind: Deployment
`,
			wantErr: `apiVersion "apps//v1" is invalid`,
		},
		{
			name: "missing kind",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
`,
			wantErr: "kind is required",
		},
		{
			name: "wildcard kind",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: '*'
`,
			wantErr: "kind must identify one concrete resource kind",
		},
		{
			name: "invalid literal namespace",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: Secret
    namespace: Not_A_Namespace
`,
			wantErr: `namespace "Not_A_Namespace" is invalid`,
		},
		{
			name: "invalid label selector",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: Secret
    labelSelector: app in
`,
			wantErr: "labelSelector is invalid",
		},
		{
			name: "invalid field selector",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: Secret
    fieldSelector: metadata.name in (one,two)
`,
			wantErr: "fieldSelector is invalid",
		},
		{
			name: "invalid option template",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: Secret
  option:
    valueTemplate: '{{ .metadata.name '
`,
			wantErr: "valueTemplate is invalid",
		},
		{
			name: "unknown extension property",
			schema: `
type: string
x-capsule-form:
  widget: kubernetes-resource
  source:
    apiVersion: v1
    kind: Secret
  value: '{{ .metadata.name }}'
`,
			wantErr: `unknown field "value"`,
		},
		{
			name: "invalid nested extension reports its property path",
			schema: `
type: object
properties:
  secret:
    type: string
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: v1
`,
			wantErr: "$.properties.secret.x-capsule-form is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ValidateSchema(y2j(tt.schema))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSchema() error = %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("ValidateSchema() error = nil, want substring %q", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSchema() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCapsuleFormExtensionDoesNotChangeParameterValidation(t *testing.T) {
	t.Parallel()

	schema := y2j(`
type: object
required:
  - secret
properties:
  secret:
    type: string
    pattern: '^[a-z0-9-]+$'
    x-capsule-form:
      widget: kubernetes-resource
      source:
        apiVersion: v1
        kind: Secret
        namespace: request
`)

	if err := Validate(schema, y2j("secret: database-credentials")); err != nil {
		t.Fatalf("Validate() rejected a valid selected value: %v", err)
	}

	if err := Validate(schema, y2j("secret: INVALID_VALUE")); err == nil {
		t.Fatal("Validate() accepted a value rejected by the property's JSON Schema")
	}
}

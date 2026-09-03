// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"
	"testing"
)

func TestValidateKubernetesCELServiceAccountPath(t *testing.T) {
	t.Parallel()

	schema := y2j(`
type: object
additionalProperties: false
required:
  - subjectKind
  - subjectName
properties:
  subjectKind:
    type: string
    enum:
      - User
      - Group
      - ServiceAccount
  subjectName:
    type: string
    minLength: 1
x-kubernetes-validations:
  - rule: "self.subjectKind != 'ServiceAccount' || self.subjectName.matches('^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?/[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$')"
    message: ServiceAccount subjects must use namespace/name
    reason: FieldValueInvalid
    fieldPath: .subjectName
`)

	tests := []struct {
		name      string
		params    string
		wantError string
	}{
		{
			name: "service account namespace and name",
			params: `
subjectKind: ServiceAccount
subjectName: operations/break-glass-runner
`,
		},
		{
			name: "service account without namespace",
			params: `
subjectKind: ServiceAccount
subjectName: break-glass-runner
`,
			wantError: "ServiceAccount subjects must use namespace/name",
		},
		{
			name: "user remains an arbitrary non-empty name",
			params: `
subjectKind: User
subjectName: alice@example.com
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(schema, y2j(tt.params))
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestValidateKubernetesCELNestedRuleAndMessageExpression(t *testing.T) {
	t.Parallel()

	schema := y2j(`
type: object
properties:
  subjects:
    type: array
    items:
      type: object
      required:
        - name
      properties:
        name:
          type: string
      x-kubernetes-validations:
        - rule: "!self.name.startsWith('system:')"
          message: subject name uses a reserved prefix
          messageExpression: "'subject ' + self.name + ' uses a reserved prefix'"
          fieldPath: .name
`)

	err := Validate(schema, y2j(`
subjects:
  - name: alice
  - name: system:masters
`))
	if err == nil || !strings.Contains(err.Error(), "subject system:masters uses a reserved prefix") {
		t.Fatalf("Validate() error = %v, want evaluated nested messageExpression", err)
	}
}

func TestValidateSchemaRejectsInvalidKubernetesCEL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		schema    string
		wantError string
	}{
		{
			name: "non boolean rule",
			schema: `
type: object
x-kubernetes-validations:
  - rule: self.subjectName
`,
			wantError: "expression must evaluate to bool",
		},
		{
			name: "non string message expression",
			schema: `
type: object
x-kubernetes-validations:
  - rule: "true"
    messageExpression: "42"
`,
			wantError: "expression must evaluate to string",
		},
		{
			name: "unknown rule field",
			schema: `
type: object
x-kubernetes-validations:
  - rule: "true"
    unknown: value
`,
			wantError: `unknown field "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ValidateSchema(y2j(tt.schema))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ValidateSchema() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

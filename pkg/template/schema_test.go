// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

var (
	schemaString = `
type: object
required: ["key1"]
properties:
  key1:
    type: string
`
	schemaStringNoAdditionalProperties = schemaString + `
additionalProperties: false`

	paramKey1Key2 = `
key1: value1
key2: value2`
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		params     string
		expectErr  bool
	}{
		{
			name:       "valid schema and valid params",
			schemaJSON: schemaString,
			params:     "key1: value1",
			expectErr:  false,
		},
		{
			name:       "valid schema and valid params (one allowed extra field)",
			schemaJSON: schemaString,
			params:     paramKey1Key2,
			expectErr:  false,
		},
		{
			name:       "valid schema and invalid params (one additional extra field)",
			schemaJSON: schemaStringNoAdditionalProperties,
			params:     paramKey1Key2,
			expectErr:  true,
		},
		{
			name:       "valid schema but invalid params",
			schemaJSON: schemaString,
			params:     "key1: 123",
			expectErr:  true,
		},
		{
			name:       "schema missing required field",
			schemaJSON: schemaString,
			params:     "",
			expectErr:  true,
		},
		{
			name:       "invalid schema JSON",
			schemaJSON: "type:",
			params:     "key1: value1",
			expectErr:  true,
		},
		{
			name:       "empty schema and params",
			schemaJSON: "",
			params:     "",
			expectErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(y2j(tt.schemaJSON), y2j(tt.params))
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func y2j(in string) []byte {
	m := make(map[string]any)
	err := yaml.Unmarshal([]byte(in), &m)
	if err != nil {
		panic(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name      string
		it        []byte
		params    string
		expectErr bool
		expected  []byte
	}{
		{
			name:      "Should create the same item if the source has no template params",
			it:        tplNestedValue,
			params:    "",
			expectErr: false,
			expected:  tplNestedValue,
		},
		{
			name:      "Should create a valid item if source has template params",
			it:        tplNestedValueParam,
			params:    "key1: value1",
			expectErr: false,
			expected:  tplNestedValue,
		},
		{
			name:      "Should fail if the template is invalid",
			it:        y2j(`key1: "{{{.key1}}"`),
			params:    "",
			expectErr: true,
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := RenderTemplate(tt.it, y2j(tt.params))
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, res)
			}
		})
	}
}

var (
	tplNestedValue = y2j(`
key1: value1
key2:
  nestedKey: nestedValue`)
	tplNestedValueParam = y2j(`
key1: "{{.key1}}"
key2:
  nestedKey: nestedValue`)
)

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

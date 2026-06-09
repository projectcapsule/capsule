// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const kubeBuilderType = "+kubebuilder:validation:Type="

func TestExtendedDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
		expected  ExtendedDuration
	}{
		{
			name:      "valid duration",
			input:     `"1h30m"`,
			expectErr: false,
			expected:  ExtendedDuration(time.Hour + 30*time.Minute),
		},
		{
			name:      "valid zero duration",
			input:     `"0s"`,
			expectErr: false,
			expected:  ExtendedDuration(0),
		},
		{
			name:      "invalid format",
			input:     `"not-a-duration"`,
			expectErr: true,
			expected:  ExtendedDuration(0),
		},
		{
			name:      "empty input",
			input:     `""`,
			expectErr: true,
			expected:  ExtendedDuration(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d ExtendedDuration
			err := d.UnmarshalJSON([]byte(tt.input))
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, d)
			}
		})
	}
}

func TestExtendedDuration_String(t *testing.T) {
	tests := []struct {
		name     string
		input    ExtendedDuration
		expected string
	}{
		{
			name:     "one hour",
			input:    ExtendedDuration(time.Hour),
			expected: "1h",
		},
		{
			name:     "hour and minutes",
			input:    ExtendedDuration(time.Hour + 30*time.Minute),
			expected: "1h30m",
		},
		{
			name:     "zero duration",
			input:    ExtendedDuration(0),
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtendedDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     ExtendedDuration
		expectErr bool
		expected  string
	}{
		{
			name:      "one hour",
			input:     ExtendedDuration(time.Hour),
			expectErr: false,
			expected:  `"1h"`,
		},
		{
			name:      "hour and minutes",
			input:     ExtendedDuration(time.Hour + 30*time.Minute),
			expectErr: false,
			expected:  `"1h30m"`,
		},
		{
			name:      "zero duration",
			input:     ExtendedDuration(0),
			expectErr: false,
			expected:  `"0s"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.MarshalJSON()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, string(result))
			}
		})
	}
}

func TestExtendedDuration_ToUnstructured(t *testing.T) {
	tests := []struct {
		name     string
		input    ExtendedDuration
		expected string
	}{
		{
			name:     "one hour",
			input:    ExtendedDuration(time.Hour),
			expected: "1h",
		},
		{
			name:     "hour and minutes",
			input:    ExtendedDuration(time.Hour + 30*time.Minute),
			expected: "1h30m",
		},
		{
			name:     "zero duration",
			input:    ExtendedDuration(0),
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.ToUnstructured()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtendedDuration_OpenAPISchemaType(t *testing.T) {
	t.Run("should return correct schema type", func(t *testing.T) {
		var d ExtendedDuration
		assert.Equal(t, []string{"string"}, d.OpenAPISchemaType())
	})

	t.Run("should verify the schema type matches the kubebuilder comment", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(
			fset,
			"extended-duration_types.go",
			nil,
			parser.ParseComments,
		)
		require.NoError(t, err)

		var d ExtendedDuration
		schemaType := findKubeBuilderComment(file)
		require.Len(t, schemaType, 1)
		assert.Equal(t, d.OpenAPISchemaType()[0], schemaType[0])
	})
}

func TestExtendedDuration_OpenAPISchemaFormat(t *testing.T) {
	t.Run("should return correct schema format", func(t *testing.T) {
		var d ExtendedDuration
		assert.Equal(t, "", d.OpenAPISchemaFormat())
	})
}

func findKubeBuilderComment(file *ast.File) []string {
	var schemaType []string
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, kubeBuilderType) {
				schemaType = append(
					schemaType,
					strings.Split(c.Text, kubeBuilderType)[1],
				)
			}
		}
	}
	return schemaType
}

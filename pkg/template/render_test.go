// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTemplateBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		context    map[string]any
		key        MissingKeyOption
		tpl        string
		want       string
		wantErr    string
		wantErrNot string
	}{
		{
			name: "renders values from nested context",
			context: map[string]any{
				"tenant": map[string]any{
					"metadata": map[string]any{
						"name": "solar",
					},
				},
				"namespace": map[string]any{
					"metadata": map[string]any{
						"name": "solar-prod",
					},
				},
			},
			key:  MissingKeyOption("error"),
			tpl:  `{{ .tenant.metadata.name }}/{{ .namespace.metadata.name }}/app:1`,
			want: "solar/solar-prod/app:1",
		},
		{
			name: "renders map keys using index",
			context: map[string]any{
				"namespace": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"registry-prefix": "harbor/team-a",
						},
					},
				},
			},
			key:  MissingKeyOption("error"),
			tpl:  `{{ index .namespace.metadata.labels "registry-prefix" }}/app:1`,
			want: "harbor/team-a/app:1",
		},
		{
			name: "renders sprig functions",
			context: map[string]any{
				"registry": "harbor",
			},
			key:  MissingKeyOption("error"),
			tpl:  `{{ .registry | upper }}/app:1`,
			want: "HARBOR/app:1",
		},
		{
			name: "missing key returns execute error when missingkey error is enabled",
			context: map[string]any{
				"namespace": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{},
					},
				},
			},
			key:     MissingKeyOption("error"),
			tpl:     `{{ .namespace.metadata.labels.registry }}/app:1`,
			wantErr: "execute template",
		},
		{
			name: "missing key invalid renders placeholder-like output",
			context: map[string]any{
				"namespace": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{},
					},
				},
			},
			key:        MissingKeyOption("invalid"),
			tpl:        `{{ .namespace.metadata.labels.registry }}/app:1`,
			wantErrNot: "execute template",
			want:       "<no value>/app:1",
		},
		{
			name: "parse error is wrapped",
			context: map[string]any{
				"tenant": map[string]any{
					"metadata": map[string]any{
						"name": "solar",
					},
				},
			},
			key:     MissingKeyOption("error"),
			tpl:     `{{ .tenant.metadata.name `,
			wantErr: "parse template",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderTemplateBytes(tt.context, tt.key, []byte(tt.tpl))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("RenderTemplateBytes() error = nil, want error containing %q", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RenderTemplateBytes() error = %q, want containing %q", err.Error(), tt.wantErr)
				}

				return
			}

			if tt.wantErrNot != "" && err != nil && strings.Contains(err.Error(), tt.wantErrNot) {
				t.Fatalf("RenderTemplateBytes() error = %q, want not containing %q", err.Error(), tt.wantErrNot)
			}

			if err != nil {
				t.Fatalf("RenderTemplateBytes() unexpected error: %v", err)
			}

			if string(got) != tt.want {
				t.Fatalf("RenderTemplateBytes() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestRenderTemplateBytes_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := []byte(`{{ .tenant.metadata.name }}/app:1`)
	original := append([]byte(nil), input...)

	_, err := RenderTemplateBytes(
		map[string]any{
			"tenant": map[string]any{
				"metadata": map[string]any{
					"name": "solar",
				},
			},
		},
		MissingKeyOption("error"),
		input,
	)
	if err != nil {
		t.Fatalf("RenderTemplateBytes() unexpected error: %v", err)
	}

	if !bytes.Equal(input, original) {
		t.Fatalf("RenderTemplateBytes() mutated input: got %q, want %q", input, original)
	}
}

func TestWithLineNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single line",
			in:   "hello",
			want: "1 | hello\n",
		},
		{
			name: "multiple lines",
			in:   "alpha\nbeta\ngamma",
			want: "1 | alpha\n2 | beta\n3 | gamma\n",
		},
		{
			name: "trailing newline includes empty final line",
			in:   "alpha\nbeta\n",
			want: "1 | alpha\n2 | beta\n3 | \n",
		},
		{
			name: "pads line numbers for double digits",
			in:   strings.Repeat("x\n", 10) + "x",
			want: " 1 | x\n 2 | x\n 3 | x\n 4 | x\n 5 | x\n 6 | x\n 7 | x\n 8 | x\n 9 | x\n10 | x\n11 | x\n",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := withLineNumbers(tt.in)
			if got != tt.want {
				t.Fatalf("withLineNumbers() = %q, want %q", got, tt.want)
			}
		})
	}
}

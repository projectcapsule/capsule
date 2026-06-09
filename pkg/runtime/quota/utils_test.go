// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestValidateQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		quantity   resource.Quantity
		wantErr    bool
		errContain string
	}{
		{
			name:     "positive integer quantity is valid",
			quantity: resource.MustParse("1"),
			wantErr:  false,
		},
		{
			name:     "positive milli quantity is valid",
			quantity: resource.MustParse("100m"),
			wantErr:  false,
		},
		{
			name:     "positive binary memory quantity is valid",
			quantity: resource.MustParse("128Mi"),
			wantErr:  false,
		},
		{
			name:     "positive decimal memory quantity is valid",
			quantity: resource.MustParse("1Gi"),
			wantErr:  false,
		},
		{
			name:       "zero quantity is invalid",
			quantity:   resource.MustParse("0"),
			wantErr:    true,
			errContain: "quantity must not be negative or 0",
		},
		{
			name:       "negative integer quantity is invalid",
			quantity:   resource.MustParse("-1"),
			wantErr:    true,
			errContain: "quantity must not be negative or 0",
		},
		{
			name:       "negative milli quantity is invalid",
			quantity:   resource.MustParse("-100m"),
			wantErr:    true,
			errContain: "quantity must not be negative or 0",
		},
		{
			name:       "negative binary quantity is invalid",
			quantity:   resource.MustParse("-128Mi"),
			wantErr:    true,
			errContain: "quantity must not be negative or 0",
		},
		{
			name:     "very small positive milli quantity is valid",
			quantity: resource.MustParse("1m"),
			wantErr:  false,
		},
		{
			name:     "large positive quantity is valid",
			quantity: resource.MustParse("999999999999"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateQuantity(tt.quantity)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContain, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestClampQuantityToZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   resource.Quantity
		want resource.Quantity
	}{
		{
			name: "positive quantity is unchanged",
			in:   resource.MustParse("5"),
			want: resource.MustParse("5"),
		},
		{
			name: "positive milli quantity is unchanged",
			in:   resource.MustParse("250m"),
			want: resource.MustParse("250m"),
		},
		{
			name: "zero quantity is unchanged",
			in:   resource.MustParse("0"),
			want: resource.MustParse("0"),
		},
		{
			name: "negative integer quantity is clamped to zero",
			in:   resource.MustParse("-5"),
			want: resource.MustParse("0"),
		},
		{
			name: "negative milli quantity is clamped to zero",
			in:   resource.MustParse("-250m"),
			want: resource.MustParse("0"),
		},
		{
			name: "negative memory quantity is clamped to zero",
			in:   resource.MustParse("-1Gi"),
			want: resource.MustParse("0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.DeepCopy()

			ClampQuantityToZero(&got)

			if !QuantityEqual(got, tt.want) {
				t.Fatalf("expected %q, got %q", tt.want.String(), got.String())
			}
		})
	}
}

func TestNegateQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   resource.Quantity
		want resource.Quantity
	}{
		{
			name: "positive integer becomes negative",
			in:   resource.MustParse("5"),
			want: resource.MustParse("-5"),
		},
		{
			name: "negative integer becomes positive",
			in:   resource.MustParse("-5"),
			want: resource.MustParse("5"),
		},
		{
			name: "zero remains zero",
			in:   resource.MustParse("0"),
			want: resource.MustParse("0"),
		},
		{
			name: "positive milli becomes negative",
			in:   resource.MustParse("250m"),
			want: resource.MustParse("-250m"),
		},
		{
			name: "negative milli becomes positive",
			in:   resource.MustParse("-250m"),
			want: resource.MustParse("250m"),
		},
		{
			name: "positive binary memory becomes negative",
			in:   resource.MustParse("1Gi"),
			want: resource.MustParse("-1Gi"),
		},
		{
			name: "negative binary memory becomes positive",
			in:   resource.MustParse("-1Gi"),
			want: resource.MustParse("1Gi"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := tt.in.DeepCopy()

			got := NegateQuantity(tt.in)

			if !QuantityEqual(got, tt.want) {
				t.Fatalf("expected %q, got %q", tt.want.String(), got.String())
			}

			if !QuantityEqual(tt.in, original) {
				t.Fatalf("NegateQuantity mutated input: expected original %q, got %q", original.String(), tt.in.String())
			}
		})
	}
}

func TestQuantityEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    resource.Quantity
		b    resource.Quantity
		want bool
	}{
		{
			name: "same integer quantities are equal",
			a:    resource.MustParse("1"),
			b:    resource.MustParse("1"),
			want: true,
		},
		{
			name: "equivalent decimal and milli quantities are equal",
			a:    resource.MustParse("1"),
			b:    resource.MustParse("1000m"),
			want: true,
		},
		{
			name: "equivalent binary quantities are equal",
			a:    resource.MustParse("1Gi"),
			b:    resource.MustParse("1024Mi"),
			want: true,
		},
		{
			name: "different integer quantities are not equal",
			a:    resource.MustParse("1"),
			b:    resource.MustParse("2"),
			want: false,
		},
		{
			name: "positive and negative quantities are not equal",
			a:    resource.MustParse("1"),
			b:    resource.MustParse("-1"),
			want: false,
		},
		{
			name: "zero quantities are equal",
			a:    resource.MustParse("0"),
			b:    resource.MustParse("0"),
			want: true,
		},
		{
			name: "equivalent CPU quantities are equal",
			a:    resource.MustParse("500m"),
			b:    resource.MustParse("0.5"),
			want: true,
		},
		{
			name: "nearby milli quantities are not equal",
			a:    resource.MustParse("500m"),
			b:    resource.MustParse("501m"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := QuantityEqual(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("expected %t for %q and %q, got %t", tt.want, tt.a.String(), tt.b.String(), got)
			}
		})
	}
}

func TestClampQuantityToZeroMutatesOnlyNegativeInput(t *testing.T) {
	t.Parallel()

	q := resource.MustParse("-10")
	ClampQuantityToZero(&q)

	if !QuantityEqual(q, resource.MustParse("0")) {
		t.Fatalf("expected quantity to be clamped to zero, got %q", q.String())
	}

	q = resource.MustParse("10")
	ClampQuantityToZero(&q)

	if !QuantityEqual(q, resource.MustParse("10")) {
		t.Fatalf("expected positive quantity to remain unchanged, got %q", q.String())
	}
}

func TestNegateQuantityReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	in := resource.MustParse("10")
	out := NegateQuantity(in)

	if !QuantityEqual(in, resource.MustParse("10")) {
		t.Fatalf("expected input to remain unchanged, got %q", in.String())
	}

	if !QuantityEqual(out, resource.MustParse("-10")) {
		t.Fatalf("expected output to be negated, got %q", out.String())
	}

	ClampQuantityToZero(&out)

	if !QuantityEqual(out, resource.MustParse("0")) {
		t.Fatalf("expected output to be clampable independently, got %q", out.String())
	}

	if !QuantityEqual(in, resource.MustParse("10")) {
		t.Fatalf("expected input to remain unchanged after mutating output, got %q", in.String())
	}
}

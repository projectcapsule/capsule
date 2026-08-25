// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"strings"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func TestValidateCELExpressions(t *testing.T) {
	t.Parallel()

	celCache, err := cache.NewCELCache()
	if err != nil {
		t.Fatalf("NewCELCache() error = %v", err)
	}

	t.Run("accepts quantity calculation and boolean selectors", func(t *testing.T) {
		t.Parallel()

		err := validateCELExpressions(celCache, []capsulev1beta2.CustomQuotaSpecSource{
			{
				CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
					CEL: `quantity(object.spec.resources.requests["cpu"])`,
					Selectors: []selectors.SelectorWithFields{
						{
							CELExpressions: []string{
								`object.spec.restartPolicy == "Always"`,
							},
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("validateCELExpressions() error = %v", err)
		}
	})

	t.Run("rejects non-quantity calculation output", func(t *testing.T) {
		t.Parallel()

		err := validateCELExpressions(celCache, []capsulev1beta2.CustomQuotaSpecSource{
			{
				CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
					CEL: `object.spec.enabled == true`,
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "kubernetes.Quantity") {
			t.Fatalf("validateCELExpressions() error = %v, want quantity output error", err)
		}
	})

	t.Run("rejects non-boolean selector output", func(t *testing.T) {
		t.Parallel()

		err := validateCELExpressions(celCache, []capsulev1beta2.CustomQuotaSpecSource{
			{
				CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
					Selectors: []selectors.SelectorWithFields{
						{
							CELExpressions: []string{
								`quantity("1")`,
							},
						},
					},
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "must evaluate to bool") {
			t.Fatalf("validateCELExpressions() error = %v, want boolean output error", err)
		}
	})
}

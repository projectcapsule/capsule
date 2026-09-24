// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	tenantvalidation "github.com/projectcapsule/capsule/internal/webhook/tenant/validation"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestClassRegexHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		handler       handlers.TypedHandler[*capsulev1beta2.Tenant]
		validTenant   *capsulev1beta2.Tenant
		invalidTenant *capsulev1beta2.Tenant
		emptyTenant   *capsulev1beta2.Tenant
	}{
		{
			name:    "PriorityClassRegexHandler",
			handler: tenantvalidation.PriorityClassRegexHandler(),
			validTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					PriorityClasses: &api.DefaultAllowedListSpec{
						Regex: "^gold-.*$",
					},
				},
			},
			invalidTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					PriorityClasses: &api.DefaultAllowedListSpec{
						Regex: "[",
					},
				},
			},
			emptyTenant: &capsulev1beta2.Tenant{},
		},
		{
			name:    "RuntimeClassRegexHandler",
			handler: tenantvalidation.RuntimeClassRegexHandler(),
			validTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					RuntimeClasses: &api.DefaultAllowedListSpec{
						Regex: "^kata-.*$",
					},
				},
			},
			invalidTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					RuntimeClasses: &api.DefaultAllowedListSpec{
						Regex: "[",
					},
				},
			},
			emptyTenant: &capsulev1beta2.Tenant{},
		},
		{
			name:    "GatewayClassRegexHandler",
			handler: tenantvalidation.GatewayClassRegexHandler(),
			validTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					GatewayOptions: capsulev1beta2.GatewayOptions{
						AllowedClasses: &api.DefaultAllowedListSpec{
							Regex: "^gw-.*$",
						},
					},
				},
			},
			invalidTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					GatewayOptions: capsulev1beta2.GatewayOptions{
						AllowedClasses: &api.DefaultAllowedListSpec{
							Regex: "[",
						},
					},
				},
			},
			emptyTenant: &capsulev1beta2.Tenant{},
		},
		{
			name:    "DeviceClassRegexHandler",
			handler: tenantvalidation.DeviceClassRegexHandler(),
			validTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					DeviceClasses: &api.SelectorAllowedListSpec{
						Regex: "^gpu-.*$",
					},
				},
			},
			invalidTenant: &capsulev1beta2.Tenant{
				Spec: capsulev1beta2.TenantSpec{
					DeviceClasses: &api.SelectorAllowedListSpec{
						Regex: "[",
					},
				},
			},
			emptyTenant: &capsulev1beta2.Tenant{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := admission.Request{}
			ctx := t.Context()

			// OnCreate valid
			res := tt.handler.OnCreate(nil, nil, tt.validTenant, nil, nil)(ctx, req)
			assert.Nil(t, res)

			// OnCreate invalid
			res = tt.handler.OnCreate(nil, nil, tt.invalidTenant, nil, nil)(ctx, req)
			require.NotNil(t, res)
			assert.False(t, res.Allowed)

			// OnCreate empty
			res = tt.handler.OnCreate(nil, nil, tt.emptyTenant, nil, nil)(ctx, req)
			assert.Nil(t, res)

			// OnUpdate valid
			res = tt.handler.OnUpdate(nil, nil, tt.validTenant, tt.emptyTenant, nil, nil)(ctx, req)
			assert.Nil(t, res)

			// OnUpdate invalid
			res = tt.handler.OnUpdate(nil, nil, tt.invalidTenant, tt.emptyTenant, nil, nil)(ctx, req)
			require.NotNil(t, res)
			assert.False(t, res.Allowed)

			// OnDelete always nil
			res = tt.handler.OnDelete(nil, nil, tt.validTenant, nil, nil)(ctx, req)
			assert.Nil(t, res)
		})
	}
}

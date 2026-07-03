// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/health"
)

// healthChecksValidationHandler rejects TenantResource/GlobalTenantResource objects
// whose spec.healthChecks contain invalid CEL expressions, so problems surface at
// admission rather than only at reconcile time.
type healthChecksValidationHandler struct{}

// HealthChecksValidationHandler validates the healthChecks of both the namespaced
// TenantResource and the cluster-scoped GlobalTenantResource.
func HealthChecksValidationHandler() handlers.Handler {
	return &healthChecksValidationHandler{}
}

func (h *healthChecksValidationHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(req, decoder)
	}
}

func (h *healthChecksValidationHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(req, decoder)
	}
}

func (h *healthChecksValidationHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *healthChecksValidationHandler) validate(req admission.Request, decoder admission.Decoder) *admission.Response {
	var checks []capsulev1beta2.HealthCheckSpec

	switch req.Kind.Kind {
	case "GlobalTenantResource":
		obj := &capsulev1beta2.GlobalTenantResource{}
		if err := decoder.Decode(req, obj); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode object: %w", err))
		}

		checks = obj.Spec.HealthChecks
	default:
		obj := &capsulev1beta2.TenantResource{}
		if err := decoder.Decode(req, obj); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode object: %w", err))
		}

		checks = obj.Spec.HealthChecks
	}

	if len(checks) == 0 {
		return nil
	}

	if err := health.Validate(checks); err != nil {
		return ad.Denyf("invalid spec.healthChecks: %v", err)
	}

	return nil
}

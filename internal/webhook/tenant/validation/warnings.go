// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type warningHandler struct {
	cfg configuration.Configuration
}

func WarningHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &warningHandler{
		cfg: cfg,
	}
}

func (h *warningHandler) OnCreate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt := &capsulev1beta2.Tenant{}
		if err := decoder.Decode(req, tnt); err != nil {
			return utils.ErroredResponse(err)
		}

		return h.handle(tnt, decoder, req)
	}
}

func (h *warningHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *warningHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		tnt := &capsulev1beta2.Tenant{}
		if err := decoder.Decode(req, tnt); err != nil {
			return utils.ErroredResponse(err)
		}

		return h.handle(tnt, decoder, req)
	}
}

func (h *warningHandler) handle(tnt *capsulev1beta2.Tenant, decoder admission.Decoder, req admission.Request) *admission.Response {
	response := &admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			UID:     req.UID,
			Allowed: true,
		},
	}

	if len(tnt.Spec.LimitRanges.Items) > 0 {
		response.Warnings = append(response.Warnings, "Limitranges are deprecated and will be removed int the future. You need to consider to migrate to TenantReplications: https://projectcapsule.dev/docs/tenants/enforcement/#limitrange-distribution-with-tenantreplications.")
	}

	if len(tnt.Spec.NetworkPolicies.Items) > 0 {
		response.Warnings = append(response.Warnings, "NetworkPolicies are deprecated and will be removed int the future. You need to consider to migrate to TenantReplications: https://projectcapsule.dev/docs/tenants/enforcement/#networkpolicy-distribution-with-tenantreplications.")
	}

	if tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.AdditionalMetadata != nil {
		response.Warnings = append(response.Warnings, "additionalMetadata is deprecated and will be removed int the future. You need to consider to migrate to AdditionalMetadataList: https://projectcapsule.dev/docs/tenants/enforcement/#additionalmetadatalist.")
	}

	if tnt.Spec.StorageClasses != nil && tnt.Spec.StorageClasses.Regex != "" {
		response.Warnings = append(response.Warnings, "Using the regex property to select StorageClasses is deprecated and will be removed int the future.")
	}

	if tnt.Spec.GatewayOptions.AllowedClasses != nil && tnt.Spec.GatewayOptions.AllowedClasses.Regex != "" {
		response.Warnings = append(response.Warnings, "Using the regex property to select GatewayClasses is deprecated and will be removed int the future.")
	}

	if tnt.Spec.PriorityClasses != nil && tnt.Spec.PriorityClasses.Regex != "" {
		response.Warnings = append(response.Warnings, "Using the regex property to select PriorityClasses is deprecated and will be removed int the future.")
	}

	if tnt.Spec.RuntimeClasses != nil && tnt.Spec.RuntimeClasses.Regex != "" {
		response.Warnings = append(response.Warnings, "Using the regex property to select RuntimeClasses is deprecated and will be removed int the future.")
	}

	return response
}

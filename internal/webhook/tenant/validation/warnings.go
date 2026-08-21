// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type warningHandler struct {
	cfg configuration.Configuration
}

func WarningHandler(cfg configuration.Configuration) handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &warningHandler{
		cfg: cfg,
	}
}

func (h *warningHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(tnt, req)
	}
}

func (h *warningHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *warningHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(tnt, req)
	}
}

func (h *warningHandler) handle(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	response := &admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			UID:     req.UID,
			Allowed: true,
		},
	}

	//nolint:staticcheck
	if tnt.Spec.ContainerRegistries != nil {
		if len(tnt.Spec.ContainerRegistries.Exact) > 0 || len(tnt.Spec.ContainerRegistries.Regex) > 0 {
			response.Warnings = append(response.Warnings,
				"The field `containerRegistries` is deprecated and will be removed in a future release. Please migrate to rules. See: https://projectcapsule.dev/docs/tenants/rules.",
			)
		}
	}

	response.Warnings = append(response.Warnings, deprecatedTenantFieldWarnings(tnt)...)

	//nolint:staticcheck
	if len(tnt.Spec.LimitRanges.Items) > 0 {
		response.Warnings = append(response.Warnings,
			"The field `limitRanges` is deprecated and will be removed in a future release. Please migrate to TenantReplications. See: https://projectcapsule.dev/docs/tenants/enforcement/#limitrange-distribution-with-tenantreplications.",
		)
	}

	//nolint:staticcheck
	if len(tnt.Spec.NetworkPolicies.Items) > 0 {
		response.Warnings = append(response.Warnings,
			"The field `networkPolicies` is deprecated and will be removed in a future release. Please migrate to TenantReplications. See: https://projectcapsule.dev/docs/tenants/enforcement/#networkpolicy-distribution-with-tenantreplications.",
		)
	}

	response.Warnings = append(response.Warnings, deprecatedNamespaceOptionWarnings(tnt)...)

	//nolint:staticcheck
	if tnt.Spec.StorageClasses != nil && tnt.Spec.StorageClasses.Regex != "" {
		response.Warnings = append(response.Warnings,
			"The `regex` selector for StorageClasses is deprecated and will be removed in a future release.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.GatewayOptions.AllowedClasses != nil && tnt.Spec.GatewayOptions.AllowedClasses.Regex != "" {
		response.Warnings = append(response.Warnings,
			"The `regex` selector for GatewayClasses is deprecated and will be removed in a future release.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.PriorityClasses != nil && tnt.Spec.PriorityClasses.Regex != "" {
		response.Warnings = append(response.Warnings,
			"The `regex` selector for PriorityClasses is deprecated and will be removed in a future release.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.RuntimeClasses != nil && tnt.Spec.RuntimeClasses.Regex != "" {
		response.Warnings = append(response.Warnings,
			"The `regex` selector for RuntimeClasses is deprecated and will be removed in a future release.",
		)
	}

	if tnt.GetAnnotations() != nil {
		for k := range tnt.GetAnnotations() {
			if strings.HasPrefix(k, meta.ResourceQuotaAnnotationPrefix) {
				response.Warnings = append(response.Warnings,
					"custom quotas via tenant annotations are deprecated and will be removed in a future release.  Please migrate to GlobalCustomQuotas. See: https://projectcapsule.dev/docs/resource-management/customquotas/#globalcustomquota.",
				)
			}
		}
	}

	return response
}

func deprecatedTenantFieldWarnings(tnt *capsulev1beta2.Tenant) (warnings []string) {
	//nolint:staticcheck
	if tnt.Spec.ResourceQuota.Scope != "" || len(tnt.Spec.ResourceQuota.Items) > 0 {
		warnings = append(warnings,
			"The field `resourceQuotas` is deprecated and will be removed in a future release. Please migrate to rules quotas. See: https://projectcapsule.dev/docs/tenants/rules/#migration.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.ServiceOptions != nil {
		warnings = append(warnings,
			"The field `serviceOptions` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.devdocs/rules/enforcement/metadata/#migrate-service-metadata.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.PodOptions != nil {
		warnings = append(warnings,
			"The field `podOptions` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-pod-metadata.",
		)
	}

	return warnings
}

func deprecatedNamespaceOptionWarnings(tnt *capsulev1beta2.Tenant) (warnings []string) {
	if tnt.Spec.NamespaceOptions == nil {
		return warnings
	}

	//nolint:staticcheck
	if tnt.Spec.NamespaceOptions.AdditionalMetadata != nil {
		warnings = append(warnings,
			"The field `additionalMetadata` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-namespace-metadata.",
		)
	}

	//nolint:staticcheck
	if len(tnt.Spec.NamespaceOptions.AdditionalMetadataList) > 0 {
		warnings = append(warnings,
			"The field `additionalMetadataList` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-namespace-metadata.",
		)
	}

	//nolint:staticcheck
	if tnt.Spec.NamespaceOptions.RequiredMetadata != nil {
		warnings = append(warnings,
			"The field `requiredMetadata` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-namespace-metadata.",
		)
	}

	//nolint:staticcheck
	if len(tnt.Spec.NamespaceOptions.ForbiddenLabels.Exact) > 0 ||
		tnt.Spec.NamespaceOptions.ForbiddenLabels.Regex != "" {
		warnings = append(warnings,
			"The field `forbiddenLabels` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-namespace-metadata.",
		)
	}

	//nolint:staticcheck
	if len(tnt.Spec.NamespaceOptions.ForbiddenAnnotations.Exact) > 0 ||
		tnt.Spec.NamespaceOptions.ForbiddenAnnotations.Regex != "" {
		warnings = append(warnings,
			"The field `forbiddenAnnotations` is deprecated and will be removed in a future release. Please migrate to rules metadata. See: https://projectcapsule.dev/docs/rules/enforcement/metadata/#migrate-namespace-metadata.",
		)
	}

	return warnings
}

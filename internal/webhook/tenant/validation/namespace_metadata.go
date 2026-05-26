// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/template"
)

type namespaceMetadataHandler struct{}

func NamespaceMetadataHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &namespaceMetadataHandler{}
}

func (h *namespaceMetadataHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return validateTenantNamespaceMetadata(tnt)
	}
}

func (h *namespaceMetadataHandler) OnDelete(
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

func (h *namespaceMetadataHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *capsulev1beta2.Tenant,
	newTnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return validateTenantNamespaceMetadata(newTnt)
	}
}

func validateTenantNamespaceMetadata(tnt *capsulev1beta2.Tenant) *admission.Response {
	if tnt == nil {
		return nil
	}

	if tnt.Spec.NamespaceOptions == nil {
		return nil
	}

	var errs []string

	errs = append(
		errs,
		validateAdditionalMetadata(
			"spec.namespaceOptions.additionalMetadata",
			tnt.Spec.NamespaceOptions.AdditionalMetadata,
		)...,
	)

	for i, item := range tnt.Spec.NamespaceOptions.AdditionalMetadataList {
		errs = append(
			errs,
			validateAdditionalMetadata(
				fmt.Sprintf("spec.namespaceOptions.additionalMetadataList[%d].additionalMetadata", i),
				&api.AdditionalMetadataSpec{
					Labels:      item.Labels,
					Annotations: item.Annotations,
				},
			)...,
		)
	}

	if len(errs) > 0 {
		resp := admission.Denied(strings.Join(errs, "; "))

		return &resp
	}

	return nil
}

func validateAdditionalMetadata(
	fieldPath string,
	metadata *api.AdditionalMetadataSpec,
) []string {
	var errs []string

	if metadata == nil {
		return errs
	}

	errs = append(errs, validateLabelMap(fieldPath+".labels", metadata.Labels)...)
	errs = append(errs, validateAnnotationMap(fieldPath+".annotations", metadata.Annotations)...)

	return errs
}

func validateLabelMap(fieldPath string, labels map[string]string) []string {
	var errs []string

	for key, value := range labels {
		errs = append(
			errs,
			template.ValidateKubernetesStringOrAllowedTemplates(
				fmt.Sprintf("%s[%q]", fieldPath, key),
				key,
				validation.IsQualifiedName,
			)...,
		)

		errs = append(
			errs,
			template.ValidateKubernetesStringOrAllowedTemplates(
				fmt.Sprintf("%s[%q]", fieldPath, key),
				value,
				validation.IsValidLabelValue,
			)...,
		)
	}

	return errs
}

func validateAnnotationMap(fieldPath string, annotations map[string]string) []string {
	var errs []string

	for key := range annotations {
		errs = append(
			errs,
			template.ValidateKubernetesStringOrAllowedTemplates(
				fmt.Sprintf("%s[%q]", fieldPath, key),
				key,
				func(value string) []string {
					return validation.IsQualifiedName(strings.ToLower(value))
				},
			)...,
		)
	}

	return errs
}

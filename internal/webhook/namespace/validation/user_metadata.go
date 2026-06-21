// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"maps"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type userMetadataHandler struct{}

func UserMetadataHandler() handlers.TypedHandlerWithTenantUser[*corev1.Namespace] {
	return &userMetadataHandler{}
}

func (h *userMetadataHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if tnt.Spec.NamespaceOptions != nil {
			labels, annotations, err := userMetadataForValidation(ns, nil, tnt)
			if err != nil {
				return ad.ErroredResponse(err)
			}

			if response := validateUserMetadata(
				ctx,
				req,
				tnt,
				ns,
				labels,
				annotations,
				tnt.Spec.NamespaceOptions,
				recorder,
			); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *userMetadataHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ users.AdmissionUser,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if len(tnt.Spec.NodeSelector) > 0 {
			v, ok := newNs.GetAnnotations()["scheduler.alpha.kubernetes.io/node-selector"]
			if !ok {
				msg := "the annotation scheduler.alpha.kubernetes.io/node-selector is enforced via tenant, cannot be removed"

				recorder.LabeledEvent(
					oldNs,
					corev1.EventTypeWarning,
					events.ReasonForbiddenNodeSelectorUpdate,
					events.ActionValidationDenied,
					msg,
				).
					WithRelated(tnt).
					WithTenantLabel(tnt).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny(msg)
			}

			if v != oldNs.GetAnnotations()["scheduler.alpha.kubernetes.io/node-selector"] {
				msg := "the annotation scheduler.alpha.kubernetes.io/node-selector is enforced via tenant, cannot be updated"

				recorder.LabeledEvent(
					oldNs,
					corev1.EventTypeWarning,
					events.ReasonForbiddenNodeSelectorUpdate,
					events.ActionValidationDenied,
					msg,
				).
					WithRelated(tnt).
					WithTenantLabel(tnt).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny(msg)
			}
		}

		if tnt.Spec.NamespaceOptions != nil {
			labels, annotations, err := userMetadataForValidation(newNs, oldNs, tnt)
			if err != nil {
				return ad.ErroredResponse(err)
			}

			if response := validateUserMetadata(
				ctx,
				req,
				tnt,
				oldNs,
				labels,
				annotations,
				tnt.Spec.NamespaceOptions,
				recorder,
			); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *userMetadataHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func validateUserMetadata(
	ctx context.Context,
	req admission.Request,
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	labels map[string]string,
	annotations map[string]string,
	options *capsulev1beta2.NamespaceOptions,
	recorder events.EventRecorder,
) *admission.Response {
	err := api.ValidateForbidden(annotations, options.ForbiddenAnnotations)
	if err != nil {
		err = errors.Wrap(err, "namespace annotations validation failed")

		recorder.LabeledEvent(
			ns,
			corev1.EventTypeWarning,
			events.ReasonForbiddenAnnotation,
			events.ActionValidationDenied,
			err.Error(),
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(err.Error())
	}

	err = api.ValidateForbidden(labels, options.ForbiddenLabels)
	if err != nil {
		err = errors.Wrap(err, "namespace labels validation failed")

		recorder.LabeledEvent(
			ns,
			corev1.EventTypeWarning,
			events.ReasonForbiddenLabel,
			events.ActionValidationDenied,
			err.Error(),
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(err.Error())
	}

	return nil
}

func userMetadataForValidation(
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) (map[string]string, map[string]string, error) {
	labels := metadataForValidation(newNs.GetLabels(), nil)
	annotations := metadataForValidation(newNs.GetAnnotations(), nil)

	// On update, validate only metadata that was added or changed by the request.
	if oldNs != nil {
		labels = metadataForValidation(newNs.GetLabels(), oldNs.GetLabels())
		annotations = metadataForValidation(newNs.GetAnnotations(), oldNs.GetAnnotations())
	}

	managedLabels, managedAnnotations, err := tenant.BuildNamespaceMetadataForTenant(newNs, tnt)
	if err != nil {
		return nil, nil, err
	}

	tenant.AddNamespaceNameLabels(managedLabels, newNs)
	tenant.AddTenantNameLabel(managedLabels, tnt)

	removeManagedMetadata(labels, managedLabels)
	removeManagedMetadata(annotations, managedAnnotations)

	return labels, annotations, nil
}

func metadataForValidation(newMetadata, oldMetadata map[string]string) map[string]string {
	if oldMetadata == nil {
		return maps.Clone(newMetadata)
	}

	metadata := make(map[string]string)

	for key, newValue := range newMetadata {
		oldValue, ok := oldMetadata[key]
		if !ok || oldValue != newValue {
			metadata[key] = newValue
		}
	}

	return metadata
}

func removeManagedMetadata(metadata map[string]string, managed map[string]string) {
	for key, managedValue := range managed {
		value, ok := metadata[key]
		if !ok {
			continue
		}

		// Only ignore metadata Capsule itself would manage.
		// Same key with a different value is still user-controlled and must be validated.
		if value == managedValue {
			delete(metadata, key)
		}
	}
}

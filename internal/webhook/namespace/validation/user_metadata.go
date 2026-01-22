// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api"
)

type userMetadataHandler struct{}

func UserMetadataHandler() capsulewebhook.TypedHandlerWithTenant[*corev1.Namespace] {
	return &userMetadataHandler{}
}

func (h *userMetadataHandler) OnCreate(
	c client.Client,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if tnt.Spec.NamespaceOptions != nil {
			err := api.ValidateForbidden(ns.Annotations, tnt.Spec.NamespaceOptions.ForbiddenAnnotations)
			if err != nil {
				err = errors.Wrap(err, "namespace annotations validation failed")
				recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenAnnotationReason, err.Error())
				response := admission.Denied(err.Error())

				return &response
			}

			err = api.ValidateForbidden(ns.Labels, tnt.Spec.NamespaceOptions.ForbiddenLabels)
			if err != nil {
				err = errors.Wrap(err, "namespace labels validation failed")
				recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenLabelReason, err.Error())
				response := admission.Denied(err.Error())

				return &response
			}
		}

		return nil
	}
}

func (h *userMetadataHandler) OnUpdate(
	client client.Client,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if len(tnt.Spec.NodeSelector) > 0 {
			v, ok := newNs.GetAnnotations()["scheduler.alpha.kubernetes.io/node-selector"]
			if !ok {
				response := admission.Denied("the node-selector annotation is enforced, cannot be removed")

				recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenNodeSelectorDeletion", string(response.Result.Reason))

				return &response
			}

			if v != oldNs.GetAnnotations()["scheduler.alpha.kubernetes.io/node-selector"] {
				response := admission.Denied("the node-selector annotation is enforced, cannot be updated")

				recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenNodeSelectorUpdate", string(response.Result.Reason))

				return &response
			}
		}

		labels, annotations := oldNs.GetLabels(), oldNs.GetAnnotations()

		if labels == nil {
			labels = make(map[string]string)
		}

		if annotations == nil {
			annotations = make(map[string]string)
		}

		for key, value := range newNs.GetLabels() {
			v, ok := labels[key]
			if !ok {
				labels[key] = value

				continue
			}

			if v != value {
				continue
			}

			delete(labels, key)
		}

		for key, value := range newNs.GetAnnotations() {
			v, ok := annotations[key]
			if !ok {
				annotations[key] = value

				continue
			}

			if v != value {
				continue
			}

			delete(annotations, key)
		}

		if tnt.Spec.NamespaceOptions != nil {
			err := api.ValidateForbidden(annotations, tnt.Spec.NamespaceOptions.ForbiddenAnnotations)
			if err != nil {
				err = errors.Wrap(err, "namespace annotations validation failed")
				recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenAnnotationReason, err.Error())
				response := admission.Denied(err.Error())

				return &response
			}

			err = api.ValidateForbidden(labels, tnt.Spec.NamespaceOptions.ForbiddenLabels)
			if err != nil {
				err = errors.Wrap(err, "namespace labels validation failed")
				recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenLabelReason, err.Error())
				response := admission.Denied(err.Error())

				return &response
			}
		}

		return nil
	}
}

func (h *userMetadataHandler) OnDelete(
	client.Client,
	*corev1.Namespace,
	admission.Decoder,
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

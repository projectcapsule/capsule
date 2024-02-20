// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type userMetadataHandler struct{}

func UserMetadataHandler() capsulewebhook.Handler {
	return &userMetadataHandler{}
}

func (r *userMetadataHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt := &capsulev1beta2.Tenant{}
		for _, objectRef := range ns.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}
		}

		if tnt.Spec.NamespaceOptions != nil {
			err := api.ValidateForbidden(ns.ObjectMeta.Annotations, tnt.Spec.NamespaceOptions.ForbiddenAnnotations)
			if err != nil {
				err = errors.Wrap(err, "namespace annotations validation failed")
				recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenAnnotationReason, err.Error())
				response := admission.Denied(err.Error())

				return &response
			}

			err = api.ValidateForbidden(ns.ObjectMeta.Labels, tnt.Spec.NamespaceOptions.ForbiddenLabels)
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

func (r *userMetadataHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return utils.ErroredResponse(err)
		}

		newNs := &corev1.Namespace{}
		if err := decoder.Decode(req, newNs); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt := &capsulev1beta2.Tenant{}
		for _, objectRef := range newNs.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}
		}

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

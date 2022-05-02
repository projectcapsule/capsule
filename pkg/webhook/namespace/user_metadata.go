// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type userMetadataHandler struct{}

func UserMetadataHandler() capsulewebhook.Handler {
	return &userMetadataHandler{}
}

func (r *userMetadataHandler) validateUserMetadata(tnt *capsulev1beta1.Tenant, recorder record.EventRecorder, labels map[string]string, annotations map[string]string) *admission.Response {
	if tnt.ForbiddenUserNamespaceLabels() != nil {
		forbiddenLabels := tnt.ForbiddenUserNamespaceLabels()

		for label := range labels {
			var forbidden, matched bool
			forbidden = forbiddenLabels.ExactMatch(label)
			matched = forbiddenLabels.RegexMatch(label)

			if forbidden || matched {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenNamespaceLabel", fmt.Sprintf("Label %s is forbidden for a namespaces of the current Tenant ", label))

				response := admission.Denied(NewNamespaceLabelForbiddenError(label, forbiddenLabels).Error())

				return &response
			}
		}
	}

	if tnt.ForbiddenUserNamespaceAnnotations() != nil {
		forbiddenAnnotations := tnt.ForbiddenUserNamespaceLabels()

		for annotation := range annotations {
			var forbidden, matched bool
			forbidden = forbiddenAnnotations.ExactMatch(annotation)
			matched = forbiddenAnnotations.RegexMatch(annotation)

			if forbidden || matched {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenNamespaceAnnotation", fmt.Sprintf("Annotation %s is forbidden for a namespaces of the current Tenant ", annotation))

				response := admission.Denied(NewNamespaceAnnotationForbiddenError(annotation, forbiddenAnnotations).Error())

				return &response
			}
		}
	}

	return nil
}

func (r *userMetadataHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt := &capsulev1beta1.Tenant{}
		for _, objectRef := range ns.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}
		}

		labels := ns.GetLabels()
		annotations := ns.GetAnnotations()

		return r.validateUserMetadata(tnt, recorder, labels, annotations)
	}
}

func (r *userMetadataHandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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

		tnt := &capsulev1beta1.Tenant{}
		for _, objectRef := range newNs.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}
		}

		var labels, annotations map[string]string

		for key, value := range newNs.GetLabels() {
			if _, ok := oldNs.GetLabels()[key]; !ok {
				if labels == nil {
					labels = make(map[string]string)
				}

				labels[key] = value
			}
		}

		for key, value := range newNs.GetAnnotations() {
			if _, ok := oldNs.GetAnnotations()[key]; !ok {
				if annotations == nil {
					annotations = make(map[string]string)
				}

				annotations[key] = value
			}
		}

		return r.validateUserMetadata(tnt, recorder, labels, annotations)
	}
}

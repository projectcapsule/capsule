// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	caputils "github.com/projectcapsule/capsule/pkg/utils"
)

type userMetadataHandler struct {
	configuration configuration.Configuration
	version       *version.Version
}

func UserMetadataHandler(configuration configuration.Configuration, ver *version.Version) handlers.Handler {
	return &userMetadataHandler{
		configuration: configuration,
		version:       ver,
	}
}

func (r *userMetadataHandler) OnCreate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		nodeWebhookSupported, _ := caputils.NodeWebhookSupported(r.version)

		if !nodeWebhookSupported {
			return nil
		}

		oldNode := &corev1.Node{}
		if err := decoder.DecodeRaw(req.OldObject, oldNode); err != nil {
			return ad.ErroredResponse(err)
		}

		newNode := &corev1.Node{}
		if err := decoder.Decode(req, newNode); err != nil {
			return ad.ErroredResponse(err)
		}

		if r.configuration.ForbiddenUserNodeLabels() != nil {
			oldNodeForbiddenLabels := r.getForbiddenNodeLabels(oldNode)
			newNodeForbiddenLabels := r.getForbiddenNodeLabels(newNode)

			if !reflect.DeepEqual(oldNodeForbiddenLabels, newNodeForbiddenLabels) {
				recorder.LabeledEvent(
					newNode,
					corev1.EventTypeWarning,
					events.ReasonForbiddenLabel,
					events.ActionValidationDenied,
					"denied modifying forbidden labels on node",
				).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny(caperrors.NewNodeLabelForbiddenError(r.configuration.ForbiddenUserNodeLabels()).Error())
			}
		}

		if r.configuration.ForbiddenUserNodeAnnotations() != nil {
			oldNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(oldNode)
			newNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(newNode)

			if !reflect.DeepEqual(oldNodeForbiddenAnnotations, newNodeForbiddenAnnotations) {
				recorder.LabeledEvent(
					newNode,
					corev1.EventTypeWarning,
					events.ReasonForbiddenAnnotation,
					events.ActionValidationDenied,
					"denied modifying forbidden annotations on node",
				).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny(caperrors.NewNodeAnnotationForbiddenError(r.configuration.ForbiddenUserNodeAnnotations()).Error())
			}
		}

		return nil
	}
}

func (r *userMetadataHandler) getForbiddenNodeLabels(node *corev1.Node) map[string]string {
	forbiddenNodeLabels := make(map[string]string)

	forbiddenLabels := r.configuration.ForbiddenUserNodeLabels()

	for label, value := range node.GetLabels() {
		var forbidden, matched bool

		forbidden = forbiddenLabels.ExactMatch(label)
		matched = forbiddenLabels.RegexMatch(label)

		if forbidden || matched {
			forbiddenNodeLabels[label] = value
		}
	}

	return forbiddenNodeLabels
}

func (r *userMetadataHandler) getForbiddenNodeAnnotations(node *corev1.Node) map[string]string {
	forbiddenNodeAnnotations := make(map[string]string)

	forbiddenAnnotations := r.configuration.ForbiddenUserNodeAnnotations()

	for annotation, value := range node.GetAnnotations() {
		var forbidden, matched bool

		forbidden = forbiddenAnnotations.ExactMatch(annotation)
		matched = forbiddenAnnotations.RegexMatch(annotation)

		if forbidden || matched {
			forbiddenNodeAnnotations[annotation] = value
		}
	}

	return forbiddenNodeAnnotations
}

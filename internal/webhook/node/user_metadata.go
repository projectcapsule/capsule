// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

type userMetadataHandler struct {
	configuration configuration.Configuration
	version       *version.Version
}

func UserMetadataHandler(configuration configuration.Configuration, ver *version.Version) capsulewebhook.Handler {
	return &userMetadataHandler{
		configuration: configuration,
		version:       ver,
	}
}

func (r *userMetadataHandler) OnCreate(client.Client, admission.Decoder, events.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnDelete(client.Client, admission.Decoder, events.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnUpdate(_ client.Client, decoder admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		nodeWebhookSupported, _ := utils.NodeWebhookSupported(r.version)

		if !nodeWebhookSupported {
			return nil
		}

		oldNode := &corev1.Node{}
		if err := decoder.DecodeRaw(req.OldObject, oldNode); err != nil {
			return utils.ErroredResponse(err)
		}

		newNode := &corev1.Node{}
		if err := decoder.Decode(req, newNode); err != nil {
			return utils.ErroredResponse(err)
		}

		if r.configuration.ForbiddenUserNodeLabels() != nil {
			oldNodeForbiddenLabels := r.getForbiddenNodeLabels(oldNode)
			newNodeForbiddenLabels := r.getForbiddenNodeLabels(newNode)

			if !reflect.DeepEqual(oldNodeForbiddenLabels, newNodeForbiddenLabels) {
				recorder.Eventf(newNode, oldNode, corev1.EventTypeWarning, evt.ReasonForbiddenLabel, evt.ActionValidationDenied, "Denied modifying forbidden labels on node")

				response := admission.Denied(NewNodeLabelForbiddenError(r.configuration.ForbiddenUserNodeLabels()).Error())

				return &response
			}
		}

		if r.configuration.ForbiddenUserNodeAnnotations() != nil {
			oldNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(oldNode)
			newNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(newNode)

			if !reflect.DeepEqual(oldNodeForbiddenAnnotations, newNodeForbiddenAnnotations) {
				recorder.Eventf(newNode, oldNode, corev1.EventTypeWarning, evt.ReasonForbiddenLabel, evt.ActionValidationDenied, "Denied modifying forbidden annotations on node")

				response := admission.Denied(NewNodeAnnotationForbiddenError(r.configuration.ForbiddenUserNodeAnnotations()).Error())

				return &response
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

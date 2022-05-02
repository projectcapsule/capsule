// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
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

func (r *userMetadataHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *userMetadataHandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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

func (r *userMetadataHandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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
				recorder.Eventf(newNode, corev1.EventTypeWarning, "ForbiddenNodeLabel", "Denied modifying forbidden labels on node")

				response := admission.Denied(NewNodeLabelForbiddenError(r.configuration.ForbiddenUserNodeLabels()).Error())

				return &response
			}
		}

		if r.configuration.ForbiddenUserNodeAnnotations() != nil {
			oldNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(oldNode)
			newNodeForbiddenAnnotations := r.getForbiddenNodeAnnotations(newNode)

			if !reflect.DeepEqual(oldNodeForbiddenAnnotations, newNodeForbiddenAnnotations) {
				recorder.Eventf(newNode, corev1.EventTypeWarning, "ForbiddenNodeLabel", "Denied modifying forbidden annotations on node")

				response := admission.Denied(NewNodeAnnotationForbiddenError(r.configuration.ForbiddenUserNodeAnnotations()).Error())

				return &response
			}
		}

		return nil
	}
}

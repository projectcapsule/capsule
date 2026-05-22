// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type handler struct {
	cfg     configuration.Configuration
	version *version.Version
}

func Handler(cfg configuration.Configuration, version *version.Version) handlers.Handler {
	return &handler{
		cfg:     cfg,
		version: version,
	}
}

func (h *handler) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, c, decoder)
	}
}

func (h *handler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, c, decoder)
	}
}

func (h *handler) mutate(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder) *admission.Response {
	var response *admission.Response

	switch req.Resource {
	case metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}:
		response = mutatePodDefaults(ctx, req, c, decoder, req.Namespace)
	case metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}:
		response = mutatePVCDefaults(ctx, req, c, decoder, req.Namespace)
	case metav1.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, metav1.GroupVersionResource{Group: "networking.k8s.io", Version: "v1beta1", Resource: "ingresses"}:
		response = mutateIngressDefaults(ctx, req, h.version, c, decoder, req.Namespace)
	case metav1.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}:
		response = mutateGatewayDefaults(ctx, req, c, decoder, req.Namespace)
	}

	if response == nil {
		skip := admission.Allowed("Skipping Mutation")

		response = &skip
	}

	return response
}

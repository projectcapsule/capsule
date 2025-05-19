// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type handler struct {
	cfg     configuration.Configuration
	version *version.Version
}

func Handler(cfg configuration.Configuration, version *version.Version) capsulewebhook.Handler {
	return &handler{
		cfg:     cfg,
		version: version,
	}
}

func (h *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *handler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *handler) mutate(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	var response *admission.Response

	switch req.Resource {
	case metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}:
		response = mutatePodDefaults(ctx, req, c, decoder, recorder, req.Namespace)
	case metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}:
		response = mutatePVCDefaults(ctx, req, c, decoder, recorder, req.Namespace)
	case metav1.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, metav1.GroupVersionResource{Group: "networking.k8s.io", Version: "v1beta1", Resource: "ingresses"}:
		response = mutateIngressDefaults(ctx, req, h.version, c, decoder, recorder, req.Namespace)
	}

	if response == nil {
		skip := admission.Allowed("Skipping Mutation")

		response = &skip
	}

	return response
}

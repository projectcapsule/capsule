// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func Register(manager controllerruntime.Manager, webhookList ...Webhook) error {
	recorder := manager.GetEventRecorderFor("tenant-webhook")

	server := manager.GetWebhookServer()

	for _, wh := range webhookList {
		server.Register(wh.GetPath(), &webhook.Admission{
			Handler: &handlerRouter{
				client:   manager.GetClient(),
				decoder:  admission.NewDecoder(manager.GetScheme()),
				recorder: recorder,
				handlers: wh.GetHandlers(),
			},
		})
	}

	return nil
}

type handlerRouter struct {
	client   client.Client
	decoder  *admission.Decoder
	recorder record.EventRecorder

	handlers []Handler
}

func (r *handlerRouter) Handle(ctx context.Context, req admission.Request) admission.Response {
	switch req.Operation {
	case admissionv1.Create:
		for _, h := range r.handlers {
			if response := h.OnCreate(r.client, r.decoder, r.recorder)(ctx, req); response != nil {
				return *response
			}
		}
	case admissionv1.Update:
		for _, h := range r.handlers {
			if response := h.OnUpdate(r.client, r.decoder, r.recorder)(ctx, req); response != nil {
				return *response
			}
		}
	case admissionv1.Delete:
		for _, h := range r.handlers {
			if response := h.OnDelete(r.client, r.decoder, r.recorder)(ctx, req); response != nil {
				return *response
			}
		}
	case admissionv1.Connect:
		return admission.Allowed("")
	}

	return admission.Allowed("")
}

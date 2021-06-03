// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"io/ioutil"

	admissionv1 "k8s.io/api/admission/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func Register(manager controllerruntime.Manager, webhookList ...Webhook) error {
	// skipping webhook setup if certificate is missing
	certData, _ := ioutil.ReadFile("/tmp/k8s-webhook-server/serving-certs/tls.crt")
	if len(certData) == 0 {
		return nil
	}

	server := manager.GetWebhookServer()
	for _, wh := range webhookList {
		server.Register(wh.GetPath(), &webhook.Admission{
			Handler: &handlerRouter{
				handler: wh.GetHandler(),
			},
		})
	}
	return nil
}

type handlerRouter struct {
	handler Handler
	client  client.Client
	decoder *admission.Decoder
}

func (r *handlerRouter) Handle(ctx context.Context, req admission.Request) admission.Response {
	switch req.Operation {
	case admissionv1.Create:
		return r.handler.OnCreate(r.client, r.decoder)(ctx, req)
	case admissionv1.Update:
		return r.handler.OnUpdate(r.client, r.decoder)(ctx, req)
	case admissionv1.Delete:
		return r.handler.OnDelete(r.client, r.decoder)(ctx, req)
	default:
		return admission.Allowed("")
	}
}

func (r *handlerRouter) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

func (r *handlerRouter) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}

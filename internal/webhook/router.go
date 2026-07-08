// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	admissionv1 "k8s.io/api/admission/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type RegistrationOptions struct {
	EnableTracing bool
}

func Register(manager controllerruntime.Manager, recorder events.EventRecorder, options RegistrationOptions, webhookList ...handlers.Webhook) error {
	server := manager.GetWebhookServer()

	for _, wh := range webhookList {
		handler := http.Handler(&webhook.Admission{
			Handler: &handlerRouter{
				client:   manager.GetClient(),
				reader:   manager.GetAPIReader(),
				decoder:  admission.NewDecoder(manager.GetScheme()),
				recorder: recorder,
				handlers: wh.GetHandlers(),
				path:     wh.GetPath(),
			},
		})

		if options.EnableTracing {
			handler = otelhttp.NewHandler(
				handler,
				"capsule.admission",
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return "capsule.admission " + r.URL.Path
				}),
			)
		}

		server.Register(wh.GetPath(), handler)
	}

	return nil
}

type handlerRouter struct {
	client   client.Client
	reader   client.Reader
	decoder  admission.Decoder
	recorder events.EventRecorder

	handlers []handlers.Handler
	path     string
}

func (r *handlerRouter) Handle(ctx context.Context, req admission.Request) admission.Response {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("admission.uid", string(req.UID)),
		attribute.String("admission.operation", string(req.Operation)),
		attribute.String("admission.kind.group", req.Kind.Group),
		attribute.String("admission.kind.version", req.Kind.Version),
		attribute.String("admission.kind.kind", req.Kind.Kind),
		attribute.String("admission.resource.group", req.Resource.Group),
		attribute.String("admission.resource.version", req.Resource.Version),
		attribute.String("admission.resource.resource", req.Resource.Resource),
		attribute.String("admission.subresource", req.SubResource),
		attribute.String("admission.namespace", req.Namespace),
		attribute.String("admission.name", req.Name),
		attribute.String("admission.user", req.UserInfo.Username),
		attribute.String("admission.webhook.path", r.path),
	)

	switch req.Operation {
	case admissionv1.Create:
		for _, h := range r.handlers {
			if response := h.OnCreate(r.client, r.reader, r.decoder, r.recorder)(ctx, req); response != nil {
				return r.recordResponse(span, *response)
			}
		}
	case admissionv1.Update:
		for _, h := range r.handlers {
			if response := h.OnUpdate(r.client, r.reader, r.decoder, r.recorder)(ctx, req); response != nil {
				return r.recordResponse(span, *response)
			}
		}
	case admissionv1.Delete:
		for _, h := range r.handlers {
			if response := h.OnDelete(r.client, r.reader, r.decoder, r.recorder)(ctx, req); response != nil {
				return r.recordResponse(span, *response)
			}
		}
	case admissionv1.Connect:
		return r.recordResponse(span, admission.Allowed(""))
	}

	return r.recordResponse(span, admission.Allowed(""))
}

func (r *handlerRouter) recordResponse(span trace.Span, response admission.Response) admission.Response {
	span.SetAttributes(attribute.Bool("admission.allowed", response.Allowed))

	if response.Result != nil {
		span.SetAttributes(
			attribute.Int64("admission.response.code", int64(response.Result.Code)),
			attribute.String("admission.response.reason", string(response.Result.Reason)),
		)
	}

	return response
}

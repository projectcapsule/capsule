/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"io/ioutil"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func Register(mgr controllerruntime.Manager, webhookList ...Webhook) error {
	// skipping webhook setup if certificate is missing
	dat, _ := ioutil.ReadFile("/tmp/k8s-webhook-server/serving-certs/tls.crt")
	if len(dat) == 0 {
		return nil
	}

	s := mgr.GetWebhookServer()
	for _, wh := range webhookList {
		s.Register(wh.GetPath(), &webhook.Admission{
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
	case admissionv1beta1.Create:
		return r.handler.OnCreate(r.client, r.decoder)(ctx, req)
	case admissionv1beta1.Update:
		return r.handler.OnUpdate(r.client, r.decoder)(ctx, req)
	case admissionv1beta1.Delete:
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

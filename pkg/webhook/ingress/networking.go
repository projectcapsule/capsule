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

package ingress

import (
	"context"
	"net/http"

	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-networking-ingress,mutating=false,failurePolicy=fail,groups=networking.k8s.io,resources=ingresses,verbs=create;update,versions=v1beta1,name=networking.ingress.capsule.clastix.io

type webhook struct{
	handler capsulewebhook.Handler
}

func NetworkingWebhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "NetworkIngress"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-networking-ingress"
}

type handler struct {
	fn func(object metav1.Object, ingressClass *string) capsulewebhook.Handler
}

func NetworkingHandler(fn func(object metav1.Object, ingressClass *string) capsulewebhook.Handler) capsulewebhook.Handler {
	return &handler{
		fn: fn,
	}
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i := &networkingv1beta1.Ingress{}
		if err := decoder.Decode(req, i); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.fn(i, i.Spec.IngressClassName).OnCreate(client, decoder)(ctx, req)
	}
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i := &networkingv1beta1.Ingress{}
		if err := decoder.Decode(req, i); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return r.fn(i, i.Spec.IngressClassName).OnUpdate(client, decoder)(ctx, req)
	}
}

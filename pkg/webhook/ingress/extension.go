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

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-extensions-ingress,mutating=false,failurePolicy=fail,groups=extensions,resources=ingresses,verbs=create;update,versions=v1beta1,name=extensions.ingress.capsule.clastix.io

type extensionWebhook struct {
	handler capsulewebhook.Handler
}

func ExtensionWebhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &extensionWebhook{handler: handler}
}

func (e *extensionWebhook) GetHandler() capsulewebhook.Handler {
	return e.handler
}

func (e *extensionWebhook) GetName() string {
	return "ExtensionIngress"
}

func (e *extensionWebhook) GetPath() string {
	return "/validating-v1-extensions-ingress"
}

type extensionIngressHandler struct {
	fn func(object metav1.Object, ingressClass *string) capsulewebhook.Handler
}

func ExtensionHandler(fn func(object metav1.Object, ingressClass *string) capsulewebhook.Handler) capsulewebhook.Handler {
	return &extensionIngressHandler{
		fn: fn,
	}
}

func (r *extensionIngressHandler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i := &extensionsv1beta1.Ingress{}
		if err := decoder.Decode(req, i); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.fn(i, i.Spec.IngressClassName).OnCreate(client, decoder)(ctx, req)
	}
}

func (r *extensionIngressHandler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *extensionIngressHandler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i := &extensionsv1beta1.Ingress{}
		if err := decoder.Decode(req, i); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.fn(i, i.Spec.IngressClassName).OnUpdate(client, decoder)(ctx, req)
	}
}

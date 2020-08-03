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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-extensions-ingress,mutating=false,failurePolicy=fail,groups=extensions,resources=ingresses,verbs=create;update,versions=v1beta1,name=extensions.ingress.capsule.clastix.io

type ExtensionIngress struct{}

func (r *ExtensionIngress) GetHandler() webhook.Handler {
	return &extensionIngressHandler{}
}

func (r *ExtensionIngress) GetName() string {
	return "ExtensionIngress"
}

func (r *ExtensionIngress) GetPath() string {
	return "/validating-v1-extensions-ingress"
}

type extensionIngressHandler struct {
}

func (r *extensionIngressHandler) OnCreate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	i := &extensionsv1beta1.Ingress{}
	if err := decoder.Decode(req, i); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return handleIngress(ctx, i, i.Spec.IngressClassName, client)
}

func (r *extensionIngressHandler) OnDelete(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

func (r *extensionIngressHandler) OnUpdate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	i := &extensionsv1beta1.Ingress{}
	if err := decoder.Decode(req, i); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return handleIngress(ctx, i, i.Spec.IngressClassName, client)
}

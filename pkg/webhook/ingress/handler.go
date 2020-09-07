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
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

type HandlerIngress struct {
	object       metav1.Object
	ingressClass *string
}

func NewHandlerIngress(object metav1.Object, ingressClass *string) capsulewebhook.Handler {
	return &HandlerIngress{object: object, ingressClass: ingressClass}
}

func (h HandlerIngress) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return h.handleIngress(ctx, h.object, h.ingressClass, client)
	}
}

func (h HandlerIngress) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h HandlerIngress) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return h.handleIngress(ctx, h.object, h.ingressClass, client)
	}
}

func (h HandlerIngress) handleIngress(ctx context.Context, object metav1.Object, ic *string, c client.Client) admission.Response {
	if ic == nil {
		if v, ok := object.GetAnnotations()["kubernetes.io/ingress.class"]; ok {
			ic = &v
		}
	}

	if ic == nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("A valid Ingress Class must be used"))
	}

	tl := &v1alpha1.TenantList{}
	if err := c.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", object.GetNamespace()),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !tl.Items[0].Spec.IngressClasses.IsStringInList(*ic) {
		return admission.Errored(http.StatusBadRequest, NewIngressClassForbidden(*ic))
	}

	return admission.Allowed("")
}

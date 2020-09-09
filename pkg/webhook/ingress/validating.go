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

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-ingress,mutating=false,failurePolicy=fail,groups=networking.k8s.io;extensions,resources=ingresses,verbs=create;update,versions=v1beta1,name=ingress.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "NetworkIngress"
}

func (w *webhook) GetPath() string {
	return "/validating-ingress"
}

type handler struct {}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i, err := r.ingressFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.validateIngress(ctx, client, i)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		i, err := r.ingressFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.validateIngress(ctx, client, i)
	}
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) ingressFromRequest(req admission.Request, decoder *admission.Decoder) (ingress Ingress, err error) {
	switch req.Kind.Group {
	case "networking":
		n := &networkingv1beta1.Ingress{}
		if err := decoder.Decode(req, n); err != nil {
			return nil, err
		}
		ingress = Networking{n}
	case "extensions":
		e := &extensionsv1beta1.Ingress{}
		if err := decoder.Decode(req, e); err != nil {
			return nil, err
		}
		ingress = Extension{e}
	default:
		err = fmt.Errorf("cannot recognize type %s", req.Kind.Group)
	}
	return
}

func (r *handler) validateIngress(ctx context.Context, c client.Client, object Ingress) admission.Response {
	ingressClass := object.IngressClass()
	if ingressClass == nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("A valid Ingress Class must be used"))
	}

	tl := &v1alpha1.TenantList{}
	if err := c.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", object.Namespace()),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !tl.Items[0].Spec.IngressClasses.IsStringInList(*ingressClass) {
		return admission.Errored(http.StatusBadRequest, NewIngressClassForbidden(*ingressClass))
	}

	return admission.Allowed("")
}

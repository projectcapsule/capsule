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
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/fields"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/go-logr/logr"
)

// +kubebuilder:webhook:path=/validating-ingress,mutating=false,failurePolicy=fail,groups=networking.k8s.io;extensions,resources=ingresses,verbs=create;update,versions=v1beta1,name=ingress-v1beta1.capsule.clastix.io
// +kubebuilder:webhook:path=/validating-ingress,mutating=false,failurePolicy=fail,groups=networking.k8s.io,resources=ingresses,verbs=create;update,versions=v1,name=ingress-v1.capsule.clastix.io

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

type handler struct {
	Log logr.Logger
}

func Handler() capsulewebhook.Handler {
	return &handler{Log: ctrl.Log.WithName("controllers").WithName("Tenant")}
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {

	return func(ctx context.Context, req admission.Request) admission.Response {
		ingress, err := r.ingressFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.validateIngress(ctx, client, ingress)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {

	return func(ctx context.Context, req admission.Request) admission.Response {
		ingress, err := r.ingressFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		return r.validateIngress(ctx, client, ingress)
	}
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {

	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) ingressFromRequest(req admission.Request, decoder *admission.Decoder) (ingress Ingress, err error) {

	switch req.Kind.Group {
	case "networking.k8s.io":
		if req.Kind.Version == "v1" {
			n := &networkingv1.Ingress{}
			if err := decoder.Decode(req, n); err != nil {
				return nil, err
			}
			ingress = NetworkingV1{Ingress: n}
			break
		}
		n := &networkingv1beta1.Ingress{}
		if err := decoder.Decode(req, n); err != nil {
			return nil, err
		}
		ingress = NetworkingV1Beta1{Ingress: n}
	case "extensions":
		e := &extensionsv1beta1.Ingress{}
		if err := decoder.Decode(req, e); err != nil {
			return nil, err
		}
		ingress = Extension{Ingress: e}
	default:
		err = fmt.Errorf("cannot recognize type %s", req.Kind.Group)
	}
	return
}

func (r *handler) validateIngress(ctx context.Context, apiClient client.Client, ingress Ingress) admission.Response {
	var valid, matched bool

	tl := &v1alpha1.TenantList{}
	if err := apiClient.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", ingress.Namespace()),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if len(tl.Items) == 0 {
		return admission.Allowed("")
	}

	tnt := tl.Items[0]

	if tnt.Spec.IngressClasses == nil {
		return admission.Allowed("")
	}

	ingressClass := ingress.IngressClass()
	if ingressClass == nil {
		return admission.Errored(http.StatusBadRequest, NewIngressClassNotValid(*tnt.Spec.IngressClasses))
	}

	if len(tnt.Spec.IngressClasses.Allowed) > 0 {
		valid = tnt.Spec.IngressClasses.Allowed.IsStringInList(*ingressClass)
	}

	if len(tnt.Spec.IngressClasses.AllowedRegex) > 0 {
		matched, _ = regexp.MatchString(tnt.Spec.IngressClasses.AllowedRegex, *ingressClass)
	}

	if !valid && !matched {
		return admission.Errored(http.StatusBadRequest, NewIngressClassForbidden(*ingressClass, *tnt.Spec.IngressClasses))
	}

	//TODO extract logic below into a method
	if tnt.Spec.IngressHostnames == nil {
		return admission.Allowed("")
	}

	valid = false
	matched = false
	hostnames := ingress.Hostnames()
	if len(hostnames) > 0 {
		valid = tnt.Spec.IngressHostnames.Allowed.AreStringsInList(hostnames)
	}

	allowedRegex := tnt.Spec.IngressHostnames.AllowedRegex
	if len(allowedRegex) > 0 {
		matched = allowedRegex.MatchesAllStrings(hostnames)
	}

	if !valid && !matched {
		return admission.Errored(http.StatusBadRequest, NewIngressHostnamesNotValid(hostnames, *tnt.Spec.IngressHostnames))
	}

	//TODO extract logic above into a method
	//r.verifyHostnames(hostnames)

	return admission.Allowed("")

}

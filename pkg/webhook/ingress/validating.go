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

	"github.com/pkg/errors"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
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
	allowHostnamesCollision bool
}

func Handler(allowIngressHostnamesCollision bool) capsulewebhook.Handler {
	return &handler{allowHostnamesCollision: allowIngressHostnamesCollision}
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
			if err = decoder.Decode(req, n); err != nil {
				return
			}
			ingress = NetworkingV1{Ingress: n}
			break
		}
		n := &networkingv1beta1.Ingress{}
		if err = decoder.Decode(req, n); err != nil {
			return
		}
		ingress = NetworkingV1Beta1{Ingress: n}
	case "extensions":
		e := &extensionsv1beta1.Ingress{}
		if err = decoder.Decode(req, e); err != nil {
			return
		}
		ingress = Extension{Ingress: e}
	default:
		err = fmt.Errorf("cannot recognize type %s", req.Kind.Group)
	}
	return
}

func (r *handler) validateClass(tenant v1alpha1.Tenant, ingressClass *string) error {
	if tenant.Spec.IngressClasses == nil {
		return nil
	}

	if ingressClass == nil {
		return NewIngressClassNotValid(*tenant.Spec.IngressClasses)
	}

	var valid, matched bool

	if len(tenant.Spec.IngressClasses.Exact) > 0 {
		valid = tenant.Spec.IngressClasses.ExactMatch(*ingressClass)
	}
	matched = tenant.Spec.IngressClasses.RegexMatch(*ingressClass)

	if !valid && !matched {
		return NewIngressClassForbidden(*ingressClass, *tenant.Spec.IngressClasses)
	}

	return nil
}

func (r *handler) validateHostnames(tenant v1alpha1.Tenant, hostnames []string) error {
	if tenant.Spec.IngressHostnames == nil {
		return nil
	}

	var valid, matched bool

	var invalidHostnames []string
	if len(hostnames) > 0 {
		for _, currentHostname := range hostnames {
			isPresent := v1alpha1.IngressHostnamesList(tenant.Spec.IngressHostnames.Exact).IsStringInList(currentHostname)
			if !isPresent {
				invalidHostnames = append(invalidHostnames, currentHostname)
			}
		}
		if len(invalidHostnames) == 0 {
			valid = true
		}
	}

	var notMatchingHostnames []string
	allowedRegex := tenant.Spec.IngressHostnames.Regex
	if len(allowedRegex) > 0 {
		for _, currentHostname := range hostnames {
			matched, _ = regexp.MatchString(tenant.Spec.IngressHostnames.Regex, currentHostname)
			if !matched {
				notMatchingHostnames = append(notMatchingHostnames, currentHostname)
			}
		}
		if len(notMatchingHostnames) == 0 {
			matched = true
		}
	}

	if !valid && !matched {
		return NewIngressHostnamesNotValid(invalidHostnames, notMatchingHostnames, *tenant.Spec.IngressHostnames)
	}

	return nil
}

func (r *handler) validateIngress(ctx context.Context, c client.Client, ingress Ingress) admission.Response {
	tl := &v1alpha1.TenantList{}
	if err := c.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", ingress.Namespace()),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if len(tl.Items) == 0 {
		return admission.Allowed("")
	}
	tnt := tl.Items[0]

	if err := r.validateClass(tnt, ingress.IngressClass()); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := r.validateHostnames(tnt, ingress.Hostnames()); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := r.validateCollision(ctx, c, ingress); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed("")
}

func (r *handler) validateCollision(ctx context.Context, clt client.Client, ingress Ingress) error {
	if r.allowHostnamesCollision {
		return nil
	}
	for _, hostname := range ingress.Hostnames() {
		collisionErr := NewIngressHostnameCollision(hostname)

		var err error
		switch ingress.(type) {
		case Extension:
			el := &extensionsv1beta1.IngressList{}
			if err = clt.List(ctx, el, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			}); err != nil {
				return err
			}
			switch len(el.Items) {
			case 0:
				break
			case 1:
				if f := el.Items[0]; f.GetName() == ingress.Name() && f.GetNamespace() == ingress.Namespace() {
					break
				}
				fallthrough
			default:
				return collisionErr
			}
		case NetworkingV1:
			nl := &networkingv1.IngressList{}
			err = clt.List(ctx, nl, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			})
			if err != nil {
				return errors.Wrap(err, "cannot list *networkingv1.IngressList by MatchingFieldsSelector")
			}
			switch len(nl.Items) {
			case 0:
				break
			case 1:
				if f := nl.Items[0]; f.GetName() == ingress.Name() && f.GetNamespace() == ingress.Namespace() {
					break
				}
				fallthrough
			default:
				return collisionErr
			}
		case NetworkingV1Beta1:
			nlb := &networkingv1beta1.IngressList{}
			err = clt.List(ctx, nlb, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			})
			if err != nil {
				return errors.Wrap(err, "cannot list *networkingv1beta1.IngressList by MatchingFieldsSelector")
			}
			switch len(nlb.Items) {
			case 0:
				break
			case 1:
				if f := nlb.Items[0]; f.GetName() == ingress.Name() && f.GetNamespace() == ingress.Namespace() {
					break
				}
				fallthrough
			default:
				return collisionErr
			}
		}
	}
	return nil
}

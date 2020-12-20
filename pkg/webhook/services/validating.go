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

package services

import (
	"context"
	"net"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-external-service-ips,mutating=false,failurePolicy=fail,groups="",resources=services,verbs=create;update,versions=v1,name=validating-external-service-ips.capsule.clastix.io

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
	return "Service"
}

func (w *webhook) GetPath() string {
	return "/validating-external-service-ips"
}

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) handleService(ctx context.Context, clt client.Client, decoder *admission.Decoder, req admission.Request) admission.Response {
	s := &corev1.Service{}
	if err := decoder.Decode(req, s); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if s.Spec.ExternalIPs == nil {
		return admission.Allowed("")
	}

	tl := &v1alpha1.TenantList{}
	if err := clt.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", s.GetNamespace()),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if len(tl.Items) == 0 {
		return admission.Allowed("")
	}
	tnt := tl.Items[0]

	if tnt.Spec.ExternalServiceIPs == nil {
		return admission.Allowed("")
	}

	for _, allowed := range tnt.Spec.ExternalServiceIPs.Allowed {
		_, allowedIP, _ := net.ParseCIDR(string(allowed))
		for _, externalIP := range s.Spec.ExternalIPs {
			IP := net.ParseIP(externalIP)
			if allowedIP.Contains(IP) {
				return admission.Allowed("")
			}
		}
	}

	return admission.Errored(http.StatusBadRequest, NewExternalServiceIPForbidden(tnt.Spec.ExternalServiceIPs.Allowed))
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return r.handleService(ctx, client, decoder, req)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return r.handleService(ctx, client, decoder, req)
	}
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

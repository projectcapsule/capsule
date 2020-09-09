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

package service_labels

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-v1-service-labels,mutating=true,failurePolicy=ignore,groups="";discovery.k8s.io,resources=services;endpoints;endpointslices,verbs=create;update,versions=v1;v1beta1,name=service.labels.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w webhook) GetName() string {
	return "ServiceLabels"
}

func (w webhook) GetPath() string {
	return "/mutate-v1-service-labels"
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		svc, err := h.svcFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return h.syncLabels(ctx, client, svc)
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		svc, err := h.svcFromRequest(req, decoder)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return h.syncLabels(ctx, client, svc)
	}
}

func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) svcFromRequest(req admission.Request, decoder *admission.Decoder) (svc ServiceType, err error) {
	switch req.Kind.Kind {
	case "Service":
		service := &corev1.Service{}
		if err := decoder.Decode(req, service); err != nil {
			return nil, err
		}
		svc = Service{service}
	case "Endpoints":
		ep := &corev1.Endpoints{}
		if err := decoder.Decode(req, ep); err != nil {
			return nil, err
		}
		svc = Endpoints{ep}
	case "EndpointSlice":
		eps := &discoveryv1beta1.EndpointSlice{}
		if err := decoder.Decode(req, eps); err != nil {
			return nil, err
		}
		svc = EndpointSlice{eps}
	default:
		err = fmt.Errorf("cannot recognize type %s", req.Kind.Kind)
	}
	return
}

func (h *handler) syncLabels(ctx context.Context, client client.Client, object ServiceType) admission.Response {
	var patch []jsonpatch.JsonPatchOperation

	ns := &corev1.Namespace{}
	tenant := &v1alpha1.Tenant{}
	if err := client.Get(ctx, types.NamespacedName{Name: object.Namespace()}, ns); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	capsuleLabel, err := v1alpha1.GetTypeLabel(tenant)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	// not a tenant NS
	if _, ok := ns.Labels[capsuleLabel]; !ok {
		return admission.Allowed("")
	}
	if err := client.Get(ctx, types.NamespacedName{Name: ns.Labels[capsuleLabel]}, tenant); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if tenant.Spec.ServicesMetadata.AdditionalLabels == nil && tenant.Spec.ServicesMetadata.AdditionalAnnotations == nil {
		return admission.Allowed("")
	}

	availableLables := object.Labels()
	availableLAnnotations := object.Annotations()

	if al := tenant.Spec.ServicesMetadata.AdditionalLabels; al != nil {
		if availableLables == nil {
			patch = append(patch, jsonpatch.JsonPatchOperation{
				Operation: "add",
				Path:      "/metadata/labels",
				Value:     al,
			})
		} else {
			for key, value := range al {
				if availableLables[key] != value {
					patch = append(patch, jsonpatch.JsonPatchOperation{
						Operation: "replace",
						Path:      "/metadata/labels/" + strings.ReplaceAll(key, "/", "~1"), // http://jsonpatch.com/#json-pointer
						Value:     value,
					})
				}
			}
		}
	}

	if aa := tenant.Spec.ServicesMetadata.AdditionalAnnotations; aa != nil {
		if availableLAnnotations == nil {
			patch = append(patch, jsonpatch.JsonPatchOperation{
				Operation: "add",
				Path:      "/metadata/annotations",
				Value:     aa,
			})
		} else {
			for key, value := range aa {
				if availableLAnnotations[key] != value {
					patch = append(patch, jsonpatch.JsonPatchOperation{
						Operation: "replace",
						Path:      "/metadata/annotations/" + strings.ReplaceAll(key, "/", "~1"), // http://jsonpatch.com/#json-pointer
						Value:     value,
					})
				}
			}
		}
	}

	if len(patch) > 0 {
		return admission.Patched("Updating labels and annotations", patch...)
	}
	return admission.Allowed("")
}

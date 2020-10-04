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

package registry

import (
	"context"
	"net/http"
	"regexp"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-registry,mutating=false,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=pod.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetName() string {
	return "registry"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-registry"
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(c client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		var valid, matched bool
		pod := &v1.Pod{}

		if err := decoder.Decode(req, pod); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		containers := pod.Spec.Containers

		for _, container := range containers {
			if container.Image == "" {
				return admission.Errored(http.StatusBadRequest, NewRegistryClassNotValid())
			}
		}

		tl := &capsulev1alpha1.TenantList{}
		if err := c.List(ctx, tl, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pod.Namespace),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tl.Items[0].Spec.RegistryClasses.Allowed) > 0 {
			for _, container := range containers {
				valid = tl.Items[0].Spec.RegistryClasses.Allowed.IsStringInList(container.Image)
			}
		}

		if len(tl.Items[0].Spec.RegistryClasses.Allowed) > 0 {
			for _, container := range containers {
				matched, _ = regexp.MatchString(tl.Items[0].Spec.RegistryClasses.AllowedRegex, container.Image)
			}
		}

		if !valid && !matched {
			for _, container := range containers {
				return admission.Errored(http.StatusBadRequest, NewregistryClassForbidden(container.Image))
			}
		}
		return admission.Allowed("")

	}
}

func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

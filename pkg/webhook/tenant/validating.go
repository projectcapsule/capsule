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

package tenant

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-tenant,mutating=false,failurePolicy=fail,groups="capsule.clastix.io",resources=tenants,verbs=create,versions=v1alpha1,name=tenant.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w webhook) GetName() string {
	return "Tenant"
}

func (w webhook) GetPath() string {
	return "/validating-v1-tenant"
}

func (w webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(clt client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		tnt := &v1alpha1.Tenant{}
		if err := decoder.Decode(req, tnt); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		matched, _ := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, tnt.GetName())
		if !matched {
			return admission.Denied("Tenant name has forbidden characters")
		}

		// Validate ingressClasses regexp
		if tnt.Spec.IngressClasses != nil && len(tnt.Spec.IngressClasses.Regex) > 0 {
			if _, err := regexp.Compile(tnt.Spec.IngressClasses.Regex); err != nil {
				return admission.Denied("Unable to compile ingressClasses allowedRegex")
			}
		}

		// Validate storageClasses regexp
		if tnt.Spec.StorageClasses != nil && len(tnt.Spec.StorageClasses.Regex) > 0 {
			if _, err := regexp.Compile(tnt.Spec.StorageClasses.Regex); err != nil {
				return admission.Denied("Unable to compile storageClasses allowedRegex")
			}
		}

		// Validate containerRegistries regexp
		if tnt.Spec.ContainerRegistries != nil && len(tnt.Spec.ContainerRegistries.Regex) > 0 {
			if _, err := regexp.Compile(tnt.Spec.ContainerRegistries.Regex); err != nil {
				return admission.Denied("Unable to compile containerRegistries allowedRegex")
			}
		}

		// Validate ingressHostnames regexp
		if tnt.Spec.IngressHostnames != nil && len(tnt.Spec.IngressHostnames.Regex) > 0 {
			if _, err := regexp.Compile(tnt.Spec.IngressHostnames.Regex); err != nil {
				return admission.Denied("Unable to compile ingressHostnames allowedRegex")
			}
		}

		if tnt.Spec.IngressHostnames != nil && len(tnt.Spec.IngressHostnames.Exact) > 0 {
			for _, h := range tnt.Spec.IngressHostnames.Exact {
				tl := &v1alpha1.TenantList{}
				err := clt.List(ctx, tl, client.MatchingFieldsSelector{
					Selector: fields.OneTermEqualSelector(".spec.ingressHostnames", h),
				})
				if err != nil {
					return admission.Errored(http.StatusBadRequest, err)
				}
				if len(tl.Items) > 0 {
					return admission.Denied(fmt.Sprintf("The allowed hostname %s is already used by the Tenant %s, cannot proceed", h, tl.Items[0].GetName()))
				}
			}

			if _, err := regexp.Compile(tnt.Spec.IngressHostnames.Regex); err != nil {
				return admission.Denied("Unable to compile ingressHostnames allowedRegex")
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

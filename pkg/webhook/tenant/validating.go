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
	"regexp"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-tenant,mutating=false,failurePolicy=fail,groups="capsule.clastix.io",resources=tenants,verbs=create;update,versions=v1alpha1,name=tenant.capsule.clastix.io

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
	checkIngressHostnamesExact bool
}

func Handler(allowTenantIngressHostnamesCollision bool) capsulewebhook.Handler {
	return &handler{checkIngressHostnamesExact: !allowTenantIngressHostnamesCollision}
}

// Validate Tenant name
func (h *handler) validateTenantName(tenant *v1alpha1.Tenant) error {
	matched, _ := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, tenant.GetName())
	if !matched {
		return fmt.Errorf("tenant name has forbidden characters")
	}
	return nil
}

// Validate ingressClasses regexp
func (h *handler) validateIngressClassesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.IngressClasses != nil && len(tenant.Spec.IngressClasses.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressClasses.Regex); err != nil {
			return fmt.Errorf("unable to compile ingressClasses allowedRegex")
		}
	}
	return nil
}

// Validate storageClasses regexp
func (h *handler) validateStorageClassesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.StorageClasses != nil && len(tenant.Spec.StorageClasses.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.StorageClasses.Regex); err != nil {
			return fmt.Errorf("unable to compile storageClasses allowedRegex")
		}
	}
	return nil
}

// Validate containerRegistries regexp
func (h *handler) validateContainerRegistriesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.ContainerRegistries != nil && len(tenant.Spec.ContainerRegistries.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.ContainerRegistries.Regex); err != nil {
			return fmt.Errorf("unable to compile containerRegistries allowedRegex")
		}
	}
	return nil
}

// Validate containerRegistries regexp
func (h *handler) validateIngressHostnamesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.IngressHostnames != nil && len(tenant.Spec.IngressHostnames.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressHostnames.Regex); err != nil {
			return fmt.Errorf("unable to compile ingressHostnames allowedRegex")
		}
	}
	return nil
}

// Check Ingress hostnames collision across all available Tenants
func (h *handler) validateIngressHostnamesCollision(context context.Context, clt client.Client, tenant *v1alpha1.Tenant) error {
	if h.checkIngressHostnamesExact && tenant.Spec.IngressHostnames != nil && len(tenant.Spec.IngressHostnames.Exact) > 0 {
		for _, h := range tenant.Spec.IngressHostnames.Exact {
			tntList := &v1alpha1.TenantList{}
			if err := clt.List(context, tntList, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.ingressHostnames", h),
			}); err != nil {
				return fmt.Errorf("cannot retrieve Tenant list using .spec.ingressHostnames field selector: %w", err)
			}
			switch {
			case len(tntList.Items) == 1 && tntList.Items[0].GetName() == tenant.GetName():
				continue
			case len(tntList.Items) > 0:
				return fmt.Errorf("the allowed hostname %s is already used by the Tenant %s, cannot proceed", h, tntList.Items[0].GetName())
			default:
				continue
			}
		}
	}
	return nil
}

func (h *handler) validateTenant(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) error {
	tenant := &v1alpha1.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return err
	}
	if err := h.validateTenantByRegex(tenant); err != nil {
		return err
	}
	if err := h.validateIngressHostnamesCollision(ctx, client, tenant); err != nil {
		return err
	}
	return nil
}

func (h *handler) validateTenantByRegex(tenant *v1alpha1.Tenant) error {
	if err := h.validateTenantName(tenant); err != nil {
		return err
	}
	if err := h.validateIngressClassesRegex(tenant); err != nil {
		return err
	}
	if err := h.validateStorageClassesRegex(tenant); err != nil {
		return err
	}
	if err := h.validateContainerRegistriesRegex(tenant); err != nil {
		return err
	}
	if err := h.validateIngressHostnamesRegex(tenant); err != nil {
		return err
	}

	return nil
}

func (h *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if err := h.validateTenant(ctx, req, client, decoder); err != nil {
			return admission.Denied(err.Error())
		}
		return admission.Allowed("")
	}
}

func (h *handler) OnDelete(client.Client, *admission.Decoder) capsulewebhook.Func {
	return func(context.Context, admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if err := h.validateTenant(ctx, req, client, decoder); err != nil {
			return admission.Denied(err.Error())
		}
		return admission.Allowed("")
	}
}

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-tenant,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="capsule.clastix.io",resources=tenants,verbs=create;update,versions=v1alpha1,name=tenant.capsule.clastix.io

type validating struct {
	handler capsulewebhook.Handler
}

func Validating(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &validating{handler: handler}
}

func (w validating) GetName() string {
	return "Tenant"
}

func (w validating) GetPath() string {
	return "/validating-v1-tenant"
}

func (w validating) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type validatingHandler struct {
	configuration configuration.Configuration
}

func ValidatingHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &validatingHandler{configuration: configuration}
}

// Validate Tenant name
func (h *validatingHandler) validateTenantName(tenant *v1alpha1.Tenant) error {
	matched, _ := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, tenant.GetName())
	if !matched {
		return fmt.Errorf("tenant name has forbidden characters")
	}
	return nil
}

// Validate ingressClasses regexp
func (h *validatingHandler) validateIngressClassesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.IngressClasses != nil && len(tenant.Spec.IngressClasses.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressClasses.Regex); err != nil {
			return fmt.Errorf("unable to compile ingressClasses allowedRegex")
		}
	}
	return nil
}

// Validate storageClasses regexp
func (h *validatingHandler) validateStorageClassesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.StorageClasses != nil && len(tenant.Spec.StorageClasses.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.StorageClasses.Regex); err != nil {
			return fmt.Errorf("unable to compile storageClasses allowedRegex")
		}
	}
	return nil
}

// Validate containerRegistries regexp
func (h *validatingHandler) validateContainerRegistriesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.ContainerRegistries != nil && len(tenant.Spec.ContainerRegistries.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.ContainerRegistries.Regex); err != nil {
			return fmt.Errorf("unable to compile containerRegistries allowedRegex")
		}
	}
	return nil
}

// Validate containerRegistries regexp
func (h *validatingHandler) validateIngressHostnamesRegex(tenant *v1alpha1.Tenant) error {
	if tenant.Spec.IngressHostnames != nil && len(tenant.Spec.IngressHostnames.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressHostnames.Regex); err != nil {
			return fmt.Errorf("unable to compile ingressHostnames allowedRegex")
		}
	}
	return nil
}

// Check Ingress hostnames collision across all available Tenants
func (h *validatingHandler) validateIngressHostnamesCollision(context context.Context, clt client.Client, tenant *v1alpha1.Tenant) error {
	if !h.configuration.AllowTenantIngressHostnamesCollision() && tenant.Spec.IngressHostnames != nil && len(tenant.Spec.IngressHostnames.Exact) > 0 {
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

func (h *validatingHandler) validateTenant(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) error {
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

func (h *validatingHandler) validateTenantByRegex(tenant *v1alpha1.Tenant) error {
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

func (h *validatingHandler) OnCreate(client client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if err := h.validateTenant(ctx, req, client, decoder); err != nil {
			return admission.Denied(err.Error())
		}
		return admission.Allowed("")
	}
}

func (h *validatingHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *validatingHandler) OnUpdate(client client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if err := h.validateTenant(ctx, req, client, decoder); err != nil {
			return admission.Denied(err.Error())
		}
		return admission.Allowed("")
	}
}

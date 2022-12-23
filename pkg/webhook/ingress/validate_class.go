// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type class struct {
	configuration configuration.Configuration
	version       *version.Version
}

func Class(configuration configuration.Configuration) capsulewebhook.Handler {
	version, _ := utils.GetK8sVersion()

	return &class{
		configuration: configuration,
		version:       version,
	}
}

func (r *class) retrieveIngressClass(ctx context.Context, ctrlClient client.Client, ingressClassName *string) (client.Object, error) {
	if r.version == nil || ingressClassName == nil {
		return nil, nil
	}

	var obj client.Object

	switch {
	case r.version.Minor() < 18:
		return nil, nil
	case r.version.Minor() < 19:
		obj = &networkingv1beta1.IngressClass{}
	default:
		obj = &networkingv1.IngressClass{}
	}

	if err := ctrlClient.Get(ctx, types.NamespacedName{Name: *ingressClassName}, obj); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return obj, nil
}

// nolint:dupl
func (r *class) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta2.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		ic, err := r.retrieveIngressClass(ctx, client, ingress.IngressClass())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		if err = r.validateClass(*tenant, ingress.IngressClass(), ic); err == nil {
			return nil
		}

		var forbiddenErr *ingressClassForbiddenError

		if errors.As(err, &forbiddenErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassForbidden", "Ingress %s/%s class is forbidden", ingress.Namespace(), ingress.Name())
		}

		var invalidErr *ingressClassNotValidError

		if errors.As(err, &invalidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassNotValid", "Ingress %s/%s class is invalid", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

// nolint:dupl
func (r *class) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta2.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		ic, err := r.retrieveIngressClass(ctx, client, ingress.IngressClass())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		if err = r.validateClass(*tenant, ingress.IngressClass(), ic); err == nil {
			return nil
		}

		var forbiddenErr *ingressClassForbiddenError

		if errors.As(err, &forbiddenErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassForbidden", "Ingress %s/%s class is forbidden", ingress.Namespace(), ingress.Name())
		}

		var invalidErr *ingressClassNotValidError

		if errors.As(err, &invalidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassNotValid", "Ingress %s/%s class is invalid", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

func (r *class) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *class) validateClass(tenant capsulev1beta2.Tenant, ingressClass *string, ingressClassObj client.Object) error {
	if tenant.Spec.IngressOptions.AllowedClasses == nil {
		return nil
	}

	if ingressClass == nil {
		return NewIngressClassNotValid(*tenant.Spec.IngressOptions.AllowedClasses)
	}

	var valid, regex, match bool

	if len(tenant.Spec.IngressOptions.AllowedClasses.Exact) > 0 {
		valid = tenant.Spec.IngressOptions.AllowedClasses.ExactMatch(*ingressClass)
	}

	regex = tenant.Spec.IngressOptions.AllowedClasses.RegexMatch(*ingressClass)

	if ingressClassObj != nil {
		match = tenant.Spec.IngressOptions.AllowedClasses.SelectorMatch(ingressClassObj)
	} else {
		match = true
	}

	if !valid && !regex && !match {
		return NewIngressClassForbidden(*ingressClass, *tenant.Spec.IngressOptions.AllowedClasses)
	}

	return nil
}

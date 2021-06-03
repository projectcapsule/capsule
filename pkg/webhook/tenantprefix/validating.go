// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenantprefix

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-namespace-tenant-prefix,sideEffects=None,admissionReviewVersions=v1,mutating=false,failurePolicy=fail,groups="",resources=namespaces,verbs=create,versions=v1,name=prefix.namespace.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{
		handler: handler,
	}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "OwnerReference"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-namespace-tenant-prefix"
}

type handler struct {
	configuration configuration.Configuration
}

func Handler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &handler{
		configuration: configuration,
	}
}

func (r *handler) OnCreate(clt client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if exp, _ := r.configuration.ProtectedNamespaceRegexp(); exp != nil {
			if matched := exp.MatchString(ns.GetName()); matched {
				return admission.Denied("Creating namespaces with name matching " + exp.String() + " regexp is not allowed; please, reach out to the system administrators")
			}
		}

		if !r.configuration.ForceTenantPrefix() {
			return admission.Allowed("")
		}

		tnt := &v1alpha1.Tenant{}
		for _, or := range ns.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			if err := clt.Get(ctx, types.NamespacedName{Name: or.Name}, tnt); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			if e := fmt.Sprintf("%s-%s", tnt.GetName(), ns.GetName()); !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
				return admission.Denied("The namespace doesn't match the tenant prefix, expected " + e)
			}
		}
		return admission.Allowed("")
	}
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

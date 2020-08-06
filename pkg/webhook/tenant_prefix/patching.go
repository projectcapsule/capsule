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

package tenant_prefix

import (
	"context"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-namespace-tenant-prefix,mutating=false,failurePolicy=fail,groups="",resources=namespaces,verbs=create,versions=v1,name=prefix.namespace.capsule.clastix.io

type Webhook struct {
	ForceTenantPrefix bool
}

func (o Webhook) GetHandler() webhook.Handler {
	return &handler{forceTenantPrefix: o.ForceTenantPrefix}
}

func (o Webhook) GetName() string {
	return "OwnerReference"
}

func (o Webhook) GetPath() string {
	return "/validating-v1-namespace-tenant-prefix"
}

type handler struct {
	forceTenantPrefix bool
}

func (r *handler) OnCreate(ctx context.Context, req admission.Request, clt client.Client, decoder *admission.Decoder) admission.Response {
	if !r.forceTenantPrefix {
		return admission.Allowed("")
	}
	ns := &corev1.Namespace{}
	if err := decoder.Decode(req, ns); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	t := &v1alpha1.Tenant{}
	for _, or := range ns.ObjectMeta.OwnerReferences {
		// retrieving the selected Tenant
		if err := clt.Get(ctx, types.NamespacedName{Name: or.Name}, t); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	if e := t.GetName() + "-" + ns.GetName(); !strings.HasPrefix(ns.GetName(), t.GetName()+"-") {
		return admission.Denied("The namespace doesn't match the tenant prefix, expected " + e)
	}
	return admission.Allowed("")
}

func (r *handler) OnDelete(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

func (r *handler) OnUpdate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

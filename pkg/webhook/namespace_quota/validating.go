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

package namespace_quota

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validate-v1-namespace-quota,mutating=false,failurePolicy=fail,groups="",resources=namespaces,verbs=create,versions=v1,name=quota.namespace.capsule.clastix.io

type Webhook struct{}

func (r *Webhook) GetHandler() webhook.Handler {
	return &handler{}
}

func (r *Webhook) GetName() string {
	return "NamespaceQuota"
}

func (r *Webhook) GetPath() string {
	return "/validate-v1-namespace-quota"
}

type handler struct {
}

func (r *handler) OnCreate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	ns := &corev1.Namespace{}
	if err := decoder.Decode(req, ns); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	for _, or := range ns.ObjectMeta.OwnerReferences {
		// retrieving the selected Tenant
		t := &v1alpha1.Tenant{}
		if err := client.Get(ctx, types.NamespacedName{Name: or.Name}, t); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if t.IsFull() {
			return admission.Denied(NewNamespaceQuotaExceededError().Error())
		}
	}
	// creating NS that is not bounded to any Tenant
	return admission.Allowed("")
}

func (r *handler) OnDelete(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

func (r *handler) OnUpdate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

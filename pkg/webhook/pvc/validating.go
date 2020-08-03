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

package pvc

import (
	"context"
	"net/http"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-pvc,mutating=false,failurePolicy=fail,groups="",resources=persistentvolumeclaims,verbs=create,versions=v1,name=pvc.capsule.clastix.io

type Webhook struct{}

func (p Webhook) GetName() string {
	return "Pvc"
}

func (p Webhook) GetPath() string {
	return "/validating-v1-pvc"
}

func (p Webhook) GetHandler() webhook.Handler {
	return &handler{}
}

type handler struct {
}

func (r *handler) OnCreate(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder) admission.Response {
	pvc := &v1.PersistentVolumeClaim{}

	if err := decoder.Decode(req, pvc); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pvc.Spec.StorageClassName == nil {
		return admission.Errored(http.StatusBadRequest, NewValidStorageClassError())
	}
	sc := *pvc.Spec.StorageClassName

	tl := &v1alpha1.TenantList{}
	if err := c.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", pvc.Namespace),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !tl.Items[0].Spec.StorageClasses.IsStringInList(sc) {
		return admission.Errored(http.StatusBadRequest, NewForbiddenStorageClassError(*pvc.Spec.StorageClassName))
	}

	return admission.Allowed("")
}

func (r *handler) OnDelete(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

func (r *handler) OnUpdate(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) admission.Response {
	return admission.Allowed("")
}

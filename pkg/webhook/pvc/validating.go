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

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-pvc,mutating=false,failurePolicy=fail,groups="",resources=persistentvolumeclaims,verbs=create,versions=v1,name=pvc.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetName() string {
	return "Pvc"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-pvc"
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
		pvc := &v1.PersistentVolumeClaim{}

		if err := decoder.Decode(req, pvc); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		tl := &capsulev1alpha1.TenantList{}
		if err := c.List(ctx, tl, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pvc.Namespace),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tl.Items) == 0 {
			return admission.Allowed("")
		}

		tnt := tl.Items[0]

		if tnt.Spec.StorageClasses == nil {
			return admission.Allowed("")
		}

		if pvc.Spec.StorageClassName == nil {
			return admission.Errored(http.StatusBadRequest, NewStorageClassNotValid(*tl.Items[0].Spec.StorageClasses))
		}

		sc := *pvc.Spec.StorageClassName
		valid = tnt.Spec.StorageClasses.ExactMatch(sc)
		matched = tnt.Spec.StorageClasses.RegexMatch(sc)
		if !valid && !matched {
			return admission.Errored(http.StatusBadRequest, NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses))
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

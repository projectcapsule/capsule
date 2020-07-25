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

package owner_reference

import (
	"context"
	"encoding/json"
	"github.com/clastix/capsule/pkg/webhook/utils"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
)

func Add(mgr manager.Manager) error {
	mgr.GetWebhookServer()
	mgr.GetWebhookServer().Register("/mutate-v1-namespace-owner-reference", &webhook.Admission{
		Handler: &ownerRef{
			schema: mgr.GetScheme(),
		},
	})
	return nil
}

type ownerRef struct {
	client  client.Client
	decoder *admission.Decoder
	// injecting the runtime.Scheme for controllerutil.SetOwnerReference
	schema *runtime.Scheme
}

func (r *ownerRef) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Decoding the NS
	ns := &corev1.Namespace{}
	if err := r.decoder.Decode(req, ns); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	g := utils.UserGroupList(req.UserInfo.Groups)
	if !g.IsInCapsuleGroup() {
		// user requested NS creation is not a Capsule user, so skipping the validation checks
		return admission.Allowed("")
	}

	if len(ns.ObjectMeta.Labels) > 0 {
		ln, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		l, ok := ns.ObjectMeta.Labels[ln]
		// assigning namespace to Tenant in case of label
		if ok {
			// retrieving the selected Tenant
			t := &v1alpha1.Tenant{}
			if err := r.client.Get(ctx, types.NamespacedName{Name: l}, t); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			// Tenant owner must adhere to user that asked for NS creation
			if t.Spec.Owner != req.UserInfo.Username {
				return admission.Denied("Cannot assign the desired namespace to a non-owned Tenant")
			}
			// Patching the response
			return r.patchResponseForOwnerRef(t, ns)
		}

	}

	tl := &v1alpha1.TenantList{}
	if err := r.client.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".spec.owner", req.UserInfo.Username),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if len(tl.Items) > 0 {
		return r.patchResponseForOwnerRef(&tl.Items[0], ns)
	}

	return admission.Denied("You do not have any Tenant assigned: please, reach out the system administrators")
}

func (r *ownerRef) patchResponseForOwnerRef(tenant *v1alpha1.Tenant, ns *corev1.Namespace) admission.Response {
	o, _ := json.Marshal(ns.DeepCopy())
	if err := controllerutil.SetControllerReference(tenant, ns, r.schema); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	c, _ := json.Marshal(ns)
	return admission.PatchResponseFromRaw(o, c)
}

func (r *ownerRef) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}

func (r *ownerRef) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

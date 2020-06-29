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

package network_policies

import (
	"context"
	"github.com/clastix/capsule/pkg/webhook/utils"
	"net/http"

	"k8s.io/api/admission/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
)

func Add(mgr manager.Manager) error {
	mgr.GetWebhookServer().Register("/validating-v1-network-policy", &webhook.Admission{
		Handler: &validatingNetworkPolicy{},
	})
	return nil
}

type validatingNetworkPolicy struct {
	client  client.Client
	decoder *admission.Decoder
}

func (r *validatingNetworkPolicy) Handle(ctx context.Context, req admission.Request) admission.Response {
	var err error

	g := utils.UserGroupList(req.UserInfo.Groups)
	if !g.IsInCapsuleGroup() {
		// not a Capsule user, can be skipped
		return admission.Allowed("")
	}

	np := &networkingv1.NetworkPolicy{}
	switch req.Operation {
	case v1beta1.Delete:
		err := r.client.Get(ctx, types.NamespacedName{
			Namespace: req.AdmissionRequest.Namespace,
			Name:      req.AdmissionRequest.Name,
		}, np)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	default:
		if err := r.decoder.Decode(req, np); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		err = r.client.Get(ctx, types.NamespacedName{
			Namespace: np.Namespace,
			Name:      np.Name,
		}, np)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	l, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if _, ok := np.GetLabels()[l]; ok {
		return admission.Denied("Capsule Network Policies cannot be manipulated: please, reach out the system administrators")
	}

	// manipulating user Network Policy: it's safe
	return admission.Allowed("")
}

func (r *validatingNetworkPolicy) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}
func (r *validatingNetworkPolicy) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

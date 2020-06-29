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

package ingress_class

import (
	"context"
	"net/http"

	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/webhook/utils"
)

func AddNetworking(mgr manager.Manager) error {
	mgr.GetWebhookServer().Register("/validating-v1-networking-ingress", &webhook.Admission{
		Handler: &validatingV1{},
	})
	return nil
}

type validatingV1 struct {
	client  client.Client
	decoder *admission.Decoder
}

func (r *validatingV1) Handle(ctx context.Context, req admission.Request) admission.Response {
	g := utils.UserGroupList(req.UserInfo.Groups)
	if !g.IsInCapsuleGroup() {
		// not a Capsule user, can be skipped
		return admission.Allowed("")
	}

	i := &networkingv1beta1.Ingress{}
	if err := r.decoder.Decode(req, i); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return handleIngress(ctx, i, i.Spec.IngressClassName, r.client)
}

func (r *validatingV1) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}

func (r *validatingV1) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

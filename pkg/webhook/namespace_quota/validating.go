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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
)

func Add(mgr manager.Manager) error {
	mgr.GetWebhookServer().Register("/validate-v1-namespace-quota", &webhook.Admission{
		Handler: &nsQuota{},
	})
	return nil
}

type nsQuota struct {
	client  client.Client
	decoder *admission.Decoder
}

func (r *nsQuota) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Decoding the NS
	ns := &corev1.Namespace{}
	if err := r.decoder.Decode(req, ns); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	for _, or := range ns.ObjectMeta.OwnerReferences {
		// retrieving the selected Tenant
		t := &v1alpha1.Tenant{}
		if err := r.client.Get(ctx, types.NamespacedName{Name: or.Name}, t); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if t.IsFull() {
			return admission.Denied("Cannot exceed Namespace quota: please, reach out the system administrators")
		}
	}
	// creating NS that is not bounded to any Tenant
	return admission.Allowed("")
}

func (r *nsQuota) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}

func (r *nsQuota) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

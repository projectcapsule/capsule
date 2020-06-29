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
	"fmt"
	"net/http"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

func Add(mgr manager.Manager) error {
	mgr.GetWebhookServer().Register("/validating-v1-pvc", &webhook.Admission{
		Handler: &validatindPvc{},
	})
	return nil
}

type validatindPvc struct {
	client  client.Client
	decoder *admission.Decoder
}

func (r *validatindPvc) Handle(ctx context.Context, req admission.Request) admission.Response {
	g := utils.UserGroupList(req.UserInfo.Groups)
	if !g.IsInCapsuleGroup() {
		// not a Capsule user, can be skipped
		return admission.Allowed("")
	}

	pvc := &v1.PersistentVolumeClaim{}
	if err := r.decoder.Decode(req, pvc); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pvc.Spec.StorageClassName == nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("A valid Strage Class must be used"))
	}
	sc := *pvc.Spec.StorageClassName

	tl := &v1alpha1.TenantList{}
	if err := r.client.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", pvc.Namespace),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !tl.Items[0].Spec.StorageClasses.IsStringInList(sc) {
		err := fmt.Errorf("Storage Class %s is forbidden for the current Tenant", *pvc.Spec.StorageClassName)
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed("")
}

func (r *validatindPvc) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}

func (r *validatindPvc) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

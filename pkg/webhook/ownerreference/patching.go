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

package ownerreference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/mutate-v1-namespace-owner-reference,mutating=true,failurePolicy=fail,groups="",resources=namespaces,verbs=create,versions=v1,name=owner.namespace.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "OwnerReference"
}

func (w *webhook) GetPath() string {
	return "/mutate-v1-namespace-owner-reference"
}

type handler struct {
	forceTenantPrefix bool
}

func Handler(forceTenantPrefix bool) capsulewebhook.Handler {
	return &handler{
		forceTenantPrefix: forceTenantPrefix,
	}
}

func (h *handler) OnCreate(clt client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		ln, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		// If we already had TenantName label on NS -> assign to it
		if l, ok := ns.ObjectMeta.Labels[ln]; ok {
			// retrieving the selected Tenant
			t := &capsulev1alpha1.Tenant{}
			if err := clt.Get(ctx, types.NamespacedName{Name: l}, t); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			// Tenant owner must adhere to user that asked for NS creation
			if !h.isTenantOwner(t.Spec.Owner, req.UserInfo) {
				return admission.Denied("Cannot assign the desired namespace to a non-owned Tenant")
			}
			// Patching the response
			return h.patchResponseForOwnerRef(t, ns)
		}

		// If we forceTenantPrefix -> find Tenant from NS name
		var tenants sortedTenants

		// Find tenants belonging to user
		{
			tl, err := h.listTenantsForOwnerKind(ctx, "User", req.UserInfo.Username, clt)
			if err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			for _, tnt := range tl.Items {
				tenants = append(tenants, tnt)
			}
		}
		// Find tenants belonging to user groups
		{
			for _, group := range req.UserInfo.Groups {
				tl, err := h.listTenantsForOwnerKind(ctx, "Group", group, clt)
				if err != nil {
					return admission.Errored(http.StatusBadRequest, err)
				}
				for _, tnt := range tl.Items {
					tenants = append(tenants, tnt)
				}
			}
		}

		sort.Sort(sort.Reverse(tenants))

		if len(tenants) == 0 {
			return admission.Denied("You do not have any Tenant assigned: please, reach out the system administrators")
		}

		if len(tenants) == 1 {
			return h.patchResponseForOwnerRef(&tenants[0], ns)
		}

		if h.forceTenantPrefix {
			for _, tnt := range tenants {
				if strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
					return h.patchResponseForOwnerRef(tnt.DeepCopy(), ns)
				}
			}
			admission.Denied("The Namespace prefix used doesn't match any available Tenant")
		}

		return admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")
	}
}
func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Denied("Capsule user cannot update a Namespace")
	}
}

func (h *handler) patchResponseForOwnerRef(tenant *capsulev1alpha1.Tenant, ns *corev1.Namespace) admission.Response {
	scheme := runtime.NewScheme()
	_ = capsulev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	o, _ := json.Marshal(ns.DeepCopy())
	if err := controllerutil.SetControllerReference(tenant, ns, scheme); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	c, _ := json.Marshal(ns)
	return admission.PatchResponseFromRaw(o, c)
}

func (h *handler) listTenantsForOwnerKind(ctx context.Context, ownerKind string, ownerName string, clt client.Client) (*capsulev1alpha1.TenantList, error) {
	tl := &capsulev1alpha1.TenantList{}
	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	err := clt.List(ctx, tl, f)
	return tl, err
}

func (h *handler) isTenantOwner(os capsulev1alpha1.OwnerSpec, userInfo authenticationv1.UserInfo) bool {
	if os.Kind == "User" && userInfo.Username == os.Name {
		return true
	}
	if os.Kind == "Group" {
		for _, group := range userInfo.Groups {
			if group == os.Name {
				return true
			}
		}
	}
	return false
}

type sortedTenants []capsulev1alpha1.Tenant

func (s sortedTenants) Len() int {
	return len(s)
}

func (s sortedTenants) Less(i, j int) bool {
	return len(s[i].GetName()) < len(s[j].GetName())
}

func (s sortedTenants) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

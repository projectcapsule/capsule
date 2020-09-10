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
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	authenticationv1 "k8s.io/api/authentication/v1"
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
		if len(ns.ObjectMeta.Labels) > 0 {
			l, ok := ns.ObjectMeta.Labels[ln]
			// assigning namespace to Tenant in case of label
			if ok {
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

		}
		// If we forceTenantPrefix -> find Tenant from NS name
		if h.forceTenantPrefix {
			t := &v1alpha1.Tenant{}
			tenantName := strings.Split(ns.GetName(), "-")[0]
			if err := clt.Get(ctx, types.NamespacedName{Name: tenantName}, t); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			return h.patchResponseForOwnerRef(t, ns)
		}

		tenants := []*capsulev1alpha1.Tenant{}

		// Find tenants belonging to user
		tlu, err := h.listTenantsForOwnerKind(ctx, "User", req.UserInfo.Username, clt)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		// No groups single tenant short-circuit
		if len(req.UserInfo.Groups) == 0 && len(tlu.Items) == 1 {
			return h.patchResponseForOwnerRef(&tlu.Items[0], ns)
		}

		switch userTenants := len(tlu.Items); {
		case userTenants > 1:
			return admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")
		case userTenants == 1:
			tenants = append(tenants, &tlu.Items[0])
		}

		for _, group := range req.UserInfo.Groups {
			tl, err := h.listTenantsForOwnerKind(ctx, "Group", group, clt)
			if err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			for _, item := range tl.Items {
				tenants = append(tenants, &item)
			}
			// more than one tenant found, returning error
			if len(tenants) > 1 {
				return admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")
			}
		}

		// Single tenant found for group
		if len(tenants) == 1 {
			return h.patchResponseForOwnerRef(tenants[0], ns)
		}

		return admission.Denied("You do not have any Tenant assigned: please, reach out the system administrators")
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

func (h *handler) listTenantsForOwnerKind(ctx context.Context, ownerKind string, ownerName string, clt client.Client) (*v1alpha1.TenantList, error) {
	tl := &v1alpha1.TenantList{}
	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	err := clt.List(ctx, tl, f)
	return tl, err
}

func (h *handler) isTenantOwner(os v1alpha1.OwnerSpec, userInfo authenticationv1.UserInfo) bool {
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

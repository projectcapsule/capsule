// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

type handler struct {
	cfg configuration.Configuration
}

func Handler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &handler{
		cfg: cfg,
	}
}

func (h *handler) OnCreate(clt client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}
		ln, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
		if err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}
		// If we already had TenantName label on NS -> assign to it
		if label, ok := ns.ObjectMeta.Labels[ln]; ok {
			// retrieving the selected Tenant
			tnt := &capsulev1alpha1.Tenant{}
			if err = clt.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
				response := admission.Errored(http.StatusBadRequest, err)

				return &response
			}
			// Tenant owner must adhere to user that asked for NS creation
			if !h.isTenantOwner(tnt.Spec.Owner, req.UserInfo) {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "NonOwnedTenant", "Namespace %s cannot be assigned to the current Tenant", ns.GetName())

				response := admission.Denied("Cannot assign the desired namespace to a non-owned Tenant")

				return &response
			}
			// Patching the response
			response := h.patchResponseForOwnerRef(tnt, ns, recorder)

			return &response
		}

		// If we forceTenantPrefix -> find Tenant from NS name
		var tenants sortedTenants

		// Find tenants belonging to user
		{
			var tntList *capsulev1alpha1.TenantList

			if tntList, err = h.listTenantsForOwnerKind(ctx, "User", req.UserInfo.Username, clt); err != nil {
				response := admission.Errored(http.StatusBadRequest, err)

				return &response
			}

			for _, tnt := range tntList.Items {
				tenants = append(tenants, tnt)
			}
		}
		// Find tenants belonging to user groups
		{
			for _, group := range req.UserInfo.Groups {
				tntList, err := h.listTenantsForOwnerKind(ctx, "Group", group, clt)
				if err != nil {
					response := admission.Errored(http.StatusBadRequest, err)

					return &response
				}

				for _, tnt := range tntList.Items {
					tenants = append(tenants, tnt)
				}
			}
		}

		sort.Sort(sort.Reverse(tenants))

		if len(tenants) == 0 {
			response := admission.Denied("You do not have any Tenant assigned: please, reach out to the system administrators")

			return &response
		}

		if len(tenants) == 1 {
			response := h.patchResponseForOwnerRef(&tenants[0], ns, recorder)

			return &response
		}

		if h.cfg.ForceTenantPrefix() {
			for _, tnt := range tenants {
				if strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
					response := h.patchResponseForOwnerRef(tnt.DeepCopy(), ns, recorder)

					return &response
				}
			}
			response := admission.Denied("The Namespace prefix used doesn't match any available Tenant")

			return &response
		}

		response := admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")

		return &response
	}
}
func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) patchResponseForOwnerRef(tenant *capsulev1alpha1.Tenant, ns *corev1.Namespace, recorder record.EventRecorder) admission.Response {
	scheme := runtime.NewScheme()
	_ = capsulev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	o, _ := json.Marshal(ns.DeepCopy())
	if err := controllerutil.SetControllerReference(tenant, ns, scheme); err != nil {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "Error", "Namespace %s cannot be assigned to the desired Tenant", ns.GetName())

		return admission.Errored(http.StatusInternalServerError, err)
	}

	recorder.Eventf(tenant, corev1.EventTypeNormal, "NamespaceCreationWebhook", "Namespace %s has been assigned to the desired Tenant", ns.GetName())

	c, _ := json.Marshal(ns)
	return admission.PatchResponseFromRaw(o, c)
}

func (h *handler) listTenantsForOwnerKind(ctx context.Context, ownerKind string, ownerName string, clt client.Client) (*capsulev1alpha1.TenantList, error) {
	tntList := &capsulev1alpha1.TenantList{}
	fields := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	err := clt.List(ctx, tntList, fields)
	return tntList, err
}

func (h *handler) isTenantOwner(ownerSpec capsulev1alpha1.OwnerSpec, userInfo authenticationv1.UserInfo) bool {
	if ownerSpec.Kind == "User" && userInfo.Username == ownerSpec.Name {
		return true
	}
	if ownerSpec.Kind == "Group" {
		for _, group := range userInfo.Groups {
			if group == ownerSpec.Name {
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

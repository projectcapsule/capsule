// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package ownerreference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type handler struct {
	cfg configuration.Configuration
}

func Handler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &handler{
		cfg: cfg,
	}
}

func (h *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.setOwnerRef(ctx, req, client, decoder, recorder)
	}
}

func (h *handler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta2.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", oldNs.Name),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		if !h.namespaceIsOwned(oldNs, tntList, req) {
			recorder.Eventf(oldNs, corev1.EventTypeWarning, "OfflimitNamespace", "Namespace %s can not be patched", oldNs.GetName())

			response := admission.Denied("Denied patch request for this namespace")

			return &response
		}

		newNs := &corev1.Namespace{}
		if err := decoder.Decode(req, newNs); err != nil {
			return utils.ErroredResponse(err)
		}

		o, err := json.Marshal(newNs.DeepCopy())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		var refs []metav1.OwnerReference

		for _, ref := range oldNs.OwnerReferences {
			if capsuleutils.IsTenantOwnerReference(ref) {
				refs = append(refs, ref)
			}
		}

		for _, ref := range newNs.OwnerReferences {
			if !capsuleutils.IsTenantOwnerReference(ref) {
				refs = append(refs, ref)
			}
		}

		newNs.OwnerReferences = refs

		c, err := json.Marshal(newNs)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(o, c)

		return &response
	}
}

func (h *handler) namespaceIsOwned(ns *corev1.Namespace, tenantList *capsulev1beta2.TenantList, req admission.Request) bool {
	for _, tenant := range tenantList.Items {
		for _, ownerRef := range ns.OwnerReferences {
			if !capsuleutils.IsTenantOwnerReference(ownerRef) {
				continue
			}

			if ownerRef.UID == tenant.UID && utils.IsTenantOwner(tenant.Spec.Owners, req.UserInfo) {
				return true
			}
		}
	}

	return false
}

func (h *handler) setOwnerRef(ctx context.Context, req admission.Request, client client.Client, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	ns := &corev1.Namespace{}
	if err := decoder.Decode(req, ns); err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return &response
	}

	ln, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return &response
	}
	// If we already had TenantName label on NS -> assign to it

	if label, ok := ns.Labels[ln]; ok {
		// retrieving the selected Tenant
		tnt := &capsulev1beta2.Tenant{}
		if err = client.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}
		// Tenant owner must adhere to user that asked for NS creation
		if !utils.IsTenantOwner(tnt.Spec.Owners, req.UserInfo) {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "NonOwnedTenant", "Namespace %s cannot be assigned to the current Tenant", ns.GetName())

			response := admission.Denied("Cannot assign the desired namespace to a non-owned Tenant")

			return &response
		}
		// Check if namespace needs Tenant name prefix
		if errResponse := h.validateNamespacePrefix(ns, tnt); errResponse != nil {
			return errResponse
		}
		// Patching the response
		response := h.patchResponseForOwnerRef(tnt, ns, recorder)

		return &response
	}

	// If we forceTenantPrefix -> find Tenant from NS name
	var tenants sortedTenants

	// Find tenants belonging to user (it can be regular user or ServiceAccount)
	if strings.HasPrefix(req.UserInfo.Username, "system:serviceaccount:") {
		var tntList *capsulev1beta2.TenantList

		if tntList, err = h.listTenantsForOwnerKind(ctx, "ServiceAccount", req.UserInfo.Username, client); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}

		for _, tnt := range tntList.Items {
			tenants = append(tenants, tnt)
		}
	} else {
		var tntList *capsulev1beta2.TenantList

		if tntList, err = h.listTenantsForOwnerKind(ctx, "User", req.UserInfo.Username, client); err != nil {
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
			tntList, err := h.listTenantsForOwnerKind(ctx, "Group", group, client)
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
		// Check if namespace needs Tenant name prefix
		if errResponse := h.validateNamespacePrefix(ns, &tenants[0]); errResponse != nil {
			return errResponse
		}

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

func (h *handler) patchResponseForOwnerRef(tenant *capsulev1beta2.Tenant, ns *corev1.Namespace, recorder record.EventRecorder) admission.Response {
	scheme := runtime.NewScheme()
	_ = capsulev1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	o, err := json.Marshal(ns.DeepCopy())
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if err = controllerutil.SetOwnerReference(tenant, ns, scheme); err != nil {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "Error", "Namespace %s cannot be assigned to the desired Tenant", ns.GetName())

		return admission.Errored(http.StatusInternalServerError, err)
	}

	recorder.Eventf(tenant, corev1.EventTypeNormal, "NamespaceCreationWebhook", "Namespace %s has been assigned to the desired Tenant", ns.GetName())

	c, err := json.Marshal(ns)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(o, c)
}

func (h *handler) listTenantsForOwnerKind(ctx context.Context, ownerKind string, ownerName string, clt client.Client) (*capsulev1beta2.TenantList, error) {
	tntList := &capsulev1beta2.TenantList{}
	fields := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	err := clt.List(ctx, tntList, fields)

	return tntList, err
}

func (h *handler) validateNamespacePrefix(ns *corev1.Namespace, tenant *capsulev1beta2.Tenant) *admission.Response {
	// Check if ForceTenantPrefix is true
	if tenant.Spec.ForceTenantPrefix != nil && *tenant.Spec.ForceTenantPrefix {
		if !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tenant.GetName())) {
			response := admission.Denied(fmt.Sprintf("The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.", tenant.GetName()))

			return &response
		}
	}

	return nil
}

type sortedTenants []capsulev1beta2.Tenant

func (s sortedTenants) Len() int {
	return len(s)
}

func (s sortedTenants) Less(i, j int) bool {
	return len(s[i].GetName()) < len(s[j].GetName())
}

func (s sortedTenants) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

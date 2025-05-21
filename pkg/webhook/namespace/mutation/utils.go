// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	v1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

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

// getNamespaceTenant returns namespace owner tenant.
func getNamespaceTenant(
	ctx context.Context,
	client client.Client,
	ns *corev1.Namespace,
	req admission.Request,
	cfg configuration.Configuration,
	recorder record.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	tenant, errResponse := getTenantByLabels(ctx, client, ns, req, recorder)
	if errResponse != nil {
		return nil, errResponse
	}

	if tenant == nil {
		tenant, errResponse = getTenantByUserInfo(ctx, ns, req.UserInfo, client, cfg)
		if errResponse != nil {
			return nil, errResponse
		}
	}

	return tenant, nil
}

// getTenantByLabels returns tenant from labels.
func getTenantByLabels(
	ctx context.Context,
	client client.Client,
	ns *corev1.Namespace,
	req admission.Request,
	recorder record.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	ln, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	// Get tenant from namespace labels.
	if label, ok := ns.Labels[ln]; ok {
		// retrieving the selected Tenant
		tnt := &capsulev1beta2.Tenant{}
		if err = client.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return nil, &response
		}
		// Tenant owner must adhere to user that asked for NS creation
		if !utils.IsTenantOwner(tnt.Spec.Owners, req.UserInfo) {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "NonOwnedTenant", "Namespace %s cannot be assigned to the current Tenant", ns.GetName())

			response := admission.Denied("Cannot assign the desired namespace to a non-owned Tenant")

			return nil, &response
		}

		return tnt, nil
	}

	// Nothing found in the labels.
	return nil, nil
}

// getTenantByUserInfo returns tenant list associated with admission request userinfo.
func getTenantByUserInfo(
	ctx context.Context,
	ns *corev1.Namespace,
	userInfo v1.UserInfo,
	clt client.Client,
	cfg configuration.Configuration,
) (*capsulev1beta2.Tenant, *admission.Response) {
	var tenants sortedTenants

	// User tenants.
	userTntList := &capsulev1beta2.TenantList{}
	fields := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("User:%s", userInfo.Username),
	}

	err := clt.List(ctx, userTntList, fields)
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	tenants = userTntList.Items

	// ServiceAccount tenants.
	if strings.HasPrefix(userInfo.Username, "system:serviceaccount:") {
		saTntList := &capsulev1beta2.TenantList{}
		fields = client.MatchingFields{
			".spec.owner.ownerkind": fmt.Sprintf("ServiceAccount:%s", userInfo.Username),
		}

		err = clt.List(ctx, saTntList, fields)
		if err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return nil, &response
		}

		tenants = append(tenants, saTntList.Items...)
	}

	// Group tenants.
	groupTntList := &capsulev1beta2.TenantList{}

	for _, group := range userInfo.Groups {
		fields = client.MatchingFields{
			".spec.owner.ownerkind": fmt.Sprintf("Group:%s", group),
		}

		err = clt.List(ctx, groupTntList, fields)
		if err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return nil, &response
		}

		tenants = append(tenants, groupTntList.Items...)
	}

	sort.Sort(sort.Reverse(tenants))

	if len(tenants) == 0 {
		response := admission.Denied("You do not have any Tenant assigned: please, reach out to the system administrators")

		return nil, &response
	}

	if len(tenants) == 1 {
		// Check if namespace needs Tenant name prefix
		if !validateNamespacePrefix(ns, &tenants[0]) {
			response := admission.Denied(fmt.Sprintf("The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.", tenants[0].GetName()))

			return nil, &response
		}

		return &tenants[0], nil
	}

	if cfg.ForceTenantPrefix() {
		for _, tnt := range tenants {
			if strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
				return &tnt, nil
			}
		}

		response := admission.Denied("The Namespace prefix used doesn't match any available Tenant")

		return nil, &response
	}

	return nil, nil
}

func validateNamespacePrefix(ns *corev1.Namespace, tenant *capsulev1beta2.Tenant) bool {
	// Check if ForceTenantPrefix is true
	if tenant.Spec.ForceTenantPrefix != nil && *tenant.Spec.ForceTenantPrefix {
		if !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tenant.GetName())) {
			return false
		}
	}

	return true
}

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"sort"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TenantByStatusNamespace(
	ctx context.Context,
	c client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, error) {
	var tntList capsulev1beta2.TenantList
	if err := c.List(ctx, &tntList, client.MatchingFields{".status.namespaces": namespace}); err != nil {
		return nil, err
	}

	if len(tntList.Items) == 0 {
		return nil, nil
	}

	t := tntList.Items[0].DeepCopy()

	return t, nil
}

func GetTenantNameByStatusNamespace(
	ctx context.Context,
	c client.Client,
	namespace string,
) (string, error) {
	var tntList capsulev1beta2.TenantList
	if err := c.List(ctx, &tntList, client.MatchingFields{".status.namespaces": namespace}); err != nil {
		return "", err
	}

	if len(tntList.Items) == 0 {
		return "", nil
	}

	return tntList.Items[0].GetName(), nil
}

func IsNamespaceInTenant(
	ctx context.Context,
	c client.Client,
	namespace string,
) (bool, error) {
	var tntList capsulev1beta2.TenantList
	if err := c.List(ctx, &tntList, client.MatchingFields{".status.namespaces": namespace}); err != nil {
		return false, err
	}

	if len(tntList.Items) == 0 {
		return false, nil
	}

	return true, nil
}

func GetTenantNameByNamespace(
	ctx context.Context,
	c client.Reader,
	namespace string,
) (tnt string, err error) {
	var ns corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: namespace}, &ns); err != nil {
		return "", err
	}

	tntName, ok := GetTenantNameByOwnerreferences(ns.OwnerReferences)
	if !ok {
		return "", nil
	}

	return tntName, nil
}

func GetTenantByNamespace(
	ctx context.Context,
	r client.Reader,
	namespace string,
) (*capsulev1beta2.Tenant, error) {
	var ns corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: namespace}, &ns); err != nil {
		return nil, err
	}

	for _, or := range ns.GetOwnerReferences() {
		if !IsTenantOwnerReference(or) {
			continue
		}

		tnt := &capsulev1beta2.Tenant{}
		if err := r.Get(ctx, client.ObjectKey{Name: or.Name}, tnt); err != nil {
			return nil, err
		}

		if or.UID != "" && tnt.UID != or.UID {
			return nil, fmt.Errorf(
				"tenant ownerReference UID mismatch for %q: namespace references UID %q but tenant has UID %q",
				or.Name, or.UID, tnt.UID,
			)
		}

		return tnt, nil
	}

	return nil, nil
}

func GetTenantNameByOwnerreferences(
	refs []metav1.OwnerReference,
) (string, bool) {
	for _, or := range refs {
		if IsTenantOwnerReference(or) {
			return or.Name, true
		}
	}

	return "", false
}

// getNamespaceTenant returns namespace owner tenant.
func GetTenantByOwnerreferences(
	ctx context.Context,
	c client.Reader,
	refs []metav1.OwnerReference,
) (tnt *capsulev1beta2.Tenant, err error) {
	for _, or := range refs {
		if !IsTenantOwnerReference(or) {
			continue
		}

		tnt = &capsulev1beta2.Tenant{}
		if err = c.Get(ctx, types.NamespacedName{Name: or.Name}, tnt); err != nil {
			return nil, err
		}

		return tnt, nil
	}

	return nil, nil
}

func GetTenantByUserInfo(
	ctx context.Context,
	c client.Reader,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	username string,
	groups []string,
) (sortedTenants, error) {
	var tenants sortedTenants

	// User tenants.
	userTntList := &capsulev1beta2.TenantList{}
	fields := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("User:%s", username),
	}

	err := c.List(ctx, userTntList, fields)
	if err != nil {
		return nil, err
	}

	tenants = userTntList.Items

	// ServiceAccount tenants.
	if strings.HasPrefix(username, "system:serviceaccount:") {
		saTntList := &capsulev1beta2.TenantList{}
		fields = client.MatchingFields{
			".spec.owner.ownerkind": fmt.Sprintf("ServiceAccount:%s", username),
		}

		err = c.List(ctx, saTntList, fields)
		if err != nil {
			return nil, err
		}

		tenants = append(tenants, saTntList.Items...)
	}

	// Group tenants.
	groupTntList := &capsulev1beta2.TenantList{}

	for _, group := range groups {
		fields = client.MatchingFields{
			".spec.owner.ownerkind": fmt.Sprintf("Group:%s", group),
		}

		err = c.List(ctx, groupTntList, fields)
		if err != nil {
			return nil, err
		}

		tenants = append(tenants, groupTntList.Items...)
	}

	sort.Sort(sort.Reverse(tenants))

	return tenants, nil
}

// getTenantByLabels returns tenant from labels.
func GetTenantByLabels(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	if label, ok := ns.Labels[meta.TenantLabel]; ok {
		tnt := &capsulev1beta2.Tenant{}
		if err := c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			return nil, err
		}

		return tnt, nil
	}

	// Nothing found in the labels.
	return nil, nil
}

func GetTenantByLabelsAndUser(
	ctx context.Context,
	c client.Reader,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	userInfo authenticationv1.UserInfo,
) (*capsulev1beta2.Tenant, error) {
	tnt, err := GetTenantByLabels(ctx, c, ns)
	if err != nil {
		return nil, err
	}

	if tnt != nil {
		if ok := users.IsTenantOwnerByStatus(tnt, userInfo); !ok {
			return nil, fmt.Errorf("can not assign the desired namespace to a non-owned Tenant")
		}

		return tnt, nil
	}

	return nil, nil
}

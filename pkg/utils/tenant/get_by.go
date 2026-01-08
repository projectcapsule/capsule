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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

func TenantByStatusNamespace(
	ctx context.Context,
	c client.Client,
	namespace string,
) (*capsulev1beta2.Tenant, error) {
	tntList := &capsulev1beta2.TenantList{}

	if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return nil, err
	}

	if len(tntList.Items) == 0 {
		return nil, nil
	}

	tnt := &capsulev1beta2.Tenant{}
	*tnt = tntList.Items[0]

	return tnt, nil
}

// getNamespaceTenant returns namespace owner tenant.
func GetTenantByOwnerreferences(
	ctx context.Context,
	c client.Client,
	refs []v1.OwnerReference,
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
	c client.Client,
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
	c client.Client,
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
	c client.Client,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	userInfo authenticationv1.UserInfo,
) (*capsulev1beta2.Tenant, error) {
	tnt, err := GetTenantByLabels(ctx, c, ns)
	if err != nil {
		return nil, err
	}

	if tnt != nil {
		if ok := users.IsTenantOwnerByStatus(ctx, c, cfg, tnt, userInfo); !ok {
			return nil, fmt.Errorf("can not assign the desired namespace to a non-owned Tenant")
		}

		return tnt, nil
	}

	return nil, nil
}

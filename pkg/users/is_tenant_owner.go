// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"context"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func IsTenantOwner(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	userInfo authenticationv1.UserInfo,
) (bool, error) {
	if isOwner := tnt.Spec.Owners.IsOwner(userInfo.Username, userInfo.Groups); isOwner {
		return true, nil
	}

	return IsCommonOwner(ctx, c, cfg, tnt, userInfo)
}

func IsTenantOwnerByStatus(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	userInfo authenticationv1.UserInfo,
) bool {
	return tnt.Status.Owners.IsOwner(userInfo.Username, userInfo.Groups)
}

func IsCommonOwner(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	userInfo authenticationv1.UserInfo,
) (bool, error) {
	// Administrators are always Owners
	if cfg.Administrators().IsPresent(userInfo.Username, userInfo.Groups) {
		return true, nil
	}

	if cfg.AllowServiceAccountPromotion() {
		parts := strings.Split(userInfo.Username, ":")

		if len(parts) != 4 {
			return false, nil
		}

		saList := &corev1.ServiceAccountList{}
		if err := c.List(ctx, saList,
			client.InNamespace(parts[2]),
			client.MatchingLabels{
				meta.OwnerPromotionLabel: meta.OwnerPromotionLabelTrigger,
			}); err != nil {
			return false, err
		}

		for _, sa := range saList.Items {
			saName := serviceaccount.ServiceAccountUsernamePrefix + sa.Namespace + ":" + sa.Name

			if userInfo.Username == saName {
				return true, nil
			}
		}
	}

	return false, nil
}
